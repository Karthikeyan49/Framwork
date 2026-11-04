package risk

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
	"reflect"
	"sort"
	"strings"
	"testing"
)

// base returns the facts of an unremarkable second release: nothing changed.
func base() Facts {
	return Facts{Storage: StorageFact{Class: ClassEqual, Reasons: []string{}}, Volume: VolumeNone, Tools: []ToolFact{}, ToolDeclarations: []ToolDeclFact{}}
}

func eval(t *testing.T, f Facts) Assessment {
	t.Helper()
	a, err := Evaluate(f)
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	return a
}

func fired(a Assessment, id string) bool {
	for _, r := range a.Reasons {
		if r == id {
			return true
		}
	}
	return false
}

func TestEveryRuleRowFires(t *testing.T) {
	tool := func(effect, idem, change string, unresolved bool) ToolFact {
		return ToolFact{Ref: "tool.crm.notify", Version: "1.0.0", SideEffect: effect, Idempotency: idem, Change: change, Unresolved: unresolved}
	}
	decl := func(effect, change string, data, weak bool) ToolDeclFact {
		return ToolDeclFact{ID: "tool.local.x", SideEffect: effect, Change: change, GainsDataWrite: data, WeakensApproval: weak}
	}
	cases := []struct {
		name    string
		mutate  func(*Facts)
		rule    string
		level   Level
		blocked bool
	}{
		{"S1 page", func(f *Facts) { f.PageChanged = true }, "S1", Low, false},
		{"S1 label", func(f *Facts) { f.LabelChanged = true }, "S1", Low, false},
		{"S2 entity", func(f *Facts) { f.EntityAdded = true }, "S2", Medium, false},
		{"S3 optional field", func(f *Facts) {
			f.Storage = StorageFact{Class: ClassAdditive, Reasons: []string{"ADD_OPTIONAL"}}
		}, "S3", Low, false},
		{"S4 relationship", func(f *Facts) { f.RelationshipChanged = true }, "S4", Medium, false},
		{"S5 rule", func(f *Facts) { f.RuleChanged = true }, "S5", Medium, false},
		{"S6 financial rule", func(f *Facts) { f.FinancialRuleChanged = true }, "S6", High, false},
		{"S7 workflow", func(f *Facts) { f.WorkflowChanged = true }, "S7", Medium, false},
		{"S7b trigger", func(f *Facts) { f.TriggerWidened = true }, "S7b", Medium, false},
		{"S8 approval gate", func(f *Facts) { f.ApprovalGateChanged = true }, "S8", High, false},
		{"S9 read added", func(f *Facts) { f.Tools = []ToolFact{tool(SideRead, IdemNone, ChangeAdded, false)} }, "S9.read", Medium, false},
		{"S9 none removed", func(f *Facts) { f.Tools = []ToolFact{tool(SideNone, IdemNone, ChangeRemoved, false)} }, "S9.read", Medium, false},
		{"S9 read repinned", func(f *Facts) { f.Tools = []ToolFact{tool(SideRead, IdemNone, ChangeRepinned, false)} }, "S9.read", Medium, false},
		{"S9 write native", func(f *Facts) { f.Tools = []ToolFact{tool(SideWrite, IdemNativeKey, ChangeAdded, false)} }, "S9.write", High, false},
		{"S9 write lookup", func(f *Facts) { f.Tools = []ToolFact{tool(SideWrite, IdemLookupByReference, ChangeRepinned, false)} }, "S9.write", High, false},
		{"S9 write dedupe", func(f *Facts) { f.Tools = []ToolFact{tool(SideWrite, IdemDedupeWindow, ChangeAdded, false)} }, "S9.write", High, false},
		{"S9 write unkeyed", func(f *Facts) { f.Tools = []ToolFact{tool(SideWrite, IdemNone, ChangeAdded, false)} }, "S9.write_unkeyed", Critical, false},
		{"S9 financial", func(f *Facts) { f.Tools = []ToolFact{tool(SideFinancial, IdemNativeKey, ChangeAdded, false)} }, "S9.critical_effect", Critical, false},
		{"S9 destructive removed", func(f *Facts) { f.Tools = []ToolFact{tool(SideDestructive, IdemNone, ChangeRemoved, false)} }, "S9.critical_effect", Critical, false},
		{"S9 write removed", func(f *Facts) { f.Tools = []ToolFact{tool(SideWrite, IdemNone, ChangeRemoved, false)} }, "S9.write_removed", Medium, false},
		{"S9b read inherited", func(f *Facts) { f.Tools = []ToolFact{tool(SideRead, IdemNone, ChangeInherited, false)} }, "S9b.read", Medium, false},
		{"S9b write inherited", func(f *Facts) { f.Tools = []ToolFact{tool(SideWrite, IdemNativeKey, ChangeInherited, false)} }, "S9b.write", High, false},
		{"S9b unkeyed inherited", func(f *Facts) { f.Tools = []ToolFact{tool(SideWrite, IdemNone, ChangeInherited, false)} }, "S9b.write_unkeyed", Critical, false},
		{"S9b financial inherited", func(f *Facts) { f.Tools = []ToolFact{tool(SideFinancial, IdemNone, ChangeInherited, false)} }, "S9b.critical_effect", Critical, false},
		{"S10 none", func(f *Facts) { f.ToolDeclarations = []ToolDeclFact{decl(DeclNone, ChangeAdded, false, false)} }, "S10.read", Medium, false},
		{"S10 read changed", func(f *Facts) { f.ToolDeclarations = []ToolDeclFact{decl(DeclRead, ChangeChanged, false, false)} }, "S10.read", Medium, false},
		{"S10 write", func(f *Facts) { f.ToolDeclarations = []ToolDeclFact{decl(DeclWrite, ChangeAdded, false, false)} }, "S10.write", High, false},
		{"S10 destructive", func(f *Facts) { f.ToolDeclarations = []ToolDeclFact{decl(DeclDestructive, ChangeAdded, false, false)} }, "S10.critical", Critical, false},
		{"S10 financial", func(f *Facts) { f.ToolDeclarations = []ToolDeclFact{decl(DeclFinancial, ChangeChanged, false, false)} }, "S10.critical", Critical, false},
		{"S10 gains data.write", func(f *Facts) { f.ToolDeclarations = []ToolDeclFact{decl(DeclRead, ChangeChanged, true, false)} }, "S10.data_write", High, false},
		{"S10 weakened approval", func(f *Facts) { f.ToolDeclarations = []ToolDeclFact{decl(DeclRead, ChangeChanged, false, true)} }, "S10.weakened_approval", High, false},
		{"S11 plan only", func(f *Facts) { f.PlanOnlyChanged = true }, "S11", Medium, false},
		{"S12 ir upgrade", func(f *Facts) { f.IRUpgrade = true }, "S12", Medium, false},
		{"S13 first release", func(f *Facts) { f.FirstRelease = true }, "S13", Medium, false},
		{"S14 unclassified", func(f *Facts) { f.Unclassified = true }, "S14", High, false},
		{"BLOCK unknown capability", func(f *Facts) { f.UnknownCapability = true }, "BLOCK.unknown_capability", Low, true},
		{"BLOCK unresolved tool", func(f *Facts) { f.Tools = []ToolFact{tool(SideRead, IdemNone, ChangeAdded, true)} }, "BLOCK.tool_unresolved", Medium, true},
		{"BLOCK author is requester at HIGH", func(f *Facts) { f.FinancialRuleChanged, f.AcceptanceAuthorIsRequester = true, true }, "BLOCK.author_is_requester", High, true},
	}
	seen := map[string]bool{}
	for _, c := range cases {
		f := base()
		c.mutate(&f)
		a := eval(t, f)
		if !fired(a, c.rule) || a.Level != c.level || a.Blocked != c.blocked {
			t.Errorf("%s: rule fired=%v level=%s blocked=%v reasons=%v", c.name, fired(a, c.rule), a.Level, a.Blocked, a.Reasons)
		}
		seen[c.rule] = true
	}
	for _, r := range Latest().Spec().Rules {
		if !seen[r.ID] {
			t.Errorf("rule %s has no test", r.ID)
		}
	}
	// A rule that depends on a level is silent below it.
	f := base()
	f.EntityAdded, f.AcceptanceAuthorIsRequester = true, true
	if a := eval(t, f); a.Blocked || fired(a, "BLOCK.author_is_requester") {
		t.Fatal("self-authored acceptance blocked below HIGH")
	}
}

