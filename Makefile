.PHONY: build run test clean deps db-setup db-seed db-reset db-seed-scoring db-migrate scoring-backfill e2e-sql e2e-reset bootstrap-admin

DB_NAME ?= auth_db

# Build the application
build:
	go build -mod=readonly -o bin/server ./cmd/server

# Run the application
run:
	go run -mod=readonly ./cmd/server

# Run tests
test:
	go test -mod=readonly -v ./...

# Clean build artifacts
clean:
	rm -rf bin/

# Download dependencies
deps:
	go mod download
	go mod tidy

# Run with hot reload (requires air)
dev:
	air

# Database setup - run the DDL
db-setup:
	set -a; . ./.env; set +a; PGSSLMODE="$$DB_SSLMODE" PGPASSWORD="$$DB_PASSWORD" psql -X -w -h "$$DB_HOST" -p "$$DB_PORT" -U "$$DB_USER" -d "$$DB_NAME" -v ON_ERROR_STOP=1 -f ddl/init.sql

# Development/test data only; seed.sql uses shared demo credentials and is not idempotent.
db-seed:
	set -a; . ./.env; set +a; PGSSLMODE="$$DB_SSLMODE" PGPASSWORD="$$DB_PASSWORD" psql -X -w -h "$$DB_HOST" -p "$$DB_PORT" -U "$$DB_USER" -d "$$DB_NAME" -v ON_ERROR_STOP=1 -f ddl/seed.sql

# Development only: wipes every table, then loads ddl/seed_v3.sql.
db-reset:
	set -a; . ./.env; set +a; PGSSLMODE="$$DB_SSLMODE" PGPASSWORD="$$DB_PASSWORD" psql -X -w -h "$$DB_HOST" -p "$$DB_PORT" -U "$$DB_USER" -d "$$DB_NAME" -v ON_ERROR_STOP=1 -f ddl/seed_v3.sql
	$(MAKE) db-seed-scoring

# Sample SLA/maturity rules; safe to re-run on an existing database (no reset).
db-seed-scoring:
	set -a; . ./.env; set +a; PGSSLMODE="$$DB_SSLMODE" PGPASSWORD="$$DB_PASSWORD" psql -X -w -h "$$DB_HOST" -p "$$DB_PORT" -U "$$DB_USER" -d "$$DB_NAME" -v ON_ERROR_STOP=1 -f ddl/seed_scoring.sql

# One scoring job cycle now: store task-day facts and freeze finished periods.
scoring-backfill:
	go run -mod=readonly ./cmd/scoring-backfill

# Applies migrations in order. Not every migration can be re-run (003 drops
# columns it reads), so on an existing database pass only the new ones, e.g.
#   make db-migrate MIGRATIONS="ddl/migrations/006_pipeline_site.sql ddl/migrations/007_scoring.sql"
MIGRATIONS ?= $(sort $(wildcard ddl/migrations/*.sql))
db-migrate:
	set -a; . ./.env; set +a; for f in $(MIGRATIONS); do echo "== $$f"; PGSSLMODE="$$DB_SSLMODE" PGPASSWORD="$$DB_PASSWORD" psql -X -w -h "$$DB_HOST" -p "$$DB_PORT" -U "$$DB_USER" -d "$$DB_NAME" -v ON_ERROR_STOP=1 -f "$$f" || exit 1; done

# One-file E2E reset (ddl/e2e_reset.sql), generated from the migration and seeds.
e2e-sql:
	scripts/build_e2e_reset.sh

# Wipe the dev database, load E2E data, and compute SLA/maturity results.
e2e-reset:
	set -a; . ./.env; set +a; PGSSLMODE="$$DB_SSLMODE" PGPASSWORD="$$DB_PASSWORD" psql -X -w -h "$$DB_HOST" -p "$$DB_PORT" -U "$$DB_USER" -d "$$DB_NAME" -v ON_ERROR_STOP=1 -f ddl/e2e_reset.sql
	$(MAKE) scoring-backfill

# Set BOOTSTRAP_ADMIN_USERNAME and BOOTSTRAP_ADMIN_PASSWORD in the environment first.
bootstrap-admin:
	go run -mod=readonly ./cmd/bootstrap-admin

# Create database
db-create:
	psql -h localhost -U postgres -c "CREATE DATABASE $(DB_NAME);"
