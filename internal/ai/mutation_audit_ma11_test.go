package ai

import "testing"

func TestEffectiveLimitsNeverRaiseACeiling(t *testing.T) {
	got := (ServiceLimits{MaxAttempts: MaxAttempts + 5, MaxOutputTokens: MaxOutputTokensCeiling + 1, InFlightPerTenant: MaxInFlightPerTenant + 1, DailyTokens: maxDBTokens + 1}).Effective()
	if got.MaxAttempts != MaxAttempts || got.MaxOutputTokens != MaxOutputTokensCeiling || got.InFlightPerTenant != MaxInFlightPerTenant || got.DailyTokens != maxDBTokens {
		t.Fatalf("a ceiling was raised: %+v", got)
	}
	low := (ServiceLimits{MaxAttempts: 1, MaxOutputTokens: 7, InFlightPerTenant: 1, DailyTokens: 9}).Effective()
	if low.MaxAttempts != 1 || low.MaxOutputTokens != 7 || low.InFlightPerTenant != 1 || low.DailyTokens != 9 {
		t.Fatalf("a lower value was not kept: %+v", low)
	}
	zero := (ServiceLimits{}).Effective()
	if zero.MaxAttempts != MaxAttempts || zero.MaxOutputTokens != DefaultMaxOutputTokens || zero.InFlightPerTenant != DefaultInFlightPerTenant || zero.DailyTokens != DefaultDailyToken {
		t.Fatalf("defaults: %+v", zero)
	}
}
