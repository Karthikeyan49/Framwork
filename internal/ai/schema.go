package ai

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/Karthikeyan49/Framwork/internal/intent"
	"github.com/Karthikeyan49/Framwork/internal/metadata"
)

// Strict decoding of model output (ADR104 section 5). A payload is accepted only if
//
//  1. it is one well-formed JSON document inside the size, depth and node bounds with
//     no duplicate or case-folded duplicate key (the metadata.DecodeJSON rules);
//  2. every key is spelled exactly as the closed schema spells it, no key is unknown,
//     none names a server-derived field, no required key is missing and none is null;
//  3. every definition body satisfies the same JSON schema authored metadata does;
//  4. every identifier, enumeration, length and list bound of this file holds.
//
// Errors are *ProposalError values whose classes never echo model-supplied text.

// Codes of metadata.Invalid that decide the error class (metadata/validation.go).
const (
	codeDuplicateKey = "duplicate_key"
	codeComplexity   = "complexity_limit"
	codeNumberLimit  = "number_limit"
)

var (
	targetPattern   = regexp.MustCompile(`^([a-z]+)\.[a-z][a-z0-9_]*(\.[a-z][a-z0-9_]*)*$`)
	hashPattern     = regexp.MustCompile(`^[0-9a-f]{64}$`)
	itemIDPattern   = regexp.MustCompile(`^[a-z][a-z0-9_.-]{0,63}$`)
	categoryPattern = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)
	riskCodePattern = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_.-]{0,63}$`)
)

const maxTargetBytes = 160

// serverDerived names (normalised: lower case, no "_" or "-") the model can never
// supply. An unknown key among them fails with ErrServerField instead of the generic
// unknown-field class, so a trace and a test can tell an injection attempt from a typo.
var serverDerived = map[string]bool{
	"changeclass": true, "class": true, "risk": true, "risklevel": true, "riskscore": true,
	"sensitive": true, "sensitiveaspects": true, "aspects": true, "derived": true, "targets": true,
	"status": true, "provenance": true, "requiredness": true, "requirednessbasis": true, "basis": true,
	"mandatory": true, "actor": true, "actorid": true, "author": true, "requester": true, "approver": true,
	"approvedby": true, "approval": true, "approvals": true, "acceptance": true, "hash": true, "hashes": true,
	"proposalhash": true, "contexthash": true, "requesthash": true, "outputhash": true, "proposalid": true,
	"revision": true, "expectedrevision": true, "createdat": true, "updatedat": true, "timestamp": true,
	"tenant": true, "tenantid": true, "user": true, "userid": true, "role": true, "roles": true,
	"permission": true, "permissions": true, "policy": true, "release": true, "sandbox": true,
	"acknowledge": true, "applied": true, "confirmed": true,
}

func serverField(key string) bool {
	return serverDerived[strings.ToLower(strings.NewReplacer("_", "", "-", "").Replace(key))]
}

// ---------------------------------------------------------------------- decoding

// strictTree decodes raw into a generic tree under every JSON-level rule.
func strictTree(raw []byte) (any, *ProposalError) {
	switch {
	case len(raw) == 0:
		return nil, proposalErr(ErrMalformed, "", "empty payload")
	case len(raw) > MaxOutputBytes:
		return nil, proposalErr(ErrTooLarge, "", "payload exceeds the output limit")
	}
	tree, err := metadata.DecodeJSONBounded(raw, MaxOutputBytes)
	if err == nil {
		return tree, nil
	}
	var invalid *metadata.Invalid
	if errors.As(err, &invalid) && len(invalid.Issues) > 0 {
		switch invalid.Issues[0].Code {
		case codeDuplicateKey:
			return nil, proposalErr(ErrDuplicateKey, "", "two keys are equal, or equal after Unicode case folding")
		case codeComplexity:
			return nil, proposalErr(ErrTooLarge, "", "nesting or node count exceeds the limit")
		case codeNumberLimit:
			return nil, proposalErr(ErrSchema, "", "number outside the supported range or precision")
		}
	}
	return nil, proposalErr(ErrMalformed, "", "payload is not one well-formed JSON document")
}

// decodeStrict performs steps 1 and 2 above and the typed decode. into is a pointer
// to a proposal struct; the generic tree is returned for sub-document validation.
func decodeStrict(raw []byte, into any) (map[string]any, *ProposalError) {
	tree, perr := strictTree(raw)
	if perr != nil {
		return nil, perr
	}
	root, ok := tree.(map[string]any)
	if !ok {
		return nil, proposalErr(ErrSchema, "", "payload must be a JSON object")
	}
	if perr = checkClosed(root, reflect.TypeOf(into).Elem(), "", true); perr != nil {
		return nil, perr
	}
	if perr = checkText(root, ""); perr != nil {
		return nil, perr
	}
	canonical, err := json.Marshal(root)
	if err != nil {
		return nil, proposalErr(ErrMalformed, "", "payload cannot be re-encoded")
	}
	dec := json.NewDecoder(bytes.NewReader(canonical))
	dec.DisallowUnknownFields()
	if err = dec.Decode(into); err != nil {
		var typeErr *json.UnmarshalTypeError
		if errors.As(err, &typeErr) {
			return nil, proposalErr(ErrSchema, safePath("/"+strings.ReplaceAll(typeErr.Field, ".", "/")), "wrong JSON type")
		}
		return nil, proposalErr(ErrSchema, "", "payload does not decode into the closed schema")
	}
	return root, nil
}

// safePath keeps a path to the characters a struct key or an index can contain, so
// an error never carries a model-chosen map key.
func safePath(p string) string {
	var b strings.Builder
	for _, r := range p {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '_', r == '/', r == '-':
			b.WriteRune(r)
		default:
			b.WriteByte('?')
		}
	}
	if s := b.String(); len(s) > 200 {
		return s[:200]
	}
	return b.String()
}

type fieldInfo struct {
	typ       reflect.Type
	omitEmpty bool
}

var fieldCache sync.Map // reflect.Type -> map[string]fieldInfo

func jsonFields(t reflect.Type) map[string]fieldInfo {
	if v, ok := fieldCache.Load(t); ok {
		return v.(map[string]fieldInfo)
	}
	out := map[string]fieldInfo{}
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if !f.IsExported() {
			continue
		}
		name, opts, _ := strings.Cut(f.Tag.Get("json"), ",")
		if name == "-" {
			continue
		}
		if name == "" {
			name = f.Name
		}
		out[name] = fieldInfo{f.Type, strings.Contains(opts, "omitempty")}
	}
	fieldCache.Store(t, out)
	return out
}

var (
	rawMessageType = reflect.TypeOf(json.RawMessage(nil))
	definitionType = reflect.TypeOf(metadata.Definition{})
	operationType  = reflect.TypeOf(PatchOperation{})
)

func sortedKeys(obj map[string]any) []string {
	keys := make([]string, 0, len(obj))
	for k := range obj {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// checkClosed walks the tree next to the Go type it will decode into. Key spelling is
// exact (encoding/json would fold case), unknown keys fail, null is refused for every
// non-pointer value and, when requireKeys is set, a key without omitempty must be
// present. Inside a metadata.Definition requireKeys is off: the metadata JSON schema,
// applied separately, owns required-ness there.
func checkClosed(node any, t reflect.Type, path string, requireKeys bool) *ProposalError {
	nullable := false
	for t.Kind() == reflect.Pointer {
		t, nullable = t.Elem(), true
	}
	if t == rawMessageType || t.Kind() == reflect.Interface {
		return nil
	}
	if node == nil {
		if nullable {
			return nil
		}
		return proposalErr(ErrSchema, safePath(path), "null is not allowed")
	}
	if t == definitionType {
		requireKeys = false
	}
	switch t.Kind() {
	case reflect.Struct:
		obj, ok := node.(map[string]any)
		if !ok {
			return proposalErr(ErrSchema, safePath(path), "expected an object")
		}
		fields := jsonFields(t)
		for _, k := range sortedKeys(obj) {
			info, known := fields[k]
			if !known {
				if serverField(k) {
					return proposalErr(ErrServerField, safePath(path), "the server derives this value; the model cannot set it")
				}
				return proposalErr(ErrUnknownField, safePath(path), "a key is not part of the closed schema")
			}
			if err := checkClosed(obj[k], info.typ, path+"/"+k, requireKeys); err != nil {
				return err
			}
		}
		if requireKeys {
			names := make([]string, 0, len(fields))
			for name := range fields {
				names = append(names, name)
			}
			sort.Strings(names)
			for _, name := range names {
				if _, present := obj[name]; !present && !fields[name].omitEmpty {
					// An add carries no expected hash: the key may be absent or empty.
					if name == "expected_hash" && t == operationType && obj["op"] == "add" {
						continue
					}
					return proposalErr(ErrSchema, safePath(path+"/"+name), "required field is missing")
				}
			}
		}
	case reflect.Slice, reflect.Array:
		arr, ok := node.([]any)
		if !ok {
			return proposalErr(ErrSchema, safePath(path), "expected an array")
		}
		for i, v := range arr {
			if err := checkClosed(v, t.Elem(), fmt.Sprintf("%s/%d", path, i), requireKeys); err != nil {
				return err
			}
		}
	case reflect.Map:
		obj, ok := node.(map[string]any)
		if !ok {
			return proposalErr(ErrSchema, safePath(path), "expected an object")
		}
		for _, k := range sortedKeys(obj) {
			if err := checkClosed(obj[k], t.Elem(), path+"/{key}", requireKeys); err != nil {
				return err
			}
		}
	}
	return nil
}

// checkText applies cleanText to every string value and every object key of the tree.
// Keys of closed structs are checked against the schema as well; keys of free-form
// maps (given values, activity inputs) have no other gate here.
func checkText(node any, path string) *ProposalError {
	switch v := node.(type) {
	case string:
		if !cleanText(v) {
			return proposalErr(ErrSchema, safePath(path), "text contains a control, bidirectional-override or invisible character")
		}
	case []any:
		for i, e := range v {
			if err := checkText(e, fmt.Sprintf("%s/%d", path, i)); err != nil {
				return err
			}
		}
	case map[string]any:
		for _, k := range sortedKeys(v) {
			if !cleanText(k) {
				return proposalErr(ErrSchema, safePath(path), "key contains a control, bidirectional-override or invisible character")
			}
			if err := checkText(v[k], path+"/"+k); err != nil {
				return err
			}
		}
	}
	return nil
}

// cleanText reports whether s may be shown to a human reviewer and stored: no C0/C1
// control character other than newline and tab, no bidirectional override or isolate,
// no line or paragraph separator, no zero-width space, word joiner, byte-order mark
// or Unicode tag character (the invisible-instruction smuggling channel).
func cleanText(s string) bool {
	for _, r := range s {
		switch {
		case r == '\n' || r == '\t':
		case r < 0x20 || r == 0x7f || r >= 0x80 && r <= 0x9f:
			return false
		case r >= 0x202a && r <= 0x202e || r >= 0x2066 && r <= 0x2069:
			return false
		case r == 0x2028 || r == 0x2029 || r == 0xfeff || r == 0x200b || r == 0x2060 || r == 0x180e:
			return false
		case r >= 0xe0000 && r <= 0xe007f:
			return false
		}
	}
	return true
}

func runes(s string) int { return utf8.RuneCountInString(s) }

// textOK reports 1..MaxTextRunes runes of clean text.
func textOK(s string) bool {
	n := runes(s)
	return n >= 1 && n <= MaxTextRunes && cleanText(s)
}

// -------------------------------------------------------------- metadata validation

var (
	validatorOnce sync.Once
	validatorInst *metadata.Validator
	validatorErr  error
)

// sharedValidator is the process-wide metadata validator (read-only after creation).
func sharedValidator() (*metadata.Validator, error) {
	validatorOnce.Do(func() { validatorInst, validatorErr = metadata.NewValidator() })
	return validatorInst, validatorErr
}

// validateDefinitionJSON checks one definition against the metadata JSON schema of the
// version it declares, by wrapping it in a one-definition document. Only the schema
// (and namespace agreement, trivially) is applied here; reference integrity belongs to
// the compiler and agreement with the real document to ApplyProposal. The validator
// reports a violation at the document root, so the finest location is the definition.
func validateDefinitionJSON(def json.RawMessage, version, namespace, path string) (*ProposalError, error) {
	v, err := sharedValidator()
	if err != nil {
		return nil, fmt.Errorf("ai: metadata validator unavailable: %w", err)
	}
	wrapped, err := json.Marshal(map[string]any{
		"schema_version": version, "app_id": "app.proposal", "namespace": namespace,
		"base_version": nil, "definitions": []json.RawMessage{def},
	})
	if err != nil {
		return proposalErr(ErrSchema, safePath(path), "definition cannot be encoded"), nil
	}
	if _, err = v.Parse(wrapped); err != nil {
		return proposalErr(ErrSchema, safePath(path), "definition violates the metadata schema"), nil
	}
	return nil, nil
}

// ------------------------------------------------------------------ patch proposal

// ParsePatchProposal strictly decodes a model payload into a PatchProposal. The result
// is validated and normalised (empty lists are non-nil, acceptance cases are
// canonical); every failure is a *ProposalError, except an unavailable metadata
// validator, which is a plain error.
func ParsePatchProposal(raw []byte) (PatchProposal, error) {
	var p PatchProposal
	tree, perr := decodeStrict(raw, &p)
	if perr != nil {
		return PatchProposal{}, perr
	}
	perr, err := validatePatchProposal(&p, tree)
	if err != nil {
		return PatchProposal{}, err
	}
	if perr != nil {
		return PatchProposal{}, perr
	}
	return p, nil
}

func validatePatchProposal(p *PatchProposal, tree map[string]any) (*ProposalError, error) {
	if p.Version != PatchProposalVersion {
		return proposalErr(ErrSchema, "/version", "unsupported proposal version"), nil
	}
	if len(p.Operations) > MaxOperations {
		return proposalErr(ErrTooLarge, "/operations", "more operations than the limit"), nil
	}
	if len(p.Operations) == 0 && len(p.UnresolvedRequirements) == 0 {
		return proposalErr(ErrSchema, "/operations", "a proposal needs an operation or an unresolved requirement"), nil
	}
	targets := make(map[string]bool, len(p.Operations))
	for i := range p.Operations {
		at := fmt.Sprintf("/operations/%d", i)
		if perr := validateOperation(&p.Operations[i], at); perr != nil {
			return perr, nil
		}
		if targets[p.Operations[i].Target] {
			return proposalErr(ErrSchema, at+"/target", "two operations name one target"), nil
		}
		targets[p.Operations[i].Target] = true
	}
	// Definition bodies after every cheaper check, so a policy violation anywhere
	// outranks a schema one.
	ops, _ := tree["operations"].([]any)
	for i := range p.Operations {
		if p.Operations[i].Definition == nil || i >= len(ops) {
			continue
		}
		op, _ := ops[i].(map[string]any)
		def, _ := op["definition"].(map[string]any)
		at := fmt.Sprintf("/operations/%d/definition", i)
		if def == nil {
			return proposalErr(ErrSchema, at, "definition must be an object"), nil
		}
		raw, err := json.Marshal(def)
		if err != nil {
			return proposalErr(ErrSchema, at, "definition cannot be encoded"), nil
		}
		version, _ := def["schema_version"].(string)
		namespace, _ := def["namespace"].(string)
		if perr, err := validateDefinitionJSON(raw, version, namespace, at); err != nil || perr != nil {
			return perr, err
		}
	}
	if perr := validateAssumptions(p.Assumptions, "/assumptions"); perr != nil {
		return perr, nil
	}
	var perr *ProposalError
	if p.UnresolvedRequirements, perr = validateUnresolved(p.UnresolvedRequirements, "/unresolved_requirements", nil); perr != nil {
		return perr, nil
	}
	if p.ProposedTests, perr = validateProposedTests(p.ProposedTests, "/proposed_tests"); perr != nil {
		return perr, nil
	}
	if p.RiskFlags, perr = validateRiskFlags(p.RiskFlags, "/risk_flags"); perr != nil {
		return perr, nil
	}
	return nil, nil
}

func validateOperation(op *PatchOperation, path string) *ProposalError {
	switch op.Op {
	case "add", "update", "delete":
	default:
		return proposalErr(ErrSchema, path+"/op", "operation must be add, update or delete")
	}
	if len(op.Target) > maxTargetBytes || !targetPattern.MatchString(op.Target) {
		return proposalErr(ErrSchema, path+"/target", "target is not a definition id")
	}
	kind := targetKind(op.Target)
	if !kindAllowed(kind) {
		return proposalErr(ErrForbiddenKind, path+"/target", "target kind is not on the allowlist")
	}
	switch {
	case op.Op == "add" && op.ExpectedHash != "":
		return proposalErr(ErrSchema, path+"/expected_hash", "an add carries no expected hash")
	case op.Op != "add" && !hashPattern.MatchString(op.ExpectedHash):
		return proposalErr(ErrSchema, path+"/expected_hash", "update and delete need the expected hash")
	}
	if op.Op == "delete" {
		if op.Definition != nil {
			return proposalErr(ErrSchema, path+"/definition", "a delete carries no definition")
		}
	} else {
		d := op.Definition
		switch {
		case d == nil:
			return proposalErr(ErrSchema, path+"/definition", "add and update need a definition")
		case d.Tool != nil || d.Kind == "tool":
			return proposalErr(ErrForbiddenKind, path+"/definition", "a tool definition cannot be proposed")
		case !kindAllowed(d.Kind):
			return proposalErr(ErrForbiddenKind, path+"/definition/kind", "definition kind is not on the allowlist")
		case d.ID != op.Target:
			return proposalErr(ErrSchema, path+"/definition/id", "definition id must equal the target")
		case d.Kind != kind:
			return proposalErr(ErrSchema, path+"/definition/kind", "definition kind must match the target")
		}
	}
	if !textOK(op.Rationale) {
		return proposalErr(ErrSchema, path+"/rationale", "rationale must be 1..2000 characters of clean text")
	}
	return nil
}

func validateAssumptions(list []string, path string) *ProposalError {
	if len(list) > MaxListItems {
		return proposalErr(ErrTooLarge, path, "more items than the limit")
	}
	for i, s := range list {
		if !textOK(s) {
			return proposalErr(ErrSchema, fmt.Sprintf("%s/%d", path, i), "text must be 1..2000 characters of clean text")
		}
	}
	return nil
}

// validateUnresolved checks open questions. seen carries the ids already used in the
// same proposal (requirement drafts) and is extended.
func validateUnresolved(list []UnresolvedRequirement, path string, seen map[string]bool) ([]UnresolvedRequirement, *ProposalError) {
	if len(list) > MaxListItems {
		return nil, proposalErr(ErrTooLarge, path, "more items than the limit")
	}
	if seen == nil {
		seen = map[string]bool{}
	}
	out := make([]UnresolvedRequirement, 0, len(list))
	for i, u := range list {
		at := fmt.Sprintf("%s/%d", path, i)
		switch {
		case !itemIDPattern.MatchString(u.ID):
			return nil, proposalErr(ErrSchema, at+"/id", "id must match the requirement id pattern")
		case seen[u.ID]:
			return nil, proposalErr(ErrSchema, at+"/id", "id is used twice")
		case !textOK(u.Question):
			return nil, proposalErr(ErrSchema, at+"/question", "question must be 1..2000 characters of clean text")
		case u.Category != "" && !categoryPattern.MatchString(u.Category):
			return nil, proposalErr(ErrSchema, at+"/category", "category must be a lower-case identifier")
		}
		seen[u.ID] = true
		out = append(out, u)
	}
	return out, nil
}

func validateRiskFlags(list []RiskFlag, path string) ([]RiskFlag, *ProposalError) {
	if len(list) > MaxListItems {
		return nil, proposalErr(ErrTooLarge, path, "more items than the limit")
	}
	out := make([]RiskFlag, 0, len(list))
	for i, f := range list {
		at := fmt.Sprintf("%s/%d", path, i)
		switch {
		case !riskCodePattern.MatchString(f.Code):
			return nil, proposalErr(ErrSchema, at+"/code", "code must be a short identifier")
		case runes(f.Detail) > MaxTextRunes || !cleanText(f.Detail):
			return nil, proposalErr(ErrSchema, at+"/detail", "detail must be at most 2000 characters of clean text")
		}
		out = append(out, f)
	}
	return out, nil
}

// zeroRequirementsHash binds the shape-only acceptance validation below to no set.
var zeroRequirementsHash = strings.Repeat("0", 64)

// validateProposedTests applies the intent package's own case-shape rules (ids, the
// expectation vocabulary, bounded scalar given-values) and returns the cases in their
// canonical form. A proposed test is advisory text with a typed shape: it is never an
// acceptance spec and names no requirement that has to exist.
func validateProposedTests(cases []intent.AcceptanceCase, path string) ([]intent.AcceptanceCase, *ProposalError) {
	if len(cases) > MaxListItems {
		return nil, proposalErr(ErrTooLarge, path, "more items than the limit")
	}
	if len(cases) == 0 {
		return []intent.AcceptanceCase{}, nil
	}
	spec := intent.AcceptanceSpec{SchemaVersion: intent.AcceptanceSpecVersion, RequirementsHash: zeroRequirementsHash, Cases: cases}
	canonical, err := spec.Canonical()
	if err != nil {
		return nil, proposalErr(ErrSchema, path, "a proposed test violates the acceptance case shape")
	}
	var back intent.AcceptanceSpec
	if json.Unmarshal(canonical, &back) != nil {
		return nil, proposalErr(ErrSchema, path, "a proposed test cannot be normalised")
	}
	return back.Cases, nil
}

// -------------------------------------------------------------- requirement proposal

// ParseRequirementProposal strictly decodes a model payload into a RequirementProposal.
// Every draft's semantics are validated by the typed decoders of the intent package for
// its category and returned in canonical form. A category the verifier does not
// understand is not a draft: it belongs in open_questions and never resolves.
func ParseRequirementProposal(raw []byte) (RequirementProposal, error) {
	var p RequirementProposal
	if _, perr := decodeStrict(raw, &p); perr != nil {
		return RequirementProposal{}, perr
	}
	if perr := validateRequirementProposal(&p); perr != nil {
		return RequirementProposal{}, perr
	}
	return p, nil
}

func validateRequirementProposal(p *RequirementProposal) *ProposalError {
	if p.Version != RequirementProposalVersion {
		return proposalErr(ErrSchema, "/version", "unsupported proposal version")
	}
	if len(p.Requirements) > MaxListItems {
		return proposalErr(ErrTooLarge, "/requirements", "more items than the limit")
	}
	if len(p.Requirements) == 0 && len(p.OpenQuestions) == 0 {
		return proposalErr(ErrSchema, "/requirements", "a proposal needs a requirement or an open question")
	}
	seen := map[string]bool{}
	drafts := make([]RequirementDraft, 0, len(p.Requirements))
	for i, d := range p.Requirements {
		at := fmt.Sprintf("/requirements/%d", i)
		switch {
		case !itemIDPattern.MatchString(d.ID):
			return proposalErr(ErrSchema, at+"/id", "id must match the requirement id pattern")
		case seen[d.ID]:
			return proposalErr(ErrSchema, at+"/id", "id is used twice")
		case !textOK(d.Statement):
			return proposalErr(ErrSchema, at+"/statement", "statement must be 1..2000 characters of clean text")
		case !categoryPattern.MatchString(d.Category):
			return proposalErr(ErrSchema, at+"/category", "category must be a lower-case identifier")
		}
		seen[d.ID] = true
		semantics, perr := canonicalSemantics(d, at+"/semantics")
		if perr != nil {
			return perr
		}
		d.Semantics = semantics
		drafts = append(drafts, d)
	}
	p.Requirements = drafts
	var perr *ProposalError
	if p.OpenQuestions, perr = validateUnresolved(p.OpenQuestions, "/open_questions", seen); perr != nil {
		return perr
	}
	if p.ProposedTests, perr = validateProposedTests(p.ProposedTests, "/proposed_tests"); perr != nil {
		return perr
	}
	return validateAssumptions(p.Assumptions, "/assumptions")
}

// canonicalSemantics runs the intent package's own validation of one draft by placing
// it in a one-requirement set with placeholder server fields (the model never sets
// requiredness, status or provenance), and returns the canonical semantics.
func canonicalSemantics(d RequirementDraft, path string) (json.RawMessage, *ProposalError) {
	if !isKnownCategory(d.Category) {
		return nil, proposalErr(ErrSemantics, path, "category is not one the verifier understands; ask it as an open question")
	}
	set := intent.RequirementSet{SchemaVersion: intent.RequirementSetVersion, AppID: "app.proposal", Requirements: []intent.RequirementV2{{
		ID: d.ID, Category: d.Category, Requiredness: intent.Optional, RequirednessBasis: intent.BasisOwnerDecision,
		Status: intent.StatusOpenQuestion, Provenance: intent.ProvenanceAIConfirmed, Semantics: d.Semantics,
	}}}
	canonical, err := set.Canonical()
	if err != nil {
		return nil, proposalErr(ErrSemantics, path, "semantics do not match the category")
	}
	var back intent.RequirementSet
	if json.Unmarshal(canonical, &back) != nil || len(back.Requirements) != 1 {
		return nil, proposalErr(ErrSemantics, path, "semantics cannot be normalised")
	}
	return back.Requirements[0].Semantics, nil
}

// isKnownCategory reports whether the verifier understands a requirement category.
func isKnownCategory(c string) bool { return intent.RequirementV2{Category: c}.Known() }
