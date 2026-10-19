package metadata

import (
	"encoding/json"
	"math/rand"
	"os"
	"reflect"
	"testing"
)

func patchFixture(t *testing.T) (Document, Definition) {
	t.Helper()
	raw, e := os.ReadFile("../../contracts/examples/contacts-draft-v1.json")
	if e != nil {
		t.Fatal(e)
	}
	var base Document
	if e = json.Unmarshal(raw, &base); e != nil {
		t.Fatal(e)
	}
	for _, d := range base.Definitions {
		if d.Kind == "page" {
			return base, d
		}
	}
	t.Fatal("fixture has no page")
	return base, Definition{}
}

func editedPage(page Definition, label string) *Definition {
	d := page
	d.Label = label
	return &d
}

func withLabel(doc Document, id, label string) Document {
	out := doc
	out.Definitions = append([]Definition(nil), doc.Definitions...)
	for i := range out.Definitions {
		if out.Definitions[i].ID == id {
			out.Definitions[i].Label = label
		}
	}
	return out
}

func rebase(p ApplicationPatch, base, current Document) (Document, []PatchConflict, error) {
	return p.Rebase("release.1", "ir.1", base, current)
}

func patch(ops ...PatchOperation) ApplicationPatch {
	return ApplicationPatch{Version: "1.0.0", BaseRelease: "release.1", BaseIRHash: "ir.1", Operations: ops}
}

func categories(c []PatchConflict) []string {
	out := []string{}
	for _, x := range c {
		out = append(out, x.Category)
	}
	return out
}

