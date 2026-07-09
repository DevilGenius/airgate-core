package accountidentity

import (
	"errors"
	"net/mail"
	"strings"
)

var ErrInvalidEmail = errors.New("invalid account email")

var ErrEmailMismatch = errors.New("account email mismatch")

func Normalize(value string) (string, error) {
	normalized := strings.ToLower(strings.TrimSpace(value))
	if normalized == "" {
		return "", nil
	}

	parsed, err := mail.ParseAddress(normalized)
	if err != nil || parsed.Address != normalized {
		return "", ErrInvalidEmail
	}
	return normalized, nil
}

// NormalizeOptional 规范化可空邮箱。空字符串与纯空白统一为 nil。
func NormalizeOptional(value *string) (*string, error) {
	if value == nil {
		return nil, nil
	}
	normalized, err := Normalize(*value)
	if err != nil {
		return nil, err
	}
	if normalized == "" {
		return nil, nil
	}
	return &normalized, nil
}

// CredentialEmail 读取 credentials.email，并保留 key 是否显式出现的信息。
func CredentialEmail(credentials map[string]string) (*string, bool, error) {
	if credentials == nil {
		return nil, false, nil
	}
	raw, ok := credentials["email"]
	if !ok {
		return nil, false, nil
	}
	normalized, err := NormalizeOptional(&raw)
	if err != nil {
		return nil, true, err
	}
	return normalized, true, nil
}

// SyncCredentials 克隆 credentials，并使 credentials.email 与顶层 email 一致。
func SyncCredentials(credentials map[string]string, email *string) map[string]string {
	if credentials == nil && email == nil {
		return nil
	}
	synced := make(map[string]string, len(credentials)+1)
	for key, value := range credentials {
		if key != "email" {
			synced[key] = value
		}
	}
	if email != nil {
		synced["email"] = *email
	}
	return synced
}

// Resolve 合并同时提交的顶层 email 与 credentials.email。
// 只提供一侧时自动补齐；两侧均提供且不一致时返回 ErrEmailMismatch。
func Resolve(email *string, credentials map[string]string) (*string, map[string]string, error) {
	topLevel, err := NormalizeOptional(email)
	if err != nil {
		return nil, nil, err
	}
	credentialEmail, credentialEmailPresent, err := CredentialEmail(credentials)
	if err != nil {
		return nil, nil, err
	}
	if credentialEmailPresent && !equalOptional(topLevel, credentialEmail) && email != nil {
		return nil, nil, ErrEmailMismatch
	}
	resolved := topLevel
	if resolved == nil && email == nil {
		resolved = credentialEmail
	}
	return resolved, SyncCredentials(credentials, resolved), nil
}

// ResolveUpdate 根据当前身份和更新字段计算最终一致状态。
// hasEmail 用于区分顶层 email 未提交与显式清空；credentials 中 email key 的存在性
// 同样用于区分“沿用当前邮箱”和“从 credentials 侧显式设置/清空”。
func ResolveUpdate(
	currentEmail *string,
	currentCredentials map[string]string,
	requestedEmail *string,
	hasEmail bool,
	requestedCredentials map[string]string,
) (*string, map[string]string, error) {
	resolvedCurrent, _, err := Resolve(currentEmail, currentCredentials)
	if err != nil {
		return nil, nil, err
	}

	if hasEmail {
		topLevel, err := NormalizeOptional(requestedEmail)
		if err != nil {
			return nil, nil, err
		}
		if requestedCredentials == nil {
			return topLevel, nonNilCredentials(SyncCredentials(currentCredentials, topLevel)), nil
		}
		credentialEmail, present, err := CredentialEmail(requestedCredentials)
		if err != nil {
			return nil, nil, err
		}
		if present && !equalOptional(topLevel, credentialEmail) {
			return nil, nil, ErrEmailMismatch
		}
		return topLevel, nonNilCredentials(SyncCredentials(requestedCredentials, topLevel)), nil
	}

	credentialEmail, present, err := CredentialEmail(requestedCredentials)
	if err != nil {
		return nil, nil, err
	}
	if present {
		return credentialEmail, nonNilCredentials(SyncCredentials(requestedCredentials, credentialEmail)), nil
	}
	return resolvedCurrent, nonNilCredentials(SyncCredentials(requestedCredentials, resolvedCurrent)), nil
}

func equalOptional(left, right *string) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}

func nonNilCredentials(credentials map[string]string) map[string]string {
	if credentials == nil {
		return map[string]string{}
	}
	return credentials
}
