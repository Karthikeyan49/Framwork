// Package corpus loads and interprets the adversarial corpus of milestone M14 (data
// under internal/ai/testdata/corpus, format m14-corpus-v1) for tests. It is test
// support: it imports neither internal/ai nor any provider, builds no prompt and calls
// no model. Items are data; this package expands their placeholder language into
// concrete bytes, applies their planted hostile text to a fixture draft and checks
// the observable result of a run against the property each item declares. Nothing it
// produces is evidence about the behaviour of a real model.
package corpus

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/Karthikeyan49/Framwork/internal/compiler"
	"github.com/Karthikeyan49/Framwork/internal/intent"
	"github.com/Karthikeyan49/Framwork/internal/metadata"
	"github.com/Karthikeyan49/Framwork/internal/tools"
)

// Version is the corpus format this package understands.
const Version = "m14-corpus-v1"

// Files are the corpus files, one per attack class.
var Files = []string{"direct_injection", "indirect_injection", "exfiltration", "hostile_output", "resource", "golden"}

// Usage is what a scripted step reports.
type Usage struct {
	InputTokens  int64 `json:"input_tokens"`
	OutputTokens int64 `json:"output_tokens"`
}

// Payload is the model answer of a step in one of four forms.
type Payload struct {
	JSON   json.RawMessage `json:"json"`
	Raw    *string         `json:"raw"`
	Base64 *string         `json:"base64"`
	PadTo  int             `json:"pad_to"`
}

// Step is one scripted provider answer.
type Step struct {
	Payload *Payload `json:"payload"`
	Usage   Usage    `json:"usage"`
	Model   string   `json:"model"`
	DelayMS int64    `json:"delay_ms"`
	Error   string   `json:"error"` // refused unavailable timeout malformed other panic
}

// Plant puts hostile text into a definition of the draft.
type Plant struct {
	On    string          `json:"on"`
	ID    string          `json:"id"`
	Path  string          `json:"path"`
	Value json.RawMessage `json:"value"`
}

// Inject makes the compile function answer with scripted diagnostics or an error on
// the Nth compile of a PATCHED document.
type Inject struct {
	Attempt     int          `json:"attempt"`
	Diagnostics []DiagInject `json:"diagnostics"`
	Error       string       `json:"error"`
}

// DiagInject is one injected diagnostic, optionally repeated.
type DiagInject struct {
	Path    string `json:"path"`
	Code    string `json:"code"`
	Message string `json:"message"`
	Repeat  int    `json:"repeat"`
}

// Tool is a tool summary a hostile catalog could produce.
type Tool struct {
	Ref        string   `json:"ref"`
	Version    string   `json:"version"`
	SideEffect string   `json:"side_effect"`
	Approval   string   `json:"approval"`
	Input      []string `json:"input"`
	Output     []string `json:"output"`
}

// BudgetSpec overrides parts of the run budget.
type BudgetSpec struct {
	WallClockMS     int64  `json:"wall_clock_ms"`
	MaxAttempts     int    `json:"max_attempts"`
	TokensRemaining *int64 `json:"tokens_remaining"`
	ZeroValue       bool   `json:"zero_value"`
}

// UsageExpect bounds the charged usage.
type UsageExpect struct {
	MinTotalTokens int64 `json:"min_total_tokens"`
	NonNegative    bool  `json:"non_negative"`
}

// DiagExpect constrains the feedback segment of the second request.
type DiagExpect struct {
	MaxItems                 int      `json:"max_items"`
	MaxSegmentBytes          int      `json:"max_segment_bytes"`
	CannotCloseWrapper       bool     `json:"cannot_close_wrapper"`
	MarkersUntrustedOrAbsent []string `json:"markers_untrusted_or_absent"`
	NoRaw                    []string `json:"no_raw"`
	Residual                 string   `json:"residual"`
}

