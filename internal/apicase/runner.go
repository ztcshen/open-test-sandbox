package apicase

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
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
	BaseURL     string
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
	if !options.DryRun {
		response, assertions, err := executeHTTP(ctx, item, options.BaseURL)
		if err != nil {
			return RunResult{}, err
		}
		if assertions.Status != "passed" {
			result.Status = "failed"
		}
		if err := writeJSON(filepath.Join(evidencePath, "response.json"), response); err != nil {
			return RunResult{}, err
		}
		if err := writeJSON(filepath.Join(evidencePath, "assertions.json"), assertions); err != nil {
			return RunResult{}, err
		}
	}
	if err := writeJSON(filepath.Join(evidencePath, "summary.json"), result); err != nil {
		return RunResult{}, err
	}
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

type ResponseEvidence struct {
	StatusCode int               `json:"statusCode"`
	Headers    map[string]string `json:"headers"`
	Body       string            `json:"body"`
}

type AssertionEvidence struct {
	Status string   `json:"status"`
	Errors []string `json:"errors,omitempty"`
}

func executeHTTP(ctx context.Context, item Case, baseURL string) (ResponseEvidence, AssertionEvidence, error) {
	endpoint, err := buildURL(baseURL, item.Request.Path)
	if err != nil {
		return ResponseEvidence{}, AssertionEvidence{}, err
	}
	var body io.Reader
	if item.Request.Body != nil {
		raw, err := json.Marshal(item.Request.Body)
		if err != nil {
			return ResponseEvidence{}, AssertionEvidence{}, fmt.Errorf("encode request body: %w", err)
		}
		body = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, strings.ToUpper(item.Request.Method), endpoint, body)
	if err != nil {
		return ResponseEvidence{}, AssertionEvidence{}, fmt.Errorf("create request: %w", err)
	}
	for key, value := range item.Request.Headers {
		req.Header.Set(key, value)
	}
	if item.Request.Body != nil && req.Header.Get("Content-Type") == "" {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return ResponseEvidence{}, AssertionEvidence{}, fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()
	rawBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return ResponseEvidence{}, AssertionEvidence{}, fmt.Errorf("read response: %w", err)
	}
	response := ResponseEvidence{
		StatusCode: resp.StatusCode,
		Headers:    map[string]string{},
		Body:       string(rawBody),
	}
	for key, values := range resp.Header {
		response.Headers[key] = strings.Join(values, ", ")
	}
	return response, assertResponse(item.Assertions, response), nil
}

func buildURL(baseURL string, path string) (string, error) {
	if strings.TrimSpace(baseURL) == "" {
		return "", errors.New("base url is required for live api case runs")
	}
	base, err := url.Parse(baseURL)
	if err != nil {
		return "", fmt.Errorf("parse base url: %w", err)
	}
	relative, err := url.Parse(path)
	if err != nil {
		return "", fmt.Errorf("parse request path: %w", err)
	}
	return base.ResolveReference(relative).String(), nil
}

func assertResponse(assertions Assertions, response ResponseEvidence) AssertionEvidence {
	var failures []string
	if len(assertions.ExpectedStatusCodes) > 0 {
		found := false
		for _, code := range assertions.ExpectedStatusCodes {
			if response.StatusCode == code {
				found = true
				break
			}
		}
		if !found {
			failures = append(failures, fmt.Sprintf("status code %d was not expected", response.StatusCode))
		}
	}
	for _, fragment := range assertions.ResponseContains {
		if !strings.Contains(response.Body, fragment) {
			failures = append(failures, fmt.Sprintf("response did not contain %q", fragment))
		}
	}
	if len(failures) > 0 {
		return AssertionEvidence{Status: "failed", Errors: failures}
	}
	return AssertionEvidence{Status: "passed"}
}
