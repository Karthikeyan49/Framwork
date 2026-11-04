package policy

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sort"
	"strings"

	"github.com/Karthikeyan49/Framwork/internal/platform/postgres"
	"github.com/Karthikeyan49/Framwork/internal/tenant"
)

var ErrRoleConflict = errors.New("role version conflict")

// RoleDenied carries bounded provenance for a known inactive/invalid head.
// It does not expose definitions or turn a failed load into a fallback grant.
type RoleDenied struct {
	Name, Hash string
	Version    int64
	Bootstrap  bool
}

func (RoleDenied) Error() string { return "role authority denied" }
func (RoleDenied) Unwrap() error { return ErrScope }
func (d RoleDenied) PolicyVersion() string {
	return (Defined{hash: d.Hash, legacy: d.Bootstrap}).policyVersion()
}

type RoleVersion struct {
	Name        string          `json:"name"`
	Version     int64           `json:"version"`
	Hash        string          `json:"hash"`
	Active      bool            `json:"active"`
	Bootstrap   bool            `json:"bootstrap"`
	OperationID string          `json:"operation_id"`
	Definition  json.RawMessage `json:"definition"`
}
type RoleStore struct{}

// RolePage omits policy bodies and uses tenant-scoped, bounded keyset reads.
type RoleSummary struct {
	Name        string `json:"name"`
	Version     int64  `json:"version"`
	Hash        string `json:"hash"`
	Active      bool   `json:"active"`
	Bootstrap   bool   `json:"bootstrap"`
	OperationID string `json:"operation_id"`
}
type RolePage struct {
	Roles []RoleSummary `json:"roles"`
	Next  string        `json:"next,omitempty"`
}

func (RoleStore) List(ctx context.Context, q postgres.Executor, a tenant.Access, after string) (RolePage, error) {
	page := RolePage{Roles: []RoleSummary{}}
	if after != "" && !ValidRoleName(after) {
		return page, ErrDefinition
	}
	rows, err := q.QueryContext(ctx, `SELECT h.name,h.version,v.body_hash,h.active,v.bootstrap,v.operation_id::text FROM control.role_heads h JOIN control.role_versions v USING(tenant_id,name,version) WHERE h.tenant_id=$1 AND h.name COLLATE "C">$2::text COLLATE "C" ORDER BY h.name COLLATE "C" LIMIT 101`, a.TenantID(), after)
	if err != nil {
		return page, err
	}
	defer rows.Close()
	for rows.Next() {
		var role RoleSummary
		if err := rows.Scan(&role.Name, &role.Version, &role.Hash, &role.Active, &role.Bootstrap, &role.OperationID); err != nil {
			return RolePage{}, err
		}
		page.Roles = append(page.Roles, role)
	}
	if err := rows.Err(); err != nil {
		return RolePage{}, err
	}
	if len(page.Roles) > 100 {
		page.Roles = page.Roles[:100]
		page.Next = page.Roles[99].Name
	}
	return page, nil
}

type Loader func(context.Context, postgres.Executor, tenant.Access) (Engine, error)
type Transactional interface {
	Resolve(context.Context, postgres.Executor, tenant.Access) (Engine, error)
}

// Runtime is a composition-only resolver. Unresolved decisions always deny.
type Runtime struct{ Worker bool }

func (r Runtime) Resolve(ctx context.Context, q postgres.Executor, a tenant.Access) (Engine, error) {
	if r.Worker {
		return (RoleStore{}).LoadWorker(ctx, q, a)
	}
	return (RoleStore{}).Load(ctx, q, a)
}
func (Runtime) Decide(ctx context.Context, s Subject, action string, resource Resource, c Context) Decision {
	return (Native{}).Decide(ctx, Subject{}, action, resource, c)
}
func (Runtime) DataScope(context.Context, Subject, string, Resource, ScopeContext) (DataScope, error) {
	return DataScope{Kind: "NONE"}, ErrScope
}