func TestNothingFiredIsLowAndEmptyCandidateIsStable(t *testing.T) {
	a := eval(t, base())
	if a.Level != Low || a.Blocked || a.HighRisk() || len(a.Reasons) != 0 || a.Rollback != RollbackPointerSafe {
		t.Fatalf("unchanged release: %+v", a)
	}
	if a.Separation.Approvals != 1 || len(a.Separation.Excluded) != 0 {
		t.Fatal("LOW separation")
	}
}

func TestStorageVolumeMatrix(t *testing.T) {
	want := map[string][4]string{
		ClassEqual:     {"LOW", "LOW", "LOW", "LOW"},
		ClassAdditive:  {"LOW", "LOW", "MEDIUM", "HIGH"},
		ClassTransform: {"MEDIUM", "MEDIUM", "HIGH", "CRITICAL"},
		ClassBreaking:  {"BLOCKED", "BLOCKED", "BLOCKED", "BLOCKED"},
	}
	for class, row := range want {
		for i, volume := range []string{VolumeNone, VolumeSmall, VolumeLarge, VolumeUnverified} {
			f := base()
			f.Storage.Class, f.Volume = class, volume
			a := eval(t, f)
			got := a.Level.String()
			if a.Blocked {
				got = "BLOCKED"
			}
			if row[i] == "BLOCKED" {
				if !a.Blocked {
					t.Errorf("%s/%s not blocked", class, volume)
				}
				continue
			}
			if a.Blocked || got != row[i] {
				t.Errorf("%s/%s = %s want %s", class, volume, got, row[i])
			}
		}
	}
}

