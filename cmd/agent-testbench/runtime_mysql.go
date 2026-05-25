package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net"
	"net/url"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"
)

const runtimeMySQLPasswordMask = "xxxxx"

type runtimeMySQLEndpointsReport struct {
	OK    bool                   `json:"ok"`
	Count int                    `json:"count"`
	Items []runtimeMySQLEndpoint `json:"items"`
}

type runtimeMySQLEndpoint struct {
	ID             string                 `json:"id"`
	Name           string                 `json:"name"`
	ContainerName  string                 `json:"containerName"`
	Image          string                 `json:"image"`
	Host           string                 `json:"host"`
	Port           int                    `json:"port"`
	User           string                 `json:"user"`
	PasswordMasked string                 `json:"passwordMasked,omitempty"`
	Database       string                 `json:"database,omitempty"`
	DSN            string                 `json:"dsn"`
	Databases      []runtimeMySQLDatabase `json:"databases,omitempty"`
	Warnings       []string               `json:"warnings,omitempty"`
}

type runtimeMySQLDatabase struct {
	Name   string   `json:"name"`
	Tables []string `json:"tables"`
}

type runtimeDockerPSContainer struct {
	ID    string `json:"ID"`
	Names string `json:"Names"`
	Image string `json:"Image"`
	Ports string `json:"Ports"`
}

type runtimeDockerInspectContainer struct {
	ID     string `json:"Id"`
	Name   string `json:"Name"`
	Config struct {
		Image string   `json:"Image"`
		Env   []string `json:"Env"`
	} `json:"Config"`
	NetworkSettings struct {
		Ports map[string][]struct {
			HostIP   string `json:"HostIp"`
			HostPort string `json:"HostPort"`
		} `json:"Ports"`
	} `json:"NetworkSettings"`
}

func runRuntime(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return errors.New("missing runtime command")
	}
	switch args[0] {
	case "mysql":
		return runRuntimeMySQL(ctx, args[1:])
	default:
		return fmt.Errorf("unknown runtime command: %s", args[0])
	}
}

func runRuntimeMySQL(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return errors.New("missing runtime mysql command")
	}
	switch args[0] {
	case "endpoints":
		return runRuntimeMySQLEndpoints(ctx, args[1:])
	default:
		return fmt.Errorf("unknown runtime mysql command: %s", args[0])
	}
}

func runRuntimeMySQLEndpoints(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("runtime mysql endpoints", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	includeTables := flags.Bool("include-tables", false, "Include database and table inventory when the container can be queried")
	jsonOutput := flags.Bool("json", false, "Emit a machine-readable JSON report")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("unexpected runtime mysql endpoints argument: %s", flags.Arg(0))
	}
	report, err := runtimeMySQLEndpoints(ctx, *includeTables)
	if err != nil {
		return err
	}
	if *jsonOutput {
		return writeIndentedJSON(report)
	}
	printRuntimeMySQLEndpoints(report)
	return nil
}

func runtimeMySQLEndpoints(ctx context.Context, includeTables bool) (runtimeMySQLEndpointsReport, error) {
	containers, err := runtimeDockerPS(ctx)
	if err != nil {
		return runtimeMySQLEndpointsReport{}, err
	}
	report := runtimeMySQLEndpointsReport{OK: true, Items: []runtimeMySQLEndpoint{}}
	for _, container := range containers {
		if !runtimeLooksLikeMySQLContainer(container) {
			continue
		}
		inspect, err := runtimeDockerInspect(ctx, container.ID)
		if err != nil {
			return runtimeMySQLEndpointsReport{}, err
		}
		endpoint, ok := runtimeMySQLEndpointFromInspect(container, inspect)
		if !ok {
			continue
		}
		if includeTables {
			databases, err := runtimeMySQLContainerTables(ctx, endpoint.ID)
			if err != nil {
				endpoint.Warnings = append(endpoint.Warnings, "table inventory unavailable")
			} else {
				endpoint.Databases = databases
			}
		}
		report.Items = append(report.Items, endpoint)
	}
	report.Count = len(report.Items)
	return report, nil
}

