// Command protogenall generates Go message code, gRPC stubs and an OpenAPI v3
// document from .proto files — without protoc or any external plugin binaries.
package main

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"

	"github.com/dvislobokov/scmd"

	"github.com/dvislobokov/protogen/internal/compile"
	"github.com/dvislobokov/protogen/internal/config"
	"github.com/dvislobokov/protogen/internal/gen"
	"github.com/dvislobokov/protogen/internal/managed"
	"github.com/dvislobokov/protogen/internal/openapival"
)

// version is overridable at build time: go build -ldflags "-X main.version=v1.2.3".
// When empty/dev, it falls back to the module version embedded by `go install`.
var version = "dev"

// generateOptions is the CLI contract of the root command. scmd.Value wraps
// options whose "explicitly set on the command line" state matters: explicit
// flags override protogenall.yaml, defaults do not (the old flag.Visit dance).
type generateOptions struct {
	ProtoPaths       []string           `flag:"proto_path" short:"I" help:"import path root (repeatable), like protoc -I"`
	Out              scmd.Value[string] `flag:"out" default:"gen" help:"output directory"`
	GoPkgPrefix      scmd.Value[string] `flag:"go-package-prefix" help:"module prefix used to synthesize go_package when a proto omits it"`
	ProtoPkg         scmd.Value[string] `flag:"proto-package" help:"override an empty proto package on target files"`
	OapiTitle        scmd.Value[string] `flag:"openapi-title" default:"API" help:"OpenAPI document title"`
	OapiVersion      scmd.Value[string] `flag:"openapi-version" default:"0.0.1" help:"OpenAPI document version"`
	OapiEnumFormat   scmd.Value[string] `flag:"openapi-enum-format" default:"string" enum:"string,number" help:"enum representation in OpenAPI (string matches grpc-gateway JSON)"`
	DescriptorSetOut scmd.Value[string] `flag:"descriptor-set-out" help:"if set, also write a FileDescriptorSet (buf image) to this path"`
	Generators       []string           `flag:"generators" enum:"messages,grpc,gateway,openapiv3" help:"subset of generators to run (repeatable or comma-separated; default: all)"`
	Config           string             `flag:"config" help:"path to a protogenall.yaml (auto-detected in the CWD if present)"`
	ListBuiltins     bool               `flag:"list-builtins" help:"print the proto import paths bundled in this binary and exit"`

	Inputs []string `arg:"..." name:"inputs" help:"proto files, directories, globs, or a project dir holding protogenall.yaml"`
}

type initOptions struct {
	Force bool   `flag:"force" help:"overwrite files that already exist"`
	Dir   string `arg:"dir" help:"target directory (default: current)"`
}

// settings is the resolved run configuration (flags merged over config file).
type settings struct {
	importPaths      []string
	inputs           []string
	out              string
	goPkgPrefix      string
	protoPkg         string
	oapiTitle        string
	oapiVersion      string
	oapiEnumFormat   string
	descriptorSetOut string
	generators       []string
}

func newApp() *scmd.App {
	return scmd.New("protogenall",
		"Generates Go messages, gRPC, gRPC-gateway and OpenAPI v3 from .proto files — no protoc, no plugins",
		scmd.WithLocale(scmd.LocaleEN),
		scmd.WithVersion(resolveVersion()),
		scmd.Root(scmd.Cmd("", "", runGenerate)),
		scmd.Cmd("init", "Scaffold a new protogen project (proto, protogenall.yaml, vendored third_party)", runInitCmd),
	)
}

func main() {
	os.Exit(newApp().Run(context.Background(), os.Args[1:]))
}

func runInitCmd(ctx context.Context, o initOptions) error {
	dir := o.Dir
	if dir == "" {
		dir = "."
	}
	return runInit(o.Force, dir)
}

