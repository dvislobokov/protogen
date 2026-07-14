// Package openapival enriches a generated OpenAPI v3 document with the
// protovalidate (buf.validate) field constraints found in the descriptors, plus
// enum value lists — so the schema reflects the same rules enforced at runtime.
//
// gnostic emits schemas purely from field types; it does not read buf.validate.
// We post-process its openapi.yaml and add: minLength/maxLength, pattern, format
// (email/uuid/...), minimum/maximum (+exclusive), enum (from const/in and enum
// fields), minItems/maxItems/uniqueItems, and required.
//
// Importing the buf/validate generated package here also registers the
// buf.validate extensions in the global type registry, so the compiler's
// normalizeExtensions step decodes them into concrete types and GetExtension
// below can read them.
package openapival

import (
	"os"
	"strconv"

	validate "buf.build/gen/go/bufbuild/protovalidate/protocolbuffers/go/buf/validate"
	yaml "go.yaml.in/yaml/v3"
	"google.golang.org/genproto/googleapis/api/annotations"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/descriptorpb"
)

type scalarLit struct{ value, tag string }

// enumEntry is one value of a proto enum.
type enumEntry struct {
	num  int32
	name string
}

// Options controls how the OpenAPI document is enriched.
type Options struct {
	// EnumsAsStrings renders enum-typed fields as `type: string` with the enum
	// value names — matching how grpc-gateway's protojson marshaler serializes
	// enums. When false, they stay numeric with an x-enum-varnames hint.
	EnumsAsStrings bool
}

// fieldRule is the subset of constraints we surface into OpenAPI.
type fieldRule struct {
	required                   bool
	readOnly, writeOnly        bool
	minLength, maxLength       *uint64
	pattern                    *string
	format                     *string
	minimum, maximum           *string
	exclusiveMin, exclusiveMax bool
	enum                       []scalarLit // from string/numeric const/in
	enumField                  []enumEntry // allowed values of an enum-typed field
	minItems, maxItems         *uint64
	uniqueItems                bool
}

// EnrichFile rewrites the OpenAPI v3 document at path, adding schema keywords
// derived from buf.validate constraints (and enum value lists) in files.
func EnrichFile(path string, files []*descriptorpb.FileDescriptorProto, opts Options) error {
	enums := buildEnumIndex(files)
	index := buildIndex(files, enums)
	if len(index) == 0 {
		return nil
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return err
	}
	if len(doc.Content) == 0 {
		return nil
	}
	root := doc.Content[0]
	schemas := dig(root, "components", "schemas")
	if schemas == nil {
		return nil
	}

	for i := 0; i+1 < len(schemas.Content); i += 2 {
		if rules, ok := index[schemas.Content[i].Value]; ok {
			applySchema(schemas.Content[i+1], rules, opts)
		}
	}

	// Document the validation-failure contract: a 400 problem+json response on
	// every operation whose request message is validated, plus its schema.
	addValidationResponses(root, files)

	out, err := yaml.Marshal(&doc)
	if err != nil {
		return err
	}
	return os.WriteFile(path, out, 0o644)
}

func applySchema(schema *yaml.Node, rules map[string]fieldRule, opts Options) {
	props := mapGet(schema, "properties")
	var required []string
	for name, r := range rules {
		if r.required {
			required = append(required, name)
		}
		prop := mapGet(props, name)
		if prop == nil {
			continue
		}
		if r.minLength != nil {
			setScalar(prop, "minLength", uintStr(*r.minLength), "!!int")
		}
		if r.maxLength != nil {
			setScalar(prop, "maxLength", uintStr(*r.maxLength), "!!int")
		}
		if r.pattern != nil {
			setScalar(prop, "pattern", *r.pattern, "!!str")
		}
		if r.format != nil {
			setScalar(prop, "format", *r.format, "!!str")
		}
		if r.minimum != nil {
			setScalar(prop, "minimum", *r.minimum, numTag(*r.minimum))
			if r.exclusiveMin {
				setScalar(prop, "exclusiveMinimum", "true", "!!bool")
			}
		}
		if r.maximum != nil {
			setScalar(prop, "maximum", *r.maximum, numTag(*r.maximum))
			if r.exclusiveMax {
				setScalar(prop, "exclusiveMaximum", "true", "!!bool")
			}
		}
		if r.minItems != nil {
			setScalar(prop, "minItems", uintStr(*r.minItems), "!!int")
		}
		if r.maxItems != nil {
			setScalar(prop, "maxItems", uintStr(*r.maxItems), "!!int")
		}
		if r.uniqueItems {
			setScalar(prop, "uniqueItems", "true", "!!bool")
		}
		if r.readOnly {
			setScalar(prop, "readOnly", "true", "!!bool")
		}
		if r.writeOnly {
			setScalar(prop, "writeOnly", "true", "!!bool")
		}
		if len(r.enum) > 0 {
			setSequence(prop, "enum", r.enum)
		}
		if len(r.enumField) > 0 {
			applyEnumStyle(prop, r.enumField, opts)
		}
	}
	if len(required) > 0 {
		setRequired(schema, required)
	}
}

