// Package managed implements "managed mode": filling in file-level metadata
// (go_package, java_package, ...) from CLI-provided defaults when a .proto file
// does not set it itself. This mirrors buf's managed mode and lets you generate
// from proto files that carry no language-specific options.
package managed

import (
	"path"
	"strings"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/descriptorpb"
)

// Options are the defaults applied to files that lack the corresponding option.
type Options struct {
	// GoPackagePrefix is a Go module path prefix. For a proto at foo/bar.proto
	// with no go_package, the injected value becomes
	//   <prefix>/foo;<lastSegmentOfProtoPackage>
	GoPackagePrefix string
	// OverrideProtoPackage, if set, replaces an empty `package` declaration.
	OverrideProtoPackage string
}

// Apply mutates the given descriptors in place, but only for files in the
// generate set (identified by name). Dependencies are left untouched unless
// they also lack go_package (protogen needs it for every referenced file).
func Apply(files []*descriptorpb.FileDescriptorProto, generate []string, opts Options) {
	genSet := make(map[string]bool, len(generate))
	for _, g := range generate {
		genSet[g] = true
	}

	for _, fd := range files {
		if fd.Options == nil {
			fd.Options = &descriptorpb.FileOptions{}
		}

		// Fill an empty proto package for generate targets when asked.
		if genSet[fd.GetName()] && fd.GetPackage() == "" && opts.OverrideProtoPackage != "" {
			fd.Package = proto.String(opts.OverrideProtoPackage)
		}

		// go_package is required by protogen for *every* file, so backfill it
		// wherever it is missing and we have a prefix to work with.
		if fd.Options.GoPackage == nil && opts.GoPackagePrefix != "" {
			fd.Options.GoPackage = proto.String(deriveGoPackage(opts.GoPackagePrefix, fd))
		}
	}
}

// deriveGoPackage builds "<prefix>/<dir>;<name>" from the proto file path and
// package, matching the convention most Go proto users follow.
func deriveGoPackage(prefix string, fd *descriptorpb.FileDescriptorProto) string {
	dir := path.Dir(fd.GetName())
	importPath := prefix
	if dir != "." && dir != "" {
		importPath = prefix + "/" + dir
	}

	name := shortName(fd.GetPackage())
	if name == "" {
		// Fall back to the file's base name without extension.
		base := path.Base(fd.GetName())
		name = strings.TrimSuffix(base, path.Ext(base))
	}
	return importPath + ";" + name
}

func shortName(protoPkg string) string {
	if protoPkg == "" {
		return ""
	}
	parts := strings.Split(protoPkg, ".")
	last := parts[len(parts)-1]
	// Avoid Go package names like "v1"; prefer the segment before a version.
	if isVersion(last) && len(parts) >= 2 {
		last = parts[len(parts)-2]
	}
	return last
}

func isVersion(s string) bool {
	if len(s) < 2 || s[0] != 'v' {
		return false
	}
	for _, r := range s[1:] {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}
