package sqlstore

import (
	"encoding/json"
	"strings"
	"time"
)

func utcNow() time.Time {
	return time.Now().UTC()
}

func dbTimeArg(d Dialect, t time.Time) any {
	if t.IsZero() {
		if d.Name() == "sqlite" {
			return ""
		}
		return nil
	}
	if d.Name() == "sqlite" {
		return encodeTime(t)
	}
	return t.UTC()
}

func encodeTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339Nano)
}

func stringDefault(value string, defaultValue string) string {
	if strings.TrimSpace(value) == "" {
		return defaultValue
	}
	return value
}

func decodeTime(value string) time.Time {
	if value == "" {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return time.Time{}
	}
	return t
}

func decodeDBTime(value any) time.Time {
	switch v := value.(type) {
	case nil:
		return time.Time{}
	case time.Time:
		return v.UTC()
	case string:
		return decodeTime(v)
	case []byte:
		return decodeTime(string(v))
	default:
		return time.Time{}
	}
}

func normalizeJSONText(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return value
	}
	var decoded any
	if err := json.Unmarshal([]byte(value), &decoded); err != nil {
		return value
	}
	encoded, err := json.Marshal(decoded)
	if err != nil {
		return value
	}
	return string(encoded)
}
