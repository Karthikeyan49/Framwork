package tools

import (
	"encoding/json"
	"strings"
	"testing"
)

// M10R-05: lookup.settle_ms is validated, omitted from the canonical encoding
// when zero (existing definition hashes and plan pins are unchanged) and part
// of the definition hash when declared.
func TestLookupSettleIntervalValidatedAndHashed(t *testing.T) {
	base := lookupDefinition("provider.example.test", 443, "sandbox.example.test", 443)
	if _, err := NewCatalog([]Definition{base}); err != nil {
		t.Fatal(err)
	}
	if raw := mustCanonical(t, base); strings.Contains(raw, "settle_ms") {
		t.Fatal("undeclared settle interval changed the canonical encoding", raw)
	}
	declared := base
	lookup := *base.Connector.Lookup
	lookup.SettleMS = 1500
	declared.Connector.Lookup = &lookup
	c, err := NewCatalog([]Definition{declared})
	if err != nil {
		t.Fatal(err)
	}
	if d, ok := c.Lookup(base.Ref, base.Version, declared.Hash()); !ok || d.Connector.Lookup.SettleMS != 1500 || declared.Hash() == base.Hash() {
		t.Fatal("declared settle interval is not part of the pinned identity")
	}
	for _, bad := range []int{-1, MaxSettleMS + 1} {
		invalid := base
		l := *base.Connector.Lookup
		l.SettleMS = bad
		invalid.Connector.Lookup = &l
		if _, err := NewCatalog([]Definition{invalid}); err == nil {
			t.Fatal("settle interval accepted:", bad)
		}
	}
}

func mustCanonical(t *testing.T, d Definition) string {
	t.Helper()
	c, err := NewCatalog([]Definition{d})
	if err != nil {
		t.Fatal(err)
	}
	got, _ := c.Lookup(d.Ref, d.Version, "")
	raw, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}
