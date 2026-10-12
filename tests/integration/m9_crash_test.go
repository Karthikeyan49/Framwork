//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/Karthikeyan49/Framwork/internal/audit"
	"github.com/Karthikeyan49/Framwork/internal/events"
	"github.com/Karthikeyan49/Framwork/internal/identity"
	"github.com/Karthikeyan49/Framwork/internal/platform/access"
	"github.com/Karthikeyan49/Framwork/internal/platform/postgres"
	"github.com/Karthikeyan49/Framwork/internal/policy"
	"github.com/Karthikeyan49/Framwork/internal/runtime/workflow"
	"github.com/Karthikeyan49/Framwork/internal/tenant"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestM9UnknownCommitRetainsReceiptsAndNeverRepeatsLocalEffect(t *testing.T) {
	m := m9Setup(t, nil)
	x := m.start(t, "unknown-commit")
	m.step(t)
	raw, observed := m8DropCommitResponse(t, os.Getenv("WORKER_DATABASE_URL"))
	db, e := postgres.Open(raw)
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	w := m.worker
	w.DB = db
	lease, found, e := w.Claim(context.Background())
	if e != nil || !found {
		t.Fatal("proxy claim", e)
	}
	if e = w.Step(context.Background(), lease); !errors.Is(e, postgres.ErrCommitUnknown) || !observed.Load() {
		t.Fatal("missing actual lost COMMIT response", e)
	}
	x = m.get(t, x.ID)
	m.counts(t, x, 1)
	if e = m.worker.Step(context.Background(), lease); !errors.Is(e, workflow.ErrLease) {
		t.Fatal("unknown commit reran stale ticket", e)
	}
	x = m.until(t, x.ID, "WAITING_APPROVAL")
	m.counts(t, x, 1)
}
func TestM9ActualProcessKillBeforeAndAfterLocalCommit(t *testing.T) {
	for _, point := range []string{"before_commit", "after_commit"} {
		t.Run(point, func(t *testing.T) {
			m := m9Setup(t, nil)
			x := m.start(t, "process-crash")
			m.step(t)
			exe, e := os.Executable()
			if e != nil {
				t.Fatal(e)
			}
			barrier := filepath.Join(t.TempDir(), "barrier")
			cmd := exec.Command(exe, "-test.run=^TestM9ProcessChild$", "-test.count=1")
			cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "APP_ENV=test", "WORKER_DATABASE_URL=" + os.Getenv("WORKER_DATABASE_URL"), "M9_CHILD_POINT=" + point, "M9_CHILD_BARRIER=" + barrier}
			cmd.Stdout = io.Discard
			cmd.Stderr = io.Discard
			if e = cmd.Start(); e != nil {
				t.Fatal(e)
			}
			t.Cleanup(func() { _ = cmd.Process.Kill() })
			done := make(chan error, 1)
			go func() { done <- cmd.Wait() }()
			deadline := time.Now().Add(10 * time.Second)
			ready := false
			for time.Now().Before(deadline) {
				if _, e = os.Stat(barrier); e == nil {
					ready = true
					break
				}
				select {
				case e := <-done:
					t.Fatal("child exited before crash barrier", e)
				default:
				}
				time.Sleep(10 * time.Millisecond)
			}
			if !ready {
				t.Fatal("crash barrier timeout")
			}
			if e = cmd.Process.Kill(); e != nil {
				t.Fatal(e)
			}
			select {
			case e = <-done:
				if e == nil {
					t.Fatal("child was not killed")
				}
			case <-time.After(3 * time.Second):
				t.Fatal("killed child did not exit")
			}
			after := m.get(t, x.ID)
			want := 0
			if point == "after_commit" {
				want = 1
			}
			m.counts(t, after, want)
			// Await actual5s lease expiry after uncommitted process death. No fixture
			// clock rewrite is used for this crash-recovery test.
			after = m.until(t, x.ID, "WAITING_APPROVAL")
			m.counts(t, after, 1)
			if point == "before_commit" {
				var n int
				if e = m.f.operator.QueryRow(`SELECT count(*) FROM control.workflow_attempts WHERE tenant_id=$1 AND id=$2 AND phase='EXPIRED'`, m.f.tenantA, x.ID).Scan(&n); e != nil || n != 1 {
					t.Fatal("missing lease expiry evidence", n, e)
				}
			}
		})
	}
}

// A real child process pauses after a SQL effect, either before the transaction
// commits or after Step confirms commit. Parent sends SIGKILL at that exact barrier.
func TestM9ProcessChild(t *testing.T) {
	point := os.Getenv("M9_CHILD_POINT")
	if point == "" {
		return
	}
	if point != "before_commit" && point != "after_commit" {
		t.Fatal("child fixture contract")
	}
	db, e := postgres.Open(os.Getenv("WORKER_DATABASE_URL"))
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	store := access.WorkflowStore(policy.Native{}, audit.Postgres{}, events.Postgres{}, func(ctx context.Context, q postgres.Executor, a tenant.Access) (identity.User, error) {
		var id string
		if e := q.QueryRowContext(ctx, `SELECT current_setting('app.job_id',true)`).Scan(&id); e != nil {
			return identity.User{}, e
		}
		return (identity.Store{}).ResolveWorker(ctx, q, id)
	})
	barrier := func() {
		if e := os.WriteFile(os.Getenv("M9_CHILD_BARRIER"), []byte("ready"), 0600); e != nil {
			t.Fatal(e)
		}
		select {}
	}
	store.Activity = func(ctx context.Context, q postgres.Executor, a tenant.Access, r workflow.ActivityRequest) (json.RawMessage, error) {
		if _, e := q.ExecContext(ctx, `INSERT INTO runtime.m9_test_effects(tenant_id,instance_id,step_key) VALUES($1,$2,$3)`, a.TenantID(), r.InstanceID, r.StepKey); e != nil {
			return nil, e
		}
		if point == "before_commit" {
			barrier()
		}
		return workflow.Noop(ctx, q, a, r)
	}
	w := workflow.Worker{DB: db, Store: store}
	lease, found, e := w.Claim(context.Background())
	if e != nil || !found {
		t.Fatal(e)
	}
	if e = w.Step(context.Background(), lease); e != nil {
		t.Fatal(e)
	}
	if point == "after_commit" {
		barrier()
	}
}
