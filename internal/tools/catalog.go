package tools

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net"
	"regexp"
	"sort"
	"strings"
)

// CatalogVersion identifies the operator-owned tool catalog contract (ADR036).
// Tools are registered by the platform operator, never by tenant metadata,
// requests or AI output. Tenants only reference a registered ref/version.
const CatalogVersion = "tool-catalog-v1"

// MaxTimeoutMS keeps a send inside one 5 s workflow lease with margin for the
// surrounding fenced transactions, so M10 needs no lease renewal.
const MaxTimeoutMS = 2000
const MaxResponseBytes = 16384
const MaxCatalogBytes = 1 << 20
const MaxTools = 256
const MaxFields = 32

var ErrCatalog = errors.New("invalid tool catalog")

var refPattern = regexp.MustCompile(`^tool\.[a-z][a-z0-9_]*(\.[a-z][a-z0-9_]*)*$`)
var semverPattern = regexp.MustCompile(`^[1-9][0-9]{0,3}\.[0-9]{1,4}\.[0-9]{1,4}$`)
var namePattern = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)
var secretPattern = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)
var headerPattern = regexp.MustCompile(`^[A-Z][A-Za-z0-9-]{0,63}$`)
var hostPattern = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?(\.[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?)+$`)
var tenantPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
var segmentPattern = regexp.MustCompile(`^[A-Za-z0-9._~-]+$`)

// Field is one typed scalar of a tool input or output. Inputs are placed in
// the path or JSON body; outputs are read from a flat JSON object response.
type Field struct {
	Name      string `json:"name"`
	Type      string `json:"type"` // string, integer, decimal, bool
	Required  bool   `json:"required"`
	MaxLength int    `json:"max_length,omitempty"`
	In        string `json:"in,omitempty"` // inputs only: path or body
}

// Endpoint is one fixed HTTPS destination. Hosts are literal DNS names; the
// network policy validates every resolved address at dial time.
type Endpoint struct {
	Host      string `json:"host"`
	Port      int    `json:"port"`
	Network   string `json:"network"` // public, internal (internal requires process opt-in)
	SecretRef string `json:"secret_ref,omitempty"`
}
type Lookup struct {
	Method string `json:"method"` // GET
	Path   string `json:"path"`   // must contain exactly one {key}
	// SettleMS is the provider's declared settle interval: how long after the
	// original send's deadline a write can still become visible. A 404 lookup
	// proves absence only when it starts after send start + timeout + SettleMS;
	// without a declaration a lookup can find an effect but never prove absence
	// (ADR036 Amendment A4). Omitted when zero, so existing hashes are unchanged.
	SettleMS int `json:"settle_ms,omitempty"`
}

// MaxSettleMS bounds a declared provider settle interval.
const MaxSettleMS = 600000
type Connector struct {
	Endpoint
	Method            string  `json:"method"` // GET, POST, PUT
	Path              string  `json:"path"`
	IdempotencyHeader string  `json:"idempotency_header,omitempty"`
	Lookup            *Lookup `json:"lookup,omitempty"`
	TimeoutMS         int     `json:"timeout_ms"`
	MaxResponseBytes  int     `json:"max_response_bytes"`
	RatePerSecond     int     `json:"rate_per_second"`
}
type DeliverySpec struct {
	IdempotencyMode    string `json:"idempotency_mode"`
	RetryMode          string `json:"retry_mode"`
	ReconciliationMode string `json:"reconciliation_mode"`
	// ReplayWindowMS is the provider's declared key-retention/dedupe window,
	// measured from the first send. Automatic resends never exceed it.
	ReplayWindowMS int64 `json:"replay_window_ms,omitempty"`
}

// SandboxSpec is the tool's own non-production behavior. REROUTE/TEST_ACCOUNT
// send only to Endpoint; SIMULATE returns Output without any I/O.
type SandboxSpec struct {
	Mode     string                     `json:"mode"` // BLOCK, SIMULATE, REROUTE, READ_ONLY, TEST_ACCOUNT
	Endpoint *Endpoint                  `json:"endpoint,omitempty"`
	Output   map[string]json.RawMessage `json:"simulated_output,omitempty"`
}
type Definition struct {
	Ref        string       `json:"ref"`
	Version    string       `json:"version"`
	SideEffect string       `json:"side_effect"` // NONE, READ, WRITE
	Approval   string       `json:"approval"`    // NONE, REQUIRED
	Delivery   DeliverySpec `json:"delivery"`
	Connector  Connector    `json:"connector"`
	Input      []Field      `json:"input"`
	Output     []Field      `json:"output"`
	Sandbox    SandboxSpec  `json:"sandbox"`
	Tenants    []string     `json:"tenants"`
}
type catalogDocument struct {
	SchemaVersion string       `json:"schema_version"`
	Tools         []Definition `json:"tools"`
}

// Catalog is immutable after load. Definitions are keyed by ref and version.
type Catalog struct {
	definitions map[string]Definition
	hashes      map[string]string
	hash        string
}

func key(ref, version string) string { return ref + "@" + version }

// Delivery converts the declared semantics to the M6R decision contract.
func (d Definition) DeliveryContract() Delivery {
	return Delivery{SideEffectClass: d.SideEffect, IdempotencyMode: d.Delivery.IdempotencyMode, RetryMode: d.Delivery.RetryMode, ReconciliationMode: d.Delivery.ReconciliationMode}
}

// Hash is the canonical identity pinned by compiled workflow plans.
func (d Definition) Hash() string {
	raw, _ := json.Marshal(d)
	h := sha256.Sum256(raw)
	return hex.EncodeToString(h[:])
}
func (d Definition) AllowsTenant(tenant string) bool {
	for _, t := range d.Tenants {
		if t == "*" || t == tenant {
			return true
		}
	}
	return false
}
func (d Definition) InputField(name string) (Field, bool) {
	for _, f := range d.Input {
		if f.Name == name {
			return f, true
		}
	}
	return Field{}, false
}

func EmptyCatalog() Catalog {
	c, _ := NewCatalog(nil)
	return c
}

// LoadCatalog strictly decodes operator configuration. Unknown, duplicate or
// differently cased keys, trailing documents and unsafe definitions fail closed.
func LoadCatalog(raw []byte) (Catalog, error) {
	if len(raw) == 0 || len(raw) > MaxCatalogBytes || !strictKeys(raw) {
		return Catalog{}, ErrCatalog
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var doc catalogDocument
	if decoder.Decode(&doc) != nil || decoder.More() || doc.SchemaVersion != CatalogVersion || doc.Tools == nil {
		return Catalog{}, ErrCatalog
	}
	// Only whitespace may follow. A stray '}' or ']' is not a decodable value,
	// so end of input is required explicitly.
	if _, err := decoder.Token(); err != io.EOF {
		return Catalog{}, ErrCatalog
	}
	return NewCatalog(doc.Tools)
}

func NewCatalog(definitions []Definition) (Catalog, error) {
	if len(definitions) > MaxTools {
		return Catalog{}, ErrCatalog
	}
	c := Catalog{definitions: map[string]Definition{}, hashes: map[string]string{}}
	keys := []string{}
	for _, d := range definitions {
		normalized, err := validateDefinition(d)
		if err != nil {
			return Catalog{}, err
		}
		k := key(normalized.Ref, normalized.Version)
		if _, exists := c.definitions[k]; exists {
			return Catalog{}, ErrCatalog
		}
		c.definitions[k] = normalized
		c.hashes[k] = normalized.Hash()
		keys = append(keys, k)
	}
	sort.Strings(keys)
	h := sha256.New()
	h.Write([]byte(CatalogVersion))
	for _, k := range keys {
		h.Write([]byte("\n" + k + "=" + c.hashes[k]))
	}
	c.hash = hex.EncodeToString(h.Sum(nil))
	return c, nil
}
func (c Catalog) Hash() string { return c.hash }
func (c Catalog) Len() int     { return len(c.definitions) }

// Lookup returns a definition only when its canonical hash still matches the
// pin; a changed or removed tool never silently replaces a pinned one.
func (c Catalog) Lookup(ref, version, pinned string) (Definition, bool) {
	k := key(ref, version)
	d, ok := c.definitions[k]
	if !ok || (pinned != "" && c.hashes[k] != pinned) {
		return Definition{}, false
	}
	return d, true
}

// Current returns the live definition for ref@version regardless of any pin. It
// exists so staleness can tell a changed definition (a different hash is
// installed) from a removed one; it never authorizes a send.
func (c Catalog) Current(ref, version string) (Definition, bool) {
	d, ok := c.definitions[key(ref, version)]
	return d, ok
}

func validateDefinition(d Definition) (Definition, error) {
	bad := ErrCatalog
	if !refPattern.MatchString(d.Ref) || len(d.Ref) > 160 || !semverPattern.MatchString(d.Version) {
		return d, bad
	}
	// M10 ships no financial/destructive tools; those need M13 authority.
	if d.SideEffect != "NONE" && d.SideEffect != "READ" && d.SideEffect != "WRITE" {
		return d, bad
	}
	if d.Approval != "NONE" && d.Approval != "REQUIRED" || d.SideEffect == "WRITE" && d.Approval != "REQUIRED" {
		return d, bad
	}
	if d.DeliveryContract().Validate() != nil || d.Delivery.ReplayWindowMS < 0 || d.Delivery.ReplayWindowMS > 7*86400000 {
		return d, bad
	}
	mode := d.Delivery.IdempotencyMode
	if (mode == "NATIVE_KEY" || mode == "DEDUPE_WINDOW") != (d.Delivery.ReplayWindowMS > 0) {
		return d, bad
	}
	if (mode == "LOOKUP_BY_REFERENCE") != (d.Connector.Lookup != nil) || mode == "LOOKUP_BY_REFERENCE" && d.Delivery.ReconciliationMode != "REMOTE_LOOKUP" {
		return d, bad
	}
	if (mode == "NONE") != (d.Connector.IdempotencyHeader == "") || d.Connector.IdempotencyHeader != "" && !safeHeader(d.Connector.IdempotencyHeader) {
		return d, bad
	}
	c := d.Connector
	if !validEndpoint(c.Endpoint) || c.TimeoutMS < 1 || c.TimeoutMS > MaxTimeoutMS || c.MaxResponseBytes < 1 || c.MaxResponseBytes > MaxResponseBytes || c.RatePerSecond < 1 || c.RatePerSecond > 100 {
		return d, bad
	}
	if c.Method != "GET" && c.Method != "POST" && c.Method != "PUT" || (d.SideEffect != "WRITE") != (c.Method == "GET") {
		return d, bad
	}
	if c.Lookup != nil && (c.Lookup.Method != "GET" || !validTemplate(c.Lookup.Path, map[string]bool{"key": true}, true) || c.Lookup.SettleMS < 0 || c.Lookup.SettleMS > MaxSettleMS) {
		return d, bad
	}
	if len(d.Input) > MaxFields || len(d.Output) < 1 || len(d.Output) > MaxFields {
		return d, bad
	}
	pathFields := map[string]bool{}
	seen := map[string]bool{}
	for _, f := range d.Input {
		if !validField(f, true) || seen[f.Name] || f.Name == "key" {
			return d, bad
		}
		seen[f.Name] = true
		if f.In == "path" {
			if !f.Required {
				return d, bad
			}
			pathFields[f.Name] = true
		}
		if f.In == "body" && c.Method == "GET" {
			return d, bad
		}
	}
	if !validTemplate(c.Path, pathFields, false) {
		return d, bad
	}
	seen = map[string]bool{}
	for _, f := range d.Output {
		if !validField(f, false) || seen[f.Name] {
			return d, bad
		}
		seen[f.Name] = true
	}
	s := d.Sandbox
	switch s.Mode {
	case "BLOCK":
		if s.Endpoint != nil || len(s.Output) > 0 {
			return d, bad
		}
	case "SIMULATE":
		if s.Endpoint != nil || !validSimulated(d.Output, s.Output) {
			return d, bad
		}
	case "REROUTE", "TEST_ACCOUNT", "READ_ONLY":
		if s.Endpoint == nil || !validEndpoint(*s.Endpoint) || len(s.Output) > 0 || s.Endpoint.Host == c.Host && s.Endpoint.Port == c.Port {
			return d, bad
		}
		// A sandbox credential is always distinct from the production one.
		if s.Endpoint.SecretRef != "" && s.Endpoint.SecretRef == c.SecretRef {
			return d, bad
		}
		if s.Mode == "TEST_ACCOUNT" && s.Endpoint.SecretRef == "" || s.Mode == "READ_ONLY" && d.SideEffect == "WRITE" {
			return d, bad
		}
	default:
		return d, bad
	}
	if len(d.Tenants) < 1 || len(d.Tenants) > 1000 {
		return d, bad
	}
	tenants := append([]string(nil), d.Tenants...)
	sort.Strings(tenants)
	for i, t := range tenants {
		if t != "*" && !tenantPattern.MatchString(t) || i > 0 && tenants[i-1] == t || t == "*" && len(tenants) != 1 {
			return d, bad
		}
	}
	d.Tenants = tenants
	return d, nil
}
func safeHeader(h string) bool {
	lower := strings.ToLower(h)
	if !headerPattern.MatchString(h) {
		return false
	}
	// Headers the gateway sets, and headers that change framing, encoding,
	// expectations, proxy or browser semantics, can never carry the key.
	for _, reserved := range []string{"authorization", "host", "content-length", "content-type", "transfer-encoding", "connection", "cookie", "proxy-authorization", "te", "upgrade", "x-forwarded-for", "x-forwarded-host", "forwarded", "accept", "accept-encoding", "content-encoding", "expect", "keep-alive", "trailer", "user-agent", "via", "x-http-method-override", "x-method-override"} {
		if lower == reserved {
			return false
		}
	}
	return !strings.HasPrefix(lower, "proxy-") && !strings.HasPrefix(lower, "sec-")
}
func validEndpoint(e Endpoint) bool {
	if len(e.Host) > 253 || !hostPattern.MatchString(e.Host) || net.ParseIP(e.Host) != nil || numericFinalLabel(e.Host) || e.Port < 1 || e.Port > 65535 {
		return false
	}
	if e.Network != "public" && e.Network != "internal" {
		return false
	}
	return e.SecretRef == "" || secretPattern.MatchString(e.SecretRef)
}

// numericFinalLabel reports a host whose last label is decimal digits or
// 0x-hex. URL parsers and system resolvers treat such names as IPv4 literals
// ("127.1", "0x7f.1"), so they are never literal DNS hosts.
func numericFinalLabel(host string) bool {
	last := host[strings.LastIndexByte(host, '.')+1:]
	digits := "0123456789"
	if strings.HasPrefix(last, "0x") {
		last, digits = last[2:], "0123456789abcdef"
		if last == "" {
			return true
		}
	}
	return last != "" && strings.Trim(last, digits) == ""
}
func validField(f Field, input bool) bool {
	if !namePattern.MatchString(f.Name) {
		return false
	}
	switch f.Type {
	case "string":
		if f.MaxLength < 1 || f.MaxLength > 4096 {
			return false
		}
	case "integer", "decimal", "bool":
		if f.MaxLength != 0 {
			return false
		}
	default:
		return false
	}
	if input {
		return f.In == "path" || f.In == "body"
	}
	return f.In == ""
}

// validTemplate accepts absolute paths of literal unreserved segments and whole
// {name} parameters. Every parameter must be declared and used exactly once.
func validTemplate(path string, params map[string]bool, exactlyKey bool) bool {
	if len(path) < 1 || len(path) > 512 || path[0] != '/' || strings.Contains(path, "//") {
		return false
	}
	used := map[string]bool{}
	for _, segment := range strings.Split(path[1:], "/") {
		if segment == "" || segment == "." || segment == ".." {
			return false
		}
		if strings.HasPrefix(segment, "{") && strings.HasSuffix(segment, "}") {
			name := segment[1 : len(segment)-1]
			if !params[name] || used[name] {
				return false
			}
			used[name] = true
			continue
		}
		if !segmentPattern.MatchString(segment) {
			return false
		}
	}
	if exactlyKey {
		return len(used) == 1 && used["key"]
	}
	return len(used) == len(params)
}
func validSimulated(fields []Field, output map[string]json.RawMessage) bool {
	if len(output) > len(fields) {
		return false
	}
	for name := range output {
		found := false
		for _, f := range fields {
			found = found || f.Name == name
		}
		if !found {
			return false
		}
	}
	_, err := DecodeOutput(fields, output)
	return err == nil
}

// DecodeOutput validates a flat provider response against the declared fields.
// Unknown response members are ignored and never stored.
func DecodeOutput(fields []Field, raw map[string]json.RawMessage) (map[string]any, error) {
	out := map[string]any{}
	for _, f := range fields {
		v, ok := raw[f.Name]
		if !ok || string(v) == "null" {
			if f.Required {
				return nil, ErrCatalog
			}
			continue
		}
		value, err := decodeScalar(f, v)
		if err != nil {
			return nil, err
		}
		out[f.Name] = value
	}
	return out, nil
}

// DecodeInput validates one typed input value from the workflow snapshot.
func DecodeInput(f Field, raw json.RawMessage) (any, error) { return decodeScalar(f, raw) }

var decimalPattern = regexp.MustCompile(`^-?(0|[1-9][0-9]{0,17})(\.[0-9]{1,18})?$`)

func decodeScalar(f Field, raw json.RawMessage) (any, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var v any
	if decoder.Decode(&v) != nil || decoder.More() {
		return nil, ErrCatalog
	}
	switch f.Type {
	case "string":
		s, ok := v.(string)
		if !ok || len(s) > f.MaxLength || strings.ContainsRune(s, 0) {
			return nil, ErrCatalog
		}
		return s, nil
	case "bool":
		b, ok := v.(bool)
		if !ok {
			return nil, ErrCatalog
		}
		return b, nil
	case "integer":
		n, ok := v.(json.Number)
		if !ok {
			return nil, ErrCatalog
		}
		i, err := n.Int64()
		if err != nil || i > 9007199254740991 || i < -9007199254740991 {
			return nil, ErrCatalog
		}
		return i, nil
	case "decimal":
		// Exact decimals travel as strings, never binary floating point.
		s, ok := v.(string)
		if !ok || !decimalPattern.MatchString(s) {
			return nil, ErrCatalog
		}
		return s, nil
	}
	return nil, ErrCatalog
}

// strictKeys rejects duplicate and non-lowercase keys before typed decoding;
// encoding/json alone accepts differently cased aliases.
func strictKeys(raw []byte) bool {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var walk func(depth int) bool
	walk = func(depth int) bool {
		if depth > 16 {
			return false
		}
		token, err := decoder.Token()
		if err != nil {
			return false
		}
		switch t := token.(type) {
		case json.Delim:
			switch t {
			case '{':
				seen := map[string]bool{}
				for decoder.More() {
					k, err := decoder.Token()
					name, ok := k.(string)
					if err != nil || !ok || seen[name] || name != strings.ToLower(name) {
						return false
					}
					seen[name] = true
					if !walk(depth + 1) {
						return false
					}
				}
				_, err = decoder.Token()
				return err == nil
			case '[':
				for decoder.More() {
					if !walk(depth + 1) {
						return false
					}
				}
				_, err = decoder.Token()
				return err == nil
			}
			return false
		default:
			return true
		}
	}
	return walk(0)
}