func runtimeDockerPS(ctx context.Context) ([]runtimeDockerPSContainer, error) {
	cmd := exec.CommandContext(ctx, "docker", "ps", "--format", "{{json .}}")
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("docker ps: %w", err)
	}
	scanner := bufio.NewScanner(bytes.NewReader(out))
	containers := []runtimeDockerPSContainer{}
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var container runtimeDockerPSContainer
		if err := json.Unmarshal([]byte(line), &container); err != nil {
			return nil, fmt.Errorf("decode docker ps row: %w", err)
		}
		containers = append(containers, container)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read docker ps output: %w", err)
	}
	return containers, nil
}

func runtimeDockerInspect(ctx context.Context, containerID string) (runtimeDockerInspectContainer, error) {
	cmd := exec.CommandContext(ctx, "docker", "inspect", containerID)
	out, err := cmd.Output()
	if err != nil {
		return runtimeDockerInspectContainer{}, fmt.Errorf("docker inspect %s: %w", containerID, err)
	}
	var inspected []runtimeDockerInspectContainer
	if err := json.Unmarshal(out, &inspected); err != nil {
		return runtimeDockerInspectContainer{}, fmt.Errorf("decode docker inspect %s: %w", containerID, err)
	}
	if len(inspected) == 0 {
		return runtimeDockerInspectContainer{}, fmt.Errorf("docker inspect %s returned no containers", containerID)
	}
	return inspected[0], nil
}

func runtimeLooksLikeMySQLContainer(container runtimeDockerPSContainer) bool {
	haystack := strings.ToLower(strings.Join([]string{container.Names, container.Image, container.Ports}, " "))
	return strings.Contains(haystack, "mysql") || strings.Contains(haystack, "mariadb") || strings.Contains(haystack, "3306/tcp")
}

func runtimeMySQLEndpointFromInspect(container runtimeDockerPSContainer, inspected runtimeDockerInspectContainer) (runtimeMySQLEndpoint, bool) {
	published := inspected.NetworkSettings.Ports["3306/tcp"]
	if len(published) == 0 {
		return runtimeMySQLEndpoint{}, false
	}
	port, err := strconv.Atoi(strings.TrimSpace(published[0].HostPort))
	if err != nil || port <= 0 {
		return runtimeMySQLEndpoint{}, false
	}
	host := runtimeNormalizePublishedHost(published[0].HostIP)
	env := runtimeEnvMap(inspected.Config.Env)
	user := runtimeFirstNonEmpty(env["MYSQL_USER"], env["MARIADB_USER"], "root")
	database := runtimeFirstNonEmpty(env["MYSQL_DATABASE"], env["MARIADB_DATABASE"])
	hasPassword := runtimeFirstNonEmpty(env["MYSQL_PASSWORD"], env["MARIADB_PASSWORD"], env["MYSQL_ROOT_PASSWORD"], env["MARIADB_ROOT_PASSWORD"]) != ""
	passwordMask := ""
	if hasPassword {
		passwordMask = runtimeMySQLPasswordMask
	}
	image := runtimeFirstNonEmpty(inspected.Config.Image, container.Image)
	name := strings.TrimPrefix(runtimeFirstNonEmpty(inspected.Name, container.Names), "/")
	id := runtimeFirstNonEmpty(inspected.ID, container.ID)
	endpoint := runtimeMySQLEndpoint{
		ID:             id,
		Name:           name,
		ContainerName:  name,
		Image:          image,
		Host:           host,
		Port:           port,
		User:           user,
		PasswordMasked: passwordMask,
		Database:       database,
	}
	endpoint.DSN = runtimeMaskedMySQLDSN(endpoint)
	return endpoint, true
}

func runtimeNormalizePublishedHost(host string) string {
	host = strings.TrimSpace(host)
	switch host {
	case "", "0.0.0.0", "::":
		return "127.0.0.1"
	default:
		return host
	}
}

