package mailer

import (
	"crypto/tls"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/smtp"
	"strings"
	"testing"
	"time"
)

func TestSendReturnsErrorWhenSMTPNotConfigured(t *testing.T) {
	m := New(Config{})

	err := m.Send("u@test.com", "主题", "<p>内容</p>")
	if err == nil {
		t.Fatal("未配置 SMTP 时应返回错误")
	}
	if !strings.Contains(err.Error(), "SMTP 未配置") {
		t.Fatalf("错误 = %v，期望提示 SMTP 未配置", err)
	}
}

func TestSendUsesSMTPConfig(t *testing.T) {
	restore := replaceMailerHooks(t)
	defer restore()

	var capturedAddr string
	var capturedAuth smtp.Auth
	var capturedFrom string
	var capturedTo []string
	var capturedMsg string
	smtpSendMail = func(addr string, auth smtp.Auth, from string, to []string, msg []byte) error {
		capturedAddr = addr
		capturedAuth = auth
		capturedFrom = from
		capturedTo = append([]string(nil), to...)
		capturedMsg = string(msg)
		return nil
	}

	m := New(Config{
		Host:     "smtp.test",
		Port:     2525,
		Username: "user",
		Password: "pass",
		FromAddr: "noreply@test.com",
		FromName: "AirGate",
	})
	if err := m.Send("u@test.com", "Subject", "<p>Body</p>"); err != nil {
		t.Fatalf("Send() error = %v", err)
	}
	if capturedAddr != "smtp.test:2525" || capturedFrom != "noreply@test.com" || len(capturedTo) != 1 || capturedTo[0] != "u@test.com" || capturedAuth == nil {
		t.Fatalf("captured smtp args addr=%q from=%q to=%v auth=%v", capturedAddr, capturedFrom, capturedTo, capturedAuth)
	}
	if !strings.Contains(capturedMsg, "From: AirGate <noreply@test.com>") ||
		!strings.Contains(capturedMsg, "Subject: Subject") ||
		!strings.Contains(capturedMsg, "<p>Body</p>") {
		t.Fatalf("captured msg = %q", capturedMsg)
	}
}

func TestSendPropagatesSMTPError(t *testing.T) {
	restore := replaceMailerHooks(t)
	defer restore()

	sendErr := errors.New("smtp failed")
	smtpSendMail = func(string, smtp.Auth, string, []string, []byte) error {
		return sendErr
	}

	m := New(Config{Host: "smtp.test", Port: 25, FromAddr: "noreply@test.com"})
	if err := m.Send("u@test.com", "Subject", "Body"); !errors.Is(err, sendErr) {
		t.Fatalf("Send() error = %v, want %v", err, sendErr)
	}
}

func TestDefaultSMTPHooks(t *testing.T) {
	tlsServer := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer tlsServer.Close()

	conn, err := defaultDialTLS("tcp", tlsServer.Listener.Addr().String(), &tls.Config{InsecureSkipVerify: true})
	if err != nil {
		t.Fatalf("defaultDialTLS() error = %v", err)
	}
	_ = conn.Close()

	clientConn, serverConn := net.Pipe()
	done := make(chan error, 1)
	go func() {
		defer func() { _ = serverConn.Close() }()
		_, writeErr := serverConn.Write([]byte("220 smtp.test ESMTP\r\n"))
		done <- writeErr
	}()

	client, err := defaultSMTPClient(clientConn, "smtp.test")
	if err != nil {
		t.Fatalf("defaultSMTPClient() error = %v", err)
	}
	_ = client.Close()
	if err := <-done; err != nil {
		t.Fatalf("SMTP greeting write error = %v", err)
	}
}

