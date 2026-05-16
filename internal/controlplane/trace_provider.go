package controlplane

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"
)

const traceProviderQueryTimeout = 5 * time.Second

type traceCandidate struct {
	TraceID  string
	Start    string
	Duration int
	IsError  bool
}

type basicTrace struct {
	EndpointNames []string `json:"endpointNames"`
	Duration      int      `json:"duration"`
	Start         string   `json:"start"`
	IsError       bool     `json:"isError"`
	TraceIDs      []string `json:"traceIds"`
}

type graphQLTraceProvider struct {
	URL string
}

func (p graphQLTraceProvider) FindCandidates(ctx context.Context, endpoint string, startedAt, endedAt time.Time) ([]traceCandidate, error) {
	endpoint = normalizeTraceEndpoint(endpoint)
	if endpoint == "" {
		return nil, fmt.Errorf("empty endpoint")
	}
	var response struct {
		Data struct {
			QueryBasicTraces struct {
				Traces []basicTrace `json:"traces"`
			} `json:"queryBasicTraces"`
		} `json:"data"`
		Errors []map[string]any `json:"errors"`
	}
	variables := map[string]any{
		"condition": map[string]any{
			"queryDuration": map[string]any{
				"start": startedAt.UTC().Format("2006-01-02 1504"),
				"end":   endedAt.UTC().Format("2006-01-02 1504"),
				"step":  "MINUTE",
			},
			"traceState": "ALL",
			"queryOrder": "BY_START_TIME",
			"paging":     map[string]any{"pageNum": 1, "pageSize": 100},
		},
	}
	if err := p.graphQL(ctx, `query($condition: TraceQueryCondition){ queryBasicTraces(condition:$condition){ traces { endpointNames duration start isError traceIds } } }`, variables, &response); err != nil {
		return nil, err
	}
	if len(response.Errors) > 0 {
		return nil, fmt.Errorf("trace provider queryBasicTraces returned %d errors", len(response.Errors))
	}
	candidates := []traceCandidate{}
	seen := map[string]bool{}
	for _, trace := range response.Data.QueryBasicTraces.Traces {
		if len(trace.TraceIDs) == 0 || !traceMatchesEndpoint(trace, endpoint) {
			continue
		}
		for _, traceID := range trace.TraceIDs {
			traceID = strings.TrimSpace(traceID)
			if traceID == "" || seen[traceID] {
				continue
			}
			seen[traceID] = true
			candidates = append(candidates, traceCandidate{
				TraceID:  traceID,
				Start:    trace.Start,
				Duration: trace.Duration,
				IsError:  trace.IsError,
			})
		}
	}
	if len(candidates) == 0 {
		return nil, fmt.Errorf("trace is not indexed yet: endpoint %s", endpoint)
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		return candidates[i].Start > candidates[j].Start
	})
	return candidates, nil
}

func (p graphQLTraceProvider) QueryTrace(ctx context.Context, traceID string) (traceData, error) {
	var response struct {
		Data struct {
			QueryTrace traceData `json:"queryTrace"`
		} `json:"data"`
		Errors []map[string]any `json:"errors"`
	}
	if err := p.graphQL(ctx, `query($traceId: ID!){ queryTrace(traceId:$traceId){ spans { traceId segmentId spanId parentSpanId refs { traceId parentSegmentId parentSpanId type } serviceCode serviceInstanceName startTime endTime endpointName type peer component isError layer } } }`, map[string]any{"traceId": traceID}, &response); err != nil {
		return traceData{}, err
	}
	if len(response.Errors) > 0 {
		return traceData{}, fmt.Errorf("trace provider queryTrace returned %d errors", len(response.Errors))
	}
	return response.Data.QueryTrace, nil
}

func (p graphQLTraceProvider) graphQL(ctx context.Context, query string, variables map[string]any, target any) error {
	if strings.TrimSpace(p.URL) == "" {
		return fmt.Errorf("trace provider GraphQL URL is not configured")
	}
	body, err := json.Marshal(map[string]any{"query": query, "variables": variables})
	if err != nil {
		return err
	}
	client := &http.Client{Timeout: traceProviderQueryTimeout}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.URL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("trace provider GraphQL request failed: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return fmt.Errorf("trace provider GraphQL returned HTTP %d", res.StatusCode)
	}
	if err := json.NewDecoder(res.Body).Decode(target); err != nil {
		return fmt.Errorf("decode trace provider GraphQL response: %w", err)
	}
	return nil
}

func traceMatchesEndpoint(trace basicTrace, endpoint string) bool {
	endpoint = normalizeTraceEndpoint(endpoint)
	for _, name := range trace.EndpointNames {
		normalized := normalizeTraceEndpoint(name)
		if normalized == endpoint || strings.Contains(normalized, endpoint) || strings.Contains(endpoint, normalized) {
			return true
		}
	}
	return false
}

func normalizeTraceEndpoint(endpoint string) string {
	endpoint = strings.TrimSpace(endpoint)
	if endpoint == "" {
		return ""
	}
	if idx := strings.Index(endpoint, "?"); idx >= 0 {
		endpoint = endpoint[:idx]
	}
	if idx := strings.Index(endpoint, ":/"); idx >= 0 {
		endpoint = endpoint[idx+1:]
	}
	return endpoint
}
