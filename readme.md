# peelen-ms-be

Go/Gin service backed by PostgreSQL. Authentication and authorization are
implemented from `prd/1.1 Authentication & Authorization.docx`; `ddl/init.sql`
also defines the operational data model in `prd/1.2 PRD - Flow Per Akun.docx`.

## Implemented capabilities

- JWT access tokens (15 minutes by default) with `sub`, `roles`, `exp`, `iat`, and `jti`.
- Hashed, rotating refresh tokens (7 days by default), reuse detection, and session revocation.
- Username/password authentication, bcrypt cost 12, first-login password change, and 5-failure/15-minute account lock.
- User, role, and site/vendor scope management with role-specific permissions.
- Audit events for login/logout and sensitive account, role, scope, password, and session actions.
- Login/refresh rate limits (10 requests/minute per IP and username/token).
- HTTPS enforcement in release mode, `Secure`/`HttpOnly`/`SameSite=Strict` refresh cookie.

Self-registration is disabled. Accounts are created by Super Admins or, for Worker
accounts in their own vendor, Admin Workers.
The rate limiter is process-local; multi-replica deployments need a shared limiter
store to enforce a single cross-instance quota.

## Database setup

`ddl/init.sql` defines the authentication and operational tables, including
sites/vendors, inventory, policies, pipelines, task assignments, daily reports,
evidence references, and inventory reports. It can initialize an empty database
and extend the current auth schema with the operational tables and missing
site/vendor columns. It does not convert unrelated legacy schemas. Operational
API handlers and workflows beyond authentication/authorization are not implied
by the presence of these tables.
The Make targets load connection settings from the local `.env` file:

```bash
make db-setup
make db-seed # optional development/test data only
```

`ddl/seed.sql` creates demo accounts with shared test credentials, so do not use it
on a production database and do not run it more than once.
For PRD 1.2 operational fixtures, run `psql` against the initialized and auth-seeded
development database with `-f ddl/seed_v2.sql`. It can be re-run without duplicating
the sample inventory, items, pipeline, or worker assignments.

For a database created from an earlier version of the current schema where
`refresh_tokens.session_id` is missing, apply the migration:

```bash
make db-migrate
```

The migration invalidates any legacy refresh token that has no associated session.
It does not convert unrelated legacy schemas; back up such a database and plan a
separate data migration rather than rerunning the new DDL against it.

## Configuration and startup

Copy `.env.example` to `.env`, configure database access, and provide a JWT key of
at least 32 bytes. `JWT_KEYS` accepts semicolon-separated `kid=secret` entries so
old keys can remain available while signing rotates to `JWT_ACTIVE_KID`.

Run the one-time Super Admin bootstrap with credentials supplied only through the
environment. The bootstrap command refuses to run once a Super Admin role exists:

```bash
read -r -p "Bootstrap username: " BOOTSTRAP_ADMIN_USERNAME
read -r -s -p "Bootstrap password: " BOOTSTRAP_ADMIN_PASSWORD
echo
export BOOTSTRAP_ADMIN_USERNAME BOOTSTRAP_ADMIN_PASSWORD
go run -mod=readonly ./cmd/bootstrap-admin
unset BOOTSTRAP_ADMIN_USERNAME BOOTSTRAP_ADMIN_PASSWORD
```

Remove bootstrap credentials immediately after success. The password must be 8-72
bytes and include a letter and a number; it is hashed using bcrypt cost 12.

```bash
go run -mod=readonly ./cmd/server
```

In `GIN_MODE=release`, configure either `TLS_CERT_FILE` and `TLS_KEY_FILE` for
direct TLS or `TRUSTED_PROXY_CIDRS` for a TLS-terminating reverse proxy. Only
requests from those trusted proxy CIDRs with `X-Forwarded-Proto: https` are treated
as HTTPS. The app rejects plain HTTP in release mode.

## API

All endpoints use the `/api/v1` prefix.

Import [ddl/peelen-ms-be.postman_collection.json](ddl/peelen-ms-be.postman_collection.json)
into Postman to try the API. Set the collection variables for the base URL,
credentials, and valid vendor/site IDs first.

