//go:build integration

package integration

// Shared fixture for the M14 AI builder tests (ADR104). It extends the M12 fixture
// with the real routes of the access API wired to an ai.Service whose gateway runs a
// ScriptedProvider: no model and no network exist in this environment, so every
// "model answer" below is a scripted byte string. Nothing here is evidence about the
// behaviour of a real model.

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Karthikeyan49/Framwork/internal/ai"
	"github.com/Karthikeyan49/Framwork/internal/audit"
	"github.com/Karthikeyan49/Framwork/internal/compiler"
	"github.com/Karthikeyan49/Framwork/internal/intent"
	"github.com/Karthikeyan49/Framwork/internal/metadata"
	"github.com/Karthikeyan49/Framwork/internal/platform/access"
	"github.com/Karthikeyan49/Framwork/internal/platform/httpserver"
	"github.com/Karthikeyan49/Framwork/internal/policy"
)

// m14Env is the M12 environment with the AI routes wired in.
type m14Env struct {
	*m13Env
	provider *ai.ScriptedProvider
	svc      *ai.Service
	api      access.API
}

// m14Options tunes the service of one test.
type m14Options struct {
	limits      ai.ServiceLimits
	noService   bool // leave AI unconfigured (routes answer 503)
	gateway     ai.Gateway
	gatewayOpts ai.Options
	catalog     *compiler.Catalog // compile catalog of the API (nil: the platform catalog)
}

func m14Setup(t *testing.T, opts m14Options, steps ...ai.ScriptStep) *m14Env {
	t.Helper()
	e := &m14Env{m13Env: m13Setup(t), provider: ai.NewScriptedProvider(steps...)}
	registry := m3Registry(t)
	gateway := opts.gateway
	if gateway == nil {
		gateway = ai.NewGateway(e.provider, opts.gatewayOpts)
	}
	e.svc = ai.NewService(gateway, registry, opts.limits)
	e.api = access.API{DB: e.f.db, Auth: e.f.verifier, Policy: policy.Native{}, PolicyLoader: (policy.RoleStore{}).Load, Audit: audit.Postgres{}, Registry: registry, PreviewEnabled: true, WorkflowsEnabled: true}
	e.api.CompilerCatalog = opts.catalog
	if !opts.noService {
		e.api.AI = access.AIConfig{Service: e.svc}
	}
	e.h = httpserver.Handler(httpserver.Options{Logger: slog.New(slog.NewJSONHandler(io.Discard, nil)), Register: e.api.Register, AllowPOST: e.api.AcceptsPOST})
	return e
}

// draft registers the order document as revision 1 (alice, owner) and returns the
// stored document.
func (e *m14Env) draft(threshold string) metadata.Document {
	e.t.Helper()
	if rev := e.register(e.alice, threshold, 0); rev != 1 {
		e.t.Fatal("unexpected revision", rev)
	}
	return e.storedDoc(1)
}

// storedDoc reads a stored revision through the API.
func (e *m14Env) storedDoc(revision int64) metadata.Document {
	e.t.Helper()
	w := m14Do(e.h, "GET", "/v1/tenants/"+e.f.tenantA+"/metadata/"+e.app+"/versions/"+itoa(revision), e.alice, nil)
	if w.Code != 200 {
		e.t.Fatal("read revision", w.Code, w.Body.String())
	}
	var v struct {
		Document metadata.Document `json:"document"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &v); err != nil {
		e.t.Fatal("revision shape", err)
	}
	return v.Document
}

func itoa(n int64) string {
	b, _ := json.Marshal(n)
	return string(b)
}

// m14Do sends an optional JSON body (a string is sent verbatim, anything else is
// encoded) with an optional token.
func m14Do(h http.Handler, method, path, token string, body any) *httptest.ResponseRecorder {
	var raw string
	switch b := body.(type) {
	case nil:
	case string:
		raw = b
	case []byte:
		raw = string(b)
	default:
		enc, _ := json.Marshal(b)
		raw = string(enc)
	}
	req := httptest.NewRequest(method, path, strings.NewReader(raw))
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if method == "POST" {
		req.Header.Set("Content-Type", "application/json")
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	return w
}

func m14DoType(h http.Handler, method, path, token, contentType, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	req.Header.Set("Content-Type", contentType)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	return w
}

func (e *m14Env) ai(method, suffix, token string, body any) *httptest.ResponseRecorder {
	e.t.Helper()
	return m14Do(e.h, method, e.root+"/ai"+suffix, token, body)
}

// proposal decodes a proposal view out of a response of the given status.
func (e *m14Env) proposal(w *httptest.ResponseRecorder, status int) ai.Proposal {
	e.t.Helper()
	if w.Code != status {
		e.t.Fatalf("status %d, want %d: %s", w.Code, status, w.Body.String())
	}
	var p ai.Proposal
	if err := json.Unmarshal(w.Body.Bytes(), &p); err != nil || p.ID == "" {
		e.t.Fatal("proposal shape", err, w.Body.String())
	}
	return p
}

// errCode decodes the error code of a response.
func m14Code(w *httptest.ResponseRecorder) string {
	var r struct {
		Code string `json:"code"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &r)
	return r.Code
}

