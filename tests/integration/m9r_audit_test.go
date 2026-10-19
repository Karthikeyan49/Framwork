//go:build integration

package integration

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/Karthikeyan49/Framwork/internal/audit"
	"github.com/Karthikeyan49/Framwork/internal/platform/postgres"
	"github.com/Karthikeyan49/Framwork/internal/policy"
	"github.com/Karthikeyan49/Framwork/internal/runtime/data"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestM9RAuditBatchesPreserveEveryDecisionAndRollBackAcrossChunks(t *testing.T) {
	m8Database(t)
	f, _, _, token, _ := m5Fixture(t)
	p, err := f.verifier.Verify(token)
	if err != nil {
		t.Fatal(err)
	}
	for _, foreign := range []bool{false, true} {
		requestID := uuid(t)
		err = postgres.WithRuntime(context.Background(), f.db, func(ctx context.Context, q postgres.Executor) error {
			a, err := m3Scope(f, ctx, q, p, f.tenantA)
			if err != nil {
				return err
			}
			entries := make([]audit.Entry, 257)
			for i := range entries {
				entries[i] = audit.Entry{ActorID: a.ActorID(), TenantID: a.TenantID(), Action: "data.field.read", Resource: "field", Allow: true, DecisionID: fmt.Sprintf("decision-%03d", i), PolicyVersion: policy.Version, RequestID: requestID, State: audit.State{FieldID: fmt.Sprintf("field.contact.test_%03d", i)}}
			}
			if foreign {
				entries[256].TenantID = f.tenantB
			}
			return audit.AppendMany(ctx, q, audit.Postgres{}, entries)
		})
		if foreign {
			var pg *pgconn.PgError
			if !errors.As(err, &pg) || pg.Code != "42501" {
				t.Fatal("foreign batch did not fail closed")
			}
		} else if err != nil {
			t.Fatal(err)
		}
		var count, identities int
		if err := f.operator.QueryRow(`SELECT count(*),count(DISTINCT decision_id) FROM audit.entries WHERE request_id=$1`, requestID).Scan(&count, &identities); err != nil {
			t.Fatal(err)
		}
		want := 257
		if foreign {
			want = 0
		}
		if count != want || identities != want {
			t.Fatal("lost or partially committed audit decisions", count, identities, want)
		}
	}
}

type m9rDeniedAuditBatch struct{ foreignTenant string }

func (w m9rDeniedAuditBatch) Append(ctx context.Context, q postgres.Executor, e audit.Entry) error {
	return (audit.Postgres{}).Append(ctx, q, e)
}
func (w m9rDeniedAuditBatch) AppendBatch(ctx context.Context, q postgres.Executor, entries []audit.Entry) error {
	poisoned := append([]audit.Entry(nil), entries...)
	if len(poisoned) == 0 {
		return errors.New("fault requires real authorization evidence")
	}
	poisoned[len(poisoned)-1].TenantID = w.foreignTenant
	return (audit.Postgres{}).AppendBatch(ctx, q, poisoned)
}
func TestM9RAuditBatchFailureAfterMutationRollsBackAllFacts(t *testing.T) {
	m8Database(t)
	f, h, base, token, app := m5Fixture(t)
	faulted := m5Handler(t, f, policy.Native{}, m9rDeniedAuditBatch{f.tenantB}, true)
	command := data.Command{Key: uuid(t), Operation: "create", Values: m5Values("Audit batch rollback")}
	if result := m5Call(faulted, base+"/commands", token, command); result.Code != 503 {
		t.Fatal("audit failure response", result.Code)
	}
	m9rCommandCounts(t, f, app, 0, 0, 0)
	if receipt := m9rReceipt(t, h, m9rReceiptPath(f, app, command.Key), token); receipt.State != "UNKNOWN" {
		t.Fatal("rolled back receipt survived")
	}
}
