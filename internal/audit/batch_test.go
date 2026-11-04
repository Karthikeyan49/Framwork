package audit

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"

	"github.com/Karthikeyan49/Framwork/internal/platform/postgres"
)

type observedExecutor struct {
	queries []string
	values  [][]any
	err     error
}

func (q *observedExecutor) ExecContext(_ context.Context, query string, args ...any) (sql.Result, error) {
	q.queries = append(q.queries, query)
	q.values = append(q.values, args)
	return nil, q.err
}
func (*observedExecutor) QueryContext(context.Context, string, ...any) (*sql.Rows, error) {
	panic("unexpected query")
}
func (*observedExecutor) QueryRowContext(context.Context, string, ...any) *sql.Row {
	panic("unexpected query")
}

func TestBatchEvidenceRemainsParametersAndInvalidSuffixWritesNothing(t *testing.T) {
	ctx := context.Background()
	entry := Entry{Action: "data.read');DROP TABLE audit.entries;--", Resource: "field", Allow: true}
	q := &observedExecutor{}
	if err := (Postgres{}).AppendBatch(ctx, q, []Entry{entry}); err != nil {
		t.Fatal(err)
	}
	if len(q.queries) != 1 || strings.Contains(q.queries[0], entry.Action) || q.values[0][2] != entry.Action {
		t.Fatal("audit evidence escaped the parameter boundary")
	}
	entries := make([]Entry, 257)
	for i := range entries {
		entries[i] = Entry{Action: "data.row.read", Allow: true}
	}
	entries[256].State.FieldID = strings.Repeat("x", 161)
	q = &observedExecutor{}
	if err := (Postgres{}).AppendBatch(ctx, q, entries); err == nil || len(q.queries) != 0 {
		t.Fatal("invalid suffix partially persisted evidence")
	}
	q = &observedExecutor{}
	if err := (Postgres{}).AppendBatch(ctx, q, make([]Entry, MaxBatchEntries+1)); err == nil || len(q.queries) != 0 {
		t.Fatal("oversized evidence reached SQL")
	}
}

type singleWriter struct {
	calls int
	err   error
}

func (w *singleWriter) Append(context.Context, postgres.Executor, Entry) error {
	w.calls++
	return w.err
}
func TestLegacyWriterFailureIsPreservedAndStopsTheBatch(t *testing.T) {
	err := errors.New("audit unavailable")
	w := &singleWriter{err: err}
	if got := AppendMany(context.Background(), &observedExecutor{}, w, []Entry{{Action: "one"}, {Action: "two"}}); got != err || w.calls != 1 {
		t.Fatal("audit failure was ignored or entries continued")
	}
}