func runGenerate(ctx context.Context, o generateOptions) error {
	if o.ListBuiltins {
		fmt.Println("bundled imports (no --proto_path needed):")
		for _, p := range compile.BuiltinImports() {
			fmt.Println("  " + p)
		}
		return nil
	}

	inputs := o.Inputs
	// `protogenall <dir>` where dir is a project root (holds protogenall.yaml,
	// e.g. created by `protogenall init <dir>`): generate that project as if
	// run from inside it.
	if o.Config == "" && len(inputs) == 1 {
		if st, err := os.Stat(inputs[0]); err == nil && st.IsDir() {
			if _, err := os.Stat(filepath.Join(inputs[0], "protogenall.yaml")); err == nil {
				if err := os.Chdir(inputs[0]); err != nil {
					return err
				}
				fmt.Println("project directory:", inputs[0])
				inputs = nil
			}
		}
	}

	s := settings{
		importPaths:      o.ProtoPaths,
		inputs:           inputs,
		out:              o.Out.Get(),
		goPkgPrefix:      o.GoPkgPrefix.Get(),
		protoPkg:         o.ProtoPkg.Get(),
		oapiTitle:        o.OapiTitle.Get(),
		oapiVersion:      o.OapiVersion.Get(),
		oapiEnumFormat:   o.OapiEnumFormat.Get(),
		descriptorSetOut: o.DescriptorSetOut.Get(),
		generators:       o.Generators,
	}

	if cfgPath := resolveConfigPath(o.Config); cfgPath != "" {
		cfg, err := config.Load(cfgPath)
		if err != nil {
			return err
		}
		s.mergeConfig(cfg, o)
		fmt.Println("using config:", cfgPath)
	}

	if len(s.importPaths) == 0 {
		s.importPaths = []string{"."}
	}
	if len(s.inputs) == 0 {
		return &scmd.UsageError{Problems: []string{
			"no inputs: pass proto files, directories or globs (e.g. `protogenall --proto_path=proto proto`),",
			"a project dir holding protogenall.yaml, or list inputs in protogenall.yaml (--config)",
		}}
	}

	return run(s)
}

// resolveVersion prefers the ldflags-injected version, then the module version
// recorded by `go install module/cmd/protogenall@vX.Y.Z`.
func resolveVersion() string {
	if version != "dev" {
		return version
	}
	if bi, ok := debug.ReadBuildInfo(); ok {
		if v := bi.Main.Version; v != "" && v != "(devel)" {
			return v
		}
	}
	return version
}

// resolveConfigPath returns the explicit config path, or auto-detects
// protogenall.yaml in the working directory.
func resolveConfigPath(explicit string) string {
	if explicit != "" {
		return explicit
	}
	if _, err := os.Stat("protogenall.yaml"); err == nil {
		return "protogenall.yaml"
	}
	return ""
}

// mergeConfig fills settings from cfg for any option not explicitly set on the
// command line (explicit flags always win; scmd.Value.IsSet tells them apart
// from defaults).
func (s *settings) mergeConfig(c *config.Config, o generateOptions) {
	if len(o.ProtoPaths) == 0 && len(c.ProtoPaths) > 0 {
		s.importPaths = c.ProtoPaths
	}
	if len(s.inputs) == 0 {
		s.inputs = c.Inputs
	}
	if !o.Out.IsSet() && c.Out != "" {
		s.out = c.Out
	}
	if !o.GoPkgPrefix.IsSet() && c.GoPackagePrefix != "" {
		s.goPkgPrefix = c.GoPackagePrefix
	}
	if !o.ProtoPkg.IsSet() && c.ProtoPackage != "" {
		s.protoPkg = c.ProtoPackage
	}
	if !o.OapiTitle.IsSet() && c.OpenAPI.Title != "" {
		s.oapiTitle = c.OpenAPI.Title
	}
	if !o.OapiVersion.IsSet() && c.OpenAPI.Version != "" {
		s.oapiVersion = c.OpenAPI.Version
	}
	if !o.OapiEnumFormat.IsSet() && c.OpenAPI.EnumFormat != "" {
		s.oapiEnumFormat = c.OpenAPI.EnumFormat
	}
	if !o.DescriptorSetOut.IsSet() && c.DescriptorSetOut != "" {
		s.descriptorSetOut = c.DescriptorSetOut
	}
	if len(o.Generators) == 0 && len(c.Generators) > 0 {
		s.generators = c.Generators
	}
}

// expandInputs turns positional arguments — which may be individual files,
// directories (walked recursively for *.proto), or globs — into a de-duplicated
// list of proto file names expressed relative to one of the import paths, which
// is what protocompile expects. An argument that is already an import-relative
// name (protoc style) is accepted as-is if it resolves under an import path.
func expandInputs(args, importPaths []string) ([]string, error) {
	seen := map[string]bool{}
	var out []string
	add := func(fsPath string) error {
		name, err := relToImport(fsPath, importPaths)
		if err != nil {
			return err
		}
		if !seen[name] {
			seen[name] = true
			out = append(out, name)
		}
		return nil
	}
	walk := func(dir string) error {
		return filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if !d.IsDir() && strings.HasSuffix(p, ".proto") {
				return add(p)
			}
			return nil
		})
	}

	for _, a := range args {
		if info, err := os.Stat(a); err == nil {
			if info.IsDir() {
				if err := walk(a); err != nil {
					return nil, err
				}
			} else if err := add(a); err != nil {
				return nil, err
			}
			continue
		}
		if matches, _ := filepath.Glob(a); len(matches) > 0 {
			for _, m := range matches {
				info, err := os.Stat(m)
				if err != nil {
					continue
				}
				if info.IsDir() {
					if err := walk(m); err != nil {
						return nil, err
					}
				} else if strings.HasSuffix(m, ".proto") {
					if err := add(m); err != nil {
						return nil, err
					}
				}
			}
			continue
		}
		// Fall back to protoc-style: a name already relative to an import path.
		if resolvedUnderImport(a, importPaths) {
			if !seen[a] {
				seen[a] = true
				out = append(out, a)
			}
			continue
		}
		return nil, fmt.Errorf("input %q: not a file, directory, glob, or import-relative proto name", a)
	}
	return out, nil
}

