// Package policy owns a fail-closed RBAC + ABAC decision port.
package policy

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"github.com/Karthikeyan49/Framwork/internal/identity"
	"github.com/Karthikeyan49/Framwork/internal/tenant"
)

// R5a adds explicit owner-only role administration to the bootstrap adapter;
// R5c-a (v5) adds owner/admin record access administration; R5c-b (v6) adds
// owner/admin team administration; M10 (v7) adds non-viewer tools.invoke; M11 (v8)
// adds sandbox.read (all roles) and owner/admin sandbox.manage; M12 (v9) adds
// requirements/acceptance/candidates read (all roles) and owner/admin
// requirements.decide, acceptance.author and candidates.build; M13 (v10) adds
// releases.read (all roles) and owner/admin releases.approve (approve and
// reject), releases.activate and releases.rollback; M14 (v11) adds ai.read (all
// roles) and owner/admin ai.propose and ai.apply (ADR104 A6).
// Retained evidence keeps its original version.
const Version = "m14-native-v11"

type Resource struct{ TenantID, Type, OwnerID, AppID, EntityID, FieldID, RecordID, RuleID, WorkflowID string }
type Subject struct {
	User   identity.User
	Access tenant.Access
}
type Context struct{ RequestID, TraceID string }
type Decision struct {
	Allow       bool     `json:"allow"`
	Reason      string   `json:"reason"`
	Obligations []string `json:"obligations"`
	ID          string   `json:"decision_id"`
	Version     string   `json:"policy_version"`
}
type Engine interface {
	Decide(context.Context, Subject, string, Resource, Context) Decision
}
type Native struct{}

func (Native) Decide(ctx context.Context, subject Subject, action string, resource Resource, _ Context) Decision {
	access := subject.Access
	d := Decision{Reason: "deny", Version: Version, Obligations: []string{}}
	id := make([]byte, 16)
	if _, err := rand.Read(id); err != nil {
		return d
	}
	d.ID = hex.EncodeToString(id)
	self := permitsSelf(subject.User.ID(), action, resource)
	tenantScoped := subject.User.ID() == access.ActorID() && permits(access.ActorID(), access.TenantID(), access.Roles(), action, resource)
	if ctx.Err() == nil && (self || tenantScoped) {
		d.Allow = true
		d.Reason = "authorized"
		d.Obligations = []string{"audit"}
	}
	return d
}
func permitsSelf(actor, action string, resource Resource) bool {
	return actor != "" && action == "identity.session.read" && resource.Type == "user" && resource.OwnerID == actor && resource.TenantID == ""
}
func permits(actor, tenantID string, roles []string, action string, resource Resource) bool {
	if actor == "" || tenantID == "" || tenantID != resource.TenantID {
		return false
	}
	for _, role := range roles {
		if !tenant.ValidRole(role) || (role != "owner" && role != "admin" && role != "member" && role != "viewer") {
			continue
		}
		switch action {
		case "roles.read":
			if resource.Type == "role" && (role == "owner" || role == "admin") {
				return true
			}
		case "roles.write", "roles.assign":
			if resource.Type == "role" && role == "owner" {
				return true
			}
		case "sandbox.read", "requirements.read", "acceptance.read", "candidates.read", "releases.read", "ai.read":
			if resource.Type == "preview" && resource.AppID != "" {
				return true
			}
		case "requirements.decide", "acceptance.author", "candidates.build", "releases.approve", "releases.activate", "releases.rollback", "ai.propose", "ai.apply":
			// M12/M13: release content and promotion authority stays with tenant
			// administrators by default; custom roles may be granted these
			// explicitly (never viewer). The database ports re-check membership,
			// separation of duties and quorum (ADR103).
			if resource.Type == "preview" && resource.AppID != "" && (role == "owner" || role == "admin") {
				return true
			}
		case "sandbox.manage":
			if resource.Type == "preview" && resource.AppID != "" && (role == "owner" || role == "admin") {
				return true
			}
		case "tools.invoke":
			// M10: governed tool sends run only through pinned workflow plans.
			if role != "viewer" && resource.Type == "workflow" && resource.AppID != "" && resource.EntityID != "" && resource.RecordID != "" && resource.WorkflowID != "" {
				return true
			}
		case "teams.read", "teams.manage":
			if resource.Type == "team" && (role == "owner" || role == "admin") {
				return true
			}
		case "commands.reconcile":
			if resource.Type == "preview" && resource.AppID != "" && role != "viewer" {
				return true
			}
		case "datasets.manage", "datasets.evolve", "indexes.manage":
			if resource.Type == "preview" && resource.AppID != "" && (role == "owner" || role == "admin") {
				return true
			}
		case "workflows.read", "workflows.start", "workflows.execute", "workflows.activity", "workflows.cancel", "workflows.approve", "workflows.reject", "workflows.recover", "workflows.reconcile":
			if resource.Type == "workflow" && resource.AppID != "" && resource.EntityID != "" && resource.RecordID != "" && resource.WorkflowID != "" {
				if action == "workflows.read" || (role != "viewer" && action != "workflows.recover" && action != "workflows.reconcile") || (role == "owner" || role == "admin") {
					return true
				}
			}
		case "events.consume":
			if role != "viewer" && resource.Type == "event" && resource.AppID != "" && resource.EntityID != "" && resource.RecordID != "" {
				return true
			}
		case "rules.evaluate":
			if resource.Type == "rule" && resource.AppID != "" && resource.EntityID != "" && resource.RuleID != "" {
				return true
			}
		case "tenant.access":
			if resource.Type == "tenant" {
				return true
			}
		case "memberships.read":
			if resource.Type == "membership" && (role == "owner" || role == "admin") {
				return true
			}
		case "metadata.read", "metadata.compile":
			if resource.Type == "metadata" {
				return true
			}
		case "preview.read":
			if resource.Type == "preview" {
				return true
			}
		case "preview.write":
			if resource.Type == "preview" && role != "viewer" {
				return true
			}
		case "data.entity.read", "data.field.read", "data.row.read", "data.aggregate.read":
			if dataResource(action, resource) {
				return true
			}
		case "data.entity.write", "data.field.write", "data.row.write":
			if role != "viewer" && dataResource(action, resource) {
				return true
			}
		case "data.row.share", "data.row.assign":
			// Bootstrap roles keep record access administration with tenant
			// administrators; custom roles may delegate it explicitly.
			if (role == "owner" || role == "admin") && dataResource(action, resource) {
				return true
			}
		case "metadata.write":
			if resource.Type == "metadata" && (role == "owner" || role == "admin") {
				return true
			}
		case "profile.read":
			if resource.Type == "user" && resource.OwnerID == actor {
				return true
			}
		}
	}
	return false
}

func dataResource(action string, r Resource) bool {
	if r.AppID == "" || r.EntityID == "" {
		return false
	}
	if action == "data.entity.read" || action == "data.entity.write" {
		return r.Type == "entity"
	}
	if action == "data.aggregate.read" {
		return r.Type == "aggregate"
	}
	if action == "data.field.read" || action == "data.field.write" {
		return r.Type == "field" && r.FieldID != ""
	}
	// Read action permission may be checked once for a collection. SQL DataScope
	// supplies its mandatory row predicate; action permission alone exposes no rows.
	if action == "data.row.share" || action == "data.row.assign" {
		return r.Type == "row" && r.RecordID != ""
	}
	return r.Type == "row" && (r.RecordID != "" || action == "data.row.read" && r.OwnerID == "")
}
