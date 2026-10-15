//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/Karthikeyan49/Framwork/internal/audit"
	"github.com/Karthikeyan49/Framwork/internal/compiler"
	"github.com/Karthikeyan49/Framwork/internal/events"
	"github.com/Karthikeyan49/Framwork/internal/identity"
	"github.com/Karthikeyan49/Framwork/internal/intent"
	"github.com/Karthikeyan49/Framwork/internal/platform/access"
	"github.com/Karthikeyan49/Framwork/internal/platform/httpserver"
	"github.com/Karthikeyan49/Framwork/internal/platform/postgres"
	"github.com/Karthikeyan49/Framwork/internal/policy"
	"github.com/Karthikeyan49/Framwork/internal/promotion"
	"github.com/Karthikeyan49/Framwork/internal/runtime/data"
	"github.com/Karthikeyan49/Framwork/internal/sandbox"
	"github.com/Karthikeyan49/Framwork/internal/tenant"
	"io"
	"log/slog"
	"strings"
	"testing"
)

type fixtureReleaseGate bool

func (g fixtureReleaseGate) AuthorizeRelease(string, string, string, string) bool { return bool(g) }
func TestM6RPersistedApprovalExactActivationCompatibilityAndRollback(t *testing.T) {
	f, _, _, _, app := m5Fixture(t)
	ctx := context.Background()
	registry := m3Registry(t)
	if e := (tenant.Store{}).Provision(ctx, f.operator, f.tenantA, "Release fixture", f.bob, "admin", true); e != nil {
		t.Fatal(e)
	}
	run := func(subject string, fn func(context.Context, postgres.Executor, tenant.Access) error) error {
		return postgres.WithRuntime(ctx, f.db, func(ctx context.Context, q postgres.Executor) error {
			p, e := f.verifier.Verify(f.token(t, subject, nil))
			if e != nil {
				return e
			}
			u, e := (identity.Store{}).Resolve(ctx, q, p)
			if e != nil {
				return e
			}
			if e = (tenant.Store{}).BindActor(ctx, q, u); e != nil {
				return e
			}
			a, e := (tenant.Store{}).Resolve(ctx, q, u, f.tenantA)
			if e != nil {
				return e
			}
			return fn(ctx, q, a)
		})
	}
	var c1, c2 promotion.Candidate
	var a1, a2 promotion.Approval
	gate := fixtureReleaseGate(true)
	store := promotion.Store{}
	if e := run(f.aliceSubject, func(ctx context.Context, q postgres.Executor, a tenant.Access) error {
		v, e := registry.Get(ctx, q, a, app, 1)
		if e != nil {
			return e
		}
		s, e := registry.Parse(v.Document)
		if e != nil {
			return e
		}
		p, d := compiler.Compile(s, compiler.PlatformCatalog())
		if !p.Valid() {
			t.Fatal(d)
		}
		i := intent.Contract{Version: "1.0.0", Requirements: []intent.Requirement{{ID: "approval", Mandatory: true, Status: "RESOLVED", EntityID: "entity.contact", FieldID: "field.contact.balance", Operator: "GT", Threshold: "500", Currency: "USD", ApproverRole: "admin", Trigger: "submit"}}}
		acceptance := intent.Acceptance{Version: "1.0.0", IntentHash: i.Hash(), AuthorActorID: f.alice, Provenance: "HUMAN_INDEPENDENT", Cases: []intent.Case{{RequirementID: "approval", Input: "501 USD", Expected: "admin approval"}}}
		acceptanceRaw, _ := json.Marshal(acceptance)
		h := strings.Repeat("a", 64)
		b := promotion.Bundle{Version: "1.0.0", RequesterID: f.alice, HighRisk: true, Dependencies: promotion.Dependencies{PolicyHash: h, ToolsHash: h, SchemaHash: h}, Intent: i, Acceptance: acceptance, Sandbox: sandbox.Context{ID: "sandbox.test", DataMode: "SYNTHETIC_SEPARATE", ToolMode: "BLOCK"}, Verification: promotion.Verification{Version: "1.0.0", SandboxID: "sandbox.test", IntentHash: i.Hash(), AcceptanceHash: promotion.Hash(acceptanceRaw), ReportHash: h, Outcome: "PASS"}}
		c1, e = promotion.Build(s, p, b)
		if e != nil {
			return e
		}
		if e = store.Stage(ctx, q, a, c1, gate); e != nil {
			return e
		}
		b = c1.Bundle()
		b.BaseRelease = c1.Hash()
		b.Verification.ReportHash = strings.Repeat("b", 64)
		c2, e = promotion.Build(s, p, b)
		if e != nil {
			return e
		}
		return store.Stage(ctx, q, a, c2, gate)
	}); e != nil {
		t.Fatal("release staging")
	}
	if e := run(f.aliceSubject, func(ctx context.Context, q postgres.Executor, a tenant.Access) error {
		_, e := store.Approve(ctx, q, a, c1, c1.Hash(), gate)
		return e
	}); e == nil {
		t.Fatal("self approval")
	}
	if e := run(f.bobSubject, func(ctx context.Context, q postgres.Executor, a tenant.Access) error {
		var e error
		a1, e = store.Approve(ctx, q, a, c1, c1.Hash(), gate)
		return e
	}); e != nil {
		t.Fatal("independent approval")
	}
	if e := run(f.bobSubject, func(ctx context.Context, q postgres.Executor, a tenant.Access) error {
		return store.Activate(ctx, q, a, c1, a1, 0, "", c1.Bundle().Dependencies, gate)
	}); e != nil {
		t.Fatal("first activation")
	}
	if e := run(f.bobSubject, func(ctx context.Context, q postgres.Executor, a tenant.Access) error {
		return store.Activate(ctx, q, a, c2, a1, 1, c1.Hash(), c2.Bundle().Dependencies, gate)
	}); e == nil {
		t.Fatal("different approved artifact activated")
	}
	if e := run(f.bobSubject, func(ctx context.Context, q postgres.Executor, a tenant.Access) error {
		var e error
		a2, e = store.Approve(ctx, q, a, c2, c2.Hash(), gate)
		if e != nil {
			return e
		}
		return store.Activate(ctx, q, a, c2, a2, 1, c1.Hash(), c2.Bundle().Dependencies, gate)
	}); e != nil {
		t.Fatal("second activation")
	}
	if e := promotion.CheckActiveCompatibility(ctx, f.operator, []string{compiler.IRVersion}); e != nil {
		t.Fatal("supported active IR")
	}
	if e := promotion.CheckActiveCompatibility(ctx, f.operator, []string{"2.0.0"}); e == nil {
		t.Fatal("unsupported platform deployment accepted")
	}
	if e := promotion.CheckActiveCompatibility(ctx, f.db, []string{compiler.IRVersion}); e == nil {
		t.Fatal("normal RLS connection falsely passed global inventory")
	}
	pin := promotion.ExecutionPin{ReleaseHash: c2.Hash(), WorkflowID: "workflow.future", WorkflowVersion: "1"}
	plan := promotion.RollbackPlan{StorageCompatible: true, ExternalEffectsReviewed: true, PlanHash: strings.Repeat("c", 64)}
	if e := run(f.bobSubject, func(ctx context.Context, q postgres.Executor, a tenant.Access) error {
		unsafe := plan
		unsafe.RecoveryRequired = true
		return store.Rollback(ctx, q, a, c1, c2.Hash(), 2, unsafe, gate)
	}); e == nil {
		t.Fatal("irreversible recovery ignored")
	}
	if e := run(f.bobSubject, func(ctx context.Context, q postgres.Executor, a tenant.Access) error {
		if e := store.Rollback(ctx, q, a, c1, c2.Hash(), 2, plan, gate); e != nil {
			return e
		}
		p, h, gen, e := store.Active(ctx, q, a, app, []string{compiler.IRVersion})
		if e != nil {
			return e
		}
		if h != c1.Hash() || gen != 3 || p.Hash() != c1.Bundle().IRHash {
			t.Fatal("rollback/stored executable mismatch")
		}
		return nil
	}); e != nil {
		t.Fatal("rollback")
	}
	if pin.ReleaseHash != c2.Hash() {
		t.Fatal("in-flight execution repinned")
	}
	if _, e := f.operator.ExecContext(ctx, `UPDATE control.release_bundles SET body=body WHERE tenant_id=$1 AND app_id=$2`, f.tenantA, app); e == nil {
		t.Fatal("immutable artifact changed")
	}
}

