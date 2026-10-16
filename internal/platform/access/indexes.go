package access

import (
	"context"
	"errors"
	"net/http"
	"regexp"
	"strings"

	"github.com/Karthikeyan49/Framwork/internal/platform/postgres"
	"github.com/Karthikeyan49/Framwork/internal/runtime/data"
	"github.com/Karthikeyan49/Framwork/internal/tenant"
	"github.com/jackc/pgx/v5/pgconn"
)

var indexesPOST = regexp.MustCompile(`^/v1/tenants/[0-9a-f-]{36}/preview/app\.[a-z][a-z0-9_]*(\.[a-z][a-z0-9_]*)*/(versions/[0-9]+|datasets/[0-9a-f-]{36}/bindings/[0-9]+)/entities/entity\.[a-z][a-z0-9_]*(\.[a-z][a-z0-9_]*)*/indexes/field\.[a-z][a-z0-9_]*(\.[a-z][a-z0-9_]*)*/(register|build)$`)

func indexReply(index data.NumericIndex, err error) (reply, error) {
	if err == nil {
		return reply{status: 200, body: index}, nil
	}
	// Management DML and evidence must roll back on every rejection, including
	// a late typed failure. Unknown COMMIT uses the durable checkpoint GET.
	status, code := 0, ""
	var pg *pgconn.PgError
	switch {
	case errors.Is(err, data.ErrInvalid), errors.Is(err, data.ErrBudget):
		status, code = 400, "invalid_index"
	case errors.Is(err, data.ErrDenied):
		status, code = 403, "forbidden"
	case errors.Is(err, data.ErrNotFound):
		status, code = 404, "not_found"
	case errors.Is(err, data.ErrBinding), errors.Is(err, data.ErrConflict):
		status, code = 409, "index_conflict"
	case errors.As(err, &pg):
		switch pg.Code {
		case "22023":
			status, code = 400, "invalid_index"
		case "42501":
			status, code = 403, "forbidden"
		case "55000", "55P03", "40001", "40P01":
			status, code = 409, "index_conflict"
		}
	}
	if status != 0 {
		return reply{}, &requestRejection{response: denied(status, code, "Index request rejected; inspect its checkpoint before retrying")}
	}
	return reply{}, err
}

func indexRequest(ctx context.Context, q postgres.Executor, r *http.Request, scope data.Scope, access tenant.Access, guard data.Guard) (reply, error) {
	entity, id := r.PathValue("entity"), r.PathValue("index")
	if err := guard(ctx, "data.entity.read", data.Resource{EntityID: entity}); err != nil {
		return indexReply(data.NumericIndex{}, err)
	}
	if err := guard(ctx, "data.field.read", data.Resource{EntityID: entity, FieldID: id}); err != nil {
		return indexReply(data.NumericIndex{}, err)
	}
	store := data.Store{}
	if r.Method == http.MethodGet {
		index, err := store.FieldIndex(ctx, q, scope, access, entity, id)
		return indexReply(index, err)
	}
	if strings.HasSuffix(r.URL.Path, "/register") {
		var body struct{}
		if readDataCommand(r, &body) != nil {
			return indexReply(data.NumericIndex{}, data.ErrInvalid)
		}
		index, err := store.RegisterFieldIndex(ctx, q, scope, access, entity, id)
		return indexReply(index, err)
	}
	var body struct {
		ExpectedCheckpoint int64 `json:"expected_checkpoint"`
	}
	if readDataCommand(r, &body) != nil {
		return indexReply(data.NumericIndex{}, data.ErrInvalid)
	}
	index, err := store.BuildFieldIndex(ctx, q, scope, access, entity, id, body.ExpectedCheckpoint)
	return indexReply(index, err)
}
