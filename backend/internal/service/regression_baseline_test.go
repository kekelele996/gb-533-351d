package service

import (
	"encoding/json"
	"testing"
	"time"

	"robot-cell-safety-envelope-validator/backend/internal/constants"
	"robot-cell-safety-envelope-validator/backend/internal/dto"
	"robot-cell-safety-envelope-validator/backend/internal/model"
)

func collisionEvent(segment int, zoneID uint, zoneName string, violation bool) dto.CollisionEvent {
	return dto.CollisionEvent{
		SegmentIndex: segment, FirstTimeMS: 1000, ZoneID: zoneID, ZoneName: zoneName,
		ZoneType: "restricted", ActualSpeedMMS: 400, AllowedSpeedMMS: 250, ClearanceMM: -30,
		Violation: violation, Evidence: "test envelope evidence",
	}
}

func interlockFinding(code, event, dependsOn string) dto.InterlockFinding {
	return dto.InterlockFinding{Code: code, Event: event, DependsOn: dependsOn, Evidence: "test interlock evidence"}
}

func runWithFindings(id uint, code string, status string, collisions []dto.CollisionEvent, findings []dto.InterlockFinding) model.ValidationRun {
	collisionJSON, _ := json.Marshal(collisions)
	findingJSON, _ := json.Marshal(findings)
	finished := time.Now().UTC()
	return model.ValidationRun{
		ID: id, AlgorithmVersion: "envelope-2d-height-v1.0", ValidationStatus: status,
		CollisionEventsJSON: string(collisionJSON), InterlockFindingsJSON: string(findingJSON),
		StartedAt: finished.Add(-time.Second), FinishedAt: &finished,
		MotionProgram: model.MotionProgram{ID: 100 + id, ProgramCode: code, Version: 1},
	}
}

func TestCompareRunsClassifiesNewGoneAndPersisted(t *testing.T) {
	baseline := runWithFindings(1, "WELD-01", constants.ValidationAccepted,
		[]dto.CollisionEvent{collisionEvent(0, 10, "gate", true), collisionEvent(1, 11, "aisle", true)},
		[]dto.InterlockFinding{interlockFinding("missing_prerequisite", "motion_start", "gate_locked")})
	current := runWithFindings(2, "WELD-01", constants.ValidationFailed,
		[]dto.CollisionEvent{collisionEvent(0, 10, "gate", true), collisionEvent(2, 12, "curtain", true)},
		[]dto.InterlockFinding{interlockFinding("dependency_cycle", "loop_a", "")})

	diff, err := compareRuns(&current, &baseline)
	if err != nil {
		t.Fatalf("compareRuns returned error: %v", err)
	}
	if *diff.BaselineRunID != 1 {
		t.Fatalf("baseline run id = %v, want 1", *diff.BaselineRunID)
	}
	if len(diff.Collision.Persisted) != 1 || diff.Collision.Persisted[0].Key != "segment:0|zone:10" {
		t.Fatalf("persisted collisions = %+v", diff.Collision.Persisted)
	}
	if len(diff.Collision.New) != 1 || diff.Collision.New[0].Key != "segment:2|zone:12" {
		t.Fatalf("new collisions = %+v", diff.Collision.New)
	}
	if len(diff.Collision.Gone) != 1 || diff.Collision.Gone[0].Key != "segment:1|zone:11" {
		t.Fatalf("gone collisions = %+v", diff.Collision.Gone)
	}
	if len(diff.Interlock.New) != 1 || diff.Interlock.New[0].Code != "dependency_cycle" {
		t.Fatalf("new interlocks = %+v", diff.Interlock.New)
	}
	if len(diff.Interlock.Gone) != 1 || diff.Interlock.Gone[0].Event != "motion_start" {
		t.Fatalf("gone interlocks = %+v", diff.Interlock.Gone)
	}
	if !diff.HasNewFindings || diff.NewCollisionCount != 1 || diff.NewInterlockCount != 1 {
		t.Fatalf("diff flags = %+v", diff)
	}
}

func TestCompareRunsWithoutBaselineTreatsAllAsNew(t *testing.T) {
	current := runWithFindings(3, "FRESH-01", constants.ValidationFailed,
		[]dto.CollisionEvent{collisionEvent(0, 10, "gate", true)},
		[]dto.InterlockFinding{interlockFinding("reversed_order", "b", "a")})
	diff, err := compareRuns(&current, nil)
	if err != nil {
		t.Fatalf("compareRuns returned error: %v", err)
	}
	if diff.BaselineRunID != nil {
		t.Fatalf("baseline run id should be nil")
	}
	if len(diff.Collision.New) != 1 || len(diff.Collision.Gone) != 0 || len(diff.Collision.Persisted) != 0 {
		t.Fatalf("collision buckets = %+v", diff.Collision)
	}
	if len(diff.Interlock.New) != 1 {
		t.Fatalf("interlock buckets = %+v", diff.Interlock)
	}
	if !diff.HasNewFindings {
		t.Fatalf("first run without baseline must flag every finding as new")
	}
}

func TestCollisionKeyIgnoresSampledTimeAndSeverity(t *testing.T) {
	left := collisionEvent(1, 10, "gate", true)
	right := collisionEvent(1, 10, "gate", false)
	right.FirstTimeMS = 9876
	right.ClearanceMM = 5
	if collisionKey(left) != collisionKey(right) {
		t.Fatalf("same segment/zone contact must share a key regardless of sampling or severity")
	}
}

func TestRegressionDiffRoundTrip(t *testing.T) {
	current := runWithFindings(4, "WELD-01", constants.ValidationFailed,
		[]dto.CollisionEvent{collisionEvent(0, 10, "gate", true)}, nil)
	baseline := runWithFindings(1, "WELD-01", constants.ValidationAccepted, nil, nil)
	diff, err := compareRuns(&current, &baseline)
	if err != nil {
		t.Fatalf("compareRuns returned error: %v", err)
	}
	encoded := encodeRegressionDiff(diff)
	decoded := decodeRegressionDiff(encoded)
	if !decoded.HasNewFindings || decoded.NewCollisionCount != 1 || decoded.BaselineRunID == nil {
		t.Fatalf("round trip diff = %+v", decoded)
	}
	if decodeRegressionDiff("").HasNewFindings {
		t.Fatalf("legacy empty diff must decode to a zero value")
	}
}
