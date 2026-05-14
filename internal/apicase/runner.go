package apicase

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type Case struct {
	ID         string     `json:"id"`
	Title      string     `json:"title"`
	Request    Request    `json:"request"`
	Assertions Assertions `json:"assertions"`
}

type Request struct {
	Method  string            `json:"method"`
	Path    string            `json:"path"`
	Headers map[string]string `json:"headers,omitempty"`
	Body    map[string]any    `json:"body,omitempty"`
}

type Assertions struct {
	ExpectedStatusCodes []int    `json:"expectedStatusCodes,omitempty"`
	ResponseContains    []string `json:"responseContains,omitempty"`
}

type RunOptions struct {
	CasePath    string
	EvidenceDir string
	DryRun      bool
	RunID       string
}

type RunResult struct {
	RunID        string `json:"runId"`
	CaseID       string `json:"caseId"`
	Status       string `json:"status"`
	DryRun       bool   `json:"dryRun"`
	EvidencePath string `json:"evidencePath"`
	CreatedAt    string `json:"createdAt"`
}

func Run(ctx context.Context, options RunOptions) (RunResult, error) {
	if !options.DryRun {
		return RunResult{}, errors.New("api case live run is not implemented")
	}
	item, err := Load(options.CasePath)
	if err != nil {
		return RunResult{}, err
	}
	runID := strings.TrimSpace(options.RunID)
	if runID == "" {
		runID = "case-run-" + time.Now().UTC().Format("20060102T150405")
	}
	root := options.EvidenceDir
	if strings.TrimSpace(root) == "" {
		root = filepath.Join(".runtime", "cases")
	}
	evidencePath := filepath.Join(root, runID)
	if err := os.MkdirAll(evidencePath, 0o755); err != nil {
		return RunResult{}, fmt.Errorf("create evidence directory: %w", err)
	}

	result := RunResult{
		RunID:        runID,
		CaseID:       item.ID,
		Status:       "passed",
		DryRun:       true,
		EvidencePath: evidencePath,
		CreatedAt:    time.Now().UTC().Format(time.RFC3339Nano),
	}
	if err := writeJSON(filepath.Join(evidencePath, "case.json"), item); err != nil {
		return RunResult{}, err
	}
	if err := writeJSON(filepath.Join(evidencePath, "request.json"), item.Request); err != nil {
		return RunResult{}, err
	}
	if err := writeJSON(filepath.Join(evidencePath, "summary.json"), result); err != nil {
		return RunResult{}, err
	}
	_ = ctx
	return result, nil
}

func Load(path string) (Case, error) {
	if strings.TrimSpace(path) == "" {
		return Case{}, errors.New("case path is required")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return Case{}, fmt.Errorf("read api case: %w", err)
	}
	var item Case
	if err := json.Unmarshal(raw, &item); err != nil {
		return Case{}, fmt.Errorf("decode api case: %w", err)
	}
	if err := validate(item); err != nil {
		return Case{}, err
	}
	return item, nil
}

func validate(item Case) error {
	if strings.TrimSpace(item.ID) == "" {
		return errors.New("case id is required")
	}
	if strings.TrimSpace(item.Request.Method) == "" {
		return errors.New("request method is required")
	}
	if strings.TrimSpace(item.Request.Path) == "" {
		return errors.New("request path is required")
	}
	return nil
}

func writeJSON(path string, value any) error {
	raw, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return fmt.Errorf("encode %s: %w", path, err)
	}
	raw = append(raw, '\n')
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}
