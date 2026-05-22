package redaction_test

import (
	"encoding/json"
	"strings"
	"testing"

	"agent-testbench/internal/domain/redaction"
)

func TestRedactTextMasksSensitiveJSONKeys(t *testing.T) {
	got := redaction.Text(`{"status":"ok","token":"abc","nested":{"password":"secret","id":"item-001"}}`)

	for _, leaked := range []string{"abc", "secret"} {
		if strings.Contains(got, leaked) {
			t.Fatalf("redacted text leaked %q: %s", leaked, got)
		}
	}
	for _, want := range []string{`"token":"[REDACTED]"`, `"password":"[REDACTED]"`, `"id":"item-001"`} {
		if !strings.Contains(got, want) {
			t.Fatalf("redacted text missing %q: %s", want, got)
		}
	}
}

func TestRedactURLMasksSensitiveQueryValues(t *testing.T) {
	got := redaction.URL("http://example.test/items?token=abc&mode=ok&client_secret=hidden")

	for _, leaked := range []string{"abc", "hidden"} {
		if strings.Contains(got, leaked) {
			t.Fatalf("redacted url leaked %q: %s", leaked, got)
		}
	}
	for _, want := range []string{"token=%5BREDACTED%5D", "mode=ok", "client_secret=%5BREDACTED%5D"} {
		if !strings.Contains(got, want) {
			t.Fatalf("redacted url missing %q: %s", want, got)
		}
	}
}

func TestRedactValueMasksNestedEvidencePayloads(t *testing.T) {
	got := redaction.Value(map[string]any{
		"path":      "/alpha?token=query-secret&mode=ok",
		"operation": "POST /alpha?client_secret=hidden",
		"headers": map[string]any{
			"Authorization": "Bearer request-secret",
			"Content-Type":  "application/json",
		},
		"body": `{"ok":true,"password":"response-secret"}`,
	}).(map[string]any)

	serialized := strings.TrimSpace(redaction.Text(mustMarshalString(t, got)))
	for _, leaked := range []string{"query-secret", "hidden", "request-secret", "response-secret"} {
		if strings.Contains(serialized, leaked) {
			t.Fatalf("redacted value leaked %q: %#v", leaked, got)
		}
	}
	for _, want := range []string{"token=%5BREDACTED%5D", "client_secret=%5BREDACTED%5D", `"Authorization":"[REDACTED]"`, `\"password\":\"[REDACTED]\"`} {
		if !strings.Contains(serialized, want) {
			t.Fatalf("redacted value missing %q: %s", want, serialized)
		}
	}
}

func mustMarshalString(t *testing.T, value any) string {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal value: %v", err)
	}
	return string(raw)
}
