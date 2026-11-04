package ai

// Additional ceilings and defaults of ADR104 (amendment A1). As with the constants
// of contract.go, configuration may lower a ceiling, never raise it.
const (
	// DefaultMaxOutputTokens is used when neither the budget nor the provider
	// configuration names an output limit; MaxOutputTokensCeiling is the hard cap
	// (MaxOutputBytes at the pessimistic four bytes per token).
	DefaultMaxOutputTokens = 8192
	MaxOutputTokensCeiling = MaxOutputBytes / 4

	DefaultInFlightPerTenant = 2
	MaxInFlightPerTenant     = 8

	// MaxConfigBytes bounds an ai-provider-v1 or script file.
	MaxConfigBytes = 64 << 10
	// MaxModelLabelRunes bounds the operator model label and a provider-reported one.
	MaxModelLabelRunes = 128
	// MaxScriptSteps bounds a scripted-provider script file.
	MaxScriptSteps = 256
)

// DefaultEvalBudget is the budget Evaluate gives a case that names none.
func DefaultEvalBudget() Budget {
	return Budget{MaxAttempts: MaxAttempts, MaxOutputTokens: DefaultMaxOutputTokens, WallClock: DefaultWallClock, TokensRemaining: DefaultDailyToken}
}
