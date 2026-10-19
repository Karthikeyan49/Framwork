package ai

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"

	contracts "github.com/Karthikeyan49/Framwork/contracts/jsonschema"
)

// The closed output schemas handed to a provider as ProviderRequest.OutputSchema
// (JSON Schema 2020-12). They describe what the strict decoders of schema.go accept;
// they are guidance for a provider that can constrain its output, never the gate:
// the decoders are. A test proves that every proposal the decoders accept validates
// against these schemas and that a table of structural violations does not.
//
// What JSON Schema cannot say (definition.id equal to the operation target, sorted
// role lists, hash agreement) is enforced by the decoders and ApplyProposal only.

const (
	idPatternText     = `[a-z][a-z0-9_]*(\.[a-z][a-z0-9_]*)*`
	itemIDPatternText = `^[a-z][a-z0-9_.-]{0,63}$`
)

var metadataSchemaFiles = map[string]string{
	"1.0.0": "application-v1.schema.json",
	"1.1.0": "application-v1.1.schema.json",
	"1.2.0": "application-v1.2.schema.json",
	"1.3.0": "application-v1.3.schema.json",
}

var patchSchemas sync.Map // document schema version -> json.RawMessage

func obj(kv ...any) map[string]any {
	m := make(map[string]any, len(kv)/2)
	for i := 0; i < len(kv); i += 2 {
		m[kv[i].(string)] = kv[i+1]
	}
	return m
}

func text(minLen, maxLen int) map[string]any {
	return obj("type", "string", "minLength", minLen, "maxLength", maxLen)
}

func listOf(item any) map[string]any {
	return obj("type", "array", "maxItems", MaxListItems, "items", item)
}

func ref(name string) map[string]any { return obj("$ref", "#/$defs/"+name) }

// commonDefs are the definitions shared by both proposal schemas.
func commonDefs() map[string]any {
	scalar := obj("oneOf", []any{
		obj("type", "string", "maxLength", 256),
		obj("type", "number"),
		obj("type", "boolean"),
		obj("type", "object", "additionalProperties", false, "required", []any{"amount", "currency"},
			"properties", obj("amount", obj("type", "string", "maxLength", 64), "currency", obj("type", "string", "pattern", "^[A-Z]{3}$"))),
	})
	roles := []any{"admin", "member", "owner", "viewer"}
	test := obj("type", "object", "additionalProperties", false, "required", []any{"id", "requirement_id", "expect"},
		"properties", obj(
			"id", obj("type", "string", "pattern", itemIDPatternText),
			"requirement_id", obj("type", "string", "pattern", itemIDPatternText),
			"given", obj("type", "object", "maxProperties", 32,
				"propertyNames", obj("maxLength", 160, "pattern", "^field\\."+idPatternText[:len(idPatternText)]+"$"),
				"additionalProperties", ref("scalar")),
			"expect", obj("type", "object", "additionalProperties", false, "required", []any{"outcome"},
				"properties", obj(
					"outcome", obj("enum", []any{"NO_APPROVAL", "APPROVAL_REQUIRED", "INPUT_REJECTED", "HOLDS"}),
					"roles", obj("type", "array", "maxItems", 4, "uniqueItems", true, "items", obj("enum", roles)),
					"reason", obj("enum", []any{"TYPE", "CURRENCY", "ENUM", "REQUIRED", "RANGE"})))))
	question := obj("type", "object", "additionalProperties", false, "required", []any{"id", "question"},
		"properties", obj(
			"id", obj("type", "string", "pattern", itemIDPatternText),
			"question", text(1, MaxTextRunes),
			"category", obj("type", "string", "pattern", "^[a-z][a-z0-9_]{0,63}$")))
	return map[string]any{"scalar": scalar, "test": test, "question": question}
}

