package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode"
)

// runInit implements `protogenall init [dir]`: it scaffolds a ready-to-generate
// project — a starter proto (with google.api.http, buf.validate, openapi.v3 and
// protogen.authz annotations) and a protogenall.yaml — so that a plain
// `protogenall` (or `protogenall <dir>`) is all that's left to run.
func runInit(args []string) error {
	fs := flag.NewFlagSet("init", flag.ExitOnError)
	force := fs.Bool("force", false, "overwrite files that already exist")
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "usage: protogenall init [--force] [dir]")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 1 {
		return fmt.Errorf("init takes at most one directory argument, got %v", fs.Args())
	}
	dir := "."
	if fs.NArg() == 1 {
		dir = fs.Arg(0)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}

	module := detectModule(dir)
	name := projectName(dir, module)
	title := strings.ToUpper(name[:1]) + name[1:]

	protoRel := filepath.Join("proto", name, "v1", name+".proto")
	files := []struct{ rel, content string }{
		{protoRel, renderTemplate(protoTemplate, name, title, module)},
		{"protogenall.yaml", renderTemplate(configTemplate, name, title, module)},
	}

	fmt.Println("initializing protogen project in", dir)
	for _, f := range files {
		path := filepath.Join(dir, f.rel)
		if _, err := os.Stat(path); err == nil && !*force {
			fmt.Println("  exists, skipped:", f.rel, "(use --force to overwrite)")
			continue
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(path, []byte(f.content), 0o644); err != nil {
			return err
		}
		fmt.Println("  wrote:", f.rel)
	}

	fmt.Println("\nnext steps:")
	if dir != "." {
		fmt.Printf("  protogenall %s        # or: cd %s && protogenall\n", dir, dir)
	} else {
		fmt.Println("  protogenall            # generates into gen/")
	}
	fmt.Println("  edit", filepath.ToSlash(protoRel), "and re-run")
	return nil
}

// detectModule returns the Go module path from dir/go.mod, or "" if absent.
func detectModule(dir string) string {
	raw, err := os.ReadFile(filepath.Join(dir, "go.mod"))
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(raw), "\n") {
		if rest, ok := strings.CutPrefix(strings.TrimSpace(line), "module "); ok {
			return strings.TrimSpace(rest)
		}
	}
	return ""
}

// projectName derives the proto package stem: the module's base name when a
// go.mod exists, else the directory's base name, sanitized to a valid
// lowercase proto identifier.
func projectName(dir, module string) string {
	base := ""
	if module != "" {
		base = filepath.Base(filepath.FromSlash(module))
	} else if abs, err := filepath.Abs(dir); err == nil {
		base = filepath.Base(abs)
	}
	return sanitizeName(base)
}

// sanitizeName lowercases and strips everything that isn't [a-z0-9_], then
// trims leading digits/underscores; empty results fall back to "app".
func sanitizeName(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if r == '_' || unicode.IsDigit(r) || (r >= 'a' && r <= 'z') {
			b.WriteRune(r)
		}
	}
	out := strings.TrimLeft(b.String(), "0123456789_")
	if out == "" {
		return "app"
	}
	return out
}

// renderTemplate substitutes the {{name}}/{{Name}}/{{module}} placeholders.
// The module placeholder falls back to an example.com path so the config is
// valid even outside a Go module.
func renderTemplate(tmpl, name, title, module string) string {
	if module == "" {
		module = "example.com/" + name
	}
	return strings.NewReplacer(
		"{{name}}", name,
		"{{Name}}", title,
		"{{module}}", module,
	).Replace(tmpl)
}

const protoTemplate = `syntax = "proto3";

// Scaffolded by ` + "`protogenall init`" + ` — edit freely and re-run protogenall.
//
// go_package is intentionally omitted: managed mode synthesizes it from
// go_package_prefix in protogenall.yaml.
package {{name}}.v1;

// All of these imports are bundled in the protogenall binary (--list-builtins).
import "google/api/annotations.proto";
import "google/api/field_behavior.proto";
import "buf/validate/validate.proto";
import "openapiv3/annotations.proto";
import "protogen/authz/authz.proto";

// Document-level OpenAPI metadata; takes precedence over the config's
// openapi.title/version.
option (openapi.v3.document) = {
  info: {
    title: "{{Name}} API"
    version: "0.1.0"
  }
};

service {{Name}}Service {
  // Methods without their own (protogen.authz.requires) stay public.
  option (protogen.authz.default_requires) = { public: true };

  rpc Get{{Name}}(Get{{Name}}Request) returns ({{Name}}) {
    option (google.api.http) = {
      get: "/v1/{{name}}/{id}"
    };
    option (openapi.v3.operation) = {
      summary: "Fetch a {{name}} by id"
    };
  }

  rpc Create{{Name}}(Create{{Name}}Request) returns ({{Name}}) {
    option (google.api.http) = {
      post: "/v1/{{name}}"
      body: "*"
    };
    option (openapi.v3.operation) = {
      summary: "Create a {{name}}"
    };
    // Enforced by the interceptors in github.com/dvislobokov/protogen/authz.
    option (protogen.authz.requires) = {
      roles: { any_of: ["admin", "editor"] }
    };
  }
}

message Get{{Name}}Request {
  // protovalidate constraints are checked at runtime and reflected into
  // the generated openapi.yaml.
  string id = 1 [(buf.validate.field).string = {min_len: 1, max_len: 64}];
}

message Create{{Name}}Request {
  string name = 1 [(buf.validate.field).string = {min_len: 1, max_len: 100}];
}

message {{Name}} {
  string id = 1 [(google.api.field_behavior) = OUTPUT_ONLY];
  string name = 2;
}
`

const configTemplate = `# protogenall configuration (scaffolded by protogenall init).
# Explicit CLI flags override these values.

proto_paths: [proto]
inputs: [proto]
out: gen
go_package_prefix: {{module}}/gen

# OpenAPI document metadata. The (openapi.v3.document) option in the proto
# takes precedence when present.
# openapi:
#   title: {{Name}} API
#   version: 0.1.0
#   enum_format: string   # string (default) | number

# Run a subset of the generators if you don't need everything:
# generators: [messages, grpc, gateway, openapiv3]

# Also write a buf-style descriptor image:
# descriptor_set_out: build/image.binpb
`
