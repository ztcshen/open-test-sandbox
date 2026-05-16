package controlplane

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"open-test-sandbox/internal/profile"
)

func TestBuildCaseHTTPRequestAddsConfiguredAuthorization(t *testing.T) {
	keyPath := writeTestSigningKey(t)
	execution := caseExecutionConfig{
		Method: "POST",
		NodeID: "service.alpha",
		Path:   "/v1/items",
		Query:  map[string]any{"order_id": "ORDER-1"},
		Body:   map[string]any{"amount": 100},
		Auth: map[string]any{
			"credentialId":     "credential-1",
			"keyPath":          keyPath,
			"providerSerialNo": "provider-1",
			"serialNo":         "serial-1",
		},
		Signed: true,
	}

	request, err := buildCaseHTTPRequest(profile.Bundle{}, execution, map[string]any{"baseUrl": "http://127.0.0.1:1234"})
	if err != nil {
		t.Fatalf("build request: %v", err)
	}

	auth := request.headers["Authorization"]
	if !strings.HasPrefix(auth, "RSA ") {
		t.Fatalf("authorization header = %q", auth)
	}
	for _, want := range []string{`credential_id="credential-1"`, `serial_no="serial-1"`, `provider_serial_no="provider-1"`} {
		if !strings.Contains(auth, want) {
			t.Fatalf("authorization header %q does not contain %s", auth, want)
		}
	}
	if request.headers["X-Forwarded-For"] == "" || request.headers["X-Real-IP"] == "" {
		t.Fatalf("expected default forwarding headers, got %#v", request.headers)
	}
}

func writeTestSigningKey(t *testing.T) string {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	raw := x509.MarshalPKCS1PrivateKey(key)
	path := filepath.Join(t.TempDir(), "request-key.pem")
	file, err := os.Create(path)
	if err != nil {
		t.Fatalf("create key: %v", err)
	}
	defer file.Close()
	if err := pem.Encode(file, &pem.Block{Type: "RSA PRIVATE KEY", Bytes: raw}); err != nil {
		t.Fatalf("write key: %v", err)
	}
	return path
}
