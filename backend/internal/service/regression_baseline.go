package service

import (
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"robot-cell-safety-envelope-validator/backend/internal/dto"
	"robot-cell-safety-envelope-validator/backend/internal/model"
)

const collisionCategory = "collision"
const interlockCategory = "interlock"

// collisionKey identifies an envelope event independently of sampled contact time,
// speed, or clearance: the same motion segment touching the same zone is one finding.
func collisionKey(event dto.CollisionEvent) string {
	return fmt.Sprintf("segment:%d|zone:%d", event.SegmentIndex, event.ZoneID)
}

// interlockKey identifies a dependency finding independently of wording.
func interlockKey(finding dto.InterlockFinding) string {
	if finding.DependsOn != "" {
		return fmt.Sprintf("%s|%s|%s", finding.Code, finding.Event, finding.DependsOn)
	}
	return fmt.Sprintf("%s|%s", finding.Code, finding.Event)
}

func collisionIdentity(event dto.CollisionEvent) dto.FindingIdentity {
	segment := event.SegmentIndex
	return dto.FindingIdentity{
		Key:      collisionKey(event),
		Category: collisionCategory,
		Code:     collisionCode(event),
		ZoneID:   event.ZoneID,
		ZoneName: event.ZoneName,
		Segment:  &segment,
		Evidence: event.Evidence,
	}
}

func collisionCode(event dto.CollisionEvent) string {
	if event.Violation {
		return "envelope_violation"
	}
	return "envelope_contact"
}

func interlockIdentity(finding dto.InterlockFinding) dto.FindingIdentity {
	return dto.FindingIdentity{
		Key:      interlockKey(finding),
		Category: interlockCategory,
		Code:     finding.Code,
		Event:    finding.Event,
		Evidence: finding.Evidence,
	}
}

func diffBucket(current, baseline map[string]dto.FindingIdentity) dto.FindingDiffBucket {
	bucket := dto.FindingDiffBucket{
		New:       make([]dto.FindingIdentity, 0),
		Gone:      make([]dto.FindingIdentity, 0),
		Persisted: make([]dto.FindingIdentity, 0),
	}
	for key, identity := range current {
		if _, exists := baseline[key]; exists {
			bucket.Persisted = append(bucket.Persisted, identity)
		} else {
			bucket.New = append(bucket.New, identity)
		}
	}
	for key, identity := range baseline {
		if _, exists := current[key]; !exists {
			bucket.Gone = append(bucket.Gone, identity)
		}
	}
	sortIdentities(bucket.New)
	sortIdentities(bucket.Gone)
	sortIdentities(bucket.Persisted)
	return bucket
}

func sortIdentities(identities []dto.FindingIdentity) {
	sort.Slice(identities, func(i, j int) bool { return identities[i].Key < identities[j].Key })
}

func findingMaps(run model.ValidationRun) (map[string]dto.FindingIdentity, map[string]dto.FindingIdentity, error) {
	var collisions []dto.CollisionEvent
	if err := json.Unmarshal([]byte(run.CollisionEventsJSON), &collisions); err != nil {
		return nil, nil, fmt.Errorf("decode collision events of run %d: %w", run.ID, err)
	}
	var findings []dto.InterlockFinding
	if err := json.Unmarshal([]byte(run.InterlockFindingsJSON), &findings); err != nil {
		return nil, nil, fmt.Errorf("decode interlock findings of run %d: %w", run.ID, err)
	}
	collisionMap := make(map[string]dto.FindingIdentity, len(collisions))
	for _, event := range collisions {
		identity := collisionIdentity(event)
		collisionMap[identity.Key] = identity
	}
	interlockMap := make(map[string]dto.FindingIdentity, len(findings))
	for _, finding := range findings {
		identity := interlockIdentity(finding)
		interlockMap[identity.Key] = identity
	}
	return collisionMap, interlockMap, nil
}

// compareRuns classifies the current run findings against a baseline run.
// A nil baseline means no accepted predecessor exists; every finding is "new".
func compareRuns(current, baseline *model.ValidationRun) (dto.RegressionDiff, error) {
	diff := dto.RegressionDiff{ComparedAt: time.Now().UTC()}
	currentCollisions, currentInterlocks, err := findingMaps(*current)
	if err != nil {
		return dto.RegressionDiff{}, err
	}
	var baselineCollisions, baselineInterlocks map[string]dto.FindingIdentity
	if baseline != nil {
		diff.BaselineRunID = &baseline.ID
		baselineCollisions, baselineInterlocks, err = findingMaps(*baseline)
		if err != nil {
			return dto.RegressionDiff{}, err
		}
	}
	diff.Collision = diffBucket(currentCollisions, baselineCollisions)
	diff.Interlock = diffBucket(currentInterlocks, baselineInterlocks)
	diff.NewCollisionCount = len(diff.Collision.New)
	diff.NewInterlockCount = len(diff.Interlock.New)
	diff.HasNewFindings = diff.NewCollisionCount > 0 || diff.NewInterlockCount > 0
	return diff, nil
}

func encodeRegressionDiff(diff dto.RegressionDiff) string {
	encoded, err := json.Marshal(diff)
	if err != nil {
		return "{}"
	}
	return string(encoded)
}

// decodeRegressionDiff tolerates legacy rows that predate regression baselines.
func decodeRegressionDiff(raw string) dto.RegressionDiff {
	diff := dto.RegressionDiff{}
	if raw == "" {
		return diff
	}
	_ = json.Unmarshal([]byte(raw), &diff)
	return diff
}
