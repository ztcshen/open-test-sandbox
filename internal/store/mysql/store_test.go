package mysql_test

import (
	"strings"
	"testing"

	"open-test-sandbox/internal/store/mysql"
)

func TestParseConfigFromURLAcceptsMySQLURL(t *testing.T) {
	cfg, err := mysql.ParseConfigFromURL("mysql://user:secret@example.com:3306/otsandbox?tls=false")
	if err != nil {
		t.Fatalf("parse mysql url: %v", err)
	}
	if cfg.URL != "mysql://user:secret@example.com:3306/otsandbox?tls=false" {
		t.Fatalf("mysql config url = %q", cfg.URL)
	}
	for _, want := range []string{
		"user:secret@tcp(example.com:3306)/otsandbox",
		"parseTime=true",
		"loc=UTC",
		"tls=false",
	} {
		if !strings.Contains(cfg.DSN, want) {
			t.Fatalf("mysql driver dsn missing %q: %q", want, cfg.DSN)
		}
	}
}

func TestParseConfigFromURLKeepsStoreTimeParsingAuthoritative(t *testing.T) {
	cfg, err := mysql.ParseConfigFromURL("mysql://user:secret@example.com:3306/otsandbox?parseTime=false&loc=Local&tls=false")
	if err != nil {
		t.Fatalf("parse mysql url: %v", err)
	}
	for _, want := range []string{"parseTime=true", "loc=UTC", "tls=false"} {
		if !strings.Contains(cfg.DSN, want) {
			t.Fatalf("mysql driver dsn missing %q: %q", want, cfg.DSN)
		}
	}
	for _, reject := range []string{"parseTime=false", "loc=Local"} {
		if strings.Contains(cfg.DSN, reject) {
			t.Fatalf("mysql driver dsn should not let URL query override Store time parsing with %q: %q", reject, cfg.DSN)
		}
	}
}

func TestParseConfigFromURLAddsBoundedNetworkTimeouts(t *testing.T) {
	cfg, err := mysql.ParseConfigFromURL("mysql://user:secret@example.com:3306/otsandbox?tls=false")
	if err != nil {
		t.Fatalf("parse mysql url: %v", err)
	}
	for _, want := range []string{"timeout=10s", "readTimeout=30s", "writeTimeout=30s"} {
		if !strings.Contains(cfg.DSN, want) {
			t.Fatalf("mysql driver dsn missing bounded network timeout %q: %q", want, cfg.DSN)
		}
	}
}

func TestParseConfigFromURLKeepsExplicitNetworkTimeouts(t *testing.T) {
	cfg, err := mysql.ParseConfigFromURL("mysql://user:secret@example.com:3306/otsandbox?timeout=2s&readTimeout=3s&writeTimeout=4s")
	if err != nil {
		t.Fatalf("parse mysql url: %v", err)
	}
	for _, want := range []string{"timeout=2s", "readTimeout=3s", "writeTimeout=4s"} {
		if !strings.Contains(cfg.DSN, want) {
			t.Fatalf("mysql driver dsn should keep explicit network timeout %q: %q", want, cfg.DSN)
		}
	}
	for _, reject := range []string{"timeout=10s", "readTimeout=30s", "writeTimeout=30s"} {
		if strings.Contains(cfg.DSN, reject) {
			t.Fatalf("mysql driver dsn should not duplicate default timeout %q when explicit timeout is set: %q", reject, cfg.DSN)
		}
	}
}

