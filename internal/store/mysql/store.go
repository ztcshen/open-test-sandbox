package mysql

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"open-test-sandbox/internal/store/sqlstore"

	mysqlDriver "github.com/go-sql-driver/mysql"
)

type Config struct {
	URL        string
	DSN        string
	DriverName string
}

type Store struct {
	*sqlstore.Store
}

type SchemaStatusResult struct {
	URL            string
	CurrentVersion int
	TargetVersion  int
	AppliedCount   int
}

func (r SchemaStatusResult) HasPending() bool {
	return r.CurrentVersion < r.TargetVersion
}

func ParseConfigFromURL(storeURL string) (Config, error) {
	storeURL = strings.TrimSpace(storeURL)
	if storeURL == "" {
		return Config{}, errors.New("mysql store url is required")
	}
	parsed, err := url.Parse(storeURL)
	if err != nil {
		return Config{}, err
	}
	if strings.ToLower(parsed.Scheme) != "mysql" {
		return Config{}, fmt.Errorf("unsupported mysql store backend %q", parsed.Scheme)
	}
	dsn, err := driverDSNFromURL(parsed)
	if err != nil {
		return Config{}, err
	}
	return Config{URL: storeURL, DSN: dsn, DriverName: sqlstore.MySQLDialect{}.DriverName()}, nil
}

func Open(ctx context.Context, cfg Config) (*Store, error) {
	db, err := openDB(ctx, cfg)
	if err != nil {
		return nil, err
	}
	if _, err := sqlstore.UpgradeSchema(ctx, db, sqlstore.MySQLDialect{}); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("upgrade mysql store schema: %w", err)
	}
	return &Store{Store: sqlstore.New(db, sqlstore.MySQLDialect{})}, nil
}

func SchemaStatus(ctx context.Context, cfg Config) (SchemaStatusResult, error) {
	db, err := openDB(ctx, cfg)
	if err != nil {
		return SchemaStatusResult{}, err
	}
	defer db.Close()
	status, err := sqlstore.SchemaStatus(ctx, db, sqlstore.MySQLDialect{})
	if err != nil {
		return SchemaStatusResult{}, err
	}
	return SchemaStatusResult{URL: cfg.URL, CurrentVersion: status.CurrentVersion, TargetVersion: status.TargetVersion, AppliedCount: status.AppliedCount}, nil
}

func UpgradeSchema(ctx context.Context, cfg Config) (SchemaStatusResult, error) {
	db, err := openDB(ctx, cfg)
	if err != nil {
		return SchemaStatusResult{}, err
	}
	defer db.Close()
	status, err := sqlstore.UpgradeSchema(ctx, db, sqlstore.MySQLDialect{})
	if err != nil {
		return SchemaStatusResult{}, err
	}
	return SchemaStatusResult{URL: cfg.URL, CurrentVersion: status.CurrentVersion, TargetVersion: status.TargetVersion, AppliedCount: status.AppliedCount}, nil
}

func openDB(ctx context.Context, cfg Config) (*sql.DB, error) {
	driverName := strings.TrimSpace(cfg.DriverName)
	if driverName == "" {
		driverName = sqlstore.MySQLDialect{}.DriverName()
	}
	dsn := strings.TrimSpace(cfg.DSN)
	if dsn == "" {
		parsed, err := url.Parse(strings.TrimSpace(cfg.URL))
		if err != nil {
			return nil, err
		}
		dsn, err = driverDSNFromURL(parsed)
		if err != nil {
			return nil, err
		}
	}
	db, err := sql.Open(driverName, dsn)
	if err != nil {
		return nil, fmt.Errorf("open mysql store: %w", err)
	}
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("ping mysql store: %w", err)
	}
	return db, nil
}

func driverDSNFromURL(parsed *url.URL) (string, error) {
	if parsed == nil {
		return "", errors.New("mysql store url is required")
	}
	host := strings.TrimSpace(parsed.Host)
	if host == "" {
		return "", errors.New("mysql store url requires host")
	}
	dbName := strings.TrimPrefix(parsed.Path, "/")
	if dbName == "" {
		return "", errors.New("mysql store url requires database name")
	}
	dbName, err := url.PathUnescape(dbName)
	if err != nil {
		return "", fmt.Errorf("decode mysql database name: %w", err)
	}
	password, _ := parsed.User.Password()
	params := map[string]string{}
	for key, values := range parsed.Query() {
		if len(values) == 0 {
			continue
		}
		switch strings.ToLower(strings.TrimSpace(key)) {
		case "parsetime", "loc":
			continue
		}
		key = canonicalMySQLParamKey(key)
		params[key] = values[len(values)-1]
	}
	if _, ok := params["loc"]; !ok {
		params["loc"] = "UTC"
	}
	setDefaultMySQLParam(params, "timeout", "10s")
	setDefaultMySQLParam(params, "readTimeout", "30s")
	setDefaultMySQLParam(params, "writeTimeout", "30s")
	cfg := mysqlDriver.NewConfig()
	cfg.User = parsed.User.Username()
	cfg.Passwd = password
	cfg.Net = "tcp"
	cfg.Addr = host
	cfg.DBName = dbName
	cfg.Params = params
	cfg.ParseTime = true
	cfg.Loc = time.UTC
	return cfg.FormatDSN(), nil
}

func setDefaultMySQLParam(params map[string]string, key string, value string) {
	for existing := range params {
		if strings.EqualFold(strings.TrimSpace(existing), key) {
			return
		}
	}
	params[key] = value
}

func canonicalMySQLParamKey(key string) string {
	switch strings.ToLower(strings.TrimSpace(key)) {
	case "timeout":
		return "timeout"
	case "readtimeout":
		return "readTimeout"
	case "writetimeout":
		return "writeTimeout"
	case "tls":
		return "tls"
	case "charset":
		return "charset"
	case "collation":
		return "collation"
	case "maxallowedpacket":
		return "maxAllowedPacket"
	default:
		return key
	}
}
