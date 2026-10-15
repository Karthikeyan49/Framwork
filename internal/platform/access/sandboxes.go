package access

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"regexp"
	"strings"

	"github.com/Karthikeyan49/Framwork/internal/audit"
	"github.com/Karthikeyan49/Framwork/internal/identity"
	"github.com/Karthikeyan49/Framwork/internal/metadata"
	"github.com/Karthikeyan49/Framwork/internal/platform/postgres"
	"github.com/Karthikeyan49/Framwork/internal/runtime/data"
	"github.com/Karthikeyan49/Framwork/internal/tenant"
	"github.com/Karthikeyan49/Framwork/internal/tools"
	"github.com/jackc/pgx/v5/pgconn"
)

var sandboxPOST = regexp.MustCompile(`^/v1/tenants/[0-9a-f-]{36}/preview/app\.[a-z][a-z0-9_]*(\.[a-z][a-z0-9_]*)*/sandboxes(/[0-9a-f-]{36}/freeze)?$`)

// Exact registered patterns; a suffix match could capture unrelated routes.
var sandboxPatterns = map[string]bool{
	"POST /v1/tenants/{tenant}/preview/{app}/sandboxes":                   true,
	"GET /v1/tenants/{tenant}/preview/{app}/sandboxes/{sandbox}":          true,
	"POST /v1/tenants/{tenant}/preview/{app}/sandboxes/{sandbox}/freeze":  true,
	"GET /v1/tenants/{tenant}/preview/{app}/sandboxes/{sandbox}/evidence": true,
}

func sandboxReply(value any, err error) (reply, error) {
	if err == nil {
		return reply{status: 200, body: value}, nil
	}
	status, code := 0, ""
	var pg *pgconn.PgError
	switch {
	case errors.Is(err, data.ErrInvalid):
		status, code = 400, "invalid_sandbox"
	case errors.Is(err, data.ErrNotFound), errors.Is(err, metadata.ErrNotFound):
		status, code = 404, "not_found"
	case errors.Is(err, data.ErrConflict), errors.Is(err, data.ErrBinding):
		status, code = 409, "sandbox_conflict"
	case errors.As(err, &pg):
		switch pg.Code {
		case "22023":
			status, code = 400, "invalid_sandbox"
		case "42501":
			status, code = 403, "forbidden"
		case "55000", "23505", "23503", "40001", "40P01", "55P03":
			status, code = 409, "sandbox_conflict"
		}
	}
	if status != 0 {
		return reply{}, &requestRejection{response: denied(status, code, "Sandbox request rejected; reload the sandbox before retrying")}
	}
	return reply{}, err
}

func (a API) sandbox(ctx context.Context, q postgres.Executor, r *http.Request, u identity.User, access tenant.Access, evidence *audit.Entry) (reply, error) {
	store := data.DatasetStore{}
	app, id := r.PathValue("app"), r.PathValue("sandbox")
	report := func(d data.Dataset, err error) (reply, error) {
		if err == nil {
			evidence.State.DatasetID = d.ID
			evidence.State.DataSchemaHash = d.SchemaHash
			evidence.State.DatasetBinding = d.ActiveBinding
			evidence.State.DatasetState = d.State
			if d.Sandbox != nil {
				evidence.State.ToolMode = d.Sandbox.ToolCeiling
				evidence.State.ManifestHash = d.Sandbox.EvidenceHash
			}
		}
		return sandboxReply(d, err)
	}
	if id == "" {
		var body struct {
			datasetWitness
			WorkspaceID string `json:"workspace_id"`
			ToolCeiling string `json:"tool_ceiling"`
		}
		if readDatasetRequest(r, &body) != nil {
			return sandboxReply(nil, data.ErrInvalid)
		}
		p, v, err := a.witness(ctx, q, access, app, body.datasetWitness, false)
		if err != nil {
			return sandboxReply(nil, err)
		}
		metadataEvidence(ctx, evidence, v)
		evidence.State.IRHash = p.Hash()
		d, err := store.CreateSandbox(ctx, q, access, p, body.Revision, body.WorkspaceID, body.ToolCeiling)
		return report(d, err)
	}
	if !tenant.ValidID(id) {
		return sandboxReply(nil, data.ErrInvalid)
	}
	evidence.State.DatasetID = id
	switch {
	case r.Method == http.MethodGet && !strings.HasSuffix(r.URL.Path, "/evidence"):
		d, err := store.Get(ctx, q, access, app, id)
		if err == nil && d.Environment != "sandbox" {
			err = data.ErrNotFound
		}
		return report(d, err)
	case r.Method == http.MethodGet:
		e, err := store.Evidence(ctx, q, access, app, id)
		return sandboxReply(e, err)
	default:
		var body struct{}
		if readDatasetRequest(r, &body) != nil {
			return sandboxReply(nil, data.ErrInvalid)
		}
		catalog := a.ToolCatalog.Hash()
		if catalog == "" {
			catalog = tools.EmptyCatalog().Hash()
		}
		policy := sha256.Sum256([]byte(evidence.PolicyVersion))
		e, err := store.FreezeSandbox(ctx, q, access, app, id, catalog, hex.EncodeToString(policy[:]))
		if err == nil {
			evidence.State.ArtifactHash = e.Hash
			evidence.Action = "sandbox.frozen"
		}
		return sandboxReply(e, err)
	}
}
