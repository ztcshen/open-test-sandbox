package redaction_test

import (
	"strings"
	"testing"

	"open-test-sandbox/internal/redaction"
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
