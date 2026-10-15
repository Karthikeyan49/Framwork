package intent

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

func thresholdSemantics(op, amount, currency string, roles ...string) json.RawMessage {
	if roles == nil {
		roles = []string{"admin"}
	}
	raw, _ := json.Marshal(ApprovalThreshold{WorkflowID: "workflow.order.approve", EntityID: "entity.order", FieldID: "field.order.amount", Currency: currency, Op: op, Amount: amount, ApproverRoles: roles})
	return raw
}

func req(id, category string, semantics json.RawMessage) RequirementV2 {
	return RequirementV2{ID: id, Category: category, Requiredness: Mandatory, RequirednessBasis: BasisPolicyFloor, Status: StatusResolved, Provenance: ProvenanceHuman, Semantics: semantics}
}

func thresholdSet(op, amount, currency string) RequirementSet {
	return RequirementSet{SchemaVersion: RequirementSetVersion, AppID: "app.orders", Requirements: []RequirementV2{req("po-approval", CategoryApprovalThreshold, thresholdSemantics(op, amount, currency))}}
}

func mustParse(t *testing.T, s RequirementSet) (RequirementSet, []byte, string) {
	t.Helper()
	canonical, err := s.Canonical()
	if err != nil {
		t.Fatal(err)
	}
	parsed, again, hash, err := ParseRequirementSet(canonical)
	if err != nil || string(again) != string(canonical) || hash != s.Hash() {
		t.Fatal("canonical round trip failed:", err)
	}
	return parsed, canonical, hash
}

func code(err error) string {
	var v *Violation
	if errors.As(err, &v) {
		return v.Code
	}
	return ""
}

func TestRequirementSetCanonicalRoundTripAndHashStability(t *testing.T) {
	set := thresholdSet("GT", "500.00", "USD")
	_, canonical, hash := mustParse(t, set)
	if len(hash) != 64 || hash != set.Hash() {
		t.Fatal("hash")
	}
	// Semantics authored with different whitespace and key order canonicalize alike.
	loose := set
	loose.Requirements = append([]RequirementV2(nil), set.Requirements...)
	loose.Requirements[0].Semantics = json.RawMessage(`{ "amount":"500.00", "op":"GT", "currency":"USD", "approver_roles":["admin"], "field_id":"field.order.amount", "entity_id":"entity.order", "workflow_id":"workflow.order.approve" }`)
	if got, err := loose.Canonical(); err != nil || string(got) != string(canonical) {
		t.Fatal("semantics are not canonicalized", err)
	}
	// A different amount is a different artifact.
	if thresholdSet("GT", "501.00", "USD").Hash() == hash {
		t.Fatal("hash ignores semantics")
	}
}

