// Package compile turns a set of .proto files into descriptors using the pure-Go
// bufbuild/protocompile compiler. No protoc binary is required.
package compile

import (
	"context"
	"embed"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/bufbuild/protocompile"
	"github.com/bufbuild/protocompile/protoutil"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/descriptorpb"
)

// builtinFS holds the commonly-imported annotation protos (google.api.http,
// buf.validate) so users never have to vendor or pass them on --proto_path.
//
//go:embed builtin
var builtinFS embed.FS

// builtinResolver serves the embedded builtin protos by import path, e.g.
// "google/api/annotations.proto".
func builtinResolver() *protocompile.SourceResolver {
	return &protocompile.SourceResolver{
		Accessor: func(path string) (io.ReadCloser, error) {
			return builtinFS.Open("builtin/" + path)
		},
	}
}

// BuiltinImports lists the import paths bundled in the binary (for docs/help).
func BuiltinImports() []string {
	var out []string
	fs.WalkDir(builtinFS, "builtin", func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			out = append(out, p[len("builtin/"):])
		}
		return nil
	})
	return out
}

// Options controls how sources are located and parsed.
type Options struct {
	// ImportPaths are the roots used to resolve `import` statements, in order.
	// The directory holding the input files is usually included here.
	ImportPaths []string
	// Filenames are the proto files to generate for, expressed relative to one
	// of the ImportPaths (this is how protoc names files too).
	Filenames []string
}

// Result holds the compiled descriptors.
type Result struct {
	// FileToGenerate mirrors protoc's semantics: only these are code-generated.
	FileToGenerate []string
	// AllFiles is every file (targets + transitive imports), topologically
	// sorted so that dependencies precede dependents — required by protogen.
	AllFiles []*descriptorpb.FileDescriptorProto
}

// Compile parses and links the given proto files entirely in-process.
func Compile(ctx context.Context, opts Options) (*Result, error) {
	comp := protocompile.Compiler{
		// Resolution order: the user's --proto_path roots first (so they can
		// override anything), then the protos bundled in this binary
		// (google.api, buf.validate), then WithStandardImports for the
		// google/protobuf well-known types. Nothing needs to be on disk.
		Resolver: protocompile.WithStandardImports(protocompile.CompositeResolver{
			&protocompile.SourceResolver{ImportPaths: opts.ImportPaths},
			builtinResolver(),
		}),
		// Keep source info so comments flow through to generated code / OpenAPI.
		SourceInfoMode: protocompile.SourceInfoStandard,
	}

	linked, err := comp.Compile(ctx, opts.Filenames...)
	if err != nil {
		return nil, fmt.Errorf("compile: %w", err)
	}

	// Collect every file in dependency order, de-duplicated.
	seen := map[string]bool{}
	var all []*descriptorpb.FileDescriptorProto
	var visit func(fd protoreflect.FileDescriptor)
	visit = func(fd protoreflect.FileDescriptor) {
		if seen[fd.Path()] {
			return
		}
		seen[fd.Path()] = true
		imports := fd.Imports()
		for i := 0; i < imports.Len(); i++ {
			visit(imports.Get(i).FileDescriptor)
		}
		fdp := protoutil.ProtoFromFileDescriptor(fd)
		all = append(all, normalizeExtensions(fdp))
	}
	for _, f := range linked {
		visit(f)
	}

	return &Result{
		FileToGenerate: opts.Filenames,
		AllFiles:       all,
	}, nil
}

// WriteDescriptorSet marshals all compiled files as a FileDescriptorSet (the
// same "image" format buf and `protoc --descriptor_set_out` produce) to path.
func WriteDescriptorSet(files []*descriptorpb.FileDescriptorProto, path string) error {
	set := &descriptorpb.FileDescriptorSet{File: files}
	b, err := proto.Marshal(set)
	if err != nil {
		return err
	}
	if dir := filepath.Dir(path); dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	return os.WriteFile(path, b, 0o644)
}

// normalizeExtensions re-encodes a file's custom options so that extensions
// (e.g. google.api.http) are decoded into the *concrete* Go extension types
// registered in this binary, rather than protocompile's dynamicpb messages.
//
// This replicates what happens at the protoc↔plugin boundary: the plugin
// unmarshals the request through the global type registry, so any extension it
// links against (here, google.golang.org/genproto/.../annotations) becomes a
// concrete value that downstream generators can read with proto.GetExtension.
func normalizeExtensions(fdp *descriptorpb.FileDescriptorProto) *descriptorpb.FileDescriptorProto {
	b, err := proto.Marshal(fdp)
	if err != nil {
		return fdp // leave as-is; extremely unlikely for a valid descriptor
	}
	fresh := &descriptorpb.FileDescriptorProto{}
	// Default resolver is protoregistry.GlobalTypes — concrete types win.
	if err := proto.Unmarshal(b, fresh); err != nil {
		return fdp
	}
	return fresh
}
