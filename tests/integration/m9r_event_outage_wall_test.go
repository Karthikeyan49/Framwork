//go:build integration && eventoutage

package integration

import (
	"testing"
	"time"
)

func TestR7bRealTenMinuteEventOutage(t *testing.T) {
	r7bRealOutage(t, 10*time.Minute)
}
