package handler

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"entgo.io/ent/dialect/sql/schema"
	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"

	entuser "github.com/DevilGenius/airgate-core/ent/user"
	appapikey "github.com/DevilGenius/airgate-core/internal/app/apikey"
	appauth "github.com/DevilGenius/airgate-core/internal/app/auth"
	appnotification "github.com/DevilGenius/airgate-core/internal/app/notification"
	appproxy "github.com/DevilGenius/airgate-core/internal/app/proxy"
	appsettings "github.com/DevilGenius/airgate-core/internal/app/settings"
	appuser "github.com/DevilGenius/airgate-core/internal/app/user"
	coreauth "github.com/DevilGenius/airgate-core/internal/auth"
	"github.com/DevilGenius/airgate-core/internal/infra/mailer"
	"github.com/DevilGenius/airgate-core/internal/infra/store"
	"github.com/DevilGenius/airgate-core/internal/server/middleware"
	"github.com/DevilGenius/airgate-core/internal/testdb"
)

func TestSettingsSMTPAndNotificationRoutes(t *testing.T) {
	host, port, messages := startHandlerSMTPServer(t)
	handler := NewSettingsHandler(nil, "", appnotification.NewService(nil))

	body := fmt.Sprintf(`{"host":%q,"port":%d,"from":"from@example.com","to":"to@example.com"}`, host, port)
	w := invokeHandlerForValidation(http.MethodPost, "/settings/smtp/test", body, nil, nil, handler.TestSMTP)
	requireOKResponse(t, asResponseView(w.Code, w.Body.String()))
	select {
	case msg := <-messages:
		if !strings.Contains(msg, "AirGate SMTP Test") || !strings.Contains(msg, "from@example.com") {
			t.Fatalf("smtp message = %s", msg)
		}
	case <-time.After(time.Second):
		t.Fatal("smtp server did not receive test message")
	}

	closedHost, closedPort := closedTCPAddress(t)
	tlsBody := fmt.Sprintf(`{"host":%q,"port":%d,"from":"from@example.com","to":"to@example.com","use_tls":true}`, closedHost, closedPort)
	w = invokeHandlerForValidation(http.MethodPost, "/settings/smtp/test", tlsBody, nil, nil, handler.TestSMTP)
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "TLS connection failed") {
		t.Fatalf("tls smtp failure status=%d body=%s", w.Code, w.Body.String())
	}

	notificationServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("notification method = %s", r.Method)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer secret-token" {
			t.Fatalf("notification auth = %q", got)
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read notification body: %v", err)
		}
		bodyText := string(body)
		if !strings.Contains(bodyText, "测试标题") || !strings.Contains(bodyText, "测试内容") {
			t.Fatalf("notification body = %s", bodyText)
		}
		w.WriteHeader(http.StatusAccepted)
	}))
	defer notificationServer.Close()

	notificationBody := fmt.Sprintf(`{"webhook_url":%q,"secret":"secret-token","body":"{\"title\":\"{{title}}\",\"content\":\"{{content}}\"}"}`, notificationServer.URL)
	w = invokeHandlerForValidation(http.MethodPost, "/settings/notification/test", notificationBody, nil, nil, handler.TestNotification)
	requireOKResponse(t, asResponseView(w.Code, w.Body.String()))

	nilHandler := NewSettingsHandler(nil, "", nil)
	w = invokeHandlerForValidation(http.MethodPost, "/settings/notification/test", notificationBody, nil, nil, nilHandler.TestNotification)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("nil notification service status=%d body=%s", w.Code, w.Body.String())
	}

	rejectingServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "nope", http.StatusTeapot)
	}))
	defer rejectingServer.Close()
	rejectingBody := fmt.Sprintf(`{"webhook_url":%q,"body":"{\"text\":\"{{title}}\"}"}`, rejectingServer.URL)
	w = invokeHandlerForValidation(http.MethodPost, "/settings/notification/test", rejectingBody, nil, nil, handler.TestNotification)
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "Webhook send failed") {
		t.Fatalf("notification failure status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestAuthAPIKeyLoginGetMeAndVerifyMailRoutesWithSQLite(t *testing.T) {
	ctx := context.Background()
	db := testdb.OpenMemoryEnt(t, "handler_auth_apikey_more", schema.WithGlobalUniqueID(false))
	defer func() {
		if err := db.Close(); err != nil {
			t.Fatalf("close db: %v", err)
		}
	}()

	secret := strings.Repeat("c", 64)
	jwtMgr := coreauth.NewJWTManager("jwt-secret", 1)
	settingsService := appsettings.NewService(store.NewSettingsStore(db))
	authService := appauth.NewService(store.NewAuthStore(db), jwtMgr)
	userService := appuser.NewService(store.NewUserStore(db))
	apiKeyService := appapikey.NewService(store.NewAPIKeyStore(db), secret)
	codeStore := mailer.NewVerifyCodeStore()
	authHandler := NewAuthHandler(authService, settingsService, userService, codeStore, db, jwtMgr)
	userHandler := NewUserHandler(userService, settingsService, nil)

	hash, err := bcrypt.GenerateFromPassword([]byte("password123"), bcrypt.DefaultCost)
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}
	userEnt, err := db.User.Create().
		SetEmail("apikey-login@example.com").
		SetPasswordHash(string(hash)).
		SetUsername("apikey-user").
		SetRole("user").
		SetMaxConcurrency(6).
		Save(ctx)
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	groupEnt, err := db.Group.Create().
		SetName("Login Group").
		SetPlatform("openai").
		SetRateMultiplier(1.5).
		SetSubscriptionType("standard").
		Save(ctx)
	if err != nil {
		t.Fatalf("create group: %v", err)
	}
	sellRate := 1.2
	key, err := apiKeyService.CreateOwned(ctx, userEnt.ID, appapikey.CreateInput{
		Name:           "login-key",
		GroupID:        int64(groupEnt.ID),
		QuotaUSD:       19,
		SellRate:       &sellRate,
		MaxConcurrency: 3,
	})
	if err != nil {
		t.Fatalf("create api key: %v", err)
	}
	coreauth.InvalidateAPIKeyCache("")

	w := invokeHandlerForValidation(http.MethodPost, "/login/api-key", `{"key":"bad-key"}`, nil, nil, authHandler.LoginByAPIKey)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("bad api key login status=%d body=%s", w.Code, w.Body.String())
	}
	w = invokeHandlerForValidation(http.MethodPost, "/login/api-key", `{"key":"sk-missing"}`, nil, nil, authHandler.LoginByAPIKey)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("missing api key login status=%d body=%s", w.Code, w.Body.String())
	}

	w = invokeHandlerForValidation(http.MethodPost, "/login/api-key", fmt.Sprintf(`{"key":%q}`, key.PlainKey), nil, nil, authHandler.LoginByAPIKey)
	requireOKResponse(t, asResponseView(w.Code, w.Body.String()))
	if !strings.Contains(w.Body.String(), `"api_key_id":`+intToString(key.ID)) ||
		!strings.Contains(w.Body.String(), `"api_key_name":"login-key"`) ||
		!strings.Contains(w.Body.String(), `"role":"api_key"`) {
		t.Fatalf("api key login body = %s", w.Body.String())
	}

	w = invokeHandlerForValidation(http.MethodGet, "/users/me", "", nil, func(c *gin.Context) {
		c.Set("user_id", userEnt.ID)
		c.Set(middleware.CtxKeyAPIKeyID, key.ID)
	}, userHandler.GetMe)
	requireOKResponse(t, asResponseView(w.Code, w.Body.String()))
	if !strings.Contains(w.Body.String(), `"api_key_platform":"openai"`) || !strings.Contains(w.Body.String(), `"api_key_rate":1.799`) {
		t.Fatalf("get me api key body = %s", w.Body.String())
	}

	host, port, messages := startHandlerSMTPServer(t)
	if err := settingsService.Update(ctx, []appsettings.ItemInput{
		{Key: "smtp_host", Value: host, Group: "smtp"},
		{Key: "smtp_port", Value: strconv.Itoa(port), Group: "smtp"},
		{Key: "smtp_from_email", Value: "noreply@example.com", Group: "smtp"},
		{Key: "smtp_from_name", Value: "AirGate Tests", Group: "smtp"},
		{Key: "smtp_use_tls", Value: "false", Group: "smtp"},
		{Key: "email_template_subject", Value: "{{site_name}} code", Group: "smtp"},
		{Key: "email_template_body", Value: "<p>{{email}} {{code}} {{site_name}}</p>", Group: "smtp"},
		{Key: "site_name", Value: "TestGate", Group: "site"},
	}); err != nil {
		t.Fatalf("save smtp settings: %v", err)
	}
	w = invokeHandlerForValidation(http.MethodPost, "/verify-code/send", `{"email":"new-user@example.com"}`, nil, nil, authHandler.SendVerifyCode)
	requireOKResponse(t, asResponseView(w.Code, w.Body.String()))
	select {
	case msg := <-messages:
		if !strings.Contains(msg, "Subject: TestGate code") || !strings.Contains(msg, "new-user@example.com") {
			t.Fatalf("verify mail message = %s", msg)
		}
	case <-time.After(time.Second):
		t.Fatal("smtp server did not receive verify mail")
	}

	w = invokeHandlerForValidation(http.MethodPost, "/verify-code/send", `{"email":"apikey-login@example.com"}`, nil, nil, authHandler.SendVerifyCode)
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "已被注册") {
		t.Fatalf("existing email verify status=%d body=%s", w.Code, w.Body.String())
	}

	if _, err := db.User.Update().Where(entuser.ID(userEnt.ID)).SetStatus("disabled").Save(ctx); err != nil {
		t.Fatalf("disable user: %v", err)
	}
	coreauth.InvalidateAPIKeyCache("")
	w = invokeHandlerForValidation(http.MethodPost, "/login/api-key", fmt.Sprintf(`{"key":%q}`, key.PlainKey), nil, nil, authHandler.LoginByAPIKey)
	if w.Code != http.StatusForbidden {
		t.Fatalf("disabled user api key login status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestProxyHandlerTestProxyReturnsProbeResultWithSQLite(t *testing.T) {
	ctx := context.Background()
	db := testdb.OpenMemoryEnt(t, "handler_proxy_test_route", schema.WithGlobalUniqueID(false))
	defer func() {
		if err := db.Close(); err != nil {
			t.Fatalf("close db: %v", err)
		}
	}()

	host, port := closedTCPAddress(t)
	proxyEnt, err := db.Proxy.Create().
		SetName("unsupported").
		SetProtocol("http").
		SetAddress(host).
		SetPort(port).
		Save(ctx)
	if err != nil {
		t.Fatalf("create proxy: %v", err)
	}
	handler := NewProxyHandler(appproxy.NewService(store.NewProxyStore(db)))

	w := invokeHandlerForValidation(http.MethodPost, "/proxies/test", "", gin.Params{{Key: "id", Value: intToString(proxyEnt.ID)}}, nil, handler.TestProxy)
	requireOKResponse(t, asResponseView(w.Code, w.Body.String()))
	if !strings.Contains(w.Body.String(), `"success":false`) {
		t.Fatalf("test proxy body = %s", w.Body.String())
	}
}

func startHandlerSMTPServer(t *testing.T) (string, int, <-chan string) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen smtp: %v", err)
	}
	messages := make(chan string, 4)
	done := make(chan struct{})
	t.Cleanup(func() {
		close(done)
		_ = ln.Close()
	})
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				select {
				case <-done:
					return
				default:
					return
				}
			}
			go handleHandlerSMTPConn(conn, messages)
		}
	}()

	host, rawPort, err := net.SplitHostPort(ln.Addr().String())
	if err != nil {
		t.Fatalf("split smtp addr: %v", err)
	}
	port, err := strconv.Atoi(rawPort)
	if err != nil {
		t.Fatalf("parse smtp port: %v", err)
	}
	return host, port, messages
}

