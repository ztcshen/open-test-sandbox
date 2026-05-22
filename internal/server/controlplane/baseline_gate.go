package controlplane

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"open-test-sandbox/internal/domain/profile"
	"open-test-sandbox/internal/store"
)

type BaselineGateRequest struct {
	ProfileID   string `json:"profileId"`
	SubjectID   string `json:"subjectId"`
	Status      string `json:"status"`
	Required    bool   `json:"required"`
	SummaryJSON string `json:"summaryJson"`
}

type BaselineGatePayload struct {
	OK           bool             `json:"ok"`
	BaselineGate BaselineGateItem `json:"baselineGate"`
}

type BaselineGateItem struct {
	ProfileID   string    `json:"profileId"`
	SubjectID   string    `json:"subjectId"`
	Status      string    `json:"status"`
	Required    bool      `json:"required"`
	SummaryJSON string    `json:"summaryJson"`
	CheckedAt   time.Time `json:"checkedAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

func GetBaselineGatePayload(ctx context.Context, runtime store.Store, bundle profile.Bundle, profileID string, subjectID string) (BaselineGatePayload, error) {
	if runtime == nil {
		return BaselineGatePayload{}, errors.New("runtime store is not configured")
	}
	profileID = baselineProfileID(profileID, bundle)
	subjectID = strings.TrimSpace(subjectID)
	if profileID == "" {
		return BaselineGatePayload{}, errors.New("profileId is required")
	}
	if subjectID == "" {
		return BaselineGatePayload{}, errors.New("subjectId is required")
	}
	gate, err := runtime.GetBaselineGate(ctx, profileID, subjectID)
	if errors.Is(err, store.ErrNotFound) {
		return BaselineGatePayload{}, fmt.Errorf("baseline gate not found: %s %s", profileID, subjectID)
	}
	if err != nil {
		return BaselineGatePayload{}, err
	}
	return baselineGatePayload(gate), nil
}

func SetBaselineGatePayload(ctx context.Context, runtime store.Store, bundle profile.Bundle, req BaselineGateRequest) (BaselineGatePayload, error) {
	if runtime == nil {
		return BaselineGatePayload{}, errors.New("runtime store is not configured")
	}
	profileID := baselineProfileID(req.ProfileID, bundle)
	subjectID := strings.TrimSpace(req.SubjectID)
	status := strings.TrimSpace(req.Status)
	if profileID == "" {
		return BaselineGatePayload{}, errors.New("profileId is required")
	}
	if subjectID == "" {
		return BaselineGatePayload{}, errors.New("subjectId is required")
	}
	if status == "" {
		return BaselineGatePayload{}, errors.New("status is required")
	}
	summaryJSON := strings.TrimSpace(req.SummaryJSON)
	if summaryJSON == "" {
		summaryJSON = "{}"
	}
	now := time.Now().UTC()
	gate, err := runtime.UpsertBaselineGate(ctx, store.BaselineGate{
		ProfileID:   profileID,
		SubjectID:   subjectID,
		Status:      status,
		Required:    req.Required,
		SummaryJSON: summaryJSON,
		CheckedAt:   now,
		UpdatedAt:   now,
	})
	if err != nil {
		return BaselineGatePayload{}, err
	}
	return baselineGatePayload(gate), nil
}

func handleBaselineGate(w http.ResponseWriter, r *http.Request, runtime store.Store, bundle profile.Bundle) {
	switch r.Method {
	case http.MethodGet:
		payload, err := GetBaselineGatePayload(r.Context(), runtime, bundle, r.URL.Query().Get("profileId"), r.URL.Query().Get("subjectId"))
		writeBaselineGateResponse(w, err, payload)
	case http.MethodPost:
		var req BaselineGateRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSONStatus(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "invalid json"})
			return
		}
		payload, err := SetBaselineGatePayload(r.Context(), runtime, bundle, req)
		writeBaselineGateResponse(w, err, payload)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func writeBaselineGateResponse(w http.ResponseWriter, err error, payload BaselineGatePayload) {
	if err == nil {
		writeJSON(w, payload)
		return
	}
	status := http.StatusInternalServerError
	if strings.Contains(err.Error(), "not found") {
		status = http.StatusNotFound
	} else if strings.Contains(err.Error(), "required") || strings.Contains(err.Error(), "invalid") {
		status = http.StatusBadRequest
	} else if strings.Contains(err.Error(), "not configured") {
		status = http.StatusNotImplemented
	}
	writeJSONStatus(w, status, map[string]any{"ok": false, "error": err.Error()})
}

func baselineProfileID(profileID string, bundle profile.Bundle) string {
	if strings.TrimSpace(profileID) != "" {
		return strings.TrimSpace(profileID)
	}
	return strings.TrimSpace(bundle.ID)
}

func baselineGatePayload(gate store.BaselineGate) BaselineGatePayload {
	return BaselineGatePayload{
		OK: true,
		BaselineGate: BaselineGateItem{
			ProfileID:   gate.ProfileID,
			SubjectID:   gate.SubjectID,
			Status:      gate.Status,
			Required:    gate.Required,
			SummaryJSON: gate.SummaryJSON,
			CheckedAt:   gate.CheckedAt,
			UpdatedAt:   gate.UpdatedAt,
		},
	}
}