func runtimeEnvMap(values []string) map[string]string {
	out := map[string]string{}
	for _, value := range values {
		key, val, ok := strings.Cut(value, "=")
		if !ok {
			continue
		}
		out[key] = val
	}
	return out
}

func runtimeMaskedMySQLDSN(endpoint runtimeMySQLEndpoint) string {
	dsn := url.URL{Scheme: "mysql", Host: net.JoinHostPort(endpoint.Host, strconv.Itoa(endpoint.Port))}
	if endpoint.PasswordMasked != "" {
		dsn.User = url.UserPassword(endpoint.User, endpoint.PasswordMasked)
	} else {
		dsn.User = url.User(endpoint.User)
	}
	if endpoint.Database != "" {
		dsn.Path = "/" + endpoint.Database
	}
	return dsn.String()
}

func runtimeMySQLContainerTables(ctx context.Context, containerID string) ([]runtimeMySQLDatabase, error) {
	const script = `
set -eu
query="select table_schema, table_name from information_schema.tables where table_schema not in ('information_schema','mysql','performance_schema','sys') order by table_schema, table_name"
mysql_user="${MYSQL_USER:-${MARIADB_USER:-root}}"
mysql_password="${MYSQL_PASSWORD:-${MARIADB_PASSWORD:-}}"
root_password="${MYSQL_ROOT_PASSWORD:-${MARIADB_ROOT_PASSWORD:-}}"
if [ "$mysql_user" = "root" ] && [ -z "$mysql_password" ]; then
  mysql_password="$root_password"
fi
run_query() {
  user="$1"
  password="$2"
  if [ -n "$password" ]; then
    MYSQL_PWD="$password" mysql -N -B -u"$user" -e "$query"
  else
    mysql -N -B -u"$user" -e "$query"
  fi
}
if run_query "$mysql_user" "$mysql_password"; then
  exit 0
fi
if [ "$mysql_user" != "root" ] && [ -n "$root_password" ]; then
  run_query root "$root_password"
  exit $?
fi
exit 1
`
	cmd := exec.CommandContext(ctx, "docker", "exec", containerID, "sh", "-lc", script)
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	return parseRuntimeMySQLTables(out), nil
}

func parseRuntimeMySQLTables(out []byte) []runtimeMySQLDatabase {
	tablesByDatabase := map[string][]string{}
	scanner := bufio.NewScanner(bytes.NewReader(out))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		fields := strings.Split(line, "\t")
		if len(fields) < 2 {
			fields = strings.Fields(line)
		}
		if len(fields) < 2 {
			continue
		}
		database := strings.TrimSpace(fields[0])
		table := strings.TrimSpace(fields[1])
		if database == "" || table == "" {
			continue
		}
		tablesByDatabase[database] = append(tablesByDatabase[database], table)
	}
	names := make([]string, 0, len(tablesByDatabase))
	for name := range tablesByDatabase {
		names = append(names, name)
	}
	sort.Strings(names)
	databases := make([]runtimeMySQLDatabase, 0, len(names))
	for _, name := range names {
		tables := normalizeStringList(tablesByDatabase[name])
		databases = append(databases, runtimeMySQLDatabase{Name: name, Tables: tables})
	}
	return databases
}

func printRuntimeMySQLEndpoints(report runtimeMySQLEndpointsReport) {
	fmt.Println("MySQL Runtime Endpoints")
	fmt.Printf("Total: %d\n", report.Count)
	for _, item := range report.Items {
		fmt.Printf("- %s: %s\n", item.ContainerName, item.DSN)
		if len(item.Databases) > 0 {
			parts := []string{}
			for _, database := range item.Databases {
				for _, table := range database.Tables {
					parts = append(parts, database.Name+"."+table)
				}
			}
			fmt.Printf("  Tables: %s\n", strings.Join(parts, ", "))
		}
		if len(item.Warnings) > 0 {
			fmt.Printf("  Warnings: %s\n", strings.Join(item.Warnings, "; "))
		}
	}
}

func runtimeFirstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
