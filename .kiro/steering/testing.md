---
inclusion: manual
name: testing
description: Guidelines and conventions for running API integration tests against peelen-ms-be
---

# Testing Guidelines — peelen-ms-be

## Structure

```
tests/
  run_tests.sh          # Main runner: runs Newman for each env, outputs JSON reports
  parse_results.py      # Parses Newman JSON → unified CSV with full req/res detail
  results/              # Raw Newman JSON output per env (gitignored)
  *.csv                 # Final test result files (committed)
postman/
  peelen-ms-be-tests.postman_collection.json   # Source of truth for test cases
  env-super-admin.json  # SUPER_ADMIN credentials
  env-admin.json        # ADMIN credentials
  env-admin-worker.json # ADMIN_WORKER credentials
  env-worker.json       # WORKER credentials
```

## Environments

| File | Role | Username | Notes |
|------|------|----------|-------|
| env-super-admin.json | SUPER_ADMIN | superadmin | Full platform access |
| env-admin.json | ADMIN | admin.paiton | Site-scoped (fa0233b3) |
| env-admin-worker.json | ADMIN_WORKER | aw.mitra | Vendor-scoped (5e6f44f6) |
| env-worker.json | WORKER | worker01 | Invalid — use w.vnd001.01 |

> **Note:** `worker01` does not exist in the DB. Use `w.vnd001.01` / `Password123!` for the WORKER role.

## Running Tests

```bash
cd /home/ivan/go/src/peelen-ms-be
bash tests/run_tests.sh
```

This will:
1. Run Newman for each env file against the full Postman collection
2. Save raw JSON reports to `tests/results/`
3. Parse all reports into `tests/test_results_<ROLE>.csv` with full request/response

## CSV Format

Each CSV contains these columns:
- `Test ID` — sequential number
- `Folder` — Postman folder (e.g. "Policies - Create")
- `Test Name` — individual test case name
- `Role` — which env was used
- `Method` — HTTP method
- `URL` — resolved URL
- `Request Headers` — sent headers (Authorization redacted)
- `Request Body` — sent body/form data
- `HTTP Status` — actual response status code
- `Response Body` — actual response body (truncated at 500 chars)
- `Test Assertions` — pass/fail for each pm.test() assertion
- `Result` — PASS / FAIL / SKIP

## Conventions

- Tests are defined in the Postman collection; do not duplicate them in code.
- The runner uses `--bail false` so one failure does not stop the suite.
- Rate-limit protection: Newman uses `--delay-request 300` (300ms between requests).
- File uploads (inventory image, policy PDF) use a minimal synthetic file generated at runtime.
- Collection variables (`policy_id`, `vendor_id`, etc.) are chained — earlier tests set IDs used by later tests. Run the full collection in order.
- `invalid_uuid` = `00000000-0000-0000-0000-000000000000`
- `malformed_uuid` = `not-a-uuid`

## Adding New Tests

1. Add the test case to the Postman collection JSON under the correct folder.
2. Re-run `bash tests/run_tests.sh` to regenerate CSVs.
3. Commit both the updated collection and the new CSV.

## Known Role Limitations

| Endpoint | ADMIN_WORKER | WORKER |
|----------|:------------:|:------:|
| POST /users (SA only) | 403 | 403 |
| POST /sites | 403 | 403 |
| POST /items | 403 | 403 |
| GET /roles | 403 | 403 |
| GET /audit-logs | 403 | 403 |
| DELETE /sites | 403 | 403 |
| POST /policies | ✅ | 403 |
| GET /policies | ✅ | ✅ |
