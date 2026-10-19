package ai

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/Karthikeyan49/Framwork/internal/compiler"
	"github.com/Karthikeyan49/Framwork/internal/intent"
	"github.com/Karthikeyan49/Framwork/internal/metadata"
)

// The semantic context bundle (ADR040 section 4 and amendment B). The bundle is a
// fixed sequence of named segments. Exactly one is TRUSTED: the instruction text of
// the template set, which is a constant. Every byte derived from a person, a tool,
// a document or the draft is carried in an UNTRUSTED segment that the builder
// encodes as one compact JSON value inside a wrapper pair. Compact JSON as produced
// by encoding/json escapes <, > and & and has no raw newline, so no author text can
// contain the closing wrapper or start a new segment line: the delimiter cannot be
// forged by construction, and a test pins that property.

// Segment names of the bundle, in order. The set and the order never depend on the
// input.
const (
	SegmentInstructions = "instructions"
	SegmentRequest      = "request"
	SegmentApplication  = "application"
	SegmentReleaseDiff  = "release_diff"
	SegmentTools        = "tools"
	SegmentRoles        = "roles"
	SegmentRequirements = "requirements"
	// SegmentRepair and SegmentDiagnostics are appended by the repair loop.
	SegmentRepair      = "repair"
	SegmentDiagnostics = "diagnostics"
)

const (
	wrapperBegin = "<untrusted>"
	wrapperEnd   = "</untrusted>"

	maxContextTools        = 200
	maxContextRoles        = 64
	maxContextRequirements = 200
	maxRoleRunes           = 64
	maxLabelRunes          = 200
	maxRowTextRunes        = 160
)

// wrapUntrusted encodes v as compact JSON between the wrapper markers. Every
// author-controlled string in v must already have passed redactString or redactRaw;
// the encoder itself escapes <, > and & and emits no raw newline.
func wrapUntrusted(v any) (string, error) {
	body, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	return wrapperBegin + "\n" + string(body) + "\n" + wrapperEnd, nil
}

// clip shortens author text to n runes after sanitizing it; the clip is visible to a
// reader because the row also carries the definition hash of the full text.
func clip(s string, n int) string {
	if len(s) > 4*n { // a rune is at most four bytes: never sanitize megabytes to keep a few runes
		s = s[:4*n]
	}
	s = sanitizeText(s)
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	r := []rune(s)
	return string(r[:n])
}

type definitionRow struct {
	ID         string          `json:"id"`
	Kind       string          `json:"kind"`
	Label      string          `json:"label"`
	Hash       string          `json:"hash,omitempty"` // server-computed: never redacted
	Editable   *bool           `json:"editable,omitempty"`
	Definition json.RawMessage `json:"definition,omitempty"` // redacted
}

type applicationSegment struct {
	SchemaVersion string          `json:"schema_version"`
	Namespace     string          `json:"namespace"`
	AppID         string          `json:"app_id"`
	Revision      int64           `json:"revision"`
	DraftHash     string          `json:"draft_hash"`
	Definitions   []definitionRow `json:"definitions"`
}

type diffRow struct {
	ID      string   `json:"id"`
	Kind    string   `json:"kind"`
	Change  string   `json:"change"`
	Aspects []string `json:"aspects"`
}

type diffSegment struct {
	Against string    `json:"against"` // active_release | none | unavailable
	Changes []diffRow `json:"changes"`
}

type listSegment[T any] struct {
	Items   []T `json:"items"`
	Omitted int `json:"omitted"`
}

type requirementRow struct {
	ID       string `json:"id"`
	Category string `json:"category"`
	Status   string `json:"status"`
}

// validateRequest checks and normalises the human's request text. Dirty runes are
// dropped (the API refuses them earlier; this is the second line), an empty or
// over-long request is refused, never truncated.
func validateRequest(text string) (string, error) {
	if len(text) > 4*MaxRequestRunes {
		return "", fmt.Errorf("%w: request is longer than %d characters", ErrInvalidInput, MaxRequestRunes)
	}
	if utf8.RuneCountInString(text) > MaxRequestRunes {
		return "", fmt.Errorf("%w: request is longer than %d characters", ErrInvalidInput, MaxRequestRunes)
	}
	clean := sanitizeText(text)
	if strings.TrimSpace(clean) == "" {
		return "", fmt.Errorf("%w: request is empty", ErrInvalidInput)
	}
	return clean, nil
}

func (g *gateway) templates() TemplateSet { return g.opts.Templates.complete() }

