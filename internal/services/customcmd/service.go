package customcmd

import (
	"context"
	"errors"
	"strings"

	"gorm.io/gorm"

	"github.com/phuc16/VCG-super-bot/internal/models"
)

// ErrNotFound is returned when a custom command does not exist
var ErrNotFound = errors.New("custom command not found")

// ErrAlreadyExists is returned when a trigger already exists for the guild
var ErrAlreadyExists = errors.New("custom command already exists")

// Service manages custom keyword commands
type Service struct {
	db *gorm.DB
}

// NewService creates a new Service
func NewService(db *gorm.DB) *Service {
	return &Service{db: db}
}

// Add creates a new custom command. Returns ErrAlreadyExists if the trigger already exists.
func (s *Service) Add(ctx context.Context, guildID, trigger, response, createdBy string) error {
	normalizedTrigger := strings.ToLower(strings.TrimSpace(trigger))

	// Check if trigger already exists
	var count int64
	if err := s.db.WithContext(ctx).Model(&models.CustomCommand{}).
		Where("guild_id = ? AND `trigger` = ? AND deleted_at IS NULL", guildID, normalizedTrigger).
		Count(&count).Error; err != nil {
		return err
	}
	if count > 0 {
		return ErrAlreadyExists
	}

	cmd := &models.CustomCommand{
		GuildID:   guildID,
		Trigger:   normalizedTrigger,
		Response:  response,
		CreatedBy: createdBy,
	}
	return s.db.WithContext(ctx).Create(cmd).Error
}

// Delete removes a custom command by trigger. Returns ErrNotFound if it doesn't exist.
func (s *Service) Delete(ctx context.Context, guildID, trigger string) error {
	normalizedTrigger := strings.ToLower(strings.TrimSpace(trigger))

	result := s.db.WithContext(ctx).Unscoped().
		Where("guild_id = ? AND `trigger` = ? AND deleted_at IS NULL", guildID, normalizedTrigger).
		Delete(&models.CustomCommand{})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

// List returns all custom commands for a guild
func (s *Service) List(ctx context.Context, guildID string) ([]models.CustomCommand, error) {
	var cmds []models.CustomCommand
	err := s.db.WithContext(ctx).
		Where("guild_id = ? AND deleted_at IS NULL", guildID).
		Order("`trigger` ASC").
		Find(&cmds).Error
	return cmds, err
}

// GetResponse returns the response for a trigger, or empty string if not found
func (s *Service) GetResponse(ctx context.Context, guildID, trigger string) (string, bool, error) {
	normalizedTrigger := strings.ToLower(strings.TrimSpace(trigger))

	var cmd models.CustomCommand
	err := s.db.WithContext(ctx).
		Where("guild_id = ? AND `trigger` = ? AND deleted_at IS NULL", guildID, normalizedTrigger).
		First(&cmd).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return cmd.Response, true, nil
}
