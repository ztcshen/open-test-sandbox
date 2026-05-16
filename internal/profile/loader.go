package profile

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const manifestName = "profile.json"
const catalogName = "catalog.json"
const agentTestProfilesName = "agent-test-profiles.json"
const configAuthoringName = "config-authoring.json"

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
	bundle.BaseDir = baseDir
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

func BundleDigest(path string) (string, error) {
	manifestPath, err := resolveManifestPath(path)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(path)
	if err != nil {
		return "", fmt.Errorf("stat profile path %s: %w", path, err)
	}
	var files []string
	if !info.IsDir() {
		files = append(files, manifestPath)
	} else {
		baseDir := filepath.Dir(manifestPath)
		if err := filepath.WalkDir(baseDir, func(path string, entry os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.IsDir() {
				return nil
			}
			files = append(files, path)
			return nil
		}); err != nil {
			return "", fmt.Errorf("walk profile bundle %s: %w", baseDir, err)
		}
	}
	sort.Strings(files)

	hash := sha256.New()
	for _, file := range files {
		rel := file
		if info.IsDir() {
			if relative, err := filepath.Rel(filepath.Dir(manifestPath), file); err == nil {
				rel = relative
			}
		}
		_, _ = io.WriteString(hash, rel)
		_, _ = hash.Write([]byte{0})
		raw, err := os.ReadFile(file)
		if err != nil {
			return "", fmt.Errorf("read profile bundle file %s: %w", file, err)
		}
		_, _ = hash.Write(raw)
		_, _ = hash.Write([]byte{0})
	}
	return "sha256:" + hex.EncodeToString(hash.Sum(nil)), nil
}

func loadAssets(baseDir string, bundle *Bundle) error {
	catalogCases, err := loadCatalogAPICases(baseDir)
	if err != nil {
		return err
	}

	services, err := loadAssetDir[Service](baseDir, "services")
	if err != nil {
		return err
	}
	bundle.Services = append(bundle.Services, services...)
	nodeConfigs, err := loadCatalogNodeConfigs(baseDir)
	if err != nil {
		return err
	}
	bundle.Services = mergeServices(bundle.Services, nodeConfigs)

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

	cases, err := loadAPICaseAssets(baseDir)
	if err != nil {
		return err
	}
	bundle.APICases = mergeAPICases(bundle.APICases, catalogCases, cases)

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

	templateConfigs, err := loadCatalogTemplateConfigs(baseDir)
	if err != nil {
		return err
	}
	bundle.TemplateConfigs = append(bundle.TemplateConfigs, templateConfigs...)

	agentTestProfiles, err := loadAgentTestProfiles(baseDir)
	if err != nil {
		return err
	}
	bundle.AgentTestProfiles = append(bundle.AgentTestProfiles, agentTestProfiles...)

	configAuthoring, err := loadOptionalJSON[ConfigAuthoring](baseDir, configAuthoringName)
	if err != nil {
		return err
	}
	bundle.ConfigAuthoring = configAuthoring
	return nil
}

func loadCatalogAPICases(baseDir string) ([]APICase, error) {
	type fileShape struct {
		InterfaceNodeCases []json.RawMessage `json:"interfaceNodeCases"`
	}
	path := filepath.Join(baseDir, catalogName)
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read profile catalog asset %s: %w", path, err)
	}
	var payload fileShape
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, fmt.Errorf("decode profile catalog asset %s: %w", path, err)
	}
	out := make([]APICase, 0, len(payload.InterfaceNodeCases))
	for _, raw := range payload.InterfaceNodeCases {
		var apiCase APICase
		if err := json.Unmarshal(raw, &apiCase); err != nil {
			return nil, fmt.Errorf("decode profile catalog api case %s: %w", path, err)
		}
		var titleCarrier struct {
			Title string `json:"title,omitempty"`
		}
		if err := json.Unmarshal(raw, &titleCarrier); err != nil {
			return nil, fmt.Errorf("decode profile catalog api case title %s: %w", path, err)
		}
		if strings.TrimSpace(apiCase.DisplayName) == "" {
			apiCase.DisplayName = titleCarrier.Title
		}
		out = append(out, apiCase)
	}
	return out, nil
}

