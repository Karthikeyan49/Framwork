package ai // zz: kills A20

import "testing"

func TestEstimateTokensRoundsUp(t *testing.T) {
	for bytes, want := range map[int]int64{0: 0, 1: 1, 3: 1, 4: 1, 5: 2, 8: 2, 9: 3} {
		if got := estimateTokens(bytes); got != want {
			t.Errorf("estimateTokens(%d) = %d, want %d", bytes, got, want)
		}
	}
}
