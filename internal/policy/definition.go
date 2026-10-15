package policy

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"regexp"
	"slices"
	"strings"

	"github.com/Karthikeyan49/Framwork/internal/metadata"
)

const DefinitionVersion = "tenant-capabilities-v1"

var ErrDefinition = errors.New("invalid capability definition")
var roleName = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)
var symbol = regexp.MustCompile(`^(app|entity|field)\.[a-z][a-z0-9_]*(\.[a-z][a-z0-9_]*)*$`)

type Selector struct {
	AppID    string `json:"app_id"`
	EntityID string `json:"entity_id"`
	FieldID  string `json:"field_id"`
}
type Grant struct {
	Effect   string     `json:"effect"`
	Actions  []string   `json:"actions"`
	Resource Selector   `json:"resource"`
	Scope    *DataScope `json:"scope,omitempty"`
}
type Definition struct {
	SchemaVersion string  `json:"schema_version"`
	Name          string  `json:"name"`
	Grants        []Grant `json:"grants"`
}
type Defined struct {
	definition Definition
	hash       string
	legacy     bool
	tenantID   string
	actorID    string
}

// Action names are capabilities, never executable code or privilege wildcards.
const actionCatalog = `tenant.access profile.read memberships.read metadata.read metadata.compile metadata.write preview.read preview.write commands.reconcile datasets.manage datasets.evolve indexes.manage rules.evaluate events.consume workflows.read workflows.start workflows.execute workflows.activity workflows.cancel workflows.approve workflows.reject workflows.recover workflows.reconcile data.entity.read data.field.read data.row.read data.aggregate.read data.entity.write data.field.write data.row.write data.row.share data.row.assign roles.read roles.write roles.assign teams.read teams.manage tools.invoke`

func knownAction(action string) bool { return slices.Contains(strings.Fields(actionCatalog), action) }

func ValidRoleName(name string) bool { return roleName.MatchString(name) }

func CompileDefinition(raw []byte, expected string) (Defined, error) {
	var d Definition
	if len(raw) == 0 || len(raw) > 65536 {
		return Defined{}, ErrDefinition
	}
	// The shared public decoder rejects duplicate keys, trailing documents,
	// invalid UTF-8 and excessive complexity before decoding the typed contract.
	decoded, err := metadata.DecodeJSON(raw)
	if err != nil || !definitionKeys(decoded) {
		return Defined{}, ErrDefinition
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&d) != nil || d.SchemaVersion != DefinitionVersion || !ValidRoleName(d.Name) || d.Grants == nil || len(d.Grants) > 64 {
		return Defined{}, ErrDefinition
	}
	scopeCounts, scopeNodes := map[string]int{}, map[string]int{}
	for _, g := range d.Grants {
		if g.Effect != "ALLOW" && g.Effect != "DENY" || len(g.Actions) == 0 || len(g.Actions) > 64 {
			return Defined{}, ErrDefinition
		}
		for _, a := range g.Actions {
			if !knownAction(a) || g.Effect == "ALLOW" && !supportedRoleAction(d.Name, a) {
				return Defined{}, ErrDefinition
			}
		}
		for prefix, v := range map[string]string{"app": g.Resource.AppID, "entity": g.Resource.EntityID, "field": g.Resource.FieldID} {
			if v != "*" && (len(v) > 160 || !symbol.MatchString(v) || !strings.HasPrefix(v, prefix+".")) {
				return Defined{}, ErrDefinition
			}
		}
		if g.Scope != nil {
			nodes := 0
			if validateDefinedScope(*g.Scope, 0, &nodes) != nil {
				return Defined{}, ErrDefinition
			}
			if g.Effect == "DENY" && g.Scope.Kind != "ALL" {
				return Defined{}, ErrDefinition
			}
			if g.Effect == "ALLOW" {
				for _, action := range g.Actions {
					scopeCounts[action]++
					scopeNodes[action] += nodes
					if scopeCounts[action] > 8 || scopeNodes[action] > 63 {
						return Defined{}, ErrDefinition
					}
				}
			}
		}
		for _, a := range g.Actions {
			// SQL intersects entity and field scopes. Row/aggregate action
			// grants are gates only; accepting an unused scope there is unsafe.
			scoped := strings.HasPrefix(a, "data.entity.") || strings.HasPrefix(a, "data.field.")
			if g.Scope != nil && !scoped || scoped && g.Effect == "ALLOW" && g.Scope == nil {
				return Defined{}, ErrDefinition
			}
		}
	}
	h := sha256.Sum256(raw)
	digest := hex.EncodeToString(h[:])
	if expected != "" && expected != digest {
		return Defined{}, ErrDefinition
	}
	return Defined{definition: d, hash: digest}, nil
}