func loadCatalogNodeConfigs(baseDir string) ([]Service, error) {
	type catalogNodeConfig struct {
		Service
		Role string `json:"role,omitempty"`
	}
	type fileShape struct {
		NodeConfigs []catalogNodeConfig `json:"nodeConfigs"`
	}
	path := filepath.Join(baseDir, catalogName)
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read profile catalog asset %s: %w", path, err)
	}
	var payload fileShape
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, fmt.Errorf("decode profile catalog asset %s: %w", path, err)
	}
	out := make([]Service, 0, len(payload.NodeConfigs))
	for _, item := range payload.NodeConfigs {
		service := item.Service
		if service.Kind == "" {
			service.Kind = item.Role
		}
		out = append(out, service)
	}
	return out, nil
}

func mergeServices(groups ...[]Service) []Service {
	merged := map[string]Service{}
	order := []string{}
	for _, group := range groups {
		for _, item := range group {
			if strings.TrimSpace(item.ID) == "" {
				continue
			}
			current, exists := merged[item.ID]
			if !exists {
				order = append(order, item.ID)
			}
			merged[item.ID] = mergeService(current, item)
		}
	}
	out := make([]Service, 0, len(order))
	for _, id := range order {
		out = append(out, merged[id])
	}
	return out
}

func mergeService(base Service, next Service) Service {
	if next.ID != "" {
		base.ID = next.ID
	}
	if next.DisplayName != "" {
		base.DisplayName = next.DisplayName
	}
	if next.Kind != "" {
		base.Kind = next.Kind
	}
	if len(next.AttachedTemplateIDs) > 0 {
		base.AttachedTemplateIDs = next.AttachedTemplateIDs
	}
	if next.GitURL != "" {
		base.GitURL = next.GitURL
	}
	if next.GitBranch != "" {
		base.GitBranch = next.GitBranch
	}
	if next.RepoEnv != "" {
		base.RepoEnv = next.RepoEnv
	}
	if next.SourcePath != "" {
		base.SourcePath = next.SourcePath
	}
	if next.ContainerName != "" {
		base.ContainerName = next.ContainerName
	}
	if next.Image != "" {
		base.Image = next.Image
	}
	if next.DockerService != "" {
		base.DockerService = next.DockerService
	}
	if next.ServicePort != 0 {
		base.ServicePort = next.ServicePort
	}
	if next.ManagementPort != 0 {
		base.ManagementPort = next.ManagementPort
	}
	if next.MemoryMb != 0 {
		base.MemoryMb = next.MemoryMb
	}
	if next.CPUMilli != 0 {
		base.CPUMilli = next.CPUMilli
	}
	if next.StartupCommand != "" {
		base.StartupCommand = next.StartupCommand
	}
	if next.HealthURL != "" {
		base.HealthURL = next.HealthURL
	}
	if next.LogPath != "" {
		base.LogPath = next.LogPath
	}
	if next.Status != "" {
		base.Status = next.Status
	}
	if next.SortOrder != 0 {
		base.SortOrder = next.SortOrder
	}
	return base
}

func mergeAPICases(groups ...[]APICase) []APICase {
	merged := map[string]APICase{}
	order := []string{}
	for _, group := range groups {
		for _, item := range group {
			if strings.TrimSpace(item.ID) == "" {
				continue
			}
			current, exists := merged[item.ID]
			if !exists {
				order = append(order, item.ID)
			}
			merged[item.ID] = mergeAPICase(current, item)
		}
	}
	out := make([]APICase, 0, len(order))
	for _, id := range order {
		out = append(out, merged[id])
	}
	return out
}

