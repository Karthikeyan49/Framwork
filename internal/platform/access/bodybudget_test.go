package access

import (
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestBodyHoldBooksPerIdentityAndInAll(t *testing.T) {
	b := NewBodyBudget(10, 4)
	a1, a2, bb, c := b.Hold("a"), b.Hold("a"), b.Hold("b"), b.Hold("c")
	if refused := a1.Add(3); refused != "" {
		t.Fatal("first booking refused:", refused)
	}
	if refused := a2.Add(2); refused != bodyRefusedSubject {
		t.Fatalf("5 bytes for one identity with a share of 4: %q", refused)
	}
	if refused := a2.Add(1); refused != "" {
		t.Fatal("the share is exactly used up, not exceeded:", refused)
	}
	if refused := bb.Add(4); refused != "" {
		t.Fatal("another identity has its own share:", refused)
	}
	if refused := c.Add(3); refused != bodyRefusedTotal {
		t.Fatalf("8 booked of 10, 3 more must not fit: %q", refused)
	}
	if used, subjects := b.InFlight(); used != 8 || subjects != 2 {
		t.Fatalf("in flight %d bytes of %d identities (a refused booking books nothing)", used, subjects)
	}
	a1.Release()
	if refused := c.Add(3); refused != "" {
		t.Fatal("released bytes are available again:", refused)
	}
	a2.Release()
	bb.Release()
	if used, _ := b.InFlight(); used != 3 {
		t.Fatalf("only the last booking is left, got %d", used)
	}
}

func TestBodyHoldReleaseIsIdempotentAndForgetsIdentities(t *testing.T) {
	b := NewBodyBudget(100, 100)
	h, other := b.Hold("a"), b.Hold("b")
	h.Add(10)
	other.Add(10)
	h.Release()
	h.Release()
	h.Release()
	if used, subjects := b.InFlight(); used != 10 || subjects != 1 {
		t.Fatalf("a repeated release must not give back more than was booked: %d bytes, %d identities", used, subjects)
	}
	if refused := h.Add(5); refused != "" {
		t.Fatal("a released hold books nothing and refuses nothing:", refused)
	}
	if used, _ := b.InFlight(); used != 10 {
		t.Fatalf("a released hold must not book again, got %d", used)
	}
	other.Release()
	if used, subjects := b.InFlight(); used != 0 || subjects != 0 {
		t.Fatalf("nothing is left to remember: %d bytes, %d identities", used, subjects)
	}
}

func TestBodyHoldIgnoresEmptyReadsAndNeverAdmitsMoreThanItsShare(t *testing.T) {
	b := NewBodyBudget(10, 3)
	h := b.Hold("a")
	for _, n := range []int64{0, -5} {
		if refused := h.Add(n); refused != "" {
			t.Fatalf("Add(%d) refused: %q", n, refused)
		}
	}
	if used, subjects := b.InFlight(); used != 0 || subjects != 0 {
		t.Fatalf("an empty read booked %d bytes for %d identities", used, subjects)
	}
	if refused := b.Hold("z").Add(4); refused != bodyRefusedSubject {
		t.Fatalf("a read larger than the share is never admitted: %q", refused)
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
				h := b.Hold(subject)
				for j := 0; j < 3; j++ {
					if h.Add(int64(1+(i+j)%5)) != "" {
						break
					}
					if used, _ := b.InFlight(); used > 64 {
						t.Errorf("the total was exceeded: %d", used)
					}
				}
				h.Release()
			}
		}(g)
	}
	wg.Wait()
	if used, subjects := b.InFlight(); used != 0 || subjects != 0 {
		t.Fatalf("leaked bookings: %d bytes, %d identities", used, subjects)
	}
}

// F-06 of the independent review: booking the declared length let identities that never send a byte fill the budget. A body is booked
// as it arrives, so a connection that has sent nothing holds nothing.
func TestBookedBodyHoldsOnlyWhatHasArrived(t *testing.T) {
	b := NewBodyBudget(64, 32)
	hold := b.Hold("a")
	defer hold.Release()
	pr, pw := io.Pipe()
	booked := &bookedBody{src: pr, hold: hold}
	done := make(chan struct{})
	var got []byte
	var readErr error
	go func() { got, readErr = io.ReadAll(io.LimitReader(booked, 1<<20+1)); close(done) }()

	time.Sleep(50 * time.Millisecond) // the request announced a megabyte and has sent nothing
	if used, subjects := b.InFlight(); used != 0 || subjects != 0 {
		t.Fatalf("a request that sent no byte holds %d bytes for %d identities", used, subjects)
	}
	if _, err := pw.Write([]byte("hello")); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		if used, _ := b.InFlight(); used == 5 {
			break
		}
		if time.Now().After(deadline) {
			used, _ := b.InFlight()
			t.Fatalf("5 bytes arrived and %d are booked", used)
		}
		time.Sleep(5 * time.Millisecond)
	}
	_ = pw.Close()
	<-done
	if string(got) != "hello" || readErr != nil || booked.refused != "" {
		t.Fatalf("read %q, %v, refused %q", got, readErr, booked.refused)
	}
}

func TestBookedBodyStopsTheReadThatDoesNotFit(t *testing.T) {
	for name, c := range map[string]struct {
		budget  *BodyBudget
		scope   string
		bytesIn int
	}{
		"the share of the identity": {NewBodyBudget(100, 8), bodyRefusedSubject, 20},
		"the total":                 {NewBodyBudget(8, 100), bodyRefusedTotal, 20},
	} {
		t.Run(name, func(t *testing.T) {
			hold := c.budget.Hold("a")
			defer hold.Release()
			booked := &bookedBody{src: strings.NewReader(strings.Repeat("x", c.bytesIn)), hold: hold}
			got, err := io.ReadAll(booked)
			if !errors.Is(err, errBodyBudget) || booked.refused != c.scope {
				t.Fatalf("error %v, refused %q, want %q", err, booked.refused, c.scope)
			}
			if len(got) > 8 {
				t.Fatalf("%d bytes were handed on past a budget of 8", len(got))
			}
			if used, _ := c.budget.InFlight(); used != int64(len(got)) {
				t.Fatalf("%d bytes booked for %d bytes read: the bytes of the refused read must not stay booked", used, len(got))
			}
			hold.Release()
			if used, subjects := c.budget.InFlight(); used != 0 || subjects != 0 {
				t.Fatalf("after the release: %d bytes, %d identities", used, subjects)
			}
		})
	}
}

func TestBookedBodyPassesEndAndErrorsThrough(t *testing.T) {
	b := NewBodyBudget(100, 100)
	hold := b.Hold("a")
	defer hold.Release()
	cause := errors.New("connection reset")
	booked := &bookedBody{src: io.MultiReader(strings.NewReader("abc"), errReader{cause}), hold: hold}
	got, err := io.ReadAll(booked)
	if string(got) != "abc" || !errors.Is(err, cause) || booked.refused != "" {
		t.Fatalf("read %q, %v, refused %q", got, err, booked.refused)
	}
	if used, _ := b.InFlight(); used != 3 {
		t.Fatalf("booked %d, want 3", used)
	}
}

type errReader struct{ err error }

func (e errReader) Read([]byte) (int, error) { return 0, e.err }
