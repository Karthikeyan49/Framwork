package promotion

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/Karthikeyan49/Framwork/internal/risk"
	"github.com/Karthikeyan49/Framwork/internal/tenant"
	"github.com/jackc/pgx/v5/pgconn"
)

const (
	tenantFixture  = "33333333-3333-3333-3333-333333333333"
	actorFixture   = "22222222-2222-2222-2222-222222222222"
	floorRequester = "aaaaaaaa-0000-0000-0000-000000000001"
	floorAuthor    = "aaaaaaaa-0000-0000-0000-000000000002"
	floorFreezer   = "aaaaaaaa-0000-0000-0000-000000000003"
	floorOutsider  = "aaaaaaaa-0000-0000-0000-000000000004"
	floorSecond    = "aaaaaaaa-0000-0000-0000-000000000005"
)

// noSQL fails the test if a wrapper reaches the database.
type noSQL struct{ t *testing.T }

func (n noSQL) ExecContext(context.Context, string, ...any) (sql.Result, error) {
	n.t.Fatal("argument validation reached SQL")
	return nil, nil
}
func (n noSQL) QueryContext(context.Context, string, ...any) (*sql.Rows, error) {
	n.t.Fatal("argument validation reached SQL")
	return nil, nil
}
func (n noSQL) QueryRowContext(context.Context, string, ...any) *sql.Row {
	n.t.Fatal("argument validation reached SQL")
	return nil
}

func TestRejectReasonsAndNotesAreClosedAndBounded(t *testing.T) {
	for _, r := range []RejectReason{ReasonRequirementsNotMet, ReasonRiskUnacceptable, ReasonEvidenceInsufficient, ReasonSuperseded, ReasonWithdrawn, ReasonOther} {
		if !r.Valid() {
			t.Fatal("reason rejected", r)
		}
	}
	for _, r := range []RejectReason{"", "withdrawn", "WITHDRAWN ", "APPROVED", "OTHER;DROP"} {
		if r.Valid() {
			t.Fatalf("reason %q accepted", r)
		}
	}
	for _, note := range []string{"", "plain note", strings.Repeat("é", MaxNoteRunes), strings.Repeat("x", MaxNoteRunes), "emoji 🚀 and ünïcode"} {
		if !ValidNote(note) {
			t.Fatalf("note %q rejected", note)
		}
	}
	for name, note := range map[string]string{
		"too long":        strings.Repeat("x", MaxNoteRunes+1),
		"too many runes":  strings.Repeat("é", MaxNoteRunes+1),
		"newline":         "line\nbreak",
		"tab":             "a\tb",
		"nul":             "a\x00b",
		"delete":          "a\x7fb",
		"c1 control":      "a\u0085b",
		"bidi override":   "safe‮evil",
		"bidi isolate":    "a⁦b",
		"line separator":  "a b",
		"invalid utf8":    "a\xffb",
		"escape sequence": "\x1b[31mred",
	} {
		if ValidNote(note) {
			t.Fatalf("%s accepted", name)
		}
	}
}

