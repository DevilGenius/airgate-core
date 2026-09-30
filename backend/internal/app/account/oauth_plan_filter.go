package account

import (
	"strings"

	"github.com/DevilGenius/airgate-core/internal/plantype"
	"github.com/DevilGenius/airgate-core/internal/plugin"
)

const (
	oauthPlanFilterPrefix      = "oauth_plan:"
	defaultOAuthPlanCredential = "plan_type"
)

type oauthPlanFilter struct {
	Platform      string
	Key           string
	Label         string
	CredentialKey string
	MatchMode     string
	Matches       []string
	KnownPlans    []CredentialStringFilter
}

func oauthPlanFilterID(platform, key string) string {
	return oauthPlanFilterPrefix + platform + ":" + key
}

func parseOAuthPlanFilterID(value string) (platform string, key string, ok bool) {
	if !strings.HasPrefix(value, oauthPlanFilterPrefix) {
		return "", "", false
	}
	rest := strings.TrimPrefix(value, oauthPlanFilterPrefix)
	parts := strings.SplitN(rest, ":", 2)
	if len(parts) != 2 {
		return "", "", false
	}
	platform = strings.TrimSpace(parts[0])
	key = strings.TrimSpace(parts[1])
	return platform, key, platform != "" && key != ""
}

func pluginOAuthPlanFilters(meta plugin.PluginMeta) []oauthPlanFilter {
	if meta.Platform == "" {
		return nil
	}
	definitions := plantype.ResolveFilters(meta.AccountPlans)
	result := make([]oauthPlanFilter, 0, len(definitions))
	for _, item := range definitions {
		plan := oauthPlanFilter{Platform: meta.Platform, Key: item.Key, Label: item.Label, CredentialKey: item.CredentialKey, MatchMode: item.MatchMode, Matches: item.Matches}
		if item.MatchMode == "unknown" {
			for _, known := range definitions {
				if known.Known() && known.CredentialKey == item.CredentialKey {
					plan.KnownPlans = append(plan.KnownPlans, CredentialStringFilter{Key: known.CredentialKey, Values: known.Matches, MatchMode: known.MatchMode})
				}
			}
		}
		result = append(result, plan)
	}
	return result
}

func (s *Service) resolveOAuthPlanFilter(value string) (oauthPlanFilter, bool) {
	platform, key, ok := parseOAuthPlanFilterID(value)
	if !ok || s.plugins == nil {
		return oauthPlanFilter{}, false
	}
	for _, meta := range s.plugins.GetAllPluginMeta() {
		if meta.Platform != platform {
			continue
		}
		for _, plan := range pluginOAuthPlanFilters(meta) {
			if plan.Key == key {
				return plan, true
			}
		}
	}
	return oauthPlanFilter{}, false
}

func (s *Service) normalizeListFilter(filter ListFilter) ListFilter {
	values := splitCommaValues(filter.AccountType)
	if len(values) == 0 {
		return filter
	}
	types := make([]string, 0, len(values))
	credentials := make([]CredentialStringFilter, 0, len(values))
	for _, value := range values {
		plan, ok := s.resolveOAuthPlanFilter(value)
		if !ok {
			types = append(types, value)
			continue
		}
		credentials = append(credentials, CredentialStringFilter{
			Platform:    plan.Platform,
			AccountType: "oauth",
			Key:         plan.CredentialKey,
			Values:      plan.Matches,
			MatchMode:   plan.MatchMode,
			KnownPlans:  plan.KnownPlans,
		})
	}
	filter.AccountType = strings.Join(types, ",")
	filter.Credentials = append(filter.Credentials, credentials...)
	return filter
}

// splitCommaValues 拆分逗号分隔的筛选值，去空白、去空项、去重并保持顺序。
func splitCommaValues(raw string) []string {
	if raw == "" {
		return nil
	}
	seen := make(map[string]struct{})
	values := make([]string, 0)
	for _, part := range strings.Split(raw, ",") {
		value := strings.TrimSpace(part)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		values = append(values, value)
	}
	return values
}

// accountPlatformMatches 判断账号平台是否命中逗号分隔的平台并集；空并集表示不筛选。
func accountPlatformMatches(platform string, filterPlatforms []string) bool {
	if len(filterPlatforms) == 0 {
		return true
	}
	for _, filterPlatform := range filterPlatforms {
		if platform == filterPlatform {
			return true
		}
	}
	return false
}
