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

const testAlgorithmVersion = "envelope-2d-height-v1.0"

var testDBCounter int64

func newTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := fmt.Sprintf("file:regression-%d?mode=memory&cache=shared", atomic.AddInt64(&testDBCounter, 1))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&model.User{}, &model.RobotCell{}, &model.SafetyZone{}, &model.MotionProgram{}, &model.ValidationRun{}, &model.AuditEvent{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	t.Cleanup(func() {
		if sqlDB, err := db.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})
	return db
}

func testCell(code string) model.RobotCell {
	return model.RobotCell{CellCode: code, Name: code + " cell", LayoutGeoJSON: "{}",
		RobotModel: "QA-Robot", ControllerModel: "QA-Control", MaxReachMM: 2400,
		OwnerTeam: "QA", CellState: constants.CellStateFrozen, CreatedBy: 1}
}

func newValidationService(db *gorm.DB) *ValidationRunService {
	return NewValidationRunService(
		db,
		repository.NewValidationRunRepository(db),
		repository.NewMotionProgramRepository(db),
		repository.NewSafetyZoneRepository(db),
		NewSystemService(repository.NewSystemRepository(db), "test-secret-at-least-24-bytes-long", time.Hour),
		testAlgorithmVersion,
	)
}

// insertAcceptedRun stores an accepted validation run with the given findings.
func insertAcceptedRun(t *testing.T, db *gorm.DB, id *uint, program model.MotionProgram, reviewer uint, collisions []dto.CollisionEvent, findings []dto.InterlockFinding, acceptedAt time.Time) model.ValidationRun {
	t.Helper()
	collisionJSON, _ := json.Marshal(collisions)
	findingJSON, _ := json.Marshal(findings)
	finished := acceptedAt.Add(-time.Hour)
	run := model.ValidationRun{
		MotionProgramID: program.ID, ZoneSnapshot: "[]", ProgramSnapshot: "{}",
		AlgorithmVersion: testAlgorithmVersion, InputHash: "hash-of-" + program.ProgramCode,
		IdempotencyKey:      "seeded-" + program.ProgramCode + "-" + timeString(acceptedAt),
		CollisionEventsJSON: string(collisionJSON), InterlockFindingsJSON: string(findingJSON),
		ValidationStatus: constants.ValidationAccepted, Explanation: "seeded accepted baseline",
		RegressionDiffJSON: "{}", RequestedBy: reviewer, StartedAt: finished.Add(-time.Minute), FinishedAt: &finished,
		ReviewedBy: &reviewer, ReviewedAt: &acceptedAt, ReviewNote: "seeded acceptance",
	}
	if err := db.Create(&run).Error; err != nil {
		t.Fatalf("insert accepted run: %v", err)
	}
	if id != nil {
		*id = run.ID
	}
	return run
}

func timeString(value time.Time) string {
	return value.Format("150405.000000")
}

func persistRun(t *testing.T, db *gorm.DB, run *model.ValidationRun) {
	t.Helper()
	if err := db.Create(run).Error; err != nil {
		t.Fatalf("persist run: %v", err)
	}
}

