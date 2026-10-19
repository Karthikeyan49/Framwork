package tools

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"
)

// keyPattern is the deterministic idempotency/reference key: SHA-256 hex.
var keyPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

// outbound is a fully validated request: the method and path come from the
// trusted definition, the path parameters and JSON body from typed inputs.
type outbound struct {
	method            string
	path              string // escaped, absolute
	body              []byte // nil means no body
	idempotencyHeader string
}

// buildOutbound validates the request against the definition before any I/O.
// LOOKUP sends only the key; inputs are not transmitted.
func buildOutbound(def Definition, r Request) (outbound, bool) {
	c := def.Connector
	switch r.Kind {
	case KindLookup:
		if c.Lookup == nil || c.Lookup.Method != http.MethodGet || !keyPattern.MatchString(r.Key) {
			return outbound{}, false
		}
		path, ok := expandPath(c.Lookup.Path, func(name string) (string, bool) {
			return r.Key, name == "key"
		})
		return outbound{method: http.MethodGet, path: path}, ok
	case KindSend:
	default:
		return outbound{}, false
	}
	// Any supplied key must be well formed; a declared header requires one.
	if (c.IdempotencyHeader != "" || r.Key != "") && !keyPattern.MatchString(r.Key) {
		return outbound{}, false
	}
	values, ok := typedInputs(def.Input, r.Inputs)
	if !ok {
		return outbound{}, false
	}
	path, ok := expandPath(c.Path, func(name string) (string, bool) {
		f, declared := def.InputField(name)
		v, present := values[name]
		if !declared || f.In != "path" || !present {
			return "", false
		}
		return formatSegment(v)
	})
	if !ok {
		return outbound{}, false
	}
	out := outbound{method: c.Method, path: path, idempotencyHeader: c.IdempotencyHeader}
	switch c.Method {
	case http.MethodGet:
	case http.MethodPost, http.MethodPut:
		body := map[string]any{}
		for _, f := range def.Input {
			if v, present := values[f.Name]; present && f.In == "body" {
				body[f.Name] = v
			}
		}
		raw, err := json.Marshal(body)
		if err != nil {
			return outbound{}, false
		}
		out.body = raw
	default:
		return outbound{}, false
	}
	return out, true
}

// typedInputs accepts only declared names whose values are already typed Go
// scalars, then canonicalizes them through DecodeInput: string (valid UTF-8,
// ≤max_length), integer (int/int32/int64/json.Number within ±2^53-1), decimal
// (canonical decimal string) and bool. A nil value is an absent input.
func typedInputs(fields []Field, inputs map[string]any) (map[string]any, bool) {
	if len(inputs) > len(fields) {
		return nil, false
	}
	out := make(map[string]any, len(inputs))
	for name, value := range inputs {
		f, declared := fieldNamed(fields, name)
		if !declared {
			return nil, false
		}
		if value == nil {
			continue
		}
		typed, ok := typedValue(f, value)
		if !ok {
			return nil, false
		}
		out[name] = typed
	}
	for _, f := range fields {
		if _, present := out[f.Name]; f.Required && !present {
			return nil, false
		}
	}
	return out, true
}

func fieldNamed(fields []Field, name string) (Field, bool) {
	for _, f := range fields {
		if f.Name == name {
			return f, true
		}
	}
	return Field{}, false
}

func typedValue(f Field, value any) (any, bool) {
	switch v := value.(type) {
	case string:
		if !utf8.ValidString(v) {
			return nil, false
		}
	case bool, int, int32, int64, json.Number:
	default:
		// Floating point, byte slices and composite values are never inputs.
		return nil, false
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, false
	}
	typed, err := DecodeInput(f, raw)
	return typed, err == nil
}

// formatSegment renders a canonical typed value for one path segment.
func formatSegment(v any) (string, bool) {
	switch t := v.(type) {
	case string:
		return t, true
	case int64:
		return strconv.FormatInt(t, 10), true
	case bool:
		return strconv.FormatBool(t), true
	}
	return "", false
}

// expandPath substitutes whole-segment {name} parameters, escaping each value
// as one segment. Values that are empty, "." or "..", or that contain a slash
// or backslash, are refused rather than escaped.
func expandPath(template string, value func(name string) (string, bool)) (string, bool) {
	if !strings.HasPrefix(template, "/") || len(template) > 512 {
		return "", false
	}
	var b strings.Builder
	for _, segment := range strings.Split(template[1:], "/") {
		b.WriteByte('/')
		if len(segment) > 2 && segment[0] == '{' && segment[len(segment)-1] == '}' {
			v, ok := value(segment[1 : len(segment)-1])
			if !ok || v == "" || v == "." || v == ".." || strings.ContainsAny(v, `/\`) {
				return "", false
			}
			b.WriteString(url.PathEscape(v))
			continue
		}
		if segment == "." || segment == ".." || !segmentPattern.MatchString(segment) {
			return "", false
		}
		b.WriteString(segment)
	}
	return b.String(), true
}

// parseFlatObject strictly decodes one JSON object whose members are all
// scalars or null. Duplicate members, nesting and trailing data are refused.
func parseFlatObject(raw []byte) (map[string]json.RawMessage, bool) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if token, err := decoder.Token(); err != nil || token != json.Delim('{') {
		return nil, false
	}
	out := map[string]json.RawMessage{}
	for decoder.More() {
		token, err := decoder.Token()
		name, isString := token.(string)
		if err != nil || !isString {
			return nil, false
		}
		if _, duplicate := out[name]; duplicate {
			return nil, false
		}
		var value json.RawMessage
		if decoder.Decode(&value) != nil {
			return nil, false
		}
		value = bytes.TrimSpace(value)
		if len(value) == 0 || value[0] == '{' || value[0] == '[' {
			return nil, false
		}
		out[name] = value
	}
	if token, err := decoder.Token(); err != nil || token != json.Delim('}') {
		return nil, false
	}
	if _, err := decoder.Token(); err != io.EOF {
		return nil, false
	}
	return out, true
}
