package plugin

import (
	"fmt"

	"github.com/DevilGenius/airgate-core/ent"
	entproxy "github.com/DevilGenius/airgate-core/ent/proxy"
	appproxy "github.com/DevilGenius/airgate-core/internal/app/proxy"
)

func proxyUsernameFromAccount(account *ent.Account, proxy *ent.Proxy) string {
	if proxy == nil {
		return ""
	}
	if proxy.Mode == entproxy.ModeGroup && account != nil && account.ProxySlot != nil {
		return appproxy.FormatSlot(*account.ProxySlot)
	}
	return proxy.Username
}

func buildProxyURLFromEnt(account *ent.Account) string {
	if account == nil || account.Edges.Proxy == nil {
		return ""
	}
	proxy := account.Edges.Proxy
	username := proxyUsernameFromAccount(account, proxy)
	if username != "" {
		return fmt.Sprintf("%s://%s:%s@%s:%d", proxy.Protocol, username, proxy.Password, proxy.Address, proxy.Port)
	}
	return fmt.Sprintf("%s://%s:%d", proxy.Protocol, proxy.Address, proxy.Port)
}
