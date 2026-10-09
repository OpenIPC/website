package httpx

import (
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"
)

// An upload slower than the server's ReadTimeout gets through as long as it
// keeps moving, and one that stops is still cut off (OpenIPC/ipctool#234: a
// 100 MB backup refused at 60 s with "i/o timeout").
func TestAnUploadThatKeepsMovingOutlastsTheReadTimeout(t *testing.T) {
	got := make(chan string, 4)
	mux := http.NewServeMux()
	mux.HandleFunc("POST /slow", func(w http.ResponseWriter, r *http.Request) {
		KeepReading(w, r, 700*time.Millisecond)
		b, err := io.ReadAll(r.Body)
		got <- fmt.Sprintf("%d %v", len(b), err)
		fmt.Fprint(w, "ok")
	})
	mux.HandleFunc("POST /plain", func(w http.ResponseWriter, r *http.Request) {
		b, err := io.ReadAll(r.Body)
		got <- fmt.Sprintf("%d %v", len(b), err)
	})
	srv := &http.Server{Handler: Log(slog.New(slog.DiscardHandler), mux), ReadTimeout: time.Second}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = srv.Serve(ln) }()
	defer srv.Close()

	// send writes n pieces, every gap apart, then reads the answer.
	send := func(path string, n int, gap time.Duration) string {
		c, err := net.Dial("tcp", ln.Addr().String())
		if err != nil {
			t.Fatal(err)
		}
		defer c.Close()
		fmt.Fprintf(c, "POST %s HTTP/1.1\r\nHost: x\r\nContent-Length: %d\r\nConnection: close\r\n\r\n", path, n*10)
		for i := 0; i < n; i++ {
			time.Sleep(gap)
			if _, err := c.Write([]byte(strings.Repeat("x", 10))); err != nil {
				break
			}
		}
		_ = c.SetReadDeadline(time.Now().Add(5 * time.Second))
		answer, _ := io.ReadAll(c)
		return string(answer)
	}

	// 3 s in all, a piece every 300 ms: three times the ReadTimeout.
	if a := send("/slow", 10, 300*time.Millisecond); !strings.HasSuffix(a, "ok") {
		t.Errorf("the moving upload's answer: %q", a)
	}
	if g := <-got; g != "100 <nil>" {
		t.Errorf("the moving upload read %s", g)
	}

	// Without it, the server's ReadTimeout cuts the same upload short.
	send("/plain", 10, 300*time.Millisecond)
	if g := <-got; strings.HasSuffix(g, "<nil>") {
		t.Errorf("without KeepReading the slow upload read %s; the test proves nothing", g)
	}

	// A gap longer than idle is still the end of it.
	send("/slow", 2, 1500*time.Millisecond)
	if g := <-got; strings.HasSuffix(g, "<nil>") {
		t.Errorf("an upload that stopped for longer than idle read %s", g)
	}
}
