package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dvislobokov/protogen/internal/config"
)

func TestInitScaffoldAndGenerate(t *testing.T) {
	dir := t.TempDir()
	// A go.mod makes init derive the project name and go_package_prefix from
	// the module path instead of the (random) temp directory name.
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/petshop\n\ngo 1.25\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := runInit([]string{dir}); err != nil {
		t.Fatal(err)
	}

	protoPath := filepath.Join(dir, "proto", "petshop", "v1", "petshop.proto")
	if _, err := os.Stat(protoPath); err != nil {
		t.Fatalf("scaffolded proto missing: %v", err)
	}

	// The builtin annotation protos must be vendored for IDE import resolution.
	for _, f := range []string{
		filepath.Join("third_party", "google", "api", "annotations.proto"),
		filepath.Join("third_party", "buf", "validate", "validate.proto"),
		filepath.Join("third_party", "openapiv3", "annotations.proto"),
		filepath.Join("third_party", "protogen", "authz", "authz.proto"),
		filepath.Join("third_party", "README.md"),
	} {
		if _, err := os.Stat(filepath.Join(dir, f)); err != nil {
			t.Errorf("expected vendored file %s: %v", f, err)
		}
	}

	cfg, err := config.Load(filepath.Join(dir, "protogenall.yaml"))
	if err != nil {
		t.Fatalf("scaffolded config does not parse: %v", err)
	}
	if cfg.GoPackagePrefix != "example.com/petshop/gen" {
		t.Fatalf("go_package_prefix = %q, want module-derived prefix", cfg.GoPackagePrefix)
	}

	// Re-running without --force must not overwrite existing files.
	if err := runInit([]string{dir}); err != nil {
		t.Fatalf("re-init must be a no-op, got: %v", err)
	}

	// The scaffolded project must generate end to end: messages, grpc,
	// gateway, openapi + enrichment. third_party is deliberately passed as an
	// input too: the vendored builtin protos must be skipped, not generated.
	out := filepath.Join(dir, "gen")
	err = run(settings{
		importPaths:    []string{filepath.Join(dir, "proto"), filepath.Join(dir, "third_party")},
		inputs:         []string{filepath.Join(dir, "proto"), filepath.Join(dir, "third_party")},
		out:            out,
		goPkgPrefix:    cfg.GoPackagePrefix,
		oapiTitle:      "API",
		oapiVersion:    "0.0.1",
		oapiEnumFormat: "string",
	})
	if err != nil {
		t.Fatalf("generation over the scaffolded project failed: %v", err)
	}
	for _, f := range []string{
		"petshop/v1/petshop.pb.go",
		"petshop/v1/petshop_grpc.pb.go",
		"petshop/v1/petshop.pb.gw.go",
		"openapi.yaml",
	} {
		if _, err := os.Stat(filepath.Join(out, f)); err != nil {
			t.Errorf("expected output %s: %v", f, err)
		}
	}
	// The vendored builtin protos must not produce generated code.
	if _, err := os.Stat(filepath.Join(out, "google")); !os.IsNotExist(err) {
		t.Errorf("vendored builtin protos were code-generated into %s", filepath.Join(out, "google"))
	}
	if _, err := os.Stat(filepath.Join(out, "buf")); !os.IsNotExist(err) {
		t.Errorf("vendored builtin protos were code-generated into %s", filepath.Join(out, "buf"))
	}

	// The proto's (openapi.v3.document) info must win in the generated doc.
	doc, err := os.ReadFile(filepath.Join(out, "openapi.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if want := "title: Petshop API"; !strings.Contains(string(doc), want) {
		t.Errorf("openapi.yaml does not contain %q", want)
	}
}

func TestSanitizeName(t *testing.T) {
	tests := []struct{ in, want string }{
		{"petshop", "petshop"},
		{"Infra-Info", "infrainfo"},
		{"123abc", "abc"},
		{"_x9", "x9"},
		{"---", "app"},
		{"", "app"},
	}
	for _, tt := range tests {
		if got := sanitizeName(tt.in); got != tt.want {
			t.Errorf("sanitizeName(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}