// applyEnumStyle renders an enum-typed field either as string names (matching
// grpc-gateway's protojson output) or as numbers with an x-enum-varnames hint.
func applyEnumStyle(prop *yaml.Node, entries []enumEntry, opts Options) {
	if opts.EnumsAsStrings {
		names := make([]scalarLit, len(entries))
		for i, e := range entries {
			names[i] = scalarLit{e.name, "!!str"}
		}
		setScalar(prop, "type", "string", "!!str") // override gnostic's integer
		removeKey(prop, "format")                  // "format: enum" no longer applies
		setSequence(prop, "enum", names)
		return
	}
	nums := make([]scalarLit, len(entries))
	varnames := make([]scalarLit, len(entries))
	for i, e := range entries {
		nums[i] = scalarLit{strconv.FormatInt(int64(e.num), 10), "!!int"}
		varnames[i] = scalarLit{e.name, "!!str"}
	}
	setSequence(prop, "enum", nums)
	setSequence(prop, "x-enum-varnames", varnames)
}

// ---- descriptor -> constraint index ----

func buildIndex(files []*descriptorpb.FileDescriptorProto, enums map[string][]enumEntry) map[string]map[string]fieldRule {
	index := map[string]map[string]fieldRule{}
	for _, f := range files {
		for _, m := range f.GetMessageType() {
			walkMessage("", m, index, enums)
		}
	}
	return index
}

func walkMessage(parentPrefix string, m *descriptorpb.DescriptorProto, index map[string]map[string]fieldRule, enums map[string][]enumEntry) {
	schemaName := parentPrefix + m.GetName()
	for _, field := range m.GetField() {
		r, ok := ruleFromField(field, enums)
		if !ok {
			continue
		}
		if index[schemaName] == nil {
			index[schemaName] = map[string]fieldRule{}
		}
		index[schemaName][field.GetJsonName()] = r
	}
	// gnostic prefixes nested messages with the immediate parent name + "_".
	for _, nested := range m.GetNestedType() {
		walkMessage(schemaName+"_", nested, index, enums)
	}
}

func ruleFromField(field *descriptorpb.FieldDescriptorProto, enums map[string][]enumEntry) (fieldRule, bool) {
	var r fieldRule
	found := false

	fr := fieldRules(field)
	if fr != nil {
		found = applyProtovalidate(&r, fr) || found
	}

	// google.api.field_behavior -> required / readOnly / writeOnly.
	for _, b := range fieldBehaviors(field) {
		switch b {
		case annotations.FieldBehavior_REQUIRED:
			r.required, found = true, true
		case annotations.FieldBehavior_OUTPUT_ONLY:
			r.readOnly, found = true, true
		case annotations.FieldBehavior_INPUT_ONLY:
			r.writeOnly, found = true, true
		}
	}

	// Enum-typed field: surface its allowed values even without a rule.
	if field.GetType() == descriptorpb.FieldDescriptorProto_TYPE_ENUM {
		if applyEnumField(&r, field, fr, enums) {
			found = true
		}
	}
	return r, found
}

func fieldBehaviors(field *descriptorpb.FieldDescriptorProto) []annotations.FieldBehavior {
	opts := field.GetOptions()
	if opts == nil {
		return nil
	}
	bs, _ := proto.GetExtension(opts, annotations.E_FieldBehavior).([]annotations.FieldBehavior)
	return bs
}

