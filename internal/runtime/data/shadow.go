package data

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"reflect"
	"sort"

	"github.com/Karthikeyan49/Framwork/internal/compiler"
	"github.com/Karthikeyan49/Framwork/internal/metadata"
	"github.com/Karthikeyan49/Framwork/internal/platform/postgres"
	"github.com/Karthikeyan49/Framwork/internal/tenant"
)

type Evolution struct {
	ID               string `json:"operation_id"`
	State            string `json:"state"`
	Checkpoint       int64  `json:"checkpoint"`
	BaseBinding      int64  `json:"base_binding"`
	BaseGeneration   int64  `json:"base_generation"`
	TargetGeneration int64  `json:"target_generation"`
	CopiedRows       int64  `json:"copied_rows"`
	VerifiedRows     int64  `json:"verified_rows"`
	PlanHash         string `json:"plan_hash"`
	SourceManifest   string `json:"source_manifest"`
	TargetManifest   string `json:"target_manifest"`
}
type evolutionState struct {
	Evolution
	base, target                 compiler.Program
	plan                         compiler.EvolutionPlan
	baseRevision, targetRevision int64
	baseHash, targetHash         string
	entity                       string
	after                        sql.NullString
}

func loadEvolution(ctx context.Context, q postgres.Executor, a tenant.Access, app, ds, id string, lock bool) (evolutionState, error) {
	var x evolutionState
	if !tenant.ValidID(ds) || !tenant.ValidID(id) {
		return x, ErrInvalid
	}
	if lock {
		var state string
		if err := q.QueryRowContext(ctx, `SELECT state FROM runtime.datasets WHERE tenant_id=$1 AND app_id=$2 AND id=$3 FOR UPDATE`, a.TenantID(), app, ds).Scan(&state); err != nil {
			return x, err
		}
	}
	var baseRaw, targetRaw, planRaw []byte
	var baseIRHash, targetIRHash string
	err := q.QueryRowContext(ctx, `SELECT o.operation_id::text,o.state,o.checkpoint,o.base_binding,o.base_generation,o.target_generation,o.copied_rows,o.verified_rows,o.plan_hash,o.source_manifest,o.target_manifest,o.after_entity,o.after_id::text,b.ir_body,b.ir_hash,o.target_ir,o.target_ir_hash,o.plan_body,sg.anchor_revision,sg.anchor_hash,tg.anchor_revision,tg.anchor_hash
 FROM runtime.dataset_evolutions o JOIN runtime.dataset_bindings b ON b.tenant_id=o.tenant_id AND b.app_id=o.app_id AND b.dataset_id=o.dataset_id AND b.version=o.base_binding
 JOIN runtime.dataset_generations sg ON sg.tenant_id=o.tenant_id AND sg.app_id=o.app_id AND sg.dataset_id=o.dataset_id AND sg.generation=o.base_generation
 JOIN runtime.dataset_generations tg ON tg.tenant_id=o.tenant_id AND tg.app_id=o.app_id AND tg.dataset_id=o.dataset_id AND tg.generation=o.target_generation
 WHERE o.tenant_id=$1 AND o.app_id=$2 AND o.dataset_id=$3 AND o.operation_id=$4`, a.TenantID(), app, ds, id).Scan(&x.ID, &x.State, &x.Checkpoint, &x.BaseBinding, &x.BaseGeneration, &x.TargetGeneration, &x.CopiedRows, &x.VerifiedRows, &x.PlanHash, &x.SourceManifest, &x.TargetManifest, &x.entity, &x.after, &baseRaw, &baseIRHash, &targetRaw, &targetIRHash, &planRaw, &x.baseRevision, &x.baseHash, &x.targetRevision, &x.targetHash)
	if errors.Is(err, sql.ErrNoRows) {
		return x, ErrNotFound
	}
	if err != nil {
		return x, err
	}
	x.base, err = compiler.Restore(baseRaw, baseIRHash, compiler.SupportedIRVersions())
	if err != nil {
		return x, ErrUnavailable
	}
	x.target, err = compiler.Restore(targetRaw, targetIRHash, compiler.SupportedIRVersions())
	if err != nil {
		return x, ErrUnavailable
	}
	x.plan, err = compiler.RestoreEvolution(x.base, x.target, planRaw, x.PlanHash)
	if err != nil {
		return x, ErrUnavailable
	}
	return x, nil
}

func (DatasetStore) Evolution(ctx context.Context, q postgres.Executor, a tenant.Access, app, ds, id string) (Evolution, error) {
	x, err := loadEvolution(ctx, q, a, app, ds, id, false)
	return x.Evolution, err
}