func TestSendUsesTLSClient(t *testing.T) {
	restore := replaceMailerHooks(t)
	defer restore()

	var client fakeSMTPClient
	dialTLS = func(string, string, *tls.Config) (net.Conn, error) {
		return noopConn{}, nil
	}
	newSMTPClient = func(net.Conn, string) (smtpClient, error) {
		client.writer = fakeWriteCloser{}
		return &client, nil
	}

	m := New(Config{Host: "smtp.test", Port: 465, FromAddr: "noreply@test.com", UseTLS: true})
	if err := m.Send("u@test.com", "Subject", "Body"); err != nil {
		t.Fatalf("Send() TLS error = %v", err)
	}
	if client.mailFrom != "noreply@test.com" || client.rcptTo != "u@test.com" || !client.closed {
		t.Fatalf("client = %+v", client)
	}
}

func TestSendTLSErrors(t *testing.T) {
	tests := []struct {
		name       string
		auth       smtp.Auth
		dialErr    error
		clientErr  error
		authErr    error
		mailErr    error
		rcptErr    error
		dataErr    error
		writeErr   error
		closeErr   error
		wantPrefix string
	}{
		{name: "dial", dialErr: errors.New("dial failed"), wantPrefix: "TLS dial"},
		{name: "client", clientErr: errors.New("client failed"), wantPrefix: "SMTP client"},
		{name: "auth", auth: smtp.PlainAuth("", "user", "pass", "smtp.test"), authErr: errors.New("auth failed"), wantPrefix: "SMTP auth"},
		{name: "mail", mailErr: errors.New("mail failed")},
		{name: "rcpt", rcptErr: errors.New("rcpt failed")},
		{name: "data", dataErr: errors.New("data failed")},
		{name: "write", writeErr: errors.New("write failed")},
		{name: "close", closeErr: errors.New("close failed")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			restore := replaceMailerHooks(t)
			defer restore()

			dialTLS = func(string, string, *tls.Config) (net.Conn, error) {
				if tt.dialErr != nil {
					return nil, tt.dialErr
				}
				return noopConn{}, nil
			}
			newSMTPClient = func(net.Conn, string) (smtpClient, error) {
				if tt.clientErr != nil {
					return nil, tt.clientErr
				}
				return &fakeSMTPClient{
					authErr: tt.authErr,
					mailErr: tt.mailErr,
					rcptErr: tt.rcptErr,
					dataErr: tt.dataErr,
					writer:  fakeWriteCloser{writeErr: tt.writeErr, closeErr: tt.closeErr},
				}, nil
			}

			m := New(Config{Host: "smtp.test", Port: 465, FromAddr: "noreply@test.com"})
			err := m.sendTLS("smtp.test:465", tt.auth, "u@test.com", []byte("Body"))
			if err == nil {
				t.Fatal("sendTLS() error = nil, want error")
			}
			if tt.wantPrefix != "" && !strings.Contains(err.Error(), tt.wantPrefix) {
				t.Fatalf("sendTLS() error = %v, want prefix %q", err, tt.wantPrefix)
			}
		})
	}
}

func TestVerifyCodeStoreGenerateCheckAndVerify(t *testing.T) {
	store := NewVerifyCodeStore()

	code := store.Generate("u@test.com")
	if len(code) != 6 {
		t.Fatalf("验证码长度 = %d，期望 6", len(code))
	}
	for _, ch := range code {
		if ch < '0' || ch > '9' {
			t.Fatalf("验证码包含非数字字符: %q", code)
		}
	}
	if !store.Check("u@test.com", code) {
		t.Fatal("生成的验证码应可通过 Check")
	}
	if !store.Verify("u@test.com", code) {
		t.Fatal("生成的验证码应可通过 Verify")
	}
	if store.Check("u@test.com", code) {
		t.Fatal("Verify 成功后验证码应被删除")
	}
	if store.Verify("missing@test.com", "000000") {
		t.Fatal("不存在的验证码不应通过 Verify")
	}
}

