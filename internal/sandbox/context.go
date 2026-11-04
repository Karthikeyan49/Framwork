// Package sandbox defines isolation admissions, not the M11 sandbox manager.
package sandbox

import "errors"

type Context struct {
	ID, DataMode, ToolMode       string
	ProductionCredentialsMounted bool
}

func (c Context) Validate() error {
	if c.ID == "" || c.ProductionCredentialsMounted {
		return errors.New("unsafe sandbox context")
	}
	switch c.DataMode {
	case "SYNTHETIC_SEPARATE", "REDACTED_SNAPSHOT", "DEDICATED_DATABASE":
	default:
		return errors.New("unknown sandbox data mode")
	}
	switch c.ToolMode {
	case "BLOCK", "SIMULATE", "REROUTE", "READ_ONLY", "TEST_ACCOUNT":
	default:
		return errors.New("unknown sandbox tool mode")
	}
	return nil
}