func TestParseRequirementSetNegatives(t *testing.T) {
	good := thresholdSet("GT", "500.00", "USD")
	canonical, _ := good.Canonical()
	text := string(canonical)
	mutate := func(old, new string) []byte {
		if !strings.Contains(text, old) {
			t.Fatalf("fixture lacks %q", old)
		}
		return []byte(strings.Replace(text, old, new, 1))
	}
	cases := map[string][]byte{
		"duplicate key":          mutate(`"id":"po-approval"`, `"id":"po-approval","id":"other"`),
		"unknown key":            mutate(`"app_id":"app.orders"`, `"app_id":"app.orders","extra":1`),
		"wrong case key":         mutate(`"app_id"`, `"App_ID"`),
		"trailing data":          append(append([]byte(nil), canonical...), []byte(` {}`)...),
		"bad schema version":     mutate(`requirements-v2`, `requirements-v3`),
		"bad app id":             mutate(`app.orders`, `orders`),
		"bad requirement id":     mutate(`"po-approval"`, `"Po Approval"`),
		"unknown requiredness":   mutate(`"MANDATORY"`, `"SOMETIMES"`),
		"unknown status":         mutate(`"RESOLVED"`, `"DONE"`),
		"unknown provenance":     mutate(`"AUTHENTICATED_HUMAN"`, `"AI"`),
		"unsorted roles":         mutate(`"approver_roles":["admin"]`, `"approver_roles":["owner","admin"]`),
		"unknown role":           mutate(`"approver_roles":["admin"]`, `"approver_roles":["ceo"]`),
		"empty roles":            mutate(`"approver_roles":["admin"]`, `"approver_roles":[]`),
		"bad operator":           mutate(`"op":"GT"`, `"op":"CONTAINS"`),
		"bad currency":           mutate(`"currency":"USD"`, `"currency":"XXX"`),
		"amount finer than unit": mutate(`"amount":"500.00"`, `"amount":"500.001"`),
		"amount not decimal":     mutate(`"amount":"500.00"`, `"amount":"five"`),
		"semantic unknown key":   mutate(`"op":"GT"`, `"op":"GT","tolerance":"1"`),
		"empty document":         []byte(``),
		"null requirements":      []byte(`{"schema_version":"requirements-v2","app_id":"app.orders","requirements":null}`),
		"oversize":               append([]byte(`{"x":"`), []byte(strings.Repeat("a", MaxRequirementBytes))...),
		"semantics wrong type":   mutate(`"approver_roles":["admin"]`, `"approver_roles":"admin"`),
		"explicit empty context": mutate(`"workflow_id"`, `"context":{},"workflow_id"`),
	}
	for name, raw := range cases {
		if _, _, _, err := ParseRequirementSet(raw); err == nil {
			t.Errorf("%s accepted", name)
		}
	}
	// A set with too many requirements.
	many := RequirementSet{SchemaVersion: RequirementSetVersion, AppID: "app.orders"}
	for i := 0; i <= MaxRequirements; i++ {
		many.Requirements = append(many.Requirements, req(fmt.Sprintf("r%d", i), CategoryDataMigration, json.RawMessage(`{"max_class":"COMPATIBLE"}`)))
	}
	if code(many.Validate()) != CodeTooLarge {
		t.Error("101 requirements accepted")
	}
	dup := RequirementSet{SchemaVersion: RequirementSetVersion, AppID: "app.orders", Requirements: []RequirementV2{
		req("same", CategoryDataMigration, json.RawMessage(`{"max_class":"COMPATIBLE"}`)), req("same", CategoryDataMigration, json.RawMessage(`{"max_class":"COMPATIBLE"}`))}}
	if dup.Validate() == nil {
		t.Error("duplicate requirement ids accepted")
	}
}

func TestUnknownCategoriesAreRecordedButFlagged(t *testing.T) {
	set := RequirementSet{SchemaVersion: RequirementSetVersion, AppID: "app.orders", Requirements: []RequirementV2{
		{ID: "sod", Category: "segregation_of_duties", Requiredness: Recommended, RequirednessBasis: BasisOwnerDecision, Status: StatusResolved, Provenance: ProvenanceHuman, Semantics: json.RawMessage(`{"zeta":1,"alpha":"x"}`)}}}
	parsed, canonical, _ := mustParse(t, set)
	if parsed.Requirements[0].Known() || !strings.Contains(string(canonical), `{"alpha":"x","zeta":1}`) {
		t.Fatal("unknown category semantics are not canonical", string(canonical))
	}
	set.Requirements[0].Semantics = json.RawMessage(`[1]`)
	if set.Validate() == nil {
		t.Fatal("non-object semantics accepted")
	}
	set.Requirements[0].Semantics = json.RawMessage(`{"a":"` + strings.Repeat("x", maxUnknownSemantics) + `"}`)
	if set.Validate() == nil {
		t.Fatal("oversized unknown semantics accepted")
	}
}

