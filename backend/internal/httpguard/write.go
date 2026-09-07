package httpguard

import (
	"bufio"
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"time"
)

const DefaultWriteIdle = 30 * time.Second

// Wrap the native writer before Gin or plugin wrappers obscure its deadline
// support. Deadlines exist only during an actual write/flush; waiting for the
// next upstream event does not expire an otherwise healthy HTTP/2 stream.
func WithWriteIdle(next http.Handler, idle time.Duration) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithCancel(r.Context())
		defer cancel()
		writer := &idleWriter{ResponseWriter: w, controller: http.NewResponseController(w), idle: idle, cancel: cancel, ctx: ctx}
		next.ServeHTTP(writer, r.WithContext(ctx))
		if writer.wrote && !writer.hijacked && writer.err == nil {
			// Flush the last net/http buffer while the deadline guard still owns
			// the writer, rather than leaving a blocking final flush to the server.
			_ = writer.FlushError()
		}
	})
}

type idleWriter struct {
	http.ResponseWriter
	controller *http.ResponseController
	idle       time.Duration
	cancel     context.CancelFunc
	ctx        context.Context
	err        error
	wrote      bool
	hijacked   bool
}

func (w *idleWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

func (w *idleWriter) beginWrite() error {
	if w.err != nil {
		return w.err
	}
	err := w.controller.SetWriteDeadline(time.Now().Add(w.idle))
	if err != nil && !errors.Is(err, http.ErrNotSupported) {
		w.err = err
		w.cancel()
		return err
	}
	return nil
}

func (w *idleWriter) endWrite(err error) {
	_ = w.controller.SetWriteDeadline(time.Time{})
	if err != nil {
		w.err = err
		w.cancel()
	}
}

func (w *idleWriter) WriteHeader(status int) {
	if w.beginWrite() != nil {
		return
	}
	w.ResponseWriter.WriteHeader(status)
	w.wrote = true
	w.endWrite(nil)
}

func (w *idleWriter) Write(p []byte) (int, error) {
	total := 0
	for len(p) > 0 {
		if err := w.beginWrite(); err != nil {
			return total, err
		}
		chunk := p[:min(len(p), 32<<10)]
		w.wrote = true
		n, err := w.ResponseWriter.Write(chunk)
		if err == nil && n != len(chunk) {
			err = io.ErrShortWrite
		}
		w.endWrite(err)
		total += n
		if err != nil {
			return total, err
		}
		p = p[n:]
	}
	return total, nil
}

func (w *idleWriter) FlushError() error {
	if err := w.beginWrite(); err != nil {
		return err
	}
	err := w.controller.Flush()
	w.endWrite(err)
	return err
}

func (w *idleWriter) Flush() { _ = w.FlushError() }

func (w *idleWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	conn, buffer, err := w.controller.Hijack()
	if err == nil {
		w.hijacked = true
	}
	return conn, buffer, err
}

func (w *idleWriter) Push(target string, options *http.PushOptions) error {
	if pusher, ok := w.ResponseWriter.(http.Pusher); ok {
		return pusher.Push(target, options)
	}
	return http.ErrNotSupported
}

func (w *idleWriter) CloseNotify() <-chan bool {
	if notifier, ok := w.ResponseWriter.(interface{ CloseNotify() <-chan bool }); ok {
		return notifier.CloseNotify()
	}
	closed := make(chan bool, 1)
	context.AfterFunc(w.ctx, func() { closed <- true })
	return closed
}
