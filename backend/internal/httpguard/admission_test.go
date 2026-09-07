package httpguard

import (
	"bufio"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestAdmissionKeepsHealthAvailableAndReleasesSlots(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	done := make(chan struct{})
	a := NewAdmission(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/hold" {
			close(entered)
			<-release
		}
		w.WriteHeader(http.StatusNoContent)
	}), 1, 1024)
	go func() {
		defer close(done)
		a.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/hold", nil))
	}()
	<-entered
	w := httptest.NewRecorder()
	a.ServeHTTP(w, httptest.NewRequest("GET", "/other", nil))
	if w.Code != 503 || w.Header().Get("Retry-After") == "" {
		t.Fatalf("full handler budget status = %d", w.Code)
	}
	w = httptest.NewRecorder()
	a.ServeHTTP(w, httptest.NewRequest("GET", "/healthz", nil))
	if w.Code != 204 {
		t.Fatal("health blocked by admission")
	}
	close(release)
	<-done
	w = httptest.NewRecorder()
	a.ServeHTTP(w, httptest.NewRequest("GET", "/other", nil))
	if w.Code != 204 {
		t.Fatal("completed handler did not release admission")
	}
}

func TestBodyBudgetIncludesBuffersRetainedAfterReading(t *testing.T) {
	read := make(chan struct{})
	release := make(chan struct{})
	done := make(chan struct{})
	a := NewAdmission(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, err := io.ReadAll(r.Body)
		if errors.Is(err, ErrBodyBudget) {
			w.WriteHeader(503)
			return
		}
		if err != nil {
			t.Error(err)
		}
		if r.URL.Path == "/hold" {
			close(read)
			<-release
		}
	}), 4, 8)
	go func() {
		defer close(done)
		a.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("POST", "/hold", strings.NewReader("1234")))
	}()
	<-read
	request := httptest.NewRequest("POST", "/overflow", strings.NewReader("123456"))
	request.ContentLength = -1 // Chunked bodies use the same byte accounting.
	w := httptest.NewRecorder()
	a.ServeHTTP(w, request)
	if w.Code != 503 || a.bytes.Load() != 4 {
		t.Fatalf("buffer budget status=%d held=%d", w.Code, a.bytes.Load())
	}
	close(release)
	<-done
	if a.bytes.Load() != 0 {
		t.Fatal("buffer reservation leaked")
	}
}

func TestSlowUploadAndHeadersHaveReadDeadlines(t *testing.T) {
	for _, request := range []string{"POST / HTTP/1.1\r\nHost: local", "POST / HTTP/1.1\r\nHost: local\r\nContent-Length: 10\r\n\r\nx"} {
		t.Run(request[:min(18, len(request))], func(t *testing.T) {
			handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, err := io.ReadAll(r.Body)
				if err != nil {
					w.WriteHeader(408)
				}
			})
			server := NewServer("127.0.0.1:0", handler, 1, 1024)
			if server.ReadHeaderTimeout <= 0 || server.ReadTimeout <= 0 || server.IdleTimeout <= 0 || server.WriteTimeout != 0 {
				t.Fatal("incorrect server deadline policy")
			}
			server.ReadHeaderTimeout = 50 * time.Millisecond
			server.ReadTimeout = 50 * time.Millisecond
			listener, err := net.Listen("tcp", server.Addr)
			if err != nil {
				t.Fatal(err)
			}
			defer listener.Close()
			defer server.Close()
			go func() { _ = server.Serve(listener) }()
			conn, err := net.Dial("tcp", listener.Addr().String())
			if err != nil {
				t.Fatal(err)
			}
			defer conn.Close()
			_ = conn.SetDeadline(time.Now().Add(time.Second))
			if _, err := io.WriteString(conn, request); err != nil {
				t.Fatal(err)
			}
			started := time.Now()
			response, err := http.ReadResponse(bufio.NewReader(conn), nil)
			if response != nil {
				_ = response.Body.Close()
			}
			if time.Since(started) > 500*time.Millisecond {
				t.Fatalf("slow request was not released: %v", err)
			}
		})
	}
}
