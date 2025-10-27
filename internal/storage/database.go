// Package storage provides database connection management and pooling.
// It wraps GORM to provide:
// - Optimized connection pooling (environment-specific settings)
// - Automatic connection retry with exponential backoff
// - Health checks with timeout context
// - Connection statistics tracking and logging
// - Graceful shutdown handling
//
// Usage:
//
//	// Connect to database
//	err := storage.Connect(cfg.GetDSN(), cfg.DebugMode)
//	if err != nil {
//		log.Fatal().Err(err).Msg("Database connection failed")
//	}
//	defer storage.Close()
//
//	// Use global DB instance
//	db := storage.GetDB()
//	var users []models.User
//	db.Find(&users)
//
//	// Health check
//	if err := storage.HealthCheck(ctx); err != nil {
//		log.Error().Err(err).Msg("Database unhealthy")
//	}
package storage

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/phuc16/VCG-super-bot/internal/storage/migrations"
	"github.com/rs/zerolog/log"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// DB is the global database connection instance.
// Use GetDB() to access this in a type-safe manner.
var DB *gorm.DB

// ConnectionConfig holds database connection pool configuration.
// Use GetDefaultConnectionConfig() for recommended production settings.
type ConnectionConfig struct {
	MaxIdleConns    int
	MaxOpenConns    int
	ConnMaxLifetime time.Duration
	ConnMaxIdleTime time.Duration
}

// GetDefaultConnectionConfig returns sensible defaults for connection pooling.
// Production settings use larger pool sizes and longer connection lifetimes
// for better performance under load.
//
// Development: 10-50 connections, 15min lifetime
// Production: 25-100 connections, 30min lifetime
func GetDefaultConnectionConfig(isProduction bool) *ConnectionConfig {
	if isProduction {
		return &ConnectionConfig{
			MaxIdleConns:    25,
			MaxOpenConns:    100,
			ConnMaxLifetime: 30 * time.Minute,
			ConnMaxIdleTime: 10 * time.Minute,
		}
	}
	return &ConnectionConfig{
		MaxIdleConns:    10,
		MaxOpenConns:    50,
		ConnMaxLifetime: 15 * time.Minute,
		ConnMaxIdleTime: 5 * time.Minute,
	}
}

// Connect establishes a connection to the MySQL database with optimized pooling.
// Uses default connection pool settings based on debug mode (debug=false means production).
// Retries connection up to 5 times with exponential backoff on failure.
// Automatically runs pending database migrations on connection.
func Connect(dsn string, debug bool) error {
	isProduction := !debug
	config := GetDefaultConnectionConfig(isProduction)
	return ConnectWithConfig(dsn, debug, config)
}

// ConnectWithMigrations connects and automatically runs pending migrations
func ConnectWithMigrations(dsn string, debug bool) error {
	isProduction := !debug
	config := GetDefaultConnectionConfig(isProduction)

	if err := ConnectWithConfig(dsn, debug, config); err != nil {
		return err
	}

	// Run migrations automatically
	return RunMigrationsOnStartup()
}

