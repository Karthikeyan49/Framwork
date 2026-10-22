//go:build integration

package integration

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Karthikeyan49/Framwork/internal/platform/access"
	"github.com/Karthikeyan49/Framwork/internal/platform/httpserver"
)

// The body pre-read (independent review IR-02) moved the read of a POST body in front of the database slot. The review of that fix
// (F-01) measured what it cost: any verified token made the API hold 1.14 MiB per connection, 1,000 connections 1.15 GiB, whatever
// the route accepts (the AI routes take 4 to 128 KiB). The budget that bounds it books the bytes that have ARRIVED: the first version
// booked the declared Content-Length, and the next review (F-06) showed eight identities without a membership filling the whole
// budget with connections that declared a body and sent none. These tests run the real server (httpserver.New, production timeouts).

// m14Serve starts the production server around the fixture's handler and returns its address.
func m14Serve(t *testing.T, e *m14Env) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := httpserver.New(listener.Addr().String(), e.h)
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(func() { _ = server.Close() })
	return listener.Addr().String()
}

// m14Head is the header block of a POST that declares declared bytes of body.
func m14Head(addr, path, token string, declared int) string {
	auth := ""
	if token != "" {
		auth = "Authorization: Bearer " + token + "\r\n"
	}
	return fmt.Sprintf("POST %s HTTP/1.1\r\nHost: %s\r\n%sContent-Type: application/json\r\nContent-Length: %d\r\n\r\n", path, addr, auth, declared)
}

