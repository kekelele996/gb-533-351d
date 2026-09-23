package repository

import (
	"fmt"
	"sort"

	"gorm.io/gorm"

	"robot-cell-safety-envelope-validator/backend/internal/model"
)

type ValidationRunRepository struct{ db *gorm.DB }

func NewValidationRunRepository(db *gorm.DB) *ValidationRunRepository {
	return &ValidationRunRepository{db: db}
}
func (repository *ValidationRunRepository) WithDB(db *gorm.DB) *ValidationRunRepository {
	return &ValidationRunRepository{db: db}
}

func (repository *ValidationRunRepository) Create(run *model.ValidationRun) error {
	if err := repository.db.Create(run).Error; err != nil {
		return fmt.Errorf("create validation run: %w", err)
	}
	return nil
}

func (repository *ValidationRunRepository) Get(id uint) (model.ValidationRun, error) {
	var run model.ValidationRun
	if err := repository.db.Preload("MotionProgram").First(&run, id).Error; err != nil {
		return run, fmt.Errorf("get validation run: %w", err)
	}
	return run, nil
}

func (repository *ValidationRunRepository) List(page, pageSize int, programID uint, status string) ([]model.ValidationRun, int64, error) {
	query := repository.db.Model(&model.ValidationRun{})
	if programID > 0 {
		query = query.Where("motion_program_id = ?", programID)
	}
	if status != "" {
		query = query.Where("validation_status = ?", status)
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, fmt.Errorf("count validation runs: %w", err)
	}
	var runs []model.ValidationRun
	if err := query.Preload("MotionProgram").Order("started_at DESC, id DESC").Offset((page - 1) * pageSize).Limit(pageSize).Find(&runs).Error; err != nil {
		return nil, 0, fmt.Errorf("list validation runs: %w", err)
	}
	return runs, total, nil
}

// LatestAcceptedBaseline returns the most recently accepted run for the same
// robot cell, program code and algorithm version. Such runs are the only
// eligible regression baselines.
func (repository *ValidationRunRepository) LatestAcceptedBaseline(cellID uint, programCode, algorithmVersion string) (model.ValidationRun, error) {
	runs, err := repository.acceptedBaselines(cellID, programCode, algorithmVersion)
	if err != nil {
		return model.ValidationRun{}, err
	}
	if len(runs) == 0 {
		return model.ValidationRun{}, fmt.Errorf("find latest accepted baseline: %w", gorm.ErrRecordNotFound)
	}
	return runs[0], nil
}

// PreviousAcceptedBaseline returns the accepted run immediately before anchor
// in (reviewed_at, id) order, excluding the anchor itself. Ordering is resolved
// in Go so timestamp storage differences (e.g. SQLite CURRENT_TIMESTAMP) cannot
// skew the fallback.
func (repository *ValidationRunRepository) PreviousAcceptedBaseline(cellID uint, programCode, algorithmVersion string, anchorID uint) (model.ValidationRun, error) {
	runs, err := repository.acceptedBaselines(cellID, programCode, algorithmVersion)
	if err != nil {
		return model.ValidationRun{}, err
	}
	for index, run := range runs {
		if run.ID == anchorID && index+1 < len(runs) {
			return runs[index+1], nil
		}
	}
	return model.ValidationRun{}, fmt.Errorf("find previous accepted baseline: %w", gorm.ErrRecordNotFound)
}

func (repository *ValidationRunRepository) acceptedBaselines(cellID uint, programCode, algorithmVersion string) ([]model.ValidationRun, error) {
	var runs []model.ValidationRun
	if err := repository.db.Model(&model.ValidationRun{}).
		Joins("JOIN motion_programs ON motion_programs.id = validation_runs.motion_program_id").
		Where("validation_runs.validation_status = ?", "accepted").
		Where("motion_programs.robot_cell_id = ? AND motion_programs.program_code = ?", cellID, programCode).
		Where("validation_runs.algorithm_version = ?", algorithmVersion).
		Find(&runs).Error; err != nil {
		return nil, fmt.Errorf("list accepted baselines: %w", err)
	}
	// Order in Go: the SQLite and PostgreSQL drivers parse stored timestamps
	// into time.Time, so lexical differences in on-disk formats (with/without
	// fractional seconds or timezone offsets) cannot change baseline order.
	sort.SliceStable(runs, func(i, j int) bool {
		left, right := runs[i].ReviewedAt, runs[j].ReviewedAt
		switch {
		case left == nil && right == nil:
			return runs[i].ID > runs[j].ID
		case left == nil:
			return false
		case right == nil:
			return true
		case left.Equal(*right):
			return runs[i].ID > runs[j].ID
		default:
			return left.After(*right)
		}
	})
	return runs, nil
}

