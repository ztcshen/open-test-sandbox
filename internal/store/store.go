package store

import (
	"context"
	"errors"
	"time"
)

var ErrNotFound = errors.New("store record not found")

const (
	StatusRunning = "running"
	StatusPassed  = "passed"
	StatusFailed  = "failed"
	StatusSkipped = "skipped"
)

type Store interface {
	Close() error

	CreateRun(context.Context, Run) (Run, error)
	GetRun(context.Context, string) (Run, error)
	ListRuns(context.Context) ([]Run, error)

	RecordAPICaseRun(context.Context, APICaseRun) (APICaseRun, error)
	ListAPICaseRuns(context.Context, string) ([]APICaseRun, error)

	RecordEvidence(context.Context, EvidenceRecord) (EvidenceRecord, error)
	ListEvidence(context.Context, string) ([]EvidenceRecord, error)

	UpsertBaselineGate(context.Context, BaselineGate) (BaselineGate, error)
	GetBaselineGate(context.Context, string, string) (BaselineGate, error)

	UpsertProfileIndex(context.Context, ProfileIndex) (ProfileIndex, error)
	GetProfileIndex(context.Context, string) (ProfileIndex, error)
}

type Run struct {
	ID           string
	ProfileID    string
	WorkflowID   string
	Status       string
	EvidenceRoot string
	SummaryJSON  string
	StartedAt    time.Time
	FinishedAt   time.Time
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

type APICaseRun struct {
	ID                   string
	RunID                string
	CaseID               string
	Status               string
	RequestSummaryJSON   string
	AssertionSummaryJSON string
	StartedAt            time.Time
	FinishedAt           time.Time
	CreatedAt            time.Time
}

type EvidenceRecord struct {
	ID        string
	RunID     string
	CaseRunID string
	Kind      string
	URI       string
	MediaType string
	SHA256    string
	SizeBytes int64
	Summary   string
	CreatedAt time.Time
}

type BaselineGate struct {
	ProfileID   string
	SubjectID   string
	Status      string
	Required    bool
	SummaryJSON string
	CheckedAt   time.Time
	UpdatedAt   time.Time
}

type ProfileIndex struct {
	ProfileID    string
	BundlePath   string
	BundleDigest string
	SummaryJSON  string
	ImportedAt   time.Time
	UpdatedAt    time.Time
}
