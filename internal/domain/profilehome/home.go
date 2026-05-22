package profilehome

import (
	"archive/tar"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"open-test-sandbox/internal/domain/profile"
)

type InstallReport struct {
	TemplatePackageID     string `json:"templatePackageId"`
	TemplatePackagePath   string `json:"templatePackagePath"`
	TemplatePackageDigest string `json:"templatePackageDigest"`
	ID                    string `json:"id"`
	DisplayName           string `json:"displayName"`
	SourcePath            string `json:"sourcePath"`
	TargetPath            string `json:"targetPath"`
	BundleDigest          string `json:"bundleDigest"`
}

type PackReport struct {
	ID           string `json:"id"`
	DisplayName  string `json:"displayName"`
	SourcePath   string `json:"sourcePath"`
	OutputPath   string `json:"outputPath"`
	BundleDigest string `json:"bundleDigest"`
	FileCount    int    `json:"fileCount"`
}

type ListReport struct {
	TemplatePackageHome string     `json:"templatePackageHome"`
	TemplatePackages    []ListItem `json:"templatePackages"`
	ProfileHome         string     `json:"profileHome"`
	Profiles            []ListItem `json:"profiles"`
}

type ListItem struct {
	TemplatePackageID     string `json:"templatePackageId"`
	TemplatePackagePath   string `json:"templatePackagePath"`
	TemplatePackageDigest string `json:"templatePackageDigest"`
	ID                    string `json:"id"`
	DisplayName           string `json:"displayName"`
	Path                  string `json:"path"`
	BundleDigest          string `json:"bundleDigest"`
	Counts                Counts `json:"counts"`
	Valid                 bool   `json:"valid"`
	Error                 string `json:"error,omitempty"`
}

type Counts struct {
	Services         int `json:"services"`
	Workflows        int `json:"workflows"`
	InterfaceNodes   int `json:"interfaceNodes"`
	APICases         int `json:"apiCases"`
	RequestTemplates int `json:"requestTemplates"`
	CaseDependencies int `json:"caseDependencies"`
	WorkflowBindings int `json:"workflowBindings"`
	Fixtures         int `json:"fixtures"`
}

func ResolveHome(value string) (string, error) {
	if strings.TrimSpace(value) == "" {
		value = os.Getenv("OTSANDBOX_PROFILE_HOME")
	}
	if strings.TrimSpace(value) == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		value = filepath.Join(home, ".otsandbox", "profiles")
	}
	return filepath.Abs(value)
}

