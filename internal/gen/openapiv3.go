package gen

import (
	oapigen "github.com/google/gnostic/cmd/protoc-gen-openapi/generator"
	"google.golang.org/protobuf/compiler/protogen"
	"google.golang.org/protobuf/proto"
)

// OpenAPIv3 emits an openapi.yaml (OpenAPI 3) document from the services,
// honoring google.api.http annotations. Provided by google/gnostic in-process.
type OpenAPIv3 struct {
	Title       string
	Version     string
	Description string
}

func (OpenAPIv3) Name() string { return "openapiv3" }

func (o OpenAPIv3) Generate(gen *protogen.Plugin) error {
	// All pointer fields must be set: gnostic dereferences them unconditionally.
	// Defaults mirror protoc-gen-openapi's own flag defaults.
	conf := oapigen.Configuration{
		Version:         proto.String(nonEmpty(o.Version, "0.0.1")),
		Title:           proto.String(nonEmpty(o.Title, "API")),
		Description:     proto.String(o.Description),
		Naming:          proto.String("json"),
		FQSchemaNaming:  proto.Bool(false),
		EnumType:        proto.String("integer"),
		CircularDepth:   intPtr(2),
		DefaultResponse: proto.Bool(true),
		OutputMode:      proto.String("merged"),
	}

	var inputs []*protogen.File
	for _, f := range gen.Files {
		if f.Generate {
			inputs = append(inputs, f)
		}
	}

	out := gen.NewGeneratedFile("openapi.yaml", "")
	return oapigen.NewOpenAPIv3Generator(gen, conf, inputs).Run(out)
}

func intPtr(i int) *int { return &i }

func nonEmpty(s, def string) string {
	if s == "" {
		return def
	}
	return s
}