func TestRequirementPolicyFloors(t *testing.T) {
	for _, c := range []struct{ category, class, want string }{
		{CategoryApprovalThreshold, "EQUAL", Mandatory}, {CategoryApprovalGate, "EQUAL", Mandatory}, {CategoryToolEffect, "ADDITIVE", Mandatory},
		{CategoryDataMigration, "EQUAL", Optional}, {CategoryDataMigration, "ADDITIVE", Mandatory}, {CategoryDataMigration, "TRANSFORM", Mandatory},
		{"segregation_of_duties", "TRANSFORM", Optional},
	} {
		if got := FloorFor(c.category, c.class); got != c.want {
			t.Errorf("FloorFor(%s,%s)=%s", c.category, c.class, got)
		}
	}
	if len(RequirementPolicyHash()) != 64 || RequirementPolicyHash() != RequirementPolicyHash() {
		t.Fatal("policy hash")
	}
	ok := thresholdSet("GT", "500.00", "USD")
	if err := CheckFloors(ok, "EQUAL"); err != nil {
		t.Fatal(err)
	}
	downgraded := thresholdSet("GT", "500.00", "USD")
	downgraded.Requirements[0].Requiredness, downgraded.Requirements[0].RequirednessBasis = Recommended, BasisOwnerDecision
	if code(CheckFloors(downgraded, "EQUAL")) != CodeFloorViolation {
		t.Fatal("downgraded floor accepted")
	}
	downgraded.Requirements[0].Requiredness, downgraded.Requirements[0].RequirednessBasis = Optional, BasisPolicyFloor
	if code(CheckFloors(downgraded, "EQUAL")) != CodeFloorViolation {
		t.Fatal("optional floor accepted")
	}
	// An owner decision may make any category mandatory; a floor claim needs a floor.
	owner := RequirementSet{SchemaVersion: RequirementSetVersion, AppID: "app.orders", Requirements: []RequirementV2{
		{ID: "x", Category: "segregation_of_duties", Requiredness: Mandatory, RequirednessBasis: BasisOwnerDecision, Status: StatusResolved, Provenance: ProvenanceHuman, Semantics: json.RawMessage(`{}`)}}}
	if err := CheckFloors(owner, "EQUAL"); err != nil {
		t.Fatal(err)
	}
	owner.Requirements[0].RequirednessBasis = BasisPolicyFloor
	if code(CheckFloors(owner, "EQUAL")) != CodeFloorViolation {
		t.Fatal("floor claimed without a floor")
	}
	// data_migration is only floor-mandatory for non-equal storage, but the claim
	// is acceptable for any release (the set is authored before the class is known).
	migration := RequirementSet{SchemaVersion: RequirementSetVersion, AppID: "app.orders", Requirements: []RequirementV2{req("m", CategoryDataMigration, json.RawMessage(`{"max_class":"TRANSFORM"}`))}}
	for _, class := range []string{"EQUAL", "ADDITIVE"} {
		if err := CheckFloors(migration, class); err != nil {
			t.Errorf("%s: %v", class, err)
		}
	}
	migration.Requirements[0].Requiredness, migration.Requirements[0].RequirednessBasis = Optional, BasisOwnerDecision
	if err := CheckFloors(migration, "EQUAL"); err != nil {
		t.Fatal(err)
	}
	if code(CheckFloors(migration, "TRANSFORM")) != CodeFloorViolation {
		t.Fatal("optional migration requirement accepted for a transform")
	}
}

func TestOracleTruthTables(t *testing.T) {
	// Exact boundaries for currencies with 0, 2 and 3 minor units.
	for _, c := range []struct {
		currency, below, at, above string
		limit                      string
	}{
		{"JPY", "499", "500", "501", "500"},
		{"USD", "499.99", "500.00", "500.01", "500"},
		{"KWD", "499.999", "500.000", "500.001", "500.000"},
	} {
		want := map[string][3]bool{ // below, at, above
			"GT": {false, false, true}, "GTE": {false, true, true}, "LT": {true, false, false},
			"LTE": {true, true, false}, "EQ": {false, true, false}, "NEQ": {true, false, true},
		}
		for op, expectations := range want {
			tr := ApprovalThreshold{Currency: c.currency, Op: op, Amount: c.limit, ApproverRoles: []string{"admin", "owner"}}
			for i, amount := range []string{c.below, c.at, c.above} {
				got, err := OracleThreshold(tr, amount, c.currency)
				if err != nil {
					t.Fatal(err)
				}
				wantOutcome := OutcomeNoApproval
				if expectations[i] {
					wantOutcome = OutcomeApprovalRequired
				}
				if got.Outcome != wantOutcome || (expectations[i] && !reflect.DeepEqual(got.Roles, []string{"admin", "owner"})) {
					t.Errorf("%s %s %s %s: %v", c.currency, op, c.limit, amount, got)
				}
			}
		}
	}
	tr := ApprovalThreshold{Currency: "USD", Op: "GT", Amount: "500.00", ApproverRoles: []string{"admin"}}
	for _, c := range []struct {
		amount, currency, reason string
	}{{"501", "INR", ReasonCurrency}, {"501", "XXX", ReasonCurrency}, {"501", "", ReasonCurrency}, {"five", "USD", ReasonType}, {"1e3", "USD", ReasonType}, {"", "USD", ReasonType}} {
		got, err := OracleThreshold(tr, c.amount, c.currency)
		if err != nil || got.Outcome != OutcomeInputRejected || got.Reason != c.reason {
			t.Errorf("%q %q: %v %v", c.amount, c.currency, got, err)
		}
	}
	// Amounts beyond float precision still compare exactly.
	big := ApprovalThreshold{Currency: "USD", Op: "GT", Amount: "999999999999999.99", ApproverRoles: []string{"admin"}}
	if got, _ := OracleThreshold(big, "1000000000000000.00", "USD"); got.Outcome != OutcomeApprovalRequired {
		t.Fatal("large amount")
	}
	if got, _ := OracleThreshold(big, "999999999999999.99", "USD"); got.Outcome != OutcomeNoApproval {
		t.Fatal("large boundary")
	}
	if _, err := OracleThreshold(ApprovalThreshold{Currency: "USD", Op: "BETWEEN", Amount: "1"}, "1", "USD"); err == nil {
		t.Fatal("unknown operator evaluated")
	}
}

