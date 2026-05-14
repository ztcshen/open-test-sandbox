package profile

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const manifestName = "profile.json"

func Load(path string) (Bundle, error) {
	manifestPath, err := resolveManifestPath(path)
	if err != nil {
		return Bundle{}, err
	}
	raw, err := os.ReadFile(manifestPath)
	if err != nil {
		return Bundle{}, fmt.Errorf("read profile manifest %s: %w", manifestPath, err)
	}

	var bundle Bundle
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&bundle); err != nil {
		return Bundle{}, fmt.Errorf("decode profile manifest %s: %w", manifestPath, err)
	}
	if err := validate(bundle); err != nil {
		return Bundle{}, fmt.Errorf("validate profile manifest %s: %w", manifestPath, err)
	}
	return bundle, nil
}

func resolveManifestPath(path string) (string, error) {
	if path == "" {
		return "", errors.New("profile path is required")
	}
	info, err := os.Stat(path)
	if err != nil {
		return "", fmt.Errorf("stat profile path %s: %w", path, err)
	}
	if info.IsDir() {
		return filepath.Join(path, manifestName), nil
	}
	return path, nil
}

func validate(bundle Bundle) error {
	if strings.TrimSpace(bundle.ID) == "" {
		return errors.New("profile id is required")
	}
	if strings.TrimSpace(bundle.DisplayName) == "" {
		return errors.New("profile displayName is required")
	}
	return nil
}
