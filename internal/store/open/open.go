package open

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"agent-testbench/internal/store"
	"agent-testbench/internal/store/mysql"
	"agent-testbench/internal/store/postgres"
	"agent-testbench/internal/store/sqlite"
	"agent-testbench/internal/store/sqlstore"
)

type Backend string

const (
	BackendSQLite   Backend = "sqlite"
	BackendPostgres Backend = "postgres"
	BackendMySQL    Backend = "mysql"
)

var ErrBackendUnavailable = errors.New("store backend unavailable")

func BackendFromReference(reference string) (Backend, error) {
	reference = strings.TrimSpace(reference)
	if reference == "" {
		return "", errors.New("store reference is required")
	}
	parsed, err := url.Parse(reference)
	if err != nil || parsed.Scheme == "" {
		return "", fmt.Errorf("store reference must be a DSN with an explicit backend scheme: %q", reference)
	}
	dialect, err := sqlstore.DialectFromReference(reference)
	if err != nil {
		return "", err
	}
	switch dialect.Name() {
	case "sqlite":
		return BackendSQLite, nil
	case "postgres":
		return BackendPostgres, nil
	case "mysql":
		return BackendMySQL, nil
	default:
		return "", fmt.Errorf("unsupported store backend %q", dialect.Name())
	}
}

func Open(ctx context.Context, reference string) (store.Store, error) {
	backend, err := BackendFromReference(reference)
	if err != nil {
		return nil, err
	}
	switch backend {
	case BackendSQLite:
		cfg, err := sqlite.ParseConfigFromURL(reference)
		if err != nil {
			return nil, err
		}
		return sqlite.Open(ctx, cfg)
	case BackendPostgres:
		cfg, err := postgres.ParseConfigFromURL(reference)
		if err != nil {
			return nil, err
		}
		return postgres.Open(ctx, cfg)
	case BackendMySQL:
		cfg, err := mysql.ParseConfigFromURL(reference)
		if err != nil {
			return nil, err
		}
		return mysql.Open(ctx, cfg)
	default:
		return nil, fmt.Errorf("unsupported store backend %q", backend)
	}
}