func TestDerivedProbes(t *testing.T) {
	probes, err := DeriveProbes("po-approval", ApprovalThreshold{Currency: "USD", Op: "GT", Amount: "500.00", ApproverRoles: []string{"admin"}})
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, p := range probes {
		got[strings.TrimPrefix(p.ID, "po-approval/probe/")] = fmt.Sprintf("%s %s %s", p.Amount, p.Currency, p.Expect)
	}
	want := map[string]string{
		"below": "499.99 USD NO_APPROVAL", "at": "500.00 USD NO_APPROVAL", "above": "500.01 USD APPROVAL_REQUIRED[admin]",
		"zero": "0.00 USD NO_APPROVAL", "negative": "-0.01 USD NO_APPROVAL", "currency": "500.00 EUR INPUT_REJECTED(CURRENCY)",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("%v", got)
	}
	again, _ := DeriveProbes("po-approval", ApprovalThreshold{Currency: "USD", Op: "GT", Amount: "500.00", ApproverRoles: []string{"admin"}})
	if !reflect.DeepEqual(probes, again) {
		t.Fatal("nondeterministic")
	}
	// GTE moves the expectation at the boundary.
	gte, _ := DeriveProbes("r", ApprovalThreshold{Currency: "USD", Op: "GTE", Amount: "500.00", ApproverRoles: []string{"admin"}})
	for _, p := range gte {
		if strings.HasSuffix(p.ID, "/at") && p.Expect.Outcome != OutcomeApprovalRequired {
			t.Fatal("GTE boundary")
		}
	}
	// Threshold zero collapses duplicate inputs deterministically.
	zero, _ := DeriveProbes("r", ApprovalThreshold{Currency: "USD", Op: "GT", Amount: "0", ApproverRoles: []string{"admin"}})
	ids := []string{}
	for _, p := range zero {
		ids = append(ids, strings.TrimPrefix(p.ID, "r/probe/"))
	}
	if !reflect.DeepEqual(ids, []string{"below", "at", "above", "currency"}) {
		t.Fatalf("%v", ids)
	}
	// Zero-decimal currency and the wrong-currency choice for EUR.
	jpy, _ := DeriveProbes("r", ApprovalThreshold{Currency: "JPY", Op: "GT", Amount: "500", ApproverRoles: []string{"admin"}})
	if jpy[0].Amount != "499" || jpy[2].Amount != "501" || jpy[4].Amount != "-1" {
		t.Fatalf("%+v", jpy)
	}
	eur, _ := DeriveProbes("r", ApprovalThreshold{Currency: "EUR", Op: "GT", Amount: "10.00", ApproverRoles: []string{"admin"}})
	if eur[len(eur)-1].Currency != "USD" {
		t.Fatal("wrong currency probe must differ from the threshold currency")
	}
	if _, err = DeriveProbes("r", ApprovalThreshold{Currency: "XXX", Amount: "1"}); err == nil {
		t.Fatal("unknown currency probed")
	}
}