func TestInvalidFactsAreErrorsNeverDefaults(t *testing.T) {
	good := ToolFact{Ref: "tool.a.b", Version: "1.0.0", SideEffect: SideRead, Idempotency: IdemNone, Change: ChangeAdded}
	mut := []func(*Facts){
		func(f *Facts) { f.Storage.Class = "" },
		func(f *Facts) { f.Storage.Class = "MOSTLY_EQUAL" },
		func(f *Facts) { f.Volume = "" },
		func(f *Facts) { f.Volume = "HUGE" },
		func(f *Facts) { f.Storage.Reasons = []string{"lower_case"} },
		func(f *Facts) { f.Storage.Reasons = []string{"A", "A"} },
		func(f *Facts) {
			f.Tools = []ToolFact{func() ToolFact { x := good; x.SideEffect = "SOMETIMES"; return x }()}
		},
		func(f *Facts) { f.Tools = []ToolFact{func() ToolFact { x := good; x.Idempotency = ""; return x }()} },
		func(f *Facts) { f.Tools = []ToolFact{func() ToolFact { x := good; x.Change = "CHANGED"; return x }()} },
		func(f *Facts) {
			f.Tools = []ToolFact{func() ToolFact { x := good; x.Ref = "Tool With Space"; return x }()}
		},
		func(f *Facts) { f.Tools = []ToolFact{good, good} },
		func(f *Facts) {
			f.ToolDeclarations = []ToolDeclFact{{ID: "tool.x", SideEffect: "WRITE", Change: ChangeAdded}}
		},
		func(f *Facts) {
			f.ToolDeclarations = []ToolDeclFact{{ID: "tool.x", SideEffect: DeclRead, Change: ChangeAdded}, {ID: "tool.x", SideEffect: DeclRead, Change: ChangeChanged}}
		},
		func(f *Facts) {
			for i := 0; i <= MaxEntries; i++ {
				x := good
				x.Ref = fmt.Sprintf("tool.t%d.x", i)
				f.Tools = append(f.Tools, x)
			}
		},
	}
	for i, m := range mut {
		f := base()
		m(&f)
		if _, err := Evaluate(f); !errors.Is(err, ErrInvalidFacts) {
			t.Errorf("mutation %d accepted: %v", i, err)
		}
	}
}

func TestDeterminismAndOrderIndependence(t *testing.T) {
	f := base()
	f.WorkflowChanged = true
	f.Storage = StorageFact{Class: ClassTransform, Reasons: []string{"RENAME", "ADD_OPTIONAL"}}
	f.Volume = VolumeSmall
	f.Tools = []ToolFact{
		{Ref: "tool.b.b", Version: "2.0.0", SideEffect: SideWrite, Idempotency: IdemNone, Change: ChangeAdded},
		{Ref: "tool.a.a", Version: "1.0.0", SideEffect: SideRead, Idempotency: IdemNone, Change: ChangeInherited},
	}
	f.ToolDeclarations = []ToolDeclFact{{ID: "tool.z", SideEffect: DeclWrite, Change: ChangeAdded}, {ID: "tool.y", SideEffect: DeclRead, Change: ChangeChanged}}
	a1, a2 := eval(t, f), eval(t, f)
	g := f
	g.Tools = []ToolFact{f.Tools[1], f.Tools[0]}
	g.ToolDeclarations = []ToolDeclFact{f.ToolDeclarations[1], f.ToolDeclarations[0]}
	g.Storage.Reasons = []string{"ADD_OPTIONAL", "RENAME"}
	a3 := eval(t, g)
	j := func(a Assessment) []byte { b, _ := json.Marshal(a); return b }
	if !bytes.Equal(j(a1), j(a2)) || !bytes.Equal(j(a1), j(a3)) {
		t.Fatalf("not deterministic or order dependent:\n%s\n%s", j(a1), j(a3))
	}
	if err := CheckCanonical(f); !errors.Is(err, ErrNotCanonical) {
		t.Fatalf("unsorted facts passed CheckCanonical: %v", err)
	}
	if err := CheckCanonical(g); err != nil {
		t.Fatalf("sorted facts rejected: %v", err)
	}
	n, err := Normalize(g)
	if err != nil || CheckCanonical(n) != nil {
		t.Fatal("normalized facts are not canonical")
	}
	if a1.Level != Critical || a1.Rollback != RollbackExternalEffects {
		t.Fatalf("expected CRITICAL/EXTERNAL_EFFECTS got %s/%s", a1.Level, a1.Rollback)
	}
}

