// Package config provides configuration management for Discord Super Bot.
// It loads configuration from environment variables and validates all settings
// on startup, failing fast if any required values are missing or invalid.
//
// Configuration includes:
// - Discord bot credentials and guild ID
// - OpenAI API credentials and settings
// - Database connection parameters
// - Application environment and logging
// - Feature flags for enabling/disabling functionality
// - Rate limiting parameters
// - GPT API settings (model, budget, concurrency)
//
// Usage:
//
//	cfg, err := config.Load()
//	if err != nil {
//		// Configuration error - bot will not start
//		log.Fatal().Err(err).Msg("Failed to load config")
//	}
//	// cfg is validated and ready to use
package config

import (
	"fmt"
	"os"
	"strconv"
)

// Config holds all application configuration loaded from environment variables.
// All fields are populated during Load() and validated before being returned.
type Config struct {
	// Discord
	DiscordToken   string
	DiscordGuildID string

	// OpenAI
	OpenAIAPIKey string
	GPTModel     string
	GPTMaxTokens int

	// Database
	DBHost     string
	DBPort     string
	DBUser     string
	DBPassword string
	DBName     string

	// Application
	AppEnv   string
	LogLevel string
	Port     string
	Timezone string

	// Rate Limiting
	RateLimitExportPerHour     int
	RateLimitActivityPerMinute int

	// GPT
	GPTMaxConcurrentRequests int
	GPTRequestTimeoutSeconds int
	GPTDailyBudgetUSD        float64

	// Development
	DebugMode  bool
	MockGPTAPI bool
}

// Load reads and validates configuration from environment variables.
// Returns a fully validated Config struct or an error if any required
// fields are missing or invalid. The bot should not start if this returns an error.
//
// Required environment variables:
// - DISCORD_TOKEN: Discord bot token
// - DISCORD_GUILD_ID: Target Discord server ID
// - OPENAI_API_KEY: OpenAI API key (unless MOCK_GPT_API=true)
// - DB_PASSWORD: MySQL database password
//
// See env.example for all available configuration options.
func Load() (*Config, error) {
	cfg := &Config{
		// Discord
		DiscordToken:   getEnv("DISCORD_TOKEN", ""),
		DiscordGuildID: getEnv("DISCORD_GUILD_ID", ""),

		// OpenAI
		OpenAIAPIKey: getEnv("OPENAI_API_KEY", ""),
		GPTModel:     getEnv("GPT_MODEL", "gpt-4o-mini"),
		GPTMaxTokens: getEnvAsInt("GPT_MAX_TOKENS", 4096),

		// Database
		DBHost:     getEnv("DB_HOST", "localhost"),
		DBPort:     getEnv("DB_PORT", "3306"),
		DBUser:     getEnv("DB_USER", "superbot"),
		DBPassword: getEnv("DB_PASSWORD", ""),
		DBName:     getEnv("DB_NAME", "superbot_dev"),

		// Application
		AppEnv:   getEnv("APP_ENV", "development"),
		LogLevel: getEnv("LOG_LEVEL", "info"),
		Port:     getEnv("PORT", "8080"),
		Timezone: getEnv("TIMEZONE", "UTC"),

		// Rate Limiting
		RateLimitExportPerHour:     getEnvAsInt("RATE_LIMIT_EXPORT_PER_HOUR", 3),
		RateLimitActivityPerMinute: getEnvAsInt("RATE_LIMIT_ACTIVITY_PER_MINUTE", 10),

		// GPT
		GPTMaxConcurrentRequests: getEnvAsInt("GPT_MAX_CONCURRENT_REQUESTS", 5),
		GPTRequestTimeoutSeconds: getEnvAsInt("GPT_REQUEST_TIMEOUT_SECONDS", 90),
		GPTDailyBudgetUSD:        getEnvAsFloat("GPT_DAILY_BUDGET_USD", 0.034),

		// Development
		DebugMode:  getEnvAsBool("DEBUG_MODE", false),
		MockGPTAPI: getEnvAsBool("MOCK_GPT_API", false),
	}

	// Validate configuration
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("configuration validation failed: %w", err)
	}

	return cfg, nil
}

