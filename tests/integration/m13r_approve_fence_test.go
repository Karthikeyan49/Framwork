//go:build integration

package integration

// Regression tests of finding F-05 of the independent re-verification (pairs S3, S4 and S5 of its release-pointer concurrency run):
// approve_release had neither the M13R-01 fence nor a lock on the active pointer, so an approval whose REPEATABLE READ transaction began
// before a committed rejection was recorded on the rejected hash, and one that began before an activation or a rollback moved the pointer
// was recorded on the superseded candidate. The approval could never move the pointer, but the port's own refusals were overtaken and
// the approvals that promotion shows and counts then included one made after a veto. Migration 00092 makes the port lock the pointer
// FOR SHARE and write the fence row, so the transaction with the older snapshot aborts with 40001.

import "testing"

// heldApproval starts an approval of hash by subject in a transaction that has taken its snapshot and waits to be released.
func (e *m13Env) heldApproval(subject, hash string) (release func() error) {
	ready, proceed, done := make(chan struct{}, 1), make(chan struct{}), make(chan error, 1)
	go func() {
		done <- e.m13rHold(subject, ready, proceed, `SELECT control.approve_release($1,$2)`, e.app, hash)
	}()
	<-ready
	return func() error { close(proceed); return <-done }
}

// S3: a rejection that commits after an approval transaction took its snapshot, and before it calls the port, must stop the approval.
func TestF05ApprovalOvertakenByACommittedRejection(t *testing.T) {
	e := m13Setup(t)
	m13RequireStack(t, e, true)
	e.twoActivations(t)
	third, _ := e.build("500", m13Relabel("Third release"), e.carol)
	finish := e.heldApproval(e.dave.subject, third)
	if _, err := e.rejectPort(e.f.bobSubject, third, "RISK_UNACCEPTABLE", "veto"); err != nil {
		t.Fatal(err)
	}
	aerr := finish()
	if aerr == nil {
		t.Errorf("F-05: an approval was recorded on a hash whose rejection had committed before the approval committed (%d approvals, %d rejections)", len(e.approvalsOf(third)), len(e.rejectionsOf(third)))
	}
	m13Refused(t, "approval overtaken by a committed rejection", aerr, "40001")
	if n := len(e.approvalsOf(third)); n != 0 {
		t.Errorf("%d approvals on the rejected candidate", n)
	}
	if n := len(e.rejectionsOf(third)); n != 1 {
		t.Errorf("%d rejections, want the one that committed", n)
	}
	// Repeated after the veto, the approval is refused with the documented message.
	_, err := e.approvePort(e.dave.subject, third)
	m13Refused(t, "approval of a rejected candidate", err, "55000")
}

// S4: an activation of another candidate that commits after the approval transaction took its snapshot leaves the candidate superseded.
func TestF05ApprovalOvertakenByAnActivationOfAnotherCandidate(t *testing.T) {
	e := m13Setup(t)
	m13RequireStack(t, e, true)
	_, second := e.twoActivations(t)
	fourth, _ := e.build("500", m13Relabel("Fourth release"), e.carol)
	fifth, _ := e.build("500", m13Relabel("Fifth release"), e.carol)
	m13Code(t, "approve the fourth", e.httpApprove(e.dave.token, fourth), 201)
	finish := e.heldApproval(e.dave.subject, fifth)
	if _, err := e.activatePort(e.erin.subject, fourth, second, 2); err != nil {
		t.Fatal(err)
	}
	aerr := finish()
	if aerr == nil {
		t.Errorf("F-05: an approval was recorded on a candidate whose base the pointer had already left (%d approvals)", len(e.approvalsOf(fifth)))
	}
	m13Refused(t, "approval overtaken by an activation", aerr, "40001")
	if n := len(e.approvalsOf(fifth)); n != 0 {
		t.Errorf("%d approvals on the superseded candidate", n)
	}
	if hash, generation, _ := e.activePointer(); hash != fourth || generation != 3 {
		t.Errorf("the pointer is %s at generation %d, want the fourth release at 3", hash, generation)
	}
	_, err := e.approvePort(e.dave.subject, fifth)
	m13Refused(t, "approval of a superseded candidate", err, "55000")
}

// S5: the same with a rollback that moves the pointer.
func TestF05ApprovalOvertakenByARollback(t *testing.T) {
	e := m13Setup(t)
	m13RequireStack(t, e, true)
	first, second := e.twoActivations(t)
	fifth, _ := e.build("500", m13Relabel("Fifth release"), e.carol)
	finish := e.heldApproval(e.dave.subject, fifth)
	if _, err := e.rollbackPort(e.erin.subject, first, second, 2, "", false); err != nil {
		t.Fatal(err)
	}
	aerr := finish()
	if aerr == nil {
		t.Errorf("F-05: an approval was recorded on a candidate whose base a rollback had already left (%d approvals)", len(e.approvalsOf(fifth)))
	}
	m13Refused(t, "approval overtaken by a rollback", aerr, "40001")
	if n := len(e.approvalsOf(fifth)); n != 0 {
		t.Errorf("%d approvals on the superseded candidate", n)
	}
}

