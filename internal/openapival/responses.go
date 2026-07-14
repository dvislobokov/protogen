package openapival

import (
	yaml "go.yaml.in/yaml/v3"
	"google.golang.org/protobuf/types/descriptorpb"
)

// validationProblemSchema is the component schema for the problem+json body
// produced by rest.ProblemErrorHandler (ASP.NET Core ValidationProblemDetails).
const validationProblemSchema = `
type: object
description: RFC 9457 problem+json returned when request validation fails.
properties:
    type:
        type: string
    title:
        type: string
    status:
        type: integer
        format: int32
    errors:
        type: object
        description: Map of field name (JSON) to its validation messages.
        additionalProperties:
            type: array
            items:
                type: string
`

const validationProblemResponse = `
description: One or more validation errors occurred.
content:
    application/problem+json:
        schema:
            $ref: '#/components/schemas/ValidationProblemDetails'
`

const problemSchemaName = "ValidationProblemDetails"

var httpMethods = []string{"get", "put", "post", "delete", "patch", "options", "head", "trace"}

// addValidationResponses adds a 400 response (referencing ValidationProblemDetails)
// to every operation whose request message carries validation constraints, and
// registers the component schema. Returns true if anything was added.
func addValidationResponses(root *yaml.Node, files []*descriptorpb.FileDescriptorProto) bool {
	validatedOp := analyzeOperations(files)
	if len(validatedOp) == 0 {
		return false
	}

	paths := mapGet(root, "paths")
	if paths == nil {
		return false
	}

	added := false
	for i := 1; i < len(paths.Content); i += 2 {
		pathItem := paths.Content[i]
		for _, method := range httpMethods {
			op := mapGet(pathItem, method)
			if op == nil {
				continue
			}
			idNode := mapGet(op, "operationId")
			if idNode == nil || !validatedOp[idNode.Value] {
				continue
			}
			responses := mapGet(op, "responses")
			if responses == nil {
				responses = &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
				setNode(op, "responses", responses)
			}
			if mapGet(responses, "400") == nil {
				setNode(responses, "400", literalNode(validationProblemResponse))
				added = true
			}
		}
	}

	if added {
		if schemas := dig(root, "components", "schemas"); schemas != nil && mapGet(schemas, problemSchemaName) == nil {
			setNode(schemas, problemSchemaName, literalNode(validationProblemSchema))
		}
	}
	return added
}

// analyzeOperations maps operationId -> whether its request message is validated
// (directly, or transitively through message-typed fields).
func analyzeOperations(files []*descriptorpb.FileDescriptorProto) map[string]bool {
	direct := map[string]bool{}    // full message name -> has a constraint
	edges := map[string][]string{} // full message name -> message-typed field targets
	var walk func(prefix string, m *descriptorpb.DescriptorProto)
	walk = func(prefix string, m *descriptorpb.DescriptorProto) {
		full := prefix + m.GetName()
		for _, f := range m.GetField() {
			if fieldRules(f) != nil {
				direct[full] = true
			}
			if f.GetType() == descriptorpb.FieldDescriptorProto_TYPE_MESSAGE {
				edges[full] = append(edges[full], trimDot(f.GetTypeName()))
			}
		}
		for _, n := range m.GetNestedType() {
			walk(full+".", n)
		}
	}
	for _, file := range files {
		prefix := file.GetPackage()
		if prefix != "" {
			prefix += "."
		}
		for _, m := range file.GetMessageType() {
			walk(prefix, m)
		}
	}

	// Transitive closure: a message is validated if it or any message it
	// references (through fields) is directly validated.
	validated := map[string]bool{}
	var visit func(name string, seen map[string]bool) bool
	visit = func(name string, seen map[string]bool) bool {
		if v, ok := validated[name]; ok {
			return v
		}
		if seen[name] {
			return false // cycle guard; resolved by another path if validated
		}
		seen[name] = true
		if direct[name] {
			validated[name] = true
			return true
		}
		for _, child := range edges[name] {
			if visit(child, seen) {
				validated[name] = true
				return true
			}
		}
		return false
	}

	out := map[string]bool{}
	for _, file := range files {
		for _, svc := range file.GetService() {
			for _, method := range svc.GetMethod() {
				op := svc.GetName() + "_" + method.GetName()
				if visit(trimDot(method.GetInputType()), map[string]bool{}) {
					out[op] = true
				}
			}
		}
	}
	return out
}

// literalNode parses a YAML literal into a value node.
func literalNode(s string) *yaml.Node {
	var doc yaml.Node
	if err := yaml.Unmarshal([]byte(s), &doc); err != nil || len(doc.Content) == 0 {
		return &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	}
	return doc.Content[0]
}

// setNode sets or replaces key on a mapping node with an arbitrary value node.
func setNode(m *yaml.Node, key string, val *yaml.Node) {
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			m.Content[i+1] = val
			return
		}
	}
	m.Content = append(m.Content,
		&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key},
		val,
	)
}
