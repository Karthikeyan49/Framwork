package metadata

import (
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
)

func sample(t *testing.T) []byte {
	t.Helper()
	raw, err := os.ReadFile("../../contracts/examples/vendor-draft-v1.json")
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
func validator(t *testing.T) *Validator {
	t.Helper()
	v, err := NewValidator()
	if err != nil {
		t.Fatal(err)
	}
	return v
}
func TestAllPrimitivesAndTypedRoundTrip(t *testing.T) {
	v := validator(t)
	source, err := v.Parse(sample(t))
	if err != nil {
		t.Fatal(err)
	}
	if len(source.document.Definitions) != 9 || source.document.Definitions[8].Tool.OutputSchema.Properties == nil {
		t.Fatal("missing typed primitive/empty tool payload")
	}
	round, err := v.Parse(source.JSON())
	if err != nil || round.Hash() != source.Hash() {
		t.Fatal("schema/typed round trip changed storage identity")
	}
	copy := source.JSON()
	copy[0] = '!'
	if source.JSON()[0] != '{' {
		t.Fatal("source bytes mutable through public port")
	}
	compact, _ := json.Marshal(source.document)
	again, err := v.Parse(compact)
	if err != nil || again.Hash() != source.Hash() {
		t.Fatal("whitespace changed source hash")
	}
}
func TestStructuralNegativeCorpus(t *testing.T) {
	v := validator(t)
	raw := sample(t)
	for name, mutate := range map[string]func(map[string]any){
		"unknown root":               func(d map[string]any) { d["tenant_id"] = "forged" },
		"schema upgrade unavailable": func(d map[string]any) { d["schema_version"] = "2.0.0" },
		"display label reference":    func(d map[string]any) { d["definitions"].([]any)[0].(map[string]any)["id"] = "Vendor" },
		"duplicate definitions":      func(d map[string]any) { defs := d["definitions"].([]any); d["definitions"] = append(defs, defs[0]) },
		"namespace mismatch":         func(d map[string]any) { d["definitions"].([]any)[0].(map[string]any)["namespace"] = "other" },
		"unknown nested key": func(d map[string]any) {
			d["definitions"].([]any)[1].(map[string]any)["field"].(map[string]any)["sql"] = "SELECT secret"
		},
		"arbitrary UI": func(d map[string]any) {
			d["definitions"].([]any)[5].(map[string]any)["page"].(map[string]any)["components"] = []any{map[string]any{"type": "script", "label": "bad"}}
		},
		"remote tool schema": func(d map[string]any) {
			d["definitions"].([]any)[8].(map[string]any)["tool"].(map[string]any)["input_schema"] = map[string]any{"$ref": "https://untrusted.example/schema"}
		},
		"inline secret": func(d map[string]any) {
			d["definitions"].([]any)[8].(map[string]any)["tool"].(map[string]any)["secret"] = "forbidden"
		},
		"enum requires values": func(d map[string]any) {
			d["definitions"].([]any)[1].(map[string]any)["field"].(map[string]any)["type"] = "enum"
		},
		"unbounded timer": func(d map[string]any) {
			d["definitions"].([]any)[7].(map[string]any)["workflow"].(map[string]any)["states"].([]any)[0].(map[string]any)["timer_ms"] = 1e12
		},
	} {
		t.Run(name, func(t *testing.T) {
			var d map[string]any
			if json.Unmarshal(raw, &d) != nil {
				t.Fatal("fixture")
			}
			mutate(d)
			bad, _ := json.Marshal(d)
			if _, err := v.Parse(bad); !errors.Is(err, ErrInvalid) {
				t.Fatal("unsafe structure accepted")
			}
		})
	}
	for name, bad := range map[string][]byte{"duplicate key": []byte(`{"schema_version":"1.0.0","schema_version":"2.0.0"}`), "trailing document": append(raw, []byte(`{}`)...), "invalid utf8": {0xff}, "too large": []byte(strings.Repeat(" ", MaxBytes+1)), "deep nesting": []byte(strings.Repeat("[", 34) + "0" + strings.Repeat("]", 34)), "large exponent": []byte(`1e999999`)} {
		t.Run(name, func(t *testing.T) {
			if _, err := v.Parse(bad); !errors.Is(err, ErrInvalid) {
				t.Fatal("invalid JSON accepted")
			}
		})
	}
}
func TestExactDecimalNormalizationAndLimits(t *testing.T) {
	for input, want := range map[string]string{"1.0": "1", "1e3": "1000", "-0.000": "0", "0.123456789012345678": "0.123456789012345678", "1e-18": "0.000000000000000001"} {
		value, err := DecodeJSON([]byte(input))
		if err != nil {
			t.Fatal(err)
		}
		encoded, _ := json.Marshal(value)
		if string(encoded) != want {
			t.Fatalf("normalization %s => %s", input, encoded)
		}
	}
	if _, err := DecodeJSON([]byte(`0.1234567890123456789`)); !errors.Is(err, ErrInvalid) {
		t.Fatal("precision beyond supported limit")
	}
}
func TestUnresolvedReferencesRemainAuthoringOnly(t *testing.T) {
	raw := strings.Replace(string(sample(t)), "field.vendor.name", "field.unknown.name", 1)
	if _, err := validator(t).Parse([]byte(raw)); err != nil {
		t.Fatal("M3 performed M4 symbol resolution")
	}
}
func FuzzJSONInputIsBounded(f *testing.F) {
	f.Add([]byte(`{"x":1,"x":2}`))
	f.Add([]byte(`{"x":[0.5]}`))
	f.Add([]byte(`1e999999`))
	f.Fuzz(func(t *testing.T, raw []byte) { _, _ = DecodeJSON(raw) })
}

func TestUnsupportedNULStringsAndKeys(t *testing.T) {
	for _, raw := range []string{`{"label":"\u0000"}`, `{"\u0000":1}`} {
		if _, err := DecodeJSON([]byte(raw)); !errors.Is(err, ErrInvalid) {
			t.Fatal("PostgreSQL-incompatible NUL accepted")
		}
	}
}

func TestYAMLConvergesAndRejectsUnsafeFeatures(t *testing.T) {
	v := validator(t)
	yamlSource := []byte(`schema_version: 1.0.0
app_id: app.minimal
namespace: example
base_version: null
definitions:
  - id: entity.vendor
    schema_version: 1.0.0
    namespace: example
    label: Vendor
    kind: entity
    entity:
      fields: []
`)
	source, err := v.ParseYAML(yamlSource)
	if err != nil {
		t.Fatal(err)
	}
	round, err := v.Parse(source.JSON())
	if err != nil || round.Hash() != source.Hash() {
		t.Fatal("YAML diverged from JSON source")
	}
	for _, bad := range []string{"x: 1\nx: 2\n", "x: &a 1\ny: *a\n", "x: !!python/object secret\n", "x: 1\n---\nx: 2\n", "x: .nan\n", "x: 0x100\n", "x: 2026-10-12\n", "x: {<<: {y: 1}}\n", "x: \"\\0\"\n"} {
		if _, err := DecodeYAML([]byte(bad)); !errors.Is(err, ErrInvalid) {
			t.Fatalf("unsafe YAML accepted: %q", bad)
		}
	}
	if _, err := DecodeYAML([]byte("x: " + strings.Repeat("[", 34) + "0" + strings.Repeat("]", 34))); !errors.Is(err, ErrInvalid) {
		t.Fatal("deep YAML accepted")
	}
}

func FuzzYAMLInputIsBounded(f *testing.F) {
	f.Add([]byte("x: [1, true, null]\n"))
	f.Add([]byte("x: &a [1]\ny: *a\n"))
	f.Add([]byte("x: 1\n---\nx: 2\n"))
	f.Fuzz(func(t *testing.T, raw []byte) { _, _ = DecodeYAML(raw) })
}
