package ai

import (
	"strings"
	"testing"

	"github.com/Karthikeyan49/Framwork/internal/compiler"
	"github.com/Karthikeyan49/Framwork/internal/intent"
)

// The independent red team deleted the redaction of one field at a time from the context builder and from the diagnostics that are fed
// back to the model, and no shipped test noticed for the application id, the tool fields, the requirement rows and the diagnostics
// (test adequacy, 37 of its 90 mutations survived). Every string below is a token that a prefix rule names, drawn at run time and
// shaped like an identifier, which is how an id or a reference carries one: it has to leave the context redacted wherever it is put.
func TestBuildContextRedactsEveryFieldThatReachesAPrompt(t *testing.T) {
	token := func(label string) (string, string) {
		body := strings.ToLower(alnum(label, 36))
		return "ghp_" + body, body
	}
	check := func(t *testing.T, field, text, body string) {
		t.Helper()
		// A field is clipped before it is redacted, so only the head of the token is certain to be there.
		if strings.Contains(text, body[:16]) {
			t.Errorf("%s reaches the prompt unredacted", field)
		}
	}
	t.Run("patch", func(t *testing.T) {
		doc := fixtureDoc(t, exWorkflow)
		app, appBody := token("surf/app")
		ns, nsBody := token("surf/ns")
		id, idBody := token("surf/id")
		kind, kindBody := token("surf/kind")
		doc.AppID, doc.Namespace = app, ns
		doc.Definitions[0].ID, doc.Definitions[0].Kind = id, kind
		in := ctxInput(t, doc, "Rename it.")
		bodies := map[string]string{"application id": appBody, "namespace": nsBody, "definition id": idBody, "definition kind": kindBody}
		toolRef, refBody := token("surf/ref")
		toolVersion, versionBody := token("surf/version")
		toolEffect, effectBody := token("surf/effect")
		toolApproval, approvalBody := token("surf/approval")
		toolIn, inBody := token("surf/in")
		toolOut, outBody := token("surf/out")
		role, roleBody := token("surf/role")
		in.Tools = []ToolSummary{{Ref: toolRef, Version: toolVersion, SideEffect: toolEffect, Approval: toolApproval, Input: []string{toolIn}, Output: []string{toolOut}}}
		in.RoleNames = []string{role}
		for k, v := range map[string]string{"tool ref": refBody, "tool version": versionBody, "tool side effect": effectBody, "tool approval": approvalBody,
			"tool input name": inBody, "tool output name": outBody, "role name": roleBody} {
			bodies[k] = v
		}
		text := allText(build(t, in))
		for field, body := range bodies {
			check(t, field, text, body)
		}
		if !strings.Contains(text, redactionPrefix) {
			t.Error("no redaction marker")
		}
	})
	t.Run("requirement capture", func(t *testing.T) {
		doc := fixtureDoc(t, exWorkflow)
		in := ctxInput(t, doc, "Capture the requirements.")
		in.Task = TaskRequirementCapture
		rid, ridBody := token("surf/rid")
		cat, catBody := token("surf/cat")
		status, statusBody := token("surf/status")
		in.Requirements = &intent.RequirementSet{Requirements: []intent.RequirementV2{{ID: rid, Category: cat, Status: status}}}
		text := allText(build(t, in))
		check(t, "requirement id", text, ridBody)
		check(t, "requirement category", text, catBody)
		check(t, "requirement status", text, statusBody)
	})
}

func TestDiagnosticsFedBackAreRedactedClippedAndCapped(t *testing.T) {
	token := "ghp_" + strings.ToLower(alnum("diag/token", 36))
	body := token[4:]
	in := []compiler.Diagnostic{{Path: "$.definitions." + token, Code: "E_" + token, Message: "the value " + token + " is not allowed; " + strings.Repeat("x", 400)}}
	for i := 0; i < 3*MaxDiagnostics; i++ {
		in = append(in, compiler.Diagnostic{Path: "$.p", Code: "E_OTHER", Message: "m"})
	}
	out := capDiagnostics(in)
	if len(out) != MaxDiagnostics {
		t.Errorf("%d diagnostics kept, want the cap of %d", len(out), MaxDiagnostics)
	}
	d := out[0]
	for field, text := range map[string]string{"path": d.Path, "code": d.Code, "message": d.Message} {
		if strings.Contains(text, body) {
			t.Errorf("the %s of a diagnostic keeps the token: %q", field, text)
		}
	}
	if n := len([]rune(d.Message)); n > 300 {
		t.Errorf("the message keeps %d characters, the clip is 300", n)
	}
	if again := capDiagnostics(out); len(again) != len(out) || again[0] != out[0] {
		t.Error("capDiagnostics is not idempotent")
	}
}
