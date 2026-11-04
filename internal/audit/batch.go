package audit

import (
	"context"
	"errors"
	"strconv"
	"strings"

	"github.com/Karthikeyan49/Framwork/internal/platform/postgres"
)

// MaxBatchEntries bounds request-local evidence, including maximum-size joined
// pages. INSERT chunks remain far below PostgreSQL's parameter-count limit.
const MaxBatchEntries = 8192
const batchRows = 128

type BatchWriter interface {
	Writer
	AppendBatch(context.Context, postgres.Executor, []Entry) error
}

// AppendMany persists every decision on the caller's transaction. It neither
// commits nor samples evidence. Callers must propagate errors and buffer HTTP
// results until the enclosing transaction commits; external effects are excluded.
func AppendMany(ctx context.Context, q postgres.Executor, w Writer, entries []Entry) error {
	if len(entries) == 0 {
		return nil
	}
	if w == nil || len(entries) > MaxBatchEntries {
		return errors.New("audit batch unavailable or oversized")
	}
	if batch, ok := w.(BatchWriter); ok {
		return batch.AppendBatch(ctx, q, entries)
	}
	for _, entry := range entries {
		if err := w.Append(ctx, q, entry); err != nil {
			return err
		}
	}
	return nil
}

func (Postgres) AppendBatch(ctx context.Context, q postgres.Executor, entries []Entry) error {
	if len(entries) > MaxBatchEntries {
		return errors.New("audit batch exceeds evidence budget")
	}
	// Validate the complete set before issuing any SQL, including entries beyond
	// a chunk boundary. Values remain parameters; no metadata becomes SQL text.
	encoded := make([][]any, len(entries))
	for i, entry := range entries {
		args, err := encodeEntry(entry)
		if err != nil {
			return err
		}
		encoded[i] = args
	}
	for start := 0; start < len(encoded); start += batchRows {
		end := min(start+batchRows, len(encoded))
		var query strings.Builder
		query.WriteString(insertEntry)
		args := make([]any, 0, (end-start)*10)
		for i := start; i < end; i++ {
			if i != start {
				query.WriteByte(',')
			}
			query.WriteByte('(')
			for column := range encoded[i] {
				if column != 0 {
					query.WriteByte(',')
				}
				parameter := "$" + strconv.Itoa(len(args)+1)
				if column < 2 {
					query.WriteString("NULLIF(" + parameter + ",'')::uuid")
				} else {
					query.WriteString(parameter)
				}
				args = append(args, encoded[i][column])
			}
			query.WriteByte(')')
		}
		if _, err := q.ExecContext(ctx, query.String(), args...); err != nil {
			return err
		}
	}
	return nil
}
