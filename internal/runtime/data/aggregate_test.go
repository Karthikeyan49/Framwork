package data

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestAggregateLegacyEncodingAndExpandedExactResult(t *testing.T) {
	for _, tc := range []struct {
		value Aggregate
		want  string
	}{{Aggregate{Op: "count"}, `{"op":"count"}`}, {Aggregate{Op: "sum", Field: "field.contact.balance"}, `{"op":"sum","field":"field.contact.balance"}`}} {
		raw, err := json.Marshal(tc.value)
		if err != nil || string(raw) != tc.want {
			t.Fatal("legacy aggregate encoding changed")
		}
	}
	for input, want := range map[string]string{
		"0": "0", "-0.000000000000000000": "0", "1.230000000000000000": "1.23",
		"100000000000000000000000.000000000000000001": "100000000000000000000000.000000000000000001",
		"-0.000000000000000001":                       "-0.000000000000000001",
	} {
		got, err := canonicalAggregateSum(input)
		if err != nil || got != want {
			t.Fatal("exact aggregate scalar changed")
		}
	}
	for _, input := range []string{"", "NaN", "Infinity", "-Infinity", "1e5", "0.0000000000000000001", strings.Repeat("9", 129)} {
		if _, err := canonicalAggregateSum(input); err == nil {
			t.Fatal("invalid aggregate scalar admitted")
		}
	}
}