// TestLatestAcceptedBaselineScoping verifies work unit, program code, algorithm
// version, acceptance-time and exclusion filtering used for automatic binding.
func TestLatestAcceptedBaselineScoping(t *testing.T) {
	db := newTestDB(t)
	repo := repository.NewValidationRunRepository(db)
	now := time.Now().UTC()
	cellA := testCell("CELL-A")
	cellB := testCell("CELL-B")
	if err := db.Create(&cellA).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&cellB).Error; err != nil {
		t.Fatal(err)
	}
	progA := model.MotionProgram{RobotCellID: cellA.ID, ProgramCode: "WELD-01", Version: 1, TrajectoryJSON: "[]", InterlockSequenceJSON: "[]", ProgramState: constants.ProgramStateActive, SourceChecksum: "a", UploadedAt: now}
	progB := model.MotionProgram{RobotCellID: cellB.ID, ProgramCode: "WELD-02", Version: 1, TrajectoryJSON: "[]", InterlockSequenceJSON: "[]", ProgramState: constants.ProgramStateActive, SourceChecksum: "b", UploadedAt: now}
	if err := db.Create(&progA).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&progB).Error; err != nil {
		t.Fatal(err)
	}
	reviewer := uint(1)
	insertAcceptedRun(t, db, nil, progA, reviewer, nil, nil, now.Add(-2*time.Hour))
	insertAcceptedRun(t, db, nil, progA, reviewer, nil, nil, now.Add(-30*time.Minute))
	insertAcceptedRun(t, db, nil, progB, reviewer, nil, nil, now.Add(-10*time.Minute))

	// Same work unit and program code: most recent accepted run wins.
	baseline, err := repo.LatestAcceptedBaseline(cellA.ID, "WELD-01", testAlgorithmVersion, now, 0)
	if err != nil {
		t.Fatalf("expected baseline: %v", err)
	}
	if baseline.MotionProgramID != progA.ID {
		t.Fatalf("baseline motion program = %d, want %d", baseline.MotionProgramID, progA.ID)
	}
	if baseline.ReviewedAt == nil || now.Sub(*baseline.ReviewedAt) > time.Hour {
		t.Fatalf("expected the 30-minute-old acceptance, got %v", baseline.ReviewedAt)
	}

	// A run accepted after completion time is not yet a valid baseline: at a
	// completion time three hours ago neither seeded acceptance existed yet.
	if _, err := repo.LatestAcceptedBaseline(cellA.ID, "WELD-01", testAlgorithmVersion, now.Add(-3*time.Hour), 0); err == nil {
		t.Fatalf("future acceptance must not be selectable as baseline")
	}

	// The same program code in another work cell would have its own baselines; a
	// cell without any accepted run must not borrow another cell's baseline.
	isolatedCell := model.RobotCell{CellCode: "CELL-ISOLATED", Name: "Isolated", LayoutGeoJSON: "{}",
		RobotModel: "QA-Robot", ControllerModel: "QA-Control", MaxReachMM: 2400, OwnerTeam: "QA",
		CellState: constants.CellStateFrozen, CreatedBy: 1}
	if err := db.Create(&isolatedCell).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := repo.LatestAcceptedBaseline(isolatedCell.ID, "WELD-01", testAlgorithmVersion, now, 0); err == nil {
		t.Fatalf("different work cell must not share a baseline")
	}

	// Algorithm version mismatch isolates baselines.
	if _, err := repo.LatestAcceptedBaseline(cellA.ID, "WELD-01", "envelope-2d-height-v9.9", now, 0); err == nil {
		t.Fatalf("different algorithm version must not share a baseline")
	}
}

