package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"regexp"
	"strings"
	"time"
	"unicode"

	"github.com/joho/godotenv"
	"golang.org/x/crypto/bcrypt"

	"peelen-ms-be/internal/infrastructure/config"
	"peelen-ms-be/internal/infrastructure/database"
)

var usernamePattern = regexp.MustCompile(`^[a-z0-9._]{3,30}$`)

func main() {
	if err := godotenv.Load(); err != nil {
		log.Print("No .env file found; reading environment")
	}
	username := strings.ToLower(strings.TrimSpace(os.Getenv("BOOTSTRAP_ADMIN_USERNAME")))
	password := os.Getenv("BOOTSTRAP_ADMIN_PASSWORD")
	fullName := strings.TrimSpace(os.Getenv("BOOTSTRAP_ADMIN_NAME"))
	if !usernamePattern.MatchString(username) {
		log.Fatal("BOOTSTRAP_ADMIN_USERNAME must be 3-30 lowercase letters, digits, dots, or underscores")
	}
	if err := validatePassword(password); err != nil {
		log.Fatal(err)
	}

	db, err := database.NewPostgresConnection(config.Load().Database)
	if err != nil {
		log.Fatalf("Connect to database: %v", err)
	}
	defer db.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		log.Fatalf("Begin bootstrap transaction: %v", err)
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(719304221)`); err != nil {
		log.Fatalf("Lock bootstrap operation: %v", err)
	}

	var alreadyBootstrapped bool
	if err := tx.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM user_roles ur JOIN roles r ON r.id = ur.role_id
			WHERE r.code = 'SUPER_ADMIN'
		)
	`).Scan(&alreadyBootstrapped); err != nil {
		log.Fatalf("Check bootstrap state: %v", err)
	}
	if alreadyBootstrapped {
		log.Fatal("A SUPER_ADMIN already exists; bootstrap is one-time only")
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(password), 12)
	if err != nil {
		log.Fatalf("Hash bootstrap password: %v", err)
	}
	var userID string
	if err := tx.QueryRowContext(ctx, `
		INSERT INTO users (username, password_hash, full_name, must_change_password)
		VALUES ($1, $2, NULLIF($3, ''), TRUE)
		RETURNING id
	`, username, string(hash), fullName).Scan(&userID); err != nil {
		log.Fatalf("Create bootstrap user: %v", err)
	}
	result, err := tx.ExecContext(ctx, `
		INSERT INTO user_roles (user_id, role_id)
		SELECT $1, id FROM roles WHERE code = 'SUPER_ADMIN'
	`, userID)
	if err != nil {
		log.Fatalf("Assign SUPER_ADMIN role: %v", err)
	}
	if affected, err := result.RowsAffected(); err != nil || affected != 1 {
		if err != nil {
			log.Fatalf("Verify SUPER_ADMIN role assignment: %v", err)
		}
		log.Fatal("SUPER_ADMIN role is missing from the database")
	}
	if err := tx.QueryRowContext(ctx, `
		INSERT INTO audit_logs (user_id, action, entity, entity_id, after_data)
		VALUES ($1, 'CREATE', 'users', $1, jsonb_build_object('username', $2, 'roles', jsonb_build_array('SUPER_ADMIN')))
		RETURNING id
	`, userID, username).Scan(new(int64)); err != nil {
		log.Fatalf("Write bootstrap audit event: %v", err)
	}
	if err := tx.Commit(); err != nil {
		log.Fatalf("Commit bootstrap transaction: %v", err)
	}
	fmt.Println("Initial SUPER_ADMIN created. Remove bootstrap credentials from the environment.")
}

func validatePassword(password string) error {
	if len(password) < 8 || len(password) > 72 {
		return errors.New("BOOTSTRAP_ADMIN_PASSWORD must contain 8-72 bytes")
	}
	hasLetter, hasNumber := false, false
	for _, char := range password {
		if unicode.IsLetter(char) {
			hasLetter = true
		}
		if unicode.IsDigit(char) {
			hasNumber = true
		}
	}
	if !hasLetter || !hasNumber {
		return errors.New("BOOTSTRAP_ADMIN_PASSWORD must contain a letter and a number")
	}
	return nil
}
