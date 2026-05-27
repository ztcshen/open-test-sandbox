package main

import (
	"path/filepath"
	"strings"
)

func normalizePath(path string) string {
	path = filepath.ToSlash(filepath.Clean(path))
	path = strings.TrimPrefix(path, "./")
	if path == "." {
		return ""
	}
	return path
}

func shouldSkipPath(path string, cfg Config) bool {
	rel := normalizePath(path)
	if rel == "" {
		return false
	}
	parts := strings.Split(rel, "/")
	for _, part := range parts {
		if cfg.ExcludedDirs[part] {
			return true
		}
	}
	for _, suffix := range cfg.ExcludedFileSuffixes {
		if strings.HasSuffix(rel, suffix) {
			return true
		}
	}
	for _, fragment := range cfg.GeneratedPathFragments {
		if strings.Contains("/"+rel+"/", fragment) {
			return true
		}
	}
	return false
}

func isGeneratedPath(path string, cfg Config) bool {
	rel := normalizePath(path)
	for _, suffix := range cfg.GeneratedFileSuffixes {
		if strings.HasSuffix(rel, suffix) {
			return true
		}
	}
	for _, fragment := range cfg.GeneratedPathFragments {
		if strings.Contains("/"+rel+"/", fragment) {
			return true
		}
	}
	return false
}

func hasPathSegment(path string, names ...string) bool {
	parts := strings.Split(normalizePath(path), "/")
	for _, part := range parts {
		for _, name := range names {
			if part == name {
				return true
			}
		}
	}
	return false
}

func scopeMatcher(paths []string) func(string) bool {
	normalized := make([]string, 0, len(paths))
	for _, path := range paths {
		path = normalizePath(path)
		if path != "" {
			normalized = append(normalized, path)
		}
	}
	return func(path string) bool {
		if len(normalized) == 0 {
			return true
		}
		path = normalizePath(path)
		for _, item := range normalized {
			if path == item || strings.HasPrefix(path, item+"/") || strings.HasPrefix(item, path+"/") {
				return true
			}
		}
		return false
	}
}

func isConfigOrEnumFile(path string) bool {
	base := strings.ToLower(filepath.Base(path))
	return strings.Contains(base, "config") ||
		strings.Contains(base, "constant") ||
		strings.Contains(base, "enum") ||
		strings.Contains(base, "schema")
}

func isRouteRegistrationFile(path string) bool {
	base := strings.ToLower(filepath.Base(path))
	return strings.HasPrefix(base, "routes_") || strings.HasSuffix(base, "_routes.go")
}

func isCLISurfaceFile(path string) bool {
	return strings.HasPrefix(normalizePath(path), "cmd/agent-testbench/")
}

func isControlPlaneSurfaceFile(path string) bool {
	return strings.HasPrefix(normalizePath(path), "internal/server/controlplane/")
}

func isStoreSchemaSurfaceFile(path string) bool {
	path = normalizePath(path)
	base := strings.ToLower(filepath.Base(path))
	return strings.HasPrefix(path, "internal/store/schema/") ||
		strings.HasPrefix(path, "internal/store/sqlstore/") ||
		strings.HasPrefix(path, "internal/store/sqlite/") ||
		strings.Contains(base, "migration") ||
		strings.Contains(base, "dialect")
}

func isProfileArtifactSurfaceFile(path string) bool {
	path = normalizePath(path)
	return strings.HasPrefix(path, "internal/domain/profilehome/") ||
		strings.HasPrefix(path, "internal/domain/profileimport/") ||
		strings.HasPrefix(path, "internal/profilepublish/")
}

func isRunnerEvidenceSurfaceFile(path string) bool {
	return strings.HasPrefix(normalizePath(path), "internal/runner/evidence/")
}

func isStoreContractSurfaceFile(path string) bool {
	path = normalizePath(path)
	return path == "internal/store/store.go" ||
		strings.HasPrefix(path, "internal/store/mysql/") ||
		strings.HasPrefix(path, "internal/store/postgres/")
}

func isQualityGateToolFile(path string) bool {
	return strings.HasPrefix(normalizePath(path), "tools/qualitygate/")
}

func isCorePath(path string) bool {
	path = normalizePath(path)
	return strings.HasPrefix(path, "cmd/") ||
		strings.HasPrefix(path, "internal/") ||
		strings.HasPrefix(path, "pkg/")
}

func packageDirForFile(path string) string {
	path = normalizePath(path)
	dir := normalizePath(filepath.Dir(path))
	if dir == "." {
		return ""
	}
	return dir
}