func handleHandlerSMTPConn(conn net.Conn, messages chan<- string) {
	defer func() { _ = conn.Close() }()
	reader := bufio.NewReader(conn)
	writer := bufio.NewWriter(conn)
	writeSMTPLine := func(line string) bool {
		if _, err := writer.WriteString(line + "\r\n"); err != nil {
			return false
		}
		return writer.Flush() == nil
	}
	if !writeSMTPLine("220 localhost ESMTP") {
		return
	}

	var data strings.Builder
	inData := false
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return
		}
		line = strings.TrimRight(line, "\r\n")
		if inData {
			if line == "." {
				select {
				case messages <- data.String():
				default:
				}
				data.Reset()
				inData = false
				if !writeSMTPLine("250 OK") {
					return
				}
				continue
			}
			data.WriteString(line)
			data.WriteByte('\n')
			continue
		}
		upper := strings.ToUpper(line)
		switch {
		case strings.HasPrefix(upper, "EHLO"), strings.HasPrefix(upper, "HELO"):
			if _, err := writer.WriteString("250-localhost\r\n250 OK\r\n"); err != nil {
				return
			}
			if err := writer.Flush(); err != nil {
				return
			}
		case strings.HasPrefix(upper, "MAIL FROM:"), strings.HasPrefix(upper, "RCPT TO:"), strings.HasPrefix(upper, "RSET"):
			if !writeSMTPLine("250 OK") {
				return
			}
		case strings.HasPrefix(upper, "DATA"):
			inData = true
			if !writeSMTPLine("354 End data with <CR><LF>.<CR><LF>") {
				return
			}
		case strings.HasPrefix(upper, "QUIT"):
			_ = writeSMTPLine("221 Bye")
			return
		default:
			if !writeSMTPLine("250 OK") {
				return
			}
		}
	}
}

func closedTCPAddress(t *testing.T) (string, int) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen closed address: %v", err)
	}
	host, rawPort, err := net.SplitHostPort(ln.Addr().String())
	if err != nil {
		_ = ln.Close()
		t.Fatalf("split closed addr: %v", err)
	}
	port, err := strconv.Atoi(rawPort)
	if err != nil {
		_ = ln.Close()
		t.Fatalf("parse closed port: %v", err)
	}
	if err := ln.Close(); err != nil {
		t.Fatalf("close reserved listener: %v", err)
	}
	return host, port
}
