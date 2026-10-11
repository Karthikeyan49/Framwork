// Package tenant owns membership and creates trusted tenant access contexts.
package tenant

import (
	"context"
	"database/sql"
	"errors"

	"github.com/Karthikeyan49/Framwork/internal/identity"
	"github.com/Karthikeyan49/Framwork/internal/platform/postgres"
)

var ErrDenied = errors.New("tenant access denied")

type Summary struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// Access is immutable to consumers and is minted only by database resolution.
type Access struct {
	actor, tenant string
	roles         []string
}

func (a Access) ActorID() string  { return a.actor }
func (a Access) TenantID() string { return a.tenant }
func (a Access) Roles() []string  { return append([]string{}, a.roles...) }

type Store struct{}

func (Store) Ready(ctx context.Context, db *sql.DB) error {
	_, err := db.ExecContext(ctx, `SELECT t.id,t.name,t.active,m.user_id,m.role,m.active,r.name FROM iam.tenants t JOIN iam.memberships m ON m.tenant_id=t.id JOIN iam.roles r ON r.name=m.role LIMIT 0`)
	if err != nil {
		return err
	}
	var actor, tenantID any
	var matches bool
	return db.QueryRowContext(ctx, `SELECT iam.authenticated_actor(),iam.current_tenant_id(),iam.tenant_matches_scope(NULL)`).Scan(&actor, &tenantID, &matches)
}

func (Store) BindActor(ctx context.Context, tx postgres.Executor, user identity.User) error {
	actor := user.ID()
	if _, err := tx.ExecContext(ctx, `SELECT set_config('app.actor_id','',true),set_config('app.tenant_id','',true)`); err != nil {
		return err
	}
	if !ValidID(actor) {
		return ErrDenied
	}
	_, err := tx.ExecContext(ctx, `SELECT set_config('app.tenant_id','',true),set_config('app.actor_id',$1,true)`, actor)
	return err
}
func (Store) List(ctx context.Context, tx postgres.Executor, user identity.User) ([]Summary, error) {
	actor := user.ID()
	rows, err := tx.QueryContext(ctx, `SELECT t.id,t.name FROM iam.tenants t JOIN iam.memberships m ON m.tenant_id=t.id WHERE m.user_id=$1 AND m.active AND t.active ORDER BY t.id LIMIT 100`, actor)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []Summary{}
	for rows.Next() {
		var t Summary
		if err = rows.Scan(&t.ID, &t.Name); err != nil {
			return nil, err
		}
		result = append(result, t)
	}
	return result, rows.Err()
}
func (Store) Resolve(ctx context.Context, tx postgres.Executor, user identity.User, selected string) (Access, error) {
	actor := user.ID()
	if _, err := tx.ExecContext(ctx, `SELECT set_config('app.tenant_id','',true)`); err != nil {
		return Access{}, err
	}
	if !ValidID(actor) || !ValidID(selected) {
		return Access{}, ErrDenied
	}
	var role string
	err := tx.QueryRowContext(ctx, `SELECT m.role FROM iam.memberships m JOIN iam.tenants t ON t.id=m.tenant_id WHERE m.user_id=$1 AND m.tenant_id=$2 AND m.active AND t.active`, actor, selected).Scan(&role)
	if errors.Is(err, sql.ErrNoRows) {
		return Access{}, ErrDenied
	}
	if err != nil {
		return Access{}, err
	}
	if _, err = tx.ExecContext(ctx, `SELECT set_config('app.tenant_id',$1,true)`, selected); err != nil {
		return Access{}, err
	}
	var matches bool
	if err = tx.QueryRowContext(ctx, `SELECT iam.tenant_matches_scope($1::uuid)`, selected).Scan(&matches); err != nil {
		return Access{}, err
	}
	if !matches {
		return Access{}, ErrDenied
	}
	return Access{actor, selected, []string{role}}, nil
}

type Member struct {
	UserID string `json:"user_id"`
	Role   string `json:"role"`
	Active bool   `json:"active"`
}

func (Store) Members(ctx context.Context, tx postgres.Executor, access Access) ([]Member, error) {
	if access.actor == "" || access.tenant == "" {
		return nil, ErrDenied
	}
	rows, err := tx.QueryContext(ctx, `SELECT user_id,role,active FROM iam.memberships WHERE tenant_id=$1 ORDER BY user_id LIMIT 100`, access.tenant)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []Member{}
	for rows.Next() {
		var m Member
		if err = rows.Scan(&m.UserID, &m.Role, &m.Active); err != nil {
			return nil, err
		}
		result = append(result, m)
	}
	return result, rows.Err()
}

// Provision owns tenant/membership writes; callers must use an explicit operator
// transaction and append audit evidence in that same transaction.
func (Store) Provision(ctx context.Context, tx postgres.Executor, tenantID, name, userID, role string, active bool) error {
	if !ValidID(tenantID) || !ValidID(userID) || name == "" || len(name) > 200 || !ValidRole(role) {
		return errors.New("invalid membership")
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO iam.tenants(id,name) VALUES($1,$2) ON CONFLICT(id) DO UPDATE SET name=excluded.name`, tenantID, name); err != nil {
		return err
	}
	// Tenant row serializes all operator membership changes, including last-owner
	// checks. Disabling a user is handled separately by the operator command.
	if _, err := tx.ExecContext(ctx, `SELECT id FROM iam.tenants WHERE id=$1 FOR UPDATE`, tenantID); err != nil {
		return err
	}
	var owners int
	var oldOwner bool
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM iam.memberships WHERE tenant_id=$1 AND role='owner' AND active`, tenantID).Scan(&owners); err != nil {
		return err
	}
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM iam.memberships WHERE tenant_id=$1 AND user_id=$2 AND role='owner' AND active)`, tenantID, userID).Scan(&oldOwner); err != nil {
		return err
	}
	if oldOwner && owners <= 1 && (!active || role != "owner") {
		return errors.New("cannot remove the last owner")
	}
	if owners == 0 && (!active || role != "owner") {
		return errors.New("bootstrap requires an active owner")
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO iam.memberships(tenant_id,user_id,role,active) VALUES($1,$2,$3,$4) ON CONFLICT(tenant_id,user_id) DO UPDATE SET role=excluded.role,active=excluded.active`, tenantID, userID, role, active)
	return err
}
func ValidRole(s string) bool { return s == "owner" || s == "admin" || s == "member" || s == "viewer" }

// A disabled identity may not retain an active owner membership. Operators must
// transfer ownership first; there is no implicit reactivation or privilege grant.
func (Store) CheckDisable(ctx context.Context, tx postgres.Executor, userID string) error {
	var owns bool
	err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM iam.memberships WHERE user_id=$1 AND role='owner' AND active)`, userID).Scan(&owns)
	if err != nil {
		return err
	}
	if owns {
		return ErrDenied
	}
	return nil
}
func ValidID(s string) bool {
	if len(s) != 36 {
		return false
	}
	for i, c := range s {
		if i == 8 || i == 13 || i == 18 || i == 23 {
			if c != '-' {
				return false
			}
		} else if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			return false
		}
	}
	return true
}
