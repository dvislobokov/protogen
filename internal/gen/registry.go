// Package gen wires compiled descriptors into protogen and runs a set of
// in-process code generators (messages, gRPC, gateway, OpenAPI).
package gen

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/dvislobokov/protogen/internal/compile"

	"google.golang.org/protobuf/compiler/protogen"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/pluginpb"
)

// Generator is one code-generation pass over the protogen plugin state.
// All generators share the same *protogen.Plugin and append to its response.
type Generator interface {
	Name() string
	Generate(gen *protogen.Plugin) error
}

// BuildRequest assembles a protoc-style CodeGeneratorRequest from compiled
// descriptors. param is the protoc plugin parameter string (e.g.
// "paths=source_relative").
func BuildRequest(res *compile.Result, param string) *pluginpb.CodeGeneratorRequest {
	return &pluginpb.CodeGeneratorRequest{
		FileToGenerate:  res.FileToGenerate,
		Parameter:       proto.String(param),
		ProtoFile:       res.AllFiles,
		CompilerVersion: &pluginpb.Version{Major: proto.Int32(5), Minor: proto.Int32(0), Patch: proto.Int32(0)},
	}
}

// Run creates the protogen plugin, executes every generator against it, and
// writes the resulting files under outDir.
func Run(req *pluginpb.CodeGeneratorRequest, gens []Generator, outDir string) error {
	plugin, err := protogen.Options{}.New(req)
	if err != nil {
		return fmt.Errorf("protogen init: %w", err)
	}
	// Advertise proto3 optional support so files using it don't get rejected.
	plugin.SupportedFeatures = uint64(pluginpb.CodeGeneratorResponse_FEATURE_PROTO3_OPTIONAL)

	for _, g := range gens {
		if err := g.Generate(plugin); err != nil {
			return fmt.Errorf("generator %s: %w", g.Name(), err)
		}
	}

	resp := plugin.Response()
	if resp.Error != nil {
		return fmt.Errorf("generation failed: %s", resp.GetError())
	}

	for _, f := range resp.GetFile() {
		outPath := filepath.Join(outDir, filepath.FromSlash(f.GetName()))
		if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(outPath, []byte(f.GetContent()), 0o644); err != nil {
			return err
		}
		fmt.Printf("  wrote %s\n", outPath)
	}
	return nil
}
