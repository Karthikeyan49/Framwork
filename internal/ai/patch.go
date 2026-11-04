package ai

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/Karthikeyan49/Framwork/internal/compiler"
	"github.com/Karthikeyan49/Framwork/internal/metadata"
)

var (
	_ ApplyPatch      = ApplyProposal
	_ DeriveSensitive = DeriveSensitiveAspects
)

// applied is the internal result of applyProposal. Diagnostics carries the metadata
// validator's issue (path rewritten to the definition id) when the patched document
// failed validation, so the repair loop can feed it back like a compiler diagnostic.
type applied struct {
	doc         metadata.Document
	conflicts   []Conflict
	diagnostics []compiler.Diagnostic
}

// ApplyProposal applies a proposal to the CURRENT draft: every update and delete is a
// compare-and-set against metadata.DefinitionHash of the definition the draft holds
// now (no stored result is trusted), every target must be of an allowlisted kind (and
// so must any definition it replaces), and the patched document must satisfy the
// metadata schema. It is all or nothing: it returns the patched document with
// definitions sorted by id, or typed conflicts and no document. A structurally
// invalid proposal (unknown operation, malformed target, missing or surplus
// definition, more than MaxOperations) is an error, not a conflict. It never mutates
// its arguments and shares no memory with them.
func ApplyProposal(current metadata.Document, p PatchProposal) (metadata.Document, []Conflict, error) {
	r, err := applyProposal(current, p)
	if err != nil {
		return metadata.Document{}, nil, err
	}
	return r.doc, r.conflicts, nil
}

func cloneJSON[T any](v T) (T, error) {
	var out T
	raw, err := json.Marshal(v)
	if err != nil {
		return out, err
	}
	err = json.Unmarshal(raw, &out)
	return out, err
}