// ContextExpect constrains the built context bundle.
type ContextExpect struct {
	RequestUntrusted   bool     `json:"request_untrusted"`
	StructureStable    bool     `json:"structure_stable"`
	CannotCloseWrapper bool     `json:"cannot_close_wrapper"`
	MarkersUntrusted   []string `json:"markers_untrusted"`
	TrustedUnchanged   bool     `json:"trusted_unchanged"`
	Absent             []string `json:"absent"`
	NoRaw              []string `json:"no_raw"`
	Error              string   `json:"error"` // reject | reject_or_route | reject_or_invalid_draft
	Residual           string   `json:"residual"`
}

// Expect is the property an item declares.
type Expect struct {
	Status          []string            `json:"status"`
	Layer           string              `json:"layer"`
	LayerAny        []string            `json:"layer_any"`
	SensitiveHas    []string            `json:"sensitive_contains"`
	SensitiveEmpty  bool                `json:"sensitive_empty"`
	OracleDiff      map[string][]string `json:"oracle_diff"`
	MaxCalls        *int                `json:"max_provider_calls"`
	Attempts        []string            `json:"attempts"`
	AttemptsAny     []string            `json:"attempts_any"`
	Context         *ContextExpect      `json:"context"`
	CanonicalAbsent []string            `json:"canonical_absent"`
	Severity        string              `json:"severity_if_failed"`
	Conflicts       []string            `json:"conflicts"`
	Diagnostics     *DiagExpect         `json:"diagnostics"`
	ElapsedMSMax    int64               `json:"elapsed_ms_max"`
	Usage           *UsageExpect        `json:"usage"`
	ContextError    string              `json:"context_error"`
	Ambiguous       string              `json:"ambiguous"`
}

// Item is one corpus entry.
type Item struct {
	ID           string          `json:"id"`
	Class        string          `json:"class"`
	Subclass     string          `json:"subclass"`
	ADR          []string        `json:"adr"`
	Description  string          `json:"description"`
	Task         string          `json:"task"`
	Fixture      string          `json:"fixture"`
	Request      string          `json:"request"`
	Plant        []Plant         `json:"plant"`
	Script       []Step          `json:"script"`
	Expect       Expect          `json:"expect"`
	Tags         []string        `json:"tags"`
	Catalog      string          `json:"catalog"`
	ToolsExtra   []Tool          `json:"tools_extra"`
	RolesExtra   []string        `json:"roles_extra"`
	ExistingReqs json.RawMessage `json:"existing_requirements"`
	DraftInvalid bool            `json:"draft_invalid"`
	CompileInj   []Inject        `json:"compile_inject"`
	CompileErr   []Inject        `json:"compile_error"`
	Budget       *BudgetSpec     `json:"budget"`
}

// Tagged reports whether the item carries a tag.
func (it Item) Tagged(tag string) bool {
	for _, t := range it.Tags {
		if t == tag {
			return true
		}
	}
	return false
}

type fileShape struct {
	Version string `json:"corpus_version"`
	Class   string `json:"class"`
	Items   []Item `json:"items"`
}

// Library is a loaded corpus with its fixtures.
type Library struct {
	Items    []Item
	Fixtures map[string]metadata.Document
}

// Load reads the corpus below dir (testdata/corpus). Every unknown key is an error, so
// a corpus feature this package does not interpret can never be silently ignored.
func Load(dir string) (*Library, error) {
	lib := &Library{Fixtures: map[string]metadata.Document{}}
	seen := map[string]bool{}
	for _, name := range Files {
		raw, err := os.ReadFile(filepath.Join(dir, name+".json"))
		if err != nil {
			return nil, err
		}
		dec := json.NewDecoder(bytes.NewReader(raw))
		dec.DisallowUnknownFields()
		var f fileShape
		if err = dec.Decode(&f); err != nil {
			return nil, fmt.Errorf("%s: %w", name, err)
		}
		if f.Version != Version || f.Class != name {
			return nil, fmt.Errorf("%s: version %q class %q", name, f.Version, f.Class)
		}
		for _, it := range f.Items {
			if it.ID == "" || seen[it.ID] || it.Class != name {
				return nil, fmt.Errorf("%s: item %q is empty, repeated or misfiled", name, it.ID)
			}
			seen[it.ID] = true
			lib.Items = append(lib.Items, it)
		}
	}
	for _, name := range []string{"orders", "minimal", "vendors"} {
		raw, err := os.ReadFile(filepath.Join(dir, "fixtures", name+".json"))
		if err != nil {
			return nil, err
		}
		var doc metadata.Document
		if err = json.Unmarshal(raw, &doc); err != nil {
			return nil, fmt.Errorf("fixture %s: %w", name, err)
		}
		lib.Fixtures[name] = doc
	}
	lib.Fixtures["gen:wide"] = wide(lib.Fixtures["orders"])
	return lib, nil
}

