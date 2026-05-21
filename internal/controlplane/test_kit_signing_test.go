package controlplane

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"open-test-sandbox/internal/profile"
	"open-test-sandbox/internal/store"
)

func TestBuildCaseHTTPRequestAddsConfiguredAuthorization(t *testing.T) {
	keyPath := writeTestSigningKey(t)
	execution := caseExecutionConfig{
		Method: "POST",
		NodeID: "service.alpha",
		Path:   "/v1/items",
		Query:  map[string]any{"order_id": "ORDER-1"},
		Body:   map[string]any{"amount": 100},
		Auth: map[string]any{
			"credentialId":     "credential-1",
			"keyPath":          keyPath,
			"providerSerialNo": "provider-1",
			"serialNo":         "serial-1",
		},
		Signed: true,
	}

	request, err := buildCaseHTTPRequest(context.Background(), profile.Bundle{}, nil, execution, "", map[string]any{"baseUrl": "http://127.0.0.1:1234"})
	if err != nil {
		t.Fatalf("build request: %v", err)
	}

	auth := request.headers["Authorization"]
	if !strings.HasPrefix(auth, "RSA ") {
		t.Fatalf("authorization header = %q", auth)
	}
	for _, want := range []string{`credential_id="credential-1"`, `serial_no="serial-1"`, `provider_serial_no="provider-1"`} {
		if !strings.Contains(auth, want) {
			t.Fatalf("authorization header %q does not contain %s", auth, want)
		}
	}
	if request.headers["X-Forwarded-For"] == "" || request.headers["X-Real-IP"] == "" {
		t.Fatalf("expected default forwarding headers, got %#v", request.headers)
	}
}

func TestApplyAPICaseRequestModelAppliesTemplatePatchAndExpectedJSON(t *testing.T) {
	request := caseHTTPRequest{
		body: map[string]any{
			"loan_apply_infos": []any{
				map[string]any{
					"credit_contract_id": "CONTRACT-1",
					"loan_amount":        12000,
				},
			},
		},
		expectedHTTPCodes: []int{200},
	}
	item := profile.APICase{
		ID:           "case.patch",
		RenderMode:   "template_patch",
		PatchJSON:    `[{"op":"remove","path":"$.loan_apply_infos[0].credit_contract_id"},{"op":"replace","path":"$.loan_apply_infos[0].loan_amount","value":0}]`,
		ExpectedJSON: `{"expectedHttpCodes":[400],"expectedResponseContains":["PARAM_ERROR"]}`,
	}

	if err := applyAPICaseRequestModel(&request, item); err != nil {
		t.Fatalf("apply request model: %v", err)
	}
	body := request.body.(map[string]any)
	first := body["loan_apply_infos"].([]any)[0].(map[string]any)
	if _, ok := first["credit_contract_id"]; ok {
		t.Fatalf("credit_contract_id was not removed: %#v", first)
	}
	if first["loan_amount"] != float64(0) {
		t.Fatalf("loan_amount = %#v", first["loan_amount"])
	}
	if len(request.expectedHTTPCodes) != 1 || request.expectedHTTPCodes[0] != 400 {
		t.Fatalf("expected status codes = %#v", request.expectedHTTPCodes)
	}
	if len(request.expectedResponse) != 1 || request.expectedResponse[0] != "PARAM_ERROR" {
		t.Fatalf("expected response = %#v", request.expectedResponse)
	}
}

func TestApplyAPICaseRequestModelPatchesGETQueryWithoutBody(t *testing.T) {
	request := caseHTTPRequest{
		method:  "GET",
		fullURL: "http://127.0.0.1:1234/v1/result?merchant_id=M1&order_id=O1&client_ip=127.0.0.1",
		body:    nil,
		expectedHTTPCodes: []int{
			200,
		},
	}
	item := profile.APICase{
		ID:           "case.query.patch",
		RenderMode:   "template_patch",
		PatchJSON:    `[{"op":"remove","path":"$.client_ip"},{"op":"replace","path":"$.order_id","value":"O2"}]`,
		ExpectedJSON: `{"expectedHttpCodes":[400],"expectedResponseContains":["PARAM_ERROR"]}`,
	}

	if err := applyAPICaseRequestModel(&request, item); err != nil {
		t.Fatalf("apply request model: %v", err)
	}
	parsed, err := url.Parse(request.fullURL)
	if err != nil {
		t.Fatalf("parse full url: %v", err)
	}
	query := parsed.Query()
	if query.Get("client_ip") != "" {
		t.Fatalf("client_ip was not removed from query: %s", request.fullURL)
	}
	if query.Get("order_id") != "O2" {
		t.Fatalf("order_id query = %q in %s", query.Get("order_id"), request.fullURL)
	}
	if request.body != nil {
		t.Fatalf("GET patch should not create a request body: %#v", request.body)
	}
	if len(request.expectedHTTPCodes) != 1 || request.expectedHTTPCodes[0] != 400 {
		t.Fatalf("expected status codes = %#v", request.expectedHTTPCodes)
	}
	if len(request.expectedResponse) != 1 || request.expectedResponse[0] != "PARAM_ERROR" {
		t.Fatalf("expected response = %#v", request.expectedResponse)
	}
}

