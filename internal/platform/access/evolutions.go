package access

import (
	"context"
	"errors"
	"net/http"
	"regexp"
	"strings"

	"github.com/Karthikeyan49/Framwork/internal/audit"
	"github.com/Karthikeyan49/Framwork/internal/compiler"
	"github.com/Karthikeyan49/Framwork/internal/platform/postgres"
	"github.com/Karthikeyan49/Framwork/internal/runtime/data"
	"github.com/Karthikeyan49/Framwork/internal/tenant"
	"github.com/jackc/pgx/v5/pgconn"
)

var evolutionPOST = regexp.MustCompile(`^/v1/tenants/[0-9a-f-]{36}/preview/app\.[a-z][a-z0-9_]*(\.[a-z][a-z0-9_]*)*/datasets/[0-9a-f-]{36}/evolutions(/[0-9a-f-]{36}/(advance|activate|abort|reverify))?$`)

func evolutionReply(result any, err error) (reply, error) {
	if err == nil {
		return reply{status: 200, body: result}, nil
	}
	status, code := 0, ""
	var pg *pgconn.PgError
	switch {
	case errors.Is(err, data.ErrInvalid), errors.Is(err, data.ErrBudget):
		status, code = 400, "invalid_evolution"
	case errors.Is(err, data.ErrDenied):
		status, code = 403, "forbidden"
	case errors.Is(err, data.ErrNotFound):
		status, code = 404, "not_found"
	case errors.Is(err, data.ErrConflict), errors.Is(err, data.ErrBinding):
		status, code = 409, "evolution_conflict"
	case errors.As(err, &pg):
		switch pg.Code {
		case "22023", "23514":
			status, code = 400, "invalid_evolution"
		case "42501":
			status, code = 403, "forbidden"
		case "55000", "55P03", "40001", "40P01", "23505", "23503":
			status, code = 409, "evolution_conflict"
		}
	}
	if status != 0 {
		return reply{}, &requestRejection{response: denied(status, code, "Evolution rejected; inspect its durable checkpoint before retrying")}
	}
	return reply{}, err
}

func (a API) evolution(ctx context.Context, q postgres.Executor, r *http.Request, access tenant.Access, evidence *audit.Entry) (reply, error) {
	app, ds, id := r.PathValue("app"), r.PathValue("dataset"), r.PathValue("evolution")
	store := data.DatasetStore{}
	evidence.State.DatasetID = ds
	report := func(x data.Evolution, err error) (reply, error) {
		if err == nil {
			evidence.State.EvolutionID = x.ID
			evidence.State.EvolutionState = x.State
			evidence.State.EvolutionCheckpoint = x.Checkpoint
			evidence.State.EvolutionPlanHash = x.PlanHash
			evidence.State.VerifiedRows = x.VerifiedRows
			evidence.State.ManifestHash = x.TargetManifest
		}
		return evolutionReply(x, err)
	}
	if r.Method == http.MethodGet {
		x, err := store.Evolution(ctx, q, access, app, ds, id)
		return report(x, err)
	}
	if id == "" {
		var body struct {
			datasetWitness
			OperationID     string                   `json:"operation_id"`
			ExpectedBinding int64                    `json:"expected_binding"`
			Intent          compiler.EvolutionIntent `json:"intent"`
		}
		if readDatasetRequest(r, &body) != nil {
			return evolutionReply(nil, data.ErrInvalid)
		}
		p, v, err := a.witness(ctx, q, access, app, body.datasetWitness, false)
		if err != nil {
			return evolutionReply(nil, err)
		}
		metadataEvidence(ctx, evidence, v)
		evidence.State.IRHash = p.Hash()
		x, err := store.PrepareEvolution(ctx, q, access, app, ds, body.OperationID, body.ExpectedBinding, p, body.Revision, body.Intent)
		return report(x, err)
	}
	var body struct {
		ExpectedCheckpoint int64 `json:"expected_checkpoint"`
	}
	if readDataCommand(r, &body) != nil {
		return evolutionReply(nil, data.ErrInvalid)
	}
	if strings.HasSuffix(r.URL.Path, "/advance") {
		x, err := store.AdvanceEvolution(ctx, q, access, app, ds, id, body.ExpectedCheckpoint)
		return report(x, err)
	}
	action := "ACTIVATE"
	if strings.HasSuffix(r.URL.Path, "/abort") {
		action = "ABORT"
	}
	if strings.HasSuffix(r.URL.Path, "/reverify") {
		action = "REVERIFY"
	}
	x, err := store.FinishEvolution(ctx, q, access, app, ds, id, body.ExpectedCheckpoint, action)
	return report(x, err)
}
