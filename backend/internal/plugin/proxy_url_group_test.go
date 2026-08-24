package plugin

import (
	"testing"

	"github.com/DevilGenius/airgate-core/ent"
	entproxy "github.com/DevilGenius/airgate-core/ent/proxy"
)

func TestBuildProxyURLFromEntUsesGroupSlot(t *testing.T) {
	slot := 0x2a
	account := &ent.Account{
		ProxySlot: &slot,
		Edges: ent.AccountEdges{Proxy: &ent.Proxy{
			Mode: entproxy.ModeGroup, Protocol: entproxy.ProtocolHTTP,
			Address: "proxy.example.com", Port: 8080, Username: "ignored", Password: "secret",
		}},
	}
	if got := buildProxyURLFromEnt(account); got != "http://002a:secret@proxy.example.com:8080" {
		t.Fatalf("proxy URL = %q", got)
	}
}