// FindBaselinesByID loads the runs referenced as baselines.
func (repository *ValidationRunRepository) FindBaselinesByID(ids []uint) ([]model.ValidationRun, error) {
	var runs []model.ValidationRun
	if len(ids) == 0 {
		return runs, nil
	}
	if err := repository.db.Preload("MotionProgram").Where("id IN ?", ids).Find(&runs).Error; err != nil {
		return nil, fmt.Errorf("find baseline runs: %w", err)
	}
	return runs, nil
}

// RebindBaselines moves every run whose baseline was voided onto the fallback
// baseline (or clears the binding when no earlier accepted run remains). The
// voided run itself is excluded because it is no longer a baseline candidate.
func (repository *ValidationRunRepository) RebindBaselines(voidedID uint, fallbackID *uint) (int64, error) {
	result := repository.db.Model(&model.ValidationRun{}).
		Where("baseline_run_id = ? AND id <> ?", voidedID, voidedID).
		Update("baseline_run_id", fallbackID)
	if result.Error != nil {
		return 0, fmt.Errorf("rebind validation baselines: %w", result.Error)
	}
	return result.RowsAffected, nil
}

func (repository *ValidationRunRepository) FindByIdempotencyKey(key string) (model.ValidationRun, error) {
	var run model.ValidationRun
	if err := repository.db.Preload("MotionProgram").Where("idempotency_key = ?", key).First(&run).Error; err != nil {
		return run, fmt.Errorf("find idempotent run: %w", err)
	}
	return run, nil
}

func (repository *ValidationRunRepository) LatestByInput(inputHash, algorithmVersion string) (model.ValidationRun, error) {
	var run model.ValidationRun
	if err := repository.db.Preload("MotionProgram").Where("input_hash = ? AND algorithm_version = ?", inputHash, algorithmVersion).
		Order("attempt DESC, id DESC").First(&run).Error; err != nil {
		return run, fmt.Errorf("find latest input run: %w", err)
	}
	return run, nil
}

func (repository *ValidationRunRepository) SetSimulating(id uint) error {
	result := repository.db.Model(&model.ValidationRun{}).Where("id = ? AND validation_status = ?", id, "queued").Update("validation_status", "simulating")
	if result.Error != nil {
		return fmt.Errorf("start validation run: %w", result.Error)
	}
	if result.RowsAffected != 1 {
		return ErrStateConflict
	}
	return nil
}

func (repository *ValidationRunRepository) Finish(run *model.ValidationRun) error {
	result := repository.db.Model(&model.ValidationRun{}).Where("id = ? AND validation_status = ?", run.ID, "simulating").Updates(map[string]any{
		"collision_events_json": run.CollisionEventsJSON, "interlock_findings_json": run.InterlockFindingsJSON,
		"risk_score": run.RiskScore, "validation_status": run.ValidationStatus,
		"explanation": run.Explanation, "finished_at": run.FinishedAt,
	})
	if result.Error != nil {
		return fmt.Errorf("finish validation run: %w", result.Error)
	}
	if result.RowsAffected != 1 {
		return ErrStateConflict
	}
	return nil
}

func (repository *ValidationRunRepository) Review(id uint, from, to string, reviewer uint, note string) error {
	result := repository.db.Model(&model.ValidationRun{}).Where("id = ? AND validation_status = ?", id, from).
		Updates(map[string]any{"validation_status": to, "reviewed_by": reviewer, "reviewed_at": gorm.Expr("CURRENT_TIMESTAMP"), "review_note": note})
	if result.Error != nil {
		return fmt.Errorf("review validation run: %w", result.Error)
	}
	if result.RowsAffected != 1 {
		return ErrStateConflict
	}
	return nil
}
