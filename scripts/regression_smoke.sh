#!/usr/bin/env bash
# End-to-end regression-baseline checks: automatic baseline binding, collision and
# interlock new/gone/persisted classification, the new-findings acceptance gate,
# and fallback rebinding after baseline invalidation.
#
# The script writes fixture data and is not idempotent: point it at a freshly
# migrated empty database (for example a scratch SQLite server).
#   JWT_SECRET=... DB_DRIVER=sqlite DB_DSN=/tmp/reg.db PORT=19533 \
#     go run ./backend/cmd/server
#   scripts/regression_smoke.sh
set -euo pipefail

api_root="${API_ROOT:-http://127.0.0.1:${BACKEND_PORT:-19533}/api/v1}"
body_file="$(mktemp)"
trap 'rm -f "$body_file"' EXIT
last_body=""
checks=0

request() {
  local label="$1" expected="$2" method="$3" path="$4"
  local token="${5:-}" payload="${6:-}" idempotency="${7:-}"
  local args=(-sS -o "$body_file" -w "%{http_code}" -X "$method")
  [[ -n "$token" ]] && args+=(-H "Authorization: Bearer $token")
  [[ -n "$payload" ]] && args+=(-H "Content-Type: application/json" --data "$payload")
  [[ -n "$idempotency" ]] && args+=(-H "Idempotency-Key: $idempotency")
  local status
  status="$(curl "${args[@]}" "$api_root$path")"
  last_body="$(cat "$body_file")"
  checks=$((checks + 1))
  if [[ "$status" != "$expected" ]]; then
    printf 'FAIL %-46s expected=%s actual=%s body=%s\n' "$label" "$expected" "$status" "$last_body" >&2
    exit 1
  fi
  printf 'PASS %-46s HTTP %s\n' "$label" "$status"
}

require_json() {
  local expression="$1" message="$2"
  if ! jq -e "$expression" >/dev/null <<<"$last_body"; then
    printf 'FAIL assertion: %s body=%s\n' "$message" "$last_body" >&2
    exit 1
  fi
}

login() { # $1 user $2 pass
  curl -sS -X POST -H "Content-Type: application/json" \
    --data "{\"username\":\"$1\",\"password\":\"$2\"}" "$api_root/auth/login" | jq -r '.data.token'
}

engineer_token="$(login engineer 'Safety#533')"
programmer_token="$(login programmer 'Program#533')"
reviewer_token="$(login reviewer 'Review#533')"

# ---------- fixture cell with two restricted zones ----------
cell_payload='{"cell_code":"REG-CELL-1","name":"Regression cell","layout_geojson":{"type":"FeatureCollection","features":[]},"robot_model":"REG-Robot","controller_model":"REG-Control","max_reach_mm":2400,"owner_team":"QA"}'
request "engineer creates regression cell" 201 POST "/cells" "$engineer_token" "$cell_payload"
cell_id="$(jq -r '.data.id' <<<"$last_body")"
request "freeze cell layout" 200 POST "/cells/$cell_id/freeze" "$engineer_token" '{}'

zone1_payload="$(jq -nc --argjson cell "$cell_id" '{robot_cell_id:$cell,name:"REG restricted gate",zone_type:"restricted",polygon_geojson:{type:"Polygon",coordinates:[[[800,-400],[1500,-400],[1500,400],[800,400],[800,-400]]]},min_height_mm:0,max_height_mm:2200,speed_limit_mm_s:100,access_rule:"Gate lock must precede motion"}')"
request "create restricted zone 1" 201 POST "/zones" "$engineer_token" "$zone1_payload"
zone1_id="$(jq -r '.data.id' <<<"$last_body")"; zone1_v="$(jq -r '.data.version' <<<"$last_body")"
request "activate zone 1" 200 POST "/zones/$zone1_id/activate" "$engineer_token" "$(jq -nc --argjson v "$zone1_v" '{version:$v}')"
zone2_payload="$(jq -nc --argjson cell "$cell_id" '{robot_cell_id:$cell,name:"REG maintenance aisle",zone_type:"service",polygon_geojson:{type:"Polygon",coordinates:[[[-1600,1000],[1600,1000],[1600,1500],[-1600,1500],[-1600,1000]]]},min_height_mm:0,max_height_mm:2600,speed_limit_mm_s:0,access_rule:"Lockout required"}')"
request "create service zone 2" 201 POST "/zones" "$engineer_token" "$zone2_payload"
zone2_id="$(jq -r '.data.id' <<<"$last_body")"; zone2_v="$(jq -r '.data.version' <<<"$last_body")"
request "activate zone 2" 200 POST "/zones/$zone2_id/activate" "$engineer_token" "$(jq -nc --argjson v "$zone2_v" '{version:$v}')"

