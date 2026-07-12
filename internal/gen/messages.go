package gen

import (
	gengo "google.golang.org/protobuf/cmd/protoc-gen-go/internal_gengo"
	"google.golang.org/protobuf/compiler/protogen"
)

// Messages generates the *.pb.go message code by calling the exact same
// generator protoc-gen-go uses. The `internal_gengo` path element is not
// literally "internal", so Go's internal-import rule does not block it.
type Messages struct{}

func (Messages) Name() string { return "messages" }

func (Messages) Generate(gen *protogen.Plugin) error {
	for _, f := range gen.Files {
		if !f.Generate {
			continue
		}
		gengo.GenerateFile(gen, f)
	}
	return nil
}
