package data

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"

	"github.com/Karthikeyan49/Framwork/internal/compiler"
	"github.com/Karthikeyan49/Framwork/internal/metadata"
	"github.com/Karthikeyan49/Framwork/internal/platform/postgres"
	"github.com/Karthikeyan49/Framwork/internal/policy"
	"github.com/Karthikeyan49/Framwork/internal/tenant"
)

// QueryVisible is the interactive projection port. Execution inputs use Query,
// which remains strict: an inaccessible field can never become a null input.
func (Store) QueryVisible(ctx context.Context, q postgres.Executor, s Scope, a tenant.Access, entity string, query Query, g Guard, resolve ...ScopeResolver) (Page, error) {
	return (Store{visible: true}).Query(ctx, q, s, a, entity, query, g, resolve...)
}
func scopeAll(s policy.DataScope) bool {
	if s.Kind == "ALL" {
		return true
	}
	if s.Kind == "OR" {
		for _, c := range s.Children {
			if scopeAll(c) {
				return true
			}
		}
	}
	return false
}
func coversEntity(field, entity policy.DataScope) bool {
	if scopeAll(field) {
		return true
	}
	f, _ := json.Marshal(field)
	e, _ := json.Marshal(entity)
	return string(f) == string(e)
}
func hasCondition(s policy.DataScope) bool {
	if s.Kind == "CONDITION" {
		return true
	}
	for _, c := range s.Children {
		if hasCondition(c) {
			return true
		}
	}
	return false
}

// Unknown/unavailable policy errors are never softened into a mask.
func fieldPredicate(ctx context.Context, e compiler.Entity, a tenant.Access, alias string, b *builder, g Guard, resolve []ScopeResolver, action, id string) (string, error) {
	r := Resource{EntityID: e.ID, FieldID: id}
	cap := strings.Replace(action, "data.", "data.field.", 1)
	if err := g.check(ctx, cap, r); err != nil {
		if errors.Is(err, ErrDenied) {
			return "FALSE", nil
		}
		return "", err
	}
	scope, err := resolveScope(ctx, resolve, action, r)
	if err != nil {
		if errors.Is(err, ErrDenied) {
			return "FALSE", nil
		}
		return "", err
	}
	n := 0
	return scopeSQL(ctx, e, scope, a, alias, b, 0, &n)
}

// Mask in SQL, never fetch full JSONB and redact it after crossing the port.
func visibleJSON(ctx context.Context, e compiler.Entity, ids []string, a tenant.Access, alias string, b *builder, g Guard, resolve []ScopeResolver) (string, error) {
	parts := []string{"'{}'::jsonb"}
	masked := []string{"'[]'::jsonb"}
	writable := []string{"'[]'::jsonb"}
	patchable := true
	entityWrite, err := resolveScope(ctx, resolve, "data.write", Resource{EntityID: e.ID})
	if err != nil && !errors.Is(err, ErrDenied) {
		return "", err
	}
	entityRead, er := resolveScope(ctx, resolve, "data.read", Resource{EntityID: e.ID})
	if er != nil {
		return "", er
	}
	patchable = patchable && err == nil && !hasCondition(entityRead) && !hasCondition(entityWrite)
	ep := "FALSE"
	if patchable {
		if er = g.check(ctx, "data.entity.write", Resource{EntityID: e.ID}); er != nil {
			if !errors.Is(er, ErrDenied) {
				return "", er
			}
			patchable = false
		} else {
			n := 0
			ep, er = scopeSQL(ctx, e, entityWrite, a, alias, b, 0, &n)
			if er != nil {
				return "", er
			}
		}
	}
	for _, id := range ids {
		f, _ := field(e, id)
		k := b.bind(id) + "::text"
		pred, er := fieldPredicate(ctx, e, a, alias, b, g, resolve, "data.read", id)
		if er != nil {
			return "", er
		}
		value := alias + ".custom_data->" + k
		present := alias + ".custom_data ? " + k
		if f.CoreID {
			value = "to_jsonb(" + alias + ".id::text)"
			present = "TRUE"
		}
		parts = append(parts, "CASE WHEN COALESCE(("+pred+"),false) AND ("+present+") THEN jsonb_build_object("+k+","+value+") ELSE '{}'::jsonb END")
		masked = append(masked, "CASE WHEN COALESCE(("+pred+"),false) THEN '[]'::jsonb ELSE jsonb_build_array("+k+") END")
		if patchable && !f.CoreID {
			rs, re := resolveScope(ctx, resolve, "data.read", Resource{EntityID: e.ID, FieldID: id})
			ws, we := resolveScope(ctx, resolve, "data.write", Resource{EntityID: e.ID, FieldID: id})
			if re != nil && !errors.Is(re, ErrDenied) {
				return "", re
			}
			if we != nil && !errors.Is(we, ErrDenied) {
				return "", we
			}
			if re == nil && we == nil && !hasCondition(rs) && !hasCondition(ws) {
				wp, er := fieldPredicate(ctx, e, a, alias, b, g, resolve, "data.write", id)
				if er != nil {
					return "", er
				}
				writable = append(writable, "CASE WHEN COALESCE(("+pred+") AND ("+wp+") AND ("+ep+"),false) THEN jsonb_build_array("+k+") ELSE '[]'::jsonb END")
			}
		}
	}
	return "jsonb_build_object('values',(" + strings.Join(parts, " || ") + "),'field_access',jsonb_build_object('masked',(" + strings.Join(masked, " || ") + "),'writable',(" + strings.Join(writable, " || ") + ")))", nil
}
func readVisibleValues(e compiler.Entity, ids []string, r *Record, raw []byte) error {
	v, err := metadata.DecodeJSON(raw)
	if err != nil {
		return ErrUnavailable
	}
	raw, _ = json.Marshal(v)
	var envelope struct {
		Values map[string]any `json:"values"`
		Access FieldAccess    `json:"field_access"`
	}
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.UseNumber()
	if dec.Decode(&envelope) != nil || envelope.Values == nil {
		return ErrUnavailable
	}
	r.Values = map[string]any{}
	r.FieldAccess = &envelope.Access
	for _, id := range ids {
		f, _ := field(e, id)
		value, present := envelope.Values[id]
		if slices.Contains(envelope.Access.Masked, id) {
			if present {
				return ErrUnavailable
			}
			continue
		}
		if !present {
			if f.Required || f.CoreID {
				return ErrUnavailable
			}
			continue
		}
		value, err = fieldValue(f, value)
		if err != nil {
			return ErrUnavailable
		}
		r.Values[id] = value
	}
	unknown := append([]string(nil), r.FieldAccess.Masked...)
	for _, f := range e.Fields {
		if !slices.Contains(ids, f.ID) {
			unknown = append(unknown, f.ID)
		}
	}
	if visiblePatchBudget(e, r.Values, unknown) != nil {
		r.FieldAccess.Writable = []string{}
	}
	return nil
}