# interlock chain used by every version
interlocks='[{"name":"emergency_stop_reset","sequence":1,"depends_on":[]},{"name":"gate_locked","sequence":2,"depends_on":["emergency_stop_reset"]},{"name":"light_curtain_clear","sequence":3,"depends_on":["gate_locked"]}]'

import_ready() { # $1 version $2 trajectory json
  local version="$1" trajectory="$2"
  local payload
  payload="$(jq -nc --argjson cell "$cell_id" --argjson v "$version" --argjson traj "$trajectory" --argjson il "$interlocks" \
    '{robot_cell_id:$cell,program_code:"REG-MOVE-1",version:$v,trajectory:$traj,tool_radius_mm:160,payload_radius_mm:100,interlock_sequence:$il}')"
  request "programmer imports program v$version" 201 POST "/programs" "$programmer_token" "$payload"
  local pid; pid="$(jq -r '.data.id' <<<"$last_body")"
  request "parse program v$version" 200 POST "/programs/$pid/transition" "$programmer_token" '{"target_state":"parsed"}'
  request "ready program v$version" 200 POST "/programs/$pid/transition" "$programmer_token" '{"target_state":"ready"}'
  echo "$pid"
}
activate_program() { # $1 pid $2 version
  request "activate program v$2" 200 POST "/programs/$1/transition" "$programmer_token" '{"target_state":"active"}'
}

# v1: straight line crosses the restricted gate once (1 collision)
traj1='[{"x_mm":0,"y_mm":0,"z_mm":700,"time_ms":0,"speed_mm_s":450},{"x_mm":2000,"y_mm":0,"z_mm":700,"time_ms":5000,"speed_mm_s":450}]'
prog1="$(import_ready 1 "$traj1" | tail -1)"
activate_program "$prog1" 1
request "simulate v1 (first run, no baseline)" 201 POST "/validations" "$engineer_token" "$(jq -nc --argjson p "$prog1" '{motion_program_id:$p,retry_failed:false}')" "reg-v1-run"
run1="$(jq -r '.data.id' <<<"$last_body")"
require_json '.data.baseline == null and .data.regression_diff.has_new_findings == true and .data.regression_diff.new_collision_count == 1 and (.data.regression_diff.collision.new | length) == 1' "v1 binds no baseline and classifies its collision as new"
request "review v1" 200 POST "/validations/$run1/review" "$reviewer_token" '{"note":"First accepted run establishes the regression baseline."}'
# v1 has a new finding but no baseline exists; acceptance must be possible.
request "accept v1 establishes baseline" 200 POST "/validations/$run1/accept" "$reviewer_token" '{"note":"Baseline accepted; later runs must not introduce findings."}'
require_json '.data.validation_status == "accepted"' "v1 accepted"

# v2: same gate crossing, then an approach that stays clear of the gate before reaching the service aisle (gate persisted, aisle new)
traj2='[{"x_mm":0,"y_mm":0,"z_mm":700,"time_ms":0,"speed_mm_s":450},{"x_mm":2000,"y_mm":0,"z_mm":700,"time_ms":4000,"speed_mm_s":450},{"x_mm":2000,"y_mm":800,"z_mm":800,"time_ms":6000,"speed_mm_s":450},{"x_mm":1000,"y_mm":1400,"z_mm":800,"time_ms":9000,"speed_mm_s":450}]'
prog2="$(import_ready 2 "$traj2" | tail -1)"
activate_program "$prog2" 2
request "simulate v2" 201 POST "/validations" "$engineer_token" "$(jq -nc --argjson p "$prog2" '{motion_program_id:$p,retry_failed:false}')" "reg-v2-run"
run2="$(jq -r '.data.id' <<<"$last_body")"
require_json ".data.baseline.id == $run1 and .data.regression_diff.has_new_findings == true and .data.regression_diff.new_collision_count == 1 and (.data.regression_diff.collision.persisted | length) == 1 and (.data.regression_diff.collision.gone | length) == 0" "v2 auto-binds v1; gate persists, aisle contacts new"
request "review v2" 200 POST "/validations/$run2/review" "$reviewer_token" '{"note":"Reviewer approval must not override new regression findings."}'
request "accept v2 blocked by new findings" 409 POST "/validations/$run2/accept" "$reviewer_token" '{"note":"Attempt to accept despite new collision findings."}'
require_json '.error.code == "new_regression_findings"' "new_regression_findings error code"
request "v2 stays reviewed after blocked accept" 200 GET "/validations/$run2" "$reviewer_token"
require_json '.data.validation_status == "reviewed" and .data.baseline.id == '"$run1" "v2 unchanged after blocked acceptance"