func TestParseConfigFromURLCanonicalizesExplicitNetworkTimeoutKeys(t *testing.T) {
	cfg, err := mysql.ParseConfigFromURL("mysql://user:secret@example.com:3306/otsandbox?Timeout=2s&READTIMEOUT=3s&writetimeout=4s")
	if err != nil {
		t.Fatalf("parse mysql url: %v", err)
	}
	for _, want := range []string{"timeout=2s", "readTimeout=3s", "writeTimeout=4s"} {
		if !strings.Contains(cfg.DSN, want) {
			t.Fatalf("mysql driver dsn should canonicalize explicit network timeout key %q: %q", want, cfg.DSN)
		}
	}
	for _, reject := range []string{"Timeout=2s", "READTIMEOUT=3s", "writetimeout=4s", "timeout=10s", "readTimeout=30s", "writeTimeout=30s"} {
		if strings.Contains(cfg.DSN, reject) {
			t.Fatalf("mysql driver dsn should not keep mixed-case or default timeout key %q: %q", reject, cfg.DSN)
		}
	}
}

func TestParseConfigFromURLCanonicalizesCommonDriverParamKeys(t *testing.T) {
	cfg, err := mysql.ParseConfigFromURL("mysql://user:secret@example.com:3306/otsandbox?TLS=false&CHARSET=utf8mb4&COLLATION=utf8mb4_unicode_ci&MAXALLOWEDPACKET=1048576")
	if err != nil {
		t.Fatalf("parse mysql url: %v", err)
	}
	for _, want := range []string{"tls=false", "charset=utf8mb4", "collation=utf8mb4_unicode_ci", "maxAllowedPacket=1048576"} {
		if !strings.Contains(cfg.DSN, want) {
			t.Fatalf("mysql driver dsn should canonicalize common driver param key %q: %q", want, cfg.DSN)
		}
	}
	for _, reject := range []string{"TLS=false", "CHARSET=utf8mb4", "COLLATION=utf8mb4_unicode_ci", "MAXALLOWEDPACKET=1048576"} {
		if strings.Contains(cfg.DSN, reject) {
			t.Fatalf("mysql driver dsn should not keep mixed-case driver param key %q: %q", reject, cfg.DSN)
		}
	}
}

func TestParseConfigFromURLCanonicalizesCommonDriverBoolParamKeys(t *testing.T) {
	cfg, err := mysql.ParseConfigFromURL("mysql://user:secret@example.com:3306/otsandbox?ALLOWNATIVEPASSWORDS=false&CHECKCONNLIVENESS=false&CLIENTFOUNDROWS=true&COLUMNSWITHALIAS=true&INTERPOLATEPARAMS=true&MULTISTATEMENTS=true&REJECTREADONLY=true")
	if err != nil {
		t.Fatalf("parse mysql url: %v", err)
	}
	for _, want := range []string{"allowNativePasswords=false", "checkConnLiveness=false", "clientFoundRows=true", "columnsWithAlias=true", "interpolateParams=true", "multiStatements=true", "rejectReadOnly=true"} {
		if !strings.Contains(cfg.DSN, want) {
			t.Fatalf("mysql driver dsn should canonicalize common bool driver param key %q: %q", want, cfg.DSN)
		}
	}
	for _, reject := range []string{"ALLOWNATIVEPASSWORDS=false", "CHECKCONNLIVENESS=false", "CLIENTFOUNDROWS=true", "COLUMNSWITHALIAS=true", "INTERPOLATEPARAMS=true", "MULTISTATEMENTS=true", "REJECTREADONLY=true"} {
		if strings.Contains(cfg.DSN, reject) {
			t.Fatalf("mysql driver dsn should not keep mixed-case bool driver param key %q: %q", reject, cfg.DSN)
		}
	}
}

func TestParseConfigFromURLRejectsNonMySQLDSN(t *testing.T) {
	_, err := mysql.ParseConfigFromURL("postgres://localhost/otsandbox")
	if err == nil {
		t.Fatal("expected non-mysql dsn to be rejected")
	}
}

func TestParseConfigFromURLRequiresDatabaseName(t *testing.T) {
	_, err := mysql.ParseConfigFromURL("mysql://user:secret@example.com:3306")
	if err == nil {
		t.Fatal("expected mysql url without database to be rejected")
	}
}
