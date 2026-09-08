package server

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/DevilGenius/airgate-core/internal/billing"
	"github.com/DevilGenius/airgate-core/internal/config"
	"github.com/DevilGenius/airgate-core/internal/plugin"
)

func TestShutdownCancelsResidualHTTPBeforeStoppingRecorder(t *testing.T) {
	root, cancelHTTP := context.WithCancel(t.Context())
	defer cancelHTTP()
	entered, ended := make(chan struct{}), make(chan struct{})
	httpServer := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { close(entered); <-r.Context().Done(); close(ended) }))
	httpServer.Config.BaseContext = func(net.Listener) context.Context { return root }
	httpServer.Start()
	defer httpServer.Close()
	clientDone := make(chan struct{})
	go func() {
		defer close(clientDone)
		resp, err := httpServer.Client().Get(httpServer.URL)
		if err == nil {
			_ = resp.Body.Close()
		}
	}()
	<-entered
	recorder := billing.NewRecorder(nil, 1)
	recorder.Start()
	s := &Server{cfg: &config.Config{Plugins: config.PluginsConfig{Marketplace: config.MarketplaceConfig{Disabled: true}}}, srv: httpServer.Config, httpCancel: cancelHTTP, recorder: recorder, pluginMgr: plugin.NewManager(t.TempDir(), "", "", nil)}
	ctx, cancel := context.WithTimeout(t.Context(), 300*time.Millisecond)
	defer cancel()
	started := time.Now()
	_ = s.Shutdown(ctx)
	if time.Since(started) > 500*time.Millisecond {
		t.Fatal("shutdown ignored shared deadline")
	}
	select {
	case <-ended:
	case <-time.After(time.Second):
		t.Fatal("residual HTTP context remained active")
	}
	<-clientDone
	if _, err := recorder.Reserve(); err == nil {
		t.Fatal("new billing producer admitted after shutdown")
	}
}
