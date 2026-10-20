//go:build integration

package integration

import (
	"context"
	"errors"
	"github.com/Karthikeyan49/Framwork/internal/audit"
	"github.com/Karthikeyan49/Framwork/internal/events"
	"github.com/Karthikeyan49/Framwork/internal/platform/access"
	"github.com/Karthikeyan49/Framwork/internal/platform/httpserver"
	"github.com/Karthikeyan49/Framwork/internal/platform/postgres"
	"github.com/Karthikeyan49/Framwork/internal/policy"
	"github.com/Karthikeyan49/Framwork/internal/runtime/data"
	"io"
	"log/slog"
	"testing"
)

type fixtureReleaseGate bool

func (g fixtureReleaseGate) AuthorizeRelease(string, string, string, string) bool { return bool(g) }

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
