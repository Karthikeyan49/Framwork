package ai

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/netip"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/Karthikeyan49/Framwork/internal/metadata"
	"github.com/Karthikeyan49/Framwork/internal/tools"
)

// Provider kinds of ai-provider-v1.
const (
	KindHTTPSJSON = "https-json"
	KindScripted  = "scripted" // development and tests only; the composition root refuses it elsewhere
)

// ProviderConfig is the validated ai-provider-v1 configuration (AI_PROVIDER_FILE).
// LoadProviderConfig rejects any limit above the contract ceilings (it never raises
// a ceiling and never clamps silently) and fills the defaults of absent limits.
type ProviderConfig struct {
	Version, Kind, Endpoint, Model, CredentialFile, ScriptFile string
	MaxAttempts, MaxOutputTokens, InFlightPerTenant            int
	WallClock                                                  time.Duration
	DailyTokenBudget                                           int64
}

// providerFile is the on-disk shape. Every key is lowercase; unknown, duplicate and
// case-folded keys are rejected before this struct is filled.
type providerFile struct {
	Version          string `json:"version"`
	Kind             string `json:"kind"`
	Endpoint         string `json:"endpoint"`
	Model            string `json:"model"`
	CredentialFile   string `json:"credential_file"`
	ScriptFile       string `json:"script_file"`
	MaxAttempts      *int   `json:"max_attempts"`
	MaxOutputTokens  *int   `json:"max_output_tokens"`
	InFlightPerTen   *int   `json:"in_flight_per_tenant"`
	WallClockMS      *int64 `json:"wall_clock_ms"`
	DailyTokenBudget *int64 `json:"daily_token_budget"`
}

var providerKeys = []string{"version", "kind", "endpoint", "model", "credential_file", "script_file", "max_attempts", "max_output_tokens", "in_flight_per_tenant", "wall_clock_ms", "daily_token_budget"}

var modelLabel = regexp.MustCompile(`^[A-Za-z0-9._:/@+-]{1,128}$`)

// maxDailyTokenBudget is the database ceiling of a daily budget (migration 60).
const maxDailyTokenBudget = 100_000_000

// readBounded reads a regular file of at most limit bytes.
func readBounded(path string, limit int) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("%w: file is not readable", ErrInvalidInput)
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return nil, fmt.Errorf("%w: not a regular file", ErrInvalidInput)
	}
	raw, err := io.ReadAll(io.LimitReader(f, int64(limit)+1))
	if err != nil || len(raw) > limit || len(raw) == 0 {
		return nil, fmt.Errorf("%w: file is empty or larger than %d bytes", ErrInvalidInput, limit)
	}
	return raw, nil
}

