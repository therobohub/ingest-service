package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/robohub/ingest-service/internal/config"
	"github.com/robohub/ingest-service/internal/db"
	"github.com/robohub/ingest-service/internal/httpapi"
)

func main() {
	// Setup logging
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	})))

	slog.Info("starting robohub-ingest service")

	// Load configuration
	cfg := config.Load()

	// Connect to database with GORM
	ctx := context.Background()
	
	gormDB, err := connectDatabase(cfg.DatabaseURL)
	if err != nil {
		slog.Error("failed to connect to database", "error", err)
		os.Exit(1)
	}

	sqlDB, err := gormDB.DB()
	if err != nil {
		slog.Error("failed to get database connection", "error", err)
		os.Exit(1)
	}
	defer sqlDB.Close()

	// Wait for database to be ready
	slog.Info("waiting for database to be ready")
	if err := waitForDB(ctx, gormDB, 10); err != nil {
		slog.Error("database not ready", "error", err)
		os.Exit(1)
	}
	slog.Info("database is ready")

	// Run automatic migrations
	slog.Info("running database migrations")
	if err := runMigrations(gormDB); err != nil {
		slog.Error("failed to run migrations", "error", err)
		os.Exit(1)
	}
	slog.Info("migrations completed successfully")

	// Create store
	store := db.NewGormStore(gormDB)

	// Create HTTP server
	server := httpapi.NewServer(store, cfg)
	handler := server.Routes()

	// Setup HTTP server
	addr := fmt.Sprintf(":%s", cfg.Port)
	httpServer := &http.Server{
		Addr:         addr,
		Handler:      handler,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Start server in a goroutine
	serverErrors := make(chan error, 1)
	go func() {
		slog.Info("starting HTTP server", "addr", addr)
		serverErrors <- httpServer.ListenAndServe()
	}()

	// Setup signal handling for graceful shutdown
	shutdown := make(chan os.Signal, 1)
	signal.Notify(shutdown, os.Interrupt, syscall.SIGTERM)

	// Block until error or shutdown signal
	select {
	case err := <-serverErrors:
		slog.Error("server error", "error", err)
		os.Exit(1)

	case sig := <-shutdown:
		slog.Info("received shutdown signal", "signal", sig)

		// Give outstanding requests 5 seconds to complete
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		if err := httpServer.Shutdown(ctx); err != nil {
			slog.Error("graceful shutdown failed", "error", err)
			if err := httpServer.Close(); err != nil {
				slog.Error("failed to close server", "error", err)
			}
		}

		slog.Info("server stopped")
	}
}

func connectDatabase(databaseURL string) (*gorm.DB, error) {
	// Configure GORM logger
	gormLogger := logger.New(
		slog.NewLogLogger(slog.Default().Handler(), slog.LevelWarn),
		logger.Config{
			SlowThreshold:             200 * time.Millisecond,
			LogLevel:                  logger.Warn,
			IgnoreRecordNotFoundError: true,
			Colorful:                  false,
		},
	)

	// Open database connection
	gormDB, err := gorm.Open(postgres.Open(databaseURL), &gorm.Config{
		Logger: gormLogger,
		NowFunc: func() time.Time {
			return time.Now().UTC()
		},
	})

	if err != nil {
		return nil, fmt.Errorf("failed to connect to database: %w", err)
	}

	// Configure connection pool
	sqlDB, err := gormDB.DB()
	if err != nil {
		return nil, fmt.Errorf("failed to get database connection: %w", err)
	}

	sqlDB.SetMaxIdleConns(10)
	sqlDB.SetMaxOpenConns(100)
	sqlDB.SetConnMaxLifetime(time.Hour)

	return gormDB, nil
}

func waitForDB(ctx context.Context, gormDB *gorm.DB, maxAttempts int) error {
	sqlDB, err := gormDB.DB()
	if err != nil {
		return err
	}

	var lastErr error
	for i := 0; i < maxAttempts; i++ {
		if err := sqlDB.PingContext(ctx); err == nil {
			return nil
		} else {
			lastErr = err
			slog.Warn("waiting for database", "attempt", i+1, "error", err)
			time.Sleep(2 * time.Second)
		}
	}
	return fmt.Errorf("database not ready after max attempts: %v", lastErr)
}

func runMigrations(gormDB *gorm.DB) error {
	// AutoMigrate will create tables, missing columns, and indexes
	// It will NOT delete columns or tables
	return gormDB.AutoMigrate(
		&db.Repo{},
		&db.Build{},
		&db.Image{},
		&db.BuildImage{},
	)
}