// Validate performs comprehensive configuration validation.
// Checks all required fields, numeric ranges, and enum values.
// Returns nil if configuration is valid, or a formatted error
// listing all validation failures if invalid.
//
// This method is called automatically by Load() and does not
// need to be called manually.
func (c *Config) Validate() error {
	var errors []string

	// Required Discord fields
	if c.DiscordToken == "" {
		errors = append(errors, "DISCORD_TOKEN is required")
	}
	if c.DiscordGuildID == "" {
		errors = append(errors, "DISCORD_GUILD_ID is required")
	}

	// Required OpenAI fields (unless mock mode)
	if !c.MockGPTAPI {
		if c.OpenAIAPIKey == "" {
			errors = append(errors, "OPENAI_API_KEY is required (or set MOCK_GPT_API=true)")
		}
	}

	// Required Database fields
	if c.DBPassword == "" {
		errors = append(errors, "DB_PASSWORD is required")
	}
	if c.DBName == "" {
		errors = append(errors, "DB_NAME is required")
	}
	if c.DBHost == "" {
		errors = append(errors, "DB_HOST is required")
	}
	if c.DBUser == "" {
		errors = append(errors, "DB_USER is required")
	}

	// Validate numeric ranges
	if c.GPTMaxTokens < 100 || c.GPTMaxTokens > 32000 {
		errors = append(errors, "GPT_MAX_TOKENS must be between 100 and 32000")
	}
	if c.RateLimitExportPerHour < 1 || c.RateLimitExportPerHour > 100 {
		errors = append(errors, "RATE_LIMIT_EXPORT_PER_HOUR must be between 1 and 100")
	}
	if c.RateLimitActivityPerMinute < 1 || c.RateLimitActivityPerMinute > 1000 {
		errors = append(errors, "RATE_LIMIT_ACTIVITY_PER_MINUTE must be between 1 and 1000")
	}
	if c.GPTMaxConcurrentRequests < 1 || c.GPTMaxConcurrentRequests > 50 {
		errors = append(errors, "GPT_MAX_CONCURRENT_REQUESTS must be between 1 and 50")
	}
	if c.GPTRequestTimeoutSeconds < 10 || c.GPTRequestTimeoutSeconds > 300 {
		errors = append(errors, "GPT_REQUEST_TIMEOUT_SECONDS must be between 10 and 300")
	}
	if c.GPTDailyBudgetUSD < 0.1 || c.GPTDailyBudgetUSD > 1000 {
		errors = append(errors, "GPT_DAILY_BUDGET_USD must be between 0.1 and 1000")
	}

	// Validate port number
	if portNum := getEnvAsInt("PORT", 8080); portNum < 1024 || portNum > 65535 {
		errors = append(errors, "PORT must be between 1024 and 65535")
	}

	// Validate database port
	if dbPort := getEnvAsInt("DB_PORT", 3306); dbPort < 1 || dbPort > 65535 {
		errors = append(errors, "DB_PORT must be between 1 and 65535")
	}

	// Validate environment
	validEnvs := map[string]bool{"development": true, "staging": true, "production": true}
	if !validEnvs[c.AppEnv] {
		errors = append(errors, "APP_ENV must be one of: development, staging, production")
	}

	// Validate log level
	validLogLevels := map[string]bool{"debug": true, "info": true, "warn": true, "error": true}
	if !validLogLevels[c.LogLevel] {
		errors = append(errors, "LOG_LEVEL must be one of: debug, info, warn, error")
	}

	// Return all validation errors
	if len(errors) > 0 {
		errorMsg := "Configuration validation errors:\n"
		for i, err := range errors {
			errorMsg += fmt.Sprintf("  %d. %s\n", i+1, err)
		}
		return fmt.Errorf(errorMsg)
	}

	return nil
}

// GetDSN returns the MySQL Data Source Name (connection string).
// Format: user:password@tcp(host:port)/database?params
//
// Example output:
// "superbot:password@tcp(localhost:3306)/superbot_dev?charset=utf8mb4&parseTime=True&loc=Local"
func (c *Config) GetDSN() string {
	return fmt.Sprintf("%s:%s@tcp(%s:%s)/%s?charset=utf8mb4&parseTime=True&loc=Local",
		c.DBUser, c.DBPassword, c.DBHost, c.DBPort, c.DBName)
}

// Helper functions
func getEnv(key string, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func getEnvAsInt(key string, defaultValue int) int {
	valueStr := getEnv(key, "")
	if value, err := strconv.Atoi(valueStr); err == nil {
		return value
	}
	return defaultValue
}

func getEnvAsFloat(key string, defaultValue float64) float64 {
	valueStr := getEnv(key, "")
	if value, err := strconv.ParseFloat(valueStr, 64); err == nil {
		return value
	}
	return defaultValue
}

func getEnvAsBool(key string, defaultValue bool) bool {
	valueStr := getEnv(key, "")
	if value, err := strconv.ParseBool(valueStr); err == nil {
		return value
	}
	return defaultValue
}
