//go:build integration

package integration

// Tool-pin staleness through the HTTP handlers (ADR102 section 2, ADR103 section 4). The pure rule
// (promotion.Staleness) is unit tested; the closure that asks the operator catalog about each pin lives
// in the API (releases.go: staleness) and was reviewed by reading only (M12 API and M13 reviews, "not
// covered"). These tests build a real candidate whose IR pins an operator tool and move the catalog.

import (
	"encoding/json"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/Karthikeyan49/Framwork/internal/compiler"
	"github.com/Karthikeyan49/Framwork/internal/intent"
	"github.com/Karthikeyan49/Framwork/internal/metadata"
	"github.com/Karthikeyan49/Framwork/internal/platform/access"
	"github.com/Karthikeyan49/Framwork/internal/promotion"
	"github.com/Karthikeyan49/Framwork/internal/tools"
)

// m13ToolStep adds a tool activity (schema 1.5.0, whose workflows author DSL 2.1.0) behind the approval state of the order workflow.
func m13ToolStep(tool string) func(*metadata.Document) {
	return func(d *metadata.Document) {
		d.SchemaVersion = metadata.ToolWorkflowSchemaVersion
		for i := range d.Definitions {
			d.Definitions[i].SchemaVersion = metadata.ToolWorkflowSchemaVersion
			if d.Definitions[i].ID != "workflow.order.approve" {
				continue
			}
			wf := d.Definitions[i].Workflow
			wf.DSLVersion = "2.1.0"
			for j := range wf.States {
				if wf.States[j].ID == "state.approval" {
					wf.States[j].Transitions = []metadata.Transition{{To: "state.tool", On: "approved"}, {To: "state.failed", On: "rejected"}}
				}
			}
			wf.States = append(wf.States, metadata.WorkflowState{ID: "state.tool", Type: "activity", Activity: &metadata.Activity{
				ToolRef: tool, Version: "1.0.0", Inputs: map[string]string{"contact_id": "field.order.id"},
				Retry: metadata.Retry{MaxAttempts: 3, InitialBackoffMS: 100, MaxBackoffMS: 400}}, Transitions: []metadata.Transition{{To: "state.complete"}}})
		}
	}
}

// m13ToolCatalog is the operator catalog with the one READ tool of the M10 fixtures, optionally edited.
func m13ToolCatalog(t *testing.T, edit func(*tools.Definition)) tools.Catalog {
	t.Helper()
	var defs []tools.Definition
	for _, d := range m10Tools(9) {
		if d.Ref == "tool.contact_lookup" {
			if edit != nil {
				edit(&d)
			}
			defs = append(defs, d)
		}
	}
	c, err := tools.NewCatalog(defs)
	if err != nil {
		t.Fatal("tool catalog:", err)
	}
	return c
}

// useToolCatalog serves the fixture's HTTP API with the given operator catalog.
func (e *m13Env) useToolCatalog(c tools.Catalog) {
	e.useAPI(func(a *access.API) {
		cc := compiler.PlatformCatalog()
		cc.ToolCatalog = c
		a.CompilerCatalog, a.ToolCatalog = &cc, c
	})
}

// buildWithTool stages a real candidate whose workflow calls toolRef. A changed tool activity needs a
// resolved tool_effect requirement (the verifier blocks it otherwise), so the requirement set carries
// one next to the approval threshold. Source author alice, acceptance author bob.
func (e *m13Env) buildWithTool(toolRef string, requester m13Actor) string {
	e.t.Helper()
	w := e.stageWithTool(toolRef, requester)
	if w.Code != 201 {
		e.t.Fatal("build candidate", w.Code, w.Body.String())
	}
	var v struct {
		Hash string `json:"artifact_hash"`
	}
	if json.Unmarshal(w.Body.Bytes(), &v) != nil || !promotion.ValidHash(v.Hash) {
		e.t.Fatal("candidate contract", w.Body.String())
	}
	return v.Hash
}

// stageWithTool prepares the artifacts and asks the builder for the candidate; it returns the reply.
func (e *m13Env) stageWithTool(toolRef string, requester m13Actor) *httptest.ResponseRecorder {
	t := e.t
	t.Helper()
	doc := orderDocument(e.app, "500")
	m13ToolStep(toolRef)(&doc)
	revision, requirements := e.heads()
	raw, _ := json.Marshal(doc)
	if w := m3Call(e.h, "POST", "/v1/tenants/"+e.f.tenantA+"/metadata/"+e.app+"/versions", e.alice, revision, raw); w.Code != 201 {
		t.Fatal("register source", w.Code, w.Body.String())
	}
	revision++
	sandbox := e.sandbox(e.alice, revision)
	set := thresholdSet(e.app, "500")
	semantics, _ := json.Marshal(intent.ToolEffect{ToolRef: toolRef, MaxSideEffect: "READ", RequiresApproval: false, MaxSandboxMode: "TEST_ACCOUNT"})
	set.Requirements = append(set.Requirements, intent.RequirementV2{ID: "po-tool", Category: intent.CategoryToolEffect, Requiredness: intent.Mandatory,
		RequirednessBasis: intent.BasisPolicyFloor, Status: intent.StatusResolved, Provenance: intent.ProvenanceHuman, Semantics: semantics})
	w := e.requirements(e.alice, set, requirements)
	if w.Code != 201 {
		t.Fatal("requirements", w.Code, w.Body.String())
	}
	var stored promotion.StoredRequirements
	_ = json.Unmarshal(w.Body.Bytes(), &stored)
	spec0 := acceptanceFor(set, "500.01")
	spec0.Cases = append(spec0.Cases, intent.AcceptanceCase{ID: "tool-within-bounds", RequirementID: "po-tool", Given: map[string]json.RawMessage{}, Expect: intent.Expectation{Outcome: intent.OutcomeHolds}})
	w = e.acceptance(e.person("bob").token, stored.Version, spec0)
	if w.Code != 201 {
		t.Fatal("acceptance", w.Code, w.Body.String())
	}
	var spec promotion.StoredAcceptance
	_ = json.Unmarshal(w.Body.Bytes(), &spec)
	return e.candidate(requester.token, m12Artifacts{revision: revision, requirementsVersion: stored.Version, acceptanceID: spec.ID, sandboxID: sandbox})
}

