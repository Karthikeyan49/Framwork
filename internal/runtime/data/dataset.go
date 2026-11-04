package data

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strconv"

	"github.com/Karthikeyan49/Framwork/internal/compiler"
	"github.com/Karthikeyan49/Framwork/internal/metadata"
	"github.com/Karthikeyan49/Framwork/internal/platform/postgres"
	"github.com/Karthikeyan49/Framwork/internal/tenant"
)

type Dataset struct {
	ID               string `json:"id"`
	WorkspaceID      string `json:"workspace_id"`
	Environment      string `json:"environment"`
	SchemaHash       string `json:"schema_hash"`
	SchemaGeneration int64  `json:"schema_generation"`
	ActiveBinding    int64  `json:"active_binding"`
	State            string `json:"state"`
	VerifiedRows     int64  `json:"verified_rows"`
	ManifestHash     string `json:"manifest_hash"`
	// Sandbox is server-derived (ADR101); it is never accepted from a request.
	Sandbox *Sandbox `json:"sandbox,omitempty"`
}

// Sandbox describes the isolation posture of a synthetic sandbox dataset.
type Sandbox struct {
	DataMode     string `json:"data_mode"`
	ToolCeiling  string `json:"tool_ceiling"`
	State        string `json:"state"`
	EvidenceHash string `json:"evidence_hash,omitempty"`
}

type DatasetStore struct{}

// Create adopts only an explicitly chosen, exact witnessed namespace. ADOPT
// seals writes until every record is verified in bounded durable batches. EMPTY
// uses an independent anchor, even for the same source/compiler/app.
func (DatasetStore) Create(ctx context.Context, q postgres.Executor, a tenant.Access, p compiler.Program, revision int64, workspace, mode string) (Dataset, error) {
	return createDataset(ctx, q, a, p, revision, workspace, mode, "preview", "")
}

// CreateSandbox creates an independent, empty synthetic dataset. Only EMPTY is
// possible: a sandbox never adopts a preview or legacy namespace. The tool
// ceiling can only reduce what each tool's own declared sandbox mode allows.
func (DatasetStore) CreateSandbox(ctx context.Context, q postgres.Executor, a tenant.Access, p compiler.Program, revision int64, workspace, ceiling string) (Dataset, error) {
	switch ceiling {
	case "BLOCK", "SIMULATE", "READ_ONLY", "REROUTE", "TEST_ACCOUNT":
	default:
		return Dataset{}, ErrInvalid
	}
	return createDataset(ctx, q, a, p, revision, workspace, "EMPTY", "sandbox", ceiling)
}

