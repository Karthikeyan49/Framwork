package app

import "log/slog"

// operatorCredentials are the variables that only the controlled migration and provisioning processes may hold (AGENTS.md: keep
// operator credentials out of runtime subprocesses): the database superuser and the migrator, the Redis administrator and the event
// operator. The launcher (scripts/runtime-env.mjs) leaves them out of a backend whose command it recognises; a backend that is
// started in a way it does not recognise is handed the whole environment, so the process itself refuses to run with them. The
// launcher's list of spellings is then a convenience and the refusal is the control (independent review IR-07, the third time the
// list was found incomplete after M6R and F-04).
var operatorCredentials = []string{
	"MIGRATION_DATABASE_URL", "MIGRATOR_PASSWORD", "POSTGRES_PASSWORD",
	"REDIS_PASSWORD", "EVENT_OPERATOR_REDIS_URL", "EVENT_OPERATOR_PASSWORD",
}

// heldOperatorCredentials names, and never shows, the operator credentials that are set in an environment.
func heldOperatorCredentials(getenv func(string) string) []string {
	var held []string
	for _, name := range operatorCredentials {
		if getenv(name) != "" {
			held = append(held, name)
		}
	}
	return held
}

// refuseOperatorCredentials reports, and logs the names of, the operator credentials in the environment of a runtime process.
func refuseOperatorCredentials(getenv func(string) string, logger *slog.Logger) bool {
	held := heldOperatorCredentials(getenv)
	if len(held) == 0 {
		return false
	}
	logger.Error("operator credentials are present in the environment of a runtime process; start it with make api, make worker or make dispatcher, or with only its own configuration", "variables", held)
	return true
}
