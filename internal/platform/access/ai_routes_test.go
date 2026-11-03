package access

import (
	"strings"
	"testing"
)

// RC-04 (independent route-contract audit): ai.go and ADR104 A7 say that the allowlist of POST paths that may carry a body (aiPOST)
// accepts exactly the POST patterns of the AI routes, and that a test compares them. No such test existed; the comparison held when the
// auditor probed it. These are the test: every registered POST pattern is accepted, nothing else that is near it is.

func TestAIPOSTAllowlistAcceptsExactlyTheRegisteredPOSTRoutes(t *testing.T) {
	const (
		tenantID   = "1111aaaa-2222-4333-8444-5555bbbbcccc"
		proposalID = "66666666-7777-4888-8999-aaaaaaaaaaaa"
		app        = "app.orders_1"
	)
	fill := func(path string) string {
		return strings.NewReplacer("{tenant}", tenantID, "{app}", app, "{id}", proposalID).Replace(path)
	}
	posts := 0
	for pattern := range aiPatterns {
		method, path, _ := strings.Cut(pattern, " ")
		accepted := aiPOST.MatchString(fill(path))
		switch method {
		case "POST":
			posts++
			if !accepted {
				t.Errorf("%s is a registered route and the allowlist refuses its body", pattern)
			}
		default:
			if accepted {
				t.Errorf("%s is not a POST route and the allowlist accepts the path for a body", pattern)
			}
		}
	}
	if posts != 5 {
		t.Errorf("%d POST routes, want the two creations and apply, confirm and reject", posts)
	}

	root := fill("/v1/tenants/{tenant}/preview/{app}/ai")
	for _, path := range []string{
		root + "/patch-proposals/",
		root + "/patch-proposals/x",
		root + "/patch-proposal",
		root + "/proposals",
		root + "/proposals/" + proposalID,
		root + "/proposals/" + proposalID + "/applyx",
		root + "/proposals/" + proposalID + "/apply/",
		root + "/proposals/" + proposalID + "/approve",
		root + "/proposals/" + strings.ToUpper(proposalID) + "/apply",
		root + "/proposals/not-a-uuid/apply",
		root + "/proposals//apply",
		root + "x/patch-proposals",
		fill("/v1/tenants/{tenant}/preview/{app}/entities/entity.order/records"),
		fill("/v1/tenants/{tenant}/metadata/{app}/versions"),
		"/v1/tenants/" + strings.ToUpper(tenantID) + "/preview/" + app + "/ai/patch-proposals",
		"/v1/tenants/" + tenantID + "/preview/App.orders/ai/patch-proposals",
		"/v1/tenants/" + tenantID + "/preview/" + app + "/ai/patch-proposals\n",
	} {
		if aiPOST.MatchString(path) {
			t.Errorf("the allowlist accepts a body for %q", path)
		}
	}
}
