package profile

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
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
	baseDir := filepath.Dir(manifestPath)
	if err := loadAssets(baseDir, &bundle); err != nil {
		return Bundle{}, err
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

func loadAssets(baseDir string, bundle *Bundle) error {
	services, err := loadAssetDir[Service](baseDir, "services")
	if err != nil {
		return err
	}
	bundle.Services = append(bundle.Services, services...)

	workflows, err := loadAssetDir[Workflow](baseDir, "workflows")
	if err != nil {
		return err
	}
	bundle.Workflows = append(bundle.Workflows, workflows...)

	nodes, err := loadAssetDir[InterfaceNode](baseDir, "interface-nodes")
	if err != nil {
		return err
	}
	bundle.InterfaceNodes = append(bundle.InterfaceNodes, nodes...)

	cases, err := loadAssetDir[APICase](baseDir, "cases")
	if err != nil {
		return err
	}
	bundle.APICases = append(bundle.APICases, cases...)

	requestTemplates, err := loadAssetDir[RequestTemplate](baseDir, "request-templates")
	if err != nil {
		return err
	}
	bundle.RequestTemplates = append(bundle.RequestTemplates, requestTemplates...)

	caseDependencies, err := loadAssetDir[CaseDependency](baseDir, "case-dependencies")
	if err != nil {
		return err
	}
	bundle.CaseDependencies = append(bundle.CaseDependencies, caseDependencies...)

	workflowBindings, err := loadAssetDir[WorkflowBinding](baseDir, "workflow-bindings")
	if err != nil {
		return err
	}
	bundle.WorkflowBindings = append(bundle.WorkflowBindings, workflowBindings...)

	fixtures, err := loadAssetDir[Fixture](baseDir, "fixtures")
	if err != nil {
		return err
	}
	bundle.Fixtures = append(bundle.Fixtures, fixtures...)
	return nil
}

func loadAssetDir[T any](baseDir string, name string) ([]T, error) {
	dir := filepath.Join(baseDir, name)
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read profile asset directory %s: %w", dir, err)
	}

	var paths []string
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		paths = append(paths, filepath.Join(dir, entry.Name()))
	}
	sort.Strings(paths)

	assets := make([]T, 0, len(paths))
	for _, path := range paths {
		var asset T
		raw, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read profile asset %s: %w", path, err)
		}
		decoder := json.NewDecoder(strings.NewReader(string(raw)))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&asset); err != nil {
			return nil, fmt.Errorf("decode profile asset %s: %w", path, err)
		}
		assets = append(assets, asset)
	}
	return assets, nil
}
