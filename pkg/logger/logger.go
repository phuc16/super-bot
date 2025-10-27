// Package logger provides structured logging functionality for VCG Super Bot.
// It wraps zerolog to provide a consistent logging interface with support for:
// - Structured logging with contextual fields
// - Multiple log levels (debug, info, warn, error)
// - Pretty console output with timestamps
// - Caller information for debugging
// - Error tracking with UUIDs (see errors.go)
// - Performance metrics tracking (see metrics.go)
//
// Usage:
//
//	logger.Init("info")
//	log.Info().Str("user_id", "12345").Msg("User action")
//	log.Error().Err(err).Msg("Operation failed")
package logger

import (
	"os"
	"strings"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

// Init initializes the global logger with the specified log level.
// Valid log levels: debug, info, warn, warning, error.
// Defaults to info if an invalid level is provided.
//
// Example:
//
//	logger.Init("debug")  // Enable debug logging
//	logger.Init("info")   // Standard logging (default)
func Init(level string) {
	// Set up console writer with timestamps
	output := zerolog.ConsoleWriter{
		Out:        os.Stdout,
		TimeFormat: time.RFC3339,
	}

	// Parse log level from environment
	var logLevel zerolog.Level
	switch strings.ToLower(level) {
	case "debug":
		logLevel = zerolog.DebugLevel
	case "info":
		logLevel = zerolog.InfoLevel
	case "warn", "warning":
		logLevel = zerolog.WarnLevel
	case "error":
		logLevel = zerolog.ErrorLevel
	default:
		logLevel = zerolog.InfoLevel
	}

	// Set global logger
	zerolog.SetGlobalLevel(logLevel)
	log.Logger = zerolog.New(output).With().Timestamp().Caller().Logger()
}

// GetLogger returns the global logger instance.
// Use this to obtain a logger reference for passing to other components.
//
// Example:
//
//	logger := logger.GetLogger()
//	logger.Info().Msg("Using logger reference")
func GetLogger() *zerolog.Logger {
	return &log.Logger
}
