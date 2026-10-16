#!/usr/bin/env bash
# run_tests.sh — runs the full Postman collection against every role env,
# captures raw Newman JSON reports, then calls parse_results.py to emit CSVs.
#
# Usage:
#   bash tests/run_tests.sh [ENV_NAME]
#
# If ENV_NAME is provided (e.g. "ADMIN_WORKER"), only that env is run.
# Otherwise all four envs are run in sequence.
#
# Output:
#   tests/results/<ROLE>_newman.json  — raw Newman report
#   tests/<ROLE>_test_results.csv     — parsed CSV with full req/res detail

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT="$SCRIPT_DIR/.."
POSTMAN_DIR="$ROOT/postman"
RESULTS_DIR="$SCRIPT_DIR/results"
COLLECTION="$POSTMAN_DIR/peelen-ms-be-tests.postman_collection.json"

mkdir -p "$RESULTS_DIR"

# Minimal valid PDF for upload tests (generated once)
PDF_FILE="$RESULTS_DIR/test_upload.pdf"
python3 -c "
content = (b'%PDF-1.4\n1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n'
           b'2 0 obj\n<< /Type /Pages /Kids [3 0 R] /Count 1 >>\nendobj\n'
           b'3 0 obj\n<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>\nendobj\n'
           b'xref\n0 4\n0000000000 65535 f \n0000000009 00000 n \n'
           b'0000000058 00000 n \n0000000115 00000 n \n'
           b'trailer\n<< /Size 4 /Root 1 0 R >>\nstartxref\n190\n%%EOF\n')
open('$PDF_FILE', 'wb').write(content)
"
echo "[runner] Test PDF created: $PDF_FILE"

# Map role name → env file
declare -A ENVS
ENVS["SUPER_ADMIN"]="$POSTMAN_DIR/env-super-admin.json"
ENVS["ADMIN"]="$POSTMAN_DIR/env-admin.json"
ENVS["ADMIN_WORKER"]="$POSTMAN_DIR/env-admin-worker.json"
ENVS["WORKER"]="$POSTMAN_DIR/env-worker.json"

# WORKER env uses worker01 which doesn't exist — patch it to w.test.fresh
WORKER_ENV_PATCHED="$RESULTS_DIR/env-worker-patched.json"
python3 -c "
import json
with open('$POSTMAN_DIR/env-worker.json') as f:
    env = json.load(f)
for v in env.get('values', []):
    if v['key'] == 'username':
        v['value'] = 'w.test.fresh'
with open('$WORKER_ENV_PATCHED', 'w') as f:
    json.dump(env, f)
print('[runner] Patched worker env: worker01 -> w.test.fresh')
"
ENVS["WORKER"]="$WORKER_ENV_PATCHED"

# Determine which envs to run
TARGET="${1:-}"
if [[ -n "$TARGET" ]]; then
    RUN_ENVS=("$TARGET")
else
    RUN_ENVS=("SUPER_ADMIN" "ADMIN" "ADMIN_WORKER" "WORKER")
fi

for ROLE in "${RUN_ENVS[@]}"; do
    ENV_FILE="${ENVS[$ROLE]}"
    REPORT="$RESULTS_DIR/${ROLE}_newman.json"
    CSV="$SCRIPT_DIR/${ROLE}_test_results.csv"

    echo ""
    echo "========================================"
    echo "[runner] Role: $ROLE"
    echo "[runner] Env:  $ENV_FILE"
    echo "[runner] Report → $REPORT"
    echo "========================================"

    # Run Newman — continue on test failures, capture JSON report
    newman run "$COLLECTION" \
        --environment "$ENV_FILE" \
        --reporters json \
        --reporter-json-export "$REPORT" \
        --delay-request 400 \
        --bail false \
        --insecure \
        --global-var "invalid_uuid=00000000-0000-0000-0000-000000000000" \
        --global-var "malformed_uuid=not-a-uuid" \
        2>&1 | tail -20 || true

    echo "[runner] Newman done for $ROLE. Parsing → $CSV"
    python3 "$SCRIPT_DIR/parse_results.py" "$REPORT" "$ROLE" "$CSV"
    echo "[runner] CSV written: $CSV"
done

echo ""
echo "[runner] All done. Results:"
ls -lh "$SCRIPT_DIR"/*.csv 2>/dev/null || echo "(no CSVs found)"
