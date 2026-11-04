package postgres

import (
	"context"
	"errors"
	"testing"
	"time"
)

func awaitQueue(t *testing.T, a *Admission, count int) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		a.mu.Lock()
		n := len(a.waiters)
		a.mu.Unlock()
		if n == count {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("admission queue did not reach expected size")
}
func TestAdmissionIsFIFOAndCancellationDoesNotLoseCapacity(t *testing.T) {
	a, err := NewAdmission(1, 3)
	if err != nil {
		t.Fatal(err)
	}
	first, err := a.Acquire(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	type accepted struct {
		id      int
		release func()
		err     error
	}
	results := make(chan accepted, 3)
	canceled, cancel := context.WithCancel(context.Background())
	for i := range 3 {
		ctx := context.Background()
		if i == 1 {
			ctx = canceled
		}
		go func() { release, err := a.Acquire(ctx); results <- accepted{i, release, err} }()
		awaitQueue(t, a, i+1)
	}
	if _, err := a.Acquire(context.Background()); !errors.Is(err, ErrAdmissionFull) {
		t.Fatal("queue bound not enforced")
	}
	cancel()
	if r := <-results; r.id != 1 || !errors.Is(r.err, context.Canceled) {
		t.Fatal("queued cancellation was not removed")
	}
	first()
	first() // A repeated defer/release cannot create an extra permit.
	for _, id := range []int{0, 2} {
		select {
		case r := <-results:
			if r.err != nil || r.id != id {
				t.Fatal("admission bypassed an older request", r.id, id)
			}
			r.release()
		case <-time.After(time.Second):
			t.Fatal("admission permit leaked")
		}
	}
	ctx, stop := context.WithTimeout(context.Background(), time.Second)
	defer stop()
	release, err := a.Acquire(ctx)
	if err != nil {
		t.Fatal("capacity was not restored")
	}
	release()
}

func TestAdmissionSimultaneousGrantAndCancellationRestoresCapacity(t *testing.T) {
	for range 50 {
		a, _ := NewAdmission(1, 1)
		holder, _ := a.Acquire(context.Background())
		ctx, cancel := context.WithCancel(context.Background())
		finished := make(chan struct{})
		go func() {
			release, err := a.Acquire(ctx)
			if err == nil {
				release()
			}
			close(finished)
		}()
		awaitQueue(t, a, 1)
		released := make(chan struct{})
		go func() { holder(); close(released) }()
		cancel()
		<-finished
		<-released
		ctx, stop := context.WithTimeout(context.Background(), time.Second)
		release, err := a.Acquire(ctx)
		stop()
		if err != nil {
			t.Fatal("grant/cancellation leaked a permit")
		}
		release()
		if stats := a.Stats(); stats.Active != 0 || stats.Queued != 0 {
			t.Fatal("capacity not restored", stats)
		}
	}
}