func TestArgumentShapesAreCheckedBeforeSQL(t *testing.T) {
	h1, h2, h3 := strings.Repeat("a", 64), strings.Repeat("b", 64), strings.Repeat("c", 64)
	if !validScopeOf(tenantFixture, actorFixture, "app.orders") || !validScopeOf(tenantFixture, actorFixture, "app.sales.orders_2") {
		t.Fatal("valid scope rejected")
	}
	for _, app := range []string{"", "orders", "app.", "app.Orders", "app.orders;", "entity.orders", "app." + strings.Repeat("a", 160)} {
		if validScopeOf(tenantFixture, actorFixture, app) {
			t.Fatalf("app %q accepted", app)
		}
	}
	if validScopeOf("", actorFixture, "app.orders") || validScopeOf(tenantFixture, "", "app.orders") || validScopeOf(strings.ToUpper(floorOutsider), actorFixture, "app.orders") || !validScopeOf(floorOutsider, actorFixture, "app.orders") {
		t.Fatal("unresolved or non-canonical scope accepted")
	}
	for _, tc := range []struct {
		hash, base string
		generation int64
		ok         bool
	}{
		{h1, "", 0, true}, {h1, h2, 1, true}, {h1, h2, MaxGeneration - 1, true},
		{h1, "", 1, false}, {h1, h2, 0, false}, {h1, h2, -1, false}, {h1, h2, MaxGeneration, false},
		{h1, "abc", 1, false}, {strings.ToUpper(h1), h2, 1, false}, {"", h2, 1, false}, {h1[:63], "", 0, false},
	} {
		if activationArgs(tc.hash, tc.base, tc.generation) != tc.ok {
			t.Fatalf("activation %q over %q@%d ok=%v", tc.hash, tc.base, tc.generation, !tc.ok)
		}
	}
	ack := RollbackPlan{StorageCompatible: true, ExternalEffectsReviewed: true, PlanHash: h3}
	if rollbackArgs(h1, h2, 1, ack) != nil || rollbackArgs(h1, h2, 7, RollbackPlan{}) != nil {
		t.Fatal("valid rollback rejected")
	}
	for name, err := range map[string]error{
		"same target":         rollbackArgs(h1, h1, 3, ack),
		"no current":          rollbackArgs(h1, "", 0, ack),
		"zero generation":     rollbackArgs(h1, h2, 0, ack),
		"generation overflow": rollbackArgs(h1, h2, MaxGeneration, ack),
		"bad target":          rollbackArgs("x", h2, 1, ack),
		"bad plan hash":       rollbackArgs(h1, h2, 1, RollbackPlan{StorageCompatible: true, ExternalEffectsReviewed: true, PlanHash: "plan"}),
	} {
		if !errors.Is(err, ErrInvalid) || !errors.Is(err, ErrGate) || errors.Is(err, ErrRecoveryRequired) {
			t.Fatalf("%s: %v", name, err)
		}
	}
	recovery := ack
	recovery.RecoveryRequired = true
	if err := rollbackArgs(h1, h2, 1, recovery); !errors.Is(err, ErrRecoveryRequired) || !errors.Is(err, ErrInvalid) {
		t.Fatal("plan requiring recovery accepted", err)
	}
	for _, tc := range []struct {
		plan RollbackPlan
		ack  bool
	}{
		{ack, true}, {recovery, false}, {RollbackPlan{}, false},
		{RollbackPlan{StorageCompatible: true, PlanHash: h3}, false}, {RollbackPlan{ExternalEffectsReviewed: true, PlanHash: h3}, false},
	} {
		if tc.plan.Acknowledged() != tc.ack {
			t.Fatalf("plan %+v acknowledged=%v", tc.plan, !tc.ack)
		}
	}
	// Without a resolved tenant and actor no wrapper touches SQL.
	ctx, q, none, st := context.Background(), noSQL{t}, tenant.Access{}, Store{}
	_, e1 := st.ApproveRelease(ctx, q, none, "app.orders", h1)
	_, e2 := st.RejectRelease(ctx, q, none, "app.orders", h1, ReasonOther, "")
	_, e3 := st.ActivateRelease(ctx, q, none, "app.orders", h1, "", 0)
	_, e4 := st.RollbackRelease(ctx, q, none, "app.orders", h1, h2, 1, ack)
	_, e5 := st.Approvals(ctx, q, none, "app.orders", h1)
	_, e6 := st.Rejections(ctx, q, none, "app.orders", h1)
	_, e7 := st.History(ctx, q, none, "app.orders", 10)
	_, e8 := st.WasActivated(ctx, q, none, "app.orders", h1)
	for i, err := range []error{e1, e2, e3, e4, e5, e6, e7, e8} {
		if !errors.Is(err, ErrInvalid) {
			t.Fatalf("call %d: %v", i+1, err)
		}
	}
}

