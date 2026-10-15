package tools

// Rank orders non-production modes by capability. The database enforces the
// same order (control.tool_mode_rank), so a worker can never send a more
// capable mode than a sandbox's server-owned ceiling.
func Rank(mode string) int {
	switch mode {
	case ModeBlock:
		return 0
	case ModeSimulate:
		return 1
	case ModeReadOnly:
		return 2
	case ModeReroute:
		return 3
	case ModeTestAccount:
		return 4
	}
	return 99
}

// EffectiveMode combines the tool's own declared non-production behavior with
// the sandbox ceiling ("" for ordinary preview executions). It is the most
// restrictive combination that the definition can actually honor; anything it
// cannot honor becomes BLOCK, so an unknown or unconfigured case never sends.
// LIVE is never returned: production activation belongs to M13.
func EffectiveMode(d Definition, ceiling string) string {
	declared := d.Sandbox.Mode
	if Rank(declared) > 4 {
		return ModeBlock
	}
	if ceiling == "" || ceiling == declared {
		return declared
	}
	if Rank(ceiling) > 4 {
		return ModeBlock
	}
	if Rank(declared) <= Rank(ceiling) {
		return declared
	}
	// The ceiling is stricter than the declared mode.
	switch ceiling {
	case ModeBlock:
		return ModeBlock
	case ModeSimulate:
		// A fixture exists only when the tool itself declared SIMULATE.
		return ModeBlock
	case ModeReadOnly:
		if d.SideEffect == "WRITE" {
			return ModeBlock
		}
		return ModeReadOnly
	case ModeReroute:
		// Declared TEST_ACCOUNT narrowed to REROUTE still uses the sandbox endpoint.
		return ModeReroute
	}
	return ModeBlock
}
