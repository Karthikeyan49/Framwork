package data

import (
	"context"
	"errors"
	"github.com/Karthikeyan49/Framwork/internal/compiler"
	"github.com/Karthikeyan49/Framwork/internal/platform/postgres"
	"github.com/Karthikeyan49/Framwork/internal/policy"
	"github.com/Karthikeyan49/Framwork/internal/tenant"
	"strings"
)

type ScopeResolver func(context.Context, string, Resource) (policy.DataScope, error)

func resolveScope(ctx context.Context, resolve []ScopeResolver, action string, r Resource) (policy.DataScope, error) {
	if len(resolve) != 1 || resolve[0] == nil {
		return policy.DataScope{}, ErrDenied
	}
	s, e := resolve[0](ctx, action, r)
	if e != nil {
		if errors.Is(e, policy.ErrScope) {
			return s, ErrDenied
		}
		return s, e
	}
	return s, nil
}

// Scope SQL is built before user predicates. Owner and shares bind the verified actor.
func scopeSQL(ctx context.Context, e compiler.Entity, s policy.DataScope, a tenant.Access, alias string, b *builder, depth int, nodes *int) (string, error) {
	*nodes++
	if depth > 8 || *nodes > 64 {
		return "", ErrBudget
	}
	switch s.Kind {
	case "ALL":
		return "TRUE", nil
	case "NONE":
		return "FALSE", nil
	case "OWNER":
		return alias + ".owner_user_id=" + b.bind(a.ActorID()) + "::uuid", nil
	case "TEAM":
		if len(s.Teams) > 64 {
			return "", ErrBudget
		}
		if len(s.Teams) == 0 {
			return "FALSE", nil
		}
		parts := ""
		for _, id := range s.Teams {
			if !tenant.ValidID(id) {
				return "", ErrInvalid
			}
			if parts != "" {
				parts += ","
			}
			parts += b.bind(id) + "::uuid"
		}
		return alias + ".owner_team_id IN (" + parts + ")", nil
	case "SHARE":
		condition := ""
		if s.WriteShare {
			condition = " AND sh.can_write"
		}
		table := "runtime.read_shares"
		if worker, _ := ctx.Value(workerReadKey{}).(bool); worker {
			table = "runtime.worker_source_shares()"
		}
		return `EXISTS(SELECT 1 FROM ` + table + ` sh WHERE sh.tenant_id=` + alias + `.tenant_id AND sh.app_id=` + alias + `.app_id AND sh.metadata_revision=` + alias + `.metadata_revision AND sh.ir_hash=` + alias + `.ir_hash AND sh.entity_id=` + alias + `.entity_id AND sh.id=` + alias + `.id AND sh.user_id=` + b.bind(a.ActorID()) + `::uuid` + condition + `)`, nil
	case "CONDITION":
		if s.Condition == nil || (alias != "r" && alias != "j") {
			return "", ErrInvalid
		}
		// Reuse validated typed field/operator/value resolution; policy cannot emit raw SQL.
		f := Filter{Op: s.Condition.Op, Field: s.Condition.Field, Value: s.Condition.Value}
		n := 0
		fragment, err := filterSQL(ctx, e, f, b, Guard(func(context.Context, string, Resource) error { return nil }), 0, &n)
		return strings.ReplaceAll(fragment, "r.", alias+"."), err
	case "AND", "OR":
		if len(s.Children) < 1 || len(s.Children) > 8 {
			return "", ErrInvalid
		}
		out := "("
		for i, c := range s.Children {
			sql, e := scopeSQL(ctx, e, c, a, alias, b, depth+1, nodes)
			if e != nil {
				return "", e
			}
			if i > 0 {
				if s.Kind == "AND" {
					out += " AND "
				} else {
					out += " OR "
				}
			}
			out += sql
		}
		return out + ")", nil
	}
	return "", ErrDenied
}

// Intersect field scopes before records enter application memory.
func recordScope(ctx context.Context, resolve []ScopeResolver, e compiler.Entity, action string) (policy.DataScope, error) {
	root, err := resolveScope(ctx, resolve, action, Resource{EntityID: e.ID})
	if err != nil {
		return policy.DataScope{}, err
	}
	children := []policy.DataScope{root}
	for _, f := range e.Fields {
		if action == "data.write" && f.CoreID {
			continue
		}
		child, er := resolveScope(ctx, resolve, action, Resource{EntityID: e.ID, FieldID: f.ID})
		if er != nil {
			return policy.DataScope{}, er
		}
		children = append(children, child)
	}
	// Scope child limits are retained by grouping fields into bounded conjunctions.
	for len(children) > 8 {
		next := []policy.DataScope{}
		for i := 0; i < len(children); i += 8 {
			end := i + 8
			if end > len(children) {
				end = len(children)
			}
			next = append(next, policy.DataScope{Kind: "AND", Children: children[i:end]})
		}
		children = next
	}
	return policy.DataScope{Kind: "AND", Children: children}, nil
}
func authorizeExistingWrite(ctx context.Context, q postgres.Executor, s Scope, a tenant.Access, e compiler.Entity, r Record, resolve []ScopeResolver) error {
	scope, er := recordScope(ctx, resolve, e, "data.write")
	if er != nil {
		return er
	}
	b := builder{args: append(base(s, a, e.ID), r.ID)}
	nodes := 0
	pred, er := scopeSQL(ctx, e, scope, a, "r", &b, 0, &nodes)
	if er != nil {
		return er
	}
	var allowed bool
	er = q.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM runtime.read_records r WHERE "+recordWhere+" AND id=$6 AND ("+pred+"))", b.args...).Scan(&allowed)
	if er != nil {
		return er
	}
	if !allowed {
		return ErrDenied
	}
	return nil
}
