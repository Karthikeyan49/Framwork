package postgres

import (
	"context"
	"errors"
	"sync"
)

var ErrAdmissionFull = errors.New("database admission queue is full")

// Admission is a bounded FIFO gate owned by one API/pool. database/sql chooses
// waiting requests randomly; a saturated closed workload can repeatedly bypass
// old requests. This gate changes neither role checks nor database isolation.
type Admission struct {
	mu                      sync.Mutex
	limit, capacity, active int
	waiters                 []*admissionWaiter
}
type admissionWaiter struct {
	ready   chan struct{}
	granted bool
}

type AdmissionStats struct{ Active, Queued, Limit, Capacity int }

func (a *Admission) Stats() AdmissionStats {
	a.mu.Lock()
	defer a.mu.Unlock()
	return AdmissionStats{a.active, len(a.waiters), a.limit, a.capacity}
}

func NewAdmission(limit, capacity int) (*Admission, error) {
	if limit < 1 || capacity < 1 {
		return nil, errors.New("invalid database admission bounds")
	}
	return &Admission{limit: limit, capacity: capacity}, nil
}

func (a *Admission) Acquire(ctx context.Context) (func(), error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	a.mu.Lock()
	if a.active < a.limit && len(a.waiters) == 0 {
		a.active++
		a.mu.Unlock()
		return a.lease(), nil
	}
	if len(a.waiters) >= a.capacity {
		a.mu.Unlock()
		return nil, ErrAdmissionFull
	}
	w := &admissionWaiter{ready: make(chan struct{})}
	a.waiters = append(a.waiters, w)
	a.mu.Unlock()
	select {
	case <-w.ready:
		if err := ctx.Err(); err != nil {
			a.lease()()
			return nil, err
		}
		return a.lease(), nil
	case <-ctx.Done():
		a.mu.Lock()
		granted := w.granted
		if !granted {
			for i, queued := range a.waiters {
				if queued == w {
					a.waiters = append(a.waiters[:i], a.waiters[i+1:]...)
					break
				}
			}
		}
		a.mu.Unlock()
		if granted {
			a.lease()()
		}
		return nil, ctx.Err()
	}
}

func (a *Admission) lease() func() {
	var once sync.Once
	return func() {
		once.Do(func() {
			a.mu.Lock()
			defer a.mu.Unlock()
			if len(a.waiters) == 0 {
				a.active--
				return
			}
			w := a.waiters[0]
			a.waiters[0] = nil
			a.waiters = a.waiters[1:]
			w.granted = true
			close(w.ready)
		})
	}
}
