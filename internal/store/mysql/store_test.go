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
