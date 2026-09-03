package diagnosticsession

import "strings"

const RedactedValue = "[REDACTED]"

// SensitiveKey is case-insensitive and covers credentials, personal data and
// unbounded content that must never appear in support evidence.
func SensitiveKey(key string) bool {
	k := strings.ToLower(strings.TrimSpace(key))
	for _, token := range []string{"password", "secret", "token", "authorization", "cookie", "email", "phone", "ssn", "salary", "access_key", "private_key"} {
		if strings.Contains(k, token) {
			return true
		}
	}
	return false
}

// Redact recursively copies arbitrary evidence, replacing sensitive map keys.
func Redact(value any) any {
	switch v := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(v))
		for k, x := range v {
			if SensitiveKey(k) {
				out[k] = RedactedValue
			} else {
				out[k] = Redact(x)
			}
		}
		return out
	case []any:
		out := make([]any, len(v))
		for i, x := range v {
			out[i] = Redact(x)
		}
		return out
	default:
		return value
	}
}