// Some infrastructure operations retain explicit SQL role reservations until
// their adapters consume capability definitions. Never accept a grant that the
// shipped executor will silently reject after a successful API admission.
func supportedRoleAction(role, action string) bool {
	if action == "roles.write" || action == "roles.assign" {
		return role == "owner"
	}
	if action == "datasets.manage" || action == "datasets.evolve" || action == "indexes.manage" {
		return role == "owner" || role == "admin"
	}
	if role == "viewer" {
		return action != "preview.write" && action != "teams.manage" && action != "tools.invoke" && action != "data.row.share" && action != "data.row.assign" && !(strings.HasPrefix(action, "data.") && strings.HasSuffix(action, ".write")) && action != "workflows.start" && action != "workflows.execute" && action != "workflows.activity" && action != "events.consume"
	}
	return true
}

// Contract keys have one spelling. encoding/json alone also accepts differently
// cased aliases, which could otherwise overwrite a prior typed security field.
func definitionKeys(value any) bool {
	switch v := value.(type) {
	case map[string]any:
		for key, child := range v {
			if key != strings.ToLower(key) {
				return false
			}
			if key != "value" && !definitionKeys(child) {
				return false
			}
		}
	case []any:
		for _, child := range v {
			if !definitionKeys(child) {
				return false
			}
		}
	}
	return true
}
func validateDefinedScope(s DataScope, depth int, n *int) error {
	(*n)++
	if depth > 6 || *n > 32 || len(s.Teams) != 0 || s.WriteShare {
		return ErrDefinition
	}
	switch s.Kind {
	case "ALL", "NONE", "OWNER", "TEAM", "SHARE":
		if s.Condition != nil || len(s.Children) != 0 {
			return ErrDefinition
		}
	case "CONDITION":
		if s.Condition == nil || len(s.Children) != 0 || len(s.Condition.Field) > 160 || !symbol.MatchString(s.Condition.Field) || !strings.HasPrefix(s.Condition.Field, "field.") || !slices.Contains([]string{"eq", "neq", "gt", "gte", "lt", "lte", "contains", "is_null", "in", "not_in"}, s.Condition.Op) || len(s.Condition.Value) > 1024 {
			return ErrDefinition
		}
		if (s.Condition.Op == "is_null") != (len(s.Condition.Value) == 0) {
			return ErrDefinition
		}
	case "AND", "OR":
		if s.Condition != nil || len(s.Children) == 0 || len(s.Children) > 8 {
			return ErrDefinition
		}
		for _, c := range s.Children {
			if validateDefinedScope(c, depth+1, n) != nil {
				return ErrDefinition
			}
		}
	default:
		return ErrDefinition
	}
	return nil
}
func (d Defined) Hash() string { return d.hash }
func (d Defined) Name() string { return d.definition.Name }
func (d Defined) policyVersion() string {
	if d.legacy {
		return "tenant-role-b4:" + d.hash
	}
	return "tenant-role-v1:" + d.hash
}
func selectorMatches(s Selector, r Resource) bool {
	return (s.AppID == "*" || s.AppID == r.AppID) && (s.EntityID == "*" || s.EntityID == r.EntityID) && (s.FieldID == "*" || s.FieldID == r.FieldID)
}
func (d Defined) validSubject(s Subject, r Resource) bool {
	return d.hash != "" && d.tenantID != "" && d.actorID != "" && s.User.ID() == d.actorID && s.Access.TenantID() == d.tenantID && s.User.ID() == s.Access.ActorID() && s.Access.TenantID() == r.TenantID && slices.Contains(s.Access.Roles(), d.Name())
}

