package policy

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

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
	return d, nil
}
func (s RoleStore) Publish(ctx context.Context, q postgres.Executor, a tenant.Access, id string, expected int64, raw []byte, active bool) (RoleVersion, error) {
	d, err := CompileDefinition(raw, "")
	if err != nil || !tenant.ValidID(id) || expected < 0 || expected >= 1e9 || d.Name() == "owner" {
		return RoleVersion{}, ErrDefinition
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
