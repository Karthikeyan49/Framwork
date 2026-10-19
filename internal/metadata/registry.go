package metadata

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/Karthikeyan49/Framwork/internal/platform/postgres"
	"github.com/Karthikeyan49/Framwork/internal/tenant"
)

var ErrUnavailable = errors.New("metadata registry unavailable")
var ErrConflict = errors.New("metadata revision conflict")
var ErrNotFound = errors.New("metadata version not found")

type Registry struct{ validator *Validator }

func NewRegistry() (*Registry, error) {
	v, err := NewValidator()
	if err != nil {
		return nil, err
	}
	return &Registry{v}, nil
}
func (r *Registry) Parse(raw []byte) (Source, error) {
	if r == nil {
		return Source{}, ErrUnavailable
	}
	return r.validator.Parse(raw)
}

type Version struct {
	AppID         string          `json:"app_id"`
	Revision      int64           `json:"revision"`
	Status        string          `json:"status"`
	SchemaVersion string          `json:"schema_version"`
	SourceHash    string          `json:"source_hash"`
	CreatedBy     string          `json:"created_by"`
	CreatedAt     time.Time       `json:"created_at"`
	Document      json.RawMessage `json:"document,omitempty"`
}

func (r *Registry) Ready(ctx context.Context, db *sql.DB) error {
	if r == nil || r.validator == nil {
		return ErrUnavailable
	}
	var ready bool
	err := db.QueryRowContext(ctx, `SELECT
 EXISTS(SELECT 1 FROM pg_catalog.pg_trigger WHERE tgrelid='control.metadata_versions'::regclass AND tgname='metadata_immutable' AND tgenabled='O' AND NOT tgisinternal)
 AND EXISTS(SELECT 1 FROM pg_catalog.pg_trigger WHERE tgrelid='control.metadata_versions'::regclass AND tgname='metadata_envelope_version' AND tgenabled='O' AND NOT tgisinternal)
 AND EXISTS(SELECT 1 FROM pg_catalog.pg_constraint WHERE conrelid='control.metadata_heads'::regclass AND conname='metadata_head_revision' AND contype='f' AND condeferrable AND condeferred AND convalidated)
 AND EXISTS(SELECT 1 FROM pg_catalog.pg_constraint WHERE conrelid='control.metadata_versions'::regclass AND conname='metadata_envelope_required' AND convalidated)
 AND (SELECT count(*)=2 AND bool_and(relrowsecurity AND relforcerowsecurity) FROM pg_catalog.pg_class WHERE oid IN('control.metadata_heads'::regclass,'control.metadata_versions'::regclass))
 AND pg_catalog.has_table_privilege('control.metadata_versions','SELECT')
 AND pg_catalog.has_column_privilege('control.metadata_versions','document','INSERT')
 AND NOT pg_catalog.has_table_privilege('control.metadata_versions','UPDATE,DELETE,TRUNCATE')
 AND NOT pg_catalog.has_any_column_privilege('control.metadata_versions','UPDATE')
 AND pg_catalog.has_column_privilege('control.metadata_heads','revision','UPDATE')`).Scan(&ready)
	if err != nil || !ready {
		return ErrUnavailable
	}
	_, err = db.ExecContext(ctx, `SELECT h.id,h.namespace,h.revision,v.schema_version,v.document,v.source_hash,v.created_by,v.created_at FROM control.metadata_heads h JOIN control.metadata_versions v ON v.tenant_id=h.tenant_id AND v.app_id=h.id AND v.revision=h.revision LIMIT 0`)
	if err != nil {
		return ErrUnavailable
	}
	return nil
}

// Append compares the expected authoring head and inserts an immutable revision.
// The composition root must authorize and audit in the same outer transaction.
func (r *Registry) Append(ctx context.Context, q postgres.Executor, scope tenant.Access, expected int64, source Source) (Version, error) {
	if r == nil || len(source.raw) == 0 || scope.TenantID() == "" || expected < 0 || expected >= 1000000000 {
		return Version{}, ErrInvalid
	}
	var revision int64
	var err error
	if expected == 0 {
		err = q.QueryRowContext(ctx, `INSERT INTO control.metadata_heads(tenant_id,id,namespace,revision) VALUES($1,$2,$3,1) ON CONFLICT(tenant_id,id) DO NOTHING RETURNING revision`, scope.TenantID(), source.AppID(), source.Namespace()).Scan(&revision)
	} else {
		err = q.QueryRowContext(ctx, `UPDATE control.metadata_heads SET revision=revision+1 WHERE tenant_id=$1 AND id=$2 AND revision=$3 AND namespace=$4 RETURNING revision`, scope.TenantID(), source.AppID(), expected, source.Namespace()).Scan(&revision)
	}

	if errors.Is(err, sql.ErrNoRows) {
		return Version{}, ErrConflict
	}
	if err != nil {
		return Version{}, err
	}
	var version Version
	err = q.QueryRowContext(ctx, `INSERT INTO control.metadata_versions(tenant_id,app_id,revision,document,source_hash) VALUES($1,$2,$3,$4::jsonb,$5) RETURNING app_id,revision,schema_version,source_hash,created_by,created_at`, scope.TenantID(), source.AppID(), revision, string(source.raw), source.hash).Scan(&version.AppID, &version.Revision, &version.SchemaVersion, &version.SourceHash, &version.CreatedBy, &version.CreatedAt)
	if err != nil {
		return Version{}, err
	}
	version.Status = "draft"
	version.Document = source.JSON()
	return version, nil
}
func (r *Registry) Get(ctx context.Context, q postgres.Executor, scope tenant.Access, app string, revision int64) (Version, error) {
	var v Version
	if r == nil || scope.TenantID() == "" || revision < 1 {
		return v, ErrNotFound
	}
	err := q.QueryRowContext(ctx, `SELECT app_id,revision,schema_version,source_hash,created_by,created_at,document FROM control.metadata_versions WHERE tenant_id=$1 AND app_id=$2 AND revision=$3`, scope.TenantID(), app, revision).Scan(&v.AppID, &v.Revision, &v.SchemaVersion, &v.SourceHash, &v.CreatedBy, &v.CreatedAt, &v.Document)
	if errors.Is(err, sql.ErrNoRows) {
		return v, ErrNotFound
	}
	if err != nil {
		return v, err
	}
	source, err := r.validator.parse(v.Document, 300000)
	if err != nil || source.Hash() != v.SourceHash || source.AppID() != app {
		return Version{}, ErrUnavailable
	}
	v.Status = "draft"
	v.Document = source.JSON()
	return v, nil
}

// List returns bounded immutable-version headers; before=0 starts at newest.
func (r *Registry) List(ctx context.Context, q postgres.Executor, scope tenant.Access, app string, before int64) ([]Version, error) {
	if r == nil || scope.TenantID() == "" || before < 0 {
		return nil, ErrInvalid
	}
	rows, err := q.QueryContext(ctx, `SELECT app_id,revision,schema_version,source_hash,created_by,created_at FROM control.metadata_versions WHERE tenant_id=$1 AND app_id=$2 AND ($3::bigint=0 OR revision<$3) ORDER BY revision DESC LIMIT 50`, scope.TenantID(), app, before)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	versions := []Version{}
	for rows.Next() {
		var v Version
		if err = rows.Scan(&v.AppID, &v.Revision, &v.SchemaVersion, &v.SourceHash, &v.CreatedBy, &v.CreatedAt); err != nil {
			return nil, err
		}
		v.Status = "draft"
		versions = append(versions, v)
	}
	return versions, rows.Err()
}