// ConnectWithConfig establishes a connection with custom pool configuration
func ConnectWithConfig(dsn string, debug bool, config *ConnectionConfig) error {
	// Configure GORM logger
	var gormLogLevel logger.LogLevel
	if debug {
		gormLogLevel = logger.Info
	} else {
		gormLogLevel = logger.Warn
	}

	// Open connection with retry
	var db *gorm.DB
	var err error
	maxRetries := 5
	retryDelay := 2 * time.Second

	for attempt := 1; attempt <= maxRetries; attempt++ {
		db, err = gorm.Open(mysql.Open(dsn), &gorm.Config{
			Logger: logger.Default.LogMode(gormLogLevel),
			NowFunc: func() time.Time {
				return time.Now().UTC()
			},
		})

		if err == nil {
			break
		}

		log.Warn().
			Err(err).
			Int("attempt", attempt).
			Int("max_attempts", maxRetries).
			Dur("retry_delay", retryDelay).
			Msg("Database connection attempt failed, retrying...")

		if attempt < maxRetries {
			time.Sleep(retryDelay)
			retryDelay *= 2 // Exponential backoff
		}
	}

	if err != nil {
		return fmt.Errorf("failed to connect to database after %d attempts: %w", maxRetries, err)
	}

	// Get underlying SQL DB for connection pooling
	sqlDB, err := db.DB()
	if err != nil {
		return fmt.Errorf("failed to get database instance: %w", err)
	}

	// Apply connection pool settings
	sqlDB.SetMaxIdleConns(config.MaxIdleConns)
	sqlDB.SetMaxOpenConns(config.MaxOpenConns)
	sqlDB.SetConnMaxLifetime(config.ConnMaxLifetime)
	sqlDB.SetConnMaxIdleTime(config.ConnMaxIdleTime)

	// Test connection
	if err := sqlDB.Ping(); err != nil {
		return fmt.Errorf("failed to ping database: %w", err)
	}

	DB = db

	// Log connection pool stats
	stats := sqlDB.Stats()
	log.Info().
		Int("max_open_conns", config.MaxOpenConns).
		Int("max_idle_conns", config.MaxIdleConns).
		Dur("conn_max_lifetime", config.ConnMaxLifetime).
		Dur("conn_max_idle_time", config.ConnMaxIdleTime).
		Int("open_connections", stats.OpenConnections).
		Msg("Database connected successfully with optimized pool settings")

	return nil
}

// Close closes the database connection
func Close() error {
	if DB != nil {
		sqlDB, err := DB.DB()
		if err != nil {
			return err
		}
		return sqlDB.Close()
	}
	return nil
}

// GetDB returns the database instance
func GetDB() *gorm.DB {
	return DB
}

// HealthCheck performs a database health check with a 2-second timeout.
// Returns nil if the database is reachable and healthy, error otherwise.
// Used by health check endpoints to verify database connectivity.
//
// Example:
//
//	ctx := context.Background()
//	if err := storage.HealthCheck(ctx); err != nil {
//		return http.StatusServiceUnavailable
//	}
func HealthCheck(ctx context.Context) error {
	if DB == nil {
		return fmt.Errorf("database not initialized")
	}

	sqlDB, err := DB.DB()
	if err != nil {
		return fmt.Errorf("failed to get database instance: %w", err)
	}

	// Create context with timeout
	pingCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	if err := sqlDB.PingContext(pingCtx); err != nil {
		return fmt.Errorf("database ping failed: %w", err)
	}

	return nil
}

// GetConnectionStats returns current connection pool statistics
func GetConnectionStats() sql.DBStats {
	if DB == nil {
		return sql.DBStats{}
	}

	sqlDB, err := DB.DB()
	if err != nil {
		return sql.DBStats{}
	}

	return sqlDB.Stats()
}

// LogConnectionStats logs current connection pool statistics.
// Includes open connections, in-use connections, idle connections,
// wait counts, and connection closures. Useful for monitoring
// database performance and debugging connection issues.
//
// Called automatically on startup and shutdown, and periodically
// during runtime (every 5 minutes).
func LogConnectionStats() {
	stats := GetConnectionStats()
	log.Info().
		Int("open_connections", stats.OpenConnections).
		Int("in_use", stats.InUse).
		Int("idle", stats.Idle).
		Int64("wait_count", stats.WaitCount).
		Dur("wait_duration", stats.WaitDuration).
		Int64("max_idle_closed", stats.MaxIdleClosed).
		Int64("max_lifetime_closed", stats.MaxLifetimeClosed).
		Msg("Database connection pool statistics")
}

// RunMigrationsOnStartup automatically runs pending migrations when the bot starts
func RunMigrationsOnStartup() error {
	if DB == nil {
		return fmt.Errorf("database not connected")
	}

	// Get underlying SQL database for migrations
	sqlDB, err := DB.DB()
	if err != nil {
		return fmt.Errorf("failed to get database instance: %w", err)
	}

	// Create migrator
	migrator := migrations.NewMigrator(sqlDB)

	// Run pending migrations
	log.Info().Msg("Running automatic database migrations on startup...")
	if err := migrator.Up(); err != nil {
		return fmt.Errorf("failed to run migrations: %w", err)
	}

	log.Info().Msg("Database migrations completed successfully")
	return nil
}
