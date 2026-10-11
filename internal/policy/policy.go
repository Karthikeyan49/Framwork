// Package policy owns a fail-closed RBAC + ABAC decision port.
package policy

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"github.com/Karthikeyan49/Framwork/internal/identity"
	"github.com/Karthikeyan49/Framwork/internal/tenant"
)

const Version = "m1-native-v1"

type Resource struct{ TenantID, Type, OwnerID string }
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
		if !tenant.ValidRole(role) {
			continue
		}
		switch action {
		case "tenant.access":
			if resource.Type == "tenant" {
				return true
			}
		case "memberships.read":
			if resource.Type == "membership" && (role == "owner" || role == "admin") {
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