// wide is the orders fixture plus enough long-labelled entities that the context
// bundle cannot fit MaxContextBytes.
func wide(base metadata.Document) metadata.Document {
	doc := base
	doc.Definitions = append([]metadata.Definition(nil), base.Definitions...)
	for i := 0; i < 200; i++ {
		id := fmt.Sprintf("entity.bulk%03d", i)
		field := fmt.Sprintf("field.bulk%03d.id", i)
		doc.Definitions = append(doc.Definitions,
			metadata.Definition{ID: id, SchemaVersion: doc.SchemaVersion, Namespace: doc.Namespace, Label: strings.Repeat("Bulk entity ", 15), Kind: "entity", Entity: &metadata.Entity{Fields: []string{field}}},
			metadata.Definition{ID: field, SchemaVersion: doc.SchemaVersion, Namespace: doc.Namespace, Label: strings.Repeat("Bulk field ", 15), Kind: "field", Field: &metadata.Field{EntityRef: id, Type: "string"}})
	}
	return doc
}

// ByID returns the item with the id.
func (l *Library) ByID(id string) (Item, bool) {
	for _, it := range l.Items {
		if it.ID == id {
			return it, true
		}
	}
	return Item{}, false
}

// ---------------------------------------------------------------- expansion

// Seg is one segment of a context or request, as the checker sees it.
type Seg struct{ Name, Trust, Text string }

// Wrapper names the delimiters of untrusted segments.
type Wrapper struct{ Begin, End string }

// Case is an item expanded against its fixture: concrete bytes only.
type Case struct {
	Item      Item
	Draft     metadata.Document // fixture with the planted text applied
	Clean     metadata.Document // the fixture as shipped
	Request   string
	Payloads  []ExpandedStep
	Tools     []Tool
	Roles     []string
	Reqs      *intent.RequirementSet
	Catalog   compiler.Catalog
	canaries  map[string]string
	DraftHash string
}

// ExpandedStep is a step with its payload bytes.
type ExpandedStep struct {
	Payload []byte
	Step    Step
}

// DefinitionHash is the CAS hash the context shows for a definition.
func DefinitionHash(d metadata.Definition) string { return metadata.DefinitionHash(d) }