func createDataset(ctx context.Context, q postgres.Executor, a tenant.Access, p compiler.Program, revision int64, workspace, mode, environment, ceiling string) (Dataset, error) {
	var d Dataset
	if !tenant.ValidID(workspace) || !p.Valid() || revision < 1 || revision > 1e9 || (mode != "ADOPT" && mode != "EMPTY") {
		return d, ErrInvalid
	}
	_, schema, err := compiler.Storage(p)
	if err != nil {
		return d, ErrInvalid
	}
	// A caller-retained workspace UUID is also the create reconciliation key.
	// A lost COMMIT reply cannot create a second dataset on an exact retry.
	var existingID, existingHash, existingEnvironment string
	var existingRevision int64
	err = q.QueryRowContext(ctx, `SELECT d.id::text,b.ir_hash,b.metadata_revision,d.environment FROM runtime.datasets d JOIN runtime.dataset_bindings b ON b.tenant_id=d.tenant_id AND b.app_id=d.app_id AND b.dataset_id=d.id AND b.version=1 WHERE d.tenant_id=$1 AND d.app_id=$2 AND d.workspace_id=$3`, a.TenantID(), p.IR().Application.AppID, workspace).Scan(&existingID, &existingHash, &existingRevision, &existingEnvironment)
	if err == nil {
		if existingHash != p.Hash() || existingRevision != revision || existingEnvironment != environment {
			return d, ErrConflict
		}
		var adopted bool
		if err = q.QueryRowContext(ctx, `SELECT anchor_revision=$4 AND anchor_hash=$5 FROM runtime.datasets WHERE tenant_id=$1 AND app_id=$2 AND id=$3`, a.TenantID(), p.IR().Application.AppID, existingID, revision, p.Hash()).Scan(&adopted); err != nil {
			return d, err
		}
		if adopted != (mode == "ADOPT") {
			return d, ErrConflict
		}
		existing, getErr := (DatasetStore{}).Get(ctx, q, a, p.IR().Application.AppID, existingID)
		if getErr == nil && environment == "sandbox" && (existing.Sandbox == nil || existing.Sandbox.ToolCeiling != ceiling) {
			return d, ErrConflict
		}
		return existing, getErr
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return d, err
	}
	id, err := newID()
	if err != nil {
		return d, err
	}
	anchorRevision, anchorHash, state := revision, p.Hash(), "ADOPTING"
	if mode == "EMPTY" {

		sum := sha256.Sum256([]byte("dataset-v1/" + id + "/" + p.Hash()))
		anchorRevision, anchorHash, state = 1, hex.EncodeToString(sum[:]), "OPEN"
		if _, err = q.ExecContext(ctx, `INSERT INTO runtime.record_sets(tenant_id,app_id,metadata_revision,ir_hash) VALUES($1,$2,$3,$4)`, a.TenantID(), p.IR().Application.AppID, anchorRevision, anchorHash); err != nil {
			return d, err
		}
	} else {
		var exists bool
		if err = q.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM runtime.datasets WHERE tenant_id=$1 AND app_id=$2 AND anchor_revision=$3 AND anchor_hash=$4)`, a.TenantID(), p.IR().Application.AppID, anchorRevision, anchorHash).Scan(&exists); err != nil {
			return d, err
		}
		if exists {
			return d, ErrConflict
		}
		// The old dataset counter is an adoption barrier for already running RR
		// legacy writers. R4 will retain the barrier while removing per-write bumps.
		result, er := q.ExecContext(ctx, `UPDATE runtime.record_sets SET write_version=write_version+1 WHERE tenant_id=$1 AND app_id=$2 AND metadata_revision=$3 AND ir_hash=$4`, a.TenantID(), p.IR().Application.AppID, anchorRevision, anchorHash)
		if er != nil {
			return d, er
		}
		n, er := result.RowsAffected()
		if er != nil {
			return d, er
		}
		if n != 1 {
			return d, ErrNotFound
		}
	}
	if _, err = q.ExecContext(ctx, `INSERT INTO runtime.datasets(tenant_id,app_id,id,workspace_id,anchor_revision,anchor_hash,schema_hash,state,environment) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)`, a.TenantID(), p.IR().Application.AppID, id, workspace, anchorRevision, anchorHash, schema, state, environment); err != nil {
		return d, err
	}
	if err = insertBinding(ctx, q, a, p, revision, id, 1, schema); err != nil {
		return d, err
	}
	if environment == "sandbox" {
		if _, err = q.ExecContext(ctx, `SELECT runtime.register_sandbox($1,$2,$3,$4)`, a.TenantID(), p.IR().Application.AppID, id, ceiling); err != nil {
			return d, err
		}
	}
	return (DatasetStore{}).Get(ctx, q, a, p.IR().Application.AppID, id)
}

func insertBinding(ctx context.Context, q postgres.Executor, a tenant.Access, p compiler.Program, revision int64, id string, version int64, schema string, generation ...int64) error {
	gen := int64(1)
	if len(generation) == 1 {
		gen = generation[0]
	}
	_, err := q.ExecContext(ctx, `INSERT INTO runtime.dataset_bindings(tenant_id,app_id,dataset_id,version,metadata_revision,ir_hash,ir_body,schema_hash,generation) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)`, a.TenantID(), p.IR().Application.AppID, id, version, revision, p.Hash(), []byte(p.JSON()), schema, gen)
	return err
}

func (DatasetStore) Get(ctx context.Context, q postgres.Executor, a tenant.Access, app, id string) (Dataset, error) {
	var d Dataset
	if !tenant.ValidID(id) {
		return d, ErrInvalid
	}
	err := q.QueryRowContext(ctx, `SELECT id::text,workspace_id::text,environment,schema_hash,schema_generation,active_binding,state,verified_rows,manifest_hash FROM runtime.datasets WHERE tenant_id=$1 AND app_id=$2 AND id=$3`, a.TenantID(), app, id).Scan(&d.ID, &d.WorkspaceID, &d.Environment, &d.SchemaHash, &d.SchemaGeneration, &d.ActiveBinding, &d.State, &d.VerifiedRows, &d.ManifestHash)
	if errors.Is(err, sql.ErrNoRows) {
		return d, ErrNotFound
	}
	if err != nil || d.Environment != "sandbox" {
		return d, err
	}
	sb := Sandbox{}
	err = q.QueryRowContext(ctx, `SELECT s.data_mode,s.tool_ceiling,s.state,coalesce(e.body_hash,'') FROM runtime.sandboxes s LEFT JOIN runtime.sandbox_evidence e ON e.tenant_id=s.tenant_id AND e.app_id=s.app_id AND e.dataset_id=s.dataset_id WHERE s.tenant_id=$1 AND s.app_id=$2 AND s.dataset_id=$3`, a.TenantID(), app, id).Scan(&sb.DataMode, &sb.ToolCeiling, &sb.State, &sb.EvidenceHash)
	if err != nil {
		return d, err
	}
	d.Sandbox = &sb
	return d, nil
}

// SandboxEvidence is the immutable freeze record bound to exact inputs.
type SandboxEvidence struct {
	Hash     string          `json:"hash"`
	Body     json.RawMessage `json:"body"`
	FrozenBy string          `json:"frozen_by"`
}

// FreezeSandbox seals the sandbox and records immutable evidence. The catalog
// and policy hashes are server-computed by the caller (never request fields).
func (DatasetStore) FreezeSandbox(ctx context.Context, q postgres.Executor, a tenant.Access, app, id, catalogHash, policyHash string) (SandboxEvidence, error) {
	if !tenant.ValidID(id) || len(catalogHash) != 64 || len(policyHash) != 64 {
		return SandboxEvidence{}, ErrInvalid
	}
	var hash string
	if err := q.QueryRowContext(ctx, `SELECT runtime.freeze_sandbox($1,$2,$3,$4,$5)`, a.TenantID(), app, id, catalogHash, policyHash).Scan(&hash); err != nil {
		return SandboxEvidence{}, err
	}
	return (DatasetStore{}).Evidence(ctx, q, a, app, id)
}
func (DatasetStore) Evidence(ctx context.Context, q postgres.Executor, a tenant.Access, app, id string) (SandboxEvidence, error) {
	var e SandboxEvidence
	if !tenant.ValidID(id) {
		return e, ErrInvalid
	}
	err := q.QueryRowContext(ctx, `SELECT body_hash,body,frozen_by::text FROM runtime.sandbox_evidence WHERE tenant_id=$1 AND app_id=$2 AND dataset_id=$3`, a.TenantID(), app, id).Scan(&e.Hash, &e.Body, &e.FrozenBy)
	if errors.Is(err, sql.ErrNoRows) {
		return e, ErrNotFound
	}
	return e, err
}

// Bind currently permits identical storage contracts. Optional-addition reader
// windows and transformed generations require the next independently reviewed
// tranche; a COMPATIBLE label alone does not bypass this stronger gate.
func (DatasetStore) Bind(ctx context.Context, q postgres.Executor, a tenant.Access, app, id string, expected int64, p compiler.Program, revision int64) (Dataset, error) {
	if !tenant.ValidID(id) || expected < 1 || expected >= 1e9 || !p.Valid() || p.IR().Application.AppID != app || revision < 1 || revision > 1e9 {
		return Dataset{}, ErrInvalid
	}
	var current int64
	var generation int64
	var schema, state string
	err := q.QueryRowContext(ctx, `SELECT active_binding,schema_hash,state,schema_generation FROM runtime.datasets WHERE tenant_id=$1 AND app_id=$2 AND id=$3 FOR UPDATE`, a.TenantID(), app, id).Scan(&current, &schema, &state, &generation)
	if errors.Is(err, sql.ErrNoRows) {
		return Dataset{}, ErrNotFound
	}
	if err != nil {
		return Dataset{}, err
	}
	_, newSchema, err := compiler.Storage(p)
	if err != nil {
		return Dataset{}, ErrInvalid
	}
	if current != expected || state != "OPEN" {
		return Dataset{}, ErrBinding
	}
	if newSchema != schema {
		return Dataset{}, ErrInvalid
	}
	if err = insertBinding(ctx, q, a, p, revision, id, current+1, schema, generation); err != nil {
		return Dataset{}, err
	}
	if _, err = q.ExecContext(ctx, `UPDATE runtime.datasets SET active_binding=active_binding+1,epoch=epoch+1 WHERE tenant_id=$1 AND app_id=$2 AND id=$3`, a.TenantID(), app, id); err != nil {
		return Dataset{}, err
	}
	return (DatasetStore{}).Get(ctx, q, a, app, id)
}

func (DatasetStore) Scope(ctx context.Context, q postgres.Executor, a tenant.Access, app, id string, version int64) (Scope, error) {
	if !tenant.ValidID(id) || version < 1 || version > 1e9 {
		return Scope{}, ErrInvalid
	}
	var raw []byte
	var hash, schema, state string
	var revision, anchorRevision int64
	var anchorHash string
	var generation int64
	err := q.QueryRowContext(ctx, `SELECT b.ir_body,b.ir_hash,b.metadata_revision,b.schema_hash,d.state,g.anchor_revision,g.anchor_hash,b.generation FROM runtime.dataset_bindings b JOIN runtime.datasets d ON d.tenant_id=b.tenant_id AND d.app_id=b.app_id AND d.id=b.dataset_id JOIN runtime.dataset_generations g ON g.tenant_id=b.tenant_id AND g.app_id=b.app_id AND g.dataset_id=b.dataset_id AND g.generation=b.generation WHERE b.tenant_id=$1 AND b.app_id=$2 AND b.dataset_id=$3 AND b.version=$4`, a.TenantID(), app, id, version).Scan(&raw, &hash, &revision, &schema, &state, &anchorRevision, &anchorHash, &generation)
	if errors.Is(err, sql.ErrNoRows) {
		return Scope{}, ErrNotFound
	}
	if err != nil {
		return Scope{}, err
	}
	if state == "ADOPTING" {
		return Scope{}, ErrBinding
	}
	p, err := compiler.Restore(raw, hash, compiler.SupportedIRVersions())
	if err != nil {
		return Scope{}, ErrUnavailable
	}
	_, actual, err := compiler.Storage(p)
	if err != nil || actual != schema || p.IR().Application.AppID != app {
		return Scope{}, ErrUnavailable
	}
	s, err := NewScope(p, revision)
	if err != nil {
		return Scope{}, err
	}
	s.anchorRevision, s.anchorHash, s.datasetID, s.binding = anchorRevision, anchorHash, id, version
	s.generation = generation
	return s, nil
}

func lockBinding(ctx context.Context, q postgres.Executor, s Scope, a tenant.Access) error {
	var id, state string
	var version int64
	var generation, mappedGeneration int64
	err := q.QueryRowContext(ctx, `SELECT d.id::text,d.state,d.active_binding,d.schema_generation,g.generation FROM runtime.dataset_generations g JOIN runtime.datasets d ON d.tenant_id=g.tenant_id AND d.app_id=g.app_id AND d.id=g.dataset_id WHERE g.tenant_id=$1 AND g.app_id=$2 AND g.anchor_revision=$3 AND g.anchor_hash=$4 FOR SHARE OF d`, a.TenantID(), s.AppID(), s.DataRevision(), s.DataHash()).Scan(&id, &state, &version, &generation, &mappedGeneration)
	if errors.Is(err, sql.ErrNoRows) {
		if s.DatasetID() != "" {
			return ErrBinding
		}
		return nil
	}
	if err != nil {
		return err
	}
	if state != "OPEN" || id != s.DatasetID() || version != s.BindingVersion() || generation != s.Generation() || mappedGeneration != generation {
		return ErrBinding
	}
	_, err = q.ExecContext(ctx, `SELECT pg_catalog.set_config('app.dataset_id',$1,true),pg_catalog.set_config('app.dataset_binding',$2,true),pg_catalog.set_config('app.execution_ir_hash',$3,true)`, id, strconv.FormatInt(version, 10), s.Hash())
	return err
}

func (DatasetStore) Writable(ctx context.Context, q postgres.Executor, s Scope, a tenant.Access) (bool, error) {
	var id, state string
	var binding int64
	var generation, currentGeneration int64
	err := q.QueryRowContext(ctx, `SELECT d.id::text,d.state,d.active_binding,g.generation,d.schema_generation FROM runtime.dataset_generations g JOIN runtime.datasets d ON d.tenant_id=g.tenant_id AND d.app_id=g.app_id AND d.id=g.dataset_id WHERE g.tenant_id=$1 AND g.app_id=$2 AND g.anchor_revision=$3 AND g.anchor_hash=$4`, a.TenantID(), s.AppID(), s.DataRevision(), s.DataHash()).Scan(&id, &state, &binding, &generation, &currentGeneration)
	if errors.Is(err, sql.ErrNoRows) {
		return s.DatasetID() == "", nil
	}
	if err != nil {
		return false, err
	}
	return id == s.DatasetID() && state == "OPEN" && binding == s.BindingVersion() && generation == currentGeneration && generation == s.Generation(), nil
}

// VerifyAdoption commits progress with its bounded verification transaction. A
// repeated request reads the durable cursor; it never repeats a committed batch.
// Source rows remain sealed and unchanged, with owner/team/share/link evidence.
func (DatasetStore) VerifyAdoption(ctx context.Context, q postgres.Executor, a tenant.Access, app, id string) (Dataset, error) {
	if !tenant.ValidID(id) {
		return Dataset{}, ErrInvalid
	}
	var state, entity, hash string
	var rowID sql.NullString
	var count, revision int64
	var anchorHash string
	err := q.QueryRowContext(ctx, `SELECT state,verified_entity,verified_id::text,verified_rows,manifest_hash,anchor_revision,anchor_hash FROM runtime.datasets WHERE tenant_id=$1 AND app_id=$2 AND id=$3 FOR UPDATE`, a.TenantID(), app, id).Scan(&state, &entity, &rowID, &count, &hash, &revision, &anchorHash)
	if errors.Is(err, sql.ErrNoRows) {
		return Dataset{}, ErrNotFound
	}
	if err != nil {
		return Dataset{}, err
	}
	if state == "OPEN" {
		return (DatasetStore{}).Get(ctx, q, a, app, id)
	}
	if state != "ADOPTING" {
		return Dataset{}, ErrBinding
	}
	var raw []byte
	var witnessHash string
	if err = q.QueryRowContext(ctx, `SELECT ir_body,ir_hash FROM runtime.dataset_bindings WHERE tenant_id=$1 AND app_id=$2 AND dataset_id=$3 AND version=1`, a.TenantID(), app, id).Scan(&raw, &witnessHash); err != nil {
		return Dataset{}, err
	}
	p, err := compiler.Restore(raw, witnessHash, compiler.SupportedIRVersions())
	if err != nil || witnessHash != anchorHash {
		return Dataset{}, ErrUnavailable
	}
	rows, err := q.QueryContext(ctx, `SELECT r.entity_id,r.id::text,r.row_version,r.created_by::text,r.created_at,r.updated_at,r.custom_data,r.owner_user_id::text,coalesce(r.owner_team_id::text,''),r.schema_version,r.updated_by::text,
 (SELECT coalesce(jsonb_agg(jsonb_build_array(sh.user_id,sh.can_write) ORDER BY sh.user_id),'[]'::jsonb) FROM (SELECT user_id,can_write FROM runtime.record_shares sh WHERE sh.tenant_id=r.tenant_id AND sh.app_id=r.app_id AND sh.metadata_revision=r.metadata_revision AND sh.ir_hash=r.ir_hash AND sh.entity_id=r.entity_id AND sh.id=r.id ORDER BY sh.user_id LIMIT 257) sh),
 (SELECT coalesce(jsonb_agg(jsonb_build_array(l.field_id,l.target_entity,l.target_id) ORDER BY l.field_id),'[]'::jsonb) FROM (SELECT field_id,target_entity,target_id FROM runtime.record_links l WHERE l.tenant_id=r.tenant_id AND l.app_id=r.app_id AND l.metadata_revision=r.metadata_revision AND l.ir_hash=r.ir_hash AND l.entity_id=r.entity_id AND l.id=r.id ORDER BY l.field_id LIMIT 33) l)
 FROM runtime.records r WHERE r.tenant_id=$1 AND r.app_id=$2 AND r.metadata_revision=$3 AND r.ir_hash=$4 AND ($5='' OR (r.entity_id,r.id)>($5::text,$6::uuid)) ORDER BY r.entity_id,r.id LIMIT 101`, a.TenantID(), app, revision, anchorHash, entity, rowID)
	if err != nil {
		return Dataset{}, err
	}
	type verified struct {
		entity                   string
		schemaVersion, updatedBy string
		r                        Record
		raw, shares, links       json.RawMessage
	}
	batch := []verified{}
	for rows.Next() {
		var v verified
		if err = rows.Scan(&v.entity, &v.r.ID, &v.r.Version, &v.r.CreatedBy, &v.r.CreatedAt, &v.r.UpdatedAt, &v.raw, &v.r.OwnerID, &v.r.TeamID, &v.schemaVersion, &v.updatedBy, &v.shares, &v.links); err != nil {
			break
		}
		batch = append(batch, v)
	}
	if err == nil {
		err = rows.Err()
	}
	_ = rows.Close()
	if err != nil {
		return Dataset{}, err
	}
	done := len(batch) <= 100
	if !done {
		batch = batch[:100]
	}
	work, processed := 0, 0
	for _, v := range batch {
		e, ok := p.Entity(v.entity)
		if !ok {
			return Dataset{}, ErrUnavailable
		}
		record := v.r
		input, er := metadata.DecodeJSON(v.raw)
		if er != nil {
			return Dataset{}, ErrUnavailable
		}
		object, ok := input.(map[string]any)
		if !ok || v.schemaVersion != "1.0.0" {
			return Dataset{}, ErrUnavailable
		}
		record.Values, err = values(e, object, record.ID, true)
		if err != nil {
			return Dataset{}, ErrUnavailable
		}
		// Bound round trips as well as rows: a reference-heavy legacy record may
		// require target validation, projection insertion and equality lookup.
		cost := 1
		for _, f := range e.Fields {
			if f.Type == "reference" && record.Values[f.ID] != nil {
				cost += 3
			}
		}
		if cost > 256 {
			return Dataset{}, ErrBudget
		}
		if processed > 0 && work+cost > 256 {
			done = false
			break
		}
		work += cost
		processed++
		var shares [][]json.RawMessage
		var links [][]string
		if json.Unmarshal(v.shares, &shares) != nil || len(shares) > 256 || json.Unmarshal(v.links, &links) != nil || len(links) > 32 {
			return Dataset{}, ErrBudget
		}
		present := map[string]bool{}
		for _, link := range links {
			if len(link) != 3 {
				return Dataset{}, ErrUnavailable
			}
			f, ok := field(e, link[0])
			if !ok || f.Type != "reference" || f.ReferenceTo != link[1] || record.Values[f.ID] != link[2] {
				return Dataset{}, ErrUnavailable
			}
			present[f.ID] = true
		}
		complete := [][]string{}
		for _, f := range e.Fields {
			if f.Type != "reference" || record.Values[f.ID] == nil {
				continue
			}
			var exists bool
			if err = q.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM runtime.records WHERE tenant_id=$1 AND app_id=$2 AND metadata_revision=$3 AND ir_hash=$4 AND entity_id=$5 AND id=$6)`, a.TenantID(), app, revision, anchorHash, f.ReferenceTo, record.Values[f.ID]).Scan(&exists); err != nil {
				return Dataset{}, err
			}
			if !exists {
				return Dataset{}, ErrUnavailable
			}
			target := record.Values[f.ID].(string)
			if !present[f.ID] {
				// V2/v8 retained records may predate derived link projections. Add
				// only missing equal facts; never overwrite an unequal destination.
				_, err = q.ExecContext(ctx, `INSERT INTO runtime.record_links(tenant_id,app_id,metadata_revision,ir_hash,entity_id,id,field_id,target_entity,target_id) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9) ON CONFLICT DO NOTHING`, a.TenantID(), app, revision, anchorHash, e.ID, record.ID, f.ID, f.ReferenceTo, target)
				if err != nil {
					return Dataset{}, err
				}
				var actualEntity, actualID string
				if err = q.QueryRowContext(ctx, `SELECT target_entity,target_id::text FROM runtime.record_links WHERE tenant_id=$1 AND app_id=$2 AND metadata_revision=$3 AND ir_hash=$4 AND entity_id=$5 AND id=$6 AND field_id=$7`, a.TenantID(), app, revision, anchorHash, e.ID, record.ID, f.ID).Scan(&actualEntity, &actualID); err != nil {
					return Dataset{}, err
				}
				if actualEntity != f.ReferenceTo || actualID != target {
					return Dataset{}, ErrUnavailable
				}
			}
			complete = append(complete, []string{f.ID, f.ReferenceTo, target})
		}
		v.links, err = json.Marshal(complete)
		if err != nil {
			return Dataset{}, err
		}
		manifest, _ := json.Marshal(struct {
			Entity                   string
			SchemaVersion, UpdatedBy string
			Record                   Record
			Raw, Shares, Links       json.RawMessage
		}{v.entity, v.schemaVersion, v.updatedBy, v.r, v.raw, v.shares, v.links})
		previous, _ := hex.DecodeString(hash)
		sum := sha256.Sum256(append(previous, manifest...))
		hash = hex.EncodeToString(sum[:])
		entity, rowID = v.entity, sql.NullString{String: v.r.ID, Valid: true}
		count++
	}
	if done {
		state = "OPEN"
	}
	_, err = q.ExecContext(ctx, `UPDATE runtime.datasets SET state=$4,verified_entity=$5,verified_id=$6,verified_rows=$7,manifest_hash=$8,epoch=epoch+1 WHERE tenant_id=$1 AND app_id=$2 AND id=$3`, a.TenantID(), app, id, state, entity, rowID, count, hash)
	if err != nil {
		return Dataset{}, err
	}
	return (DatasetStore{}).Get(ctx, q, a, app, id)
}