func applyProposal(current metadata.Document, p PatchProposal) (applied, error) {
	if p.Version != PatchProposalVersion {
		return applied{}, proposalErr(ErrSchema, "/version", "unsupported proposal version")
	}
	if len(p.Operations) > MaxOperations {
		return applied{}, proposalErr(ErrTooLarge, "/operations", "more operations than the limit")
	}
	base, err := cloneJSON(current)
	if err != nil {
		return applied{}, fmt.Errorf("ai: the current draft cannot be copied: %w", err)
	}
	byID := make(map[string]metadata.Definition, len(base.Definitions))
	for _, d := range base.Definitions {
		if _, repeated := byID[d.ID]; repeated {
			return applied{}, errors.New("ai: the current draft repeats a definition id")
		}
		byID[d.ID] = d
	}

	var conflicts []Conflict
	var diagnostics []compiler.Diagnostic
	conflict := func(target, category, reason string) {
		conflicts = append(conflicts, Conflict{Target: target, Category: category, Reason: reason})
	}
	// invalid records a definition that cannot be written; the diagnostic is what the
	// repair loop feeds back to the model (the validator's own wording and codes).
	invalid := func(target, code, message string) {
		conflict(target, ConflictInvalid, "invalid definition ("+code+"): "+message)
		diagnostics = append(diagnostics, compiler.Diagnostic{Path: "/" + target, Code: code, Message: message})
	}
	seen := make(map[string]bool, len(p.Operations))
	for i, op := range p.Operations {
		at := fmt.Sprintf("/operations/%d", i)
		switch {
		case op.Op != "add" && op.Op != "update" && op.Op != "delete":
			return applied{}, proposalErr(ErrSchema, at+"/op", "operation must be add, update or delete")
		case len(op.Target) > maxTargetBytes || !targetPattern.MatchString(op.Target):
			return applied{}, proposalErr(ErrSchema, at+"/target", "target is not a definition id")
		case op.Op == "delete" && op.Definition != nil:
			return applied{}, proposalErr(ErrSchema, at+"/definition", "a delete carries no definition")
		case op.Op != "delete" && op.Definition == nil:
			return applied{}, proposalErr(ErrSchema, at+"/definition", "add and update need a definition")
		}
		if seen[op.Target] {
			conflict(op.Target, ConflictDuplicateTarget, "two operations name this target")
			continue
		}
		seen[op.Target] = true
		if !kindAllowed(targetKind(op.Target)) {
			conflict(op.Target, ConflictForbiddenKind, "the target kind is not on the allowlist")
			continue
		}
		existing, found := byID[op.Target]
		if found && !kindAllowed(existing.Kind) {
			conflict(op.Target, ConflictForbiddenKind, "the existing definition is of a kind a proposal may not replace")
			continue
		}
		if d := op.Definition; d != nil {
			switch {
			case d.Tool != nil || !kindAllowed(d.Kind):
				conflict(op.Target, ConflictForbiddenKind, "the proposed definition is of a kind a proposal may not write")
				continue
			case d.ID != op.Target || d.Kind != targetKind(op.Target):
				conflict(op.Target, ConflictInvalid, "the definition id and kind must match the target")
				continue
			case found && existing.Kind != d.Kind:
				conflict(op.Target, ConflictInvalid, "the kind of a definition cannot change")
				continue
			case holdsRedaction(*d):
				conflict(op.Target, ConflictInvalid, "the definition contains a redaction placeholder; text the model was not shown cannot be written back")
				continue
			case d.SchemaVersion != base.SchemaVersion:
				invalid(op.Target, "schema_version_mismatch", "The definition schema version must equal the document's")
				continue
			case d.Namespace != base.Namespace:
				invalid(op.Target, "namespace_mismatch", "Definition namespace must match its envelope")
				continue
			}
			raw, err := json.Marshal(d)
			if err != nil {
				return applied{}, fmt.Errorf("ai: a proposed definition cannot be encoded: %w", err)
			}
			perr, err := validateDefinitionJSON(raw, d.SchemaVersion, d.Namespace, "/"+op.Target)
			if err != nil {
				return applied{}, err
			}
			if perr != nil {
				invalid(op.Target, "schema_violation", "Definition violates its versioned metadata schema")
				continue
			}
		}
		switch op.Op {
		case "add":
			switch {
			case op.ExpectedHash != "":
				conflict(op.Target, ConflictStaleHash, "an add carries no expected hash")
			case found:
				conflict(op.Target, ConflictCollision, "a definition with this id already exists in the draft")
			}
		default: // update, delete
			switch {
			case !found:
				conflict(op.Target, ConflictMissing, "the draft has no definition with this id")
			case op.ExpectedHash != metadata.DefinitionHash(existing):
				conflict(op.Target, ConflictStaleHash, "the definition changed since the model was shown it")
			}
		}
	}
	if len(conflicts) > 0 {
		sortConflicts(conflicts)
		sortDiagnostics(diagnostics)
		return applied{conflicts: conflicts, diagnostics: diagnostics}, nil
	}

	for _, op := range p.Operations {
		if op.Op == "delete" {
			delete(byID, op.Target)
			continue
		}
		d, err := cloneJSON(*op.Definition)
		if err != nil {
			return applied{}, fmt.Errorf("ai: a proposed definition cannot be copied: %w", err)
		}
		byID[op.Target] = d
	}
	patched := base
	patched.Definitions = make([]metadata.Definition, 0, len(byID))
	for _, d := range byID {
		patched.Definitions = append(patched.Definitions, d)
	}
	sort.Slice(patched.Definitions, func(i, j int) bool { return patched.Definitions[i].ID < patched.Definitions[j].ID })

	issue, err := validatePatched(patched)
	if err != nil {
		return applied{}, err
	}
	if issue != nil {
		target := issueTarget(issue.Path, patched.Definitions)
		return applied{
			conflicts:   []Conflict{{Target: target, Category: ConflictInvalid, Reason: "invalid definition (" + issue.Code + "): " + issue.Message}},
			diagnostics: []compiler.Diagnostic{{Path: rewriteIssuePath(issue.Path, target), Code: issue.Code, Message: issue.Message}},
		}, nil
	}
	return applied{doc: patched}, nil
}

func sortDiagnostics(d []compiler.Diagnostic) {
	sort.SliceStable(d, func(i, j int) bool {
		if d[i].Path != d[j].Path {
			return d[i].Path < d[j].Path
		}
		return d[i].Code < d[j].Code
	})
}

func sortConflicts(c []Conflict) {
	sort.SliceStable(c, func(i, j int) bool {
		if c[i].Target != c[j].Target {
			return c[i].Target < c[j].Target
		}
		return c[i].Category < c[j].Category
	})
}