# v3: moves well clear of both zones -> 0 findings; gate finding disappears, nothing new
traj3='[{"x_mm":0,"y_mm":0,"z_mm":700,"time_ms":0,"speed_mm_s":450},{"x_mm":-2000,"y_mm":-1400,"z_mm":700,"time_ms":6000,"speed_mm_s":450}]'
prog3="$(import_ready 3 "$traj3" | tail -1)"
activate_program "$prog3" 3
request "simulate v3" 201 POST "/validations" "$engineer_token" "$(jq -nc --argjson p "$prog3" '{motion_program_id:$p,retry_failed:false}')" "reg-v3-run"
run3="$(jq -r '.data.id' <<<"$last_body")"
require_json ".data.baseline.id == $run1 and .data.regression_diff.has_new_findings == false and .data.regression_diff.new_collision_count == 0 and (.data.regression_diff.collision.gone | length) == 1 and (.data.regression_diff.collision.persisted | length) == 0" "v3 binds v1; gate finding gone, nothing new"
request "review v3" 200 POST "/validations/$run3/review" "$reviewer_token" '{"note":"Clean rerun, only findings disappeared relative to baseline."}'
request "accept v3 allowed" 200 POST "/validations/$run3/accept" "$reviewer_token" '{"note":"Newest accepted run becomes the active regression baseline."}'
require_json '.data.validation_status == "accepted" and .data.baseline.id == '"$run1" "v3 accepted and still displays bound v1"

# v4: same clean input as v3 -> idempotent input reuse
request "repeat v3 simulation reuses run" 200 POST "/validations" "$engineer_token" "$(jq -nc --argjson p "$prog3" '{motion_program_id:$p,retry_failed:false}')" "reg-v3-rerun"
require_json ".data.id == $run3 and .data.reused == true and .data.baseline.id == $run1" "reused response keeps baseline binding"

# ---------- invalidation fallback chain ----------
# v4 repeats the gate crossing: new vs clean baseline v3, persisted after fallback to v1.
traj4='[{"x_mm":0,"y_mm":0,"z_mm":700,"time_ms":0,"speed_mm_s":450},{"x_mm":2000,"y_mm":0,"z_mm":700,"time_ms":5000,"speed_mm_s":450}]'
prog4="$(import_ready 4 "$traj4" | tail -1)"
activate_program "$prog4" 4
request "simulate v4 against latest baseline v3" 201 POST "/validations" "$engineer_token" "$(jq -nc --argjson p "$prog4" '{motion_program_id:$p,retry_failed:false}')" "reg-v4-run"
run4="$(jq -r '.data.id' <<<"$last_body")"
require_json ".data.baseline.id == $run3 and .data.regression_diff.new_collision_count == 1 and (.data.regression_diff.collision.gone | length) == 0" "v4 binds newest accepted run v3, gate collision new"

# Void v3: v4 must fall back to v1; gate collision now persisted, no new findings.
request "void accepted baseline v3" 200 POST "/validations/$run3/void" "$reviewer_token" '{"note":"Invalidate newest baseline to exercise fallback rebinding."}'
request "v4 rebound to previous baseline v1" 200 GET "/validations/$run4" "$reviewer_token"
require_json ".data.baseline.id == $run1 and .data.regression_diff.has_new_findings == false and (.data.regression_diff.collision.persisted | length) == 1 and (.data.regression_diff.collision.new | length) == 0" "v4 falls back to v1 after v3 voided"

# Refresh-equivalent second fetch returns identical binding and classification.
request "v4 fetch stays consistent on refresh" 200 GET "/validations/$run4" "$engineer_token"
require_json ".data.baseline.id == $run1 and .data.regression_diff.new_collision_count == 0 and .data.regression_diff.new_interlock_count == 0" "v4 binding stable across reads"

# Void v1: v4 must have no baseline left; its collision becomes new again.
request "void previous baseline v1" 200 POST "/validations/$run1/void" "$reviewer_token" '{"note":"Invalidate previous baseline; dependents lose baseline."}'
request "v4 loses baseline entirely" 200 GET "/validations/$run4" "$reviewer_token"
require_json '.data.baseline == null and .data.regression_diff.has_new_findings == true and .data.regression_diff.new_collision_count == 1 and (.data.regression_diff.collision.gone | length) == 0' "v4 ends without baseline; finding reclassified as new"