// m14Answer reads the response on conn within wait: its status and the "code" of an error body, or 0 when nothing came.
func m14Answer(conn net.Conn, wait time.Duration) (status int, code string, retryAfter string) {
	_ = conn.SetReadDeadline(time.Now().Add(wait))
	resp, err := http.ReadResponse(bufio.NewReader(conn), nil)
	if err != nil {
		return 0, "", ""
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	var v struct {
		Code string `json:"code"`
	}
	_ = json.Unmarshal(raw, &v)
	return resp.StatusCode, v.Code, resp.Header.Get("Retry-After")
}

// m14RawPost sends one POST that announces declared bytes, sends send of them and waits for the answer.
func m14RawPost(t *testing.T, addr, path, token string, declared int, send []byte, wait time.Duration) (status int, code string, took time.Duration) {
	t.Helper()
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	started := time.Now()
	if _, err = conn.Write(append([]byte(m14Head(addr, path, token, declared)), send...)); err != nil {
		t.Fatal(err)
	}
	status, code, _ = m14Answer(conn, wait)
	return status, code, time.Since(started)
}

func heapInUse() uint64 {
	runtime.GC()
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	return m.HeapInuse
}

// m14Flood opens n connections of one token one after the other, spacing apart, that announce 1 MiB and send chunk of it, then
// stall. Opening them in sequence makes the count of admitted connections exact (a flood that arrives at once is admitted in an order
// nobody controls). It reads what each connection is told within 1.5 s: refused counts the answers by status, waiting the
// connections that were admitted and are being held.
type m14FloodResult struct {
	conns   []net.Conn
	refused map[int]int
	waiting int
}

func m14Flood(t *testing.T, addr, path, token string, n int, chunk []byte, spacing time.Duration) *m14FloodResult {
	t.Helper()
	f := &m14FloodResult{refused: map[int]int{}}
	head := []byte(m14Head(addr, path, token, 1<<20))
	t.Cleanup(f.close)
	for i := 0; i < n; i++ {
		c, err := net.Dial("tcp", addr)
		if err != nil {
			t.Fatal(err)
		}
		f.conns = append(f.conns, c)
		_ = c.SetWriteDeadline(time.Now().Add(5 * time.Second))
		if _, err = c.Write(append(append([]byte{}, head...), chunk...)); err != nil {
			t.Fatal(err)
		}
		time.Sleep(spacing)
	}
	var mu sync.Mutex
	var wg sync.WaitGroup
	for _, c := range f.conns {
		wg.Add(1)
		go func(c net.Conn) {
			defer wg.Done()
			status, _, retry := m14Answer(c, 1500*time.Millisecond)
			mu.Lock()
			defer mu.Unlock()
			if status == 0 {
				f.waiting++
				return
			}
			f.refused[status]++
			if (status == 429 || status == 503) && retry == "" {
				t.Errorf("a refusal (%d) without Retry-After", status)
			}
		}(c)
	}
	wg.Wait()
	return f
}

func (f *m14FloodResult) close() {
	for _, c := range f.conns {
		c.Close()
	}
}

func TestM14PreReadBufferingIsBounded(t *testing.T) {
	if testing.Short() {
		t.Skip("takes about 30 s of wall clock")
	}
	const connections = 60
	chunk := bytes.Repeat([]byte("a"), 400<<10) // 400 KiB of a 1 MiB body: 24 MiB if every connection were buffered

	t.Run("the share of one identity bounds what a flood holds", func(t *testing.T) {
		budget := access.NewBodyBudget(8<<20, 3<<20)
		e := m14Setup(t, m14Options{bodies: budget})
		e.draft("500")
		addr := m14Serve(t, e)
		path := e.root + "/ai/patch-proposals"
		before := heapInUse()
		flood := m14Flood(t, addr, path, e.alice, connections, chunk, 40*time.Millisecond)
		grown := int64(heapInUse()) - int64(before)
		t.Logf("%d connections of one token, 400 KiB sent each: %d admitted and held, refused %v, heap grew by %.1f MiB", connections, flood.waiting, flood.refused, float64(grown)/(1<<20))
		// A share of 3 MiB holds seven 400 KiB bodies (the eighth would need 3,200 KiB): the others are told as their bytes arrive.
		if flood.waiting != 7 || flood.refused[429] != connections-7 {
			t.Fatalf("a share of 3 MiB admits exactly seven 400 KiB bodies: %d waiting, refused %v", flood.waiting, flood.refused)
		}
		if grown > 16<<20 {
			t.Fatalf("the heap grew by %.1f MiB: the flood was buffered", float64(grown)/(1<<20))
		}
		// Another identity is not touched by the flood: it reaches the handler (a viewer may not propose).
		if status, code, took := m14RawPost(t, addr, path, e.viewer, 2, []byte(`{}`), 5*time.Second); status != 403 || took > 3*time.Second {
			t.Fatalf("another identity must reach the handler (403 for a viewer), got %d %s after %v", status, code, took)
		}
		// The flooding identity has 272 KiB of its share left: a body that does not fit is refused at once, typed and with Retry-After.
		if status, code, took := m14RawPost(t, addr, path, e.alice, 300<<10, bytes.Repeat([]byte("a"), 300<<10), 5*time.Second); status != 429 || code != "too_many_uploads" || took > time.Second {
			t.Fatalf("the flooding identity is refused at once: %d %s after %v", status, code, took)
		}
		// The bytes come back when the connections end, and the route's own cap still decides afterwards.
		flood.close()
		deadline := time.Now().Add(8 * time.Second)
		for {
			if used, subjects := budget.InFlight(); used == 0 && subjects == 0 {
				break
			}
			if time.Now().After(deadline) {
				used, subjects := budget.InFlight()
				t.Fatalf("the booked bytes were not returned after the connections ended: %d bytes, %d identities", used, subjects)
			}
			time.Sleep(100 * time.Millisecond)
		}
		big := []byte(`{"request":"` + strings.Repeat("a", 1<<20) + `"}`)
		if status, code, _ := m14RawPost(t, addr, path, e.alice, len(big), big, 10*time.Second); status != 413 || code != "body_too_large" {
			t.Fatalf("a 1 MiB body is admitted and the AI route's own cap answers 413 body_too_large, got %d %s", status, code)
		}
	})

	t.Run("without a budget the same flood is buffered (control)", func(t *testing.T) {
		e := m14Setup(t, m14Options{bodies: access.NewBodyBudget(1<<40, 1<<40)})
		e.draft("500")
		addr := m14Serve(t, e)
		before := heapInUse()
		flood := m14Flood(t, addr, e.root+"/ai/patch-proposals", e.alice, connections, chunk, 10*time.Millisecond)
		grown := int64(heapInUse()) - int64(before)
		t.Logf("control, %d connections of one token, 400 KiB sent each: %d held, refused %v, heap grew by %.1f MiB", connections, flood.waiting, flood.refused, float64(grown)/(1<<20))
		if flood.waiting != connections || len(flood.refused) != 0 || grown < 15<<20 {
			t.Fatalf("the control must show the amplification the budget removes: %d held, refused %v, heap +%.1f MiB", flood.waiting, flood.refused, float64(grown)/(1<<20))
		}
	})

	t.Run("the total bounds identities that stay within their own share", func(t *testing.T) {
		e := m14Setup(t, m14Options{bodies: access.NewBodyBudget(2<<20+1000, 8<<20)})
		e.draft("500")
		addr := m14Serve(t, e)
		path := e.root + "/ai/patch-proposals"
		almost := bytes.Repeat([]byte("a"), 1<<20-1) // a body that is one byte short of its 1 MiB: held until the read timeout
		flood := m14Flood(t, addr, path, e.alice, 2, almost, 100*time.Millisecond)
		if flood.waiting != 2 || len(flood.refused) != 0 {
			t.Fatalf("two 1 MiB bodies fit the total: %d waiting, refused %v", flood.waiting, flood.refused)
		}
		if status, code, took := m14RawPost(t, addr, path, e.viewer, 8<<10, bytes.Repeat([]byte("a"), 8<<10), 5*time.Second); status != 503 || code != "service_busy" || took > time.Second {
			t.Fatalf("8 KiB more than the total leaves room for: %d %s after %v", status, code, took)
		}
	})

	// Review F-06: the first budget booked the declared length, so eight identities that were never provisioned could each open
	// connections that announced 1 MiB and sent nothing, fill the 32 MiB total with them, and keep every body-carrying request of
	// every tenant at 503 service_busy for as long as they reconnected. A body is booked as it arrives now.
	t.Run("connections that announce a body and send none hold nothing", func(t *testing.T) {
		budget := access.NewBodyBudget(access.DefaultBodyBudgetBytes, access.DefaultBodyBudgetPerUser)
		e := m14Setup(t, m14Options{bodies: budget})
		e.draft("500")
		addr := m14Serve(t, e)
		const identities, perIdentity = 20, 4 // 80 MiB announced, against a total of 32 MiB
		var conns []net.Conn
		t.Cleanup(func() {
			for _, c := range conns {
				c.Close()
			}
		})
		for i := 0; i < identities; i++ {
			token := e.f.token(t, uuid(t), nil) // signed, never provisioned: it needs no membership to be read
			for j := 0; j < perIdentity; j++ {
				c, err := net.Dial("tcp", addr)
				if err != nil {
					t.Fatal(err)
				}
				conns = append(conns, c)
				if _, err = c.Write([]byte(m14Head(addr, e.root+"/ai/patch-proposals", token, 1<<20))); err != nil {
					t.Fatal(err)
				}
			}
		}
		time.Sleep(1500 * time.Millisecond) // every connection has reached its body read
		if used, subjects := budget.InFlight(); used != 0 || subjects != 0 {
			t.Fatalf("%d connections that sent no body hold %d bytes of %d identities", len(conns), used, subjects)
		}
		started := time.Now()
		status, code, _ := m14RawPost(t, addr, "/v1/tenants/"+e.f.tenantA+"/metadata/"+e.app+"/versions", e.alice, 2, []byte(`{}`), 5*time.Second)
		if took := time.Since(started); status == 0 || status == 429 || status == 503 || took > 3*time.Second {
			t.Fatalf("the tenant owner's request was refused or delayed behind %d silent connections: %d %s after %v", len(conns), status, code, took)
		}
		t.Logf("the owner's POST behind %d silent connections: %d %s", len(conns), status, code)
	})

	t.Run("an anonymous or invalid caller is never read", func(t *testing.T) {
		e := m14Setup(t, m14Options{})
		addr := m14Serve(t, e)
		// Announce 900,000 bytes, send one. A read of the body would wait for the read timeout (10 s).
		for name, path := range map[string]string{
			"candidates":       e.root + "/candidates",
			"acceptance-specs": e.root + "/acceptance-specs",
			"commands":         e.root + "/versions/1/entities/entity.order/commands",
			"ai":               e.root + "/ai/patch-proposals",
			"metadata":         "/v1/tenants/" + e.f.tenantA + "/metadata/" + e.app + "/versions",
		} {
			for who, token := range map[string]string{"anonymous": "", "invalid bearer": "not-a-token"} {
				if status, _, took := m14RawPost(t, addr, path, token, 900000, []byte("a"), 8*time.Second); status != 401 || took > 3*time.Second {
					t.Errorf("%s, %s: status %d after %v, want 401 at once", name, who, status, took)
				}
			}
		}
	})
}
