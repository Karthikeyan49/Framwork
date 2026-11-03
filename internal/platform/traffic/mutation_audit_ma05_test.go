package traffic

import (
	"context"
	"testing"
	"time"
)

func TestRefillNeverExceedsTheBurst(t *testing.T) {
	now := time.Unix(1000, 0)
	g, err := New(Limits{PeerRate: 10, PeerBurst: 20, GlobalRate: 100, GlobalBurst: 200}, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	ctx, remote := context.Background(), "192.0.2.1:4000"
	if !g.Peer(ctx, remote).Allowed() { // 19 tokens left
		t.Fatal("the first request was refused")
	}
	now = now.Add(1500 * time.Millisecond) // +15 tokens: 34 without the ceiling, 20 with it
	allowed := 0
	for i := 0; i < 40; i++ {
		if g.Peer(ctx, remote).Allowed() {
			allowed++
		}
	}
	if allowed != 20 {
		t.Fatalf("%d requests were admitted in one burst, the burst is 20", allowed)
	}
}
