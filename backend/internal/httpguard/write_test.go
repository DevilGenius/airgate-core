package httpguard

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestSlowReaderCancelsHandlerContext(t *testing.T) {
	done := make(chan error, 1)
	handler := WithWriteIdle(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data := make([]byte, 32<<10)
		for range 256 {
			if _, err := w.Write(data); err != nil {
				done <- r.Context().Err()
				return
			}
		}
		done <- errors.New("test did not fill the socket")
	}), 50*time.Millisecond)
	server := httptest.NewUnstartedServer(handler)
	server.Config.ConnState = func(conn net.Conn, state http.ConnState) {
		if state == http.StateNew {
			if tcp, ok := conn.(*net.TCPConn); ok {
				_ = tcp.SetWriteBuffer(1024)
			}
		}
	}
	server.Start()
	defer server.Close()
	conn, err := net.Dial("tcp", server.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_, err = io.WriteString(conn, "GET / HTTP/1.1\r\nHost: local\r\n\r\n")
	if err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("write timeout did not cancel request: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("slow client retained handler")
	}
}

func TestHTTP2UpstreamPauseDoesNotExpireWriteDeadline(t *testing.T) {
	handler := WithWriteIdle(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, err := io.WriteString(w, "first"); err != nil {
			t.Error(err)
			return
		}
		w.(http.Flusher).Flush()
		time.Sleep(100 * time.Millisecond) // Longer than write idle, with no write in progress.
		if _, err := io.WriteString(w, "second"); err != nil {
			t.Error(err)
		}
	}), 30*time.Millisecond)
	server := httptest.NewUnstartedServer(handler)
	server.EnableHTTP2 = true
	server.StartTLS()
	defer server.Close()
	response, err := server.Client().Get(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	data, err := io.ReadAll(response.Body)
	if err != nil || string(data) != "firstsecond" || response.ProtoMajor != 2 {
		t.Fatalf("long stream interrupted: protocol=%s body=%q error=%v", response.Proto, data, err)
	}
}
