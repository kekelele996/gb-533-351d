import { ValidationStatus } from './enums/validation-status';
import { InterlockEvent, TrajectoryPoint } from './motion-program';
import { SafetyZone } from './safety-zone';

export interface CollisionEvent {
  segment_index: number;
  first_time_ms: number;
  zone_id: number;
  zone_name: string;
  zone_type: string;
  allowed_speed_mm_s: number;
  actual_speed_mm_s: number;
  clearance_mm: number;
  violation: boolean;
  evidence: string;
}

export interface InterlockFinding {
  code: string;
  event: string;
  depends_on?: string;
  path?: string[];
  evidence: string;
}

export interface BaselineSummary {
  id: number;
  motion_program_id: number;
  program_code: string;
  program_version: number;
  algorithm_version: string;
  validation_status: string;
  accepted_at?: string;
}

export type CollisionDiff = CollisionEvent;
export type InterlockDiff = InterlockFinding;

export interface FindingCategoryDiff {
  added: CollisionDiff[];
  removed: CollisionDiff[];
  persisted: CollisionDiff[];
}

export interface InterlockCategoryDiff {
  added: InterlockDiff[];
  removed: InterlockDiff[];
  persisted: InterlockDiff[];
}

export interface RegressionBaseline {
  bound: BaselineSummary;
  collision_diff: FindingCategoryDiff;
  interlock_diff: InterlockCategoryDiff;
  has_new_findings: boolean;
}

export interface ProgramSnapshot {
  id: number;
  robot_cell_id: number;
  program_code: string;
  version: number;
  trajectory?: TrajectoryPoint[];
  tool_radius_mm: number;
  payload_radius_mm: number;
  interlock_sequence: InterlockEvent[];
  source_checksum: string;
  program_state: string;
}

export type ZoneSnapshot = Pick<SafetyZone, 'id' | 'name' | 'zone_type' | 'polygon_geojson' | 'min_height_mm' | 'max_height_mm' | 'speed_limit_mm_s' | 'access_rule' | 'version'>;

export interface ValidationRun {
  id: number;
  motion_program_id: number;
  program_code: string;
  program_version: number;
  zone_snapshot: ZoneSnapshot[];
  program_snapshot: ProgramSnapshot;
  algorithm_version: string;
  input_hash: string;
  idempotency_key: string;
  attempt: number;
  retry_of_id?: number;
  baseline_run_id?: number;
  collision_events: CollisionEvent[];
  interlock_findings: InterlockFinding[];
  risk_score: number;
  validation_status: ValidationStatus;
  explanation: string;
  requested_by: number;
  started_at: string;
  finished_at?: string;
  reviewed_by?: number;
  reviewed_at?: string;
  review_note: string;
  reused: boolean;
  regression?: RegressionBaseline;
}

export interface AuditEvent {
  id: number;
  actor: string;
  role: string;
  action: string;
  resource_type: string;
  resource_id: string;
  request_id: string;
  parameters: Record<string, unknown>;
  before: Record<string, unknown>;
  after: Record<string, unknown>;
  created_at: string;
}
