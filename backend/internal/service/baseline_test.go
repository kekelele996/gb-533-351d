package service

import (
	"encoding/json"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"robot-cell-safety-envelope-validator/backend/internal/constants"
	"robot-cell-safety-envelope-validator/backend/internal/dto"
	"robot-cell-safety-envelope-validator/backend/internal/model"
	"robot-cell-safety-envelope-validator/backend/internal/repository"
)

var baselineDBCounter int64

type baselineFixture struct {
	db       *gorm.DB
	service  *ValidationRunService
	uploader dto.Actor
	reviewer dto.Actor
	cellID   uint
}

func newBaselineFixture(t *testing.T) baselineFixture {
	t.Helper()
	dsn := fmt.Sprintf("file:baseline%d?mode=memory&cache=shared", atomic.AddInt64(&baselineDBCounter, 1))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&model.User{}, &model.RobotCell{}, &model.SafetyZone{}, &model.MotionProgram{}, &model.ValidationRun{}, &model.AuditEvent{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	// Each test gets an isolated in-memory schema.
	t.Cleanup(func() {
		sqlDB, _ := db.DB()
		_ = sqlDB.Close()
	})

	uploaderUser := model.User{Username: "uploader", PasswordHash: "x", Role: constants.RoleRobotProgrammer, Active: true}
	reviewerUser := model.User{Username: "reviewer", PasswordHash: "x", Role: constants.RoleReviewer, Active: true}
	if err := db.Create(&uploaderUser).Error; err != nil {
		t.Fatalf("create uploader: %v", err)
	}
	if err := db.Create(&reviewerUser).Error; err != nil {
		t.Fatalf("create reviewer: %v", err)
	}
	cell := model.RobotCell{CellCode: "CELL-BL", Name: "Baseline cell", LayoutGeoJSON: "{}", RobotModel: "R", ControllerModel: "C", MaxReachMM: 2000, OwnerTeam: "QA", CellState: constants.CellStateFrozen, LayoutVersion: 1, CreatedBy: uploaderUser.ID}
	if err := db.Create(&cell).Error; err != nil {
		t.Fatalf("create cell: %v", err)
	}
	zones := []model.SafetyZone{
		{RobotCellID: cell.ID, Name: "Operating", ZoneType: "operating", PolygonGeoJSON: polygon(-1000, -1000, 1000, 1000), MinHeightMM: 0, MaxHeightMM: 3000, SpeedLimitMMS: 2000, AccessRule: "a", ZoneState: constants.ZoneStateActive, Version: 1, CreatedBy: uploaderUser.ID},
		{RobotCellID: cell.ID, Name: "Restricted gate", ZoneType: "restricted", PolygonGeoJSON: polygon(1000, -400, 2000, 400), MinHeightMM: 0, MaxHeightMM: 3000, SpeedLimitMMS: 500, AccessRule: "b", ZoneState: constants.ZoneStateActive, Version: 1, CreatedBy: uploaderUser.ID},
		{RobotCellID: cell.ID, Name: "Service aisle", ZoneType: "service", PolygonGeoJSON: polygon(-2000, 1000, 2000, 2000), MinHeightMM: 0, MaxHeightMM: 3000, SpeedLimitMMS: 0, AccessRule: "c", ZoneState: constants.ZoneStateActive, Version: 1, CreatedBy: uploaderUser.ID},
		{RobotCellID: cell.ID, Name: "Far restricted", ZoneType: "restricted", PolygonGeoJSON: polygon(3000, 3000, 4000, 4000), MinHeightMM: 0, MaxHeightMM: 3000, SpeedLimitMMS: 200, AccessRule: "d", ZoneState: constants.ZoneStateActive, Version: 1, CreatedBy: uploaderUser.ID},
	}
	if err := db.Create(&zones).Error; err != nil {
		t.Fatalf("create zones: %v", err)
	}
	system := NewSystemService(repository.NewSystemRepository(db), "test-secret-at-least-24-bytes-long", time.Hour)
	service := NewValidationRunService(db, repository.NewValidationRunRepository(db), repository.NewMotionProgramRepository(db), repository.NewSafetyZoneRepository(db), system, "envelope-2d-height-v1.0")
	return baselineFixture{db: db, service: service,
		uploader: dto.Actor{ID: uploaderUser.ID, Username: "uploader", Role: uploaderUser.Role},
		reviewer: dto.Actor{ID: reviewerUser.ID, Username: "reviewer", Role: reviewerUser.Role},
		cellID:   cell.ID}
}

func polygon(x1, y1, x2, y2 int) string {
	coordinates := [][][2]int{{{x1, y1}, {x2, y1}, {x2, y2}, {x1, y2}, {x1, y1}}}
	raw, _ := json.Marshal(struct {
		Type        string     `json:"type"`
		Coordinates [][][2]int `json:"coordinates"`
	}{Type: "Polygon", Coordinates: coordinates})
	return string(raw)
}

// baselineTrajectories stay out of every zone except the one intentionally
// targeted, so each produces at most one envelope event:
//   - clean: moves through empty floor space, zero findings.
//   - gate: ends inside the restricted gate (new collision when swapped from clean).
//   - aisle: ends inside the service aisle instead.
var (
	trajectoryClean = []dto.TrajectoryPoint{{XMM: -2000, YMM: 700, ZMM: 800, TimeMS: 0, SpeedMMS: 400}, {XMM: -1500, YMM: 700, ZMM: 800, TimeMS: 1000, SpeedMMS: 400}}
	trajectoryGate  = []dto.TrajectoryPoint{{XMM: 1200, YMM: 0, ZMM: 800, TimeMS: 0, SpeedMMS: 700}, {XMM: 1800, YMM: 0, ZMM: 800, TimeMS: 2000, SpeedMMS: 700}}
	trajectoryAisle = []dto.TrajectoryPoint{{XMM: -1200, YMM: 1200, ZMM: 800, TimeMS: 0, SpeedMMS: 400}, {XMM: -200, YMM: 1500, ZMM: 800, TimeMS: 2000, SpeedMMS: 400}}
)

// simpleInterlocks prefixes the mandatory reset event; extras must already
// carry deterministic sequence numbers starting at 2.
func simpleInterlocks(extras ...dto.InterlockEvent) []dto.InterlockEvent {
	events := []dto.InterlockEvent{{Name: "estop_reset", Sequence: 1, DependsOn: []string{}}}
	return append(events, extras...)
}

func (fixture baselineFixture) createProgram(t *testing.T, code string, version int, trajectory []dto.TrajectoryPoint, events []dto.InterlockEvent) model.MotionProgram {
	t.Helper()
	trajectoryJSON, _ := json.Marshal(trajectory)
	interlockJSON, _ := json.Marshal(events)
	program := model.MotionProgram{
		RobotCellID: fixture.cellID, ProgramCode: code, Version: version, TrajectoryJSON: string(trajectoryJSON),
		ToolRadiusMM: 50, PayloadRadiusMM: 30, InterlockSequenceJSON: string(interlockJSON),
		SourceChecksum: "checksum-" + code, ProgramState: constants.ProgramStateReady,
		UploadedBy: fixture.uploader.ID, UploadedAt: time.Now().UTC(),
	}
	if err := fixture.db.Create(&program).Error; err != nil {
		t.Fatalf("create program: %v", err)
	}
	return program
}

func (fixture baselineFixture) runSimulation(t *testing.T, programID uint, key string) dto.ValidationRunResponse {
	t.Helper()
	response, reused, err := fixture.service.Create(dto.CreateValidationRunRequest{MotionProgramID: programID}, key, fixture.uploader, "request-"+key)
	if err != nil {
		t.Fatalf("create validation run: %v", err)
	}
	if reused {
		t.Fatalf("expected a fresh run for key %s", key)
	}
	return response
}

func (fixture baselineFixture) reviewAndAccept(t *testing.T, id uint) dto.ValidationRunResponse {
	t.Helper()
	if _, err := fixture.service.Review(id, "Independent offline evidence review completed.", fixture.reviewer, "request-review"); err != nil {
		t.Fatalf("review run %d: %v", id, err)
	}
	response, err := fixture.service.Accept(id, "Evidence accepted for planning; site authority remains separate.", fixture.reviewer, "request-accept")
	if err != nil {
		t.Fatalf("accept run %d: %v", id, err)
	}
	return response
}

func (fixture baselineFixture) setAcceptedAt(t *testing.T, id uint, acceptedAt time.Time) {
	t.Helper()
	if err := fixture.db.Model(&model.ValidationRun{}).Where("id = ?", id).Update("reviewed_at", acceptedAt).Error; err != nil {
		t.Fatalf("override reviewed_at: %v", err)
	}
}

// forceAccept models an accepted historical run established outside the
// current regression gate (for example before the feature existed).
func (fixture baselineFixture) forceAccept(t *testing.T, id uint, acceptedAt time.Time) {
	t.Helper()
	if err := fixture.db.Model(&model.ValidationRun{}).Where("id = ?", id).Updates(map[string]any{
		"validation_status": constants.ValidationAccepted, "reviewed_by": fixture.reviewer.ID,
		"reviewed_at": acceptedAt, "review_note": "Historical accepted baseline.",
	}).Error; err != nil {
		t.Fatalf("force accept run %d: %v", id, err)
	}
}

func TestFirstRunHasNoBaseline(t *testing.T) {
	fixture := newBaselineFixture(t)
	program := fixture.createProgram(t, "MOVE-BL-1", 1, trajectoryClean, simpleInterlocks())
	run := fixture.runSimulation(t, program.ID, "key-no-baseline")
	if run.BaselineRunID != nil || run.Regression != nil {
		t.Fatalf("expected first run without baseline, got id=%v regression=%+v", run.BaselineRunID, run.Regression)
	}
}

func TestNewRunBindsLatestAcceptedBaselineAndClassifiesFindings(t *testing.T) {
	fixture := newBaselineFixture(t)

	// Run 1: clean program is accepted and becomes the baseline.
	program1 := fixture.createProgram(t, "MOVE-BL", 1, trajectoryClean, simpleInterlocks())
	baseline := fixture.runSimulation(t, program1.ID, "key-baseline")
	if baseline.ValidationStatus != constants.ValidationPassed {
		t.Fatalf("expected passed clean run, got %s", baseline.ValidationStatus)
	}
	fixture.reviewAndAccept(t, baseline.ID)
	fixture.setAcceptedAt(t, baseline.ID, time.Now().UTC().Add(-2*time.Hour))

	// Run 2: same program code (new version) drives into the restricted gate.
	program2 := fixture.createProgram(t, "MOVE-BL", 2, trajectoryGate, simpleInterlocks())
	regressed := fixture.runSimulation(t, program2.ID, "key-regression")
	if regressed.BaselineRunID == nil || *regressed.BaselineRunID != baseline.ID {
		t.Fatalf("expected baseline %d binding, got %v", baseline.ID, regressed.BaselineRunID)
	}
	if regressed.Regression == nil {
		t.Fatal("expected regression block")
	}
	if !regressed.Regression.HasNewFindings {
		t.Fatal("expected new findings flag")
	}
	if len(regressed.Regression.CollisionDiff.Added) != 1 {
		t.Fatalf("expected 1 added collision, got %d", len(regressed.Regression.CollisionDiff.Added))
	}
	if added := regressed.Regression.CollisionDiff.Added[0]; added.ZoneName != "Restricted gate" || !added.Violation {
		t.Fatalf("unexpected added collision: %+v", added)
	}
	if len(regressed.Regression.CollisionDiff.Removed) != 0 || len(regressed.Regression.CollisionDiff.Persisted) != 0 {
		t.Fatalf("expected no removed or persisted collisions, got %+v", regressed.Regression.CollisionDiff)
	}

	// Acceptance must be blocked despite the human review.
	if _, err := fixture.service.Review(regressed.ID, "Independent offline evidence review completed.", fixture.reviewer, "r"); err != nil {
		t.Fatalf("review: %v", err)
	}
	_, err := fixture.service.Accept(regressed.ID, "Reviewer would still accept despite regression.", fixture.reviewer, "a")
	if err == nil {
		t.Fatal("expected acceptance to be blocked by new findings")
	}
	appError, ok := err.(*AppError)
	if !ok || appError.Code != "new_regression_findings" {
		t.Fatalf("expected new_regression_findings conflict, got %v", err)
	}

	// The gate run later becomes an accepted historical baseline (its finding is
	// then part of the accepted evidence set).
	fixture.forceAccept(t, regressed.ID, time.Now().UTC().Add(-time.Hour))

	// Run 3: moving from the gate to the aisle shows one removed, one added.
	program3 := fixture.createProgram(t, "MOVE-BL", 3, trajectoryAisle, simpleInterlocks())
	swapped := fixture.runSimulation(t, program3.ID, "key-swap")
	if swapped.BaselineRunID == nil || *swapped.BaselineRunID != regressed.ID {
		t.Fatalf("new run must bind latest accepted baseline #%d, got %v", regressed.ID, swapped.BaselineRunID)
	}
	if len(swapped.Regression.CollisionDiff.Added) != 1 || swapped.Regression.CollisionDiff.Added[0].ZoneName != "Service aisle" {
		t.Fatalf("expected added service-aisle collision, got %+v", swapped.Regression.CollisionDiff.Added)
	}
	if len(swapped.Regression.CollisionDiff.Removed) != 1 || swapped.Regression.CollisionDiff.Removed[0].ZoneName != "Restricted gate" {
		t.Fatalf("expected removed gate collision, got %+v", swapped.Regression.CollisionDiff.Removed)
	}
}

func TestPersistedAndRemovedInterlockFindings(t *testing.T) {
	fixture := newBaselineFixture(t)

	// Baseline: clean trajectory plus a missing prerequisite finding.
	baselineEvents := simpleInterlocks(dto.InterlockEvent{Name: "gate_locked", Sequence: 2, DependsOn: []string{"missing_event"}})
	program1 := fixture.createProgram(t, "MOVE-IL", 1, trajectoryClean, baselineEvents)
	baseline := fixture.runSimulation(t, program1.ID, "key-il-baseline")
	if baseline.ValidationStatus != constants.ValidationFailed || len(baseline.InterlockFindings) != 1 {
		t.Fatalf("expected one interlock finding, got status=%s findings=%d", baseline.ValidationStatus, len(baseline.InterlockFindings))
	}
	fixture.reviewAndAccept(t, baseline.ID)
	fixture.setAcceptedAt(t, baseline.ID, time.Now().UTC().Add(-2*time.Hour))

	// New version: same missing prerequisite persists; a reversed order appears.
	nextEvents := simpleInterlocks(
		dto.InterlockEvent{Name: "gate_locked", Sequence: 2, DependsOn: []string{"missing_event"}},
		dto.InterlockEvent{Name: "curtain_clear", Sequence: 4, DependsOn: []string{"gate_sensor"}},
		dto.InterlockEvent{Name: "gate_sensor", Sequence: 5, DependsOn: []string{}},
	)
	program2 := fixture.createProgram(t, "MOVE-IL", 2, trajectoryClean, nextEvents)
	next := fixture.runSimulation(t, program2.ID, "key-il-next")
	if !next.Regression.HasNewFindings {
		t.Fatal("expected new interlock finding to be flagged")
	}
	if len(next.Regression.InterlockDiff.Persisted) != 1 || next.Regression.InterlockDiff.Persisted[0].Code != "missing_prerequisite" {
		t.Fatalf("expected persisted missing_prerequisite, got %+v", next.Regression.InterlockDiff.Persisted)
	}
	if len(next.Regression.InterlockDiff.Added) != 1 || next.Regression.InterlockDiff.Added[0].Code != "reversed_order" {
		t.Fatalf("expected added reversed_order, got %+v", next.Regression.InterlockDiff.Added)
	}

	// The run carrying the reversed order later becomes an accepted historical
	// baseline; a subsequent version that drops that dependency compares against
	// it, so the finding moves from added to removed while the original missing
	// prerequisite persists.
	fixture.forceAccept(t, next.ID, time.Now().UTC().Add(-time.Hour))

	program3 := fixture.createProgram(t, "MOVE-IL", 3, trajectoryClean, baselineEvents)
	resolved := fixture.runSimulation(t, program3.ID, "key-il-resolved")
	if resolved.BaselineRunID == nil || *resolved.BaselineRunID != next.ID {
		t.Fatalf("expected binding to latest accepted baseline #%d, got %v", next.ID, resolved.BaselineRunID)
	}
	if len(resolved.Regression.InterlockDiff.Added) != 0 {
		t.Fatalf("expected no added findings, got %+v", resolved.Regression.InterlockDiff.Added)
	}
	if len(resolved.Regression.InterlockDiff.Removed) != 1 || resolved.Regression.InterlockDiff.Removed[0].Code != "reversed_order" {
		t.Fatalf("expected removed reversed_order, got %+v", resolved.Regression.InterlockDiff.Removed)
	}
	if len(resolved.Regression.InterlockDiff.Persisted) != 1 {
		t.Fatalf("expected one persisted finding, got %d", len(resolved.Regression.InterlockDiff.Persisted))
	}
	accepted := fixture.reviewAndAccept(t, resolved.ID)
	if accepted.ValidationStatus != constants.ValidationAccepted {
		t.Fatalf("expected accepted status, got %s", accepted.ValidationStatus)
	}
}

func TestVoidedBaselineFallsBackToPreviousAcceptedRunAndThenUnbinds(t *testing.T) {
	fixture := newBaselineFixture(t)

	// Baseline #1: clean and accepted.
	program1 := fixture.createProgram(t, "MOVE-FB", 1, trajectoryClean, simpleInterlocks())
	run1 := fixture.runSimulation(t, program1.ID, "key-fb-1")
	fixture.reviewAndAccept(t, run1.ID)
	fixture.setAcceptedAt(t, run1.ID, time.Now().UTC().Add(-3*time.Hour))

	// Baseline #2: gate violation. It regresses against #1 and therefore cannot
	// pass the acceptance gate today; model it as an accepted historical run
	// that pre-dates the current regression gate.
	program2 := fixture.createProgram(t, "MOVE-FB", 2, trajectoryGate, simpleInterlocks())
	run2 := fixture.runSimulation(t, program2.ID, "key-fb-2")
	if run2.Regression == nil || *run2.BaselineRunID != run1.ID || !run2.Regression.HasNewFindings {
		t.Fatalf("run 2 should regress against baseline 1: %+v", run2.Regression)
	}
	fixture.forceAccept(t, run2.ID, time.Now().UTC().Add(-time.Hour))

	// Run 3 binds latest accepted baseline (#2) and regresses with the aisle.
	program3 := fixture.createProgram(t, "MOVE-FB", 3, trajectoryAisle, simpleInterlocks())
	run3 := fixture.runSimulation(t, program3.ID, "key-fb-3")
	if run3.BaselineRunID == nil || *run3.BaselineRunID != run2.ID {
		t.Fatalf("expected binding to latest baseline #%d, got %v", run2.ID, run3.BaselineRunID)
	}
	if len(run3.Regression.CollisionDiff.Added) != 1 || run3.Regression.CollisionDiff.Added[0].ZoneName != "Service aisle" {
		t.Fatalf("expected aisle addition versus gate baseline, got %+v", run3.Regression.CollisionDiff.Added)
	}
	if len(run3.Regression.CollisionDiff.Removed) != 1 {
		t.Fatalf("expected gate removal, got %+v", run3.Regression.CollisionDiff.Removed)
	}

	// Voiding baseline #2 rebinds run 3 to the previous accepted run #1.
	if _, err := fixture.service.Void(run2.ID, "Baseline withdrawn after geometry correction.", fixture.reviewer, "request-void-2"); err != nil {
		t.Fatalf("void baseline 2: %v", err)
	}
	rebound, err := fixture.service.Get(run3.ID)
	if err != nil {
		t.Fatalf("reload run 3: %v", err)
	}
	if rebound.BaselineRunID == nil || *rebound.BaselineRunID != run1.ID {
		t.Fatalf("expected fallback to baseline #%d, got %v", run1.ID, rebound.BaselineRunID)
	}
	if len(rebound.Regression.CollisionDiff.Added) != 1 || rebound.Regression.CollisionDiff.Added[0].ZoneName != "Service aisle" {
		t.Fatalf("expected aisle addition versus clean fallback, got %+v", rebound.Regression.CollisionDiff.Added)
	}

	// Voiding #1 as well leaves run 3 without any binding.
	if _, err := fixture.service.Void(run1.ID, "Oldest baseline withdrawn too.", fixture.reviewer, "request-void-1"); err != nil {
		t.Fatalf("void baseline 1: %v", err)
	}
	unbound, err := fixture.service.Get(run3.ID)
	if err != nil {
		t.Fatalf("reload run 3: %v", err)
	}
	if unbound.BaselineRunID != nil || unbound.Regression != nil {
		t.Fatalf("expected unbound run, got id=%v regression=%+v", unbound.BaselineRunID, unbound.Regression)
	}

	// Baseline lookup is scoped by cell and program code: a different code never binds.
	other := fixture.createProgram(t, "MOVE-OTHER", 1, trajectoryGate, simpleInterlocks())
	otherRun := fixture.runSimulation(t, other.ID, "key-fb-other")
	if otherRun.BaselineRunID != nil {
		t.Fatalf("different program code must not bind baselines, got %v", otherRun.BaselineRunID)
	}
}

func TestVoidedBaselineWithTiedAcceptanceTimestampsFallsBackByID(t *testing.T) {
	fixture := newBaselineFixture(t)

	program1 := fixture.createProgram(t, "MOVE-TIE", 1, trajectoryClean, simpleInterlocks())
	run1 := fixture.runSimulation(t, program1.ID, "key-tie-1")
	fixture.forceAccept(t, run1.ID, time.Now().UTC().Add(-time.Hour))

	program2 := fixture.createProgram(t, "MOVE-TIE", 2, trajectoryClean, simpleInterlocks())
	run2 := fixture.runSimulation(t, program2.ID, "key-tie-2")
	// Identical reviewed_at: the higher ID is the latest baseline.
	fixture.forceAccept(t, run2.ID, mustAcceptedAt(t, fixture, run1.ID))

	program3 := fixture.createProgram(t, "MOVE-TIE", 3, trajectoryGate, simpleInterlocks())
	run3 := fixture.runSimulation(t, program3.ID, "key-tie-3")
	if run3.BaselineRunID == nil || *run3.BaselineRunID != run2.ID {
		t.Fatalf("expected latest baseline #%d by id tie-break, got %v", run2.ID, run3.BaselineRunID)
	}

	if _, err := fixture.service.Void(run2.ID, "Withdraw the latest baseline.", fixture.reviewer, "void-tie"); err != nil {
		t.Fatalf("void: %v", err)
	}
	rebound, err := fixture.service.Get(run3.ID)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if rebound.BaselineRunID == nil || *rebound.BaselineRunID != run1.ID {
		t.Fatalf("expected fallback to #%d despite equal timestamps, got %v", run1.ID, rebound.BaselineRunID)
	}
}

func mustAcceptedAt(t *testing.T, fixture baselineFixture, id uint) time.Time {
	t.Helper()
	run, err := fixture.service.repository.Get(id)
	if err != nil {
		t.Fatalf("load run %d: %v", id, err)
	}
	if run.ReviewedAt == nil {
		t.Fatalf("run %d has no reviewed_at", id)
	}
	return *run.ReviewedAt
}

func TestBaselineScopedByAlgorithmVersion(t *testing.T) {
	fixture := newBaselineFixture(t)
	program1 := fixture.createProgram(t, "MOVE-ALG", 1, trajectoryGate, simpleInterlocks())
	baseline := fixture.runSimulation(t, program1.ID, "key-alg-1")
	fixture.reviewAndAccept(t, baseline.ID)

	otherVersion := NewValidationRunService(fixture.db, repository.NewValidationRunRepository(fixture.db),
		repository.NewMotionProgramRepository(fixture.db), repository.NewSafetyZoneRepository(fixture.db),
		NewSystemService(repository.NewSystemRepository(fixture.db), "test-secret-at-least-24-bytes-long", time.Hour),
		"envelope-2d-height-v2.0")
	program2 := fixture.createProgram(t, "MOVE-ALG", 2, trajectoryGate, simpleInterlocks())
	response, _, err := otherVersion.Create(dto.CreateValidationRunRequest{MotionProgramID: program2.ID}, "key-alg-2", fixture.uploader, "request-alg-2")
	if err != nil {
		t.Fatalf("create run with other algorithm: %v", err)
	}
	if response.BaselineRunID != nil {
		t.Fatalf("different algorithm version must not bind the v1 baseline, got %v", response.BaselineRunID)
	}
}