# ---------- interlock regression category ----------
prog5_payload="$(jq -nc --argjson cell "$cell_id" --argjson il '[{"name":"motion_start","sequence":1,"depends_on":["missing_gate_signal"]}]' \
  '{robot_cell_id:$cell,program_code:"REG-IL-1",version:1,trajectory:[{"x_mm":0,"y_mm":0,"z_mm":700,"time_ms":0,"speed_mm_s":300},{"x_mm":-1000,"y_mm":-700,"z_mm":700,"time_ms":3000,"speed_mm_s":300},{"x_mm":-2000,"y_mm":-1400,"z_mm":700,"time_ms":6000,"speed_mm_s":300}],tool_radius_mm:160,payload_radius_mm:100,interlock_sequence:$il}')"
request "import interlock program v1" 201 POST "/programs" "$programmer_token" "$prog5_payload"
prog5="$(jq -r '.data.id' <<<"$last_body")"
request "parse interlock v1" 200 POST "/programs/$prog5/transition" "$programmer_token" '{"target_state":"parsed"}'
request "ready interlock v1" 200 POST "/programs/$prog5/transition" "$programmer_token" '{"target_state":"ready"}'
activate_program "$prog5" 1
request "simulate interlock v1" 201 POST "/validations" "$engineer_token" "$(jq -nc --argjson p "$prog5" '{motion_program_id:$p,retry_failed:false}')" "reg-il-v1"
runil1="$(jq -r '.data.id' <<<"$last_body")"
require_json '.data.baseline == null and .data.regression_diff.new_interlock_count == 1 and .data.regression_diff.interlock.new[0].code == "missing_prerequisite"' "first interlock finding is new"
request "review interlock v1" 200 POST "/validations/$runil1/review" "$reviewer_token" '{"note":"First program version accepted as baseline."}'
request "accept interlock v1" 200 POST "/validations/$runil1/accept" "$reviewer_token" '{"note":"Interlock baseline established."}'

prog6_payload="$(jq -nc --argjson cell "$cell_id" --argjson il '[{"name":"a","sequence":1,"depends_on":["b"]},{"name":"b","sequence":2,"depends_on":["a"]}]' \
  '{robot_cell_id:$cell,program_code:"REG-IL-1",version:2,trajectory:[{"x_mm":0,"y_mm":0,"z_mm":700,"time_ms":0,"speed_mm_s":300},{"x_mm":-1000,"y_mm":-700,"z_mm":700,"time_ms":3000,"speed_mm_s":300},{"x_mm":-2000,"y_mm":-1400,"z_mm":700,"time_ms":6000,"speed_mm_s":300}],tool_radius_mm:160,payload_radius_mm:100,interlock_sequence:$il}')"
request "import interlock program v2" 201 POST "/programs" "$programmer_token" "$prog6_payload"
prog6="$(jq -r '.data.id' <<<"$last_body")"
request "parse interlock v2" 200 POST "/programs/$prog6/transition" "$programmer_token" '{"target_state":"parsed"}'
request "ready interlock v2" 200 POST "/programs/$prog6/transition" "$programmer_token" '{"target_state":"ready"}'
activate_program "$prog6" 2
request "simulate interlock v2" 201 POST "/validations" "$engineer_token" "$(jq -nc --argjson p "$prog6" '{motion_program_id:$p,retry_failed:false}')" "reg-il-v2"
runil2="$(jq -r '.data.id' <<<"$last_body")"
require_json ".data.baseline.id == $runil1 and .data.regression_diff.has_new_findings == true and (.data.regression_diff.interlock.new | map(.code) | index(\"dependency_cycle\")) != null and (.data.regression_diff.interlock.gone | length) == 1" "interlock v2: missing prerequisite gone, cycle new"
request "review interlock v2" 200 POST "/validations/$runil2/review" "$reviewer_token" '{"note":"Cycle is a new finding; acceptance must remain blocked."}'
request "interlock v2 accept blocked" 409 POST "/validations/$runil2/accept" "$reviewer_token" '{"note":"Attempt to accept a new dependency cycle."}'
require_json '.error.code == "new_regression_findings"' "interlock acceptance blocked with same error code"

printf '\nALL %d REGRESSION CHECKS PASSED\n' "$checks"