// Expand interprets the placeholder language of one item.
func (l *Library) Expand(it Item) (Case, error) {
	fixture, ok := l.Fixtures[it.Fixture]
	if !ok {
		return Case{}, fmt.Errorf("%s: unknown fixture %q", it.ID, it.Fixture)
	}
	c := Case{Item: it, Clean: cloneDoc(fixture), canaries: canaries()}
	c.Draft = cloneDoc(fixture)
	e := &expander{lib: l, c: &c, fixture: c.Draft}
	for _, p := range it.Plant {
		if p.On != "draft" {
			return Case{}, fmt.Errorf("%s: unsupported plant target %q", it.ID, p.On)
		}
		var value any
		if err := json.Unmarshal(p.Value, &value); err != nil {
			return Case{}, err
		}
		value, err := e.tree(value)
		if err != nil {
			return Case{}, err
		}
		found := false
		for i := range c.Draft.Definitions {
			if c.Draft.Definitions[i].ID != p.ID {
				continue
			}
			var tree any
			raw, _ := json.Marshal(c.Draft.Definitions[i])
			if err = json.Unmarshal(raw, &tree); err != nil {
				return Case{}, err
			}
			if tree, err = applyOp(tree, patchOp{Op: "replace", Path: p.Path, Value: value}); err != nil {
				return Case{}, fmt.Errorf("%s: plant %s: %w", it.ID, p.ID, err)
			}
			out, _ := json.Marshal(tree)
			var d metadata.Definition
			if err = json.Unmarshal(out, &d); err != nil {
				return Case{}, fmt.Errorf("%s: plant %s: %w", it.ID, p.ID, err)
			}
			c.Draft.Definitions[i] = d
			found = true
		}
		if !found {
			return Case{}, fmt.Errorf("%s: plant target %s not in the fixture", it.ID, p.ID)
		}
	}
	e.fixture = c.Draft
	raw, _ := json.Marshal(c.Draft)
	c.DraftHash = hashHex(raw)
	var err error
	if c.Request, err = e.str(it.Request); err != nil {
		return Case{}, err
	}
	for _, s := range it.Script {
		step := s
		var payload []byte
		if s.Payload != nil {
			if payload, err = e.payload(*s.Payload); err != nil {
				return Case{}, fmt.Errorf("%s: %w", it.ID, err)
			}
		}
		c.Payloads = append(c.Payloads, ExpandedStep{Payload: payload, Step: step})
	}
	for _, t := range it.ToolsExtra {
		raw, _ := json.Marshal(t)
		var tree any
		_ = json.Unmarshal(raw, &tree)
		if tree, err = e.tree(tree); err != nil {
			return Case{}, err
		}
		raw, _ = json.Marshal(tree)
		var out Tool
		_ = json.Unmarshal(raw, &out)
		c.Tools = append(c.Tools, out)
	}
	for _, r := range it.RolesExtra {
		s, err := e.str(r)
		if err != nil {
			return Case{}, err
		}
		c.Roles = append(c.Roles, s)
	}
	if len(it.ExistingReqs) > 0 {
		var tree any
		if err = json.Unmarshal(it.ExistingReqs, &tree); err != nil {
			return Case{}, err
		}
		if tree, err = e.tree(tree); err != nil {
			return Case{}, err
		}
		raw, _ = json.Marshal(tree)
		var set intent.RequirementSet
		if err = json.Unmarshal(raw, &set); err != nil {
			return Case{}, fmt.Errorf("%s: existing requirements: %w", it.ID, err)
		}
		c.Reqs = &set
	}
	c.Catalog = compiler.Catalog{Version: "corpus-v1", Permissions: []string{"data.read", "data.write"}, Tools: []string{}}
	if it.Catalog == "vendor_tools" {
		vc, err := VendorCatalog()
		if err != nil {
			return Case{}, err
		}
		c.Catalog = vc
	}
	return c, nil
}

func cloneDoc(d metadata.Document) metadata.Document {
	raw, _ := json.Marshal(d)
	var out metadata.Document
	_ = json.Unmarshal(raw, &out)
	return out
}

// Expanded reports the concrete form of a string of the item (markers and canaries of
// its expectations) so a checker can look for it in a context.
func (c Case) Expanded(s string) string {
	e := &expander{c: &c, fixture: c.Draft}
	out, err := e.str(s)
	if err != nil {
		return s
	}
	return out
}

// Canary returns the secret-shaped value a "canary:NAME" placeholder expands to.
func (c Case) Canary(name string) string { return c.canaries[name] }