func TestApplyAPICaseRequestModelPatchesEquivalentBodyFields(t *testing.T) {
	request := caseHTTPRequest{
		method: "POST",
		body: map[string]any{
			"action": 20001,
			"data": map[string]any{
				"approvalStatus": 2,
				"orderId":        "ORDER-1",
			},
		},
		expectedHTTPCodes: []int{200},
	}
	item := profile.APICase{
		ID:           "case.body.equivalent.patch",
		RenderMode:   "template_patch",
		PatchJSON:    `[{"op":"replace","path":"$.approval_status","value":999},{"op":"remove","path":"$.financing_order_id"}]`,
		ExpectedJSON: `{"expectedHttpCodes":[200],"expectedResponseContains":["\"code\":-1"]}`,
	}

	if err := applyAPICaseRequestModel(&request, item); err != nil {
		t.Fatalf("apply request model: %v", err)
	}
	body := request.body.(map[string]any)
	data := body["data"].(map[string]any)
	if data["approvalStatus"] != float64(999) {
		t.Fatalf("approvalStatus = %#v", data["approvalStatus"])
	}
	if _, ok := data["orderId"]; ok {
		t.Fatalf("orderId was not removed: %#v", data)
	}
}

func TestApplyAPICaseRequestModelRendersPatchValues(t *testing.T) {
	request := caseHTTPRequest{
		method: "POST",
		body: map[string]any{
			"project_id": "688968",
		},
		expectedHTTPCodes: []int{200},
	}
	item := profile.APICase{
		ID:         "case.patch.render",
		RenderMode: "template_patch",
		PatchJSON:  `[{"op":"replace","path":"$.project_id","value":"PROJECT_NOT_EXIST_{{serial:NP}}"}]`,
	}

	if err := applyAPICaseRequestModel(&request, item); err != nil {
		t.Fatalf("apply request model: %v", err)
	}
	body := request.body.(map[string]any)
	if strings.Contains(valueString(body["project_id"]), "{{serial:NP}}") {
		t.Fatalf("patch value was not rendered: %#v", body["project_id"])
	}
}

func TestApplyAPICaseRequestModelMergesSandboxCallbackTemplateModel(t *testing.T) {
	request := caseHTTPRequest{
		method:  "POST",
		fullURL: "http://127.0.0.1:28080/__sandbox/llt/callback",
		body: map[string]any{
			"action": 20001,
			"data": map[string]any{
				"approvalStatus": 2,
				"orderId":        "ORDER-1",
			},
		},
		expectedHTTPCodes: []int{200},
	}
	item := profile.APICase{
		ID:                  "case.callback.template.model",
		RenderMode:          "template_patch",
		PayloadTemplateJSON: `{"receiver_public_key":"PUB-1","sign_required":false,"transaction_id":"{{serial:TXN}}"}`,
		PatchJSON:           `[{"op":"remove","path":"$.receiver_public_key"}]`,
	}

	if err := applyAPICaseRequestModel(&request, item); err != nil {
		t.Fatalf("apply request model: %v", err)
	}
	body := request.body.(map[string]any)
	if _, ok := body["receiver_public_key"]; ok {
		t.Fatalf("receiver_public_key should be removed: %#v", body)
	}
	if body["sign_required"] != false {
		t.Fatalf("sign_required = %#v", body["sign_required"])
	}
	if strings.Contains(valueString(body["transaction_id"]), "{{") {
		t.Fatalf("transaction_id was not rendered: %#v", body["transaction_id"])
	}
}

func TestRenderCaseStringLeavesUnknownTokensWithoutLooping(t *testing.T) {
	got := renderCaseString("{{ unknown.token }}-{{serial:OK}}", nil)
	if !strings.HasPrefix(got, "{{ unknown.token }}-OK") {
		t.Fatalf("rendered string = %q", got)
	}
}