func validResource(action string, r Resource, actor string) bool {
	if strings.HasPrefix(action, "data.") {
		return dataResource(action, r)
	}
	switch action {
	case "tenant.access":
		return r.Type == "tenant"
	case "profile.read":
		return r.Type == "user" && r.OwnerID == actor
	case "memberships.read":
		return r.Type == "membership"
	case "roles.read", "roles.write", "roles.assign":
		return r.Type == "role"
	case "teams.read", "teams.manage":
		return r.Type == "team"
	case "tools.invoke":
		return r.Type == "workflow" && r.AppID != "" && r.EntityID != "" && r.RecordID != "" && r.WorkflowID != ""
	case "metadata.read", "metadata.compile", "metadata.write":
		return r.Type == "metadata" && r.AppID != ""
	case "preview.read", "preview.write", "commands.reconcile", "datasets.manage", "datasets.evolve", "indexes.manage":
		return r.Type == "preview" && r.AppID != ""
	case "rules.evaluate":
		return r.Type == "rule" && r.AppID != "" && r.EntityID != "" && r.RuleID != ""
	case "events.consume":
		return r.Type == "event" && r.AppID != "" && r.EntityID != "" && r.RecordID != ""
	default:
		return strings.HasPrefix(action, "workflows.") && r.Type == "workflow" && r.AppID != "" && r.EntityID != "" && r.RecordID != "" && r.WorkflowID != ""
	}
}
func (d Defined) Decide(ctx context.Context, s Subject, action string, r Resource, c Context) Decision {
	decision := (Native{}).Decide(ctx, Subject{}, action, r, c)
	decision.Version = d.policyVersion()
	if decision.ID == "" {
		return decision
	}
	if ctx.Err() != nil || !d.validSubject(s, r) {
		return decision
	}
	if d.legacy {
		decision = (Native{}).Decide(ctx, s, action, r, c)
		decision.Version = d.policyVersion()
		return decision
	}
	if !knownAction(action) || !validResource(action, r, s.User.ID()) {
		return decision
	}
	allow := false
	for _, g := range d.definition.Grants {
		if !selectorMatches(g.Resource, r) {
			continue
		}
		if !slices.Contains(g.Actions, action) {
			continue
		}
		if g.Effect == "DENY" {
			return decision
		}
		allow = true
	}
	if allow {
		decision.Allow = true
		decision.Reason = "authorized"
		decision.Obligations = []string{"audit"}
	}
	return decision
}
func liveScope(s DataScope, c ScopeContext, write bool) DataScope {
	if s.Condition != nil {
		condition := *s.Condition
		condition.Value = append(json.RawMessage{}, condition.Value...)
		s.Condition = &condition
	}
	if s.Kind == "TEAM" {
		s.Teams = append([]string{}, c.Teams...)
	}
	if s.Kind == "SHARE" {
		s.WriteShare = write
	}
	children := make([]DataScope, len(s.Children))
	for i, v := range s.Children {
		children[i] = liveScope(v, c, write)
	}
	s.Children = children
	return s
}
func (d Defined) DataScope(ctx context.Context, s Subject, action string, r Resource, c ScopeContext) (DataScope, error) {
	deny := DataScope{Kind: "NONE"}
	if ctx.Err() != nil || !d.validSubject(s, r) || (action != "data.read" && action != "data.write") || r.AppID == "" || r.EntityID == "" || (r.Type != "entity" && (r.Type != "field" || r.FieldID == "")) {
		return deny, ErrScope
	}
	if d.legacy {
		return (Native{}).DataScope(ctx, s, action, r, c)
	}
	capability := "data.entity.read"
	if r.FieldID != "" {
		capability = "data.field.read"
	}
	write := action == "data.write"
	if write {
		capability = strings.ReplaceAll(capability, "read", "write")
	}
	var scopes []DataScope
	for _, g := range d.definition.Grants {
		if !selectorMatches(g.Resource, r) || !slices.Contains(g.Actions, capability) {
			continue
		}
		if g.Effect == "DENY" {
			return deny, ErrScope
		}
		if g.Scope != nil && g.Scope.Kind != "NONE" {
			scopes = append(scopes, liveScope(*g.Scope, c, write))
		}
	}
	if len(scopes) == 0 || len(scopes) > 8 {
		return deny, ErrScope
	}
	if len(scopes) == 1 {
		return scopes[0], nil
	}
	return DataScope{Kind: "OR", Children: scopes}, nil
}
