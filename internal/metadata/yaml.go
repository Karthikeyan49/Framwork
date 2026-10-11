package metadata

import (
	"bytes"
	"encoding/json"
	"io"
	"regexp"
	"strconv"
	"unicode/utf8"

	"go.yaml.in/yaml/v3"
)

var jsonNumber = regexp.MustCompile(`^-?(0|[1-9][0-9]*)(\.[0-9]+)?([eE][+-]?[0-9]+)?$`)

// DecodeYAML accepts a bounded JSON-compatible YAML subset. No aliases, anchors,
// merge keys, custom tags, timestamp/binary constructors or multiple documents.
// It always converges through the same strict JSON and schema validation path.
func DecodeYAML(raw []byte) (any, error) {
	if len(raw) == 0 || len(raw) > MaxBytes+4096 || !utf8.Valid(raw) {
		return nil, ErrInvalid
	}
	decoder := yaml.NewDecoder(bytes.NewReader(raw))
	var root yaml.Node
	if decoder.Decode(&root) != nil || root.Kind != yaml.DocumentNode || len(root.Content) != 1 {
		return nil, ErrInvalid
	}
	var extra yaml.Node
	if err := decoder.Decode(&extra); err != io.EOF {
		return nil, invalid("", "multiple_documents", "Exactly one YAML document is required")
	}
	nodes := 0
	var convert func(*yaml.Node, int) (any, error)
	convert = func(node *yaml.Node, depth int) (any, error) {
		nodes++
		if depth > 32 || nodes > 16000 {
			return nil, invalid("", "complexity_limit", "YAML exceeds nesting or node limits")
		}
		if node.Anchor != "" || node.Kind == yaml.AliasNode {
			return nil, invalid("", "yaml_alias", "YAML aliases and anchors are forbidden")
		}
		switch node.Kind {
		case yaml.MappingNode:
			if node.Tag != "!!map" || len(node.Content)%2 != 0 {
				return nil, ErrInvalid
			}
			value := map[string]any{}
			for i := 0; i < len(node.Content); i += 2 {
				key := node.Content[i]
				if key.Kind != yaml.ScalarNode || key.Tag != "!!str" || key.Anchor != "" {
					return nil, ErrInvalid
				}
				if _, exists := value[key.Value]; exists {
					return nil, invalid("", "duplicate_key", "Duplicate YAML keys are forbidden")
				}
				entry, err := convert(node.Content[i+1], depth+1)
				if err != nil {
					return nil, err
				}
				value[key.Value] = entry
			}
			return value, nil
		case yaml.SequenceNode:
			if node.Tag != "!!seq" {
				return nil, ErrInvalid
			}
			value := []any{}
			for _, child := range node.Content {
				entry, err := convert(child, depth+1)
				if err != nil {
					return nil, err
				}
				value = append(value, entry)
			}
			return value, nil
		case yaml.ScalarNode:
			switch node.Tag {
			case "!!str":
				return node.Value, nil
			case "!!null":
				return nil, nil
			case "!!bool":
				value, err := strconv.ParseBool(node.Value)
				if err != nil {
					return nil, ErrInvalid
				}
				return value, nil
			case "!!int", "!!float":
				if !jsonNumber.MatchString(node.Value) {
					return nil, ErrInvalid
				}
				return json.Number(node.Value), nil
			}
			return nil, invalid("", "yaml_tag", "Only JSON-compatible YAML types are permitted")
		}
		return nil, ErrInvalid
	}
	value, err := convert(root.Content[0], 0)
	if err != nil {
		return nil, err
	}
	jsonBytes, err := json.Marshal(value)
	if err != nil {
		return nil, ErrInvalid
	}
	return DecodeJSON(jsonBytes)
}
func (v *Validator) ParseYAML(raw []byte) (Source, error) {
	if len(raw) > MaxBytes {
		return Source{}, ErrInvalid
	}
	value, err := DecodeYAML(raw)
	if err != nil {
		return Source{}, err
	}
	jsonBytes, err := json.Marshal(value)
	if err != nil {
		return Source{}, ErrInvalid
	}
	return v.Parse(jsonBytes)
}