func TestM13ToolPinStalenessAtViewApproveAndActivate(t *testing.T) {
	e := m13Setup(t)
	m13RequireStack(t, e, true)
	original := m13ToolCatalog(t, nil)
	e.useToolCatalog(original)
	hash := e.buildWithTool("tool.contact_lookup", e.carol)
	if v := m13Decode(e.httpCandidate(e.viewer, hash)); v.Stale == nil || *v.Stale {
		t.Fatalf("a fresh tool-pinned candidate reads stale: %+v", v)
	}
	var b promotion.BundleV2
	if err := json.Unmarshal(e.bundle(hash), &b); err != nil || len(b.Dependencies.ToolPins) != 1 || b.Dependencies.ToolPins[0].Ref != "tool.contact_lookup" {
		t.Fatalf("the candidate does not pin the tool: %v %+v", err, b.Dependencies.ToolPins)
	}
	m13Code(t, "approve while the catalog is unchanged", e.httpApprove(e.dave.token, hash), 201)

	changed := m13ToolCatalog(t, func(d *tools.Definition) { d.Connector.TimeoutMS = 900 })
	removed, err := tools.NewCatalog(nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name    string
		catalog tools.Catalog
		reason  string
	}{{"definition changed", changed, "TOOL_CHANGED"}, {"tool removed", removed, "TOOL_REMOVED"}} {
		e.useToolCatalog(tc.catalog)
		before := e.state()
		view := m13Decode(e.httpCandidate(e.viewer, hash))
		if view.Stale == nil || !*view.Stale || !slices.Equal(view.StaleReasons, []string{tc.reason}) {
			t.Errorf("%s: view stale=%v reasons=%v, want [%s]", tc.name, view.Stale, view.StaleReasons, tc.reason)
		}
		m13Reasons(t, tc.name+": approve", m13Code(t, tc.name+": approve", e.httpApprove(e.erin.token, hash), 409, "candidate_stale"), tc.reason)
		m13Reasons(t, tc.name+": activate", m13Code(t, tc.name+": activate", e.httpActivate(e.dave.token, hash, "", 0), 409, "candidate_stale"), tc.reason)
		e.unchanged(tc.name, before)
	}
	// Restoring the exact definition makes the same candidate fresh again: nothing was stored.
	e.useToolCatalog(original)
	if view := m13Decode(e.httpCandidate(e.viewer, hash)); view.Stale == nil || *view.Stale {
		t.Fatalf("the candidate stayed stale after the catalog was restored: %+v", view)
	}
	m13Code(t, "activate once the catalog is restored", e.httpActivate(e.dave.token, hash, "", 0), 200)
}

// A tool the tenant may not use is refused when the candidate is built (422: the risk assessment blocks the
// unresolved pin, BLOCK.tool_unresolved), so a tenant-denied pin never reaches approval. After a build, narrowing the tenant list
// changes the pinned hash (the list is part of the definition), which reads as TOOL_CHANGED: the
// TOOL_TENANT_DENIED reason of the pure rule cannot arise through the API and stays defence in depth.
func TestM13ToolPinTenantListIsPartOfThePinAndOfTheBuildCheck(t *testing.T) {
	e := m13Setup(t)
	m13RequireStack(t, e, true)
	otherTenant := "00000000-0000-4000-8000-000000000001"
	e.useToolCatalog(m13ToolCatalog(t, func(d *tools.Definition) { d.Tenants = []string{otherTenant} }))
	w := e.stageWithTool("tool.contact_lookup", e.carol)
	if w.Code != 422 || !strings.Contains(strings.ToLower(w.Body.String()), "tool_unresolved") {
		t.Fatalf("a tool the tenant may not use was built into a candidate: %d %s", w.Code, w.Body.String())
	}

	e = m13Setup(t)
	e.useToolCatalog(m13ToolCatalog(t, nil))
	hash := e.buildWithTool("tool.contact_lookup", e.carol)
	e.useToolCatalog(m13ToolCatalog(t, func(d *tools.Definition) { d.Tenants = []string{otherTenant} }))
	view := m13Decode(e.httpCandidate(e.viewer, hash))
	if view.Stale == nil || !*view.Stale || !slices.Equal(view.StaleReasons, []string{"TOOL_CHANGED"}) {
		t.Fatalf("narrowing the tenant list after the build: stale=%v reasons=%v, want [TOOL_CHANGED]", view.Stale, view.StaleReasons)
	}
	m13Reasons(t, "approve", m13Code(t, "approve", e.httpApprove(e.dave.token, hash), 409, "candidate_stale"), "TOOL_CHANGED")
}