// TestVoidRebindsDependentsToPreviousAcceptance verifies the fallback chain:
// run C bound to accepted B; when B is voided, C rebinds to accepted A, and when
// A is also voided, C ends up without a baseline with every finding reclassified
// as new.
func TestVoidRebindsDependentsToPreviousAcceptance(t *testing.T) {
	db := newTestDB(t)
	service := newValidationService(db)
	now := time.Now().UTC()
	engineer := dto.Actor{ID: 7, Username: "engineer", Role: constants.RoleSafetyEngineer}
	reviewer := dto.Actor{ID: 9, Username: "reviewer", Role: constants.RoleReviewer}

	cell := testCell("CELL-REBASE")
	if err := db.Create(&cell).Error; err != nil {
		t.Fatal(err)
	}
	program := model.MotionProgram{RobotCellID: cell.ID, ProgramCode: "WELD-REBASE", Version: 1, TrajectoryJSON: "[]", InterlockSequenceJSON: "[]", ProgramState: constants.ProgramStateActive, SourceChecksum: "rebase", UploadedAt: now}
	if err := db.Create(&program).Error; err != nil {
		t.Fatal(err)
	}

	gateCollision := []dto.CollisionEvent{collisionEvent(0, 10, "gate", true)}
	curtainCollision := []dto.CollisionEvent{collisionEvent(0, 10, "gate", true), collisionEvent(2, 12, "curtain", true)}

	runA := insertAcceptedRun(t, db, nil, program, reviewer.ID, gateCollision, nil, now.Add(-3*time.Hour))
	runB := insertAcceptedRun(t, db, nil, program, reviewer.ID, curtainCollision, nil, now.Add(-2*time.Hour))

	// Run C is completed later and bound to B at completion.
	finishedC := now.Add(-time.Hour)
	diffB, err := compareRuns(&model.ValidationRun{CollisionEventsJSON: mustJSON(t, curtainCollision), InterlockFindingsJSON: "[]"}, &runB)
	if err != nil {
		t.Fatal(err)
	}
	runC := model.ValidationRun{
		MotionProgramID: program.ID, ZoneSnapshot: "[]", ProgramSnapshot: "{}",
		AlgorithmVersion: testAlgorithmVersion, InputHash: "hash-c", IdempotencyKey: "run-c",
		CollisionEventsJSON: mustJSON(t, curtainCollision), InterlockFindingsJSON: "[]",
		ValidationStatus: constants.ValidationFailed, Explanation: "run c",
		BaselineRunID: &runB.ID, RegressionDiffJSON: encodeRegressionDiff(diffB),
		RequestedBy: engineer.ID, StartedAt: finishedC.Add(-time.Minute), FinishedAt: &finishedC, ReviewNote: "",
	}
	persistRun(t, db, &runC)
	if decodeRegressionDiff(runC.RegressionDiffJSON).HasNewFindings {
		t.Fatalf("run C must match baseline B with no new findings")
	}

	// Voiding B must fall C back to A, reclassifying the curtain collision as new.
	if _, err := service.Void(runB.ID, "void newest baseline for regression fallback test", reviewer, "request-b-void"); err != nil {
		t.Fatalf("void run B: %v", err)
	}
	var reloadedC model.ValidationRun
	if err := db.Preload("MotionProgram").First(&reloadedC, runC.ID).Error; err != nil {
		t.Fatal(err)
	}
	if reloadedC.BaselineRunID == nil || *reloadedC.BaselineRunID != runA.ID {
		t.Fatalf("run C baseline = %v, want run A %d", reloadedC.BaselineRunID, runA.ID)
	}
	diffAfter := decodeRegressionDiff(reloadedC.RegressionDiffJSON)
	if !diffAfter.HasNewFindings || diffAfter.NewCollisionCount != 1 {
		t.Fatalf("after fallback to A, curtain collision must be new: %+v", diffAfter)
	}
	if diffAfter.Collision.Gone != nil && len(diffAfter.Collision.Gone) != 0 {
		t.Fatalf("A has no findings absent from C, gone = %+v", diffAfter.Collision.Gone)
	}

	// Voiding A removes the baseline entirely; both collisions become new.
	if _, err := service.Void(runA.ID, "void previous baseline for regression fallback test", reviewer, "request-a-void"); err != nil {
		t.Fatalf("void run A: %v", err)
	}
	if err := db.First(&reloadedC, runC.ID).Error; err != nil {
		t.Fatal(err)
	}
	if reloadedC.BaselineRunID != nil {
		t.Fatalf("run C baseline = %v, want nil after all baselines voided", *reloadedC.BaselineRunID)
	}
	diffFinal := decodeRegressionDiff(reloadedC.RegressionDiffJSON)
	if !diffFinal.HasNewFindings || diffFinal.NewCollisionCount != 2 {
		t.Fatalf("without baseline every finding must be new: %+v", diffFinal)
	}
}

