//go:build integration

package integration

import (
	"context"
	"encoding/binary"
	"errors"
	"github.com/Karthikeyan49/Framwork/internal/events"
	"github.com/Karthikeyan49/Framwork/internal/platform/postgres"
	"io"
	"net"
	"net/url"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

// This local test proxy forwards protocol frames unchanged. It discards the
// consumer transaction's successful COMMIT response AFTER PostgreSQL emits it,
// reproducing a committed effect with an unknown result to the caller.
func m8DropCommitResponse(t *testing.T, raw string) (string, *atomic.Bool) {
	t.Helper()
	u, e := url.Parse(raw)
	if e != nil || u.Query().Get("sslmode") != "disable" {
		t.Fatal("local plaintext-only test fixture required")
	}
	listener, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	var connections sync.Map
	backendAddress := u.Host
	acceptDone := make(chan struct{})
	var commits atomic.Int64
	observed := &atomic.Bool{}
	var wg sync.WaitGroup
	t.Cleanup(func() {
		listener.Close()
		<-acceptDone
		connections.Range(func(k, v any) bool { _ = k.(net.Conn).Close(); return true })
		wg.Wait()
	})
	go func() {
		defer close(acceptDone)
		for {
			client, e := listener.Accept()
			if e != nil {
				return
			}
			backend, e := net.Dial("tcp", backendAddress)
			if e != nil {
				client.Close()
				continue
			}
			connections.Store(client, true)
			connections.Store(backend, true)
			var drop atomic.Bool
			wg.Add(2)
			go func() {
				defer wg.Done()
				defer client.Close()
				defer backend.Close()
				head := make([]byte, 4)
				if _, e := io.ReadFull(client, head); e != nil {
					return
				}
				length := int(binary.BigEndian.Uint32(head))
				if length < 8 || length > 65536 {
					return
				}
				body := make([]byte, length-4)
				if _, e = io.ReadFull(client, body); e != nil {
					return
				}
				if _, e = backend.Write(append(head, body...)); e != nil {
					return
				}
				for {
					frame, e := m8Frame(client)
					if e != nil {
						return
					}
					if frame[0] == 'Q' && strings.EqualFold(string(frame[5:]), "COMMIT\x00") && commits.Add(1) == 2 {
						drop.Store(true)
					}
					if _, e = backend.Write(frame); e != nil {
						return
					}
				}
			}()
			go func() {
				defer wg.Done()
				defer client.Close()
				defer backend.Close()
				for {
					frame, e := m8Frame(backend)
					if e != nil {
						return
					}
					if drop.Load() && frame[0] == 'C' && string(frame[5:]) == "COMMIT\x00" {
						observed.Store(true)
						return
					}
					if _, e = client.Write(frame); e != nil {
						return
					}
				}
			}()
		}
	}()
	u.Host = listener.Addr().String()
	return u.String(), observed
}
func m8Frame(r io.Reader) ([]byte, error) {
	head := make([]byte, 5)
	if _, e := io.ReadFull(r, head); e != nil {
		return nil, e
	}
	n := int(binary.BigEndian.Uint32(head[1:]))
	if n < 4 || n > 4<<20 {
		return nil, io.ErrUnexpectedEOF
	}
	body := make([]byte, n-4)
	if _, e := io.ReadFull(r, body); e != nil {
		return nil, e
	}
	return append(head, body...), nil
}
func TestM8UnknownCommitDoesNotAckAndRedeliveryDeduplicates(t *testing.T) {
	m8Database(t)
	f, h, base, token, _ := m5Fixture(t)
	m5Create(t, h, base, token, "unknown commit")
	d, w := m8Roles(t)
	dispatch := events.Dispatcher{DB: d}
	p := m8Claim(t, dispatch)
	if e := dispatch.Finish(context.Background(), p, true); e != nil {
		t.Fatal(e)
	}
	proxy, observed := m8DropCommitResponse(t, os.Getenv("WORKER_DATABASE_URL"))
	worker, e := postgres.Open(proxy)
	if e != nil {
		t.Fatal(e)
	}
	defer worker.Close()
	raw, _ := p.Bytes()
	transport := &m8AckTransport{}
	err := m8Consumer(worker).Handle(context.Background(), transport, events.Message{ID: "1-0", Body: raw})
	if !observed.Load() || !errors.Is(err, postgres.ErrCommitUnknown) || transport.ack != 0 {
		t.Fatalf("unknown commit boundary observed=%v unknown=%v ack=%d", observed.Load(), errors.Is(err, postgres.ErrCommitUnknown), transport.ack)
	}
	m8AssertCounts(t, f.operator, f.tenantA, 1)
	if e := m8Consumer(w).Handle(context.Background(), transport, events.Message{ID: "1-0", Body: raw}); e != nil {
		t.Fatal(e)
	}
	if transport.ack != 1 {
		t.Fatal("confirmed receipt did not ack")
	}
	m8AssertCounts(t, f.operator, f.tenantA, 1)
}