// PatchOutputSchema returns the closed schema of ai-patch-proposal-v1 for documents of
// the given metadata schema version. Its definition rule is the metadata schema of
// that version restricted to the allowlisted kinds, so a tool definition is not
// representable. Output is deterministic.
func PatchOutputSchema(documentSchemaVersion string) (json.RawMessage, error) {
	if v, ok := patchSchemas.Load(documentSchemaVersion); ok {
		return v.(json.RawMessage), nil
	}
	file, ok := metadataSchemaFiles[documentSchemaVersion]
	if !ok {
		return nil, fmt.Errorf("%w: no metadata schema for version %q", ErrInvalidInput, documentSchemaVersion)
	}
	raw, err := contracts.Files.ReadFile(file)
	if err != nil {
		return nil, err
	}
	var meta struct {
		Defs map[string]any `json:"$defs"`
	}
	if err = json.Unmarshal(raw, &meta); err != nil {
		return nil, err
	}
	definition, _ := meta.Defs["definition"].(map[string]any)
	alternatives, _ := definition["oneOf"].([]any)
	kept := []any{}
	for _, alt := range alternatives {
		kind := ""
		if props, ok := alt.(map[string]any)["properties"].(map[string]any); ok {
			if k, ok := props["kind"].(map[string]any); ok {
				kind, _ = k["const"].(string)
			}
		}
		if kindAllowed(kind) {
			kept = append(kept, alt)
		}
	}
	meta.Defs["definition"] = obj("oneOf", kept)

	// Keep only what the allowed kinds reach: the tool and value-schema rules drop out.
	reach := map[string]bool{}
	var walk func(node any)
	walk = func(node any) {
		switch v := node.(type) {
		case map[string]any:
			if r, ok := v["$ref"].(string); ok {
				if name, found := strings.CutPrefix(r, "#/$defs/"); found && !reach[name] {
					reach[name] = true
					walk(meta.Defs[name])
				}
			}
			for _, child := range v {
				walk(child)
			}
		case []any:
			for _, child := range v {
				walk(child)
			}
		}
	}
	reach["definition"] = true
	walk(meta.Defs["definition"])
	defs := map[string]any{}
	for name := range reach {
		defs[name] = meta.Defs[name]
	}
	for name, d := range commonDefs() {
		defs[name] = d
	}
	hash := obj("type", "string", "pattern", "^[0-9a-f]{64}$")
	defs["operation"] = obj("type", "object", "additionalProperties", false,
		"required", []any{"op", "target", "rationale"},
		"properties", obj(
			"op", obj("enum", []any{"add", "update", "delete"}),
			"target", obj("type", "string", "maxLength", maxTargetBytes, "pattern", "^("+strings.Join(allowedKinds, "|")+")\\."+idPatternText+"$"),
			"expected_hash", obj("type", "string", "pattern", "^([0-9a-f]{64})?$"),
			"definition", obj("anyOf", []any{obj("type", "null"), ref("definition")}),
			"rationale", text(1, MaxTextRunes)),
		"allOf", []any{
			obj("if", obj("properties", obj("op", obj("const", "add")), "required", []any{"op"}),
				"then", obj("required", []any{"definition"}, "properties", obj("expected_hash", obj("const", ""), "definition", ref("definition")))),
			obj("if", obj("properties", obj("op", obj("const", "update")), "required", []any{"op"}),
				"then", obj("required", []any{"definition", "expected_hash"}, "properties", obj("expected_hash", hash, "definition", ref("definition")))),
			obj("if", obj("properties", obj("op", obj("const", "delete")), "required", []any{"op"}),
				"then", obj("required", []any{"expected_hash"}, "properties", obj("expected_hash", hash, "definition", obj("type", "null")))),
		})
	defs["risk_flag"] = obj("type", "object", "additionalProperties", false, "required", []any{"code", "detail"},
		"properties", obj("code", obj("type", "string", "pattern", "^[A-Za-z][A-Za-z0-9_.-]{0,63}$"), "detail", text(0, MaxTextRunes)))

	root := obj(
		"$schema", "https://json-schema.org/draft/2020-12/schema",
		"$id", "urn:framework:ai:"+PatchProposalVersion+":document-"+documentSchemaVersion,
		"title", PatchProposalVersion,
		"description", "Closed output schema of an application patch proposal. Server-derived values (change class, risk, status, provenance, requiredness, actor, hashes) are not representable.",
		"type", "object", "additionalProperties", false,
		"required", []any{"version", "operations", "assumptions", "unresolved_requirements", "proposed_tests", "risk_flags"},
		"properties", obj(
			"version", obj("const", PatchProposalVersion),
			"operations", obj("type", "array", "maxItems", MaxOperations, "items", ref("operation")),
			"assumptions", listOf(text(1, MaxTextRunes)),
			"unresolved_requirements", listOf(ref("question")),
			"proposed_tests", listOf(ref("test")),
			"risk_flags", listOf(ref("risk_flag"))),
		"$defs", defs)
	out, err := json.Marshal(root)
	if err != nil {
		return nil, err
	}
	patchSchemas.Store(documentSchemaVersion, json.RawMessage(out))
	return out, nil
}

var (
	requirementSchemaOnce sync.Once
	requirementSchemaText json.RawMessage
)

