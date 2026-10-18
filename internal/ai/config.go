package ai

import "time"

// Provider kinds of ai-provider-v1.
const (
	KindHTTPSJSON = "https-json"
	KindScripted  = "scripted" // development and tests only; the composition root refuses it elsewhere
)

// ProviderConfig is the validated ai-provider-v1 configuration (AI_PROVIDER_FILE).
// Every limit is already clamped to the contract ceilings.
type ProviderConfig struct {
	Version, Kind, Endpoint, Model, CredentialFile, ScriptFile string
	MaxAttempts, MaxOutputTokens, InFlightPerTenant            int
	WallClock                                                  time.Duration
	DailyTokenBudget                                           int64
}

// LoadProviderConfig strictly loads an ai-provider-v1 file and returns the SHA-256
// (hex) of its bytes for the audit trail.
func LoadProviderConfig(path string) (ProviderConfig, string, error) {
	return ProviderConfig{}, "", ErrNotImplemented
}

// NewProvider builds the adapter a configuration names.
func NewProvider(cfg ProviderConfig) (Provider, error) { return nil, ErrNotImplemented }

// Budget derives the pipeline budget of one run from the configuration and the
// tenant's remaining daily token budget.
func (c ProviderConfig) Budget(tokensRemaining int64) Budget {
	return Budget{MaxAttempts: c.MaxAttempts, MaxOutputTokens: c.MaxOutputTokens, WallClock: c.WallClock, TokensRemaining: tokensRemaining}
}
