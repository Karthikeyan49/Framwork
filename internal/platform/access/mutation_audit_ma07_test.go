package access // zz: kills F02, F11

import "testing"

func TestBodyBudgetReachesItsTotalExactlyAndTheDefaultShareFitsAMegabyte(t *testing.T) {
	b := NewBodyBudget(100, 60)
	if r := b.Hold("a").Add(60); r != "" {
		t.Fatalf("a share of exactly 60 refused: %q", r)
	}
	if r := b.Hold("b").Add(40); r != "" { // 60 + 40 == the total of 100
		t.Fatalf("the last 40 bytes of a total of 100 refused: %q", r)
	}
	if r := b.Hold("c").Add(1); r != bodyRefusedTotal {
		t.Fatalf("a byte over the total: %q", r)
	}
	if DefaultBodyBudgetPerUser <= 1<<20 || DefaultBodyBudgetBytes < DefaultBodyBudgetPerUser {
		t.Fatalf("defaults %d total, %d per identity", DefaultBodyBudgetBytes, DefaultBodyBudgetPerUser)
	}
}