// labelPatch is a scripted answer that renames one definition. Presentation only.
func labelPatch(t *testing.T, doc metadata.Document, id, label string) []byte {
	t.Helper()
	return patchPayload(t, doc, func(p *ai.PatchProposal) {
		d := m14Def(t, doc, id)
		updated := d
		updated.Label = label
		p.Operations = []ai.PatchOperation{{Op: "update", Target: id, ExpectedHash: metadata.DefinitionHash(d), Definition: &updated, Rationale: "rename"}}
	})
}

func m14Def(t *testing.T, doc metadata.Document, id string) metadata.Definition {
	t.Helper()
	for _, d := range doc.Definitions {
		if d.ID == id {
			return d
		}
	}
	t.Fatal("no definition", id)
	return metadata.Definition{}
}

// patchPayload builds the JSON of a patch proposal from an empty one.
func patchPayload(t *testing.T, _ metadata.Document, build func(*ai.PatchProposal)) []byte {
	t.Helper()
	p := ai.PatchProposal{Version: ai.PatchProposalVersion, Operations: []ai.PatchOperation{}, Assumptions: []string{}, UnresolvedRequirements: []ai.UnresolvedRequirement{}, RiskFlags: []ai.RiskFlag{}}
	build(&p)
	raw, _, err := ai.CanonicalPatch(p)
	if err != nil {
		t.Fatal("scripted proposal does not pass the strict parser:", err)
	}
	return raw
}

// step is a scripted provider answer with plausible usage.
func step(payload []byte) ai.ScriptStep {
	return ai.ScriptStep{Payload: payload, Usage: ai.Usage{InputTokens: 1200, OutputTokens: 300}, Model: "scripted-model-1", Delay: 0}
}

// auditCount counts the audit rows of the tenant for an action of this app.
func (e *m14Env) auditCount(action string, allowed bool) int {
	e.t.Helper()
	var n int
	if err := e.f.operator.QueryRow(`SELECT count(*) FROM audit.entries WHERE tenant_id=$1 AND action=$2 AND allowed=$3 AND state->>'metadata_app'=$4`, e.f.tenantA, action, allowed, e.app).Scan(&n); err != nil {
		e.t.Fatal(err)
	}
	return n
}

// auditStates returns the audit state documents of an action of this app.
func (e *m14Env) auditStates(action string) []map[string]any {
	e.t.Helper()
	rows, err := e.f.operator.Query(`SELECT state FROM audit.entries WHERE tenant_id=$1 AND action=$2 AND state->>'metadata_app'=$3 ORDER BY occurred_at`, e.f.tenantA, action, e.app)
	if err != nil {
		e.t.Fatal(err)
	}
	defer rows.Close()
	var out []map[string]any
	for rows.Next() {
		var raw []byte
		var state map[string]any
		if err = rows.Scan(&raw); err != nil || json.Unmarshal(raw, &state) != nil {
			e.t.Fatal("audit state", err)
		}
		out = append(out, state)
	}
	return out
}

var _ = time.Second