func TestPortErrorsAreTypedWithoutHidingTheSQLState(t *testing.T) {
	for code, kind := range map[string]error{"P0002": ErrNotFound, "55000": ErrConflict, "40001": ErrConflict, "40P01": ErrConflict, "55P03": ErrConflict, "23505": ErrConflict, "23503": ErrConflict, "42501": ErrForbidden, "22023": ErrInvalid} {
		cause := &pgconn.PgError{Code: code, Message: "secret detail of " + code}
		err := classify(fmt.Errorf("wrapped: %w", cause))
		var pg *pgconn.PgError
		if !errors.Is(err, kind) || !errors.As(err, &pg) || pg.Code != code {
			t.Fatalf("%s: %v", code, err)
		}
		if strings.Contains(err.Error(), "secret") {
			t.Fatalf("%s: driver text in %q", code, err.Error())
		}
		for _, other := range []error{ErrNotFound, ErrConflict, ErrForbidden, ErrInvalid} {
			if other != kind && errors.Is(err, other) {
				t.Fatalf("%s also matched %v", code, other)
			}
		}
		// Only invalid arguments read as the M12 ErrGate (400) mapping.
		if errors.Is(err, ErrGate) != (kind == ErrInvalid) {
			t.Fatalf("%s: ErrGate=%v", code, errors.Is(err, ErrGate))
		}
	}
	other := &pgconn.PgError{Code: "XX000"}
	plain := errors.New("transport lost")
	if classify(other) != error(other) || classify(plain) != plain || classify(nil) != nil {
		t.Fatal("unclassified errors were rewritten")
	}
}

func TestDeriveStatusPrecedence(t *testing.T) {
	for _, tc := range []struct {
		rejections, approvals, required int
		active, ever                    bool
		want                            Status
	}{
		{0, 0, 1, false, false, StatusStaged},
		{0, 1, 2, false, false, StatusStaged},
		{0, 1, 1, false, false, StatusApproved},
		{0, 3, 2, false, false, StatusApproved},
		{0, 5, 0, false, false, StatusStaged},
		{0, 1, -1, false, false, StatusStaged},
		{0, 1, 1, false, true, StatusSuperseded},
		{0, 0, 1, false, true, StatusSuperseded},
		{0, 1, 1, true, true, StatusActive},
		{0, 0, 2, true, false, StatusActive},
		{1, 1, 1, true, true, StatusRejected},
		{1, 0, 1, false, false, StatusRejected},
		{2, 2, 2, false, true, StatusRejected},
	} {
		if got := DeriveStatus(tc.rejections, tc.approvals, tc.required, tc.active, tc.ever); got != tc.want {
			t.Fatalf("%+v: got %s", tc, got)
		}
	}
}

