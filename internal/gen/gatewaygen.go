package gen

import (
	"fmt"
	"os"

	"github.com/dvislobokov/protogen/internal/gateway/httprule"

	"google.golang.org/genproto/googleapis/api/annotations"
	"google.golang.org/protobuf/compiler/protogen"
	"google.golang.org/protobuf/proto"
)

// Gateway generates *.pb.gw.go reverse-proxy code (grpc-gateway) in-process.
// It models protoc-gen-grpc-gateway's output for methods with a google.api.http
// binding: unary and server-streaming (path params, whole-body ("*") or field
// bodies, query params). Client-streaming and bidi cannot be exposed over REST
// and are skipped. additional_bindings are out of scope.
type Gateway struct{}

func (Gateway) Name() string { return "gateway" }

// binding is the resolved HTTP mapping for one method.
type binding struct {
	method       *protogen.Method
	httpMethod   string // http.MethodGet, ...
	path         string // "/v1/greeter/{name}"
	body         string // "", "*", or a field name
	pathParams   []*protogen.Field
	serverStream bool // response is a server stream (forwarded as a chunked stream)
	tmpl         httprule.Template
}

func (Gateway) Generate(gen *protogen.Plugin) error {
	for _, f := range gen.Files {
		if !f.Generate || len(f.Services) == 0 {
			continue
		}
		bindings, err := collectBindings(f)
		if err != nil {
			return err
		}
		if len(bindings) == 0 {
			continue
		}
		genGatewayFile(gen, f, bindings)
	}
	return nil
}

func collectBindings(f *protogen.File) (map[*protogen.Service][]binding, error) {
	out := map[*protogen.Service][]binding{}
	for _, svc := range f.Services {
		for _, m := range svc.Methods {
			rule := httpRule(m)
			if rule == nil {
				continue // no REST mapping: gateway skips it
			}
			// Client-streaming and bidi have no HTTP/JSON representation.
			if m.Desc.IsStreamingClient() {
				fmt.Fprintf(os.Stderr, "gateway: skipping %s.%s (client/bidi streaming cannot be exposed over REST)\n", svc.GoName, m.GoName)
				continue
			}
			b, err := resolveBinding(m, rule)
			if err != nil {
				return nil, err
			}
			b.serverStream = m.Desc.IsStreamingServer()
			out[svc] = append(out[svc], b)
		}
	}
	return out, nil
}

func httpRule(m *protogen.Method) *annotations.HttpRule {
	opts := m.Desc.Options()
	if opts == nil {
		return nil
	}
	ext := proto.GetExtension(opts, annotations.E_Http)
	rule, _ := ext.(*annotations.HttpRule)
	if rule == nil || rule.GetPattern() == nil {
		return nil
	}
	return rule
}

func resolveBinding(m *protogen.Method, rule *annotations.HttpRule) (binding, error) {
	b := binding{method: m, body: rule.GetBody()}
	switch p := rule.GetPattern().(type) {
	case *annotations.HttpRule_Get:
		b.httpMethod, b.path = "MethodGet", p.Get
	case *annotations.HttpRule_Post:
		b.httpMethod, b.path = "MethodPost", p.Post
	case *annotations.HttpRule_Put:
		b.httpMethod, b.path = "MethodPut", p.Put
	case *annotations.HttpRule_Patch:
		b.httpMethod, b.path = "MethodPatch", p.Patch
	case *annotations.HttpRule_Delete:
		b.httpMethod, b.path = "MethodDelete", p.Delete
	default:
		return b, fmt.Errorf("gateway: unsupported http pattern on %s", m.GoName)
	}

	compiler, err := httprule.Parse(b.path)
	if err != nil {
		return b, fmt.Errorf("gateway: parse path %q: %w", b.path, err)
	}
	b.tmpl = compiler.Compile()

	// Map bound path fields to their Go fields on the request message.
	for _, fieldPath := range b.tmpl.Fields {
		fld := findField(m.Input, fieldPath)
		if fld == nil {
			return b, fmt.Errorf("gateway: path param %q not found on %s", fieldPath, m.Input.GoIdent.GoName)
		}
		b.pathParams = append(b.pathParams, fld)
	}
	return b, nil
}

// findField resolves a top-level path-param field by proto name.
func findField(msg *protogen.Message, name string) *protogen.Field {
	for _, fld := range msg.Fields {
		if string(fld.Desc.Name()) == name {
			return fld
		}
	}
	return nil
}