func (RoleStore) Current(ctx context.Context, q postgres.Executor, a tenant.Access, name string) (RoleVersion, error) {
	var v RoleVersion
	if !ValidRoleName(name) {
		return v, ErrDefinition
	}
	err := q.QueryRowContext(ctx, `SELECT h.name,h.version,v.body_hash,h.active,v.bootstrap,v.operation_id::text,v.body FROM control.role_heads h JOIN control.role_versions v USING(tenant_id,name,version) WHERE h.tenant_id=$1 AND h.name=$2`, a.TenantID(), name).Scan(&v.Name, &v.Version, &v.Hash, &v.Active, &v.Bootstrap, &v.OperationID, &v.Definition)
	if errors.Is(err, sql.ErrNoRows) {
		return v, ErrScope
	}
	return v, err
}

// Operation reconciles an unknown commit using its immutable request witness;
// a newer head never changes the version returned for an earlier operation.
func (RoleStore) Operation(ctx context.Context, q postgres.Executor, a tenant.Access, id string) (RoleVersion, error) {
	var v RoleVersion
	if !tenant.ValidID(id) {
		return v, ErrDefinition
	}
	err := q.QueryRowContext(ctx, `SELECT name,version,body_hash,active,bootstrap,operation_id::text,body FROM control.role_versions WHERE tenant_id=$1 AND operation_id=$2`, a.TenantID(), id).Scan(&v.Name, &v.Version, &v.Hash, &v.Active, &v.Bootstrap, &v.OperationID, &v.Definition)
	if errors.Is(err, sql.ErrNoRows) {
		return v, ErrScope
	}
	return v, err
}
func compileVersion(v RoleVersion) (Defined, error) {
	d, err := CompileDefinition(v.Definition, v.Hash)
	if err != nil || d.Name() != v.Name || !v.Active {
		return Defined{}, ErrScope
	}
	if v.Bootstrap {
		if v.Name != "owner" && v.Name != "admin" && v.Name != "member" && v.Name != "viewer" || len(d.definition.Grants) != 0 {
			return Defined{}, ErrScope
		}
		d.legacy = true
	}
	return d, nil
}

// Load reads the role selected by live membership, not a request-supplied role.
func (s RoleStore) Load(ctx context.Context, q postgres.Executor, a tenant.Access) (Engine, error) {
	roles := a.Roles()
	if len(roles) != 1 {
		return nil, ErrScope
	}
	v, err := s.Current(ctx, q, a, roles[0])
	if err != nil {
		return nil, err
	}
	d, err := compileVersion(v)
	if err != nil {
		return nil, RoleDenied{Name: v.Name, Hash: v.Hash, Version: v.Version, Bootstrap: v.Bootstrap}
	}
	d.tenantID, d.actorID = a.TenantID(), a.ActorID()
	return d, nil
}
func (s RoleStore) LoadWorker(ctx context.Context, q postgres.Executor, a tenant.Access) (Engine, error) {
	var v RoleVersion
	var tenantID, actorID string
	err := q.QueryRowContext(ctx, `SELECT tenant_id::text,actor_id::text,name,version,body_hash,active,bootstrap,body FROM control.worker_role_definition()`).Scan(&tenantID, &actorID, &v.Name, &v.Version, &v.Hash, &v.Active, &v.Bootstrap, &v.Definition)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrScope
	}
	if err != nil {
		return nil, err
	}
	if len(a.Roles()) != 1 || a.Roles()[0] != v.Name || a.TenantID() != tenantID || a.ActorID() != actorID {
		return nil, ErrScope
	}
	d, err := compileVersion(v)
	if err != nil {
		return nil, RoleDenied{Name: v.Name, Hash: v.Hash, Version: v.Version, Bootstrap: v.Bootstrap}
	}
	d.tenantID, d.actorID = tenantID, actorID
	return d, nil
}
func (s RoleStore) Publish(ctx context.Context, q postgres.Executor, a tenant.Access, id string, expected int64, raw []byte, active bool) (RoleVersion, error) {
	d, err := CompileDefinition(raw, "")
	if err != nil || !tenant.ValidID(id) || expected < 0 || expected >= 1e9 || d.Name() == "owner" {
		return RoleVersion{}, ErrDefinition
	}
	if expected == 0 {
		// A new head: refuse the one above the bound a policy snapshot can carry, so that a
		// tenant can never publish its way out of building release candidates (review F10).
		// A replay of a recorded operation is not a new head. Role heads cannot be retired,
		// so the bound is on all heads, active or not. Best effort under concurrency: two
		// simultaneous publications by the owner may overshoot by one; the snapshot then
		// refuses with a typed, audited error instead.
		var full bool
		if err = q.QueryRowContext(ctx, `SELECT (SELECT count(*) FROM control.role_heads WHERE tenant_id=$1) >= $3 AND NOT EXISTS (SELECT 1 FROM control.role_versions WHERE tenant_id=$1 AND operation_id=$2)`, a.TenantID(), id, MaxRoleHeads).Scan(&full); err != nil {
			return RoleVersion{}, err
		}
		if full {
			return RoleVersion{}, ErrTooManyRoles
		}
	}
	_, err = q.ExecContext(ctx, `SELECT control.publish_role($1,$2,$3,$4,$5,$6,$7)`, a.TenantID(), d.Name(), id, expected, raw, d.Hash(), active)
	if err != nil {
		return RoleVersion{}, err
	}
	return s.Operation(ctx, q, a, id)
}
func (RoleStore) Assign(ctx context.Context, q postgres.Executor, a tenant.Access, user, role, expectedRole string) error {
	if !tenant.ValidID(user) || !ValidRoleName(role) || !ValidRoleName(expectedRole) {
		return ErrDefinition
	}
	_, err := q.ExecContext(ctx, `SELECT control.assign_role($1,$2,$3,$4)`, a.TenantID(), user, role, expectedRole)
	return err
}

