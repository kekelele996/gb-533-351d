package service

import (
	"encoding/json"
	"fmt"

	"robot-cell-safety-envelope-validator/backend/internal/dto"
	"robot-cell-safety-envelope-validator/backend/internal/model"
)

func unmarshalJSON(raw string, target any) error {
	return json.Unmarshal([]byte(raw), target)
}

// A regression baseline is an accepted run for the same robot cell, the same
// program code and the same algorithm version. Findings are classified by
// stable logical identities so that zone renames or a shifted first-contact
// sample do not hide a real regression.

func collisionKey(event dto.CollisionEvent) string {
	return fmt.Sprintf("seg:%d|zone:%d", event.SegmentIndex, event.ZoneID)
}

func interlockKey(finding dto.InterlockFinding) string {
	switch finding.Code {
	case "missing_prerequisite":
		return finding.Code + "|" + finding.Event + "|" + finding.DependsOn
	case "reversed_order":
		return finding.Code + "|" + finding.Event + "|" + finding.DependsOn
	case "dependency_cycle":
		return finding.Code + "|" + cycleSignature(finding.Path)
	default:
		return finding.Code + "|" + finding.Event
	}
}

// cycleSignature canonicalizes a directed cycle so that the same closed loop,
// regardless of which node the DFS started on, compares equal.
func cycleSignature(path []string) string {
	if len(path) <= 1 {
		return joinPath(path)
	}
	ring := path[:len(path)-1]
	rotations := make([]string, 0, len(ring))
	for start := range ring {
		rotated := make([]string, 0, len(ring))
		for offset := range ring {
			rotated = append(rotated, ring[(start+offset)%len(ring)])
		}
		rotations = append(rotations, joinPath(rotated))
	}
	signature := rotations[0]
	for _, candidate := range rotations[1:] {
		if candidate < signature {
			signature = candidate
		}
	}
	return signature
}

func joinPath(path []string) string {
	result := ""
	for index, item := range path {
		if index > 0 {
			result += ">"
		}
		result += item
	}
	return result
}

func buildRegressionBaseline(run, baseline model.ValidationRun) (*dto.RegressionBaseline, error) {
	var runCollisions []dto.CollisionEvent
	if err := unmarshalJSON(run.CollisionEventsJSON, &runCollisions); err != nil {
		return nil, fmt.Errorf("run collisions: %w", err)
	}
	var baselineCollisions []dto.CollisionEvent
	if err := unmarshalJSON(baseline.CollisionEventsJSON, &baselineCollisions); err != nil {
		return nil, fmt.Errorf("baseline collisions: %w", err)
	}
	var runFindings []dto.InterlockFinding
	if err := unmarshalJSON(run.InterlockFindingsJSON, &runFindings); err != nil {
		return nil, fmt.Errorf("run interlock findings: %w", err)
	}
	var baselineFindings []dto.InterlockFinding
	if err := unmarshalJSON(baseline.InterlockFindingsJSON, &baselineFindings); err != nil {
		return nil, fmt.Errorf("baseline interlock findings: %w", err)
	}
	return buildRegressionBaselineFrom(runCollisions, runFindings, baseline, baselineCollisions, baselineFindings), nil
}

func buildRegressionBaselineFrom(runCollisions []dto.CollisionEvent, runFindings []dto.InterlockFinding, baseline model.ValidationRun, baselineCollisions []dto.CollisionEvent, baselineFindings []dto.InterlockFinding) *dto.RegressionBaseline {
	collisionAdded, collisionRemoved, collisionPersisted := classifyCollisions(runCollisions, baselineCollisions)
	interlockAdded, interlockRemoved, interlockPersisted := classifyInterlocks(runFindings, baselineFindings)
	return &dto.RegressionBaseline{
		Bound: dto.BaselineSummary{
			ID: baseline.ID, MotionProgramID: baseline.MotionProgramID,
			ProgramCode: baseline.MotionProgram.ProgramCode, ProgramVersion: baseline.MotionProgram.Version,
			AlgorithmVersion: baseline.AlgorithmVersion, ValidationStatus: baseline.ValidationStatus,
			AcceptedAt: baseline.ReviewedAt,
		},
		CollisionDiff: dto.FindingCategoryDiff{
			Added: collisionAdded, Removed: collisionRemoved, Persisted: collisionPersisted,
		},
		InterlockDiff: dto.InterlockCategoryDiff{
			Added: interlockAdded, Removed: interlockRemoved, Persisted: interlockPersisted,
		},
		HasNewFindings: len(collisionAdded) > 0 || len(interlockAdded) > 0,
	}
}

func classifyCollisions(current, baseline []dto.CollisionEvent) (added, removed, persisted []dto.CollisionDiff) {
	added = make([]dto.CollisionDiff, 0)
	removed = make([]dto.CollisionDiff, 0)
	persisted = make([]dto.CollisionDiff, 0)
	baselineByKey := make(map[string]dto.CollisionEvent, len(baseline))
	for _, event := range baseline {
		baselineByKey[collisionKey(event)] = event
	}
	currentByKey := make(map[string]dto.CollisionEvent, len(current))
	for _, event := range current {
		key := collisionKey(event)
		currentByKey[key] = event
		if _, exists := baselineByKey[key]; exists {
			persisted = append(persisted, toCollisionDiff(event))
		} else {
			added = append(added, toCollisionDiff(event))
		}
	}
	for key, event := range baselineByKey {
		if _, exists := currentByKey[key]; !exists {
			removed = append(removed, toCollisionDiff(event))
		}
	}
	return added, removed, persisted
}

func classifyInterlocks(current, baseline []dto.InterlockFinding) (added, removed, persisted []dto.InterlockDiff) {
	added = make([]dto.InterlockDiff, 0)
	removed = make([]dto.InterlockDiff, 0)
	persisted = make([]dto.InterlockDiff, 0)
	baselineByKey := make(map[string]dto.InterlockFinding, len(baseline))
	for _, finding := range baseline {
		baselineByKey[interlockKey(finding)] = finding
	}
	currentByKey := make(map[string]dto.InterlockFinding, len(current))
	for _, finding := range current {
		key := interlockKey(finding)
		currentByKey[key] = finding
		if _, exists := baselineByKey[key]; exists {
			persisted = append(persisted, toInterlockDiff(finding))
		} else {
			added = append(added, toInterlockDiff(finding))
		}
	}
	for key, finding := range baselineByKey {
		if _, exists := currentByKey[key]; !exists {
			removed = append(removed, toInterlockDiff(finding))
		}
	}
	return added, removed, persisted
}

func toCollisionDiff(event dto.CollisionEvent) dto.CollisionDiff {
	return dto.CollisionDiff{
		SegmentIndex: event.SegmentIndex, ZoneID: event.ZoneID, ZoneName: event.ZoneName,
		ZoneType: event.ZoneType, Violation: event.Violation, FirstTimeMS: event.FirstTimeMS,
		ActualSpeedMMS: event.ActualSpeedMMS, AllowedSpeedMMS: event.AllowedSpeedMMS,
		ClearanceMM: event.ClearanceMM, Evidence: event.Evidence,
	}
}

func toInterlockDiff(finding dto.InterlockFinding) dto.InterlockDiff {
	return dto.InterlockDiff{
		Code: finding.Code, Event: finding.Event, DependsOn: finding.DependsOn,
		Path: finding.Path, Evidence: finding.Evidence,
	}
}