func (DatasetStore) PrepareEvolution(ctx context.Context, q postgres.Executor, a tenant.Access, app, ds, id string, expected int64, target compiler.Program, revision int64, intent compiler.EvolutionIntent) (Evolution, error) {
	if !tenant.ValidID(id) || !target.Valid() || target.IR().Application.AppID != app || expected < 1 || revision < 1 {
		return Evolution{}, ErrInvalid
	}
	baseScope, err := (DatasetStore{}).Scope(ctx, q, a, app, ds, expected)
	if err != nil {
		return Evolution{}, err
	}
	p, err := compiler.PlanEvolution(baseScope.Program(), target, intent)
	if err != nil {
		return Evolution{}, ErrInvalid
	}
	if err = ValidateBackfills(p, target); err != nil {
		return Evolution{}, err
	}
	_, err = q.ExecContext(ctx, `SELECT runtime.start_evolution($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`, a.TenantID(), app, ds, id, expected, revision, []byte(target.JSON()), target.Hash(), []byte(p.JSON()), p.Hash(), p.Artifact().SchemaHash)
	if err != nil {
		return Evolution{}, err
	}
	return (DatasetStore{}).Evolution(ctx, q, a, app, ds, id)
}

type evolutionRow struct {
	entity, id, createdBy, updatedBy, schema, owner string
	team                                            sql.NullString
	version                                         int64
	created, updated                                json.RawMessage
	raw, shares, links                              json.RawMessage
}

func evolutionRows(ctx context.Context, q postgres.Executor, a tenant.Access, app string, rev int64, hash, entity string, after sql.NullString) ([]evolutionRow, error) {
	rows, err := q.QueryContext(ctx, `SELECT r.entity_id,r.id::text,r.row_version,r.custom_data,r.created_by::text,to_jsonb(r.created_at),to_jsonb(r.updated_at),r.schema_version,r.owner_user_id::text,r.owner_team_id::text,r.updated_by::text,
 (SELECT coalesce(jsonb_agg(jsonb_build_array(s.user_id,s.can_write) ORDER BY s.user_id),'[]'::jsonb) FROM (SELECT user_id,can_write FROM runtime.read_shares s WHERE s.tenant_id=r.tenant_id AND s.app_id=r.app_id AND s.metadata_revision=r.metadata_revision AND s.ir_hash=r.ir_hash AND s.entity_id=r.entity_id AND s.id=r.id ORDER BY user_id LIMIT 257)s),
 (SELECT coalesce(jsonb_agg(jsonb_build_array(l.field_id,l.target_entity,l.target_id) ORDER BY l.field_id),'[]'::jsonb) FROM (SELECT field_id,target_entity,target_id FROM runtime.record_links l WHERE l.tenant_id=r.tenant_id AND l.app_id=r.app_id AND l.metadata_revision=r.metadata_revision AND l.ir_hash=r.ir_hash AND l.entity_id=r.entity_id AND l.id=r.id ORDER BY field_id LIMIT 33)l)
 FROM runtime.read_records r WHERE r.tenant_id=$1 AND r.app_id=$2 AND r.metadata_revision=$3 AND r.ir_hash=$4 AND ($5='' OR (r.entity_id,r.id)>($5::text,$6::uuid)) ORDER BY r.entity_id,r.id LIMIT 101`, a.TenantID(), app, rev, hash, entity, after)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []evolutionRow
	for rows.Next() {
		var r evolutionRow
		if err = rows.Scan(&r.entity, &r.id, &r.version, &r.raw, &r.createdBy, &r.created, &r.updated, &r.schema, &r.owner, &r.team, &r.updatedBy, &r.shares, &r.links); err != nil {
			return nil, err
		}
		result = append(result, r)
	}
	return result, rows.Err()
}

func decodedRecord(raw []byte) (map[string]any, error) {
	v, err := metadata.DecodeJSON(raw)
	if err != nil {
		return nil, ErrUnavailable
	}
	obj, ok := v.(map[string]any)
	if !ok {
		return nil, ErrUnavailable
	}
	return obj, nil
}
func manifestNext(previous string, value any) string {
	raw, _ := json.Marshal(value)
	p, _ := hex.DecodeString(previous)
	h := sha256.Sum256(append(p, raw...))
	return hex.EncodeToString(h[:])
}
func recordManifest(r evolutionRow, values map[string]any, links [][]string) any {
	return struct {
		Entity, ID, Creator, Updater, Schema, Owner string
		Version                                     int64
		Team                                        sql.NullString
		Created, Updated, Shares                    json.RawMessage
		Values                                      map[string]any
		Links                                       [][]string
	}{r.entity, r.id, r.createdBy, r.updatedBy, r.schema, r.owner, r.version, r.team, r.created, r.updated, r.shares, values, links}
}

