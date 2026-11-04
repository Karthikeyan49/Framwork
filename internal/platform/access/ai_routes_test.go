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

	// Neighbours made from the registered paths themselves (independent review of the follow-up, FU-08: a list of 17 hand-picked paths cannot see an extra
	// alternative, such as "proposals-x", that is not on the list): every segment of every registered POST path with a character added before or after it, with its
	// last character taken away, or in upper case, and the path with a segment added, doubled or dropped. None may be accepted, unless it is a registered path.
	registered := map[string]bool{}
	var literals [][]string
	for pattern := range aiPatterns {
		if method, path, _ := strings.Cut(pattern, " "); method == "POST" {
			registered[fill(path)] = true
			literals = append(literals, strings.Split(path, "/"))
		}
	}
	neighbours := 0
	for _, segments := range literals {
		join := func(parts []string) string {
			filled := make([]string, len(parts))
			for i, part := range parts {
				filled[i] = fill(part)
			}
			return strings.Join(filled, "/")
		}
		for i := 1; i < len(segments); i++ {
			value := fill(segments[i])
			variants := []string{value + "x", value + "-x", value + "_x", value + ".x", "x" + value, "-" + value, strings.ToUpper(value), value + "%2f", value + "\n", ""}
			if len(value) > 1 {
				variants = append(variants, value[:len(value)-1])
			}
			if segments[i] == "{app}" {
				// An application id is free text of a few characters: "app.orders_1x" is another application, not a neighbour. Only what no id can be.
				variants = []string{strings.ToUpper(value), value + "%2f", value + "\n", ""}
			}
			for _, variant := range variants {
				parts := append([]string{}, segments...)
				parts[i] = variant
				candidate := strings.NewReplacer("{tenant}", tenantID, "{app}", app, "{id}", proposalID).Replace(strings.Join(parts[:i], "/")) + "/" + variant
				if i+1 < len(parts) {
					candidate += "/" + join(parts[i+1:])
				}
				prefix := strings.NewReplacer("{tenant}", tenantID, "{app}", app, "{id}", proposalID).Replace(strings.Join(parts[:i], "/")) + "/" + variant // the path cut after the segment
				for _, c := range []string{candidate, prefix} {
					neighbours++
					if !registered[c] && aiPOST.MatchString(c) {
						t.Errorf("the allowlist accepts a body for %q, which is next to a registered route", c)
					}
				}
			}
		}
		extra := join(segments)
		for _, candidate := range []string{extra + "/", extra + "/x", extra + "x", extra + "//", strings.Replace(extra, "/ai/", "/ai//", 1), strings.Replace(extra, "/ai/", "/ai/x/", 1)} {
			neighbours++
			if !registered[candidate] && aiPOST.MatchString(candidate) {
				t.Errorf("the allowlist accepts a body for %q, which is next to a registered route", candidate)
			}
		}
	}
	if neighbours < 150 {
		t.Errorf("only %d neighbours of the registered routes were tried", neighbours)
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