// TestAcceptBlockedByNewFindings ensures review approval cannot override the
// regression gate, while a run with no new findings accepts normally.
func TestAcceptBlockedByNewFindings(t *testing.T) {
	db := newTestDB(t)
	service := newValidationService(db)
	now := time.Now().UTC()
	reviewer := dto.Actor{ID: 9, Username: "reviewer", Role: constants.RoleReviewer}
	cell := testCell("CELL-GATE")
	if err := db.Create(&cell).Error; err != nil {
		t.Fatal(err)
	}
	program := model.MotionProgram{RobotCellID: cell.ID, ProgramCode: "WELD-GATE", Version: 1, TrajectoryJSON: "[]", InterlockSequenceJSON: "[]", ProgramState: constants.ProgramStateActive, SourceChecksum: "gate", UploadedBy: 5, UploadedAt: now}
	if err := db.Create(&program).Error; err != nil {
		t.Fatal(err)
	}
	baseline := insertAcceptedRun(t, db, nil, program, 5, nil, nil, now.Add(-2*time.Hour))
	blockedDiff, err := compareRuns(
		&model.ValidationRun{CollisionEventsJSON: mustJSON(t, []dto.CollisionEvent{collisionEvent(0, 10, "gate", true)}), InterlockFindingsJSON: "[]"},
		&baseline,
	)
	if err != nil {
		t.Fatal(err)
	}
	blocked := model.ValidationRun{
		MotionProgramID: program.ID, ZoneSnapshot: "[]", ProgramSnapshot: "{}",
		AlgorithmVersion: testAlgorithmVersion, InputHash: "blocked", IdempotencyKey: "blocked",
		CollisionEventsJSON: mustJSON(t, []dto.CollisionEvent{collisionEvent(0, 10, "gate", true)}), InterlockFindingsJSON: "[]",
		ValidationStatus: constants.ValidationReviewed, Explanation: "new violation",
		BaselineRunID: &baseline.ID, RegressionDiffJSON: encodeRegressionDiff(blockedDiff),
		RequestedBy: 7, StartedAt: now.Add(-time.Minute), ReviewNote: "reviewed",
	}
	persistRun(t, db, &blocked)
	if _, err := service.Accept(blocked.ID, "attempt to accept over new findings", reviewer, "request-accept-blocked"); err == nil {
		t.Fatalf("acceptance with new findings must fail")
	}
	var still model.ValidationRun
	if err := db.First(&still, blocked.ID).Error; err != nil {
		t.Fatal(err)
	}
	if still.ValidationStatus != constants.ValidationReviewed {
		t.Fatalf("status = %s, want reviewed", still.ValidationStatus)
	}

	cleanDiff, err := compareRuns(&model.ValidationRun{CollisionEventsJSON: "[]", InterlockFindingsJSON: "[]"}, &baseline)
	if err != nil {
		t.Fatal(err)
	}
	clean := model.ValidationRun{
		MotionProgramID: program.ID, ZoneSnapshot: "[]", ProgramSnapshot: "{}",
		AlgorithmVersion: testAlgorithmVersion, InputHash: "clean", IdempotencyKey: "clean",
		CollisionEventsJSON: "[]", InterlockFindingsJSON: "[]",
		ValidationStatus: constants.ValidationReviewed, Explanation: "all clear",
		BaselineRunID: &baseline.ID, RegressionDiffJSON: encodeRegressionDiff(cleanDiff),
		RequestedBy: 7, StartedAt: now.Add(-time.Minute), ReviewNote: "reviewed",
	}
	persistRun(t, db, &clean)
	accepted, err := service.Accept(clean.ID, "no new findings, accept", reviewer, "request-accept-clean")
	if err != nil {
		t.Fatalf("acceptance without new findings: %v", err)
	}
	if accepted.ValidationStatus != constants.ValidationAccepted || accepted.Baseline == nil || accepted.Baseline.ID != baseline.ID {
		t.Fatalf("accepted response = %+v", accepted)
	}

	// The first run of a program has no baseline: findings are recorded as new but
	// cannot be "regressions", so acceptance must be allowed to establish baseline zero.
	firstDiff, err := compareRuns(&model.ValidationRun{
		CollisionEventsJSON:   mustJSON(t, []dto.CollisionEvent{collisionEvent(0, 10, "gate", true)}),
		InterlockFindingsJSON: "[]",
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	first := model.ValidationRun{
		MotionProgramID: program.ID, ZoneSnapshot: "[]", ProgramSnapshot: "{}",
		AlgorithmVersion: testAlgorithmVersion, InputHash: "first", IdempotencyKey: "first",
		CollisionEventsJSON: mustJSON(t, []dto.CollisionEvent{collisionEvent(0, 10, "gate", true)}), InterlockFindingsJSON: "[]",
		ValidationStatus: constants.ValidationReviewed, Explanation: "first run with a known finding",
		BaselineRunID: nil, RegressionDiffJSON: encodeRegressionDiff(firstDiff),
		RequestedBy: 7, StartedAt: now.Add(-time.Minute), ReviewNote: "reviewed first",
	}
	persistRun(t, db, &first)
	if _, err := service.Accept(first.ID, "establish the first baseline", reviewer, "request-accept-first"); err != nil {
		t.Fatalf("first run without baseline must be acceptable: %v", err)
	}
}

func mustJSON(t *testing.T, value any) string {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(encoded)
}