func TestDatabaseFloorsMirrorAndNeverExceedThePolicy(t *testing.T) {
	parties := risk.Parties{Requester: floorRequester, AcceptanceAuthor: floorAuthor, SandboxFreezer: floorFreezer}
	for level, want := range map[string]struct {
		required int
		roles    []string
		excluded []string
	}{
		"LOW":      {1, []string{}, []string{}},
		"MEDIUM":   {1, []string{}, []string{}},
		"HIGH":     {1, []string{risk.RoleRequester, risk.RoleAcceptanceAuthor}, []string{floorRequester, floorAuthor}},
		"CRITICAL": {2, []string{risk.RoleRequester, risk.RoleAcceptanceAuthor, risk.RoleSandboxFreezer}, []string{floorRequester, floorAuthor, floorFreezer}},
		"":         {2, []string{risk.RoleRequester, risk.RoleAcceptanceAuthor, risk.RoleSandboxFreezer}, []string{floorRequester, floorAuthor, floorFreezer}},
		"BLOCKED":  {2, []string{risk.RoleRequester, risk.RoleAcceptanceAuthor, risk.RoleSandboxFreezer}, []string{floorRequester, floorAuthor, floorFreezer}},
	} {
		if RequiredApprovals(level) != want.required || !slices.Equal(FloorRoles(level), want.roles) || !slices.Equal(FloorExcluded(level, parties), want.excluded) {
			t.Fatalf("%q: required=%d roles=%v excluded=%v", level, RequiredApprovals(level), FloorRoles(level), FloorExcluded(level, parties))
		}
	}
	// The floor is what a compromised API cannot go below; the shipped policy
	// may be stricter but never weaker (Go applies it with CheckSeparation).
	for _, rule := range risk.Latest().Spec().Separation {
		if rule.Approvals < RequiredApprovals(rule.Level) {
			t.Fatalf("%s: policy quorum %d below the database floor", rule.Level, rule.Approvals)
		}
		for _, role := range FloorRoles(rule.Level) {
			if !slices.Contains(rule.Excluded, role) {
				t.Fatalf("%s: policy does not exclude floor role %s", rule.Level, role)
			}
		}
	}
	for _, tc := range []struct {
		level, actor, role string
		admitted           bool
	}{
		{"LOW", floorRequester, "", true},
		{"MEDIUM", floorAuthor, "", true},
		{"HIGH", floorRequester, risk.RoleRequester, false},
		{"HIGH", floorAuthor, risk.RoleAcceptanceAuthor, false},
		{"HIGH", floorFreezer, "", true},
		{"HIGH", floorOutsider, "", true},
		{"CRITICAL", floorFreezer, risk.RoleSandboxFreezer, false},
		{"CRITICAL", floorOutsider, "", true},
		{"LOW", "not-an-id", "", false},
		{"LOW", strings.ToUpper(floorOutsider), "", false},
	} {
		role, ok := FloorExclusion(tc.level, parties, tc.actor)
		if role != tc.role || ok != tc.admitted {
			t.Fatalf("%s/%s: role=%q admitted=%v", tc.level, tc.actor, role, ok)
		}
	}
	// Party ids compare case-insensitively; an unknown party excludes everyone.
	upper := risk.Parties{Requester: strings.ToUpper(floorRequester), AcceptanceAuthor: floorAuthor, SandboxFreezer: floorFreezer}
	if _, ok := FloorExclusion("HIGH", upper, floorRequester); ok {
		t.Fatal("case variant of the floorRequester admitted")
	}
	if role, ok := FloorExclusion("CRITICAL", risk.Parties{Requester: floorRequester, AcceptanceAuthor: floorAuthor}, floorOutsider); ok || role != risk.RoleSandboxFreezer {
		t.Fatal("missing floorFreezer id did not fail closed", role)
	}
	if _, ok := FloorExclusion("LOW", risk.Parties{}, floorOutsider); !ok {
		t.Fatal("LOW floor needs no parties")
	}
	approvers := []string{floorRequester, floorAuthor, floorFreezer, floorOutsider, floorOutsider, floorSecond, "bogus"}
	for level, want := range map[string]int{"LOW": 5, "MEDIUM": 5, "HIGH": 3, "CRITICAL": 2} {
		if got := CountEligible(level, parties, approvers); got != want {
			t.Fatalf("%s: eligible=%d want %d", level, got, want)
		}
	}
	if CountEligible("CRITICAL", parties, []string{floorOutsider}) >= RequiredApprovals("CRITICAL") {
		t.Fatal("one floorOutsider met the CRITICAL quorum")
	}
	// The pure floor agrees with risk.CheckSeparation for the floor requirement.
	for _, level := range []string{"LOW", "MEDIUM", "HIGH", "CRITICAL"} {
		sep := risk.Separation{Approvals: RequiredApprovals(level), Excluded: FloorRoles(level)}
		for _, set := range [][]string{{floorOutsider}, {floorOutsider, floorSecond}, {floorRequester, floorOutsider}, {floorFreezer, floorOutsider, floorSecond}, {floorAuthor}} {
			eligible := CountEligible(level, parties, set) == len(set) && len(set) >= sep.Approvals
			if (risk.CheckSeparation(sep, parties, set) == nil) != eligible {
				t.Fatalf("%s %v: floor and CheckSeparation disagree", level, set)
			}
		}
	}
}

func TestRollbackClassGate(t *testing.T) {
	if RollbackClassNeedsPlan(risk.RollbackPointerSafe) {
		t.Fatal("pointer-safe rollback demanded a plan")
	}
	for _, class := range []string{risk.RollbackDataNotReversible, risk.RollbackExternalEffects, "", "pointer_safe", "POINTER_SAFE ", "UNKNOWN"} {
		if !RollbackClassNeedsPlan(class) {
			t.Fatalf("class %q skipped the plan", class)
		}
	}
}
