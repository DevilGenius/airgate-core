package plugin

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/DevilGenius/airgate-core/internal/auth"
	"github.com/DevilGenius/airgate-core/internal/config"
)

// Opt-in: replaces the three running local development gateways. It never
// sends a request to an upstream model or logs credentials or API responses.
func TestLocalGatewayGenerationReload(t *testing.T) {
	path := os.Getenv("AIRGATE_GENERATION_CONFIG")
	if path == "" {
		t.Skip("set AIRGATE_GENERATION_CONFIG to opt in to local gateway reloads")
	}
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatal("cannot load local config")
	}
	token, err := auth.NewJWTManager(cfg.JWT.Secret, 1).GenerateToken(1, "admin", "")
	if err != nil {
		t.Fatal("cannot create local test identity")
	}
	client := &http.Client{Timeout: 3 * time.Minute}
	request := func(method, path string, decode any) {
		t.Helper()
		req, err := http.NewRequestWithContext(t.Context(), method, "http://127.0.0.1:9517"+path, nil)
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Authorization", "Bearer "+token)
		resp, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("%s: HTTP %d", path, resp.StatusCode)
		}
		if decode != nil {
			if err := json.NewDecoder(io.LimitReader(resp.Body, 2<<20)).Decode(decode); err != nil {
				t.Fatal(err)
			}
		} else {
			_, _ = io.Copy(io.Discard, resp.Body)
		}
	}
	type row struct {
		Name        string `json:"name"`
		Generation  string `json:"generation"`
		UpdateState string `json:"update_state"`
	}
	list := func() map[string]row {
		var envelope struct {
			Data struct {
				List []row `json:"list"`
			} `json:"data"`
		}
		request(http.MethodGet, "/api/v1/admin/plugins", &envelope)
		result := map[string]row{}
		for _, p := range envelope.Data.List {
			result[p.Name] = p
		}
		return result
	}
	before := list()
	ctx, cancel := context.WithCancel(context.Background())
	var healthFailures, healthChecks atomic.Int64
	done := make(chan struct{})
	go func() {
		defer close(done)
		hc := &http.Client{Timeout: time.Second}
		ticker := time.NewTicker(20 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
			req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "http://127.0.0.1:9517/healthz", nil)
			resp, err := hc.Do(req)
			if ctx.Err() != nil {
				if resp != nil {
					_ = resp.Body.Close()
				}
				return
			}
			healthChecks.Add(1)
			if err != nil {
				healthFailures.Add(1)
				continue
			}
			_, _ = io.Copy(io.Discard, resp.Body)
			_ = resp.Body.Close()
			if resp.StatusCode != 200 {
				healthFailures.Add(1)
			}
		}
	}()
	defer func() { cancel(); <-done }()
	for _, name := range []string{"gateway-openai", "gateway-claude", "gateway-kiro"} {
		if before[name].Generation == "" {
			t.Fatalf("%s has no running generation", name)
		}
		started := time.Now()
		request(http.MethodPost, "/api/v1/admin/plugins/"+name+"/reload", nil)
		after := list()[name]
		if after.Generation == "" || after.Generation == before[name].Generation {
			t.Fatalf("%s generation did not change", name)
		}
		request(http.MethodGet, "/plugins/"+name+"/assets/index.js", nil)
		t.Logf("%s generation replaced; assets 200; elapsed %s", name, time.Since(started).Round(time.Millisecond))
	}
	cancel()
	<-done
	if healthFailures.Load() != 0 {
		t.Fatalf("health failed %d/%d times", healthFailures.Load(), healthChecks.Load())
	}
	t.Logf("all %d health probes remained HTTP 200", healthChecks.Load())
}