// decodeStrictObject decodes raw into dest with the shared strict JSON rules and an
// exact, case-sensitive key set.
func decodeStrictObject(raw []byte, limit int, dest any, keys []string) error {
	tree, err := metadata.DecodeJSONBounded(raw, limit)
	if err != nil {
		return fmt.Errorf("%w: not strict JSON", ErrInvalidInput)
	}
	object, ok := tree.(map[string]any)
	if !ok {
		return fmt.Errorf("%w: not a JSON object", ErrInvalidInput)
	}
	for key := range object {
		known := false
		for _, k := range keys {
			known = known || k == key
		}
		if !known {
			return fmt.Errorf("%w: unknown field", ErrInvalidInput)
		}
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if err = d.Decode(dest); err != nil || d.More() {
		return fmt.Errorf("%w: wrong field types", ErrInvalidInput)
	}
	return nil
}

// LoadProviderConfig strictly loads an ai-provider-v1 file and returns the SHA-256
// (hex) of its bytes for the audit trail. It reads only the named file. Errors carry
// fixed text: never the path, a credential or file content.
func LoadProviderConfig(path string) (ProviderConfig, string, error) {
	var zero ProviderConfig
	raw, err := readBounded(path, MaxConfigBytes)
	if err != nil {
		return zero, "", err
	}
	var f providerFile
	if err = decodeStrictObject(raw, MaxConfigBytes, &f, providerKeys); err != nil {
		return zero, "", err
	}
	bad := func(why string) (ProviderConfig, string, error) {
		return zero, "", fmt.Errorf("%w: ai-provider-v1 %s", ErrInvalidInput, why)
	}
	if f.Version != ProviderConfigVersion {
		return bad("version is not supported")
	}
	cfg := ProviderConfig{Version: f.Version, Kind: f.Kind, Endpoint: f.Endpoint, Model: f.Model, CredentialFile: f.CredentialFile, ScriptFile: f.ScriptFile}
	if !modelLabel.MatchString(cfg.Model) {
		return bad("model must be a short label")
	}
	switch cfg.Kind {
	case KindHTTPSJSON:
		if cfg.ScriptFile != "" {
			return bad("script_file belongs to kind scripted")
		}
		if err = validateEndpoint(cfg.Endpoint); err != nil {
			return bad(err.Error())
		}
		if cfg.CredentialFile != "" {
			if _, _, err = credentialRef(cfg.CredentialFile); err != nil {
				return bad(err.Error())
			}
		}
	case KindScripted:
		if cfg.Endpoint != "" || cfg.CredentialFile != "" {
			return bad("endpoint and credential_file belong to kind https-json")
		}
		if cfg.ScriptFile == "" || !filepath.IsAbs(cfg.ScriptFile) {
			return bad("script_file must be an absolute path")
		}
	default:
		return bad("kind is not supported")
	}
	within := func(p *int, def, ceiling int, what string) (int, error) {
		if p == nil {
			return def, nil
		}
		if *p < 1 || *p > ceiling {
			return 0, fmt.Errorf("%s must be between 1 and %d", what, ceiling)
		}
		return *p, nil
	}
	if cfg.MaxAttempts, err = within(f.MaxAttempts, MaxAttempts, MaxAttempts, "max_attempts"); err != nil {
		return bad(err.Error())
	}
	if cfg.MaxOutputTokens, err = within(f.MaxOutputTokens, DefaultMaxOutputTokens, MaxOutputTokensCeiling, "max_output_tokens"); err != nil {
		return bad(err.Error())
	}
	if cfg.InFlightPerTenant, err = within(f.InFlightPerTen, DefaultInFlightPerTenant, MaxInFlightPerTenant, "in_flight_per_tenant"); err != nil {
		return bad(err.Error())
	}
	cfg.WallClock = DefaultWallClock
	if f.WallClockMS != nil {
		if *f.WallClockMS < 1 || *f.WallClockMS > DefaultWallClock.Milliseconds() {
			return bad(fmt.Sprintf("wall_clock_ms must be between 1 and %d", DefaultWallClock.Milliseconds()))
		}
		cfg.WallClock = time.Duration(*f.WallClockMS) * time.Millisecond
	}
	cfg.DailyTokenBudget = DefaultDailyToken
	if f.DailyTokenBudget != nil {
		if *f.DailyTokenBudget < 1 || *f.DailyTokenBudget > maxDailyTokenBudget {
			return bad(fmt.Sprintf("daily_token_budget must be between 1 and %d", maxDailyTokenBudget))
		}
		cfg.DailyTokenBudget = *f.DailyTokenBudget
	}
	return cfg, sha256Hex(raw), nil
}

// parseEndpoint splits a validated endpoint into the pieces the adapter dials.
func parseEndpoint(endpoint string) (host string, port int, u *url.URL, err error) {
	u, perr := url.Parse(endpoint)
	switch {
	case perr != nil || len(endpoint) > 512 || strings.ContainsAny(endpoint, " \t\r\n\\"):
		return "", 0, nil, fmt.Errorf("endpoint is not a URL")
	case u.Scheme != "https":
		return "", 0, nil, fmt.Errorf("endpoint must use https")
	case u.User != nil || u.Fragment != "" || u.RawQuery != "" || u.Opaque != "" || u.Hostname() == "":
		return "", 0, nil, fmt.Errorf("endpoint must be https://host[:port]/path without credentials, query or fragment")
	}
	host = strings.ToLower(u.Hostname())
	for i := 0; i < len(host); i++ {
		if host[i] >= 0x80 || host[i] <= 0x20 {
			return "", 0, nil, fmt.Errorf("endpoint host must be ASCII")
		}
	}
	port = 443
	portText := u.Port()
	if p := portText; p != "" {
		n := 0
		for _, c := range p {
			if c < '0' || c > '9' || n > 65535 {
				return "", 0, nil, fmt.Errorf("endpoint port is invalid")
			}
			n = n*10 + int(c-'0')
		}
		if n < 1 || n > 65535 {
			return "", 0, nil, fmt.Errorf("endpoint port is invalid")
		}
		port = n
	}
	// The transport dials the host exactly as the URL spells it; normalize the case so
	// the dialer's own comparison (which uses the lower-case host) cannot disagree.
	u.Host = host
	if strings.Contains(host, ":") {
		u.Host = "[" + host + "]"
	}
	if portText != "" {
		u.Host = net.JoinHostPort(host, portText)
	}
	return host, port, u, nil
}

// validateEndpoint is the config-time check; the dial-time check is the real gate.
func validateEndpoint(endpoint string) error {
	host, _, _, err := parseEndpoint(endpoint)
	if err != nil {
		return err
	}
	if host == "localhost" || strings.HasSuffix(host, ".localhost") || strings.HasSuffix(host, ".internal") || strings.HasSuffix(host, ".local") {
		return fmt.Errorf("endpoint host is not a public host")
	}
	if ip, perr := netip.ParseAddr(host); perr == nil && tools.DeniedAddress(ip, false) {
		return fmt.Errorf("endpoint address is not a public address")
	}
	return nil
}

// credentialRef splits an absolute credential path into the directory and the
// reference tools.FileSecrets resolves (a file name of [a-z][a-z0-9_]{0,63}).
func credentialRef(path string) (dir, ref string, err error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return "", "", fmt.Errorf("credential_file must be a clean absolute path")
	}
	dir, ref = filepath.Split(path)
	dir = filepath.Clean(dir)
	if len(ref) == 0 || len(ref) > 64 || ref[0] < 'a' || ref[0] > 'z' {
		return "", "", fmt.Errorf("credential_file name must match [a-z][a-z0-9_]{0,63}")
	}
	for i := 0; i < len(ref); i++ {
		if c := ref[i]; !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '_') {
			return "", "", fmt.Errorf("credential_file name must match [a-z][a-z0-9_]{0,63}")
		}
	}
	return dir, ref, nil
}

// NewProvider builds the adapter a configuration names.
func NewProvider(cfg ProviderConfig) (Provider, error) {
	switch cfg.Kind {
	case KindHTTPSJSON:
		return newHTTPJSONProvider(cfg, httpOptions{})
	case KindScripted:
		steps, err := LoadScript(cfg.ScriptFile)
		if err != nil {
			return nil, err
		}
		return NewScriptedProvider(steps...), nil
	}
	return nil, fmt.Errorf("%w: unknown provider kind", ErrInvalidInput)
}

// Budget derives the pipeline budget of one run from the configuration and the
// tenant's remaining daily token budget.
func (c ProviderConfig) Budget(tokensRemaining int64) Budget {
	return Budget{MaxAttempts: c.MaxAttempts, MaxOutputTokens: c.MaxOutputTokens, WallClock: c.WallClock, TokensRemaining: tokensRemaining}
}
