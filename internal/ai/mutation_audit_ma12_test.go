package ai

import "testing"

func TestReserveCoversEveryAttemptAndItsOutput(t *testing.T) {
	limits := func(attempts, output int) ServiceLimits {
		return ServiceLimits{MaxAttempts: attempts, MaxOutputTokens: output}
	}
	one := reserveFor(limits(1, 1000), 1000, 200, 500)
	small, large := reserveFor(limits(3, 1000), 1000, 200, 500), reserveFor(limits(3, 2000), 1000, 200, 500)
	if small != 3*one {
		t.Fatalf("three attempts reserve %d, one attempt reserves %d", small, one)
	}
	if large-small != 3*1000 {
		t.Fatalf("1000 more output tokens per attempt reserve %d more tokens, want 3000", large-small)
	}
	if got := reserveFor(limits(16, 1_000_000), MaxContextBytes, 0, 1<<20); got != maxReserve {
		t.Fatalf("a huge reservation is %d, the database ceiling is %d", got, maxReserve)
	}
}
