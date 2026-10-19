//go:build integration

package integration

import (
	"strings"
	"testing"
)

// Strictness of every AI body and of the unconfigured deployment (ADR104 A7).

// TestM14APIBodiesAreStrict feeds every POST route the bodies a hostile caller would
// try. Nothing may be stored, called or changed, and every refusal is a 4xx.
func TestM14APIBodiesAreStrict(t *testing.T) {
	e := m14Setup(t, m14Options{})
	e.draft("500")
	ready := e.readyPatch(1, "entity.order", "Purchase order")
	callsBefore := e.provider.Calls()
	rowsBefore := e.proposalRows()

	deep := strings.Repeat(`{"a":`, 40) + "1" + strings.Repeat("}", 40)
	creates := []struct{ name, body string }{
		{"empty body", ``},
		{"not json", `request=hello`},
		{"array", `["hello"]`},
		{"null", `null`},
		{"string", `"hello"`},
		{"no request", `{}`},
		{"unknown key", `{"request":"hello","extra":1}`},
		{"server-derived status", `{"request":"hello","status":"READY"}`},
		{"server-derived risk", `{"request":"hello","risk":"LOW"}`},
		{"server-derived requester", `{"request":"hello","requester_id":"00000000-0000-0000-0000-000000000000"}`},
		{"a proposal supplied by the caller", `{"request":"hello","proposal":{"version":"ai-patch-proposal-v1"}}`},
		{"a tenant supplied by the caller", `{"request":"hello","tenant_id":"00000000-0000-0000-0000-000000000000"}`},
		{"duplicate key", `{"request":"a","request":"b"}`},
		{"case-folded key", `{"Request":"hello"}`},
		{"upper-case key", `{"REQUEST":"hello"}`},
		{"unicode-folded duplicate", "{\"request\":\"a\",\"requeſt\":\"b\"}"},
		{"trailing data", `{"request":"hello"} {}`},
		{"depth bomb", `{"request":"hello","expected_revision":1,"x":` + deep + `}`},
		{"request is a number", `{"request":5}`},
		{"request is null", `{"request":null}`},
		{"empty request", `{"request":""}`},
		{"whitespace request", `{"request":" \n\t "}`},
		{"4001 characters", `{"request":"` + strings.Repeat("a", 4001) + `"}`},
		{"NUL", `{"request":"a\u0000b"}`},
		{"escape control", `{"request":"a\u001bb"}`},
		{"bidi override", `{"request":"a‮b"}`},
		{"line separator", `{"request":"a b"}`},
		{"zero width space", `{"request":"a​b"}`},
		{"expected_revision zero", `{"request":"hello","expected_revision":0}`},
		{"expected_revision negative", `{"request":"hello","expected_revision":-1}`},
		{"expected_revision huge", `{"request":"hello","expected_revision":10000000000}`},
		{"expected_revision fraction", `{"request":"hello","expected_revision":1.5}`},
		{"expected_revision string", `{"request":"hello","expected_revision":"1"}`},
		{"expected_revision null", `{"request":"hello","expected_revision":null,"x":1}`},
	}
	for _, route := range []string{"/patch-proposals", "/requirement-proposals"} {
		for _, c := range creates {
			w := e.ai("POST", route, e.alice, c.body)
			if w.Code != 400 || m14Code(w) != "invalid_ai_request" {
				t.Errorf("%s %s: %d %s", route, c.name, w.Code, w.Body.String())
			}
		}
		if w := e.ai("POST", route, e.alice, `{"request":"`+strings.Repeat("a", aiCreateCap)+`"}`); w.Code != 413 || m14Code(w) != "body_too_large" {
			t.Errorf("%s oversized: %d %s", route, w.Code, w.Body.String())
		}
	}
	// A request of exactly 4000 characters, and one with a tab and newlines, are fine as
	// far as the body goes (they reach the provider, which has no answer: FAILED, 503).
	e.provider.Push(step([]byte("{}")), step([]byte("{}")), step([]byte("{}")))
	if w := e.propose(e.alice, strings.Repeat("a", 4000)); w.Code == 400 || w.Code == 413 {
		t.Errorf("exactly 4000 characters refused: %d %s", w.Code, w.Body.String())
	}
	if w := e.propose(e.alice, "line one\n\tline two\r\n"); w.Code == 400 || w.Code == 413 {
		t.Errorf("tab and newlines refused: %d %s", w.Code, w.Body.String())
	}
	rowsBefore += 2
	callsBefore = e.provider.Calls()

	applies := []struct{ name, body string }{
		{"empty", ``},
		{"no acknowledge", `{"expected_revision":1}`},
		{"no expected_revision", `{"acknowledge":[]}`},
		{"null acknowledge", `{"expected_revision":1,"acknowledge":null}`},
		{"acknowledge not a list", `{"expected_revision":1,"acknowledge":"x"}`},
		{"acknowledge of numbers", `{"expected_revision":1,"acknowledge":[1]}`},
		{"unknown key", `{"expected_revision":1,"acknowledge":[],"force":true}`},
		{"result supplied", `{"expected_revision":1,"acknowledge":[],"result_revision":2}`},
		{"status supplied", `{"expected_revision":1,"acknowledge":[],"status":"APPLIED"}`},
		{"actor supplied", `{"expected_revision":1,"acknowledge":[],"actor_id":"x"}`},
		{"duplicate key", `{"expected_revision":1,"expected_revision":1,"acknowledge":[]}`},
		{"case-folded key", `{"Expected_Revision":1,"acknowledge":[]}`},
		{"expected_revision zero", `{"expected_revision":0,"acknowledge":[]}`},
		{"expected_revision fraction", `{"expected_revision":1.5,"acknowledge":[]}`},
		{"expected_revision huge", `{"expected_revision":1000000001,"acknowledge":[]}`},
		{"513 acknowledgements", `{"expected_revision":1,"acknowledge":[` + strings.TrimSuffix(strings.Repeat(`"a:b",`, 513), ",") + `]}`},
		{"trailing data", `{"expected_revision":1,"acknowledge":[]}x`},
	}
	for _, c := range applies {
		w := e.ai("POST", "/proposals/"+ready.ID+"/apply", e.alice, c.body)
		if w.Code != 400 || m14Code(w) != "invalid_ai_request" {
			t.Errorf("apply %s: %d %s", c.name, w.Code, w.Body.String())
		}
	}
	if w := e.ai("POST", "/proposals/"+ready.ID+"/apply", e.alice, `{"expected_revision":1,"acknowledge":[`+strings.Repeat(`"`+strings.Repeat("a", 1000)+`",`, 200)+`"x"]}`); w.Code != 413 {
		t.Errorf("apply over its cap: %d", w.Code)
	}

	confirms := []struct{ name, body string }{
		{"no decisions", `{"expected_requirements_version":0}`},
		{"null decisions", `{"expected_requirements_version":0,"decisions":null}`},
		{"decision with a status", `{"expected_requirements_version":0,"decisions":[{"id":"a","decision":"RESOLVE","status":"RESOLVED"}]}`},
		{"decision with requiredness", `{"expected_requirements_version":0,"decisions":[{"id":"a","decision":"RESOLVE","requiredness":"OPTIONAL"}]}`},
		{"decision with provenance", `{"expected_requirements_version":0,"decisions":[{"id":"a","decision":"RESOLVE","provenance":"AUTHENTICATED_HUMAN"}]}`},
		{"negative version", `{"expected_requirements_version":-1,"decisions":[]}`},
		{"huge version", `{"expected_requirements_version":1000000,"decisions":[]}`},
		{"unknown key", `{"expected_requirements_version":0,"decisions":[],"x":1}`},
		{"51 decisions", `{"expected_requirements_version":0,"decisions":[` + strings.TrimSuffix(strings.Repeat(`{"id":"a","decision":"RESOLVE"},`, 51), ",") + `]}`},
	}
	for _, c := range confirms {
		w := e.ai("POST", "/proposals/"+ready.ID+"/confirm", e.alice, c.body)
		if w.Code != 400 || m14Code(w) != "invalid_ai_request" {
			t.Errorf("confirm %s: %d %s", c.name, w.Code, w.Body.String())
		}
	}

	rejects := []struct{ name, body string }{
		{"no reason", `{}`}, {"empty reason", `{"reason":""}`}, {"unknown reason", `{"reason":"BECAUSE"}`}, {"lower case", `{"reason":"not_wanted"}`},
		{"note supplied", `{"reason":"OTHER","note":"free text"}`}, {"number", `{"reason":5}`}, {"duplicate", `{"reason":"OTHER","reason":"OTHER"}`},
		{"case-folded", `{"Reason":"OTHER"}`}, {"oversized", `{"reason":"OTHER","x":"` + strings.Repeat("a", 5000) + `"}`},
	}
	for _, c := range rejects {
		w := e.ai("POST", "/proposals/"+ready.ID+"/reject", e.alice, c.body)
		if w.Code != 400 && w.Code != 413 {
			t.Errorf("reject %s: %d %s", c.name, w.Code, w.Body.String())
		}
	}

	// Wrong content types and identifiers.
	for name, ct := range map[string]string{"text/plain": "text/plain", "form": "application/x-www-form-urlencoded", "json with charset junk": "application/json; charset=\""} {
		_ = name
		w := m14DoType(e.h, "POST", e.root+"/ai/proposals/"+ready.ID+"/reject", e.alice, ct, `{"reason":"OTHER"}`)
		if w.Code != 415 && w.Code != 400 {
			t.Errorf("content type %q: %d %s", ct, w.Code, w.Body.String())
		}
	}
	for _, id := range []string{"not-a-uuid", "00000000-0000-0000-0000-000000000000", strings.ToUpper(ready.ID), ready.ID + "0", "..", "%2e%2e", strings.Repeat("a", 36)} {
		for _, suffix := range []string{"", "/apply", "/confirm", "/reject"} {
			method := "POST"
			if suffix == "" {
				method = "GET"
			}
			var body any
			if method == "POST" {
				body = `{"reason":"OTHER"}`
			}
			// An identifier that is not a lower-case UUID is refused as an unknown
			// resource, or its body is refused by the POST allowlist.
			if w := e.ai(method, "/proposals/"+id+suffix, e.alice, body); w.Code != 404 && w.Code != 400 && w.Code != 405 && w.Code != 413 && !(id == ".." && w.Code == 307) { // the mux cleans a dot-dot segment with a redirect
				t.Errorf("%s id %q%s: %d %s", method, id, suffix, w.Code, w.Body.String())
			}
		}
	}

	if got := e.proposalRows(); got != rowsBefore {
		t.Errorf("hostile bodies stored %d proposals", got-rowsBefore)
	}
	if e.provider.Calls() != callsBefore {
		t.Errorf("hostile bodies reached the provider (%d calls)", e.provider.Calls()-callsBefore)
	}
	if rev, _ := e.heads(); rev != 1 {
		t.Errorf("hostile bodies moved the draft to %d", rev)
	}
	// The READY proposal is untouched by every refusal above.
	if p := e.proposal(e.ai("GET", "/proposals/"+ready.ID, e.alice, nil), 200); p.Status != "READY" {
		t.Errorf("proposal is %s", p.Status)
	}
}

