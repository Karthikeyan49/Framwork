package access

import (
	"sync"
	"testing"
)

func TestBodyBudgetBooksPerIdentityAndInAll(t *testing.T) {
	b := NewBodyBudget(10, 4)
	ra, refused := b.Reserve("a", 3)
	if refused != "" {
		t.Fatal("first booking refused:", refused)
	}
	if _, refused = b.Reserve("a", 2); refused != bodyRefusedSubject {
		t.Fatalf("5 bytes for one identity with a share of 4: %q", refused)
	}
	ra2, refused := b.Reserve("a", 1)
	if refused != "" {
		t.Fatal("the share is exactly used up, not exceeded:", refused)
	}
	rb, refused := b.Reserve("b", 4)
	if refused != "" {
		t.Fatal("another identity has its own share:", refused)
	}
	if _, refused = b.Reserve("c", 3); refused != bodyRefusedTotal {
		t.Fatalf("8 booked of 10, 3 more must not fit: %q", refused)
	}
	if used, subjects := b.InFlight(); used != 8 || subjects != 2 {
		t.Fatalf("in flight %d bytes of %d identities", used, subjects)
	}
	ra()
	if _, refused = b.Reserve("c", 3); refused != "" {
		t.Fatal("released bytes are available again:", refused)
	}
	ra2()
	rb()
	if used, _ := b.InFlight(); used != 3 {
		t.Fatalf("only the last booking is left, got %d", used)
	}
}

func TestBodyBudgetReleaseIsIdempotentAndForgetsIdentities(t *testing.T) {
	b := NewBodyBudget(100, 100)
	release, _ := b.Reserve("a", 10)
	other, _ := b.Reserve("b", 10)
	release()
	release()
	release()
	if used, subjects := b.InFlight(); used != 10 || subjects != 1 {
		t.Fatalf("a repeated release must not give back more than was booked: %d bytes, %d identities", used, subjects)
	}
	other()
	if used, subjects := b.InFlight(); used != 0 || subjects != 0 {
		t.Fatalf("nothing is left to remember: %d bytes, %d identities", used, subjects)
	}
}

func TestBodyBudgetBooksAtLeastOneByteAndNeverMoreThanItsShare(t *testing.T) {
	b := NewBodyBudget(10, 3)
	for i := 0; i < 3; i++ {
		if _, refused := b.Reserve("a", 0); refused != "" {
			t.Fatalf("booking %d of an empty body refused: %q", i, refused)
		}
	}
	if _, refused := b.Reserve("a", -5); refused != bodyRefusedSubject {
		t.Fatalf("a booking counts one byte at least, so the fourth is over a share of 3: %q", refused)
	}
	if _, refused := b.Reserve("z", 4); refused != bodyRefusedSubject {
		t.Fatalf("a request larger than the share is never admitted: %q", refused)
	}
}

func TestBodyBudgetIsSafeUnderConcurrency(t *testing.T) {
	b := NewBodyBudget(64, 16)
	var wg sync.WaitGroup
	for g := 0; g < 32; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			subject := string(rune('a' + g%4))
			for i := 0; i < 500; i++ {
				release, refused := b.Reserve(subject, int64(1+i%5))
				if refused != "" {
					continue
				}
				if used, _ := b.InFlight(); used > 64 {
					t.Errorf("the total was exceeded: %d", used)
				}
				release()
			}
		}(g)
	}
	wg.Wait()
	if used, subjects := b.InFlight(); used != 0 || subjects != 0 {
		t.Fatalf("leaked bookings: %d bytes, %d identities", used, subjects)
	}
}

func TestReservationIsTheDeclaredLengthUpToTheCap(t *testing.T) {
	const limit = 1<<20 + 1
	for _, c := range []struct{ declared, want int64 }{
		{0, 1}, {1, 1}, {1000, 1000}, {1 << 20, 1 << 20}, {1<<20 + 1, limit}, {5 << 20, limit}, {-1, limit},
	} {
		if got := reservation(c.declared, limit); got != c.want {
			t.Errorf("declared %d: booked %d, want %d", c.declared, got, c.want)
		}
	}
	if got := reservation(100000, 64<<10+1); got != 64<<10+1 {
		t.Errorf("a command body is capped at its own limit, booked %d", got)
	}
}