func TestPatchThreeWayConflictAndSensitiveKinds(t *testing.T) {
	base, page := patchFixture(t)
	changed := editedPage(page, "Updated page")
	p := patch(PatchOperation{Op: "update", Target: page.ID, ExpectedHash: definitionHash(page), ChangeClass: "PRESENTATION", Definition: changed})
	out, conflicts, e := rebase(p, base, base)
	if e != nil || len(conflicts) != 0 || len(out.Definitions) != len(base.Definitions) {
		t.Fatal("independent page merge", e, conflicts)
	}
	current := withLabel(base, page.ID, "Other edit")
	if _, c, e := rebase(p, base, current); e != nil || !reflect.DeepEqual(categories(c), []string{ConflictCurrentDiverged}) {
		t.Fatal("overlapping changes auto merged", c, e)
	}
	p.Operations[0].ChangeClass = "PERMISSION"
	if _, c, e := rebase(p, base, base); e != nil || !reflect.DeepEqual(categories(c), []string{ConflictDeclaredClassMistake}) {
		t.Fatal("sensitive change auto merged", c, e)
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
	if out, c, e := rebase(p, base, base); e != nil || !reflect.DeepEqual(categories(c), []string{ConflictSensitiveChange}) || len(out.Definitions) != 0 {
		t.Fatal("sensitive kind disguised as page", c, e)
	}
	if _, _, e := p.Rebase("other", "ir.1", base, base); e == nil {
		t.Fatal("stale base release")
	}
}

// A caller-declared class never makes a non-page edit presentation, and a
// non-page edit is a conflict whatever class it declares.
func TestPatchClassIsDerivedNotDeclared(t *testing.T) {
	base, page := patchFixture(t)
	for _, d := range base.Definitions {
		if d.Kind == "page" {
			continue
		}
		edited := d
		edited.Label = "Hostile edit"
		for _, declared := range []string{"PRESENTATION", "SENSITIVE", "", "anything"} {
			p := patch(PatchOperation{Op: "update", Target: d.ID, ExpectedHash: definitionHash(d), ChangeClass: declared, Definition: &edited})
			out, c, e := rebase(p, base, base)
			if e != nil || len(out.Definitions) != 0 || !reflect.DeepEqual(categories(c), []string{ConflictSensitiveChange}) {
				t.Fatalf("%s as %q: %v %v", d.Kind, declared, c, e)
			}
		}
		p := patch(PatchOperation{Op: "delete", Target: d.ID, ExpectedHash: definitionHash(d), ChangeClass: "PRESENTATION"})
		if _, c, e := rebase(p, base, base); e != nil || !reflect.DeepEqual(categories(c), []string{ConflictSensitiveChange}) {
			t.Fatalf("%s deleted as presentation: %v %v", d.Kind, c, e)
		}
	}
	// A page edit declared with the wrong class, or no class, is refused.
	for _, declared := range []string{"SENSITIVE", "", "PERMISSION"} {
		p := patch(PatchOperation{Op: "update", Target: page.ID, ExpectedHash: definitionHash(page), ChangeClass: declared, Definition: editedPage(page, "x")})
		if _, c, e := rebase(p, base, base); e != nil || !reflect.DeepEqual(categories(c), []string{ConflictDeclaredClassMistake}) {
			t.Fatalf("declared %q: %v %v", declared, c, e)
		}
	}
}

func TestPatchAddUpdateDeleteMatrix(t *testing.T) {
	base, page := patchFixture(t)
	newPage := page
	newPage.ID, newPage.Label = "page.contact.extra", "Extra"
	otherPage := newPage
	otherPage.Label = "Different"
	op := func(kind, target, expected string, d *Definition) PatchOperation {
		return PatchOperation{Op: kind, Target: target, ExpectedHash: expected, ChangeClass: "PRESENTATION", Definition: d}
	}
	withExtra := base
	withExtra.Definitions = append(append([]Definition(nil), base.Definitions...), newPage)
	withOther := base
	withOther.Definitions = append(append([]Definition(nil), base.Definitions...), otherPage)
	withoutPage := base
	withoutPage.Definitions = nil
	for _, d := range base.Definitions {
		if d.ID != page.ID {
			withoutPage.Definitions = append(withoutPage.Definitions, d)
		}
	}
	h := definitionHash(page)
	cases := []struct {
		name       string
		p          ApplicationPatch
		base, cur  Document
		wantErr    bool
		categories []string
		pages      int // expected page definitions after a clean merge
		label      string
	}{
		{name: "add new page", p: patch(op("add", newPage.ID, "", &newPage)), base: base, cur: base, pages: 2},
		{name: "add convergent", p: patch(op("add", newPage.ID, "", &newPage)), base: base, cur: withExtra, pages: 2},
		{name: "add collides with a different current page", p: patch(op("add", newPage.ID, "", &newPage)), base: base, cur: withOther, categories: []string{ConflictAddCollision}},
		{name: "add of an existing base page", p: patch(op("add", page.ID, "", &Definition{ID: page.ID, Kind: "page"})), base: base, cur: base, categories: []string{ConflictAddCollision}},
		{name: "add with expected hash", p: patch(op("add", newPage.ID, h, &newPage)), base: base, cur: base, categories: []string{ConflictStaleExpectedHash}},
		{name: "update applies", p: patch(op("update", page.ID, h, editedPage(page, "New"))), base: base, cur: base, pages: 1, label: "New"},
		{name: "update convergent", p: patch(op("update", page.ID, h, editedPage(page, "New"))), base: base, cur: withLabel(base, page.ID, "New"), pages: 1, label: "New"},
		{name: "update stale expected hash", p: patch(op("update", page.ID, "00", editedPage(page, "New"))), base: base, cur: base, categories: []string{ConflictStaleExpectedHash}},
		{name: "update deleted in current", p: patch(op("update", page.ID, h, editedPage(page, "New"))), base: base, cur: withoutPage, categories: []string{ConflictCurrentDiverged}},
		{name: "update missing in base", p: patch(op("update", newPage.ID, h, &newPage)), base: base, cur: base, categories: []string{ConflictMissingTarget}},
		{name: "delete applies", p: patch(op("delete", page.ID, h, nil)), base: base, cur: base, pages: 0},
		{name: "delete convergent", p: patch(op("delete", page.ID, h, nil)), base: base, cur: withoutPage, pages: 0},
		{name: "delete of an edited page", p: patch(op("delete", page.ID, h, nil)), base: base, cur: withLabel(base, page.ID, "Edited"), categories: []string{ConflictCurrentDiverged}},
		{name: "delete stale", p: patch(op("delete", page.ID, "00", nil)), base: base, cur: base, categories: []string{ConflictStaleExpectedHash}},
		{name: "delete missing in base", p: patch(op("delete", newPage.ID, h, nil)), base: base, cur: base, categories: []string{ConflictMissingTarget}},
		{name: "unknown operation", p: patch(PatchOperation{Op: "merge", Target: page.ID, ChangeClass: "PRESENTATION"}), base: base, cur: base, wantErr: true},
		{name: "definition id differs from target", p: patch(op("update", page.ID, h, &newPage)), base: base, cur: base, wantErr: true},
		{name: "update without definition", p: patch(op("update", page.ID, h, nil)), base: base, cur: base, wantErr: true},
		{name: "delete with definition", p: patch(op("delete", page.ID, h, &page)), base: base, cur: base, wantErr: true},
		{name: "duplicate targets", p: patch(op("update", page.ID, h, editedPage(page, "A")), op("update", page.ID, h, editedPage(page, "B"))), base: base, cur: base, wantErr: true},
		{name: "empty patch", p: patch(), base: base, cur: base, wantErr: true},
		{name: "one conflict blocks the whole patch", p: patch(op("add", newPage.ID, "", &newPage), op("update", page.ID, "00", editedPage(page, "New"))), base: base, cur: base, categories: []string{ConflictStaleExpectedHash}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			out, conflicts, err := rebase(c.p, c.base, c.cur)
			switch {
			case c.wantErr:
				if err == nil {
					t.Fatalf("accepted: %v %v", conflicts, out.Definitions)
				}
			case len(c.categories) > 0:
				if err != nil || !reflect.DeepEqual(categories(conflicts), c.categories) || len(out.Definitions) != 0 {
					t.Fatalf("conflicts %v err %v", conflicts, err)
				}
			default:
				if err != nil || len(conflicts) != 0 {
					t.Fatalf("conflicts %v err %v", conflicts, err)
				}
				pages, label := 0, ""
				for _, d := range out.Definitions {
					if d.Kind == "page" {
						pages++
						if d.ID == page.ID {
							label = d.Label
						}
					}
				}
				if pages != c.pages || (c.label != "" && label != c.label) {
					t.Fatalf("pages %d label %q", pages, label)
				}
			}
		})
	}
}

