package auth

import (
	"github.com/pquerna/otp/totp"
)

const totpIssuer = "AirGate"

// GenerateSecret 生成 TOTP 密钥
// 返回 Base32 编码的密钥和 otpauth:// URI
func GenerateSecret(email string) (secret, uri string, err error) {
	key, err := totp.Generate(totp.GenerateOpts{
		Issuer:      totpIssuer,
		AccountName: email,
	})
	if err != nil {
		return "", "", err
	}
	return key.Secret(), key.URL(), nil
}

// ValidateCode 验证 TOTP 验证码
func ValidateCode(secret, code string) bool {
	return totp.Validate(code, secret)
}
