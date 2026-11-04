//go:build integration

package integration

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/Karthikeyan49/Framwork/internal/audit"
	"github.com/Karthikeyan49/Framwork/internal/platform/access"
	"github.com/Karthikeyan49/Framwork/internal/platform/httpserver"
	"github.com/Karthikeyan49/Framwork/internal/platform/postgres"
	"github.com/Karthikeyan49/Framwork/internal/policy"
	"github.com/Karthikeyan49/Framwork/internal/runtime/data"
)

func TestM9RQueuedCommandRechecksLiveMembershipBeforeMutation(t *testing.T) {
	m8Database(t)
	f, _, base, token, app := m5Fixture(t)
	gate, _ := postgres.NewAdmission(1, 8)
	held, err := gate.Acquire(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer held()
	a := access.API{DB: f.db, Admission: gate, Auth: f.verifier, Policy: policy.Native{}, Audit: audit.Postgres{}, Registry: m3Registry(t), PreviewEnabled: true}
	h := httpserver.Handler(httpserver.Options{Logger: slog.New(slog.NewJSONHandler(io.Discard, nil)), Register: a.Register, AllowPOST: a.AcceptsPOST})
	result := make(chan int, 1)
	command := data.Command{Key: uuid(t), Operation: "create", Values: m5Values("Revoked while queued")}
	go func() { result <- m5Call(h, base+"/commands", token, command).Code }()
	deadline := time.Now().Add(time.Second)
	for gate.Stats().Queued != 1 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if gate.Stats().Queued != 1 {
		t.Fatal("command did not queue before authorization")
	}
	if _, err := f.operator.Exec(`UPDATE iam.memberships SET active=false WHERE tenant_id=$1 AND user_id=$2`, f.tenantA, f.alice); err != nil {
		t.Fatal(err)
	}
	held()
	select {
	case code := <-result:
		if code != 403 {
			t.Fatal("queued revoked command", code)
		}
	case <-time.After(4 * time.Second):
		t.Fatal("queued command did not finish")
	}
	m9rCommandCounts(t, f, app, 0, 0, 0)
	if stats := gate.Stats(); stats.Active != 0 || stats.Queued != 0 {
		t.Fatal("rejected command leaked admission capacity", stats)
	}
}