func ResolveReference(value string, homeValue string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", errors.New("profile path or id is required")
	}
	if _, err := os.Stat(value); err == nil {
		return value, nil
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	if strings.ContainsAny(value, `/\`) || value == "." || value == ".." {
		return value, nil
	}
	home, err := ResolveHome(homeValue)
	if err != nil {
		return "", err
	}
	return filepath.Join(home, value), nil
}

func Install(from string, homeValue string, force bool) (InstallReport, error) {
	sourcePath := strings.TrimSpace(from)
	if sourcePath == "" {
		return InstallReport{}, errors.New("--from is required")
	}
	sourcePath, err := filepath.Abs(sourcePath)
	if err != nil {
		return InstallReport{}, err
	}
	originalSourcePath := sourcePath
	cleanup := func() {}
	if IsArchivePath(sourcePath) {
		extractedPath, extractedCleanup, err := extractProfileArchive(sourcePath)
		if err != nil {
			return InstallReport{}, err
		}
		sourcePath = extractedPath
		cleanup = extractedCleanup
	}
	defer cleanup()
	bundle, err := profile.Load(sourcePath)
	if err != nil {
		return InstallReport{}, err
	}
	if !safeInstallID(bundle.ID) {
		return InstallReport{}, fmt.Errorf("profile id %q cannot be installed as a local directory name", bundle.ID)
	}
	home, err := ResolveHome(homeValue)
	if err != nil {
		return InstallReport{}, err
	}
	targetPath := filepath.Join(home, bundle.ID)
	if IsCoreProfilesPath(targetPath) {
		return InstallReport{}, errors.New("profile bundles must be installed outside this core repository")
	}
	if _, err := os.Stat(targetPath); err == nil {
		if !force {
			return InstallReport{}, fmt.Errorf("%s already exists; pass --force to replace it", targetPath)
		}
		if err := os.RemoveAll(targetPath); err != nil {
			return InstallReport{}, err
		}
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return InstallReport{}, err
	}
	if err := copyDir(sourcePath, targetPath); err != nil {
		return InstallReport{}, err
	}
	digest, err := profile.BundleDigest(targetPath)
	if err != nil {
		return InstallReport{}, err
	}
	return InstallReport{
		TemplatePackageID:     bundle.ID,
		TemplatePackagePath:   targetPath,
		TemplatePackageDigest: digest,
		ID:                    bundle.ID,
		DisplayName:           bundle.DisplayName,
		SourcePath:            originalSourcePath,
		TargetPath:            targetPath,
		BundleDigest:          digest,
	}, nil
}

func Pack(reference string, homeValue string, outputPath string, force bool) (PackReport, error) {
	outputPath = strings.TrimSpace(outputPath)
	if outputPath == "" {
		return PackReport{}, errors.New("--output is required")
	}
	sourcePath, err := ResolveReference(reference, homeValue)
	if err != nil {
		return PackReport{}, err
	}
	sourcePath, err = filepath.Abs(sourcePath)
	if err != nil {
		return PackReport{}, err
	}
	bundle, err := profile.Load(sourcePath)
	if err != nil {
		return PackReport{}, err
	}
	if !safeInstallID(bundle.ID) {
		return PackReport{}, fmt.Errorf("profile id %q cannot be packed as an archive root directory name", bundle.ID)
	}
	outputPath, err = filepath.Abs(outputPath)
	if err != nil {
		return PackReport{}, err
	}
	if _, err := os.Stat(outputPath); err == nil {
		if !force {
			return PackReport{}, fmt.Errorf("%s already exists; pass --force to replace it", outputPath)
		}
		if err := os.Remove(outputPath); err != nil {
			return PackReport{}, err
		}
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return PackReport{}, err
	}
	if err := os.MkdirAll(filepath.Dir(outputPath), 0o755); err != nil {
		return PackReport{}, err
	}
	fileCount, err := writeArchive(sourcePath, bundle.ID, outputPath)
	if err != nil {
		_ = os.Remove(outputPath)
		return PackReport{}, err
	}
	digest, err := profile.BundleDigest(sourcePath)
	if err != nil {
		return PackReport{}, err
	}
	return PackReport{
		ID:           bundle.ID,
		DisplayName:  bundle.DisplayName,
		SourcePath:   sourcePath,
		OutputPath:   outputPath,
		BundleDigest: digest,
		FileCount:    fileCount,
	}, nil
}

func List(homeValue string) (ListReport, error) {
	home, err := ResolveHome(homeValue)
	if err != nil {
		return ListReport{}, err
	}
	report := ListReport{TemplatePackageHome: home, TemplatePackages: []ListItem{}, ProfileHome: home, Profiles: []ListItem{}}
	entries, err := os.ReadDir(home)
	if errors.Is(err, os.ErrNotExist) {
		return report, nil
	}
	if err != nil {
		return ListReport{}, err
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		path := filepath.Join(home, entry.Name())
		if _, err := os.Stat(filepath.Join(path, "profile.json")); err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return ListReport{}, err
		}
		bundle, err := profile.Load(path)
		if err != nil {
			report.Profiles = append(report.Profiles, invalidListItem(entry.Name(), path, err))
			continue
		}
		digest, err := profile.BundleDigest(path)
		if err != nil {
			report.Profiles = append(report.Profiles, invalidListItem(bundle.ID, path, err))
			continue
		}
		report.Profiles = append(report.Profiles, ListItem{
			TemplatePackageID:     bundle.ID,
			TemplatePackagePath:   path,
			TemplatePackageDigest: digest,
			ID:                    bundle.ID,
			DisplayName:           bundle.DisplayName,
			Path:                  path,
			BundleDigest:          digest,
			Counts:                countsFrom(bundle.Counts()),
			Valid:                 true,
		})
	}
	sort.Slice(report.Profiles, func(i, j int) bool {
		return report.Profiles[i].ID < report.Profiles[j].ID
	})
	report.TemplatePackages = report.Profiles
	return report, nil
}

func writeArchive(sourcePath string, rootName string, outputPath string) (int, error) {
	info, err := os.Stat(sourcePath)
	if err != nil {
		return 0, err
	}
	if !info.IsDir() {
		return 0, fmt.Errorf("profile pack source must be a directory: %s", sourcePath)
	}
	file, err := os.OpenFile(outputPath, os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0o644)
	if err != nil {
		return 0, err
	}
	defer file.Close()
	gzipWriter := gzip.NewWriter(file)
	defer gzipWriter.Close()
	tarWriter := tar.NewWriter(gzipWriter)
	defer tarWriter.Close()

	fileCount := 0
	err = filepath.WalkDir(sourcePath, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(sourcePath, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		if shouldSkipProfileInstallPath(rel, entry.IsDir()) {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.IsDir() {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		header, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return err
		}
		header.Name = filepath.ToSlash(filepath.Join(rootName, rel))
		if err := tarWriter.WriteHeader(header); err != nil {
			return err
		}
		source, err := os.Open(path)
		if err != nil {
			return err
		}
		if _, err := io.Copy(tarWriter, source); err != nil {
			_ = source.Close()
			return err
		}
		if err := source.Close(); err != nil {
			return err
		}
		fileCount++
		return nil
	})
	if err != nil {
		return 0, err
	}
	return fileCount, nil
}

func IsArchivePath(path string) bool {
	name := strings.ToLower(path)
	return strings.HasSuffix(name, ".tar.gz") || strings.HasSuffix(name, ".tgz")
}

func extractProfileArchive(path string) (string, func(), error) {
	tempDir, err := os.MkdirTemp("", "otsandbox-profile-archive-*")
	if err != nil {
		return "", func() {}, err
	}
	cleanup := func() {
		_ = os.RemoveAll(tempDir)
	}
	file, err := os.Open(path)
	if err != nil {
		cleanup()
		return "", func() {}, err
	}
	defer file.Close()
	gzipReader, err := gzip.NewReader(file)
	if err != nil {
		cleanup()
		return "", func() {}, err
	}
	defer gzipReader.Close()
	reader := tar.NewReader(gzipReader)
	for {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			cleanup()
			return "", func() {}, err
		}
		if err := extractArchiveEntry(reader, header, tempDir); err != nil {
			cleanup()
			return "", func() {}, err
		}
	}
	profileRoot, err := findExtractedProfileRoot(tempDir)
	if err != nil {
		cleanup()
		return "", func() {}, err
	}
	return profileRoot, cleanup, nil
}

func extractArchiveEntry(reader *tar.Reader, header *tar.Header, tempDir string) error {
	name := filepath.Clean(header.Name)
	if name == "." || filepath.IsAbs(name) || strings.HasPrefix(name, ".."+string(os.PathSeparator)) || name == ".." {
		return fmt.Errorf("archive entry escapes profile root: %s", header.Name)
	}
	targetPath := filepath.Join(tempDir, name)
	rel, err := filepath.Rel(tempDir, targetPath)
	if err != nil {
		return err
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return fmt.Errorf("archive entry escapes profile root: %s", header.Name)
	}
	switch header.Typeflag {
	case tar.TypeDir:
		return os.MkdirAll(targetPath, 0o755)
	case tar.TypeReg, tar.TypeRegA:
		if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
			return err
		}
		file, err := os.OpenFile(targetPath, os.O_CREATE|os.O_WRONLY|os.O_EXCL, os.FileMode(header.Mode).Perm())
		if err != nil {
			return err
		}
		if _, err := io.Copy(file, reader); err != nil {
			_ = file.Close()
			return err
		}
		return file.Close()
	default:
		return nil
	}
}

func findExtractedProfileRoot(tempDir string) (string, error) {
	var roots []string
	if _, err := os.Stat(filepath.Join(tempDir, "profile.json")); err == nil {
		roots = append(roots, tempDir)
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	entries, err := os.ReadDir(tempDir)
	if err != nil {
		return "", err
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		path := filepath.Join(tempDir, entry.Name())
		if _, err := os.Stat(filepath.Join(path, "profile.json")); err == nil {
			roots = append(roots, path)
		} else if err != nil && !errors.Is(err, os.ErrNotExist) {
			return "", err
		}
	}
	if len(roots) == 0 {
		return "", errors.New("profile archive does not contain profile.json")
	}
	if len(roots) > 1 {
		return "", errors.New("profile archive contains multiple profile roots")
	}
	return roots[0], nil
}

func invalidListItem(id string, path string, err error) ListItem {
	return ListItem{
		TemplatePackageID:   strings.TrimSpace(id),
		TemplatePackagePath: path,
		ID:                  strings.TrimSpace(id),
		Path:                path,
		Valid:               false,
		Error:               err.Error(),
	}
}

func IsCoreProfilesPath(path string) bool {
	clean := filepath.Clean(path)
	if clean == "profiles" || strings.HasPrefix(clean, "profiles"+string(os.PathSeparator)) {
		return true
	}
	absPath, err := filepath.Abs(clean)
	if err != nil {
		return false
	}
	coreProfilesPath, err := filepath.Abs("profiles")
	if err != nil {
		return false
	}
	rel, err := filepath.Rel(coreProfilesPath, absPath)
	if err != nil {
		return false
	}
	return rel == "." || (!strings.HasPrefix(rel, ".."+string(os.PathSeparator)) && rel != "..")
}

func safeInstallID(id string) bool {
	id = strings.TrimSpace(id)
	if id == "" || id == "." || id == ".." {
		return false
	}
	return filepath.Base(id) == id && !strings.ContainsAny(id, `/\`)
}

func copyDir(sourcePath string, targetPath string) error {
	info, err := os.Stat(sourcePath)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("profile install source must be a directory: %s", sourcePath)
	}
	return filepath.WalkDir(sourcePath, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(sourcePath, path)
		if err != nil {
			return err
		}
		if rel != "." && shouldSkipProfileInstallPath(rel, entry.IsDir()) {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		target := filepath.Join(targetPath, rel)
		if entry.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, raw, info.Mode().Perm())
	})
}

func shouldSkipProfileInstallPath(rel string, isDir bool) bool {
	parts := strings.Split(filepath.ToSlash(filepath.Clean(rel)), "/")
	for _, part := range parts {
		switch part {
		case ".git", ".hg", ".svn", ".runtime":
			return true
		}
	}
	if isDir {
		return false
	}
	name := strings.ToLower(parts[len(parts)-1])
	for _, suffix := range []string{".sqlite", ".sqlite-shm", ".sqlite-wal", ".db", ".db-shm", ".db-wal", ".log"} {
		if strings.HasSuffix(name, suffix) {
			return true
		}
	}
	return false
}

func countsFrom(counts profile.Counts) Counts {
	return Counts{
		Services:         counts.Services,
		Workflows:        counts.Workflows,
		InterfaceNodes:   counts.InterfaceNodes,
		APICases:         counts.APICases,
		RequestTemplates: counts.RequestTemplates,
		CaseDependencies: counts.CaseDependencies,
		WorkflowBindings: counts.WorkflowBindings,
		Fixtures:         counts.Fixtures,
	}
}
