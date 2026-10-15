package access

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"regexp"
	"strconv"

	"github.com/Karthikeyan49/Framwork/internal/audit"
	"github.com/Karthikeyan49/Framwork/internal/compiler"
	"github.com/Karthikeyan49/Framwork/internal/metadata"
	"github.com/Karthikeyan49/Framwork/internal/platform/postgres"
	"github.com/Karthikeyan49/Framwork/internal/tenant"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	"strings"
)

var metadataPOST = regexp.MustCompile(`^/v1/tenants/[0-9a-f-]{36}/metadata/app\.[a-z][a-z0-9_]*(\.[a-z][a-z0-9_]*)*/versions$`)

var metadataAppID = regexp.MustCompile(`^app\.[a-z][a-z0-9_]*(\.[a-z][a-z0-9_]*)*$`)

func (a API) AcceptsPOST(path string) bool {
	return rolePOST.MatchString(path) || teamPOST.MatchString(path) || a.Registry != nil && (metadataPOST.MatchString(path) || a.PreviewEnabled && (previewPOST.MatchString(path) || rulesPOST.MatchString(path) || workflowsPOST.MatchString(path) || datasetManagementPOST.MatchString(path) || datasetDataPOST.MatchString(path) || indexesPOST.MatchString(path) || recordAccessPOST.MatchString(path) || evolutionPOST.MatchString(path)))
}
func (a API) metadata(ctx context.Context, q postgres.Executor, r *http.Request, scope tenant.Access, evidence *audit.Entry) (reply, error) {
	if a.Registry == nil {
		return reply{}, metadata.ErrUnavailable
	}
	app := r.PathValue("app")
	evidence.State.MetadataApp = app
	if len(app) > 160 || !metadataAppID.MatchString(app) {
		evidence.State.MetadataApp = ""
		return denied(400, "invalid_app", "Invalid application identifier"), nil
	}
	if r.Method == http.MethodPost {
		contentType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if err != nil || (contentType != "application/json" && contentType != "application/yaml") {
			return denied(415, "unsupported_media_type", "Use application/json or application/yaml"), nil
		}
		raw, err := io.ReadAll(io.LimitReader(r.Body, metadata.MaxBytes+4097))
		if err != nil {
			return reply{}, err
		}
		if len(raw) > metadata.MaxBytes+4096 {
			return denied(413, "body_too_large", "Metadata request is too large"), nil
		}
		var value any
		if contentType == "application/yaml" {
			value, err = metadata.DecodeYAML(raw)
		} else {
			value, err = metadata.DecodeJSON(raw)
		}
		if err != nil {
			return metadataInvalid(err), nil
		}
		object, ok := value.(map[string]any)
		if !ok || len(object) != 2 || object["document"] == nil || object["expected_revision"] == nil {
			return metadataInvalid(metadata.ErrInvalid), nil
		}
		expected, ok := object["expected_revision"].(json.Number)
		if !ok {
			return metadataInvalid(metadata.ErrInvalid), nil
		}
		revision, err := expected.Int64()
		if err != nil || revision < 0 || revision >= 1000000000 {
			return metadataInvalid(metadata.ErrInvalid), nil
		}
		sourceJSON, err := json.Marshal(object["document"])
		if err != nil {
			return metadataInvalid(err), nil
		}
		source, err := a.Registry.Parse(sourceJSON)
		if err != nil {
			return metadataInvalid(err), nil
		}
		if source.AppID() != app {
			return denied(400, "app_mismatch", "Document application must match the route"), nil
		}
		v, err := a.Registry.Append(ctx, q, scope, revision, source)
		if errors.Is(err, metadata.ErrConflict) {
			return denied(409, "revision_conflict", "Reload the current revision before appending"), nil
		}
		if err != nil {
			return reply{}, err
		}
		metadataEvidence(ctx, evidence, v)
		return reply{status: 201, body: v}, nil
	}
	if r.PathValue("revision") != "" {
		revision, err := strconv.ParseInt(r.PathValue("revision"), 10, 64)
		if err != nil || revision < 1 || revision > 1000000000 {
			return denied(400, "invalid_revision", "Invalid revision"), nil
		}
		version, err := a.Registry.Get(ctx, q, scope, app, revision)
		if errors.Is(err, metadata.ErrNotFound) {
			return denied(404, "not_found", "Metadata version not found"), nil
		}
		if err != nil {
			return reply{}, err
		}
		metadataEvidence(ctx, evidence, version)
		if strings.HasSuffix(r.URL.Path, "/compile") {
			source, err := a.Registry.Parse(version.Document)
			if err != nil {
				return reply{}, err
			}
			program, diagnostics := compiler.Compile(source, a.catalog())
			compilerVersion := compiler.Version
			if program.Valid() {
				compilerVersion = program.IR().CompilerVersion
			} else if version.SchemaVersion == metadata.RuleSchemaVersion {
				compilerVersion = compiler.RuleCompilerVersion
			}
			trace.SpanFromContext(ctx).SetAttributes(attribute.String("compiler_version", compilerVersion))
			status := 200
			if !program.Valid() {
				status = 422
			}
			return reply{status: status, body: struct {
				Hash        string                `json:"ir_hash"`
				IR          json.RawMessage       `json:"ir,omitempty"`
				Diagnostics []compiler.Diagnostic `json:"diagnostics"`
			}{program.Hash(), program.JSON(), diagnostics}}, nil
		}
		return reply{status: 200, body: version}, nil
	}
	before := int64(0)
	if r.URL.Query().Get("before") != "" {
		var err error
		before, err = strconv.ParseInt(r.URL.Query().Get("before"), 10, 64)
		if err != nil || before < 1 || before > 1000000000 {
			return denied(400, "invalid_cursor", "Invalid version cursor"), nil
		}
	}
	versions, err := a.Registry.List(ctx, q, scope, app, before)
	if err != nil {
		return reply{}, err
	}
	next := int64(0)
	if len(versions) == 50 {
		next = versions[49].Revision
	}
	return reply{status: 200, body: struct {
		Versions   []metadata.Version `json:"versions"`
		NextBefore int64              `json:"next_before"`
	}{versions, next}}, nil
}
func metadataInvalid(err error) reply {
	issues := []metadata.Issue{{Path: "", Code: "invalid_metadata", Message: "Metadata request is invalid"}}
	var detail *metadata.Invalid
	if errors.As(err, &detail) {
		issues = detail.Issues
	}
	return reply{status: 400, code: "invalid_metadata", message: "Metadata request failed validation", issues: issues}
}
func metadataEvidence(ctx context.Context, evidence *audit.Entry, version metadata.Version) {
	evidence.State.MetadataRevision = version.Revision
	evidence.State.MetadataHash = version.SourceHash
	trace.SpanFromContext(ctx).SetAttributes(attribute.String("metadata_app", version.AppID), attribute.Int64("metadata_version", version.Revision), attribute.String("metadata_source_hash", version.SourceHash))
}