// Untouched definitions survive byte for byte, output is sorted, and the order
// of operations never changes the result.
func TestPatchPreservesUntouchedAndIsOrderIndependent(t *testing.T) {
	base, page := patchFixture(t)
	n1, n2, n3 := page, page, page
	n1.ID, n1.Label = "page.contact.a", "A"
	n2.ID, n2.Label = "page.contact.b", "B"
	n3.ID, n3.Label = "page.contact.c", "C"
	ops := []PatchOperation{
		{Op: "add", Target: n1.ID, ChangeClass: "PRESENTATION", Definition: &n1},
		{Op: "add", Target: n2.ID, ChangeClass: "PRESENTATION", Definition: &n2},
		{Op: "add", Target: n3.ID, ChangeClass: "PRESENTATION", Definition: &n3},
		{Op: "update", Target: page.ID, ExpectedHash: definitionHash(page), ChangeClass: "PRESENTATION", Definition: editedPage(page, "Renamed")},
	}
	want, conflicts, err := rebase(patch(ops...), base, base)
	if err != nil || len(conflicts) != 0 {
		t.Fatal(conflicts, err)
	}
	wantJSON, _ := json.Marshal(want)
	rng := rand.New(rand.NewSource(7))
	for i := 0; i < 25; i++ {
		shuffled := append([]PatchOperation(nil), ops...)
		rng.Shuffle(len(shuffled), func(a, b int) { shuffled[a], shuffled[b] = shuffled[b], shuffled[a] })
		got, c, err := rebase(patch(shuffled...), base, base)
		gotJSON, _ := json.Marshal(got)
		if err != nil || len(c) != 0 || string(gotJSON) != string(wantJSON) {
			t.Fatalf("order changed the result on round %d", i)
		}
	}
	index := map[string]string{}
	for _, d := range want.Definitions {
		raw, _ := json.Marshal(d)
		index[d.ID] = string(raw)
	}
	touched := map[string]bool{page.ID: true, n1.ID: true, n2.ID: true, n3.ID: true}
	for _, d := range base.Definitions {
		raw, _ := json.Marshal(d)
		if !touched[d.ID] && index[d.ID] != string(raw) {
			t.Fatalf("untouched %s changed", d.ID)
		}
	}
	for i := 1; i < len(want.Definitions); i++ {
		if want.Definitions[i-1].ID >= want.Definitions[i].ID {
			t.Fatal("output is not sorted by id")
		}
	}
	// Idempotence: rebasing onto the merged draft is a clean no-op.
	again, c, err := rebase(patch(ops...), base, want)
	againJSON, _ := json.Marshal(again)
	if err != nil || len(c) != 0 || string(againJSON) != string(wantJSON) {
		t.Fatal("re-applying the patch is not a no-op", c, err)
	}
}

func TestPatchRejectsMismatchedOrInvalidDocuments(t *testing.T) {
	base, page := patchFixture(t)
	p := patch(PatchOperation{Op: "update", Target: page.ID, ExpectedHash: definitionHash(page), ChangeClass: "PRESENTATION", Definition: editedPage(page, "x")})
	other := base
	other.AppID = "app.other"
	if _, _, e := rebase(p, base, other); e == nil {
		t.Fatal("different applications merged")
	}
	dup := base
	dup.Definitions = append(append([]Definition(nil), base.Definitions...), page)
	if _, _, e := rebase(p, base, dup); e == nil {
		t.Fatal("duplicate current definition accepted")
	}
	if _, _, e := rebase(p, dup, base); e == nil {
		t.Fatal("duplicate base definition accepted")
	}
	p.Version = "2.0.0"
	if _, _, e := rebase(p, base, base); e == nil {
		t.Fatal("unknown patch version accepted")
	}
	tooMany := patch()
	for i := 0; i < 101; i++ {
		tooMany.Operations = append(tooMany.Operations, PatchOperation{Op: "delete", Target: "page.p" + string(rune('a'+i%26)) + string(rune('a'+i/26)), ChangeClass: "PRESENTATION"})
	}
	if _, _, e := rebase(tooMany, base, base); e == nil {
		t.Fatal("oversized patch accepted")
	}
}