func canaries() map[string]string {
	sk := "sk-live-0123456789abcdefABCDEF0123456789"
	return map[string]string{
		"api_key_sk": sk, "api_key_sk_head": sk[:20], "api_key_sk_tail": sk[20:],
		"aws_access_key": "AKIAIOSFODNN7EXAMPLE", "aws_secret": "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
		"b64_run":       "dGhpcyBpcyBhIHZlcnkgc2VjcmV0IHZhbHVlIHRoYXQgc2hvdWxkIG5vdCBsZWF2ZQ9xK2Vz",
		"bearer_header": "Authorization: Bearer abcdefghijklmnopqrstuvwxyz0123456789",
		"card":          "4111 1111 1111 1111", "dsn_postgres": "postgres://app:s3cr3tPassw0rd@db.internal:5432/prod",
		"github_token": "ghp_0123456789abcdefghijklmnopqrstuvwxyz", "hex64": "9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08",
		"jwt":             "eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.dBjftJeZ4CVPmB92K27uhbUJU1p1r_wW1gFWFOEjXk0",
		"pem_private_key": "-----BEGIN PRIVATE KEY-----\nMIIEvQIBADANBgkqhkiG9w0BAQEFAASCBKcwggSjAgEAAoIBAQC7\n-----END PRIVATE KEY-----",
		"record_value":    "RECORD-VALUE-4417-Acme-balance-93210.55", "slack_token": "xoxb-123456789012-abcdefghijklmnop",
		"tenant_uuid": "3d7ef8e0-ee36-4f99-8a09-5dc180f1106d", "url_userinfo": "https://user:hunter2hunter2@example.com/path", "user_email": "jane.doe@example.com",
	}
}

type expander struct {
	lib     *Library
	c       *Case
	fixture metadata.Document
	index   int
	hasIdx  bool
}

func hashHex(b []byte) string { return sha256Hex(b) }

// payload expands one step payload to bytes.
func (e *expander) payload(p Payload) ([]byte, error) {
	switch {
	case p.Raw != nil:
		return e.padded(*p.Raw, p.PadTo)
	case p.Base64 != nil:
		dec, err := base64.StdEncoding.DecodeString(*p.Base64)
		if err != nil {
			return nil, err
		}
		return e.padded(string(dec), p.PadTo)
	case len(p.JSON) > 0:
		var tree any
		dec := json.NewDecoder(bytes.NewReader(p.JSON))
		dec.UseNumber()
		if err := dec.Decode(&tree); err != nil {
			return nil, err
		}
		tree, err := e.tree(tree)
		if err != nil {
			return nil, err
		}
		return json.Marshal(tree)
	}
	return nil, nil
}

func (e *expander) padded(s string, padTo int) ([]byte, error) {
	if padTo == 0 {
		out, err := e.str(s)
		return []byte(out), err
	}
	withMark := strings.Replace(s, "{{pad}}", "\x00PAD\x00", 1)
	out, err := e.str(withMark)
	if err != nil {
		return nil, err
	}
	fill := padTo - (len(out) - len("\x00PAD\x00"))
	if fill < 0 {
		fill = 0
	}
	return []byte(strings.Replace(out, "\x00PAD\x00", strings.Repeat("a", fill), 1)), nil
}

// tree expands placeholders, $gen and $def inside a decoded JSON value.
func (e *expander) tree(v any) (any, error) {
	switch t := v.(type) {
	case string:
		return e.str(t)
	case []any:
		out := make([]any, 0, len(t))
		for _, el := range t {
			if m, ok := el.(map[string]any); ok {
				if _, gen := m["$gen"]; gen {
					items, err := e.generate(m)
					if err != nil {
						return nil, err
					}
					out = append(out, items...)
					continue
				}
			}
			x, err := e.tree(el)
			if err != nil {
				return nil, err
			}
			out = append(out, x)
		}
		return out, nil
	case map[string]any:
		if _, def := t["$def"]; def {
			return e.definition(t)
		}
		if _, gen := t["$gen"]; gen {
			items, err := e.generate(t)
			if err != nil {
				return nil, err
			}
			return items, nil
		}
		out := make(map[string]any, len(t))
		for k, el := range t {
			x, err := e.tree(el)
			if err != nil {
				return nil, err
			}
			out[k] = x
		}
		return out, nil
	}
	return v, nil
}

func (e *expander) generate(m map[string]any) ([]any, error) {
	if m["$gen"] != "repeat" {
		return nil, fmt.Errorf("unsupported generator %v", m["$gen"])
	}
	n, err := toInt(m["count"])
	if err != nil || n < 0 || n > 100000 {
		return nil, fmt.Errorf("generator count")
	}
	out := make([]any, 0, n)
	saved, savedHas := e.index, e.hasIdx
	defer func() { e.index, e.hasIdx = saved, savedHas }()
	for i := 0; i < n; i++ {
		e.index, e.hasIdx = i, true
		x, err := e.tree(deepCopy(m["value"]))
		if err != nil {
			return nil, err
		}
		out = append(out, x)
	}
	return out, nil
}

