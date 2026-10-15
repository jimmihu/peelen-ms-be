// Command scoring-backfill runs one scoring job cycle on demand: it stores the
// task-day facts of every final day and freezes every finished period, using
// the same code as the server's background job. Useful right after seeding.
package main

import (
	"context"
	"log"
	"time"
	_ "time/tzdata"

	"github.com/joho/godotenv"

	"peelen-ms-be/internal/infrastructure/config"
	"peelen-ms-be/internal/infrastructure/database"
	"peelen-ms-be/internal/repository/postgres"
	"peelen-ms-be/internal/usecase"
)

func main() {
	if err := godotenv.Load(); err != nil {
		log.Print("No .env file found; reading environment")
	}
	cfg := config.Load()
	location, err := time.LoadLocation(cfg.Database.TimeZone)
	if err != nil {
		log.Fatalf("Invalid APP_TIMEZONE %q: %v", cfg.Database.TimeZone, err)
	}
	db, err := database.NewPostgresConnection(cfg.Database)
	if err != nil {
		log.Fatalf("Connect to database: %v", err)
	}
	defer db.Close()

	scoring := usecase.NewScoringUsecase(postgres.NewScoringRepository(db)).(*usecase.ScoringUsecaseImpl).WithLocation(location)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	facts, err := scoring.FinalizeFacts(ctx)
	if err != nil {
		log.Fatalf("Finalize facts: %v", err)
	}
	log.Printf("Stored %d task-day facts", facts)
	periods, err := scoring.ClosePeriods(ctx)
	if err != nil {
		log.Fatalf("Close periods: %v", err)
	}
	log.Printf("Froze %d rule periods", periods)
}
