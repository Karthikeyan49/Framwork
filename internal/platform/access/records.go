package access

import (
	"context"
	"errors"
	"net/http"
	"regexp"

	"github.com/Karthikeyan49/Framwork/internal/audit"
	"github.com/Karthikeyan49/Framwork/internal/platform/postgres"
	"github.com/Karthikeyan49/Framwork/internal/runtime/data"
	"github.com/Karthikeyan49/Framwork/internal/tenant"
	"github.com/jackc/pgx/v5/pgconn"
)

var recordAccessPOST = regexp.MustCompile(`^/v1/tenants/[0-9a-f-]{36}/preview/app\.[a-z][a-z0-9_]*(\.[a-z][a-z0-9_]*)*/(versions/[0-9]+|datasets/[0-9a-f-]{36}/bindings/[0-9]+)/entities/entity\.[a-z][a-z0-9_]*(\.[a-z][a-z0-9_]*)*/records/[0-9a-f-]{36}/access$`)

// Every rejection rolls back the whole access transaction, including a late
// SQL witness failure, before a fresh transaction audits the denial. Access
// changes are never retried automatically: after an unknown commit, reload
// the record access and compare its version and owner/shares.
func recordAccessReply(value data.Access, err error) (reply, error) {
	if err == nil {
		return reply{status: 200, body: value}, nil
	}
	status, code := 0, ""
	var pg *pgconn.PgError
	switch {
	case errors.Is(err, data.ErrInvalid), errors.Is(err, data.ErrBudget):
		status, code = 400, "invalid_record_access"
	case errors.Is(err, data.ErrDenied):
		status, code = 403, "forbidden"
	case errors.Is(err, data.ErrNotFound):
		status, code = 404, "not_found"
	case errors.Is(err, data.ErrBinding):
		status, code = 409, "dataset_conflict"
	case errors.Is(err, data.ErrConflict):
		status, code = 409, "record_conflict"
	case errors.As(err, &pg):
		switch pg.Code {
		case "22023", "22P02":
			status, code = 400, "invalid_record_access"
		case "42501":
			status, code = 403, "forbidden"
		case "55000", "23503", "55P03", "40001", "40P01":
			status, code = 409, "record_conflict"
		}
	}
	if status != 0 {
		return reply{}, &requestRejection{response: denied(status, code, "Record access request rejected; reload the record access before retrying")}
	}
	return reply{}, err
}

func recordAccessRequest(ctx context.Context, q postgres.Executor, r *http.Request, scope data.Scope, access tenant.Access, evidence *audit.Entry, guard data.Guard, resolve data.ScopeResolver) (reply, error) {
	entity, id := r.PathValue("entity"), r.PathValue("record")
	if len(entity) > 160 || !tenant.ValidID(id) {
		return recordAccessReply(data.Access{}, data.ErrInvalid)
	}
	evidence.State.EntityID, evidence.State.RecordID = entity, id
	store := data.Store{}
	if r.Method == http.MethodGet {
		v, err := store.RecordAccess(ctx, q, scope, access, entity, id, guard, resolve)
		evidence.State.RecordVersion = v.Version
		return recordAccessReply(v, err)
	}
	var command data.AccessCommand
	if err := readDataCommand(r, &command); err != nil {
		return recordAccessReply(data.Access{}, data.ErrInvalid)
	}
	switch command.Operation {
	case "owner", "share", "unshare":
		evidence.State.AccessOperation = command.Operation
	}
	if tenant.ValidID(command.UserID) {
		evidence.State.PolicySubject = command.UserID
	}
	if tenant.ValidID(command.TeamID) {
		evidence.State.AccessTeamID = command.TeamID
	}
	v, err := store.AdministerAccess(ctx, q, scope, access, entity, id, command, guard, resolve)
	if err == nil {
		evidence.Action = "records.access.changed"
		evidence.Resource = "record"
		evidence.State.RecordVersion = v.Version
	}
	return recordAccessReply(v, err)
}