func fieldRules(field *descriptorpb.FieldDescriptorProto) *validate.FieldRules {
	opts := field.GetOptions()
	if opts == nil {
		return nil
	}
	fr, _ := proto.GetExtension(opts, validate.E_Field).(*validate.FieldRules)
	return fr
}

func applyProtovalidate(r *fieldRule, fr *validate.FieldRules) bool {
	found := false
	if fr.GetRequired() {
		r.required, found = true, true
	}
	if s := fr.GetString_(); s != nil {
		found = applyString(r, s) || found
	}
	if b := fr.GetBool(); b != nil && b.HasConst() {
		r.enum = append(r.enum, scalarLit{strconv.FormatBool(b.GetConst()), "!!bool"})
		found = true
	}
	if rep := fr.GetRepeated(); rep != nil {
		found = applyRepeated(r, rep) || found
	}
	found = applyNumeric(r, fr) || found
	return found
}

func applyString(r *fieldRule, s *validate.StringRules) bool {
	found := false
	if s.HasMinLen() {
		v := s.GetMinLen()
		r.minLength, found = &v, true
	}
	if s.HasMaxLen() {
		v := s.GetMaxLen()
		r.maxLength, found = &v, true
	}
	if s.HasLen() { // exact length == min == max
		v := s.GetLen()
		r.minLength, r.maxLength, found = &v, &v, true
	}
	if s.HasPattern() {
		v := s.GetPattern()
		r.pattern, found = &v, true
	}
	if f := stringFormat(s); f != "" {
		r.format, found = &f, true
	}
	if s.HasConst() {
		r.enum, found = append(r.enum, scalarLit{s.GetConst(), "!!str"}), true
	}
	for _, v := range s.GetIn() {
		r.enum, found = append(r.enum, scalarLit{v, "!!str"}), true
	}
	return found
}

func stringFormat(s *validate.StringRules) string {
	switch {
	case s.HasEmail() && s.GetEmail():
		return "email"
	case s.HasUuid() && s.GetUuid():
		return "uuid"
	case s.HasHostname() && s.GetHostname():
		return "hostname"
	case s.HasIpv4() && s.GetIpv4():
		return "ipv4"
	case s.HasUri() && s.GetUri():
		return "uri"
	}
	return ""
}

func applyRepeated(r *fieldRule, rep *validate.RepeatedRules) bool {
	found := false
	if rep.HasMinItems() {
		v := rep.GetMinItems()
		r.minItems, found = &v, true
	}
	if rep.HasMaxItems() {
		v := rep.GetMaxItems()
		r.maxItems, found = &v, true
	}
	if rep.HasUnique() && rep.GetUnique() {
		r.uniqueItems, found = true, true
	}
	return found
}