// heads reads the metadata and requirement-set heads of the app.
func (e *m14Env) heads() (revision, requirements int64) {
	e.t.Helper()
	if err := e.f.operator.QueryRow(`SELECT (SELECT coalesce(max(revision),0) FROM control.metadata_versions WHERE tenant_id=$1 AND app_id=$2),
 (SELECT coalesce(max(version),0) FROM control.requirement_sets WHERE tenant_id=$1 AND app_id=$2)`, e.f.tenantA, e.app).Scan(&revision, &requirements); err != nil {
		e.t.Fatal("heads", err)
	}
	return revision, requirements
}

// ------------------------------------------------------------------ request helpers

func (e *m14Env) propose(token, request string) *httptest.ResponseRecorder {
	return e.ai("POST", "/patch-proposals", token, map[string]any{"request": request})
}

func (e *m14Env) apply(token, id string, expected int64, ack []string) *httptest.ResponseRecorder {
	if ack == nil {
		ack = []string{}
	}
	return e.ai("POST", "/proposals/"+id+"/apply", token, map[string]any{"expected_revision": expected, "acknowledge": ack})
}

func (e *m14Env) reject(token, id, reason string) *httptest.ResponseRecorder {
	return e.ai("POST", "/proposals/"+id+"/reject", token, map[string]any{"reason": reason})
}

func (e *m14Env) confirm(token, id string, version int64, decisions map[string]string) *httptest.ResponseRecorder {
	list := []map[string]string{}
	for id, d := range decisions {
		list = append(list, map[string]string{"id": id, "decision": d})
	}
	return e.ai("POST", "/proposals/"+id+"/confirm", token, map[string]any{"expected_requirements_version": version, "decisions": list})
}

// readyPatch scripts one label answer and creates a READY patch proposal as alice.
func (e *m14Env) readyPatch(revision int64, id, label string) ai.Proposal {
	e.t.Helper()
	e.provider.Push(e.answerFor(revision, func(doc metadata.Document) []byte { return labelPatch(e.t, doc, id, label) }))
	p := e.proposal(e.propose(e.alice, "Rename "+id+" to "+label+"."), 201)
	if p.Status != ai.ProposalReady {
		e.t.Fatalf("proposal is %s (%s)", p.Status, p.FailureCode)
	}
	return p
}

// reqAnswer scripts a valid requirement proposal (one approval threshold requirement).
func (e *m14Env) reqAnswer(id string) ai.ScriptStep {
	sem, _ := json.Marshal(intent.ApprovalThreshold{WorkflowID: "workflow.order.approve", EntityID: "entity.order", FieldID: "field.order.amount", Currency: "USD", Op: "GT", Amount: "500", ApproverRoles: []string{"admin"}})
	p := ai.RequirementProposal{Version: ai.RequirementProposalVersion, Requirements: []ai.RequirementDraft{{ID: id, Category: intent.CategoryApprovalThreshold, Statement: "Orders above 500 USD need admin approval.", Semantics: sem}},
		OpenQuestions: []ai.UnresolvedRequirement{{ID: "q-currency", Question: "Which currencies are in scope?"}}, ProposedTests: []intent.AcceptanceCase{}, Assumptions: []string{}}
	raw, _, err := ai.CanonicalRequirementProposal(p)
	if err != nil {
		e.t.Fatal("scripted requirement proposal:", err)
	}
	return step(raw)
}

// customRole publishes a stored role of tenant A and assigns it to a new member.
func (e *m14Env) customRole(name string, actions ...string) m13Actor {
	e.t.Helper()
	e.publishRole(name, 0, actions...)
	a := e.member("user_"+name, "viewer")
	r5Assign(e.t, e.h, "/v1/tenants/"+e.f.tenantA, e.alice, a.id, name, 200)
	return e.fresh(a)
}

// proposalRows counts the proposals of tenant A (operator view).
func (e *m14Env) proposalRows() int {
	e.t.Helper()
	var n int
	if err := e.f.operator.QueryRow(`SELECT count(*) FROM control.ai_proposals WHERE tenant_id=$1 AND app_id=$2`, e.f.tenantA, e.app).Scan(&n); err != nil {
		e.t.Fatal(err)
	}
	return n
}
