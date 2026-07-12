// Command protogenall generates Go message code, gRPC stubs and an OpenAPI v3
// document from .proto files — without protoc or any external plugin binaries.
package main

import (
	"context"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"

	"github.com/dvislobokov/protogen/internal/compile"
	"github.com/dvislobokov/protogen/internal/config"
	"github.com/dvislobokov/protogen/internal/gen"
	"github.com/dvislobokov/protogen/internal/managed"
	"github.com/dvislobokov/protogen/internal/openapival"
)

// version is overridable at build time: go build -ldflags "-X main.version=v1.2.3".
// When empty/dev, it falls back to the module version embedded by `go install`.
var version = "dev"

type stringList []string

func (s *stringList) String() string { return strings.Join(*s, ",") }
func (s *stringList) Set(v string) error {
	*s = append(*s, v)
	return nil
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

func main() {
	var importPaths stringList
	flag.Var(&importPaths, "proto_path", "import path root (repeatable), like protoc -I")

	out := flag.String("out", "gen", "output directory")
	goPkgPrefix := flag.String("go-package-prefix", "", "module prefix used to synthesize go_package when a proto omits it")
	protoPkg := flag.String("proto-package", "", "override an empty proto `package` on target files")
	oapiTitle := flag.String("openapi-title", "API", "OpenAPI document title")
	oapiVersion := flag.String("openapi-version", "0.0.1", "OpenAPI document version")
	oapiEnumFormat := flag.String("openapi-enum-format", "string", "enum representation in OpenAPI: string (value names, matches grpc-gateway JSON) or number")
	descriptorSetOut := flag.String("descriptor-set-out", "", "if set, also write a FileDescriptorSet (buf image) to this path")
	generators := flag.String("generators", "", "comma-separated subset of messages,grpc,gateway,openapiv3 (default: all)")
	configPath := flag.String("config", "", "path to a protogenall.yaml (auto-detected in the CWD if present)")
	listBuiltins := flag.Bool("list-builtins", false, "print the proto import paths bundled in this binary and exit")
	showVersion := flag.Bool("version", false, "print version and exit")

	flag.Parse()

	if *showVersion {
		fmt.Println("protogenall", resolveVersion())
		return
	}

	if *listBuiltins {
		fmt.Println("bundled imports (no --proto_path needed):")
		for _, p := range compile.BuiltinImports() {
			fmt.Println("  " + p)
		}
		return
	}

	set := map[string]bool{}
	flag.Visit(func(f *flag.Flag) { set[f.Name] = true })

	s := settings{
		importPaths:      importPaths,
		inputs:           flag.Args(),
		out:              *out,
		goPkgPrefix:      *goPkgPrefix,
		protoPkg:         *protoPkg,
		oapiTitle:        *oapiTitle,
		oapiVersion:      *oapiVersion,
		oapiEnumFormat:   *oapiEnumFormat,
		descriptorSetOut: *descriptorSetOut,
		generators:       splitComma(*generators),
	}

	if cfgPath := resolveConfigPath(*configPath); cfgPath != "" {
		cfg, err := config.Load(cfgPath)
		if err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			os.Exit(1)
		}
		s.mergeConfig(cfg, set)
		fmt.Println("using config:", cfgPath)
	}

	if len(s.importPaths) == 0 {
		s.importPaths = []string{"."}
	}
	if len(s.inputs) == 0 {
		fmt.Fprintln(os.Stderr, "usage: protogenall [flags] <proto files | directories | globs>")
		fmt.Fprintln(os.Stderr, "  e.g. protogenall --proto_path=proto --out=gen proto   # whole tree")
		fmt.Fprintln(os.Stderr, "  or provide inputs via protogenall.yaml (--config)")
		flag.PrintDefaults()
		os.Exit(2)
	}

	if err := run(s); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
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
// command line (explicit flags always win).
func (s *settings) mergeConfig(c *config.Config, set map[string]bool) {
	if !set["proto_path"] && len(c.ProtoPaths) > 0 {
		s.importPaths = c.ProtoPaths
	}
	if len(s.inputs) == 0 {
		s.inputs = c.Inputs
	}
	if !set["out"] && c.Out != "" {
		s.out = c.Out
	}
	if !set["go-package-prefix"] && c.GoPackagePrefix != "" {
		s.goPkgPrefix = c.GoPackagePrefix
	}
	if !set["proto-package"] && c.ProtoPackage != "" {
		s.protoPkg = c.ProtoPackage
	}
	if !set["openapi-title"] && c.OpenAPI.Title != "" {
		s.oapiTitle = c.OpenAPI.Title
	}
	if !set["openapi-version"] && c.OpenAPI.Version != "" {
		s.oapiVersion = c.OpenAPI.Version
	}
	if !set["openapi-enum-format"] && c.OpenAPI.EnumFormat != "" {
		s.oapiEnumFormat = c.OpenAPI.EnumFormat
	}
	if !set["descriptor-set-out"] && c.DescriptorSetOut != "" {
		s.descriptorSetOut = c.DescriptorSetOut
	}
	if !set["generators"] && len(c.Generators) > 0 {
		s.generators = c.Generators
	}
}

func splitComma(s string) []string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
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

func run(s settings) error {
	ctx := context.Background()

	files, err := expandInputs(s.inputs, s.importPaths)
	if err != nil {
		return err
	}
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
