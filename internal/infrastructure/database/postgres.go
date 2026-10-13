package database

import (
	"database/sql"
	"fmt"
	"net"
	"net/url"

	_ "github.com/lib/pq"

	"peelen-ms-be/internal/infrastructure/config"
)

// NewPostgresConnection creates a new PostgreSQL database connection
func NewPostgresConnection(cfg config.DatabaseConfig) (*sql.DB, error) {
	connectionURL := &url.URL{
		Scheme: "postgres",
		User:   url.UserPassword(cfg.User, cfg.Password),
		Host:   net.JoinHostPort(cfg.Host, cfg.Port),
		Path:   "/" + cfg.DBName,
	}
	query := url.Values{}
	query.Set("sslmode", cfg.SSLMode)
	connectionURL.RawQuery = query.Encode()

	db, err := sql.Open("postgres", connectionURL.String())
	if err != nil {
		return nil, fmt.Errorf("failed to open database connection: %w", err)
	}

	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	db.SetMaxOpenConns(25)
	db.SetMaxIdleConns(5)

	return db, nil
}
