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

// Add creates a new custom command. Returns ErrAlreadyExists if the trigger
// already exists.
//
// imageData/imageName are optional — pass nil/empty for none. The BYTES are
// stored, not a link: attachments passed as slash-command options live at a
// signed, time-limited Discord URL that expires within hours and cannot be
// refreshed, so anything storing the URL renders an empty box soon after.
func (s *Service) Add(ctx context.Context, guildID, trigger, response, createdBy string, imageData []byte, imageName string) error {
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
	if len(imageData) > 0 {
		cmd.ImageData = imageData
		if imageName != "" {
			cmd.ImageName = &imageName
		}
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

// listColumns is every column except image_data. Selecting explicitly
// matters: a bare Find would pull every stored image in the guild across
// the wire and into memory, on a listing that never displays images. Only
// Get — one row, for an actual keyword reply — needs the bytes.
var listColumns = []string{
	"id", "created_at", "updated_at", "deleted_at",
	"guild_id", "`trigger`", "response", "image_url", "image_name", "created_by",
}

// List returns all custom commands for a guild, WITHOUT their image bytes.
// Callers that need an image must fetch that command via Get.
func (s *Service) List(ctx context.Context, guildID string) ([]models.CustomCommand, error) {
	var cmds []models.CustomCommand
	err := s.db.WithContext(ctx).
		Select(listColumns).
		Where("guild_id = ? AND deleted_at IS NULL", guildID).
		Order("`trigger` ASC").
		Find(&cmds).Error
	return cmds, err
}

// Get returns the custom command for a trigger, or (nil, nil) if not found.
func (s *Service) Get(ctx context.Context, guildID, trigger string) (*models.CustomCommand, error) {
	normalizedTrigger := strings.ToLower(strings.TrimSpace(trigger))

	var cmd models.CustomCommand
	err := s.db.WithContext(ctx).
		Where("guild_id = ? AND `trigger` = ? AND deleted_at IS NULL", guildID, normalizedTrigger).
		First(&cmd).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &cmd, nil
}