func spec(set RequirementSet, cases ...AcceptanceCase) AcceptanceSpec {
	return AcceptanceSpec{SchemaVersion: AcceptanceSpecVersion, RequirementsHash: set.Hash(), Cases: cases}
}

func money(amount, currency string) map[string]json.RawMessage {
	return map[string]json.RawMessage{"field.order.amount": json.RawMessage(fmt.Sprintf(`{"amount":%q,"currency":%q}`, amount, currency))}
}

func TestAcceptanceSpecParseAndIndependence(t *testing.T) {
	set := thresholdSet("GT", "500.00", "USD")
	s := spec(set, AcceptanceCase{ID: "c1", RequirementID: "po-approval", Given: money("501.00", "USD"), Expect: Expectation{Outcome: OutcomeApprovalRequired, Roles: []string{"admin"}}})
	canonical, err := s.Canonical()
	if err != nil {
		t.Fatal(err)
	}
	parsed, again, hash, err := ParseAcceptanceSpec(canonical)
	if err != nil || string(again) != string(canonical) || hash != s.Hash() || len(parsed.Cases) != 1 {
		t.Fatal("round trip", err)
	}
	text := string(canonical)
	for name, raw := range map[string]string{
		"ir hash":           strings.Replace(text, `"schema_version"`, `"ir_hash":"`+strings.Repeat("a", 64)+`","schema_version"`, 1),
		"source hash":       strings.Replace(text, `"schema_version"`, `"source_hash":"`+strings.Repeat("a", 64)+`","schema_version"`, 1),
		"compiler version":  strings.Replace(text, `"schema_version"`, `"compiler_version":"1.0.0","schema_version"`, 1),
		"artifact hash":     strings.Replace(text, `"schema_version"`, `"artifact_hash":"x","schema_version"`, 1),
		"metadata revision": strings.Replace(text, `"schema_version"`, `"metadata_revision":1,"schema_version"`, 1),
		"duplicate key":     strings.Replace(text, `"cases"`, `"cases":[],"cases"`, 1),
		"cased key":         strings.Replace(text, `"cases"`, `"Cases"`, 1),
		"bad hash":          strings.Replace(text, set.Hash(), "xyz", 1),
		"unknown outcome":   strings.Replace(text, `APPROVAL_REQUIRED`, `MAYBE`, 1),
		"nested object":     strings.Replace(text, `"given":{"field.order.amount":{"amount":"501.00","currency":"USD"}}`, `"given":{"field.order.amount":{"amount":"501.00","currency":"USD","extra":1}}`, 1),
	} {
		if _, _, _, err := ParseAcceptanceSpec([]byte(raw)); err == nil {
			t.Errorf("%s accepted", name)
		}
	}
	if _, _, _, err := ParseAcceptanceSpec([]byte(`{"schema_version":"acceptance-v2","requirements_hash":"` + set.Hash() + `","cases":null}`)); err == nil {
		t.Error("null cases accepted")
	}
	big := spec(set)
	for i := 0; i <= MaxAcceptanceCases; i++ {
		big.Cases = append(big.Cases, AcceptanceCase{ID: fmt.Sprintf("c%d", i), RequirementID: "po-approval", Expect: Expectation{Outcome: OutcomeHolds}})
	}
	if code(big.Validate()) != CodeTooLarge {
		t.Error("1001 cases accepted")
	}
}