func mergeAPICase(base APICase, next APICase) APICase {
	if next.ID != "" {
		base.ID = next.ID
	}
	if next.DisplayName != "" {
		base.DisplayName = next.DisplayName
	}
	if next.NodeID != "" {
		base.NodeID = next.NodeID
	}
	if next.CaseType != "" {
		base.CaseType = next.CaseType
	}
	if next.Scenario != "" {
		base.Scenario = next.Scenario
	}
	if next.PayloadTemplateJSON != "" {
		base.PayloadTemplateJSON = next.PayloadTemplateJSON
	}
	if next.RequestTemplateID != "" {
		base.RequestTemplateID = next.RequestTemplateID
	}
	if next.PatchJSON != "" {
		base.PatchJSON = next.PatchJSON
	}
	if next.RenderMode != "" {
		base.RenderMode = next.RenderMode
	}
	if next.ExpectedJSON != "" {
		base.ExpectedJSON = next.ExpectedJSON
	}
	if next.requiredForAdmissionSet {
		base.RequiredForAdmission = next.RequiredForAdmission
	}
	if next.Status != "" {
		base.Status = next.Status
	}
	if next.SortOrder != 0 {
		base.SortOrder = next.SortOrder
	}
	if next.CasePath != "" {
		base.CasePath = next.CasePath
	}
	if next.BaseURL != "" {
		base.BaseURL = next.BaseURL
	}
	if next.EvidenceDir != "" {
		base.EvidenceDir = next.EvidenceDir
	}
	if next.TimeoutSeconds != 0 {
		base.TimeoutSeconds = next.TimeoutSeconds
	}
	if next.DefaultOverrides != nil {
		base.DefaultOverrides = next.DefaultOverrides
	}
	return base
}

func loadCatalogTemplateConfigs(baseDir string) ([]TemplateConfig, error) {
	type fileShape struct {
		TemplateConfigs []TemplateConfig `json:"templateConfigs"`
	}
	path := filepath.Join(baseDir, catalogName)
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read profile catalog asset %s: %w", path, err)
	}
	var payload fileShape
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, fmt.Errorf("decode profile catalog asset %s: %w", path, err)
	}
	return payload.TemplateConfigs, nil
}

func loadAgentTestProfiles(baseDir string) ([]AgentTestProfile, error) {
	type fileShape struct {
		SchemaVersion string             `json:"schemaVersion,omitempty"`
		Profiles      []AgentTestProfile `json:"profiles"`
	}
	payload, err := loadOptionalJSON[fileShape](baseDir, agentTestProfilesName)
	if err != nil {
		return nil, err
	}
	return payload.Profiles, nil
}

func loadOptionalJSON[T any](baseDir string, name string) (T, error) {
	var out T
	path := filepath.Join(baseDir, name)
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return out, nil
	}
	if err != nil {
		return out, fmt.Errorf("read profile asset %s: %w", path, err)
	}
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&out); err != nil {
		return out, fmt.Errorf("decode profile asset %s: %w", path, err)
	}
	return out, nil
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

func loadAPICaseAssets(baseDir string) ([]APICase, error) {
	dir := filepath.Join(baseDir, "cases")
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

	cases := make([]APICase, 0, len(paths))
	for _, path := range paths {
		var item APICase
		raw, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read profile asset %s: %w", path, err)
		}
		decoder := json.NewDecoder(strings.NewReader(string(raw)))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&item); err != nil {
			return nil, fmt.Errorf("decode profile asset %s: %w", path, err)
		}
		if strings.TrimSpace(item.CasePath) == "" {
			item.CasePath = relativeBundlePath(baseDir, path)
		}
		cases = append(cases, item)
	}
	return cases, nil
}

func relativeBundlePath(baseDir string, path string) string {
	relative, err := filepath.Rel(baseDir, path)
	if err != nil {
		return filepath.ToSlash(path)
	}
	return filepath.ToSlash(relative)
}