const aiCreateCap = 32 << 10

// TestM14APIUnconfiguredAnswers503BeforeAnythingHappens: with no provider configured
// the routes exist (in preview), an authorized caller gets 503 ai_unavailable before
// any body is read or any row is written, a denied caller still gets 403/401, and no
// other route is affected.
func TestM14APIUnconfiguredAnswers503BeforeAnythingHappens(t *testing.T) {
	e := m14Setup(t, m14Options{noService: true})
	e.draft("500")
	id := "11111111-2222-4333-8444-555555555555"
	routes := []struct{ method, path string }{
		{"POST", "/patch-proposals"}, {"POST", "/requirement-proposals"}, {"GET", "/proposals"}, {"GET", "/proposals/" + id},
		{"POST", "/proposals/" + id + "/apply"}, {"POST", "/proposals/" + id + "/confirm"}, {"POST", "/proposals/" + id + "/reject"},
	}
	for _, r := range routes {
		bodies := []any{nil}
		if r.method == "POST" {
			bodies = []any{`{"request":"hello"}`, `{{{ not json`, ""}
		}
		for _, body := range bodies {
			w := e.ai(r.method, r.path, e.alice, body)
			if w.Code != 503 || m14Code(w) != "ai_unavailable" {
				t.Errorf("%s %s: %d %s", r.method, r.path, w.Code, w.Body.String())
			}
		}
		wantViewer := 503 // ai.read is allowed for a viewer; everything else is refused before the 503
		if r.method == "POST" {
			wantViewer = 403
		}
		if w := e.ai(r.method, r.path, e.viewer, bodyFor(r.method)); w.Code != wantViewer {
			t.Errorf("viewer %s %s: %d, want %d", r.method, r.path, w.Code, wantViewer)
		}
		if w := e.ai(r.method, r.path, "", bodyFor(r.method)); w.Code != 401 {
			t.Errorf("anonymous %s %s: %d", r.method, r.path, w.Code)
		}
	}
	if e.proposalRows() != 0 || e.provider.Calls() != 0 {
		t.Fatal("an unconfigured deployment wrote a row or called a provider")
	}
	// The rest of the platform is unchanged.
	if rev, _ := e.heads(); rev != 1 {
		t.Fatal("head", rev)
	}
	if w := m3Call(e.h, "GET", "/v1/tenants/"+e.f.tenantA+"/metadata/"+e.app+"/versions/1/compile", e.alice, 0, nil); w.Code != 200 {
		t.Fatalf("compile: %d %s", w.Code, w.Body.String())
	}
	if e.register(e.alice, "600", 1) != 2 {
		t.Fatal("a metadata append stopped working")
	}
	if e.auditCount("ai.propose.rejected", false) == 0 {
		t.Error("the 503 refusals are audited")
	}
}

func bodyFor(method string) any {
	if method == "POST" {
		return `{"request":"x"}`
	}
	return nil
}
