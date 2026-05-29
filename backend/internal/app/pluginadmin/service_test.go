package pluginadmin

import (
	"context"
	"strings"
	"testing"

	"github.com/DevilGenius/airgate-core/internal/plugin"
)

func TestReloadRejectsNonDevPlugin(t *testing.T) {
	service := NewService(pluginAdminManagerStub{}, pluginMarketplaceStub{})
	if err := service.Reload(t.Context(), "demo"); err != ErrPluginNotDev {
		t.Fatalf("Reload() error = %v, want %v", err, ErrPluginNotDev)
	}
}

func TestListMarketplaceMarksInstalled(t *testing.T) {
	service := NewService(pluginAdminManagerStub{
		allMeta: []plugin.PluginMeta{{Name: "gateway-openai", Version: "0.2.1"}},
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
	if items[0].InstalledVersion != "0.2.1" {
		t.Fatalf("InstalledVersion = %q, want 0.2.1", items[0].InstalledVersion)
	}
}

func TestListDisplaysDevPluginVersionAsDev(t *testing.T) {
	service := NewService(pluginAdminManagerStub{
		allMeta: []plugin.PluginMeta{{Name: "gateway-openai", Version: "0.2.1", IsDev: true}},
	}, pluginMarketplaceStub{})

	items := service.List()
	if len(items) != 1 || items[0].Version != "dev" {
		t.Fatalf("unexpected plugin list: %+v", items)
	}
}

func TestListDisplaysTaggedPluginWithShortHash(t *testing.T) {
	commitSHA := "1234567890abcdef1234567890abcdef12345678"
	service := NewService(pluginAdminManagerStub{
		allMeta: []plugin.PluginMeta{{
			Name:         "gateway-openai",
			Version:      "0.2.1",
			BinarySHA256: strings.Repeat("a", 64),
			CommitSHA:    commitSHA,
		}},
	}, pluginMarketplaceStub{})

	items := service.List()
	if len(items) != 1 || items[0].Version != "0.2.1-1234567" {
		t.Fatalf("unexpected plugin list: %+v", items)
	}
}

func TestListDerivesInstalledCommitFromMatchingMarketplaceAsset(t *testing.T) {
	assetHash := strings.Repeat("a", 64)
	commitSHA := "abcdef1234567890abcdef1234567890abcdef12"
	service := NewService(pluginAdminManagerStub{
		allMeta: []plugin.PluginMeta{{
			Name:         "gateway-openai",
			Version:      "0.2.1",
			BinarySHA256: assetHash,
		}},
	}, pluginMarketplaceStub{
		listAvailable: func(context.Context) ([]plugin.MarketplacePlugin, error) {
			return []plugin.MarketplacePlugin{{
				Name:      "gateway-openai",
				Version:   "0.2.1",
				SHA256:    assetHash,
				CommitSHA: commitSHA,
			}}, nil
		},
	})

	items := service.List()
	if len(items) != 1 || items[0].Version != "0.2.1-abcdef1" {
		t.Fatalf("unexpected plugin list: %+v", items)
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

func TestListMarketplaceOffersUpdateForSameVersionDifferentHash(t *testing.T) {
	installedHash := strings.Repeat("a", 64)
	latestHash := strings.Repeat("b", 64)
	latestCommit := strings.Repeat("c", 40)
	service := NewService(pluginAdminManagerStub{
		allMeta: []plugin.PluginMeta{{
			Name:         "gateway-openai",
			Version:      "0.2.1",
			BinarySHA256: installedHash,
		}},
	}, pluginMarketplaceStub{
		listAvailable: func(context.Context) ([]plugin.MarketplacePlugin, error) {
			return []plugin.MarketplacePlugin{{
				Name:      "gateway-openai",
				Version:   "0.2.1",
				SHA256:    "sha256:" + latestHash,
				CommitSHA: latestCommit,
			}}, nil
		},
	})

	items, err := service.ListMarketplace(t.Context())
	if err != nil {
		t.Fatalf("ListMarketplace() error = %v", err)
	}
	if len(items) != 1 || !items[0].Installed || !items[0].HasUpdate {
		t.Fatalf("unexpected marketplace items: %+v", items)
	}
	if items[0].Version != "0.2.1" || items[0].DisplayVersion != "0.2.1-ccccccc" {
		t.Fatalf("unexpected market versions: version=%q display=%q", items[0].Version, items[0].DisplayVersion)
	}
	if items[0].InstalledVersion != "0.2.1-aaaaaaa" {
		t.Fatalf("InstalledVersion = %q", items[0].InstalledVersion)
	}
}

func TestListMarketplaceDoesNotOfferUpdateForSameVersionSameHash(t *testing.T) {
	hash := strings.Repeat("a", 64)
	commitSHA := "fedcba9876543210fedcba9876543210fedcba98"
	service := NewService(pluginAdminManagerStub{
		allMeta: []plugin.PluginMeta{{
			Name:         "gateway-openai",
			Version:      "0.2.1",
			BinarySHA256: hash,
		}},
	}, pluginMarketplaceStub{
		listAvailable: func(context.Context) ([]plugin.MarketplacePlugin, error) {
			return []plugin.MarketplacePlugin{{
				Name:      "gateway-openai",
				Version:   "0.2.1",
				SHA256:    hash,
				CommitSHA: commitSHA,
			}}, nil
		},
	})

	items, err := service.ListMarketplace(t.Context())
	if err != nil {
		t.Fatalf("ListMarketplace() error = %v", err)
	}
	if len(items) != 1 || !items[0].Installed || items[0].HasUpdate {
		t.Fatalf("unexpected marketplace items: %+v", items)
	}
	if items[0].InstalledVersion != "0.2.1-fedcba9" || items[0].DisplayVersion != "0.2.1-fedcba9" {
		t.Fatalf("unexpected versions: installed=%q display=%q", items[0].InstalledVersion, items[0].DisplayVersion)
	}
}

func TestListMarketplaceDoesNotOfferHashUpdateForDifferentVersion(t *testing.T) {
	service := NewService(pluginAdminManagerStub{
		allMeta: []plugin.PluginMeta{{
			Name:         "gateway-openai",
			Version:      "0.3.0",
			BinarySHA256: strings.Repeat("a", 64),
		}},
	}, pluginMarketplaceStub{
		listAvailable: func(context.Context) ([]plugin.MarketplacePlugin, error) {
			return []plugin.MarketplacePlugin{{
				Name:    "gateway-openai",
				Version: "0.2.1",
				SHA256:  strings.Repeat("b", 64),
			}}, nil
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
func (s pluginAdminManagerStub) InstallFromGithub(context.Context, string, string) error { return nil }
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