// BuildContext assembles the canonical bundle for one request. It reads nothing but
// its input: metadata of the draft, the diff against the active release, the tool
// catalog summary, role names and requirement ids. A bundle over MaxContextBytes is
// ErrContextTooLarge; nothing is truncated to make it fit.
func (g *gateway) BuildContext(in ContextInput) (Context, error) {
	if in.Task != TaskApplicationPatch && in.Task != TaskRequirementCapture {
		return Context{}, fmt.Errorf("%w: unknown task", ErrInvalidInput)
	}
	request, err := validateRequest(in.Request)
	if err != nil {
		return Context{}, err
	}
	if in.DraftHash != "" && !apiHex(in.DraftHash) {
		return Context{}, fmt.Errorf("%w: draft hash is not a digest", ErrInvalidInput)
	}
	if in.DraftRevision < 0 {
		return Context{}, fmt.Errorf("%w: negative draft revision", ErrInvalidInput)
	}
	tpl := g.templates()
	instructions := tpl.Patch
	if in.Task == TaskRequirementCapture {
		instructions = tpl.Requirement
	}

	patch := in.Task == TaskApplicationPatch
	rows := make([]definitionRow, 0, len(in.Draft.Definitions))
	for _, d := range in.Draft.Definitions {
		row := definitionRow{ID: redactString(clip(d.ID, maxRowTextRunes)), Kind: redactString(clip(d.Kind, 32)), Label: redactString(clip(d.Label, maxLabelRunes))}
		if patch {
			row.Hash = metadata.DefinitionHash(d)
			editable := kindAllowed(d.Kind)
			row.Editable = &editable
			if editable {
				body, rerr := redactRaw(d)
				if rerr != nil {
					return Context{}, fmt.Errorf("ai: a definition cannot be encoded: %w", rerr)
				}
				row.Definition = body
			}
		}
		rows = append(rows, row)
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].ID < rows[j].ID })
	application := applicationSegment{SchemaVersion: redactString(clip(in.Draft.SchemaVersion, 32)), Namespace: redactString(clip(in.Draft.Namespace, maxRowTextRunes)),
		AppID: redactString(clip(in.Draft.AppID, maxRowTextRunes)), Revision: in.DraftRevision, DraftHash: in.DraftHash, Definitions: rows}

	diff := diffSegment{Against: "none", Changes: []diffRow{}}
	if in.BaseProgram != nil && in.BaseProgram.Valid() {
		diff.Against = "unavailable"
		if in.DraftProgram != nil && in.DraftProgram.Valid() {
			if d, derr := compiler.ComputeDiff(*in.BaseProgram, *in.DraftProgram); derr == nil {
				diff.Against = "active_release"
				for _, c := range d.Changes {
					aspects := append([]string{}, c.Aspects...)
					for i := range aspects {
						aspects[i] = redactString(clip(aspects[i], 64))
					}
					diff.Changes = append(diff.Changes, diffRow{ID: redactString(clip(c.ID, maxRowTextRunes)), Kind: redactString(clip(c.Kind, 32)), Change: redactString(clip(c.Change, 32)), Aspects: aspects})
				}
			}
		}
	}
	if len(diff.Changes) > maxContextTools*2 {
		diff.Changes = diff.Changes[:maxContextTools*2]
	}

	tools := make([]ToolSummary, 0, len(in.Tools))
	for _, t := range in.Tools {
		tools = append(tools, cleanTool(t))
	}
	sort.Slice(tools, func(i, j int) bool {
		if tools[i].Ref != tools[j].Ref {
			return tools[i].Ref < tools[j].Ref
		}
		return tools[i].Version < tools[j].Version
	})
	toolList := listSegment[ToolSummary]{Items: tools, Omitted: 0}
	if len(tools) > maxContextTools {
		toolList.Items, toolList.Omitted = tools[:maxContextTools], len(tools)-maxContextTools
	}

	roleSet := map[string]bool{}
	for _, r := range in.RoleNames {
		if r = redactString(clip(r, maxRoleRunes)); r != "" {
			roleSet[r] = true
		}
	}
	roles := make([]string, 0, len(roleSet))
	for r := range roleSet {
		roles = append(roles, r)
	}
	sort.Strings(roles)
	roleList := listSegment[string]{Items: roles}
	if len(roles) > maxContextRoles {
		roleList.Items, roleList.Omitted = roles[:maxContextRoles], len(roles)-maxContextRoles
	}

	reqs := []requirementRow{}
	if in.Requirements != nil {
		for _, r := range in.Requirements.Requirements {
			if patch && r.Status != intent.StatusOpenQuestion {
				continue
			}
			reqs = append(reqs, requirementRow{ID: redactString(clip(r.ID, maxRowTextRunes)), Category: redactString(clip(r.Category, maxRowTextRunes)), Status: redactString(clip(r.Status, maxRowTextRunes))})
		}
	}
	sort.Slice(reqs, func(i, j int) bool { return reqs[i].ID < reqs[j].ID })
	reqList := listSegment[requirementRow]{Items: reqs}
	if len(reqs) > maxContextRequirements {
		reqList.Items, reqList.Omitted = reqs[:maxContextRequirements], len(reqs)-maxContextRequirements
	}

	segments := []Segment{{Name: SegmentInstructions, Trust: TrustTrusted, Text: instructions}}
	for _, s := range []struct {
		name  string
		value any
	}{
		{SegmentRequest, struct {
			Request string `json:"request"`
		}{redactString(request)}},
		{SegmentApplication, application},
		{SegmentReleaseDiff, diff},
		{SegmentTools, toolList},
		{SegmentRoles, roleList},
		{SegmentRequirements, reqList},
	} {
		text, werr := wrapUntrusted(s.value)
		if werr != nil {
			return Context{}, fmt.Errorf("ai: context segment %s cannot be encoded: %w", s.name, werr)
		}
		segments = append(segments, Segment{Name: s.name, Trust: TrustUntrusted, Text: text})
	}
	total := 0
	for _, s := range segments {
		total += len(s.Text)
	}
	if total > MaxContextBytes {
		return Context{}, ErrContextTooLarge
	}
	c := Context{Version: ContextVersion, Segments: segments, Bytes: total}
	c.Hash = contextHash(c)
	return c, nil
}

