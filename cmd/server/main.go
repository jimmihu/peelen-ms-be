package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/joho/godotenv"

	"peelen-ms-be/internal/delivery/http/handler"
	"peelen-ms-be/internal/delivery/http/middleware"
	"peelen-ms-be/internal/delivery/http/router"
	"peelen-ms-be/internal/infrastructure/config"
	"peelen-ms-be/internal/infrastructure/database"
	"peelen-ms-be/internal/infrastructure/storage"
	"peelen-ms-be/internal/repository/postgres"
	"peelen-ms-be/internal/usecase"
)

func main() {
	// Load .env file if it exists
	if err := godotenv.Load(); err != nil {
		log.Println("No .env file found, using environment variables")
	}

	// Load configuration
	cfg := config.Load()
	if err := cfg.Server.Validate(); err != nil {
		log.Fatalf("Invalid server configuration: %v", err)
	}
	if err := cfg.Auth.Validate(); err != nil {
		log.Fatalf("Invalid authentication configuration: %v", err)
	}
	if err := cfg.ObjectStorage.Validate(); err != nil {
		log.Fatalf("Invalid object storage configuration: %v", err)
	}

	// Set Gin mode
	gin.SetMode(cfg.Server.Mode)

	// Initialize database connection
	db, err := database.NewPostgresConnection(cfg.Database)
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}
	defer db.Close()

	log.Println("Connected to database successfully")

	// Initialize repositories
	userRepo := postgres.NewUserRepository(db)
	sessionRepo := postgres.NewSessionRepository(db)

	// Initialize usecases
	authUsecaseConfig := usecase.AuthUsecaseConfig{
		AccessTokenDuration:  cfg.Auth.AccessTokenDuration,
		RefreshTokenDuration: cfg.Auth.RefreshTokenDuration,
		BcryptCost:           cfg.Auth.BcryptCost,
		ActiveKeyID:          cfg.Auth.ActiveKeyID,
		JWTKeys:              cfg.Auth.JWTKeys,
	}
	authUsecase := usecase.NewAuthUsecase(userRepo, sessionRepo, authUsecaseConfig)

	// Initialize handlers
	authHandler := handler.NewAuthHandler(authUsecase)
	adminRepository := postgres.NewAdminRepository(db)
	adminUsecase := usecase.NewAdminUsecase(adminRepository, cfg.Auth.BcryptCost)
	adminHandler := handler.NewAdminHandler(adminUsecase)
	operationalRepository := postgres.NewOperationalRepository(db)
	operationalUsecase := usecase.NewOperationalUsecase(operationalRepository)
	var objectStorage storage.ObjectStorage
	if cfg.ObjectStorage.Configured() {
		minioStorage, err := storage.NewMinIOObjectStorage(cfg.ObjectStorage)
		if err != nil {
			log.Fatalf("Initialize object storage: %v", err)
		}
		objectStorage = minioStorage
	}
	operationalHandler := handler.NewOperationalHandler(operationalUsecase, objectStorage)
	workflowRepository := postgres.NewWorkflowRepository(db)
	workflowUsecase := usecase.NewWorkflowUsecase(workflowRepository)
	workflowHandler := handler.NewWorkflowHandler(workflowUsecase)

	// Initialize middleware
	authMiddleware := middleware.NewAuthMiddleware(authUsecase)

	// Initialize router
	r := router.NewRouter(router.Config{
		AuthHandler:        authHandler,
		AdminHandler:       adminHandler,
		OperationalHandler: operationalHandler,
		WorkflowHandler:    workflowHandler,
		AuthMiddleware:     authMiddleware,
		EnforceHTTPS:       cfg.Server.Mode == "release",
		TrustedProxyCIDRs:  cfg.Server.TrustedProxyCIDRs,
	})
	if err := r.SetTrustedProxies(cfg.Server.TrustedProxyCIDRs); err != nil {
		log.Fatalf("Invalid trusted proxy configuration: %v", err)
	}

	// Create HTTP server
	srv := &http.Server{
		Addr:         ":" + cfg.Server.Port,
		Handler:      r,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Start server in a goroutine
	go func() {
		log.Printf("Starting server on port %s", cfg.Server.Port)
		var serveErr error
		if cfg.Server.TLSCertFile != "" {
			serveErr = srv.ListenAndServeTLS(cfg.Server.TLSCertFile, cfg.Server.TLSKeyFile)
		} else {
			serveErr = srv.ListenAndServe()
		}
		if serveErr != nil && serveErr != http.ErrServerClosed {
			log.Fatalf("Failed to start server: %v", serveErr)
		}
	}()

	// Wait for interrupt signal to gracefully shutdown the server
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Println("Shutting down server...")

	// Create a deadline to wait for
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		log.Fatalf("Server forced to shutdown: %v", err)
	}

	log.Println("Server exited gracefully")
}
