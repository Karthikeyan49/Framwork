package promotion

import (
	"github.com/Karthikeyan49/Framwork/internal/compiler"
	"github.com/Karthikeyan49/Framwork/internal/metadata"
	"os"
	"testing"
)

func TestM7CandidateStoredRulesCompatibility(t *testing.T) {
	prior, _, _ := candidateFixture(t)
	b := prior.Bundle()
	raw, e := os.ReadFile("../../contracts/examples/contacts-rules-v2.json")
	if e != nil {
		t.Fatal(e)
	}
	v, e := metadata.NewValidator()
	if e != nil {
		t.Fatal(e)
	}
	s, e := v.Parse(raw)
	if e != nil {
		t.Fatal(e)
	}
	p, ds := compiler.Compile(s, compiler.PlatformCatalog())
	if len(ds) > 0 {
		t.Fatal(ds)
	}
	c, e := Build(s, p, b)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = Restore(c.JSON(), c.Hash(), compiler.SupportedIRVersions()); e != nil {
		t.Fatal(e)
	}
	if _, e = Restore(c.JSON(), c.Hash(), []string{compiler.IRVersion}); e == nil {
		t.Fatal("unsupported release admitted")
	}
}
