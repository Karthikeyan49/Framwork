package ai

import (
	"testing"
	"time"
)

// The provider phase may use the contract's whole default wall clock; configuration may lower it and never raise it.
func TestEffectiveWallClock(t *testing.T) {
	for name, tc := range map[string]struct{ in, want time.Duration }{
		"unset takes the default":       {0, DefaultWallClock},
		"negative takes the default":    {-time.Second, DefaultWallClock},
		"a lower value is kept":         {5 * time.Second, 5 * time.Second},
		"the default itself is kept":    {DefaultWallClock, DefaultWallClock},
		"a higher value is lowered":     {10 * time.Minute, DefaultWallClock},
		"one tick above is lowered":     {DefaultWallClock + time.Nanosecond, DefaultWallClock},
		"one millisecond is kept as is": {time.Millisecond, time.Millisecond},
	} {
		if got := (ServiceLimits{WallClock: tc.in}).Effective().WallClock; got != tc.want {
			t.Errorf("%s: %v, want %v", name, got, tc.want)
		}
	}
	if APIWallClockCeiling != DefaultWallClock {
		t.Fatal("the API ceiling is the contract's default wall clock")
	}
}
