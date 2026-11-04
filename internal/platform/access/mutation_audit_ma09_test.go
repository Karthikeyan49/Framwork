package access

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Karthikeyan49/Framwork/internal/promotion"
	"github.com/Karthikeyan49/Framwork/internal/risk"
)

func TestAuthorityQuorumExcludedRolesBarredActorsAndEligibleApprovers(t *testing.T) {
	const requester, author, freezer, other, second = "aaaaaaaa-0000-4000-8000-000000000001", "aaaaaaaa-0000-4000-8000-000000000002", "aaaaaaaa-0000-4000-8000-000000000003", "aaaaaaaa-0000-4000-8000-000000000004", "aaaaaaaa-0000-4000-8000-000000000005"
	parties := risk.Parties{Requester: requester, AcceptanceAuthor: author, SandboxFreezer: freezer}
	high := authority{level: "HIGH", separation: risk.Separation{Approvals: 1, Excluded: []string{risk.RoleRequester, risk.RoleAcceptanceAuthor}}, parties: parties}
	critical := authority{level: "CRITICAL", separation: risk.Separation{Approvals: 2, Excluded: []string{risk.RoleRequester, risk.RoleAcceptanceAuthor, risk.RoleSandboxFreezer}}, parties: parties}
	stricter := authority{level: "LOW", separation: risk.Separation{Approvals: 3, Excluded: []string{risk.RoleSandboxFreezer}}, parties: parties}
	if high.required() != 1 || critical.required() != 2 || stricter.required() != 3 || (authority{level: "LOW", separation: risk.Separation{Approvals: 1}}).required() != 1 {
		t.Fatal("required quorum", high.required(), critical.required(), stricter.required())
	}
	if got := stricter.excluded(); !reflect.DeepEqual(got, []string{risk.RoleSandboxFreezer}) {
		t.Fatalf("a bundle that excludes the freezer at LOW: %v", got)
	}
	if got := high.excluded(); !reflect.DeepEqual(got, []string{risk.RoleRequester, risk.RoleAcceptanceAuthor}) {
		t.Fatalf("HIGH excludes %v", got)
	}
	for _, tc := range []struct {
		au     authority
		actor  string
		role   string
		barred bool
	}{
		{high, requester, risk.RoleRequester, true}, {high, author, risk.RoleAcceptanceAuthor, true}, {high, freezer, "", false}, {high, other, "", false},
		{critical, freezer, risk.RoleSandboxFreezer, true}, {critical, other, "", false}, {high, strings.ToUpper(requester), "", true}, {high, "not-an-id", "", true},
		{authority{level: "HIGH", separation: high.separation, parties: risk.Parties{Requester: requester}}, other, risk.RoleAcceptanceAuthor, true}, // an unknown party bars everyone
	} {
		if role, barred := tc.au.bars(tc.actor); role != tc.role || barred != tc.barred {
			t.Errorf("bars(%s) = %q, %v; want %q, %v", tc.actor, role, barred, tc.role, tc.barred)
		}
	}
	approvals := []promotion.Decision{{ActorID: other, At: time.Unix(1, 0)}, {ActorID: requester}, {ActorID: strings.ToUpper(other)}, {ActorID: second}, {ActorID: other}, {ActorID: author}}
	if got := high.eligible(approvals); !reflect.DeepEqual(got, []string{other, second}) {
		t.Fatalf("eligible approvers %v", got)
	}
}
