// Package httpguard bounds HTTP resources before authentication and parsing.
package httpguard

import (
	"errors"
	"io"
	"net/http"
	"sync"
	"sync/atomic"
	"time"
)

var ErrBodyBudget = errors.New("request body buffer budget exhausted")

const (
	DefaultMaxInFlight = 1024
	DefaultBodyBytes   = 128 << 20
)

type Admission struct {
	next     http.Handler
	slots    chan struct{}
	maxBytes int64
	bytes    atomic.Int64
}

func NewServer(addr string, next http.Handler, maxRequests int, maxBytes int64) *http.Server {
	return &http.Server{
		Addr:              addr,
		Handler:           NewAdmission(WithWriteIdle(next, DefaultWriteIdle), maxRequests, maxBytes),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       60 * time.Second,
		IdleTimeout:       90 * time.Second,
	}
}

func NewAdmission(next http.Handler, maxRequests int, maxBytes int64) *Admission {
	if maxRequests <= 0 {
		maxRequests = DefaultMaxInFlight
	}
	if maxBytes <= 0 {
		maxBytes = DefaultBodyBytes
	}
	return &Admission{next: next, slots: make(chan struct{}, maxRequests), maxBytes: maxBytes}
}

func (a *Admission) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Read-only health checks remain available while the business entry is full.
	if r.Method == http.MethodGet && r.URL.Path == "/healthz" {
		a.next.ServeHTTP(w, r)
		return
	}
	select {
	case a.slots <- struct{}{}:
		defer func() { <-a.slots }()
	default:
		w.Header().Set("Retry-After", "1")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = io.WriteString(w, `{"error":{"type":"server_error","code":"server_busy","message":"服务繁忙，请稍后重试"}}`)
		return
	}
	if r.Body != nil && r.Body != http.NoBody {
		body := &budgetBody{ReadCloser: r.Body, admission: a}
		r.Body = body
		// Retain the reservation for the entire handler, including upstream
		// execution: parsed/body buffers commonly live much longer than the read.
		defer body.releaseAll()
	}
	a.next.ServeHTTP(w, r)
}

type budgetBody struct {
	io.ReadCloser
	admission *Admission
	mu        sync.Mutex
	held      int64
	released  bool
}

func (b *budgetBody) reserve(want int) int {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.released {
		return 0
	}
	for {
		used := b.admission.bytes.Load()
		n := min(int64(want), 32<<10, b.admission.maxBytes-used)
		if n <= 0 {
			return 0
		}
		if b.admission.bytes.CompareAndSwap(used, used+n) {
			b.held += n
			return int(n)
		}
	}
}

func (b *budgetBody) releaseUnused(n int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if !b.released {
		b.held -= int64(n)
		b.admission.bytes.Add(-int64(n))
	}
}

func (b *budgetBody) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	reserved := b.reserve(len(p))
	if reserved == 0 {
		return 0, ErrBodyBudget
	}
	n, err := b.ReadCloser.Read(p[:reserved])
	b.releaseUnused(reserved - n)
	return n, err
}

func (b *budgetBody) releaseAll() {
	b.mu.Lock()
	defer b.mu.Unlock()
	if !b.released {
		b.released = true
		b.admission.bytes.Add(-b.held)
		b.held = 0
	}
}