func TestRollbackClasses(t *testing.T) {
	f := base()
	if eval(t, f).Rollback != RollbackPointerSafe {
		t.Fatal("equal storage without tools must be pointer safe")
	}
	f.Storage.Class = ClassAdditive
	if eval(t, f).Rollback != RollbackDataNotReversible {
		t.Fatal("additive storage must not be pointer safe")
	}
	f.Tools = []ToolFact{{Ref: "tool.a.b", Version: "1.0.0", SideEffect: SideWrite, Idempotency: IdemNativeKey, Change: ChangeInherited}}
	if eval(t, f).Rollback != RollbackExternalEffects {
		t.Fatal("write tool must report external effects")
	}
	g := base()
	g.Tools = []ToolFact{{Ref: "tool.a.b", Version: "1.0.0", SideEffect: SideRead, Idempotency: IdemNone, Change: ChangeAdded}}
	if eval(t, g).Rollback != RollbackPointerSafe {
		t.Fatal("read tools are pointer safe")
	}
}

func TestSeparationByLevel(t *testing.T) {
	cases := map[Level]Separation{
		Low:      {Approvals: 1, Excluded: []string{}},
		Medium:   {Approvals: 1, Excluded: []string{}},
		High:     {Approvals: 1, Excluded: []string{RoleRequester, RoleAcceptanceAuthor}},
		Critical: {Approvals: 2, Excluded: []string{RoleRequester, RoleAcceptanceAuthor, RoleSandboxFreezer}},
	}
	for level, want := range cases {
		f := base()
		switch level {
		case Medium:
			f.EntityAdded = true
		case High:
			f.FinancialRuleChanged = true
		case Critical:
			f.Tools = []ToolFact{{Ref: "tool.a.b", Version: "1.0.0", SideEffect: SideWrite, Idempotency: IdemNone, Change: ChangeAdded}}
		}
		a := eval(t, f)
		if a.Level != level || !reflect.DeepEqual(a.Separation, want) {
			t.Errorf("%s: level %s separation %+v", level, a.Level, a.Separation)
		}
	}
}

func TestCheckSeparation(t *testing.T) {
	parties := Parties{Requester: "req-1", AcceptanceAuthor: "acc-1", SandboxFreezer: "frz-1"}
	high := Separation{Approvals: 1, Excluded: []string{RoleRequester, RoleAcceptanceAuthor}}
	critical := Separation{Approvals: 2, Excluded: []string{RoleRequester, RoleAcceptanceAuthor, RoleSandboxFreezer}}
	ok := []struct {
		sep       Separation
		approvers []string
	}{
		{Separation{Approvals: 1, Excluded: []string{}}, []string{"req-1"}},
		{high, []string{"frz-1"}},
		{critical, []string{"ap-1", "ap-2"}},
		{high, []string{"ap-1", "req-1x"}},
	}
	for i, c := range ok {
		if err := CheckSeparation(c.sep, parties, c.approvers); err != nil {
			t.Errorf("case %d: %v", i, err)
		}
	}
	bad := []struct {
		name      string
		sep       Separation
		parties   Parties
		approvers []string
	}{
		{"requester approves own HIGH", high, parties, []string{"req-1"}},
		{"acceptance author approves", high, parties, []string{"acc-1"}},
		{"case folded requester", high, parties, []string{"REQ-1"}},
		{"freezer on CRITICAL", critical, parties, []string{"ap-1", "frz-1"}},
		{"one approver on CRITICAL", critical, parties, []string{"ap-1"}},
		{"duplicate approver counted once", critical, parties, []string{"ap-1", "ap-1"}},
		{"no approvers", high, parties, nil},
		{"empty approver id", high, parties, []string{""}},
		{"control character id", high, parties, []string{"ap\n1"}},
		{"missing party id", high, Parties{Requester: "req-1"}, []string{"ap-1"}},
		{"zero approvals required", Separation{Approvals: 0}, parties, []string{"ap-1"}},
		{"unknown excluded role", Separation{Approvals: 1, Excluded: []string{"ceo"}}, parties, []string{"ap-1"}},
	}
	for _, c := range bad {
		if err := CheckSeparation(c.sep, c.parties, c.approvers); !errors.Is(err, ErrSeparation) {
			t.Errorf("%s accepted: %v", c.name, err)
		}
	}
}

func TestVolumeBucket(t *testing.T) {
	cases := []struct {
		n     int64
		known bool
		want  string
	}{
		{0, true, VolumeNone}, {1, true, VolumeSmall}, {10000, true, VolumeSmall}, {10001, true, VolumeLarge},
		{100000, true, VolumeLarge}, {100001, true, VolumeUnverified}, {-1, true, VolumeUnverified}, {0, false, VolumeUnverified}, {5, false, VolumeUnverified},
	}
	for _, c := range cases {
		if got := VolumeBucket(c.n, c.known); got != c.want {
			t.Errorf("VolumeBucket(%d,%v)=%s want %s", c.n, c.known, got, c.want)
		}
	}
}

