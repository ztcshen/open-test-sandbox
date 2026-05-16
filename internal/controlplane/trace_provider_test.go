package controlplane

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestGraphQLTraceProviderFindsCandidatesAndQueriesSpans(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload struct {
			Query     string         `json:"query"`
			Variables map[string]any `json:"variables"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode provider request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.Contains(payload.Query, "queryBasicTraces"):
			_, _ = w.Write([]byte(`{"data":{"queryBasicTraces":{"traces":[{"endpointNames":["POST:/alpha"],"duration":120,"start":"2026-05-14 1015","isError":false,"traceIds":["trace.alpha"]}]}}}`))
		case strings.Contains(payload.Query, "queryTrace"):
			_, _ = w.Write([]byte(`{"data":{"queryTrace":{"spans":[{"traceId":"trace.alpha","segmentId":"segment.entry","spanId":0,"parentSpanId":-1,"refs":[],"serviceCode":"service.entry","endpointName":"/alpha","type":"Entry","component":"Tomcat"},{"traceId":"trace.alpha","segmentId":"segment.worker","spanId":0,"parentSpanId":-1,"refs":[{"traceId":"trace.alpha","parentSegmentId":"segment.entry","parentSpanId":0,"type":"CrossProcess"}],"serviceCode":"service.worker","endpointName":"POST:/alpha","type":"Entry","component":"Server"}]}}}`))
		default:
			t.Fatalf("unexpected provider query: %s", payload.Query)
		}
	}))
	defer server.Close()

	provider := graphQLTraceProvider{URL: server.URL}
	candidates, err := provider.FindCandidates(t.Context(), "/alpha", time.Date(2026, 5, 14, 10, 14, 0, 0, time.UTC), time.Date(2026, 5, 14, 10, 16, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("find candidates: %v", err)
	}
	if len(candidates) != 1 || candidates[0].TraceID != "trace.alpha" {
		t.Fatalf("candidates = %#v", candidates)
	}
	trace, err := provider.QueryTrace(t.Context(), candidates[0].TraceID)
	if err != nil {
		t.Fatalf("query trace: %v", err)
	}
	topology := buildTraceTopology("step.alpha", "case.alpha", "request.alpha", trace)
	if topology.Status != "complete" || len(topology.ConfirmedEdges) != 1 {
		t.Fatalf("topology = %#v", topology)
	}
}
