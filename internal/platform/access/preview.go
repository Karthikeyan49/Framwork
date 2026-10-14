package access

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/Karthikeyan49/Framwork/internal/audit"
	"github.com/Karthikeyan49/Framwork/internal/compiler"
	"github.com/Karthikeyan49/Framwork/internal/events"
	"github.com/Karthikeyan49/Framwork/internal/identity"
	"github.com/Karthikeyan49/Framwork/internal/metadata"
	"github.com/Karthikeyan49/Framwork/internal/platform/postgres"
	"github.com/Karthikeyan49/Framwork/internal/policy"
	"github.com/Karthikeyan49/Framwork/internal/runtime/data"
	"github.com/Karthikeyan49/Framwork/internal/tenant"
	"github.com/jackc/pgx/v5/pgconn"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	"io"
	"mime"
	"net/http"
	"regexp"
	"strconv"
	"strings"
)

var previewPOST = regexp.MustCompile(`^/v1/tenants/[0-9a-f-]{36}/preview/app\.[a-z][a-z0-9_]*(\.[a-z][a-z0-9_]*)*/versions/[0-9]+/entities/entity\.[a-z][a-z0-9_]*(\.[a-z][a-z0-9_]*)*/(query|commands)$`)
var rulesPOST = regexp.MustCompile(`^/v1/tenants/[0-9a-f-]{36}/preview/app\.[a-z][a-z0-9_]*(\.[a-z][a-z0-9_]*)*/versions/[0-9]+/rules/rule\.[a-z][a-z0-9_]*(\.[a-z][a-z0-9_]*)*/evaluate$`)

func readDataCommand(r *http.Request, dest any) error {
	typ, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || typ != "application/json" {
		return data.ErrInvalid
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, data.MaxRecordBytes+1))
	if err != nil || len(raw) > data.MaxRecordBytes {
		return data.ErrInvalid
	}
	value, err := metadata.DecodeJSON(raw)
	if err != nil {
		return data.ErrInvalid
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return data.ErrInvalid
	}
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.UseNumber()
	decoder.DisallowUnknownFields()
	if decoder.Decode(dest) != nil {
		return data.ErrInvalid
	}
	return nil
}
func (a API) preview(ctx context.Context, q postgres.Executor, r *http.Request, u identity.User, access tenant.Access, evidence *audit.Entry) (reply, error) {
	if !a.PreviewEnabled || a.Registry == nil {
		return denied(404, "not_found", "Preview runtime is unavailable"), nil
	}
	app := r.PathValue("app")
	revision, err := strconv.ParseInt(r.PathValue("revision"), 10, 64)
	if err != nil || revision < 1 || revision > 1e9 || len(app) > 160 || !metadataAppID.MatchString(app) {
		return denied(400, "invalid_preview", "Invalid preview selection"), nil
	}
	version, err := a.Registry.Get(ctx, q, access, app, revision)
	if errors.Is(err, metadata.ErrNotFound) {
		return denied(404, "not_found", "Preview source not found"), nil
	}
	if err != nil {
		return reply{}, err
	}
	// In-flight reads/signals restore stored IR, never today's compiler output.
	if r.PathValue("workflow") != "" && r.PathValue("instance") != "" {
		return a.previewWorkflow(ctx, q, r, u, access, evidence, compiler.Program{}, revision)
	}
	source, err := a.Registry.Parse(version.Document)
	if err != nil {
		return reply{}, err
	}
	program, diagnostics := compiler.Compile(source, a.catalog())
	if !program.Valid() {
		return reply{status: 422, body: struct {
			Diagnostics []compiler.Diagnostic `json:"diagnostics"`
		}{diagnostics}}, nil
	}
	scope, err := data.NewScope(program, revision)
	if err != nil {
		return reply{}, err
	}
	metadataEvidence(ctx, evidence, version)
	evidence.State.IRHash = program.Hash()
	trace.SpanFromContext(ctx).SetAttributes(attribute.String("compiler_version", program.IR().CompilerVersion), attribute.String("ir_hash", program.Hash()), attribute.String("data_mode", "preview"))
	return a.executePreview(ctx, q, r, u, access, evidence, scope)
}

