package dto

import (
	"encoding/json"
	"time"
)

type CollisionEvent struct {
	SegmentIndex    int     `json:"segment_index"`
	FirstTimeMS     float64 `json:"first_time_ms"`
	ZoneID          uint    `json:"zone_id"`
	ZoneName        string  `json:"zone_name"`
	ZoneType        string  `json:"zone_type"`
	AllowedSpeedMMS float64 `json:"allowed_speed_mm_s"`
	ActualSpeedMMS  float64 `json:"actual_speed_mm_s"`
	ClearanceMM     float64 `json:"clearance_mm"`
	Violation       bool    `json:"violation"`
	Evidence        string  `json:"evidence"`
}

type InterlockFinding struct {
	Code      string   `json:"code"`
	Event     string   `json:"event"`
	DependsOn string   `json:"depends_on,omitempty"`
	Path      []string `json:"path,omitempty"`
	Evidence  string   `json:"evidence"`
}

type CreateValidationRunRequest struct {
	MotionProgramID uint `json:"motion_program_id" validate:"required"`
	RetryFailed     bool `json:"retry_failed"`
}

type ReviewValidationRequest struct {
	Note string `json:"note" validate:"required,min=8,max=1000"`
}

// BaselineSummary describes the accepted run a validation run is regressed against.
type BaselineSummary struct {
	ID               uint       `json:"id"`
	MotionProgramID  uint       `json:"motion_program_id"`
	ProgramCode      string     `json:"program_code"`
	ProgramVersion   int        `json:"program_version"`
	AlgorithmVersion string     `json:"algorithm_version"`
	Attempt          int        `json:"attempt"`
	AcceptedAt       *time.Time `json:"accepted_at"`
}

// RegressionDiff classifies findings relative to the bound baseline.
type RegressionDiff struct {
	BaselineRunID     *uint             `json:"baseline_run_id"`
	ComparedAt        time.Time         `json:"compared_at"`
	Collision         FindingDiffBucket `json:"collision"`
	Interlock         FindingDiffBucket `json:"interlock"`
	NewCollisionCount int               `json:"new_collision_count"`
	NewInterlockCount int               `json:"new_interlock_count"`
	HasNewFindings    bool              `json:"has_new_findings"`
}

type FindingDiffBucket struct {
	New       []FindingIdentity `json:"new"`
	Gone      []FindingIdentity `json:"gone"`
	Persisted []FindingIdentity `json:"persisted"`
}

// FindingIdentity is a stable, category-independent identifier for one finding.
type FindingIdentity struct {
	Key      string `json:"key"`
	Category string `json:"category"`
	Code     string `json:"code"`
	Event    string `json:"event,omitempty"`
	ZoneID   uint   `json:"zone_id,omitempty"`
	ZoneName string `json:"zone_name,omitempty"`
	Segment  *int   `json:"segment,omitempty"`
	Evidence string `json:"evidence"`
}

type ValidationRunResponse struct {
	ID                uint               `json:"id"`
	MotionProgramID   uint               `json:"motion_program_id"`
	ProgramCode       string             `json:"program_code"`
	ProgramVersion    int                `json:"program_version"`
	ZoneSnapshot      json.RawMessage    `json:"zone_snapshot"`
	ProgramSnapshot   json.RawMessage    `json:"program_snapshot"`
	AlgorithmVersion  string             `json:"algorithm_version"`
	InputHash         string             `json:"input_hash"`
	IdempotencyKey    string             `json:"idempotency_key"`
	Attempt           int                `json:"attempt"`
	RetryOfID         *uint              `json:"retry_of_id"`
	CollisionEvents   []CollisionEvent   `json:"collision_events"`
	InterlockFindings []InterlockFinding `json:"interlock_findings"`
	RiskScore         float64            `json:"risk_score"`
	ValidationStatus  string             `json:"validation_status"`
	Explanation       string             `json:"explanation"`
	Baseline          *BaselineSummary   `json:"baseline"`
	RegressionDiff    RegressionDiff     `json:"regression_diff"`
	RequestedBy       uint               `json:"requested_by"`
	StartedAt         time.Time          `json:"started_at"`
	FinishedAt        *time.Time         `json:"finished_at"`
	ReviewedBy        *uint              `json:"reviewed_by"`
	ReviewedAt        *time.Time         `json:"reviewed_at"`
	ReviewNote        string             `json:"review_note"`
	Reused            bool               `json:"reused"`
}
