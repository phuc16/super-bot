package config

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/robfig/cron/v3"
	"github.com/rs/zerolog/log"
	"gorm.io/gorm"

	"github.com/phuc16/VCG-super-bot/internal/models"
)

// Service handles server configuration management
type Service struct {
	db    *gorm.DB
	cache map[string]*models.ServerConfig
	mu    sync.RWMutex
}

// NewService creates a new configuration service
func NewService(db *gorm.DB) *Service {
	return &Service{
		db:    db,
		cache: make(map[string]*models.ServerConfig),
	}
}

// Get retrieves the server configuration for a guild
func (s *Service) Get(ctx context.Context, guildID string) (*models.ServerConfig, error) {
	// Check cache first
	s.mu.RLock()
	if config, exists := s.cache[guildID]; exists {
		s.mu.RUnlock()
		return config, nil
	}
	s.mu.RUnlock()

	// Not in cache, fetch from database
	var config models.ServerConfig
	err := s.db.WithContext(ctx).
		Where("guild_id = ?", guildID).
		First(&config).Error

	if err != nil {
		if err == gorm.ErrRecordNotFound {
			// Create default config for new guild
			return s.createDefault(ctx, guildID)
		}
		return nil, fmt.Errorf("failed to fetch config: %w", err)
	}

	// Update cache
	s.mu.Lock()
	s.cache[guildID] = &config
	s.mu.Unlock()

	return &config, nil
}

// createDefault creates a default configuration for a new guild
func (s *Service) createDefault(ctx context.Context, guildID string) (*models.ServerConfig, error) {
	config := &models.ServerConfig{
		GuildID:                 guildID,
		ActiveRoleName:          "Active of the Week",
		InactivityThresholdDays: 30,
		WeeklyScheduleCron:      "0 0 * * 1", // Monday 00:00
		Timezone:                "UTC",
		EnabledFeatures:         models.DefaultEnabledFeatures(),
	}

	if err := s.db.WithContext(ctx).Create(config).Error; err != nil {
		return nil, fmt.Errorf("failed to create default config: %w", err)
	}

	// Update cache
	s.mu.Lock()
	s.cache[guildID] = config
	s.mu.Unlock()

	log.Info().
		Str("guild_id", guildID).
		Msg("Created default server configuration")

	return config, nil
}

// SetReportsChannel sets the reports channel ID
func (s *Service) SetReportsChannel(ctx context.Context, guildID, channelID string) error {
	config, err := s.Get(ctx, guildID)
	if err != nil {
		return err
	}

	config.ReportsChannelID = &channelID

	if err := s.db.WithContext(ctx).Save(config).Error; err != nil {
		return fmt.Errorf("failed to update reports channel: %w", err)
	}

	// Invalidate cache
	s.invalidateCache(guildID)

	log.Info().
		Str("guild_id", guildID).
		Str("channel_id", channelID).
		Msg("Updated reports channel")

	return nil
}

// SetActiveRoleName sets the active role name
func (s *Service) SetActiveRoleName(ctx context.Context, guildID, roleName string) error {
	if err := validateRoleName(roleName); err != nil {
		return err
	}

	config, err := s.Get(ctx, guildID)
	if err != nil {
		return err
	}

	config.ActiveRoleName = roleName
	// Clear role ID when name changes - it will be recreated
	config.ActiveRoleID = nil

	if err := s.db.WithContext(ctx).Save(config).Error; err != nil {
		return fmt.Errorf("failed to update active role name: %w", err)
	}

	// Invalidate cache
	s.invalidateCache(guildID)

	log.Info().
		Str("guild_id", guildID).
		Str("role_name", roleName).
		Msg("Updated active role name")

	return nil
}

// SetInactivityThreshold sets the inactivity threshold in days
func (s *Service) SetInactivityThreshold(ctx context.Context, guildID string, days int) error {
	if err := validateInactivityThreshold(days); err != nil {
		return err
	}

	config, err := s.Get(ctx, guildID)
	if err != nil {
		return err
	}

	config.InactivityThresholdDays = days

	if err := s.db.WithContext(ctx).Save(config).Error; err != nil {
		return fmt.Errorf("failed to update inactivity threshold: %w", err)
	}

	// Invalidate cache
	s.invalidateCache(guildID)

	log.Info().
		Str("guild_id", guildID).
		Int("days", days).
		Msg("Updated inactivity threshold")

	return nil
}

// SetWeeklySchedule sets the weekly schedule cron expression and timezone
func (s *Service) SetWeeklySchedule(ctx context.Context, guildID, cronExpr, timezone string) error {
	if err := validateCronExpression(cronExpr); err != nil {
		return err
	}

	if err := validateTimezone(timezone); err != nil {
		return err
	}

	config, err := s.Get(ctx, guildID)
	if err != nil {
		return err
	}

	config.WeeklyScheduleCron = cronExpr
	config.Timezone = timezone

	if err := s.db.WithContext(ctx).Save(config).Error; err != nil {
		return fmt.Errorf("failed to update weekly schedule: %w", err)
	}

	// Invalidate cache
	s.invalidateCache(guildID)

	log.Info().
		Str("guild_id", guildID).
		Str("cron", cronExpr).
		Str("timezone", timezone).
		Msg("Updated weekly schedule")

	return nil
}

// SetFeatures updates the enabled features
func (s *Service) SetFeatures(ctx context.Context, guildID string, features models.EnabledFeatures) error {
	config, err := s.Get(ctx, guildID)
	if err != nil {
		return err
	}

	config.EnabledFeatures = features

	if err := s.db.WithContext(ctx).Save(config).Error; err != nil {
		return fmt.Errorf("failed to update features: %w", err)
	}

	// Invalidate cache
	s.invalidateCache(guildID)

	log.Info().
		Str("guild_id", guildID).
		Interface("features", features).
		Msg("Updated enabled features")

	return nil
}

// UpdateActiveRoleID updates the cached role ID (used by role assigner)
func (s *Service) UpdateActiveRoleID(ctx context.Context, guildID, roleID string) error {
	config, err := s.Get(ctx, guildID)
	if err != nil {
		return err
	}

	config.ActiveRoleID = &roleID

	if err := s.db.WithContext(ctx).Save(config).Error; err != nil {
		return fmt.Errorf("failed to update active role ID: %w", err)
	}

	// Invalidate cache
	s.invalidateCache(guildID)

	return nil
}

// invalidateCache removes a guild's config from cache
func (s *Service) invalidateCache(guildID string) {
	s.mu.Lock()
	delete(s.cache, guildID)
	s.mu.Unlock()
}

// ClearCache clears all cached configurations
func (s *Service) ClearCache() {
	s.mu.Lock()
	s.cache = make(map[string]*models.ServerConfig)
	s.mu.Unlock()

	log.Debug().Msg("Configuration cache cleared")
}

// Validation functions

func validateRoleName(name string) error {
	if name == "" {
		return fmt.Errorf("role name cannot be empty")
	}
	if len(name) > 100 {
		return fmt.Errorf("role name must be 100 characters or less")
	}
	return nil
}

func validateInactivityThreshold(days int) error {
	if days < 7 || days > 90 {
		return fmt.Errorf("inactivity threshold must be between 7 and 90 days")
	}
	return nil
}

func validateCronExpression(expr string) error {
	parser := cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)
	_, err := parser.Parse(expr)
	if err != nil {
		return fmt.Errorf("invalid cron expression: %w", err)
	}
	return nil
}

func validateTimezone(tz string) error {
	_, err := time.LoadLocation(tz)
	if err != nil {
		return fmt.Errorf("invalid timezone: %w", err)
	}
	return nil
}