type rejectedEventWriter struct{}

func (rejectedEventWriter) Append(context.Context, postgres.Executor, events.Intent) error {
	return errors.New("simulated event persistence failure")
}
func TestM6RBusinessAuditAndOutboxRollbackTogether(t *testing.T) {
	f, h, base, token, app := m5Fixture(t)
	record := m5Create(t, h, base, token, "Committed record")
	var records, audits, outbox int
	count := func() {
		if e := f.operator.QueryRow(`SELECT (SELECT count(*) FROM runtime.records WHERE tenant_id=$1 AND app_id=$2),(SELECT count(*) FROM audit.entries WHERE tenant_id=$1),(SELECT count(*) FROM runtime.outbox_intents WHERE tenant_id=$1 AND app_id=$2)`, f.tenantA, app).Scan(&records, &audits, &outbox); e != nil {
			t.Fatal("atomicity inspection")
		}
	}
	count()
	if records != 1 || outbox != 1 {
		t.Fatal("business/event mismatch")
	}
	beforeAudit := audits
	api := access.API{DB: f.db, Auth: f.verifier, Policy: policy.Native{}, Audit: audit.Postgres{}, Registry: m3Registry(t), PreviewEnabled: true, Events: rejectedEventWriter{}}
	fail := httpserver.Handler(httpserver.Options{Logger: slog.New(slog.NewJSONHandler(io.Discard, nil)), Register: api.Register, AllowPOST: api.AcceptsPOST})
	if w := m5Call(fail, base+"/commands", token, data.Command{Operation: "update", ID: record.ID, ExpectedVersion: 1, Values: m5Values("Must roll back")}); w.Code != 503 {
		t.Fatal("failed outbox reported success")
	}
	count()
	if records != 1 || outbox != 1 || audits != beforeAudit {
		t.Fatal("partial business/audit/outbox commit")
	}
	var version int64
	var name string
	if e := f.operator.QueryRow(`SELECT row_version,custom_data->>'field.contact.name' FROM runtime.records WHERE tenant_id=$1 AND id=$2`, f.tenantA, record.ID).Scan(&version, &name); e != nil || version != 1 || name != "Committed record" {
		t.Fatal("business update survived rollback")
	}
	// Audit failure before intent persistence also rolls back the business record.
	api.Audit = failedAudit{}
	api.Events = events.Postgres{}
	fail = httpserver.Handler(httpserver.Options{Logger: slog.New(slog.NewJSONHandler(io.Discard, nil)), Register: api.Register, AllowPOST: api.AcceptsPOST})
	if w := m5Call(fail, base+"/commands", token, data.Command{Operation: "create", Values: m5Values("Must not exist")}); w.Code != 503 {
		t.Fatal("audit failure success")
	}
	count()
	if records != 1 || outbox != 1 {
		t.Fatal("audit failure partial commit")
	}
}
