package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRuntimeMySQLEndpointsDiscoversDockerPublishedDatabases(t *testing.T) {
	fakeEnv := fakeRuntimeMySQLDockerCommand(t)

	out := runCLIWithEnv(t, fakeEnv, "runtime", "mysql", "endpoints", "--include-tables", "--json")

	var report struct {
		OK    bool `json:"ok"`
		Count int  `json:"count"`
		Items []struct {
			ID             string `json:"id"`
			Name           string `json:"name"`
			ContainerName  string `json:"containerName"`
			Image          string `json:"image"`
			Host           string `json:"host"`
			Port           int    `json:"port"`
			User           string `json:"user"`
			PasswordMasked string `json:"passwordMasked"`
			Database       string `json:"database"`
			DSN            string `json:"dsn"`
			Databases      []struct {
				Name   string   `json:"name"`
				Tables []string `json:"tables"`
			} `json:"databases"`
		} `json:"items"`
	}
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatalf("decode runtime mysql endpoints json: %v\n%s", err, out)
	}
	if !report.OK || report.Count != 1 || len(report.Items) != 1 {
		t.Fatalf("report summary = %#v", report)
	}
	item := report.Items[0]
	if item.ContainerName != "agent-testbench-mysql-1" || item.Image != "mysql:8.4" || item.Host != "127.0.0.1" || item.Port != 33306 || item.User != "app" || item.PasswordMasked != "xxxxx" || item.Database != "appdb" {
		t.Fatalf("connection item = %#v", item)
	}
	if item.DSN != "mysql://app:xxxxx@127.0.0.1:33306/appdb" {
		t.Fatalf("masked DSN = %q", item.DSN)
	}
	if len(item.Databases) != 2 || item.Databases[0].Name != "appdb" || strings.Join(item.Databases[0].Tables, ",") != "orders,users" || item.Databases[1].Name != "reporting" || strings.Join(item.Databases[1].Tables, ",") != "daily_summary" {
		t.Fatalf("database table inventory = %#v", item.Databases)
	}
	if strings.Contains(out, "appsecret") || strings.Contains(out, "rootsecret") {
		t.Fatalf("endpoint report must not expose plaintext container passwords:\n%s", out)
	}
}

func TestRuntimeMySQLEndpointsReportsEmptyDockerInventory(t *testing.T) {
	dir := t.TempDir()
	dockerPath := filepath.Join(dir, "docker")
	writeFile(t, dockerPath, "#!/bin/sh\nif [ \"$1\" = \"ps\" ]; then exit 0; fi\nexit 1\n")
	if err := os.Chmod(dockerPath, 0o755); err != nil {
		t.Fatalf("chmod fake docker: %v", err)
	}

	out := runCLIWithEnv(t, []string{"PATH=" + dir + string(os.PathListSeparator) + os.Getenv("PATH")}, "runtime", "mysql", "endpoints", "--json")

	var report struct {
		OK    bool  `json:"ok"`
		Count int   `json:"count"`
		Items []any `json:"items"`
	}
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatalf("decode empty runtime mysql endpoints json: %v\n%s", err, out)
	}
	if !report.OK || report.Count != 0 || len(report.Items) != 0 {
		t.Fatalf("empty report = %#v", report)
	}
}

func fakeRuntimeMySQLDockerCommand(t *testing.T) []string {
	t.Helper()
	dir := t.TempDir()
	dockerPath := filepath.Join(dir, "docker")
	writeFile(t, dockerPath, `#!/usr/bin/env bash
set -euo pipefail
if [[ "$1" == "ps" ]]; then
  printf '%s\n' '{"ID":"mysql-container-id","Names":"agent-testbench-mysql-1","Image":"mysql:8.4","Ports":"0.0.0.0:33306->3306/tcp"}'
  printf '%s\n' '{"ID":"app-container-id","Names":"agent-testbench-api-1","Image":"alpine:3.20","Ports":"0.0.0.0:18080->8080/tcp"}'
  exit 0
fi
if [[ "$1" == "inspect" && "$2" == "mysql-container-id" ]]; then
  cat <<'JSON'
[{
  "Id": "mysql-container-id",
  "Name": "/agent-testbench-mysql-1",
  "Config": {
    "Image": "mysql:8.4",
    "Env": [
      "MYSQL_DATABASE=appdb",
      "MYSQL_USER=app",
      "MYSQL_PASSWORD=appsecret",
      "MYSQL_ROOT_PASSWORD=rootsecret"
    ]
  },
  "NetworkSettings": {
    "Ports": {
      "3306/tcp": [
        {"HostIp": "0.0.0.0", "HostPort": "33306"}
      ]
    }
  }
}]
JSON
  exit 0
fi
if [[ "$1" == "inspect" && "$2" == "app-container-id" ]]; then
  cat <<'JSON'
[{
  "Id": "app-container-id",
  "Name": "/agent-testbench-api-1",
  "Config": {"Image": "alpine:3.20", "Env": []},
  "NetworkSettings": {"Ports": {}}
}]
JSON
  exit 0
fi
if [[ "$1" == "exec" ]]; then
  printf 'appdb\torders\n'
  printf 'appdb\tusers\n'
  printf 'reporting\tdaily_summary\n'
  exit 0
fi
printf 'unexpected docker args: %s\n' "$*" >&2
exit 1
`)
	if err := os.Chmod(dockerPath, 0o755); err != nil {
		t.Fatalf("chmod fake docker: %v", err)
	}
	return []string{"PATH=" + dir + string(os.PathListSeparator) + os.Getenv("PATH")}
}
