package metadata

import (
	"encoding/json"
	"os"
	"testing"
)

func TestPatchThreeWayConflictAndSensitiveKinds(t *testing.T) {
	raw, e := os.ReadFile("../../contracts/examples/contacts-draft-v1.json")
	if e != nil {
		t.Fatal(e)
	}
	var base Document
	_ = json.Unmarshal(raw, &base)
	var page Definition
	for _, d := range base.Definitions {
		if d.Kind == "page" {
			page = d
		}
	}
	changed := page
	changed.Label = "Updated page"
	p := ApplicationPatch{Version: "1.0.0", BaseRelease: "release.1", BaseIRHash: "ir.1", Operations: []PatchOperation{{Op: "update", Target: page.ID, ExpectedHash: definitionHash(page), ChangeClass: "PRESENTATION", Definition: &changed}}}
	out, conflicts, e := p.Rebase("release.1", "ir.1", base, base)
	if e != nil || len(conflicts) != 0 || len(out.Definitions) != len(base.Definitions) {
		t.Fatal("independent page merge")
	}
	current := base
	current.Definitions = append([]Definition(nil), base.Definitions...)
	for i, d := range current.Definitions {
		if d.ID == page.ID {
			current.Definitions[i].Label = "Other edit"
		}
	}
	if _, c, e := p.Rebase("release.1", "ir.1", base, current); e != nil || len(c) != 1 {
		t.Fatal("overlapping changes auto merged")
	}
	p.Operations[0].ChangeClass = "PERMISSION"
	if _, c, e := p.Rebase("release.1", "ir.1", base, base); e != nil || len(c) != 1 {
		t.Fatal("sensitive change auto merged")
	}
	p.Operations[0].ChangeClass = "PRESENTATION"
	for _, d := range base.Definitions {
		if d.Kind == "field" {
			replacement := page
			replacement.ID = d.ID
			p.Operations[0] = PatchOperation{Op: "update", Target: d.ID, ExpectedHash: definitionHash(d), ChangeClass: "PRESENTATION", Definition: &replacement}
			break
		}
	}
	if _, _, e := p.Rebase("release.1", "ir.1", base, base); e == nil {
		t.Fatal("sensitive kind disguised as page")
	}
	if _, _, e := p.Rebase("other", "ir.1", base, base); e == nil {
		t.Fatal("stale base release")
	}
}