| Method | Path | Access |
|---|---|---|
| POST | `/auth/login` | Public; returns access token, sets HttpOnly refresh cookie |
| POST | `/auth/refresh` | Public; rotates refresh token and sets replacement cookie |
| POST | `/auth/logout` | Authenticated; revokes current session |
| POST | `/auth/logout-all` | Authenticated; revokes all of the caller's sessions |
| GET | `/auth/me` | Authenticated; returns profile, current roles and scopes |
| PUT | `/auth/password` | Authenticated; changes own password and revokes sessions |
| GET, POST | `/users` | Super Admin; Admin Worker sees/creates workers in own vendor |
| GET, PATCH | `/users/{id}` | Super Admin; Admin Worker limited to own vendor's workers |
| POST | `/users/{id}/reset-password` | Super Admin; Admin Worker limited to own vendor's workers |
| PUT | `/users/{id}/roles` | Super Admin |
| PUT | `/users/{id}/scopes` | Super Admin |
| DELETE | `/users/{id}/sessions` | Super Admin |
| GET | `/roles` | Super Admin |
| GET | `/audit-logs` | Super Admin |
| GET, POST | `/sites` | All roles list in scope; Super Admin manages |
| GET, PATCH, DELETE | `/sites/{id}` | All roles read in scope; Super Admin manages |
| GET, POST | `/vendors` | All roles list in scope; Super Admin manages |
| GET, PATCH, DELETE | `/vendors/{id}` | All roles read in scope; Super Admin manages |
| GET | `/shifts` | Authenticated; returns the available shifts and their hours |
| POST | `/shifts` | Super Admin; creates one of the predefined shift names |
| GET, PUT, DELETE | `/shifts/{shift}` | Authenticated read; Super Admin updates hours or deletes an unused shift |
| GET, POST | `/assignments` | SA, Admin, Admin Worker list in scope; Worker lists own; only Admin Worker assigns |
| GET, PATCH, DELETE | `/assignments/{id}` | Scoped read; only Admin Worker activates/deactivates in own vendor |
| GET | `/me/task` | Authenticated; active assignments for current user and date |
| GET, POST | `/pipeline-reports` | Scoped reports; assigned worker submits |
| GET, PATCH | `/pipeline-reports/{id}` | Scoped report read/update; manager reviews in scope |
| GET, POST | `/inventory-report` | Scoped inventory report list/create |
| GET | `/inventory-report/{id}` | Scoped inventory report details |
| PATCH | `/inventory-report/{id}/review` | Super Admin, Admin, or Admin Worker in scope |
| GET | `/dashboard/report` | Authenticated; daily scoped status summary |
| GET | `/reports/daily` | Authenticated; daily scoped pipeline completion summary |
| GET, POST | `/inventories` | All roles read in scope; SA, Admin, Admin Worker write |
| GET, PATCH, DELETE | `/inventories/{id}` | All roles read in scope; SA, Admin, Admin Worker write |
| GET, POST | `/policies` | All roles read in scope; SA, Admin, Admin Worker write |
| GET, PATCH, DELETE | `/policies/{id}` | All roles read in scope; SA, Admin, Admin Worker write |
| GET | `/policies/{id}/file` | All roles read in scope; returns a short-lived signed URL |
| GET, POST | `/items` | All roles read assigned/in-scope items; SA writes |
| GET, PATCH, DELETE | `/items/{id}` | All roles read assigned/in-scope items; SA writes |
| GET, POST | `/pipelines` | All roles read in scope; SA and Admin write |
| GET, PATCH, DELETE | `/pipelines/{id}` | All roles read in scope; SA and Admin write |
| GET, POST | `/pipelines/{id}/items` | All roles read assigned/in-scope tasks; SA, Admin, Admin Worker write |
| GET, PATCH, DELETE | `/pipeline-items/{id}` | All roles read assigned/in-scope tasks; SA, Admin, Admin Worker write |

Operational list endpoints accept `page` (default `1`), `limit` (default `20`,
maximum `100`), `status` (default `ACTIVE`), and `q`; vendor-scoped resources also
accept `vendor_id` and `site_id`. Inventory lists additionally accept
`status_condition`. Responses use `{ "data": [], "page": 1, "limit": 20, "total": 0 }`.
DELETE operations on operational master records set status to `INACTIVE`.

Inventory create/update requests use `multipart/form-data`; fields include
`vendor_id`, `item`, `jenis_item`, `qty`, and `status_condition`, with an optional
`image` file (JPEG, PNG, or WebP; up to 5 MiB). Policy create/update requests use
`multipart/form-data` with `name`, `vendor_id` (create only), and a `file` PDF (up
to 10 MiB). Files are stored in MinIO; create the bucket and configure
`MINIO_ENDPOINT`, `MINIO_ACCESS_KEY`, `MINIO_SECRET_KEY`, `MINIO_BUCKET`, and
`MINIO_USE_SSL` in the environment. `GET /inventories/{id}` returns a signed
`image_url` for active records; `GET /policies/{id}/file` returns a signed URL
valid for 10 minutes for active policies.

Pipeline creation and updates are available to Super Admin globally and Admin
within assigned sites. Master items are shared across pipelines and sites, so
only Super Admin may create, update, or deactivate them. Admin Workers may edit
pipeline items in their vendor scope, but may not create or update pipelines.
Worker pipeline/task reads are restricted to active assignments.

Sites and vendors are soft-deactivated by setting `status` to `INACTIVE`.
Super Admin can create and edit them; Admin reads sites/vendors attached to their
site scope, and vendor roles read only their assigned vendors and related sites.
`GET /me/task?date=YYYY-MM-DD` returns active tasks assigned to the authenticated
user, with any report already submitted for that date.
`GET /shifts` returns `pagi` (08:00-16:00), `siang` (16:00-00:00), and `malam`
(00:00-08:00). Assignments and pipeline reports must use one of these shift names.
Only Super Admin can manage shifts. Create with `shift`, `start_time`, and
`end_time` in `HH:MM` format; update only changes the hours, not the shift name.
A shift cannot be deleted while referenced by an assignment or report.

