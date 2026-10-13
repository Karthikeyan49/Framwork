package access

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/Karthikeyan49/Framwork/internal/audit"
	"github.com/Karthikeyan49/Framwork/internal/compiler"
	"github.com/Karthikeyan49/Framwork/internal/identity"
	"github.com/Karthikeyan49/Framwork/internal/metadata"
	"github.com/Karthikeyan49/Framwork/internal/platform/postgres"
	"github.com/Karthikeyan49/Framwork/internal/runtime/data"
	"github.com/Karthikeyan49/Framwork/internal/tenant"
)

var datasetManagementPOST = regexp.MustCompile(`^/v1/tenants/[0-9a-f-]{36}/preview/app\.[a-z][a-z0-9_]*(\.[a-z][a-z0-9_]*)*/datasets(/[0-9a-f-]{36}/(verify|bindings))?$`)
var datasetDataPOST = regexp.MustCompile(`^/v1/tenants/[0-9a-f-]{36}/preview/app\.[a-z][a-z0-9_]*(\.[a-z][a-z0-9_]*)*/datasets/[0-9a-f-]{36}/bindings/[0-9]+/entities/entity\.[a-z][a-z0-9_]*(\.[a-z][a-z0-9_]*)*/(query|commands)$`)

type datasetWitness struct {
	Revision int64           `json:"metadata_revision"`
	IRHash   string          `json:"ir_hash"`
	IR       json.RawMessage `json:"compiled_ir"`
}

// Bound authoring requests retain duplicate-key rejection and the existing
// metadata JSON byte budget. Larger artifacts are rejected, never truncated.
func readDatasetRequest(r *http.Request, dest any) error {
	typ, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || typ != "application/json" {
		return data.ErrInvalid
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, metadata.MaxBytes+4097))
	if err != nil {
		return data.ErrInvalid
	}
	_, err = metadata.DecodeJSON(raw)
	if err != nil {
		return data.ErrInvalid
	}
	// Keep canonical artifact bytes/order intact after duplicate-key validation.
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	d.DisallowUnknownFields()
	if d.Decode(dest) != nil {
		return data.ErrInvalid
	}
	return nil
}

func (a API) witness(ctx context.Context, q postgres.Executor, access tenant.Access, app string, w datasetWitness, historicalAdoption bool) (compiler.Program, metadata.Version, error) {
	if w.Revision < 1 || w.Revision > 1e9 {
		return compiler.Program{}, metadata.Version{}, data.ErrInvalid
	}
	v, err := a.Registry.Get(ctx, q, access, app, w.Revision)
	if err != nil {
		return compiler.Program{}, v, err
	}
	p, err := compiler.Restore(w.IR, w.IRHash, compiler.SupportedIRVersions())
	if err != nil {
		return compiler.Program{}, v, data.ErrInvalid
	}
	source, err := a.Registry.Parse(v.Document)
	if err != nil {
		return compiler.Program{}, v, err
	}
	if p.IR().Application.AppID != app || !compiler.MatchesStoredSource(p, source) {
		return compiler.Program{}, v, data.ErrInvalid
	}
	if !historicalAdoption {
		// New admissions run the trusted compiler once and bind its exact bytes.
		// Restore is a reader, not provenance for a caller-supplied catalog/flags.
		compiled, diagnostics := compiler.Compile(source, a.catalog())
		if !compiled.Valid() || len(diagnostics) != 0 || compiled.Hash() != p.Hash() {
			return compiler.Program{}, v, data.ErrInvalid
		}
		p = compiled
	}
	// Historical ADOPT is admitted only when Create also proves that this exact
	// hash/revision already names the chosen legacy namespace; never recompile it.
	return p, v, nil
}

func datasetResult(result any, err error) (reply, error) {
	switch {
	case err == nil:
		return reply{status: 200, body: result}, nil
	case errors.Is(err, data.ErrInvalid):
		return denied(400, "invalid_dataset", "Dataset request or schema binding is unsupported"), nil
	case errors.Is(err, data.ErrConflict), errors.Is(err, data.ErrBinding):
		return denied(409, "dataset_conflict", "Dataset is sealed or its binding has changed"), nil
	case errors.Is(err, data.ErrNotFound), errors.Is(err, metadata.ErrNotFound):
		return denied(404, "not_found", "Dataset or source witness not found"), nil
	default:
		return reply{}, err
	}
}