func toInt(v any) (int, error) {
	switch n := v.(type) {
	case json.Number:
		i, err := n.Int64()
		return int(i), err
	case float64:
		return int(n), nil
	}
	return 0, fmt.Errorf("not a number")
}

func deepCopy(v any) any {
	raw, _ := json.Marshal(v)
	var out any
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	_ = dec.Decode(&out)
	return out
}

type patchOp struct {
	Op    string `json:"op"`
	Path  string `json:"path"`
	Value any    `json:"value"`
}

// definition resolves {"$def": id, "patch": [...], "fixture": name}.
func (e *expander) definition(m map[string]any) (any, error) {
	id, _ := m["$def"].(string)
	doc := e.fixture
	if name, ok := m["fixture"].(string); ok && e.lib != nil {
		other, found := e.lib.Fixtures[name]
		if !found {
			return nil, fmt.Errorf("unknown fixture %q", name)
		}
		doc = other
	}
	var found *metadata.Definition
	for i := range doc.Definitions {
		if doc.Definitions[i].ID == id {
			found = &doc.Definitions[i]
		}
	}
	if found == nil {
		return nil, fmt.Errorf("$def %s is not in the fixture", id)
	}
	raw, _ := json.Marshal(found)
	var tree any
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	if err := dec.Decode(&tree); err != nil {
		return nil, err
	}
	if patch, ok := m["patch"].([]any); ok {
		for _, p := range patch {
			pm, _ := p.(map[string]any)
			value, err := e.tree(deepCopy(pm["value"]))
			if err != nil {
				return nil, err
			}
			op := patchOp{Path: fmt.Sprint(pm["path"]), Value: value}
			op.Op, _ = pm["op"].(string)
			if tree, err = applyOp(tree, op); err != nil {
				return nil, fmt.Errorf("$def %s: %w", id, err)
			}
		}
	}
	return tree, nil
}

// applyOp applies one add/replace/remove of a JSON pointer to a decoded tree.
func applyOp(tree any, op patchOp) (any, error) {
	if op.Path == "" || op.Path[0] != '/' {
		return nil, fmt.Errorf("bad path %q", op.Path)
	}
	parts := strings.Split(op.Path[1:], "/")
	for i := range parts {
		parts[i] = strings.ReplaceAll(strings.ReplaceAll(parts[i], "~1", "/"), "~0", "~")
	}
	var walk func(node any, rest []string) (any, error)
	walk = func(node any, rest []string) (any, error) {
		key := rest[0]
		last := len(rest) == 1
		switch n := node.(type) {
		case map[string]any:
			if last {
				switch op.Op {
				case "replace", "add":
					n[key] = op.Value
				case "remove":
					delete(n, key)
				default:
					return nil, fmt.Errorf("unsupported op %q", op.Op)
				}
				return n, nil
			}
			child, ok := n[key]
			if !ok {
				return nil, fmt.Errorf("no member %q", key)
			}
			x, err := walk(child, rest[1:])
			if err != nil {
				return nil, err
			}
			n[key] = x
			return n, nil
		case []any:
			if last && key == "-" && op.Op == "add" {
				return append(n, op.Value), nil
			}
			i, err := strconv.Atoi(key)
			if err != nil || i < 0 || i >= len(n) {
				return nil, fmt.Errorf("bad index %q", key)
			}
			if last {
				switch op.Op {
				case "replace":
					n[i] = op.Value
				case "add":
					n = append(n[:i], append([]any{op.Value}, n[i:]...)...)
				case "remove":
					n = append(n[:i], n[i+1:]...)
				default:
					return nil, fmt.Errorf("unsupported op %q", op.Op)
				}
				return n, nil
			}
			x, err := walk(n[i], rest[1:])
			if err != nil {
				return nil, err
			}
			n[i] = x
			return n, nil
		}
		return nil, fmt.Errorf("cannot descend into %T", node)
	}
	return walk(tree, parts)
}

