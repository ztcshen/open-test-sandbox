package profile_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"open-test-sandbox/internal/profile"
)

func TestLoadEmptyProfileBundle(t *testing.T) {
	bundle, err := profile.Load(filepath.Join("..", "..", "profiles", "empty"))
	if err != nil {
		t.Fatalf("load empty profile: %v", err)
	}

	if bundle.ID != "empty" || bundle.DisplayName != "Empty Profile" {
		t.Fatalf("bundle identity = %#v", bundle)
	}
	if bundle.Counts().Workflows != 0 || bundle.Counts().APICases != 0 || bundle.Counts().Fixtures != 0 {
		t.Fatalf("empty bundle counts = %#v", bundle.Counts())
	}
}

func TestLoadProfileReturnsActionableValidationErrors(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "profile.json"), []byte(`{"displayName":"Missing ID"}`), 0o644); err != nil {
		t.Fatalf("write invalid profile: %v", err)
	}

	_, err := profile.Load(dir)
	if err == nil || !strings.Contains(err.Error(), "profile id is required") {
		t.Fatalf("load invalid profile error = %v", err)
	}
}