func TestRenderCaseStringRendersDynamicDefaultValues(t *testing.T) {
	got := renderCaseString("{{override:finished_at|now:datetime}}", nil)
	if got == "now:datetime" || !strings.Contains(got, " ") {
		t.Fatalf("rendered default = %q", got)
	}
}

func TestRenderCaseStringRendersDynamicOverrideValues(t *testing.T) {
	got := renderCaseString("{{override:finished_at|}}", map[string]any{"finished_at": "now:datetime"})
	if got == "now:datetime" || !strings.Contains(got, " ") {
		t.Fatalf("rendered override = %q", got)
	}
}

func TestSerialValueIsUniqueWithinSameSecond(t *testing.T) {
	first := serialValue("GEN")
	second := serialValue("GEN")
	if first == second {
		t.Fatalf("serial values should be unique: %q", first)
	}
}

func TestDeriveCaseExecutionConfigFromCatalogUsesRequestTemplate(t *testing.T) {
	catalog := store.ProfileCatalog{
		RequestTemplates: []store.CatalogRequestTemplate{
			{
				ID:           "tpl.case",
				NodeID:       "node.alpha",
				Method:       "GET",
				Path:         "/v1/query",
				TemplateJSON: `{"order_id":"{{serial:ORD}}","mode":"{{override:mode|ok}}"}`,
				Status:       "active",
			},
		},
	}
	item := store.CatalogAPICase{
		ID:                "case.alpha",
		NodeID:            "node.alpha",
		RequestTemplateID: "tpl.case",
		ExpectedJSON:      `{"expectedHttpCodes":[404],"expectedResponseContains":["ORDER_NOT_EXIST"],"requireRequestId":true}`,
	}

	execution := deriveCaseExecutionConfigFromCatalog(catalog, item)
	if execution == nil {
		t.Fatal("expected derived execution config")
	}
	if execution.Method != "GET" || execution.NodeID != "node.alpha" || execution.Path != "/v1/query" {
		t.Fatalf("execution identity = %#v", execution)
	}
	if execution.Query["mode"] != "{{override:mode|ok}}" {
		t.Fatalf("execution query = %#v", execution.Query)
	}
	if len(execution.ExpectedHTTPCodes) != 1 || execution.ExpectedHTTPCodes[0] != 404 || !execution.RequireRequestID {
		t.Fatalf("execution expected config = %#v", execution)
	}
}

func TestDeriveCaseExecutionConfigFromCatalogPrefersSiblingConfig(t *testing.T) {
	catalog := store.ProfileCatalog{
		RequestTemplates: []store.CatalogRequestTemplate{
			{
				ID:           "tpl.callback",
				NodeID:       "node.callback",
				Method:       "POST",
				Path:         "/provider/raw",
				TemplateJSON: `{"action":30001}`,
				Status:       "active",
			},
		},
		TemplateConfigs: []store.CatalogTemplateConfig{
			{
				ID:         "cfg.callback.success",
				NodeID:     "node.callback",
				Status:     "active",
				ConfigJSON: `{"caseId":"case.success","caseExecution":{"method":"POST","nodeId":"node.callback","path":"/__sandbox/llt/callback","body":{"action":30001},"expectedHttpCodes":[200]}}`,
			},
		},
	}
	item := store.CatalogAPICase{
		ID:                "case.missing-action",
		NodeID:            "node.callback",
		RequestTemplateID: "tpl.callback",
		ExpectedJSON:      `{"expectedHttpCodes":[200],"expectedResponseContains":["\"code\":-1"]}`,
	}

	execution := deriveCaseExecutionConfigFromCatalog(catalog, item)
	if execution == nil {
		t.Fatal("expected derived execution config")
	}
	if execution.Path != "/__sandbox/llt/callback" {
		t.Fatalf("execution path = %q", execution.Path)
	}
	if len(execution.ExpectedResponse) != 1 || execution.ExpectedResponse[0] != `"code":-1` {
		t.Fatalf("execution expected response = %#v", execution.ExpectedResponse)
	}
}

func writeTestSigningKey(t *testing.T) string {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	raw := x509.MarshalPKCS1PrivateKey(key)
	path := filepath.Join(t.TempDir(), "request-key.pem")
	file, err := os.Create(path)
	if err != nil {
		t.Fatalf("create key: %v", err)
	}
	defer file.Close()
	if err := pem.Encode(file, &pem.Block{Type: "RSA PRIVATE KEY", Bytes: raw}); err != nil {
		t.Fatalf("write key: %v", err)
	}
	return path
}