// cleanTool bounds and sanitizes a tool summary supplied by the platform. The
// summary type has no field for an endpoint, host or credential; this keeps what it
// does carry short and clean.
func cleanTool(t ToolSummary) ToolSummary {
	names := func(in []string) []string {
		out := make([]string, 0, len(in))
		for i, n := range in {
			if i >= 64 {
				break
			}
			out = append(out, redactString(clip(n, maxRowTextRunes)))
		}
		return out
	}
	return ToolSummary{Ref: redactString(clip(t.Ref, maxRowTextRunes)), Version: redactString(clip(t.Version, 32)), SideEffect: redactString(clip(t.SideEffect, 64)),
		Approval: redactString(clip(t.Approval, 64)), Input: names(t.Input), Output: names(t.Output)}
}

// contextHash is the SHA-256 of the canonical encoding of the version and the
// segments; it is what the trace records as ContextHash.
func contextHash(c Context) string {
	raw, _ := json.Marshal(struct {
		Version  string    `json:"version"`
		Segments []Segment `json:"segments"`
	}{c.Version, c.Segments})
	return sha256Hex(raw)
}

// checkContext refuses a context the gateway did not build: a wrong version, a
// changed byte, a TRUSTED segment that is not the template's instruction text, an
// UNTRUSTED segment that is not wrapped, or one over the cap.
func (g *gateway) checkContext(c Context, task Task) error {
	bad := func(why string) error { return fmt.Errorf("%w: context %s", ErrInvalidInput, why) }
	if c.Version != ContextVersion || len(c.Segments) == 0 {
		return bad("version")
	}
	total := 0
	tpl := g.templates()
	for i, s := range c.Segments {
		total += len(s.Text)
		switch s.Trust {
		case TrustTrusted:
			want := tpl.Patch
			if task == TaskRequirementCapture {
				want = tpl.Requirement
			}
			if i != 0 || s.Name != SegmentInstructions || s.Text != want {
				return bad("has a trusted segment that is not the instruction text")
			}
		case TrustUntrusted:
			if !strings.HasPrefix(s.Text, wrapperBegin+"\n") || !strings.HasSuffix(s.Text, "\n"+wrapperEnd) ||
				strings.Count(s.Text, wrapperEnd) != 1 || strings.Count(s.Text, wrapperBegin) != 1 {
				return bad("has an unwrapped untrusted segment")
			}
		default:
			return bad("has an unlabelled segment")
		}
	}
	if total > MaxContextBytes {
		return ErrContextTooLarge
	}
	if c.Hash == "" || c.Hash != contextHash(c) {
		return bad("does not match its hash")
	}
	return nil
}

var errNoProvider = fmt.Errorf("%w: gateway has no provider", ErrNotConfigured)