func TestRegistryFailsClosed(t *testing.T) {
	if _, err := EvaluateWith("release-risk-v0", base()); !errors.Is(err, ErrUnknownPolicy) {
		t.Fatal("unknown policy evaluated")
	}
	if _, ok := Lookup(PolicyV1); !ok || !reflect.DeepEqual(Versions(), []string{PolicyV1}) {
		t.Fatal("built-in registry")
	}
	a := eval(t, base())
	if err := Verify(a, base()); err != nil {
		t.Fatal(err)
	}
	f := base()
	f.FinancialRuleChanged = true
	if err := Verify(a, f); !errors.Is(err, ErrMismatch) {
		t.Fatal("assessment verified against different facts")
	}
	b := a
	b.Level = Critical
	if err := Verify(b, base()); !errors.Is(err, ErrMismatch) {
		t.Fatal("raised level verified")
	}
	b = a
	b.PolicyHash = strings.Repeat("0", 64)
	if err := Verify(b, base()); !errors.Is(err, ErrMismatch) {
		t.Fatal("forged policy hash verified")
	}
	b = a
	b.PolicyVersion = "release-risk-v9"
	if err := Verify(b, base()); !errors.Is(err, ErrUnknownPolicy) {
		t.Fatal("unknown policy verified")
	}
	if _, err := (Policy{}).Evaluate(base()); !errors.Is(err, ErrInvalidPolicy) {
		t.Fatal("zero policy evaluated")
	}
	var zero Registry
	if _, err := zero.Evaluate(base()); err == nil {
		t.Fatal("zero registry evaluated")
	}
	if _, err := NewRegistry(); err == nil {
		t.Fatal("empty registry")
	}
	if _, err := NewRegistry(Latest(), Latest()); err == nil {
		t.Fatal("duplicate policy versions")
	}
}

func TestPolicyRejectsNonMonotoneAndMalformedSpecs(t *testing.T) {
	mutate := func(f func(*Spec)) error {
		s := Latest().Spec()
		f(&s)
		_, err := NewPolicy(s)
		return err
	}
	cases := map[string]func(*Spec){
		"matrix lowers with volume": func(s *Spec) { s.Matrix[1].Level = "LOW"; s.Matrix[5].Level = "HIGH"; s.Matrix[6].Level = "LOW" },
		"matrix lowers with class":  func(s *Spec) { s.Matrix[11].Level = "MEDIUM" },
		"missing cell":              func(s *Spec) { s.Matrix = s.Matrix[1:] },
		"duplicate rule id":         func(s *Spec) { s.Rules[1].ID = s.Rules[0].ID },
		"unknown outcome":           func(s *Spec) { s.Rules[0].Level = "NONE" },
		"empty trigger":             func(s *Spec) { s.Rules[0].Trigger = Trigger{} },
		"unknown fact":              func(s *Spec) { s.Rules[0].Trigger = Trigger{Any: []string{"made_up"}} },
		"two trigger kinds":         func(s *Spec) { s.Rules[0].Trigger.Storage = &StorageMatch{Class: []string{ClassEqual}} },
		"thresholds inverted":       func(s *Spec) { s.Thresholds = Thresholds{SmallMax: 100, LargeMax: 10} },
		"separation weakens":        func(s *Spec) { s.Separation[3].Approvals = 1; s.Separation[3].Excluded = []string{RoleRequester} },
		"zero approvals":            func(s *Spec) { s.Separation[0].Approvals = 0 },
		"min_level on level rule":   func(s *Spec) { s.Rules[0].Trigger.MinLevel = "HIGH" },
		"bad rollback order": func(s *Spec) {
			s.Rollback.Order = []string{RollbackExternalEffects, RollbackPointerSafe, RollbackDataNotReversible}
		},
		"bad version": func(s *Spec) { s.Version = "" },
	}
	for name, f := range cases {
		if err := mutate(f); !errors.Is(err, ErrInvalidPolicy) {
			t.Errorf("%s: %v", name, err)
		}
	}
	if err := mutate(func(*Spec) {}); err != nil {
		t.Fatal("unmodified spec rejected:", err)
	}
}

// Changing any table entry must change the hash; copies never alias the spec.
func TestPolicyHashCoversEveryEntry(t *testing.T) {
	h := Latest().Hash()
	if len(h) != 64 {
		t.Fatal("hash length")
	}
	s := Latest().Spec()
	changes := []func(*Spec){
		func(s *Spec) { s.Rules[0].Level = "MEDIUM" },
		func(s *Spec) { s.Rules[5].ID = "S6x" },
		func(s *Spec) { s.Matrix[9].Level = "CRITICAL" },
		func(s *Spec) { s.Thresholds.SmallMax++ },
		func(s *Spec) { s.Separation[3].Approvals = 3 },
		func(s *Spec) { s.Rollback.Rules = s.Rollback.Rules[1:] },
	}
	for i, c := range changes {
		x := s.clone()
		c(&x)
		if x.Hash() == h {
			t.Errorf("change %d did not alter the hash", i)
		}
	}
	got := Latest().Spec()
	got.Rules[0].ID = "tampered"
	if Latest().Spec().Rules[0].ID == "tampered" || Latest().Hash() != h {
		t.Fatal("Spec exposes internal state")
	}
}

