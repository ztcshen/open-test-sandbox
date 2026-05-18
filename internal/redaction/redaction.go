package redaction

import (
	"encoding/json"
	"net/url"
	"strings"
)

const Mask = "[REDACTED]"

var sensitiveKeys = []string{
	"authorization",
	"client_secret",
	"cookie",
	"password",
	"secret",
	"set-cookie",
	"token",
}

func Text(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return value
	}
	var decoded any
	if err := json.Unmarshal([]byte(value), &decoded); err != nil {
		return value
	}
	redacted := redactValue(decoded)
	raw, err := json.Marshal(redacted)
	if err != nil {
		return value
	}
	return string(raw)
}

func URL(value string) string {
	parsed, err := url.Parse(value)
	if err != nil {
		return value
	}
	query := parsed.Query()
	for key := range query {
		if isSensitiveKey(key) {
			query.Set(key, Mask)
		}
	}
	parsed.RawQuery = query.Encode()
	return parsed.String()
}

func redactValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(typed))
		for key, item := range typed {
			if isSensitiveKey(key) {
				out[key] = Mask
				continue
			}
			out[key] = redactValue(item)
		}
		return out
	case []any:
		out := make([]any, 0, len(typed))
		for _, item := range typed {
			out = append(out, redactValue(item))
		}
		return out
	default:
		return value
	}
}

func isSensitiveKey(key string) bool {
	normalized := strings.ToLower(strings.TrimSpace(key))
	normalized = strings.ReplaceAll(normalized, "-", "_")
	for _, sensitive := range sensitiveKeys {
		candidate := strings.ReplaceAll(sensitive, "-", "_")
		if normalized == candidate || strings.Contains(normalized, candidate) {
			return true
		}
	}
	return false
}