// applyNumeric handles the common scalar numeric rule types.
func applyNumeric(r *fieldRule, fr *validate.FieldRules) bool {
	setRange := func(hasGte bool, gte string, hasLte bool, lte string, hasGt bool, gt string, hasLt bool, lt string, hasConst bool, cst string) bool {
		found := false
		if hasGte {
			r.minimum, found = &gte, true
		}
		if hasGt {
			r.minimum, r.exclusiveMin, found = &gt, true, true
		}
		if hasLte {
			r.maximum, found = &lte, true
		}
		if hasLt {
			r.maximum, r.exclusiveMax, found = &lt, true, true
		}
		if hasConst {
			r.enum, found = append(r.enum, scalarLit{cst, numTag(cst)}), true
		}
		return found
	}
	switch {
	case fr.GetInt32() != nil:
		n := fr.GetInt32()
		return setRange(n.HasGte(), i(int64(n.GetGte())), n.HasLte(), i(int64(n.GetLte())), n.HasGt(), i(int64(n.GetGt())), n.HasLt(), i(int64(n.GetLt())), n.HasConst(), i(int64(n.GetConst())))
	case fr.GetInt64() != nil:
		n := fr.GetInt64()
		return setRange(n.HasGte(), i(n.GetGte()), n.HasLte(), i(n.GetLte()), n.HasGt(), i(n.GetGt()), n.HasLt(), i(n.GetLt()), n.HasConst(), i(n.GetConst()))
	case fr.GetUint32() != nil:
		n := fr.GetUint32()
		return setRange(n.HasGte(), u(uint64(n.GetGte())), n.HasLte(), u(uint64(n.GetLte())), n.HasGt(), u(uint64(n.GetGt())), n.HasLt(), u(uint64(n.GetLt())), n.HasConst(), u(uint64(n.GetConst())))
	case fr.GetUint64() != nil:
		n := fr.GetUint64()
		return setRange(n.HasGte(), u(n.GetGte()), n.HasLte(), u(n.GetLte()), n.HasGt(), u(n.GetGt()), n.HasLt(), u(n.GetLt()), n.HasConst(), u(n.GetConst()))
	case fr.GetFloat() != nil:
		n := fr.GetFloat()
		return setRange(n.HasGte(), fl(float64(n.GetGte())), n.HasLte(), fl(float64(n.GetLte())), n.HasGt(), fl(float64(n.GetGt())), n.HasLt(), fl(float64(n.GetLt())), n.HasConst(), fl(float64(n.GetConst())))
	case fr.GetDouble() != nil:
		n := fr.GetDouble()
		return setRange(n.HasGte(), fl(n.GetGte()), n.HasLte(), fl(n.GetLte()), n.HasGt(), fl(n.GetGt()), n.HasLt(), fl(n.GetLt()), n.HasConst(), fl(n.GetConst()))
	}
	return false
}

// applyEnumField lists an enum field's allowed values (respecting buf.validate
// in/not_in) as OpenAPI `enum` numbers plus `x-enum-varnames` names.
func applyEnumField(r *fieldRule, field *descriptorpb.FieldDescriptorProto, fr *validate.FieldRules, enums map[string][]enumEntry) bool {
	all, ok := enums[trimDot(field.GetTypeName())]
	if !ok {
		return false
	}
	allowed := all
	if er := fr.GetEnum(); er != nil {
		if in := er.GetIn(); len(in) > 0 {
			allowed = filterEnum(all, in, true)
		} else if notIn := er.GetNotIn(); len(notIn) > 0 {
			allowed = filterEnum(all, notIn, false)
		}
	}
	r.enumField = append(r.enumField, allowed...)
	return len(allowed) > 0
}

// filterEnum keeps (keep=true) or drops (keep=false) the given numbers.
func filterEnum(all []enumEntry, nums []int32, keep bool) []enumEntry {
	set := map[int32]bool{}
	for _, n := range nums {
		set[n] = true
	}
	var out []enumEntry
	for _, e := range all {
		if set[e.num] == keep {
			out = append(out, e)
		}
	}
	return out
}

// ---- enum descriptor index ----

func buildEnumIndex(files []*descriptorpb.FileDescriptorProto) map[string][]enumEntry {
	out := map[string][]enumEntry{}
	for _, f := range files {
		prefix := f.GetPackage()
		if prefix != "" {
			prefix += "."
		}
		for _, e := range f.GetEnumType() {
			addEnum(out, prefix, e)
		}
		for _, m := range f.GetMessageType() {
			walkEnums(out, prefix, m)
		}
	}
	return out
}

func walkEnums(out map[string][]enumEntry, prefix string, m *descriptorpb.DescriptorProto) {
	mp := prefix + m.GetName() + "."
	for _, e := range m.GetEnumType() {
		addEnum(out, mp, e)
	}
	for _, nested := range m.GetNestedType() {
		walkEnums(out, mp, nested)
	}
}

func addEnum(out map[string][]enumEntry, prefix string, e *descriptorpb.EnumDescriptorProto) {
	var vals []enumEntry
	for _, v := range e.GetValue() {
		vals = append(vals, enumEntry{num: v.GetNumber(), name: v.GetName()})
	}
	out[prefix+e.GetName()] = vals
}

func trimDot(s string) string {
	if len(s) > 0 && s[0] == '.' {
		return s[1:]
	}
	return s
}

func i(v int64) string  { return strconv.FormatInt(v, 10) }
func u(v uint64) string { return strconv.FormatUint(v, 10) }
func fl(v float64) string {
	return strconv.FormatFloat(v, 'g', -1, 64)
}