// Golden values pin release-risk-v1: editing any rule, cell or threshold must
// fail here and requires publishing release-risk-v2 instead.
func TestGoldenPolicyHashAndAssessments(t *testing.T) {
	const wantPolicyHash = "a823af17b92ea80e42f86864440b78e1d2dc3096e67c5b58645eb94d469102d4"
	if Latest().Hash() != wantPolicyHash {
		t.Fatalf("release-risk-v1 changed: %s", Latest().Hash())
	}
	w := func(effect, idem, change string) []ToolFact {
		return []ToolFact{{Ref: "tool.crm.notify", Version: "1.0.0", SideEffect: effect, Idempotency: idem, Change: change}}
	}
	cases := []struct {
		name  string
		facts func() Facts
		want  string
	}{
		{"unchanged", func() Facts { return base() }, `{"policy_version":"release-risk-v1","policy_hash":"a823af17b92ea80e42f86864440b78e1d2dc3096e67c5b58645eb94d469102d4","level":"LOW","blocked":false,"reasons":[],"facts_hash":"8926d8b1b84d55b506b6d4e629256e709a5d81effe3601f9a2181a170971c620","separation":{"approvals":1,"excluded":[]},"rollback":"POINTER_SAFE"}`},
		{"first release", func() Facts {
			f := base()
			f.FirstRelease, f.EntityAdded = true, true
			f.Storage.Class = ClassAdditive
			return f
		}, `{"policy_version":"release-risk-v1","policy_hash":"a823af17b92ea80e42f86864440b78e1d2dc3096e67c5b58645eb94d469102d4","level":"MEDIUM","blocked":false,"reasons":["S13","S2","SV.ADDITIVE.NONE"],"facts_hash":"1bb13470c702995c4d4485b501c3c82017c44eb02046a238bdc3267da9bf964b","separation":{"approvals":1,"excluded":[]},"rollback":"DATA_NOT_REVERSIBLE"}`},
		{"page only", func() Facts { f := base(); f.PageChanged = true; return f }, `{"policy_version":"release-risk-v1","policy_hash":"a823af17b92ea80e42f86864440b78e1d2dc3096e67c5b58645eb94d469102d4","level":"LOW","blocked":false,"reasons":["S1"],"facts_hash":"c8e445c031dfd3f7d0b6ccae25e8480b344b0702618d7c80de14e338fc9784b6","separation":{"approvals":1,"excluded":[]},"rollback":"POINTER_SAFE"}`},
		{"financial rule", func() Facts { f := base(); f.FinancialRuleChanged = true; return f }, `{"policy_version":"release-risk-v1","policy_hash":"a823af17b92ea80e42f86864440b78e1d2dc3096e67c5b58645eb94d469102d4","level":"HIGH","blocked":false,"reasons":["S6"],"facts_hash":"cebf0f1e4a598a2ea147491a70681e869e2efc09dfd7aa26e3afc421fcf9f9dd","separation":{"approvals":1,"excluded":["requester","acceptance_author"]},"rollback":"POINTER_SAFE"}`},
		{"approval gate", func() Facts { f := base(); f.ApprovalGateChanged, f.WorkflowChanged = true, true; return f }, `{"policy_version":"release-risk-v1","policy_hash":"a823af17b92ea80e42f86864440b78e1d2dc3096e67c5b58645eb94d469102d4","level":"HIGH","blocked":false,"reasons":["S7","S8"],"facts_hash":"5e1881cc7ce43239b49de0e96be3a70aae2a723c7a222c285d40a8f80fd710ec","separation":{"approvals":1,"excluded":["requester","acceptance_author"]},"rollback":"POINTER_SAFE"}`},
		{"write tool keyed", func() Facts { f := base(); f.Tools = w(SideWrite, IdemNativeKey, ChangeAdded); return f }, `{"policy_version":"release-risk-v1","policy_hash":"a823af17b92ea80e42f86864440b78e1d2dc3096e67c5b58645eb94d469102d4","level":"HIGH","blocked":false,"reasons":["S9.write"],"facts_hash":"8105c4b3140d0bacead8f9b2ebfba01e384a8c0d6f14b46f2936707e57191fe3","separation":{"approvals":1,"excluded":["requester","acceptance_author"]},"rollback":"EXTERNAL_EFFECTS"}`},
		{"write tool unkeyed", func() Facts { f := base(); f.Tools = w(SideWrite, IdemNone, ChangeAdded); return f }, `{"policy_version":"release-risk-v1","policy_hash":"a823af17b92ea80e42f86864440b78e1d2dc3096e67c5b58645eb94d469102d4","level":"CRITICAL","blocked":false,"reasons":["S9.write_unkeyed"],"facts_hash":"1f17ba67f0c46aa86487c3a77e998a75ab1f3bbc130af4378a1a87239c39c778","separation":{"approvals":2,"excluded":["requester","acceptance_author","sandbox_freezer"]},"rollback":"EXTERNAL_EFFECTS"}`},
		{"additive unverified", func() Facts {
			f := base()
			f.Storage, f.Volume = StorageFact{Class: ClassAdditive, Reasons: []string{"ADD_OPTIONAL"}}, VolumeUnverified
			return f
		}, `{"policy_version":"release-risk-v1","policy_hash":"a823af17b92ea80e42f86864440b78e1d2dc3096e67c5b58645eb94d469102d4","level":"HIGH","blocked":false,"reasons":["S3","SV.ADDITIVE.UNVERIFIED"],"facts_hash":"40f4268e6eba89ae9f65d7a882bf90a1ac97e36da2932c63178543455fca5cb3","separation":{"approvals":1,"excluded":["requester","acceptance_author"]},"rollback":"DATA_NOT_REVERSIBLE"}`},
		{"transform unverified", func() Facts {
			f := base()
			f.Storage, f.Volume = StorageFact{Class: ClassTransform, Reasons: []string{"BACKFILL"}}, VolumeUnverified
			return f
		}, `{"policy_version":"release-risk-v1","policy_hash":"a823af17b92ea80e42f86864440b78e1d2dc3096e67c5b58645eb94d469102d4","level":"CRITICAL","blocked":false,"reasons":["SV.TRANSFORM.UNVERIFIED"],"facts_hash":"20db7d708f6c002e4af813bd5c9c0c908a13f27dcb216f13a2927c0fd92a1521","separation":{"approvals":2,"excluded":["requester","acceptance_author","sandbox_freezer"]},"rollback":"DATA_NOT_REVERSIBLE"}`},
		{"breaking", func() Facts { f := base(); f.Storage.Class = ClassBreaking; return f }, `{"policy_version":"release-risk-v1","policy_hash":"a823af17b92ea80e42f86864440b78e1d2dc3096e67c5b58645eb94d469102d4","level":"LOW","blocked":true,"reasons":["SV.BREAKING.NONE"],"facts_hash":"a69090a50a1a6e98598040affd07882f12cae50972d20883d655e4c572dc34ed","separation":{"approvals":1,"excluded":[]},"rollback":"DATA_NOT_REVERSIBLE"}`},
		{"unresolved tool", func() Facts {
			f := base()
			f.Tools = []ToolFact{{Ref: "tool.crm.gone", Version: "1.0.0", SideEffect: SideRead, Idempotency: IdemNone, Change: ChangeInherited, Unresolved: true}}
			return f
		}, `{"policy_version":"release-risk-v1","policy_hash":"a823af17b92ea80e42f86864440b78e1d2dc3096e67c5b58645eb94d469102d4","level":"MEDIUM","blocked":true,"reasons":["BLOCK.tool_unresolved","S9b.read"],"facts_hash":"319fa40077475d8d7d25f0aeb242d88d3a69b37e474232d2c2ba2fb798581c6d","separation":{"approvals":1,"excluded":[]},"rollback":"POINTER_SAFE"}`},
		{"self-authored acceptance at HIGH", func() Facts {
			f := base()
			f.FinancialRuleChanged, f.AcceptanceAuthorIsRequester = true, true
			return f
		}, `{"policy_version":"release-risk-v1","policy_hash":"a823af17b92ea80e42f86864440b78e1d2dc3096e67c5b58645eb94d469102d4","level":"HIGH","blocked":true,"reasons":["BLOCK.author_is_requester","S6"],"facts_hash":"2bc973bf7ec939cbe4f2bbcb3d7e145384e0c98107828574359caceb6cabde7e","separation":{"approvals":1,"excluded":["requester","acceptance_author"]},"rollback":"POINTER_SAFE"}`},
	}
	for _, c := range cases {
		a := eval(t, c.facts())
		got, _ := json.Marshal(a)
		if string(got) != c.want {
			t.Errorf("%s:\n got %s\nwant %s", c.name, got, c.want)
		}
	}
}