func (DatasetStore) AdvanceEvolution(ctx context.Context, q postgres.Executor, a tenant.Access, app, ds, id string, expected int64) (Evolution, error) {
	x, err := loadEvolution(ctx, q, a, app, ds, id, true)
	if err != nil {
		return x.Evolution, err
	}
	if expected != x.Checkpoint {
		return x.Evolution, ErrConflict
	}
	if x.State == "READY" || x.State == "ACTIVATED" || x.State == "ABORTED" {
		return x.Evolution, nil
	}
	rev, hash := x.baseRevision, x.baseHash
	if x.State == "LINKING" {
		rev, hash = x.targetRevision, x.targetHash
	}
	batch, err := evolutionRows(ctx, q, a, app, rev, hash, x.entity, x.after)
	if err != nil {
		return x.Evolution, err
	}
	done := len(batch) <= 100
	if !done {
		batch = batch[:100]
	}
	work, processed := 0, 0
	for _, r := range batch {
		var shares [][]json.RawMessage
		var links [][]string
		if json.Unmarshal(r.shares, &shares) != nil || len(shares) > 256 || json.Unmarshal(r.links, &links) != nil || len(links) > 32 {
			return x.Evolution, ErrBudget
		}
		entity, ok := x.target.Entity(r.entity)
		if !ok {
			return x.Evolution, ErrUnavailable
		}
		cost := 1 + len(shares)
		for _, f := range entity.Fields {
			if f.Type == "reference" {
				cost += 3
			}
		}
		if cost > 512 {
			return x.Evolution, ErrBudget
		}
		if processed > 0 && work+cost > 512 {
			done = false
			break
		}
		work += cost
		input, err := decodedRecord(r.raw)
		if err != nil {
			return x.Evolution, err
		}
		if x.State == "COPYING" || x.State == "VERIFYING" {
			output, err := TransformRecord(x.plan, x.base, x.target, r.entity, r.id, input)
			if err != nil {
				return x.Evolution, err
			}
			raw, err := json.Marshal(output)
			if err != nil || len(raw) > MaxRecordBytes {
				return x.Evolution, ErrBudget
			}
			if x.State == "COPYING" {
				_, err = q.ExecContext(ctx, `SELECT runtime.copy_evolution_record($1,$2,$3,$4,$5,$6,$7::jsonb)`, a.TenantID(), app, ds, id, r.entity, r.id, string(raw))
				if err != nil {
					return x.Evolution, err
				}
			} else {
				// Verify exact target values/header against a newly recomputed
				// transform, not a builder-supplied PASS or copied count alone.
				var actual []byte
				var version int64
				var creator, updater, schema string
				var created, updated json.RawMessage
				err = q.QueryRowContext(ctx, `SELECT custom_data,row_version,created_by::text,updated_by::text,schema_version,to_jsonb(created_at),to_jsonb(updated_at) FROM runtime.records WHERE tenant_id=$1 AND app_id=$2 AND metadata_revision=$3 AND ir_hash=$4 AND entity_id=$5 AND id=$6`, a.TenantID(), app, x.targetRevision, x.targetHash, r.entity, r.id).Scan(&actual, &version, &creator, &updater, &schema, &created, &updated)
				if err != nil {
					return x.Evolution, err
				}
				targetValues, err := decodedRecord(actual)
				if err != nil || !reflect.DeepEqual(output, targetValues) || version != r.version || creator != r.createdBy || updater != r.updatedBy || schema != r.schema || string(created) != string(r.created) || string(updated) != string(r.updated) {
					return x.Evolution, ErrConflict
				}
				var owner string
				var team sql.NullString
				var tombstone bool
				if err = q.QueryRowContext(ctx, `SELECT owner_user_id::text,owner_team_id::text,tombstone FROM runtime.record_identity WHERE tenant_id=$1 AND app_id=$2 AND dataset_id=$3 AND entity_id=$4 AND id=$5`, a.TenantID(), app, ds, r.entity, r.id).Scan(&owner, &team, &tombstone); err != nil {
					return x.Evolution, err
				}
				if owner != r.owner || team != r.team || tombstone {
					return x.Evolution, ErrConflict
				}
				expectedLinks := [][]string{}
				for _, f := range entity.Fields {
					if f.Type == "reference" && output[f.ID] != nil {
						expectedLinks = append(expectedLinks, []string{f.ID, f.ReferenceTo, output[f.ID].(string)})
					}
				}
				sort.Slice(expectedLinks, func(i, j int) bool { return expectedLinks[i][0] < expectedLinks[j][0] })
				var linkRaw []byte
				if err = q.QueryRowContext(ctx, `SELECT coalesce(jsonb_agg(jsonb_build_array(field_id,target_entity,target_id) ORDER BY field_id),'[]'::jsonb) FROM (SELECT field_id,target_entity,target_id FROM runtime.record_links WHERE tenant_id=$1 AND app_id=$2 AND metadata_revision=$3 AND ir_hash=$4 AND entity_id=$5 AND id=$6 ORDER BY field_id LIMIT 33)x`, a.TenantID(), app, x.targetRevision, x.targetHash, r.entity, r.id).Scan(&linkRaw); err != nil {
					return x.Evolution, err
				}
				var actualLinks [][]string
				if json.Unmarshal(linkRaw, &actualLinks) != nil || !reflect.DeepEqual(expectedLinks, actualLinks) {
					return x.Evolution, ErrConflict
				}
				x.SourceManifest = manifestNext(x.SourceManifest, recordManifest(r, output, expectedLinks))
				x.TargetManifest = manifestNext(x.TargetManifest, recordManifest(r, targetValues, actualLinks))
			}
		} else if x.State == "LINKING" {
			normalized, err := values(entity, input, r.id, true)
			if err != nil {
				return x.Evolution, err
			}
			for _, f := range entity.Fields {
				if f.Type == "reference" && normalized[f.ID] != nil {
					_, err = q.ExecContext(ctx, `SELECT runtime.link_evolution_record($1,$2,$3,$4,$5,$6,$7,$8,$9)`, a.TenantID(), app, ds, id, r.entity, r.id, f.ID, f.ReferenceTo, normalized[f.ID])
					if err != nil {
						return x.Evolution, err
					}
				}
			}
		} else {
			return x.Evolution, ErrInvalid
		}
		x.entity, x.after = r.entity, sql.NullString{String: r.id, Valid: true}
		processed++
	}
	if done && x.State == "VERIFYING" {
		var sourceCount, targetCount int64
		if err = q.QueryRowContext(ctx, `SELECT (SELECT count(*) FROM runtime.records WHERE tenant_id=$1 AND app_id=$2 AND metadata_revision=$3 AND ir_hash=$4),(SELECT count(*) FROM runtime.records WHERE tenant_id=$1 AND app_id=$2 AND metadata_revision=$5 AND ir_hash=$6)`, a.TenantID(), app, x.baseRevision, x.baseHash, x.targetRevision, x.targetHash).Scan(&sourceCount, &targetCount); err != nil {
			return x.Evolution, err
		}
		if sourceCount != targetCount || sourceCount != x.CopiedRows || sourceCount != x.VerifiedRows+int64(processed) || x.SourceManifest != x.TargetManifest {
			return x.Evolution, ErrConflict
		}
	}
	_, err = q.ExecContext(ctx, `SELECT runtime.checkpoint_evolution($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`, a.TenantID(), app, ds, id, expected, x.State, x.entity, x.after, int64(processed), x.SourceManifest, x.TargetManifest, done)
	if err != nil {
		return x.Evolution, err
	}
	return (DatasetStore{}).Evolution(ctx, q, a, app, ds, id)
}

func (DatasetStore) FinishEvolution(ctx context.Context, q postgres.Executor, a tenant.Access, app, ds, id string, expected int64, action string) (Evolution, error) {
	if !tenant.ValidID(ds) || !tenant.ValidID(id) || expected < 1 || (action != "ACTIVATE" && action != "ABORT" && action != "REVERIFY") {
		return Evolution{}, ErrInvalid
	}
	if action == "ACTIVATE" {
		if _, err := loadEvolution(ctx, q, a, app, ds, id, true); err != nil {
			return Evolution{}, err
		}
	}
	_, err := q.ExecContext(ctx, `SELECT runtime.finish_evolution($1,$2,$3,$4,$5,$6)`, a.TenantID(), app, ds, id, expected, action)
	if err != nil {
		return Evolution{}, err
	}
	return (DatasetStore{}).Evolution(ctx, q, a, app, ds, id)
}