func TestValidateAcceptance(t *testing.T) {
	set := thresholdSet("GT", "500.00", "USD")
	ok := AcceptanceCase{ID: "c1", RequirementID: "po-approval", Given: money("501.00", "USD"), Expect: Expectation{Outcome: OutcomeApprovalRequired, Roles: []string{"admin"}}}
	if err := ValidateAcceptance(spec(set, ok), set); err != nil {
		t.Fatal(err)
	}
	cases := map[string]struct {
		s    AcceptanceSpec
		want string
	}{
		"human contradicts oracle on currency":  {spec(set, AcceptanceCase{ID: "c1", RequirementID: "po-approval", Given: money("500001", "INR"), Expect: Expectation{Outcome: OutcomeApprovalRequired, Roles: []string{"admin"}}}), CodeOracleDisagrees},
		"human contradicts oracle on boundary":  {spec(set, AcceptanceCase{ID: "c1", RequirementID: "po-approval", Given: money("500.00", "USD"), Expect: Expectation{Outcome: OutcomeApprovalRequired, Roles: []string{"admin"}}}), CodeOracleDisagrees},
		"human names the wrong role":            {spec(set, AcceptanceCase{ID: "c1", RequirementID: "po-approval", Given: money("501.00", "USD"), Expect: Expectation{Outcome: OutcomeApprovalRequired, Roles: []string{"member"}}}), CodeOracleDisagrees},
		"human says no approval above":          {spec(set, AcceptanceCase{ID: "c1", RequirementID: "po-approval", Given: money("501.00", "USD"), Expect: Expectation{Outcome: OutcomeNoApproval}}), CodeOracleDisagrees},
		"unknown requirement":                   {spec(set, AcceptanceCase{ID: "c1", RequirementID: "nope", Expect: Expectation{Outcome: OutcomeHolds}}), CodeUnknownReq},
		"threshold case without the money":      {spec(set, AcceptanceCase{ID: "c1", RequirementID: "po-approval", Expect: Expectation{Outcome: OutcomeNoApproval}}), CodeInvalidCase},
		"threshold case claiming HOLDS":         {spec(set, AcceptanceCase{ID: "c1", RequirementID: "po-approval", Given: money("501.00", "USD"), Expect: Expectation{Outcome: OutcomeHolds}}), CodeInvalidCase},
		"no cases leaves a mandatory uncovered": {spec(set), CodeMissingCoverage},
		"bound to another requirement set":      {AcceptanceSpec{SchemaVersion: AcceptanceSpecVersion, RequirementsHash: strings.Repeat("0", 64), Cases: []AcceptanceCase{ok}}, CodeHashMismatch},
	}
	for name, c := range cases {
		if got := code(ValidateAcceptance(c.s, set)); got != c.want {
			t.Errorf("%s: %q want %s", name, got, c.want)
		}
	}
	// Static requirements are asserted with HOLDS.
	static := RequirementSet{SchemaVersion: RequirementSetVersion, AppID: "app.orders", Requirements: []RequirementV2{
		req("gate", CategoryApprovalGate, json.RawMessage(`{"workflow_id":"workflow.order.approve","approver_roles":["admin"]}`))}}
	if err := ValidateAcceptance(spec(static, AcceptanceCase{ID: "g1", RequirementID: "gate", Expect: Expectation{Outcome: OutcomeHolds}}), static); err != nil {
		t.Fatal(err)
	}
	if code(ValidateAcceptance(spec(static, AcceptanceCase{ID: "g1", RequirementID: "gate", Expect: Expectation{Outcome: OutcomeNoApproval}}), static)) != CodeInvalidCase {
		t.Fatal("static requirement asserted with an approval outcome")
	}
	if code(ValidateAcceptance(spec(static, AcceptanceCase{ID: "g1", RequirementID: "gate", Given: money("1", "USD"), Expect: Expectation{Outcome: OutcomeHolds}}), static)) != CodeInvalidCase {
		t.Fatal("static requirement with given values")
	}
	// A non-mandatory requirement needs no case.
	optional := static
	optional.Requirements = []RequirementV2{static.Requirements[0]}
	optional.Requirements[0].Requiredness, optional.Requirements[0].RequirednessBasis = Optional, BasisOwnerDecision
	if err := ValidateAcceptance(spec(optional), optional); err != nil {
		t.Fatal(err)
	}
}

func TestExpectationEqualityIsOrderInsensitiveForRoles(t *testing.T) {
	a := Expectation{Outcome: OutcomeApprovalRequired, Roles: []string{"owner", "admin"}}
	b := Expectation{Outcome: OutcomeApprovalRequired, Roles: []string{"admin", "owner"}}
	if !a.Equal(b) || a.Equal(Expectation{Outcome: OutcomeApprovalRequired, Roles: []string{"admin"}}) || a.Equal(Expectation{Outcome: OutcomeNoApproval}) {
		t.Fatal("expectation equality")
	}
	if SandboxRank("BLOCK") >= SandboxRank("SIMULATE") || SandboxRank("TEST_ACCOUNT") != 4 || SandboxRank("LIVE") != -1 || SideEffectRank("WRITE") != 2 || SideEffectRank("DESTROY") != -1 {
		t.Fatal("rank tables")
	}
}
