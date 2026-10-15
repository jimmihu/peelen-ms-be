#!/usr/bin/env bash
# run-tests.sh — run the test collection for each role and produce a combined CSV report

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
COLLECTION="$SCRIPT_DIR/peelen-ms-be-tests.postman_collection.json"
OUTPUT_DIR="$SCRIPT_DIR/results"
CSV_FILE="$SCRIPT_DIR/results/test-results.csv"
TIMESTAMP="$(date '+%Y-%m-%d %H:%M:%S')"

mkdir -p "$OUTPUT_DIR"

# ── helpers ──────────────────────────────────────────────────────────────────

# Escape a value for CSV (wrap in quotes, escape internal quotes)
csv_escape() {
  local val="$1"
  val="${val//\"/\"\"}"
  echo "\"$val\""
}

# ── header ───────────────────────────────────────────────────────────────────

echo "Role,Folder,Test Name,Status,HTTP Status,Response Time (ms),Error" > "$CSV_FILE"

# ── per-role runs ─────────────────────────────────────────────────────────────

declare -A ROLES=(
  [SUPER_ADMIN]="$SCRIPT_DIR/env-super-admin.json"
  [ADMIN]="$SCRIPT_DIR/env-admin.json"
  [ADMIN_WORKER]="$SCRIPT_DIR/env-admin-worker.json"
  [WORKER]="$SCRIPT_DIR/env-worker.json"
)

ROLE_ORDER=("SUPER_ADMIN" "ADMIN" "ADMIN_WORKER" "WORKER")

for ROLE in "${ROLE_ORDER[@]}"; do
  ENV_FILE="${ROLES[$ROLE]}"
  JSON_OUT="$OUTPUT_DIR/${ROLE}.json"

  echo ""
  echo "▶  Running collection as role: $ROLE"
  echo "   Environment : $ENV_FILE"
  echo "   Output JSON : $JSON_OUT"

  # Run newman; --bail is NOT used so all tests run even on failure
  newman run "$COLLECTION" \
    --environment "$ENV_FILE" \
    --reporters json \
    --reporter-json-export "$JSON_OUT" \
    --timeout-request 15000 \
    --suppress-exit-code \
    2>&1 | tail -20 || true

  # ── parse JSON and append to CSV ──────────────────────────────────────────
  if [[ ! -f "$JSON_OUT" ]]; then
    echo "  WARNING: no output file found for $ROLE, skipping CSV rows"
    continue
  fi

  # Use node (available with newman) to parse the JSON report
  node - "$JSON_OUT" "$ROLE" "$CSV_FILE" <<'NODE_EOF'
const fs   = require('fs');
const path = require('path');

const [,, jsonFile, role, csvFile] = process.argv;
const report = JSON.parse(fs.readFileSync(jsonFile, 'utf8'));

const escape = (v) => `"${String(v ?? '').replace(/"/g, '""')}"`;

const lines = [];

const executions = report.run?.executions ?? [];

for (const exec of executions) {
  const folder   = exec.item?.name ?? '';          // request name (deepest)
  // Try to reconstruct folder path from parent chain
  const parents  = (exec.item?.__parent?.__parent?.name
                    ? exec.item.__parent.__parent.name + ' / '
                    : '') +
                   (exec.item?.__parent?.name
                    ? exec.item.__parent.name + ' / '
                    : '');
  const testName = folder;
  const httpStatus = exec.response?.code ?? '';
  const respTime   = exec.response?.responseTime ?? '';

  const assertions = exec.assertions ?? [];

  if (assertions.length === 0) {
    // Request ran but had no tests (pre-request items, etc.)
    lines.push([
      escape(role),
      escape(parents.replace(/ \/ $/, '')),
      escape(testName),
      escape('NO_TESTS'),
      escape(httpStatus),
      escape(respTime),
      escape('')
    ].join(','));
    continue;
  }

  for (const assertion of assertions) {
    const status = assertion.error ? 'FAIL' : 'PASS';
    const errMsg = assertion.error?.message ?? '';
    lines.push([
      escape(role),
      escape(parents.replace(/ \/ $/, '')),
      escape(testName + ' — ' + assertion.assertion),
      escape(status),
      escape(httpStatus),
      escape(respTime),
      escape(errMsg)
    ].join(','));
  }
}

fs.appendFileSync(csvFile, lines.join('\n') + (lines.length ? '\n' : ''));
console.log(`  ✔  Appended ${lines.length} rows to CSV for role ${role}`);
NODE_EOF

done

echo ""
echo "═══════════════════════════════════════════════════════"
echo "  Done. Results written to:"
echo "  $CSV_FILE"
echo "═══════════════════════════════════════════════════════"