// str expands the {{...}} placeholders of one string.
func (e *expander) str(s string) (string, error) {
	if !strings.Contains(s, "{{") {
		return s, nil
	}
	var b strings.Builder
	for {
		i := strings.Index(s, "{{")
		if i < 0 {
			b.WriteString(s)
			return b.String(), nil
		}
		j := strings.Index(s[i:], "}}")
		if j < 0 {
			b.WriteString(s)
			return b.String(), nil
		}
		b.WriteString(s[:i])
		name := s[i+2 : i+j]
		s = s[i+j+2:]
		out, err := e.placeholder(name)
		if err != nil {
			return "", err
		}
		b.WriteString(out)
	}
}

func (e *expander) def(id string) (metadata.Definition, error) {
	for _, d := range e.fixture.Definitions {
		if d.ID == id {
			return d, nil
		}
	}
	return metadata.Definition{}, fmt.Errorf("{{hash}}: %s is not in the fixture", id)
}

func (e *expander) placeholder(name string) (string, error) {
	kind, arg, _ := strings.Cut(name, ":")
	switch kind {
	case "i":
		if !e.hasIdx {
			return "", fmt.Errorf("{{i}} outside a generator")
		}
		return strconv.Itoa(e.index), nil
	case "hash", "hash_upper":
		d, err := e.def(arg)
		if err != nil {
			return "", err
		}
		h := metadata.DefinitionHash(d)
		if kind == "hash_upper" {
			h = strings.ToUpper(h)
		}
		return h, nil
	case "hashof":
		fixture, id, _ := strings.Cut(arg, ":")
		if e.lib == nil {
			return "", fmt.Errorf("{{hashof}} needs a library")
		}
		doc, ok := e.lib.Fixtures[fixture]
		if !ok {
			return "", fmt.Errorf("unknown fixture %q", fixture)
		}
		for _, d := range doc.Definitions {
			if d.ID == id {
				return metadata.DefinitionHash(d), nil
			}
		}
		return "", fmt.Errorf("%s is not in fixture %s", id, fixture)
	case "stale":
		d, err := e.def(arg)
		if err != nil {
			return "", err
		}
		d.Label += " (an older version)"
		return metadata.DefinitionHash(d), nil
	case "hash_after":
		id, edit, _ := strings.Cut(arg, ":")
		path, value, _ := strings.Cut(edit, "=")
		d, err := e.def(id)
		if err != nil {
			return "", err
		}
		raw, _ := json.Marshal(d)
		var tree any
		_ = json.Unmarshal(raw, &tree)
		if tree, err = applyOp(tree, patchOp{Op: "replace", Path: path, Value: value}); err != nil {
			return "", err
		}
		out, _ := json.Marshal(tree)
		var after metadata.Definition
		if err = json.Unmarshal(out, &after); err != nil {
			return "", err
		}
		return metadata.DefinitionHash(after), nil
	case "lie":
		return sha256Hex([]byte("lie:" + arg)), nil
	case "canary":
		v, ok := e.c.canaries[arg]
		if !ok {
			return "", fmt.Errorf("unknown canary %q", arg)
		}
		return v, nil
	case "repeat":
		last := strings.LastIndex(arg, ":")
		if last < 0 {
			return "", fmt.Errorf("bad repeat %q", arg)
		}
		n, err := strconv.Atoi(arg[last+1:])
		if err != nil || n < 0 || n > 1<<25 {
			return "", fmt.Errorf("bad repeat count %q", arg[last+1:])
		}
		return strings.Repeat(arg[:last], n), nil
	case "rune":
		r, err := strconv.ParseInt(arg, 16, 32)
		if err != nil {
			return "", err
		}
		return string(rune(r)), nil
	case "rune_repeat":
		hex, count, _ := strings.Cut(arg, ":")
		r, err := strconv.ParseInt(hex, 16, 32)
		n, err2 := strconv.Atoi(count)
		if err != nil || err2 != nil || n < 0 || n > 1<<22 {
			return "", fmt.Errorf("bad rune_repeat %q", arg)
		}
		return strings.Repeat(string(rune(r)), n), nil
	case "wrapper_begin":
		return "<untrusted>", nil
	case "wrapper_end":
		return "</untrusted>", nil
	case "nest_arrays":
		n, err := strconv.Atoi(arg)
		return strings.Repeat("[", n) + strings.Repeat("]", n), err
	case "open_arrays":
		n, err := strconv.Atoi(arg)
		return strings.Repeat("[", n), err
	case "nest_objects":
		n, err := strconv.Atoi(arg)
		return strings.Repeat(`{"a":`, n) + "1" + strings.Repeat("}", n), err
	}
	return "", fmt.Errorf("unknown placeholder {{%s}}", name)
}

