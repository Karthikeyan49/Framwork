package data

import (
	"context"
	"github.com/Karthikeyan49/Framwork/internal/platform/postgres"
	"github.com/Karthikeyan49/Framwork/internal/tenant"
)

type LegacyAdapter struct{}

func (LegacyAdapter) Query(ctx context.Context, q postgres.Executor, s Scope, a tenant.Access, e string, v Query, g Guard, resolve ...ScopeResolver) (Page, error) {
	return (Store{legacy: true}).Query(ctx, q, s, a, e, v, g, resolve...)
}
func (st Store) recordsTable() string {
	if st.workerSource {
		return "runtime.worker_source_record()"
	}
	if st.legacy {
		return "runtime.read_preview_records"
	}
	return "runtime.read_records"
}

// WorkerSourceAdapter reads the exact masked source admitted by a bound job.
// It exposes no mutation port and retains normal policy/DataScope validation.
type WorkerSourceAdapter struct{}
type workerReadKey struct{}

func (WorkerSourceAdapter) Query(ctx context.Context, q postgres.Executor, s Scope, a tenant.Access, e string, v Query, g Guard, resolve ...ScopeResolver) (Page, error) {
	if v.RecordID == "" || v.Limit != 1 || v.Filter != nil || v.Sort != nil || v.Join != nil || v.Aggregate != nil || v.After != "" {
		return Page{}, ErrDenied
	}
	return (Store{workerSource: true}).Query(context.WithValue(ctx, workerReadKey{}, true), q, s, a, e, v, g, resolve...)
}
