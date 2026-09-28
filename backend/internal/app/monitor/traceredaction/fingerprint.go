package traceredaction

import (
	"encoding/hex"
	"net/http"
	"strings"

	"github.com/zeebo/xxh3"
)

const HashAlgorithm = "xxh3-128"

func Hash(value []byte) string {
	digest := xxh3.Hash128(value).Bytes()
	return hex.EncodeToString(digest[:])
}

// HeaderFingerprints gives canonical aliases a stable precedence, independent
// of Go map iteration. Both sides use the same multi-value encoding.
func HeaderFingerprints(headers http.Header) map[string]string {
	var out map[string]string
	for _, field := range identityHeaders {
		for _, alias := range field.aliases {
			values, exists := headerValues(headers, alias)
			if !exists {
				continue
			}
			key, digest, _ := HeaderFingerprint(alias, values)
			if out == nil {
				out = make(map[string]string)
			}
			out[key] = digest
			break
		}
	}
	return out
}

func headerValues(headers http.Header, name string) ([]string, bool) {
	if values, ok := headers[http.CanonicalHeaderKey(name)]; ok {
		return values, true
	}
	if values, ok := headers[name]; ok {
		return values, true
	}
	for key, values := range headers {
		if strings.EqualFold(strings.TrimSpace(key), name) {
			return values, true
		}
	}
	return nil, false
}

func HashString(value string) string {
	digest := xxh3.HashString128(value).Bytes()
	return hex.EncodeToString(digest[:])
}

// HeaderFingerprint is shared by ingress persistence and outbound capture.
// Header aliases identify the same field; values retain their order and bytes.
func HeaderFingerprint(name string, values []string) (key, digest string, ok bool) {
	name = strings.ToLower(strings.TrimSpace(name))
	for _, field := range identityHeaders {
		for _, alias := range field.aliases {
			if name == alias {
				return "x-airgate-trace-" + field.name + "-" + HashAlgorithm, HashString(strings.Join(values, "\x00")), true
			}
		}
	}
	return "", "", false
}