// ------------------------------------------------------------ compile function

// CompileFunc builds the compile function of a case from a real compile function. It
// injects the scripted diagnostics or error on the Nth compile of a patched document
// (compiles of the unchanged draft are not counted).
func (c Case) CompileFunc(real func(metadata.Document) (compiler.Program, []compiler.Diagnostic, error)) func(metadata.Document) (compiler.Program, []compiler.Diagnostic, error) {
	draftJSON, _ := json.Marshal(c.Draft)
	var calls int
	e := &expander{c: &c, fixture: c.Draft}
	return func(doc metadata.Document) (compiler.Program, []compiler.Diagnostic, error) {
		raw, _ := json.Marshal(doc)
		if bytes.Equal(raw, draftJSON) {
			return real(doc)
		}
		calls++
		for _, inj := range c.Item.CompileErr {
			if inj.Attempt == calls {
				return compiler.Program{}, nil, fmt.Errorf("%s", inj.Error)
			}
		}
		for _, inj := range c.Item.CompileInj {
			if inj.Attempt != calls {
				continue
			}
			var out []compiler.Diagnostic
			for _, d := range inj.Diagnostics {
				n := d.Repeat
				if n < 1 {
					n = 1
				}
				path, _ := e.str(d.Path)
				code, _ := e.str(d.Code)
				msg, _ := e.str(d.Message)
				for i := 0; i < n; i++ {
					out = append(out, compiler.Diagnostic{Path: path, Code: code, Message: msg})
				}
			}
			return compiler.Program{}, out, nil
		}
		return real(doc)
	}
}

// VendorCatalog is the compile catalog of the items that use the vendor_tools catalog:
// the platform permissions and one operator-registered READ tool.
func VendorCatalog() (compiler.Catalog, error) {
	catalog, err := tools.NewCatalog([]tools.Definition{vendorLookup()})
	if err != nil {
		return compiler.Catalog{}, err
	}
	return compiler.Catalog{Version: "corpus-v1", Permissions: []string{"data.read", "data.write"}, Tools: []string{"tool.vendor.lookup"}, ToolCatalog: catalog}, nil
}

// vendorLookup is the operator-registered READ tool the vendor_tools catalog holds.
func vendorLookup() tools.Definition {
	return tools.Definition{
		Ref: "tool.vendor_lookup", Version: "1.0.0", SideEffect: "READ", Approval: "NONE",
		Delivery: tools.DeliverySpec{IdempotencyMode: "NONE", RetryMode: "SAFE_RETRY", ReconciliationMode: "NONE"},
		Connector: tools.Connector{
			Endpoint: tools.Endpoint{Host: "vendors.example.com", Port: 443, Network: "public"},
			Method:   "GET", Path: "/v1/vendors/{vendor_id}", TimeoutMS: 1500, MaxResponseBytes: 1024, RatePerSecond: 10,
		},
		Input:   []tools.Field{{Name: "vendor_id", Type: "string", Required: true, MaxLength: 64, In: "path"}},
		Output:  []tools.Field{{Name: "name", Type: "string", MaxLength: 64}},
		Sandbox: tools.SandboxSpec{Mode: "READ_ONLY", Endpoint: &tools.Endpoint{Host: "sandbox.vendors.example.com", Port: 443, Network: "public"}},
		Tenants: []string{"*"},
	}
}

// SortedKeys is a helper for stable reports.
func SortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
