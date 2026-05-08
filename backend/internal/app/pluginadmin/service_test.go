package pluginadmin

import (
	"context"
	"testing"

	"github.com/DouDOU-start/airgate-core/internal/plugin"
)

func TestReloadRejectsNonDevPlugin(t *testing.T) {
	service := NewService(pluginAdminManagerStub{}, pluginMarketplaceStub{})
	if err := service.Reload(t.Context(), "demo"); err != ErrPluginNotDev {
		t.Fatalf("Reload() error = %v, want %v", err, ErrPluginNotDev)
	}
}

func TestListMarketplaceMarksInstalled(t *testing.T) {
	service := NewService(pluginAdminManagerStub{
		allMeta: []plugin.PluginMeta{{Name: "gateway-openai"}},
	}, pluginMarketplaceStub{
		listAvailable: func(context.Context) ([]plugin.MarketplacePlugin, error) {
			return []plugin.MarketplacePlugin{{Name: "gateway-openai"}, {Name: "gateway-gemini"}}, nil
		},
	})

	items, err := service.ListMarketplace(t.Context())
	if err != nil {
		t.Fatalf("ListMarketplace() error = %v", err)
	}
	if len(items) != 2 || !items[0].Installed || items[1].Installed {
		t.Fatalf("unexpected marketplace items: %+v", items)
	}
}

func TestListMarketplaceDoesNotOfferUpdatesForDevPlugin(t *testing.T) {
	service := NewService(pluginAdminManagerStub{
		allMeta: []plugin.PluginMeta{{Name: "airgate-playground", Version: "0.1.0", IsDev: true}},
	}, pluginMarketplaceStub{
		listAvailable: func(context.Context) ([]plugin.MarketplacePlugin, error) {
			return []plugin.MarketplacePlugin{{Name: "airgate-playground", Version: "0.1.10"}}, nil
		},
	})

	items, err := service.ListMarketplace(t.Context())
	if err != nil {
		t.Fatalf("ListMarketplace() error = %v", err)
	}
	if len(items) != 1 || !items[0].Installed || items[0].HasUpdate {
		t.Fatalf("unexpected marketplace items: %+v", items)
	}
}

type pluginAdminManagerStub struct {
	allMeta []plugin.PluginMeta
}

func (s pluginAdminManagerStub) GetAllPluginMeta() []plugin.PluginMeta {
	return append([]plugin.PluginMeta(nil), s.allMeta...)
}
func (s pluginAdminManagerStub) InstallFromBinary(context.Context, string, []byte) error { return nil }
func (s pluginAdminManagerStub) InstallFromGithub(context.Context, string) error         { return nil }
func (s pluginAdminManagerStub) Uninstall(context.Context, string) error                 { return nil }
func (s pluginAdminManagerStub) ReloadDev(context.Context, string) error                 { return nil }
func (s pluginAdminManagerStub) ReloadInstance(context.Context, string) error            { return nil }
func (s pluginAdminManagerStub) IsDev(string) bool                                       { return false }
func (s pluginAdminManagerStub) GetInstance(string) *plugin.PluginInstance               { return nil }
func (s pluginAdminManagerStub) GetPluginConfig(context.Context, string) (map[string]string, error) {
	return nil, nil
}
func (s pluginAdminManagerStub) UpdatePluginConfig(context.Context, string, map[string]string) error {
	return nil
}

type pluginMarketplaceStub struct {
	listAvailable func(context.Context) ([]plugin.MarketplacePlugin, error)
}

func (s pluginMarketplaceStub) ListAvailable(ctx context.Context) ([]plugin.MarketplacePlugin, error) {
	if s.listAvailable == nil {
		return nil, nil
	}
	return s.listAvailable(ctx)
}

func (s pluginMarketplaceStub) SyncFromGithub(context.Context) error {
	return nil
}