// relToImport expresses a filesystem path as a slash path relative to the first
// import path that contains it.
func relToImport(fsPath string, importPaths []string) (string, error) {
	abs, err := filepath.Abs(fsPath)
	if err != nil {
		return "", err
	}
	for _, root := range importPaths {
		rootAbs, err := filepath.Abs(root)
		if err != nil {
			continue
		}
		rel, err := filepath.Rel(rootAbs, abs)
		if err != nil {
			continue
		}
		if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			continue
		}
		return filepath.ToSlash(rel), nil
	}
	return "", fmt.Errorf("%q is not under any --proto_path root %v", fsPath, importPaths)
}

func resolvedUnderImport(name string, importPaths []string) bool {
	for _, root := range importPaths {
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(name))); err == nil {
			return true
		}
	}
	return false
}

// dropBuiltinTargets removes the annotation protos bundled in the binary from
// the generation targets. `protogenall init` vendors them under third_party/
// for IDE import resolution; if an input sweeps them in, they must still be
// compiled as imports only, never code-generated.
func dropBuiltinTargets(files []string) []string {
	builtin := map[string]bool{}
	for _, p := range compile.BuiltinImports() {
		builtin[p] = true
	}
	var out []string
	for _, f := range files {
		if !builtin[f] {
			out = append(out, f)
		}
	}
	if dropped := len(files) - len(out); dropped > 0 {
		fmt.Printf("skipping %d vendored builtin proto(s): compiled as imports, not generated\n", dropped)
	}
	return out
}

func run(s settings) error {
	ctx := context.Background()

	files, err := expandInputs(s.inputs, s.importPaths)
	if err != nil {
		return err
	}
	files = dropBuiltinTargets(files)
	if len(files) == 0 {
		return fmt.Errorf("no .proto files matched %v", s.inputs)
	}

	generators, hasOpenAPI, err := buildGenerators(s.generators, s.oapiTitle, s.oapiVersion)
	if err != nil {
		return err
	}

	fmt.Println("compiling", len(files), "proto file(s) with bufbuild/protocompile (no protoc)...")
	res, err := compile.Compile(ctx, compile.Options{
		ImportPaths: s.importPaths,
		Filenames:   files,
	})
	if err != nil {
		return err
	}

	// Managed mode: backfill go_package / package before protogen runs.
	managed.Apply(res.AllFiles, res.FileToGenerate, managed.Options{
		GoPackagePrefix:      s.goPkgPrefix,
		OverrideProtoPackage: s.protoPkg,
	})

	if s.descriptorSetOut != "" {
		if err := compile.WriteDescriptorSet(res.AllFiles, s.descriptorSetOut); err != nil {
			return err
		}
		fmt.Println("wrote descriptor set:", s.descriptorSetOut)
	}

	req := gen.BuildRequest(res, "paths=source_relative")

	fmt.Println("generating:")
	if err := gen.Run(req, generators, s.out); err != nil {
		return err
	}

	// Enrich the OpenAPI doc with validation constraints, enums, field_behavior.
	if hasOpenAPI {
		opts := openapival.Options{EnumsAsStrings: s.oapiEnumFormat != "number"}
		if err := openapival.EnrichFile(filepath.Join(s.out, "openapi.yaml"), res.AllFiles, opts); err != nil {
			return fmt.Errorf("openapi validation enrichment: %w", err)
		}
	}
	return nil
}

// buildGenerators resolves generator names into instances. Empty means all.
// It also reports whether OpenAPI v3 is among them (for the enrichment step).
func buildGenerators(names []string, oapiTitle, oapiVersion string) ([]gen.Generator, bool, error) {
	if len(names) == 0 {
		names = []string{"messages", "grpc", "gateway", "openapiv3"}
	}
	var gens []gen.Generator
	hasOpenAPI := false
	for _, n := range names {
		switch n {
		case "messages":
			gens = append(gens, gen.Messages{})
		case "grpc":
			gens = append(gens, gen.GRPC{})
		case "gateway":
			gens = append(gens, gen.Gateway{})
		case "openapiv3":
			gens = append(gens, gen.OpenAPIv3{Title: oapiTitle, Version: oapiVersion})
			hasOpenAPI = true
		default:
			return nil, false, fmt.Errorf("unknown generator %q (want messages, grpc, gateway, or openapiv3)", n)
		}
	}
	return gens, hasOpenAPI, nil
}