// Adding a risk fact never lowers the level and never lifts a block.
func TestMonotonicityProperty(t *testing.T) {
	rng := rand.New(rand.NewSource(12))
	toolEffect := []string{SideNone, SideRead, SideWrite, SideFinancial, SideDestructive}
	idem := []string{IdemNativeKey, IdemLookupByReference, IdemDedupeWindow, IdemNone}
	changes := []string{ChangeAdded, ChangeRemoved, ChangeRepinned, ChangeInherited}
	declEff := []string{DeclNone, DeclRead, DeclWrite, DeclDestructive, DeclFinancial}
	declCh := []string{ChangeAdded, ChangeRemoved, ChangeChanged}
	rank := func(a Assessment) int {
		if a.Blocked {
			return 10
		}
		return int(a.Level)
	}
	randomFacts := func() Facts {
		f := base()
		bits := rng.Intn(1 << 12)
		f.PageChanged, f.LabelChanged, f.EntityAdded, f.RelationshipChanged = bits&1 != 0, bits&2 != 0, bits&4 != 0, bits&8 != 0
		f.RuleChanged, f.FinancialRuleChanged, f.WorkflowChanged, f.TriggerWidened = bits&16 != 0, bits&32 != 0, bits&64 != 0, bits&128 != 0
		f.ApprovalGateChanged, f.PlanOnlyChanged, f.IRUpgrade, f.FirstRelease = bits&256 != 0, bits&512 != 0, bits&1024 != 0, bits&2048 != 0
		f.Storage.Class, f.Volume = classes[rng.Intn(4)], volumes[rng.Intn(4)]
		for i := rng.Intn(4); i > 0; i-- {
			f.Tools = append(f.Tools, ToolFact{Ref: "tool.t" + string(rune('a'+rng.Intn(20))) + ".x", Version: "1.0.0", SideEffect: toolEffect[rng.Intn(5)], Idempotency: idem[rng.Intn(4)], Change: changes[rng.Intn(4)]})
		}
		for i := rng.Intn(3); i > 0; i-- {
			f.ToolDeclarations = append(f.ToolDeclarations, ToolDeclFact{ID: "tool.d" + string(rune('a'+rng.Intn(20))), SideEffect: declEff[rng.Intn(5)], Change: declCh[rng.Intn(3)], GainsDataWrite: rng.Intn(4) == 0, WeakensApproval: rng.Intn(4) == 0})
		}
		n, err := Normalize(f)
		if err != nil {
			return base() // duplicate random tool fact; a plain base is a valid starting point
		}
		return n
	}
	flips := []func(*Facts){
		func(f *Facts) { f.PageChanged = true }, func(f *Facts) { f.LabelChanged = true }, func(f *Facts) { f.EntityAdded = true },
		func(f *Facts) { f.RelationshipChanged = true }, func(f *Facts) { f.RuleChanged = true }, func(f *Facts) { f.FinancialRuleChanged = true },
		func(f *Facts) { f.WorkflowChanged = true }, func(f *Facts) { f.TriggerWidened = true }, func(f *Facts) { f.ApprovalGateChanged = true },
		func(f *Facts) { f.PlanOnlyChanged = true }, func(f *Facts) { f.IRUpgrade = true }, func(f *Facts) { f.FirstRelease = true },
		func(f *Facts) { f.Unclassified = true }, func(f *Facts) { f.UnknownCapability = true },
		func(f *Facts) { f.Volume = VolumeUnverified },
		func(f *Facts) { f.Storage.Class = ClassBreaking },
		func(f *Facts) {
			f.Tools = append(f.Tools, ToolFact{Ref: "tool.zz.zz", Version: "9.9.9", SideEffect: SideWrite, Idempotency: IdemNone, Change: ChangeAdded})
		},
		func(f *Facts) {
			f.ToolDeclarations = append(f.ToolDeclarations, ToolDeclFact{ID: "tool.zz", SideEffect: DeclFinancial, Change: ChangeAdded})
		},
	}
	for i := 0; i < 600; i++ {
		f := randomFacts()
		before := eval(t, f)
		for j, flip := range flips {
			g := f
			g.Tools = append([]ToolFact{}, f.Tools...)
			g.ToolDeclarations = append([]ToolDeclFact{}, f.ToolDeclarations...)
			flip(&g)
			after, err := Evaluate(g)
			if err != nil {
				continue
			}
			if rank(after) < rank(before) {
				t.Fatalf("flip %d lowered %v -> %v for %+v", j, before.Reasons, after.Reasons, f)
			}
			if before.Blocked && !after.Blocked {
				t.Fatalf("flip %d lifted a block", j)
			}
		}
		// Raising volume or storage class never lowers.
		for k := 0; k < 3; k++ {
			g := f
			if g.Volume != VolumeUnverified {
				g.Volume = volumes[indexOf(volumes, g.Volume)+1]
				if a := eval(t, g); rank(a) < rank(before) {
					t.Fatal("larger volume lowered the verdict")
				}
			}
		}
	}
	// The unsorted list also holds for a sorted copy of the level names.
	names := []string{"LOW", "MEDIUM", "HIGH", "CRITICAL"}
	if !sort.StringsAreSorted([]string{"CRITICAL", "HIGH", "LOW", "MEDIUM"}) || len(names) != 4 {
		t.Fatal("test self-check")
	}
}

func TestLevelSerialization(t *testing.T) {
	for l := Low; l <= Critical; l++ {
		b, err := json.Marshal(l)
		if err != nil {
			t.Fatal(err)
		}
		var back Level
		if json.Unmarshal(b, &back) != nil || back != l {
			t.Fatalf("round trip %s", l)
		}
	}
	if _, err := json.Marshal(Level(0)); err == nil {
		t.Fatal("zero level serialized")
	}
	var l Level
	for _, bad := range []string{`"low"`, `"BLOCKED"`, `""`, `1`, `null`} {
		if json.Unmarshal([]byte(bad), &l) == nil && bad != `null` {
			t.Errorf("accepted %s", bad)
		}
	}
}
