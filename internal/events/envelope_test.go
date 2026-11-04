package events

import (
	"context"
	"encoding/json"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
	"strings"
	"testing"
)

func TestPointerRejectsAmbiguity(t *testing.T) {
	p := Pointer{SchemaVersion: "1.0.0", TenantID: "11111111-1111-1111-1111-111111111111", EventID: "22222222-2222-2222-2222-222222222222", BodyHash: strings.Repeat("a", 64), ConsumerID: ConsumerID, Generation: 1, LeaseToken: "33333333-3333-3333-3333-333333333333"}
	raw, _ := p.Bytes()
	got, err := DecodePointer(raw)
	if err != nil || got != p {
		t.Fatal("round trip")
	}
	for _, bad := range []string{string(raw) + " ", string(raw) + "{}", strings.Replace(string(raw), `"generation":1`, `"generation":1,"generation":2`, 1), strings.Replace(string(raw), "1.0.0", "2.0.0", 1), strings.Replace(string(raw), ConsumerID, "workflow.unknown", 1), strings.Replace(string(raw), `"generation":1`, `"generation":0`, 1), strings.Replace(string(raw), `"body_hash"`, `"sql"`, 1)} {
		if _, err := DecodePointer([]byte(bad)); err == nil {
			t.Fatal("ambiguous pointer accepted")
		}
	}
}
func TestCorrelationValidation(t *testing.T) {
	for _, bad := range []string{"secret", "00-" + strings.Repeat("0", 32) + "-0011223344556677-01", "01-00112233445566778899aabbccddeeff-0011223344556677-01"} {
		if validTrace(bad) {
			t.Fatal("invalid trace accepted")
		}
	}
	ctx := (propagation.TraceContext{}).Extract(context.Background(), propagation.MapCarrier{"traceparent": "00-00112233445566778899aabbccddeeff-0011223344556677-01"})
	if !trace.SpanContextFromContext(ctx).IsValid() {
		t.Fatal("parent not parsed")
	}
	if !validTrace("") || !validTrace("00-00112233445566778899aabbccddeeff-0011223344556677-01") {
		t.Fatal("valid trace rejected")
	}
}
func FuzzPointer(f *testing.F) {
	f.Add([]byte(`{"schema_version":"1.0.0"}`))
	f.Fuzz(func(t *testing.T, raw []byte) {
		p, err := DecodePointer(raw)
		if err == nil {
			b, e := json.Marshal(p)
			if e != nil || string(b) != string(raw) || !p.Valid() {
				t.Fatal("noncanonical pointer accepted")
			}
		}
	})
}
