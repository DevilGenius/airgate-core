package account

import (
	"encoding/json"
	"strings"

	"github.com/DevilGenius/airgate-core/internal/plugin"
)

const (
	oauthPlanFilterPrefix      = "oauth_plan:"
	oauthPlanMetadataKey       = "account.oauth_plans"
	defaultOAuthPlanCredential = "plan_type"
)

type oauthPlanFilterMeta struct {
	Key           string   `json:"key"`
	Label         string   `json:"label"`
	CredentialKey string   `json:"credential_key"`
	MatchMode     string   `json:"match"`
	Matches       []string `json:"matches"`
}

type oauthPlanFilter struct {
	Platform      string
	Key           string
	Label         string
	CredentialKey string
	MatchMode     string
	Matches       []string
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
	raw := strings.TrimSpace(meta.Metadata[oauthPlanMetadataKey])
	if raw == "" || meta.Platform == "" {
		return nil
	}

	var declared []oauthPlanFilterMeta
	if err := json.Unmarshal([]byte(raw), &declared); err != nil {
		return nil
	}

	result := make([]oauthPlanFilter, 0, len(declared))
	for _, item := range declared {
		key := strings.TrimSpace(item.Key)
		if key == "" {
			continue
		}
		credentialKey := strings.TrimSpace(item.CredentialKey)
		if credentialKey == "" {
			credentialKey = defaultOAuthPlanCredential
		}
		matches := normalizedPlanMatches(item.Matches, key)
		if len(matches) == 0 {
			continue
		}
		matchMode := strings.ToLower(strings.TrimSpace(item.MatchMode))
		if matchMode != "contains" {
			matchMode = "exact"
		}
		label := strings.TrimSpace(item.Label)
		if label == "" {
			label = key
		}
		result = append(result, oauthPlanFilter{
			Platform:      meta.Platform,
			Key:           key,
			Label:         label,
			CredentialKey: credentialKey,
			MatchMode:     matchMode,
			Matches:       matches,
		})
	}
	return result
}

func normalizedPlanMatches(values []string, fallback string) []string {
	if len(values) == 0 {
		values = []string{fallback}
	}
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
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
