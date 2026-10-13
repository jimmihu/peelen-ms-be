# peelen-ms-be

Go/Gin authentication and authorization service backed by PostgreSQL, implementing
the Authentication & Authorization requirements in `prd/1.1 Authentication & Authorization.docx`.

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

`ddl/init.sql` describes the current PRD schema and is intended for a new/empty database.
The Make targets load connection settings from the local `.env` file:

```bash
make db-setup
make db-seed # optional development/test data only
```

`ddl/seed.sql` creates demo accounts with shared test credentials, so do not use it
on a production database and do not run it more than once.

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
