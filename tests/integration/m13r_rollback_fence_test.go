//go:build integration

package integration

// Regression tests of independent reviews IR-06 (merge review) and M14R-04 (M14 review): rollback_release is the third writer of the
// active pointer and did not write the M13R-01 fence, so a rollback whose REPEATABLE READ snapshot predated a committed rejection of
// its target moved the pointer onto the rejected hash. Migration 00091 makes it write the same row.

import "testing"

// twoActivations activates a first release, then a second over it, and returns both hashes (the pointer is second@2).
func (e *m13Env) twoActivations(t *testing.T) (first, second string) {
	t.Helper()
	first, _ = e.build("500", nil, e.carol)
	m13Code(t, "approve the first", e.httpApprove(e.dave.token, first), 201)
	m13Code(t, "activate the first", e.httpActivate(e.erin.token, first, "", 0), 200)
	second, _ = e.build("500", m13Relabel("Second release"), e.carol)
	m13Code(t, "approve the second", e.httpApprove(e.dave.token, second), 201)
	m13Code(t, "activate the second", e.httpActivate(e.erin.token, second, first, 1), 200)
	if hash, generation, _ := e.activePointer(); hash != second || generation != 2 {
		t.Fatalf("set-up: the pointer is %s at generation %d", hash, generation)
	}
	return first, second
}

// A rejection that commits after a rollback transaction took its snapshot, and before it calls the port, must stop the rollback.
func TestM13R01RollbackOvertakenByACommittedRejection(t *testing.T) {
	e := m13Setup(t)
	m13RequireStack(t, e, true)
	first, second := e.twoActivations(t)
	ready, proceed, done := make(chan struct{}, 1), make(chan struct{}), make(chan error, 1)
	go func() {
		done <- e.m13rHold(e.erin.subject, ready, proceed, `SELECT control.rollback_release($1,$2,$3,$4,$5,$6)`, e.app, first, second, int64(2), "", false)
	}()
	<-ready // the rollback transaction exists; its snapshot predates the rejection
	if _, err := e.rejectPort(e.f.bobSubject, first, "RISK_UNACCEPTABLE", "veto"); err != nil {
		t.Fatal(err)
	}
	close(proceed)
	rerr := <-done
	hash, generation, _ := e.activePointer()
	if rerr == nil && hash == first {
		t.Errorf("IR-06: the pointer moved onto a hash whose rejection had committed before the rollback committed (rejections=%d)", len(e.rejectionsOf(first)))
	}
	m13Refused(t, "rollback overtaken by a committed rejection", rerr, "40001")
	if hash != second || generation != 2 {
		t.Errorf("the pointer must stay on the second release at generation 2, got %s at %d", hash, generation)
	}
}

// The fence is per hash: a rejection of ANOTHER candidate does not abort the rollback.
func TestM13R01RejectionOfAnotherHashDoesNotBlockTheRollback(t *testing.T) {
	e := m13Setup(t)
	m13RequireStack(t, e, true)
	first, second := e.twoActivations(t)
	other, err := e.stageLevel("LOW")
	if err != nil {
		t.Fatal(err)
	}
	ready, proceed, done := make(chan struct{}, 1), make(chan struct{}), make(chan error, 1)
	go func() {
		done <- e.m13rHold(e.erin.subject, ready, proceed, `SELECT control.rollback_release($1,$2,$3,$4,$5,$6)`, e.app, first, second, int64(2), "", false)
	}()
	<-ready
	if _, err = e.rejectPort(e.f.bobSubject, other, "RISK_UNACCEPTABLE", "veto of the other candidate"); err != nil {
		t.Fatal(err)
	}
	close(proceed)
	if err = <-done; err != nil {
		t.Fatalf("an unrelated rejection aborted the rollback: %v", err)
	}
	if hash, generation, _ := e.activePointer(); hash != first || generation != 3 {
		t.Errorf("the pointer is %s at generation %d, want the first release at generation 3", hash, generation)
	}
}

// Control without a race: a target that was rejected before the rollback began is refused with the documented message.
func TestM13R01RollbackToAnAlreadyRejectedTargetIsRefused(t *testing.T) {
	e := m13Setup(t)
	m13RequireStack(t, e, true)
	first, second := e.twoActivations(t)
	if _, err := e.rejectPort(e.f.bobSubject, first, "RISK_UNACCEPTABLE", "veto after it ran"); err != nil {
		t.Fatal(err)
	}
	_, err := e.rollbackPort(e.erin.subject, first, second, 2, "", false)
	m13Refused(t, "rollback onto a rejected release", err, "55000")
	if hash, _, _ := e.activePointer(); hash != second {
		t.Errorf("the pointer moved to %s", hash)
	}
}
