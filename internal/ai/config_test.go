package ai

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func writeFile(t *testing.T, name, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

const validHTTPS = `{"version":"ai-provider-v1","kind":"https-json","endpoint":"https://models.example.com/v1/complete","model":"vendor-model-1"}`

func TestLoadProviderConfigDefaultsAndHash(t *testing.T) {
	path := writeFile(t, "ai.json", validHTTPS)
	cfg, hash, err := LoadProviderConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Kind != KindHTTPSJSON || cfg.Model != "vendor-model-1" || cfg.MaxAttempts != MaxAttempts || cfg.MaxOutputTokens != DefaultMaxOutputTokens ||
		cfg.InFlightPerTenant != DefaultInFlightPerTenant || cfg.WallClock != DefaultWallClock || cfg.DailyTokenBudget != DefaultDailyToken {
		t.Fatalf("defaults %+v", cfg)
	}
	if hash != sha256Hex([]byte(validHTTPS)) {
		t.Fatal("the hash is of the file bytes")
	}
	if _, h2, _ := LoadProviderConfig(path); h2 != hash {
		t.Fatal("hash is not stable")
	}
	b := cfg.Budget(1234)
	if b.TokensRemaining != 1234 || b.MaxAttempts != cfg.MaxAttempts || b.WallClock != cfg.WallClock {
		t.Fatalf("budget %+v", b)
	}
}

func TestLoadProviderConfigLimits(t *testing.T) {
	ok := func(extra string) string { return strings.Replace(validHTTPS, "}", ","+extra+"}", 1) }
	good := map[string]func(ProviderConfig) bool{
		`"max_attempts":2`:                func(c ProviderConfig) bool { return c.MaxAttempts == 2 },
		`"max_output_tokens":1000`:        func(c ProviderConfig) bool { return c.MaxOutputTokens == 1000 },
		`"in_flight_per_tenant":1`:        func(c ProviderConfig) bool { return c.InFlightPerTenant == 1 },
		`"wall_clock_ms":2500`:            func(c ProviderConfig) bool { return c.WallClock == 2500*time.Millisecond },
		`"daily_token_budget":123456`:     func(c ProviderConfig) bool { return c.DailyTokenBudget == 123456 },
		`"max_attempts":3`:                func(c ProviderConfig) bool { return c.MaxAttempts == 3 },
		`"daily_token_budget":100000000`:  func(c ProviderConfig) bool { return c.DailyTokenBudget == 100_000_000 },
		`"wall_clock_ms":60000`:           func(c ProviderConfig) bool { return c.WallClock == time.Minute },
		`"credential_file":"/run/ai/key"`: func(c ProviderConfig) bool { return c.CredentialFile == "/run/ai/key" },
	}
	for extra, check := range good {
		cfg, _, err := LoadProviderConfig(writeFile(t, "ai.json", ok(extra)))
		if err != nil || !check(cfg) {
			t.Errorf("%s: %v %+v", extra, err, cfg)
		}
	}
	// A ceiling is never raised, a floor never lowered, and nothing is silently clamped.
	for _, extra := range []string{
		`"max_attempts":4`, `"max_attempts":0`, `"max_attempts":-1`, `"max_output_tokens":0`, `"max_output_tokens":` + itoa(MaxOutputTokensCeiling+1),
		`"in_flight_per_tenant":9`, `"in_flight_per_tenant":0`, `"wall_clock_ms":60001`, `"wall_clock_ms":0`, `"daily_token_budget":100000001`, `"daily_token_budget":0`,
		`"max_attempts":"3"`, `"max_attempts":1.5`, `"wall_clock_ms":null,"wall_clock_ms":5`,
		`"credential_file":"relative/key"`, `"credential_file":"/run/ai/Key"`, `"credential_file":"/run/ai/../ai/key"`, `"credential_file":"/run/ai/"`, `"credential_file":"/run/ai/1key"`,
		`"script_file":"/tmp/x"`, `"unknown":1`, `"Model":"upper"`,
	} {
		if _, _, err := LoadProviderConfig(writeFile(t, "ai.json", ok(extra))); !errors.Is(err, ErrInvalidInput) {
			t.Errorf("%s accepted: %v", extra, err)
		}
	}
}

func itoa(n int) string { return strings.TrimSpace(strings.Repeat(" ", 0) + string(appendInt(nil, n))) }

func appendInt(b []byte, n int) []byte {
	if n == 0 {
		return append(b, '0')
	}
	var d []byte
	for ; n > 0; n /= 10 {
		d = append([]byte{byte('0' + n%10)}, d...)
	}
	return append(b, d...)
}

func TestLoadProviderConfigStrictness(t *testing.T) {
	cases := map[string]string{
		"empty":                ``,
		"not an object":        `[]`,
		"trailing data":        validHTTPS + `{}`,
		"duplicate key":        `{"version":"ai-provider-v1","version":"ai-provider-v1","kind":"https-json","endpoint":"https://a.example.com/","model":"m"}`,
		"case-folded dup":      `{"version":"ai-provider-v1","Version":"ai-provider-v1","kind":"https-json","endpoint":"https://a.example.com/","model":"m"}`,
		"wrong version":        strings.Replace(validHTTPS, "ai-provider-v1", "ai-provider-v2", 1),
		"no version":           `{"kind":"https-json","endpoint":"https://a.example.com/","model":"m"}`,
		"unknown kind":         strings.Replace(validHTTPS, "https-json", "openai", 1),
		"http endpoint":        strings.Replace(validHTTPS, "https://", "http://", 1),
		"no model":             `{"version":"ai-provider-v1","kind":"https-json","endpoint":"https://a.example.com/"}`,
		"bad model":            strings.Replace(validHTTPS, "vendor-model-1", "model with spaces", 1),
		"long model":           strings.Replace(validHTTPS, "vendor-model-1", strings.Repeat("m", 129), 1),
		"userinfo":             strings.Replace(validHTTPS, "models.example.com", "user:pw@models.example.com", 1),
		"query":                strings.Replace(validHTTPS, "/v1/complete", "/v1/complete?key=abc", 1),
		"fragment":             strings.Replace(validHTTPS, "/v1/complete", "/v1#x", 1),
		"localhost":            strings.Replace(validHTTPS, "models.example.com", "localhost", 1),
		"loopback":             strings.Replace(validHTTPS, "models.example.com", "127.0.0.1", 1),
		"private":              strings.Replace(validHTTPS, "models.example.com", "10.1.2.3", 1),
		"link local":           strings.Replace(validHTTPS, "models.example.com", "169.254.169.254", 1),
		"ipv6 loopback":        strings.Replace(validHTTPS, "models.example.com", "[::1]", 1),
		"internal suffix":      strings.Replace(validHTTPS, "models.example.com", "api.corp.internal", 1),
		"bad port":             strings.Replace(validHTTPS, "models.example.com", "models.example.com:70000", 1),
		"port zero":            strings.Replace(validHTTPS, "models.example.com", "models.example.com:0", 1),
		"non ascii host":       strings.Replace(validHTTPS, "models.example.com", "mödels.example.com", 1),
		"script on https":      strings.Replace(validHTTPS, "}", `,"script_file":"/tmp/s.json"}`, 1),
		"endpoint on scripted": `{"version":"ai-provider-v1","kind":"scripted","endpoint":"https://a.example.com/","script_file":"/tmp/s.json","model":"m"}`,
		"scripted no file":     `{"version":"ai-provider-v1","kind":"scripted","model":"m"}`,
		"scripted relative":    `{"version":"ai-provider-v1","kind":"scripted","script_file":"s.json","model":"m"}`,
	}
	for name, content := range cases {
		if _, _, err := LoadProviderConfig(writeFile(t, "ai.json", content)); !errors.Is(err, ErrInvalidInput) {
			t.Errorf("%s: accepted (%v)", name, err)
		}
	}
	for name, path := range map[string]string{"missing": filepath.Join(t.TempDir(), "nope.json"), "directory": t.TempDir()} {
		if _, _, err := LoadProviderConfig(path); !errors.Is(err, ErrInvalidInput) {
			t.Errorf("%s: %v", name, err)
		}
	}
	big := writeFile(t, "big.json", validHTTPS[:len(validHTTPS)-1]+`,"model_notes":"`+strings.Repeat("a", MaxConfigBytes)+`"}`)
	if _, _, err := LoadProviderConfig(big); !errors.Is(err, ErrInvalidInput) {
		t.Errorf("oversized: %v", err)
	}
	// Errors never echo the path or the content.
	_, _, err := LoadProviderConfig(writeFile(t, "supersecret_name.json", `{"version":"ai-provider-v1","kind":"https-json","endpoint":"https://user:hunter2@x.example.com/","model":"m"}`))
	if err == nil || strings.Contains(err.Error(), "hunter2") || strings.Contains(err.Error(), "supersecret_name") {
		t.Fatalf("error leaks input: %v", err)
	}
}

func TestLoadProviderConfigScripted(t *testing.T) {
	script := writeFile(t, "script.json", `{"steps":[{"payload":"{}","usage":{"input_tokens":3,"output_tokens":4},"model":"m1"},{"error":"unavailable"},{"payload":{"a":1},"delay_ms":5}]}`)
	cfg, _, err := LoadProviderConfig(writeFile(t, "ai.json", `{"version":"ai-provider-v1","kind":"scripted","script_file":"`+script+`","model":"dev"}`))
	if err != nil || cfg.Kind != KindScripted || cfg.ScriptFile != script {
		t.Fatalf("%+v %v", cfg, err)
	}
	p, err := NewProvider(cfg)
	if err != nil {
		t.Fatal(err)
	}
	sp, ok := p.(*ScriptedProvider)
	if !ok {
		t.Fatalf("provider %T", p)
	}
	steps, err := LoadScript(script)
	if err != nil || len(steps) != 3 || string(steps[0].Payload) != "{}" || steps[0].Usage.OutputTokens != 4 || !errors.Is(steps[1].Err, ErrProviderUnavailable) || string(steps[2].Payload) != `{"a":1}` || steps[2].Delay != 5*time.Millisecond {
		t.Fatalf("steps %+v %v", steps, err)
	}
	_ = sp
	for name, content := range map[string]string{
		"no steps": `{"steps":[]}`, "unknown error": `{"steps":[{"error":"explode"}]}`, "bad delay": `{"steps":[{"payload":"x","delay_ms":-1}]}`,
		"long delay": `{"steps":[{"payload":"x","delay_ms":60001}]}`, "unknown field": `{"steps":[{"payload":"x","cost":1}]}`, "extra key": `{"steps":[{"payload":"x"}],"x":1}`,
		"duplicate": `{"steps":[],"steps":[]}`,
	} {
		if _, err := LoadScript(writeFile(t, "s.json", content)); !errors.Is(err, ErrInvalidInput) {
			t.Errorf("%s: %v", name, err)
		}
	}
	var many strings.Builder
	many.WriteString(`{"steps":[`)
	for i := 0; i <= MaxScriptSteps; i++ {
		if i > 0 {
			many.WriteString(",")
		}
		many.WriteString(`{"payload":"x"}`)
	}
	many.WriteString(`]}`)
	if _, err := LoadScript(writeFile(t, "many.json", many.String())); !errors.Is(err, ErrInvalidInput) {
		t.Errorf("too many steps: %v", err)
	}
}

func TestNewProviderRejectsUnknownKindsAndBadEndpoints(t *testing.T) {
	for _, cfg := range []ProviderConfig{
		{Kind: "openai"}, {}, {Kind: KindHTTPSJSON, Endpoint: "http://a.example.com/"}, {Kind: KindHTTPSJSON, Endpoint: "https://127.0.0.1/"},
		{Kind: KindHTTPSJSON, Endpoint: "https://a.example.com/", CredentialFile: "relative"}, {Kind: KindScripted, ScriptFile: "/nonexistent/script.json"},
	} {
		if p, err := NewProvider(cfg); err == nil || p != nil {
			t.Errorf("%+v: provider built (%v)", cfg, err)
		}
	}
	p, err := NewProvider(ProviderConfig{Kind: KindHTTPSJSON, Endpoint: "https://Models.Example.com:8443/v1/x", Model: "m"})
	if err != nil {
		t.Fatal(err)
	}
	h := p.(*HTTPJSONProvider)
	if h.host != "models.example.com" || h.port != 8443 || !strings.HasPrefix(h.url, "https://models.example.com:8443/") {
		t.Fatalf("host normalization: %s %d %s", h.host, h.port, h.url)
	}
}
