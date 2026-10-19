package access

import (
	"net"
	"net/http"
	"testing"
	"time"
)

// A server-wide write timeout ends a slow response; extendWriteDeadline lets a request that
// asked for more time finish (the AI provider phase runs longer than the 10 s server default).
func TestExtendWriteDeadlineLetsASlowResponseFinish(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/extended", func(w http.ResponseWriter, r *http.Request) {
		extendWriteDeadline(w, 5*time.Second)
		time.Sleep(700 * time.Millisecond)
		_, _ = w.Write([]byte("done"))
	})
	mux.HandleFunc("/plain", func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(700 * time.Millisecond)
		_, _ = w.Write([]byte("done"))
	})
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv := &http.Server{Handler: mux, WriteTimeout: 300 * time.Millisecond}
	go func() { _ = srv.Serve(listener) }()
	t.Cleanup(func() { _ = srv.Close() })
	client := &http.Client{Timeout: 10 * time.Second}

	resp, err := client.Get("http://" + listener.Addr().String() + "/extended")
	if err != nil {
		t.Fatalf("extended request failed: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("extended request answered %d", resp.StatusCode)
	}
	// Control: without the extension the same handler is cut off by the write timeout.
	if resp, err = client.Get("http://" + listener.Addr().String() + "/plain"); err == nil {
		resp.Body.Close()
		t.Fatal("the control request was not cut off by the write timeout; the test proves nothing")
	}
}

// Writers without deadline support (the test recorders of the integration suite) are left alone.
func TestExtendWriteDeadlineIgnoresWritersWithoutDeadlines(t *testing.T) {
	extendWriteDeadline(nilDeadlineWriter{}, time.Second)
}

type nilDeadlineWriter struct{}

func (nilDeadlineWriter) Header() http.Header         { return http.Header{} }
func (nilDeadlineWriter) Write(b []byte) (int, error) { return len(b), nil }
func (nilDeadlineWriter) WriteHeader(int)             {}