func TestVerifyCodeStoreRejectsAndCleansExpiredCode(t *testing.T) {
	store := NewVerifyCodeStore()
	store.mu.Lock()
	store.codes["u@test.com"] = codeEntry{
		code:      "123456",
		expiresAt: time.Now().Add(-time.Minute),
	}
	store.mu.Unlock()

	if store.Check("u@test.com", "123456") {
		t.Fatal("过期验证码不应通过 Check")
	}
	store.mu.RLock()
	_, exists := store.codes["u@test.com"]
	store.mu.RUnlock()
	if exists {
		t.Fatal("过期验证码应在 Check 后被删除")
	}

	store.mu.Lock()
	store.codes["old@test.com"] = codeEntry{code: "111111", expiresAt: time.Now().Add(-time.Minute)}
	store.codes["new@test.com"] = codeEntry{code: "222222", expiresAt: time.Now().Add(time.Minute)}
	store.mu.Unlock()
	store.cleanup()

	store.mu.RLock()
	_, oldExists := store.codes["old@test.com"]
	_, newExists := store.codes["new@test.com"]
	store.mu.RUnlock()
	if oldExists || !newExists {
		t.Fatalf("清理结果异常: old=%v new=%v", oldExists, newExists)
	}
}

func TestVerifyCodeStoreBackgroundCleanup(t *testing.T) {
	previous := verifyCodeCleanupInterval
	verifyCodeCleanupInterval = time.Millisecond
	t.Cleanup(func() { verifyCodeCleanupInterval = previous })

	store := NewVerifyCodeStore()
	store.mu.Lock()
	store.codes["old@test.com"] = codeEntry{code: "111111", expiresAt: time.Now().Add(-time.Minute)}
	store.mu.Unlock()

	deadline := time.Now().Add(200 * time.Millisecond)
	for time.Now().Before(deadline) {
		store.mu.RLock()
		_, exists := store.codes["old@test.com"]
		store.mu.RUnlock()
		if !exists {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("background cleanup did not remove expired code")
}

func replaceMailerHooks(t *testing.T) func() {
	t.Helper()
	prevSendMail := smtpSendMail
	prevDialTLS := dialTLS
	prevNewClient := newSMTPClient
	return func() {
		smtpSendMail = prevSendMail
		dialTLS = prevDialTLS
		newSMTPClient = prevNewClient
	}
}

type fakeSMTPClient struct {
	authErr  error
	mailErr  error
	rcptErr  error
	dataErr  error
	writer   io.WriteCloser
	mailFrom string
	rcptTo   string
	closed   bool
}

func (c *fakeSMTPClient) Auth(smtp.Auth) error {
	return c.authErr
}

func (c *fakeSMTPClient) Mail(from string) error {
	c.mailFrom = from
	return c.mailErr
}

func (c *fakeSMTPClient) Rcpt(to string) error {
	c.rcptTo = to
	return c.rcptErr
}

func (c *fakeSMTPClient) Data() (io.WriteCloser, error) {
	if c.dataErr != nil {
		return nil, c.dataErr
	}
	if c.writer == nil {
		c.writer = fakeWriteCloser{}
	}
	return c.writer, nil
}

func (c *fakeSMTPClient) Close() error {
	c.closed = true
	return nil
}

type fakeWriteCloser struct {
	writeErr error
	closeErr error
}

func (w fakeWriteCloser) Write(p []byte) (int, error) {
	if w.writeErr != nil {
		return 0, w.writeErr
	}
	return len(p), nil
}

func (w fakeWriteCloser) Close() error {
	return w.closeErr
}

type noopConn struct{}

func (noopConn) Read([]byte) (int, error)         { return 0, io.EOF }
func (noopConn) Write(p []byte) (int, error)      { return len(p), nil }
func (noopConn) Close() error                     { return nil }
func (noopConn) LocalAddr() net.Addr              { return fakeAddr("local") }
func (noopConn) RemoteAddr() net.Addr             { return fakeAddr("remote") }
func (noopConn) SetDeadline(time.Time) error      { return nil }
func (noopConn) SetReadDeadline(time.Time) error  { return nil }
func (noopConn) SetWriteDeadline(time.Time) error { return nil }

type fakeAddr string

func (a fakeAddr) Network() string { return string(a) }
func (a fakeAddr) String() string  { return string(a) }