// RequirementOutputSchema returns the closed schema of ai-requirement-proposal-v1:
// one typed semantics object per supported category, open questions for the rest.
func RequirementOutputSchema() json.RawMessage {
	requirementSchemaOnce.Do(func() {
		defs := commonDefs()
		idRef := func(kind string) map[string]any {
			return obj("type", "string", "maxLength", 160, "pattern", "^"+kind+"\\."+idPatternText+"$")
		}
		roles := obj("type", "array", "minItems", 1, "maxItems", 4, "uniqueItems", true, "items", obj("enum", []any{"admin", "member", "owner", "viewer"}),
			"description", "sorted ascending, no duplicates")
		defs["semantics_approval_threshold"] = obj("type", "object", "additionalProperties", false,
			"required", []any{"workflow_id", "entity_id", "field_id", "currency", "op", "amount", "approver_roles"},
			"properties", obj(
				"workflow_id", idRef("workflow"), "entity_id", idRef("entity"), "field_id", idRef("field"),
				"currency", obj("type", "string", "pattern", "^[A-Z]{3}$"),
				"op", obj("enum", []any{"GT", "GTE", "LT", "LTE", "EQ", "NEQ"}),
				"amount", obj("type", "string", "maxLength", 64, "pattern", "^-?(0|[1-9][0-9]*)(\\.[0-9]+)?$"),
				"approver_roles", roles,
				"context", obj("type", "object", "maxProperties", 32, "propertyNames", obj("pattern", "^field\\."+idPatternText+"$"), "additionalProperties", ref("scalar"))))
		defs["semantics_approval_gate"] = obj("type", "object", "additionalProperties", false,
			"required", []any{"workflow_id", "approver_roles"},
			"properties", obj("workflow_id", idRef("workflow"), "approver_roles", roles))
		defs["semantics_tool_effect"] = obj("type", "object", "additionalProperties", false,
			"required", []any{"tool_ref", "max_side_effect", "requires_approval", "max_sandbox_mode"},
			"properties", obj(
				"tool_ref", idRef("tool"),
				"max_side_effect", obj("enum", []any{"NONE", "READ", "WRITE"}),
				"requires_approval", obj("type", "boolean"),
				"max_sandbox_mode", obj("enum", []any{"BLOCK", "SIMULATE", "READ_ONLY", "REROUTE", "TEST_ACCOUNT"})))
		defs["semantics_data_migration"] = obj("type", "object", "additionalProperties", false,
			"required", []any{"max_class"}, "properties", obj("max_class", obj("enum", []any{"COMPATIBLE", "TRANSFORM"})))
		branch := func(category string) map[string]any {
			return obj("properties", obj("category", obj("const", category), "semantics", ref("semantics_"+category)))
		}
		defs["draft"] = obj("type", "object", "additionalProperties", false,
			"required", []any{"id", "category", "statement", "semantics"},
			"properties", obj(
				"id", obj("type", "string", "pattern", itemIDPatternText),
				"category", obj("enum", []any{"approval_threshold", "approval_gate", "tool_effect", "data_migration"}),
				"statement", text(1, MaxTextRunes),
				"semantics", obj("type", "object")),
			"oneOf", []any{branch("approval_threshold"), branch("approval_gate"), branch("tool_effect"), branch("data_migration")})
		root := obj(
			"$schema", "https://json-schema.org/draft/2020-12/schema",
			"$id", "urn:framework:ai:"+RequirementProposalVersion,
			"title", RequirementProposalVersion,
			"description", "Closed output schema of a requirement proposal. Requiredness, its basis, status and provenance are set by the server when a human confirms; anything outside the four supported categories is an open question.",
			"type", "object", "additionalProperties", false,
			"required", []any{"version", "requirements", "open_questions", "proposed_tests", "assumptions"},
			"properties", obj(
				"version", obj("const", RequirementProposalVersion),
				"requirements", listOf(ref("draft")),
				"open_questions", listOf(ref("question")),
				"proposed_tests", listOf(ref("test")),
				"assumptions", listOf(text(1, MaxTextRunes))),
			"$defs", defs)
		out, err := json.Marshal(root)
		if err != nil {
			panic(err) // a static structure: cannot fail
		}
		requirementSchemaText = out
	})
	return requirementSchemaText
}

// schemaVersions lists the document versions PatchOutputSchema serves, sorted.
func schemaVersions() []string {
	out := make([]string, 0, len(metadataSchemaFiles))
	for v := range metadataSchemaFiles {
		out = append(out, v)
	}
	sort.Strings(out)
	return out
}
