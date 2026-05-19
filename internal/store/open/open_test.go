package open_test

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	storeopen "open-test-sandbox/internal/store/open"
)

func TestBackendFromReferenceRecognizesSupportedDatabaseFamilies(t *testing.T) {
	tests := []struct {
		name string
		ref  string
		want storeopen.Backend
	}{
		{name: "sqlite dsn", ref: "sqlite:///tmp/store.sqlite", want: storeopen.BackendSQLite},
		{name: "file dsn", ref: "file:/tmp/store.sqlite", want: storeopen.BackendSQLite},
		{name: "postgres dsn", ref: "postgres://user:pass@localhost:5432/otsandbox", want: storeopen.BackendPostgres},
		{name: "postgresql dsn", ref: "postgresql://user:pass@localhost:5432/otsandbox", want: storeopen.BackendPostgres},
		{name: "mysql dsn", ref: "mysql://user:pass@localhost:3306/otsandbox", want: storeopen.BackendMySQL},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := storeopen.BackendFromReference(tt.ref)
			if err != nil {
				t.Fatalf("backend from reference: %v", err)
			}
			if got != tt.want {
				t.Fatalf("backend = %q want %q", got, tt.want)
			}
		})
	}
}

func TestBackendFromReferenceRequiresExplicitBackendScheme(t *testing.T) {
	for _, ref := range []string{"", filepath.Join("runtime", "store.sqlite")} {
		t.Run(ref, func(t *testing.T) {
			_, err := storeopen.BackendFromReference(ref)
			if err == nil {
				t.Fatalf("expected %q to require an explicit backend scheme", ref)
			}
		})
	}
}

func TestOpenRejectsRecognizedButUnavailableBackendWithClearError(t *testing.T) {
	_, err := storeopen.Open(context.Background(), "mysql://user:pass@localhost:3306/otsandbox")
	if err == nil {
		t.Fatal("expected mysql backend to be recognized but unavailable")
	}
	if !errors.Is(err, storeopen.ErrBackendUnavailable) || !strings.Contains(err.Error(), "mysql") {
		t.Fatalf("mysql unavailable error = %v", err)
	}
}
