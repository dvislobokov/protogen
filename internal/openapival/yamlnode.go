package openapival

import (
	"strconv"
	"strings"

	yaml "go.yaml.in/yaml/v3"
)

// mapGet returns the value node for key in a mapping node, or nil.
func mapGet(m *yaml.Node, key string) *yaml.Node {
	if m == nil || m.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			return m.Content[i+1]
		}
	}
	return nil
}

// dig follows a chain of mapping keys.
func dig(n *yaml.Node, keys ...string) *yaml.Node {
	for _, k := range keys {
		n = mapGet(n, k)
		if n == nil {
			return nil
		}
	}
	return n
}

// setScalar sets or replaces key on a mapping node with a scalar value.
func setScalar(m *yaml.Node, key, value, tag string) {
	if m == nil || m.Kind != yaml.MappingNode {
		return
	}
	val := &yaml.Node{Kind: yaml.ScalarNode, Tag: tag, Value: value}
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			m.Content[i+1] = val
			return
		}
	}
	m.Content = append(m.Content,
		&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key},
		val,
	)
}

// setRequired merges names into the schema's "required" sequence (deduped).
func setRequired(schema *yaml.Node, names []string) {
	seq := mapGet(schema, "required")
	if seq == nil {
		seq = &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
		schema.Content = append(schema.Content,
			&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "required"},
			seq,
		)
	}
	existing := map[string]bool{}
	for _, item := range seq.Content {
		existing[item.Value] = true
	}
	for _, n := range names {
		if existing[n] {
			continue
		}
		seq.Content = append(seq.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: n})
		existing[n] = true
	}
}

// removeKey deletes key (and its value) from a mapping node, if present.
func removeKey(m *yaml.Node, key string) {
	if m == nil || m.Kind != yaml.MappingNode {
		return
	}
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			m.Content = append(m.Content[:i], m.Content[i+2:]...)
			return
		}
	}
}

// setSequence sets or replaces key on a mapping node with a scalar sequence.
func setSequence(m *yaml.Node, key string, items []scalarLit) {
	if m == nil || m.Kind != yaml.MappingNode {
		return
	}
	seq := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
	for _, it := range items {
		seq.Content = append(seq.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: it.tag, Value: it.value})
	}
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			m.Content[i+1] = seq
			return
		}
	}
	m.Content = append(m.Content,
		&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key},
		seq,
	)
}

func uintStr(v uint64) string { return strconv.FormatUint(v, 10) }

// numTag picks the right YAML scalar tag for a numeric literal.
func numTag(v string) string {
	if strings.ContainsAny(v, ".eE") {
		return "!!float"
	}
	return "!!int"
}
