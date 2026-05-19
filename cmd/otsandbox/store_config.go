package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type storeConfigFile struct {
	Active string                      `json:"active,omitempty"`
	Stores map[string]storeConfigEntry `json:"stores"`
}

type storeConfigEntry struct {
	Name    string `json:"name"`
	URL     string `json:"url"`
	Backend string `json:"backend"`
}

type currentStoreReport struct {
	OK      bool   `json:"ok"`
	Name    string `json:"name"`
	Backend string `json:"backend"`
	URL     string `json:"url"`
}

var errNoActiveStoreConfigured = errors.New("no active store configured")

func activeStoreRequiredError() error {
	return fmt.Errorf("%w; run `otsandbox store config set NAME --url postgres://...` then `otsandbox store use NAME`", errNoActiveStoreConfigured)
}

func runStoreConfig(args []string) error {
	if len(args) == 0 {
		return errors.New("missing store config command")
	}
	switch args[0] {
	case "set":
		return runStoreConfigSet(args[1:])
	case "list":
		return runStoreConfigList(args[1:])
	case "remove":
		return runStoreConfigRemove(args[1:])
	default:
		return fmt.Errorf("unknown store config command: %s", args[0])
	}
}

func runStoreConfigSet(args []string) error {
	if len(args) == 0 {
		return errors.New("store name is required")
	}
	name := strings.TrimSpace(args[0])
	flags := flag.NewFlagSet("store config set", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	storeURL := flags.String("url", "", "PostgreSQL Store DSN")
	if err := flags.Parse(args[1:]); err != nil {
		return err
	}
	entry, err := newStoreConfigEntry(name, *storeURL)
	if err != nil {
		return err
	}
	cfg, err := loadStoreConfig()
	if err != nil {
		return err
	}
	if cfg.Stores == nil {
		cfg.Stores = map[string]storeConfigEntry{}
	}
	cfg.Stores[name] = entry
	if err := saveStoreConfig(cfg); err != nil {
		return err
	}
	fmt.Printf("Configured store: %s\n", entry.Name)
	fmt.Printf("Backend: %s\n", entry.Backend)
	fmt.Printf("URL: %s\n", maskStoreURL(entry.URL))
	return nil
}

func runStoreConfigList(args []string) error {
	flags := flag.NewFlagSet("store config list", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	jsonOutput := flags.Bool("json", false, "Emit a machine-readable JSON report")
	if err := flags.Parse(args); err != nil {
		return err
	}
	cfg, err := loadStoreConfig()
	if err != nil {
		return err
	}
	names := make([]string, 0, len(cfg.Stores))
	for name := range cfg.Stores {
		names = append(names, name)
	}
	sort.Strings(names)
	if *jsonOutput {
		items := make([]map[string]any, 0, len(names))
		for _, name := range names {
			entry := cfg.Stores[name]
			items = append(items, map[string]any{
				"name":    entry.Name,
				"backend": entry.Backend,
				"url":     maskStoreURL(entry.URL),
				"active":  name == cfg.Active,
			})
		}
		return writeIndentedJSON(map[string]any{"ok": true, "active": cfg.Active, "stores": items})
	}
	for _, name := range names {
		entry := cfg.Stores[name]
		marker := " "
		if name == cfg.Active {
			marker = "*"
		}
		fmt.Printf("%s %s\t%s\t%s\n", marker, entry.Name, entry.Backend, maskStoreURL(entry.URL))
	}
	return nil
}

func runStoreConfigRemove(args []string) error {
	flags := flag.NewFlagSet("store config remove", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 1 {
		return errors.New("store name is required")
	}
	name := strings.TrimSpace(flags.Arg(0))
	cfg, err := loadStoreConfig()
	if err != nil {
		return err
	}
	if _, ok := cfg.Stores[name]; !ok {
		return fmt.Errorf("store config %q not found", name)
	}
	delete(cfg.Stores, name)
	if cfg.Active == name {
		cfg.Active = ""
	}
	if err := saveStoreConfig(cfg); err != nil {
		return err
	}
	fmt.Printf("Removed store: %s\n", name)
	return nil
}

func runStoreUse(args []string) error {
	flags := flag.NewFlagSet("store use", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 1 {
		return errors.New("store name is required")
	}
	name := strings.TrimSpace(flags.Arg(0))
	cfg, err := loadStoreConfig()
	if err != nil {
		return err
	}
	if _, ok := cfg.Stores[name]; !ok {
		return fmt.Errorf("store config %q not found", name)
	}
	cfg.Active = name
	if err := saveStoreConfig(cfg); err != nil {
		return err
	}
	fmt.Printf("Active store: %s\n", name)
	return nil
}

func runStoreCurrent(args []string) error {
	flags := flag.NewFlagSet("store current", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	jsonOutput := flags.Bool("json", false, "Emit a machine-readable JSON report")
	if err := flags.Parse(args); err != nil {
		return err
	}
	entry, err := activeStoreConfig()
	if err != nil {
		return err
	}
	report := currentStoreReport{OK: true, Name: entry.Name, Backend: entry.Backend, URL: maskStoreURL(entry.URL)}
	if *jsonOutput {
		return writeIndentedJSON(report)
	}
	fmt.Printf("Active store: %s\n", entry.Name)
	fmt.Printf("Backend: %s\n", entry.Backend)
	fmt.Printf("URL: %s\n", maskStoreURL(entry.URL))
	return nil
}

func activeStoreConfig() (storeConfigEntry, error) {
	cfg, err := loadStoreConfig()
	if err != nil {
		return storeConfigEntry{}, err
	}
	if strings.TrimSpace(cfg.Active) == "" {
		return storeConfigEntry{}, activeStoreRequiredError()
	}
	entry, ok := cfg.Stores[cfg.Active]
	if !ok {
		return storeConfigEntry{}, fmt.Errorf("active store config %q not found", cfg.Active)
	}
	return entry, nil
}

func resolveStoreReference(storeRef string, legacyStoreURL string) (string, error) {
	storeRef = strings.TrimSpace(storeRef)
	if storeRef == "" {
		legacyStoreURL = strings.TrimSpace(legacyStoreURL)
		if legacyStoreURL != "" {
			return normalizeLegacyStoreURL(legacyStoreURL)
		}
		entry, err := activeStoreConfig()
		if err != nil {
			if errors.Is(err, errNoActiveStoreConfigured) {
				return "", nil
			}
			return "", err
		}
		return entry.URL, nil
	}
	if backend, err := storeBackendFromURL(storeRef); err == nil && backend != "" {
		return storeRef, nil
	}
	cfg, err := loadStoreConfig()
	if err != nil {
		return "", err
	}
	entry, ok := cfg.Stores[storeRef]
	if !ok {
		return "", fmt.Errorf("store config %q not found", storeRef)
	}
	return entry.URL, nil
}

func normalizeLegacyStoreURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", nil
	}
	if _, err := storeBackendFromURL(raw); err == nil {
		return raw, nil
	}
	if strings.Contains(raw, "://") {
		return "", fmt.Errorf("invalid store url %q", raw)
	}
	return "sqlite://" + raw, nil
}

func resolveRequiredStoreReference(storeRef string, legacyStoreURL string) (string, error) {
	resolved, err := resolveStoreReference(storeRef, legacyStoreURL)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(resolved) == "" {
		return "", errNoActiveStoreConfigured
	}
	return resolved, nil
}

func resolveOptionalBundleStoreReference(profileRef string, storeRef string, legacyStoreURL string) (string, error) {
	if strings.TrimSpace(profileRef) != "" && strings.TrimSpace(storeRef) == "" && strings.TrimSpace(legacyStoreURL) == "" {
		return "", nil
	}
	return resolveStoreReference(storeRef, legacyStoreURL)
}

func newStoreConfigEntry(name string, rawURL string) (storeConfigEntry, error) {
	if name == "" {
		return storeConfigEntry{}, errors.New("store name is required")
	}
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return storeConfigEntry{}, errors.New("--url is required")
	}
	backend, err := storeBackendFromURL(rawURL)
	if err != nil {
		return storeConfigEntry{}, err
	}
	if backend != "postgres" {
		return storeConfigEntry{}, fmt.Errorf("store config %q must use postgres:// or postgresql://", name)
	}
	return storeConfigEntry{Name: name, URL: rawURL, Backend: backend}, nil
}

func storeBackendFromURL(rawURL string) (string, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.Scheme == "" {
		return "", fmt.Errorf("invalid store url %q", rawURL)
	}
	switch strings.ToLower(parsed.Scheme) {
	case "postgres", "postgresql":
		return "postgres", nil
	case "sqlite", "file":
		return "sqlite", nil
	default:
		return "", fmt.Errorf("unsupported store backend %q", parsed.Scheme)
	}
}

func loadStoreConfig() (storeConfigFile, error) {
	path, err := storeConfigPath()
	if err != nil {
		return storeConfigFile{}, err
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return storeConfigFile{Stores: map[string]storeConfigEntry{}}, nil
		}
		return storeConfigFile{}, err
	}
	var cfg storeConfigFile
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return storeConfigFile{}, err
	}
	if cfg.Stores == nil {
		cfg.Stores = map[string]storeConfigEntry{}
	}
	return cfg, nil
}

func saveStoreConfig(cfg storeConfigFile) error {
	path, err := storeConfigPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	return os.WriteFile(path, raw, 0o600)
}

func storeConfigPath() (string, error) {
	if home := strings.TrimSpace(os.Getenv("OTSANDBOX_CONFIG_HOME")); home != "" {
		return filepath.Join(home, "store-config.json"), nil
	}
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "open-test-sandbox", "store-config.json"), nil
}

func maskStoreURL(rawURL string) string {
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.User == nil {
		return rawURL
	}
	username := parsed.User.Username()
	if username == "" {
		return rawURL
	}
	parsed.User = url.UserPassword(username, "xxxxx")
	return parsed.String()
}