// RoleHead identifies the active definition of one tenant role.
type RoleHead struct {
	Name    string `json:"name"`
	Version int64  `json:"version"`
	Hash    string `json:"hash"`
	Active  bool   `json:"active"`
}

// MaxRoleHeads bounds a policy snapshot. A tenant above it cannot build
// release candidates (fail closed). Role heads cannot be retired, so Publish refuses
// the head that would exceed the bound (ErrTooManyRoles).
const MaxRoleHeads = 1024

// ErrTooManyRoles refuses a new role head beyond MaxRoleHeads.
var ErrTooManyRoles = errors.New("role head limit reached")

// ActionCatalogHash is the sha256 of the sorted action names the engine knows.
// A new or removed capability changes it, so candidates built before the change
// become stale (ADR102 section 2).
func ActionCatalogHash() string { return catalogHash(strings.Fields(actionCatalog)) }

func catalogHash(names []string) string {
	names = append([]string(nil), names...)
	sort.Strings(names)
	h := sha256.Sum256([]byte(strings.Join(names, "\n")))
	return hex.EncodeToString(h[:])
}

// Snapshot lists every role head of the caller's tenant ordered by name. It is
// read inside the caller's repeatable-read transaction and feeds the
// whole-tenant policy bundle hash: any role change, activation change or engine
// change must make previously built candidates stale.
func (RoleStore) Snapshot(ctx context.Context, q postgres.Executor, a tenant.Access) ([]RoleHead, error) {
	rows, err := q.QueryContext(ctx, `SELECT h.name,h.version,v.body_hash,h.active FROM control.role_heads h JOIN control.role_versions v ON v.tenant_id=h.tenant_id AND v.name=h.name AND v.version=h.version WHERE h.tenant_id=$1 ORDER BY h.name LIMIT $2`, a.TenantID(), MaxRoleHeads+1)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	heads := []RoleHead{}
	for rows.Next() {
		var h RoleHead
		if err = rows.Scan(&h.Name, &h.Version, &h.Hash, &h.Active); err != nil {
			return nil, err
		}
		heads = append(heads, h)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	if len(heads) > MaxRoleHeads {
		return nil, ErrDefinition
	}
	return heads, nil
}