func (a API) executePreview(ctx context.Context, q postgres.Executor, r *http.Request, u identity.User, access tenant.Access, evidence *audit.Entry, scope data.Scope) (reply, error) {
	app, program, revision := scope.AppID(), scope.Program(), scope.Revision()
	trace.SpanFromContext(ctx).SetAttributes(attribute.String("compiler_version", program.IR().CompilerVersion), attribute.String("ir_hash", scope.Hash()), attribute.String("data_mode", "preview"))
	if scope.DatasetID() != "" {
		trace.SpanFromContext(ctx).SetAttributes(attribute.String("dataset_id", scope.DatasetID()), attribute.Int64("dataset_binding", scope.BindingVersion()), attribute.Int64("data_schema_generation", 1))
	}
	guard := func(ctx context.Context, action string, resource data.Resource) error {
		decision := enforce(a.Policy.Decide(ctx, policy.Subject{User: u, Access: access}, action, policy.Resource{TenantID: access.TenantID(), Type: strings.Split(action, ".")[1], OwnerID: resource.OwnerID, AppID: app, EntityID: resource.EntityID, FieldID: resource.FieldID, RecordID: resource.RecordID}, policy.Context{RequestID: evidence.RequestID, TraceID: evidence.TraceID}))
		event := *evidence
		event.Action = action
		event.Resource = strings.Split(action, ".")[1]
		event.DecisionID = decision.ID
		event.PolicyVersion = decision.Version
		event.Allow = decision.Allow
		event.State.EntityID = resource.EntityID
		event.State.FieldID = resource.FieldID
		event.State.RecordID = resource.RecordID
		if err := a.Audit.Append(ctx, q, event); err != nil {
			return err
		}
		if !decision.Allow {
			return data.ErrDenied
		}
		return nil
	}
	if r.PathValue("workflow") != "" {
		return a.previewWorkflow(ctx, q, r, u, access, evidence, program, revision)
	}
	if r.Method == http.MethodGet {
		// Capability hints improve UX; each command/query still enforces live policy.
		canWrite := enforce(a.Policy.Decide(ctx, policy.Subject{User: u, Access: access}, "preview.write", policy.Resource{TenantID: access.TenantID(), Type: "preview", AppID: app}, policy.Context{})).Allow
		writable, err := (data.DatasetStore{}).Writable(ctx, q, scope, access)
		if err != nil {
			return reply{}, err
		}
		canWrite = canWrite && writable
		return reply{status: 200, body: struct {
			Mode           string      `json:"mode"`
			Revision       int64       `json:"revision"`
			Hash           string      `json:"ir_hash"`
			IR             compiler.IR `json:"ir"`
			CanWrite       bool        `json:"can_write"`
			DatasetID      string      `json:"dataset_id,omitempty"`
			BindingVersion int64       `json:"binding_version,omitempty"`
		}{"preview", revision, program.Hash(), program.IR(), canWrite, scope.DatasetID(), scope.BindingVersion()}}, nil
	}
	entity := r.PathValue("entity")
	teams, err := (tenant.Store{}).Teams(ctx, q, access)
	if err != nil {
		return reply{}, err
	}
	scopes := map[string]policy.DataScope{}
	resolve := func(ctx context.Context, action string, resource data.Resource) (policy.DataScope, error) {
		engine, ok := a.Policy.(policy.ScopeEngine)
		if !ok {
			return policy.DataScope{}, policy.ErrScope
		}
		key := action + ":" + resource.EntityID + ":" + resource.FieldID
		if scope, ok := scopes[key]; ok {
			return scope, nil
		}
		typ := "entity"
		if resource.FieldID != "" {
			typ = "field"
		}
		scope, e := engine.DataScope(ctx, policy.Subject{User: u, Access: access}, action, policy.Resource{TenantID: access.TenantID(), Type: typ, AppID: app, EntityID: resource.EntityID, FieldID: resource.FieldID}, policy.ScopeContext{Teams: teams, RequestID: evidence.RequestID, TraceID: evidence.TraceID})
		entry := *evidence
		entry.Action = strings.Replace(action, "data.", "data.scope.", 1)
		entry.Resource = typ
		entry.Allow = e == nil
		entry.State.EntityID = resource.EntityID
		entry.State.FieldID = resource.FieldID
		raw, _ := json.Marshal(scope)
		hash := sha256.Sum256(raw)
		entry.State.ScopeHash = hex.EncodeToString(hash[:])
		if er := a.Audit.Append(ctx, q, entry); er != nil {
			return policy.DataScope{}, er
		}
		if e != nil {
			return policy.DataScope{}, e
		}
		scopes[key] = scope
		return scope, nil
	}
	if r.PathValue("rule") != "" {
		return a.evaluateRule(ctx, q, r, u, access, evidence, program, scope, guard, resolve)
	}
	evidence.State.EntityID = entity
	if len(entity) > 160 {
		evidence.State.EntityID = ""
		return denied(400, "invalid_entity", "Invalid entity selection"), nil
	}
	var result any
	if strings.HasSuffix(r.URL.Path, "/query") {
		var query data.Query
		if err = readDataCommand(r, &query); err == nil {
			result, err = (data.Store{}).Query(ctx, q, scope, access, entity, query, guard, resolve)
		}
	} else {
		var command data.Command
		if err = readDataCommand(r, &command); err == nil {
			if tenant.ValidID(command.Key) {
				evidence.State.CommandKey = command.Key
			}
			var record data.Record
			record, err = (data.Store{}).Command(ctx, q, scope, access, entity, command, guard, resolve)
			result = record
			evidence.State.RecordID = record.ID
			evidence.State.RecordVersion = record.Version
			evidence.State.CommandReplayed = record.Replayed
			if err == nil && !record.Replayed {
				id, e := events.NewID()
				if e != nil {
					return reply{}, e
				}
				eventType := map[string]string{"create": "record.created", "update": "record.updated", "delete": "record.deleted"}[command.Operation]
				entry := *evidence
				entry.Action = eventType
				entry.Resource = "record"
				entry.Allow = true
				writer := a.Events
				if writer == nil {
					writer = events.Postgres{}
				}
				intent := events.Intent{ID: id, TenantID: access.TenantID(), AppID: app, IRHash: program.Hash(), EntityID: entity, RecordID: record.ID, Type: eventType, MetadataRevision: revision, RecordVersion: record.Version, RequestID: entry.RequestID}
				if scope.DatasetID() != "" {
					intent.DataRevision, intent.DataHash = scope.DataRevision(), scope.DataHash()
				}
				if e = events.Append(ctx, q, a.Audit, writer, entry, intent); e != nil {
					return reply{}, e
				}
				if e = a.workflowTriggers(ctx, q, u, access, program, events.Intent{ID: id, TenantID: access.TenantID(), AppID: app, IRHash: program.Hash(), EntityID: entity, RecordID: record.ID, Type: eventType, MetadataRevision: revision, RecordVersion: record.Version, RequestID: entry.RequestID}); e != nil {
					return reply{}, e
				}
			}
		}
	}
	var databaseError *pgconn.PgError
	if errors.As(err, &databaseError) {
		switch databaseError.Code {
		case "23503", "55P03":
			// These SQL failures abort the transaction. Roll back the mutation,
			// receipt, audit and outbox before auditing the defined rejection.
			return reply{}, &requestRejection{response: denied(409, "record_conflict", "A concurrent change or reference prevents this command; reconcile and reload before retrying")}
		case "55000":
			return reply{}, &requestRejection{response: denied(409, "dataset_conflict", "Select the active dataset binding before retrying")}
		}
	}
	switch {
	case err == nil:
		return reply{status: 200, body: result}, nil
	case errors.Is(err, data.ErrInvalid), errors.Is(err, data.ErrBudget):
		return denied(400, "invalid_query_or_command", "Data request violates the typed contract or budget"), nil
	case errors.Is(err, data.ErrDenied):
		return denied(403, "forbidden", "Data access is denied"), nil
	case errors.Is(err, data.ErrBinding):
		return denied(409, "dataset_conflict", "Select the active dataset binding before retrying"), nil
	case errors.Is(err, data.ErrConflict):
		return denied(409, "record_conflict", "Reload the record before retrying"), nil
	case errors.Is(err, data.ErrCommandConflict):
		return denied(409, "command_conflict", "This command key already identifies a different request"), nil
	case errors.Is(err, data.ErrNotFound):
		return denied(404, "not_found", "Record not found"), nil
	default:
		return reply{}, err
	}
}
