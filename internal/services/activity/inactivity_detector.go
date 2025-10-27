package activity

import (
	"context"
	"fmt"
	"time"

	"github.com/rs/zerolog/log"
	"gorm.io/gorm"

	"github.com/phuc16/VCG-super-bot/internal/models"
)

// InactiveUser represents a user who has been inactive
type InactiveUser struct {
	UserID         string
	Username       string
	LastActivityAt time.Time
	DaysInactive   int
	AvatarURL      string
}

// InactivityDetector identifies inactive users
type InactivityDetector struct {
	db *gorm.DB
}

// NewInactivityDetector creates a new inactivity detector
func NewInactivityDetector(db *gorm.DB) *InactivityDetector {
	return &InactivityDetector{db: db}
}

// DetectInactiveUsers finds users who have been inactive for the specified number of days
func (d *InactivityDetector) DetectInactiveUsers(ctx context.Context, guildID string, thresholdDays int) ([]InactiveUser, error) {
	// Validate threshold
	if thresholdDays < 7 || thresholdDays > 90 {
		return nil, fmt.Errorf("threshold must be between 7 and 90 days, got %d", thresholdDays)
	}

	// Calculate cutoff date
	cutoffDate := time.Now().AddDate(0, 0, -thresholdDays)

	log.Debug().
		Str("guild_id", guildID).
		Int("threshold_days", thresholdDays).
		Time("cutoff_date", cutoffDate).
		Msg("Detecting inactive users")

	// Query users with last activity before cutoff
	var activities []models.UserActivity
	err := d.db.WithContext(ctx).
		Where("guild_id = ? AND is_bot = ?", guildID, false).
		Where("last_activity_at IS NOT NULL AND last_activity_at < ?", cutoffDate).
		Order("last_activity_at ASC"). // Oldest inactive first
		Find(&activities).Error

	if err != nil {
		return nil, fmt.Errorf("failed to query inactive users: %w", err)
	}

	// Convert to InactiveUser format
	inactiveUsers := make([]InactiveUser, 0, len(activities))
	now := time.Now()

	for _, activity := range activities {
		if activity.LastActivityAt == nil {
			continue // Skip users with no activity
		}

		daysInactive := int(now.Sub(*activity.LastActivityAt).Hours() / 24)

		inactiveUsers = append(inactiveUsers, InactiveUser{
			UserID:         activity.UserID,
			Username:       activity.Username,
			LastActivityAt: *activity.LastActivityAt,
			DaysInactive:   daysInactive,
			AvatarURL:      activity.AvatarURL,
		})
	}

	log.Info().
		Str("guild_id", guildID).
		Int("threshold_days", thresholdDays).
		Int("inactive_count", len(inactiveUsers)).
		Msg("Inactive users detected")

	return inactiveUsers, nil
}

// GetUserInactivityStatus checks if a specific user is inactive
func (d *InactivityDetector) GetUserInactivityStatus(ctx context.Context, guildID string, userID string, thresholdDays int) (*InactiveUser, error) {
	var activity models.UserActivity

	err := d.db.WithContext(ctx).
		Where("guild_id = ? AND user_id = ?", guildID, userID).
		First(&activity).Error

	if err == gorm.ErrRecordNotFound {
		return nil, nil // User has no activity record
	}
	if err != nil {
		return nil, fmt.Errorf("failed to query user activity: %w", err)
	}

	// Check if user is inactive
	if activity.LastActivityAt == nil {
		return nil, nil // User has no activity
	}

	cutoffDate := time.Now().AddDate(0, 0, -thresholdDays)
	if activity.LastActivityAt.After(cutoffDate) {
		return nil, nil // User is active
	}

	// User is inactive
	daysInactive := int(time.Since(*activity.LastActivityAt).Hours() / 24)

	return &InactiveUser{
		UserID:         activity.UserID,
		Username:       activity.Username,
		LastActivityAt: *activity.LastActivityAt,
		DaysInactive:   daysInactive,
		AvatarURL:      activity.AvatarURL,
	}, nil
}

// GetInactivityStats returns statistics about inactive users
func (d *InactivityDetector) GetInactivityStats(ctx context.Context, guildID string, thresholdDays int) (map[string]interface{}, error) {
	cutoffDate := time.Now().AddDate(0, 0, -thresholdDays)

	// Count total users
	var totalUsers int64
	err := d.db.WithContext(ctx).
		Model(&models.UserActivity{}).
		Where("guild_id = ? AND is_bot = ?", guildID, false).
		Count(&totalUsers).Error

	if err != nil {
		return nil, err
	}

	// Count inactive users
	var inactiveUsers int64
	err = d.db.WithContext(ctx).
		Model(&models.UserActivity{}).
		Where("guild_id = ? AND is_bot = ?", guildID, false).
		Where("last_activity_at IS NOT NULL AND last_activity_at < ?", cutoffDate).
		Count(&inactiveUsers).Error

	if err != nil {
		return nil, err
	}

	// Count active users
	activeUsers := totalUsers - inactiveUsers

	// Calculate percentage
	var inactivePercentage float64
	if totalUsers > 0 {
		inactivePercentage = float64(inactiveUsers) / float64(totalUsers) * 100
	}

	return map[string]interface{}{
		"total_users":         totalUsers,
		"active_users":        activeUsers,
		"inactive_users":      inactiveUsers,
		"inactive_percentage": inactivePercentage,
		"threshold_days":      thresholdDays,
	}, nil
}

// ValidateThreshold validates the inactivity threshold
func ValidateThreshold(thresholdDays int) error {
	if thresholdDays < 7 {
		return fmt.Errorf("threshold must be at least 7 days")
	}
	if thresholdDays > 90 {
		return fmt.Errorf("threshold must be at most 90 days")
	}
	return nil
}