func (a API) dataset(ctx context.Context, q postgres.Executor, r *http.Request, u identity.User, access tenant.Access, evidence *audit.Entry) (reply, error) {
	store := data.DatasetStore{}
	app, id := r.PathValue("app"), r.PathValue("dataset")
	report := func(d data.Dataset, err error) (reply, error) {
		if err == nil {
			evidence.State.DatasetID = d.ID
			evidence.State.DataSchemaHash = d.SchemaHash
			evidence.State.DataSchemaGeneration = d.SchemaGeneration
			evidence.State.DatasetBinding = d.ActiveBinding
			evidence.State.DatasetState = d.State
			evidence.State.VerifiedRows = d.VerifiedRows
			evidence.State.ManifestHash = d.ManifestHash
		}
		return datasetResult(d, err)
	}
	if id == "" {
		var body struct {
			datasetWitness
			WorkspaceID string `json:"workspace_id"`
			Mode        string `json:"mode"`
		}
		if readDatasetRequest(r, &body) != nil {
			return datasetResult(nil, data.ErrInvalid)
		}
		p, v, err := a.witness(ctx, q, access, app, body.datasetWitness, body.Mode == "ADOPT")
		if err != nil {
			return datasetResult(nil, err)
		}
		metadataEvidence(ctx, evidence, v)
		evidence.State.IRHash = p.Hash()
		d, err := store.Create(ctx, q, access, p, body.Revision, body.WorkspaceID, body.Mode)
		return report(d, err)
	}
	if r.PathValue("binding") == "" {
		if r.Method == http.MethodGet {
			d, err := store.Get(ctx, q, access, app, id)
			return report(d, err)
		}
		if strings.HasSuffix(r.URL.Path, "/verify") {
			var body struct{}
			if readDatasetRequest(r, &body) != nil {
				return datasetResult(nil, data.ErrInvalid)
			}
			d, err := store.VerifyAdoption(ctx, q, access, app, id)
			return report(d, err)
		}
		var body struct {
			datasetWitness
			ExpectedBinding int64 `json:"expected_binding"`
		}
		if readDatasetRequest(r, &body) != nil {
			return datasetResult(nil, data.ErrInvalid)
		}
		p, v, err := a.witness(ctx, q, access, app, body.datasetWitness, false)
		if err != nil {
			return datasetResult(nil, err)
		}
		metadataEvidence(ctx, evidence, v)
		evidence.State.IRHash = p.Hash()
		d, err := store.Bind(ctx, q, access, app, id, body.ExpectedBinding, p, body.Revision)
		return report(d, err)
	}
	version, err := strconv.ParseInt(r.PathValue("binding"), 10, 64)
	if err != nil {
		return datasetResult(nil, data.ErrInvalid)
	}
	scope, err := store.Scope(ctx, q, access, app, id, version)
	if err != nil {
		return datasetResult(nil, err)
	}
	// Read the original immutable source only to verify the stored witness. No
	// Compile call or active-pointer fallback is used for any binding execution.
	v, err := a.Registry.Get(ctx, q, access, app, scope.Revision())
	if err != nil {
		return datasetResult(nil, err)
	}
	source, err := a.Registry.Parse(v.Document)
	if err != nil {
		return reply{}, err
	}
	if !compiler.MatchesStoredSource(scope.Program(), source) {
		return reply{}, data.ErrUnavailable
	}
	metadataEvidence(ctx, evidence, v)
	evidence.State.IRHash = scope.Hash()
	evidence.State.DatasetID = scope.DatasetID()
	evidence.State.DataSchemaGeneration = 1
	evidence.State.DatasetBinding = scope.BindingVersion()
	return a.executePreview(ctx, q, r, u, access, evidence, scope)
}