// Controls: the fence is per hash and approvers do not conflict with each other, so honest concurrency is untouched.
func TestF05ApprovalsStillRunTogether(t *testing.T) {
	e := m13Setup(t)
	m13RequireStack(t, e, true)
	e.twoActivations(t)
	third, _ := e.build("500", m13Relabel("Third release"), e.carol)
	other, err := e.stageLevel("LOW")
	if err != nil {
		t.Fatal(err)
	}

	t.Run("a rejection of another candidate does not abort the approval", func(t *testing.T) {
		finish := e.heldApproval(e.dave.subject, third)
		if _, err := e.rejectPort(e.f.bobSubject, other, "RISK_UNACCEPTABLE", "veto of the other candidate"); err != nil {
			t.Fatal(err)
		}
		if err := finish(); err != nil {
			t.Fatalf("an unrelated rejection aborted the approval: %v", err)
		}
		if n := len(e.approvalsOf(third)); n != 1 {
			t.Fatalf("%d approvals, want 1", n)
		}
	})

	t.Run("approvers of a candidate whose fence row exists do not conflict", func(t *testing.T) {
		// dave's approval above created the fence row; erin's transaction is older than alice's approval and both go through.
		finish := e.heldApproval(e.erin.subject, third)
		if _, err := e.approvePort(e.f.aliceSubject, third); err != nil {
			t.Fatalf("a concurrent approval: %v", err)
		}
		if err := finish(); err != nil {
			t.Fatalf("an approval aborted by another approver of the same candidate: %v", err)
		}
		if n := len(e.approvalsOf(third)); n != 3 {
			t.Fatalf("%d approvals, want 3", n)
		}
	})

	t.Run("two approvals that start together both go through, even on a candidate nobody has touched", func(t *testing.T) {
		// Every candidate gets its fence row when it is staged, so the first approval does not create it and cannot make a veto abort.
		fresh, _ := e.build("500", m13Relabel("Fresh release"), e.carol)
		finish := e.heldApproval(e.dave.subject, fresh)
		if _, err := e.approvePort(e.erin.subject, fresh); err != nil {
			t.Fatal(err)
		}
		if err := finish(); err != nil {
			t.Fatalf("the later of two first approvals aborted: %v", err)
		}
		if n := len(e.approvalsOf(fresh)); n != 2 {
			t.Fatalf("%d approvals, want 2", n)
		}
	})

	t.Run("a veto racing the first approval of a candidate is never the one that aborts", func(t *testing.T) {
		fresh, _ := e.build("500", m13Relabel("Vetoed release"), e.carol)
		finish := e.heldApproval(e.dave.subject, fresh)
		recorded, err := e.rejectPort(e.f.bobSubject, fresh, "RISK_UNACCEPTABLE", "veto during the first approval")
		if err != nil || !recorded {
			t.Fatalf("the veto failed (%v, recorded %v): a veto must stay possible", err, recorded)
		}
		m13Refused(t, "the approval that started before the veto", finish(), "40001")
	})
}

// Every candidate has its fence row from the moment it exists (the AFTER INSERT trigger of 00092), and the ones that existed before
// the migration got theirs from its backfill.
func TestF05EveryCandidateHasItsFenceRow(t *testing.T) {
	e := m13Setup(t)
	m13RequireStack(t, e, true)
	e.twoActivations(t)
	e.build("500", m13Relabel("Third release"), e.carol)
	var candidates, fenced int
	if err := e.f.operator.QueryRow(`SELECT count(*), count(f.artifact_hash) FROM control.release_candidates c
 LEFT JOIN control.release_fences f ON f.tenant_id=c.tenant_id AND f.app_id=c.app_id AND f.artifact_hash=c.artifact_hash`).Scan(&candidates, &fenced); err != nil {
		t.Fatal(err)
	}
	if candidates < 3 || fenced != candidates {
		t.Fatalf("%d candidates, %d with a fence row", candidates, fenced)
	}
}

// The residual that the fix of F-05 names. The first activation of an application has no pointer row to lock, so an approval of ANOTHER first
// candidate whose transaction began before that activation does not abort (there is nothing for FOR SHARE to find, and a row created after a
// snapshot is invisible to it). Whatever the approval does, it cannot take effect: that candidate was built on no release, the pointer is now
// the first release, and activation refuses the candidate as stale.
func TestF05ApprovalOvertakenByAFirstActivationCannotTakeEffect(t *testing.T) {
	e := m13Setup(t)
	m13RequireStack(t, e, true)
	first, _ := e.build("500", nil, e.carol)
	other, _ := e.build("500", m13Relabel("Other first release"), e.carol)
	m13Code(t, "approve the first", e.httpApprove(e.dave.token, first), 201)
	finish := e.heldApproval(e.dave.subject, other) // its snapshot predates the activation, and there is no pointer row yet
	m13Code(t, "activate the first", e.httpActivate(e.erin.token, first, "", 0), 200)
	aerr := finish()
	t.Logf("the approval of the other first candidate: err=%v, %d approval(s) recorded", aerr, len(e.approvalsOf(other)))
	_, err := e.activatePort(e.erin.subject, other, "", 0)
	m13Refused(t, "activation of the other first candidate", err, "55000")
	if hash, generation, _ := e.activePointer(); hash != first || generation != 1 {
		t.Errorf("the pointer is %s at generation %d, want the first release at 1", hash, generation)
	}
	if n := len(e.approvalsOf(first)); n != 1 {
		t.Errorf("%d approvals on the activated release, want 1", n)
	}
}
