// Package config loads protogenall.yaml — a declarative alternative to the CLI
// flags, so a repo can commit its generation settings.
package config

import (
	"bytes"
	"fmt"
	"os"

	yaml "go.yaml.in/yaml/v3"
)

// Config mirrors the CLI options. Explicitly-set flags override these values.
type Config struct {
	// ProtoPaths are import roots (like protoc -I).
	ProtoPaths []string `yaml:"proto_paths"`
	// Inputs are files, directories, or globs to generate for.
	Inputs []string `yaml:"inputs"`
	// Out is the output directory.
	Out string `yaml:"out"`
	// GoPackagePrefix seeds managed-mode go_package.
	GoPackagePrefix string `yaml:"go_package_prefix"`
	// ProtoPackage overrides an empty proto `package`.
	ProtoPackage string `yaml:"proto_package"`
	// OpenAPI carries document metadata.
	OpenAPI struct {
		Title   string `yaml:"title"`
		Version string `yaml:"version"`
	} `yaml:"openapi"`
	// DescriptorSetOut, if set, writes a FileDescriptorSet there.
	DescriptorSetOut string `yaml:"descriptor_set_out"`
	// Generators selects which passes run (messages, grpc, gateway, openapiv3).
	// Empty means "all".
	Generators []string `yaml:"generators"`
}

// Load reads and parses a config file, rejecting unknown keys.
func Load(path string) (*Config, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var c Config
	dec := yaml.NewDecoder(bytes.NewReader(raw))
	dec.KnownFields(true)
	if err := dec.Decode(&c); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	return &c, nil
}