// validatePatched runs the metadata validator over the patched document and returns
// its first issue. A failure that is not a validation issue is an error.
func validatePatched(doc metadata.Document) (*metadata.Issue, error) {
	v, err := sharedValidator()
	if err != nil {
		return nil, fmt.Errorf("ai: metadata validator unavailable: %w", err)
	}
	raw, err := json.Marshal(doc)
	if err != nil {
		return &metadata.Issue{Code: "invalid_json", Message: "The patched document cannot be encoded"}, nil
	}
	if _, err = v.Parse(raw); err == nil {
		return nil, nil
	}
	var invalid *metadata.Invalid
	if errors.As(err, &invalid) && len(invalid.Issues) > 0 {
		issue := invalid.Issues[0]
		return &issue, nil
	}
	if errors.Is(err, metadata.ErrInvalid) {
		return &metadata.Issue{Code: "invalid", Message: "The patched document is invalid"}, nil
	}
	return nil, fmt.Errorf("ai: metadata validation failed: %w", err)
}

// issueTarget maps "/definitions/N/..." to the id of the Nth definition.
func issueTarget(path string, defs []metadata.Definition) string {
	rest, ok := strings.CutPrefix(path, "/definitions/")
	if !ok {
		return ""
	}
	idx, _, _ := strings.Cut(rest, "/")
	n, err := strconv.Atoi(idx)
	if err != nil || n < 0 || n >= len(defs) {
		return ""
	}
	return defs[n].ID
}

// rewriteIssuePath turns "/definitions/N/rest" into "/<id>/rest", the shape the
// compiler's own diagnostics use.
func rewriteIssuePath(path, target string) string {
	if target == "" {
		return path
	}
	rest, _ := strings.CutPrefix(path, "/definitions/")
	_, tail, _ := strings.Cut(rest, "/")
	if tail == "" {
		return "/" + target
	}
	return "/" + target + "/" + tail
}

// ---------------------------------------------------------------- sensitive aspects

// Aspects that do not make a change sensitive: a label rewrite and a page layout can
// be reviewed on the page itself. Every other aspect of the diff-v1 vocabulary (and
// any aspect a later diff version adds) is sensitive: storage, rule logic and
// financial rules, workflow graph, trigger, approval gate, tool pin and declaration,
// relationship, plan-only changes and anything the diff could not classify.
var plainAspects = map[string]bool{compiler.AspectLabel: true, compiler.AspectPage: true}

// SensitiveAspect reports whether an aspect of compiler.ComputeDiff requires the
// caller to acknowledge it before a proposal is applied.
func SensitiveAspect(aspect string) bool { return !plainAspects[aspect] }

// DeriveSensitiveAspects diffs the program a proposal was applied to (current) with
// the program the patched document compiled to, and derives what the apply endpoint
// makes the caller acknowledge: the changed definitions with their server-derived
// kind and operation, the union of aspects, and the sorted "<target>:<aspect>" list of
// the sensitive ones. A zero current program means the draft did not compile; every
// definition then counts as added, so everything it brings is sensitive.
func DeriveSensitiveAspects(current, patched compiler.Program) (Derived, error) {
	if !patched.Valid() {
		return Derived{}, compiler.ErrDiff
	}
	diff, err := compiler.ComputeDiff(current, patched)
	if err != nil {
		return Derived{}, err
	}
	return derivedFromDiff(diff), nil
}

func derivedFromDiff(diff compiler.Diff) Derived {
	d := Derived{Targets: []DerivedTarget{}, Sensitive: []string{}, Aspects: []string{}}
	aspects := map[string]bool{}
	for _, c := range diff.Changes {
		op := "update"
		switch c.Change {
		case compiler.ChangeAdded:
			op = "add"
		case compiler.ChangeRemoved:
			op = "delete"
		}
		d.Targets = append(d.Targets, DerivedTarget{ID: c.ID, Kind: c.Kind, Op: op})
		for _, a := range c.Aspects {
			aspects[a] = true
			if SensitiveAspect(a) {
				d.Sensitive = append(d.Sensitive, c.ID+":"+a)
			}
		}
	}
	for a := range aspects {
		d.Aspects = append(d.Aspects, a)
	}
	sort.Strings(d.Aspects)
	sort.Strings(d.Sensitive)
	sort.Slice(d.Targets, func(i, j int) bool { return d.Targets[i].ID < d.Targets[j].ID })
	return d
}