```http
POST /api/v1/shifts
Content-Type: application/json

{"shift":"pagi","start_time":"08:00","end_time":"16:00"}
```

Use `GET /api/v1/shifts/{shift}` to read one shift, `PUT` on the same path to
change its hours, and `DELETE` to remove an unused shift. Shift names are limited
to `pagi`, `siang`, and `malam`; duplicate creation or deleting a shift still in
use returns a conflict.

`POST /assignments` assigns a Worker to a pipeline item and shift with
`pipeline_item_id`, `user_id`, and `shift`. The target must be an active `WORKER`
scoped to the vendor that owns the pipeline, and the pipeline item, pipeline, and
item must be active. Only Admin Workers assign, within their own vendor; Super
Admins and Admins can read assignments in scope for monitoring. Creating an existing active assignment returns a
conflict; creating one that was deactivated reactivates it.
`PATCH /assignments/{id}` accepts only `is_active`, and `DELETE` deactivates. To move
a Worker to another shift, deactivate the assignment and create a new one, because
pipeline reports reference the pipeline item, user, and shift. `GET /assignments`
accepts `page`, `limit`, `vendor_id`, `site_id`, `pipeline_id`, `pipeline_item_id`,
`user_id`, `shift`, and `is_active`; Workers only see their own assignments.

`POST /pipeline-reports` accepts `pipeline_item_id`, `shift`, `report_date`
(`YYYY-MM-DD`), optional `status` (`IN_PROGRESS` or `SUBMITTED`), and optional
report details (`evidence_before`, `evidence_after`, `open_date`, `closed_date`,
`note_category`, `notes`). The caller must have an active assignment for the task
and shift. `PATCH /pipeline-reports/{id}` lets the assigned user update report
details/status or an in-scope manager set the review status (`COMPLETED`,
`REJECTED`, `IN_PROGRESS`, `SUBMITTED`) and revision state (`REQUESTED`, `NONE`).
Status and revision changes are recorded in `pipeline_report_history`.

`POST /inventory-report` accepts `vendor_id`, `report_type` (`REQUEST` or
`DAMAGED`), `description`, and optional `inventory_id` and `evidence` object key.
`DAMAGED` requires an inventory belonging to that vendor. Managers can review
reports using `PATCH /inventory-report/{id}/review` with `status` (`IN_REVIEW`,
`APPROVED`, `REJECTED`, or `RESOLVED`) and optional `review_reason`.
Report lists accept `page`, `limit`, `vendor_id`, `site_id`, `user_id`, `status`,
`from`, and `to`.

`GET /dashboard/report?date=YYYY-MM-DD` returns counts for pipeline and inventory
reports; `GET /reports/daily?date=YYYY-MM-DD` returns pipeline completion counts
grouped by vendor and pipeline. Both support optional `site_id` and `vendor_id`
filters and enforce the caller's scopes.

Login example:

```json
{
  "username": "worker_1",
  "password": "Password123"
}
```

The response body contains `access_token`, `expires_at`, and user authorization
details. The refresh token is returned only as a `Secure`, `HttpOnly`,
`SameSite=Strict` cookie (path `/api/v1/auth`), not in JSON. A non-browser client
may provide `refresh_token` in the refresh request body.

Create-user example (Super Admin):

```json
{
  "username": "worker_1",
  "full_name": "Worker One",
  "email": "worker@example.com",
  "password": "Temporary123",
  "roles": ["WORKER"],
  "vendor_ids": ["20000000-0000-4000-8000-000000000001"]
}
```

The response includes a temporary password (generated if omitted); it is never
written to audit logs, and the user must replace it on first login. Admin Workers
can only create `WORKER` accounts, and the request's vendor is ignored: the new
Worker is assigned to the actor's own vendor.

For role/scope changes, `PUT /api/v1/users/{id}/roles` accepts `roles` and optional
`site_ids`/`vendor_ids`; `PUT /api/v1/users/{id}/scopes` accepts the scope arrays
and an optional `roles` array. Omitted values are preserved, and role/scope
combinations are validated together. For example:

```json
{
  "roles": ["WORKER"],
  "vendor_ids": ["20000000-0000-4000-8000-000000000001"]
}
```

To generate a reset password, send `{}` to
`POST /api/v1/users/{id}/reset-password`; otherwise include a `password` field.

Error responses follow this shape:

```json
{
  "error": {
    "code": "AUTH_INVALID_CREDENTIALS",
    "message": "Username or password is incorrect"
  }
}
```

## Tests and quality checks

```bash
go test -mod=readonly ./...
go vet -mod=readonly ./...
```

The GitHub Actions workflow runs tests, vet, and build for pushes and pull requests.
