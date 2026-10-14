.PHONY: build run test clean deps db-setup db-seed db-reset db-migrate bootstrap-admin

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

db-migrate:
	set -a; . ./.env; set +a; PGSSLMODE="$$DB_SSLMODE" PGPASSWORD="$$DB_PASSWORD" psql -X -w -h "$$DB_HOST" -p "$$DB_PORT" -U "$$DB_USER" -d "$$DB_NAME" -v ON_ERROR_STOP=1 -f ddl/migrations/001_refresh_token_session.sql

# Set BOOTSTRAP_ADMIN_USERNAME and BOOTSTRAP_ADMIN_PASSWORD in the environment first.
bootstrap-admin:
	go run -mod=readonly ./cmd/bootstrap-admin

# Create database
db-create:
	psql -h localhost -U postgres -c "CREATE DATABASE $(DB_NAME);"
