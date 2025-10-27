package roles

import (
	"context"
	"fmt"

	"github.com/rs/zerolog/log"
	"gorm.io/gorm"

	"github.com/phuc16/VCG-super-bot/internal/models"
)

// TopUser represents a user with their activity rank
type TopUser struct {
	UserID       string
	Username     string
	MessageCount int
	Rank         int
	AvatarURL    string
}

// Analyzer identifies top active users
type Analyzer struct {
	db *gorm.DB
}

// NewAnalyzer creates a new role analyzer
func NewAnalyzer(db *gorm.DB) *Analyzer {
	return &Analyzer{db: db}
}

// GetTopActiveUsers identifies the top N users by message count, including ties
func (a *Analyzer) GetTopActiveUsers(ctx context.Context, guildID string, topN int) ([]TopUser, error) {
	// Query users sorted by current week messages
	var activities []models.UserActivity

	err := a.db.WithContext(ctx).
		Where("guild_id = ? AND is_bot = ?", guildID, false).
		Where("current_week_messages > 0").
		Order("current_week_messages DESC, username ASC").
		Find(&activities).Error

	if err != nil {
		return nil, fmt.Errorf("failed to query user activities: %w", err)
	}

	if len(activities) == 0 {
		log.Info().Str("guild_id", guildID).Msg("No active users found for the week")
		return []TopUser{}, nil
	}

	// Apply ranking with tie handling
	topUsers := make([]TopUser, 0, topN+10) // Extra space for ties
	currentRank := 1
	lastMessageCount := -1
	usersAtRank := 0

	for _, activity := range activities {
		// Assign rank (ties get the same rank)
		if activity.CurrentWeekMessages != lastMessageCount {
			currentRank += usersAtRank
			usersAtRank = 0
			lastMessageCount = activity.CurrentWeekMessages
		}
		usersAtRank++

		topUser := TopUser{
			UserID:       activity.UserID,
			Username:     activity.Username,
			MessageCount: activity.CurrentWeekMessages,
			Rank:         currentRank,
			AvatarURL:    activity.AvatarURL,
		}

		// Include if within top N OR tied with the Nth position
		if currentRank <= topN {
			topUsers = append(topUsers, topUser)
		} else if len(topUsers) > 0 && topUsers[len(topUsers)-1].Rank == currentRank {
			// Include tied users
			topUsers = append(topUsers, topUser)
		} else {
			// Past top N and not tied, stop
			break
		}
	}

	log.Info().
		Str("guild_id", guildID).
		Int("top_n", topN).
		Int("selected_users", len(topUsers)).
		Msg("Top active users identified")

	return topUsers, nil
}

// GetUserRank gets the rank of a specific user
func (a *Analyzer) GetUserRank(ctx context.Context, guildID string, userID string) (rank int, messageCount int, err error) {
	var activity models.UserActivity

	err = a.db.WithContext(ctx).
		Where("guild_id = ? AND user_id = ?", guildID, userID).
		First(&activity).Error

	if err == gorm.ErrRecordNotFound {
		return 0, 0, nil // User has no activity
	}
	if err != nil {
		return 0, 0, err
	}

	// Count how many users have more messages
	var higherCount int64
	err = a.db.WithContext(ctx).
		Model(&models.UserActivity{}).
		Where("guild_id = ? AND is_bot = ? AND current_week_messages > ?",
			guildID, false, activity.CurrentWeekMessages).
		Count(&higherCount).Error

	if err != nil {
		return 0, 0, err
	}

	rank = int(higherCount) + 1
	messageCount = activity.CurrentWeekMessages

	return rank, messageCount, nil
}

// GetTotalActiveUsers returns the count of users with activity this week
func (a *Analyzer) GetTotalActiveUsers(ctx context.Context, guildID string) (int64, error) {
	var count int64

	err := a.db.WithContext(ctx).
		Model(&models.UserActivity{}).
		Where("guild_id = ? AND is_bot = ? AND current_week_messages > 0", guildID, false).
		Count(&count).Error

	if err != nil {
		return 0, err
	}

	return count, nil
}

// GetServerStats returns aggregated server statistics
func (a *Analyzer) GetServerStats(ctx context.Context, guildID string) (map[string]interface{}, error) {
	type Stats struct {
		TotalUsers        int64
		ActiveUsers       int64
		TotalMessages     int64
		TotalWeekMessages int64
	}

	var stats Stats

	// Total users
	err := a.db.WithContext(ctx).
		Model(&models.UserActivity{}).
		Where("guild_id = ? AND is_bot = ?", guildID, false).
		Count(&stats.TotalUsers).Error

	if err != nil {
		return nil, err
	}

	// Active users (this week)
	err = a.db.WithContext(ctx).
		Model(&models.UserActivity{}).
		Where("guild_id = ? AND is_bot = ? AND current_week_messages > 0", guildID, false).
		Count(&stats.ActiveUsers).Error

	if err != nil {
		return nil, err
	}

	// Total messages (all time)
	err = a.db.WithContext(ctx).
		Model(&models.UserActivity{}).
		Where("guild_id = ? AND is_bot = ?", guildID, false).
		Select("COALESCE(SUM(total_messages), 0)").
		Scan(&stats.TotalMessages).Error

	if err != nil {
		return nil, err
	}

	// Total messages this week
	err = a.db.WithContext(ctx).
		Model(&models.UserActivity{}).
		Where("guild_id = ? AND is_bot = ?", guildID, false).
		Select("COALESCE(SUM(current_week_messages), 0)").
		Scan(&stats.TotalWeekMessages).Error

	if err != nil {
		return nil, err
	}

	return map[string]interface{}{
		"total_users":         stats.TotalUsers,
		"active_users":        stats.ActiveUsers,
		"total_messages":      stats.TotalMessages,
		"total_week_messages": stats.TotalWeekMessages,
	}, nil
}

// GetTopActiveUsersAllTime returns the top users by total messages (all-time)
func (a *Analyzer) GetTopActiveUsersAllTime(ctx context.Context, guildID string, topN int) ([]TopUser, error) {
	var activities []models.UserActivity

	err := a.db.WithContext(ctx).
		Where("guild_id = ? AND is_bot = ?", guildID, false).
		Where("total_messages > 0").
		Order("total_messages DESC, username ASC").
		Find(&activities).Error

	if err != nil {
		return nil, fmt.Errorf("failed to query user activities: %w", err)
	}

	if len(activities) == 0 {
		return []TopUser{}, nil
	}

	topUsers := make([]TopUser, 0, topN+10)
	currentRank := 1
	lastMessageCount := -1
	usersAtRank := 0

	for _, activity := range activities {
		if activity.TotalMessages != lastMessageCount {
			currentRank += usersAtRank
			usersAtRank = 0
			lastMessageCount = activity.TotalMessages
		}
		usersAtRank++

		topUser := TopUser{
			UserID:       activity.UserID,
			Username:     activity.Username,
			MessageCount: activity.TotalMessages,
			Rank:         currentRank,
			AvatarURL:    activity.AvatarURL,
		}

		if currentRank <= topN {
			topUsers = append(topUsers, topUser)
		} else if len(topUsers) > 0 && topUsers[len(topUsers)-1].Rank == currentRank {
			topUsers = append(topUsers, topUser)
		} else {
			break
		}
	}

	return topUsers, nil
}

// GetUserRankAllTime gets the all-time rank of a specific user
func (a *Analyzer) GetUserRankAllTime(ctx context.Context, guildID string, userID string) (rank int, messageCount int, err error) {
	var activity models.UserActivity

	err = a.db.WithContext(ctx).
		Where("guild_id = ? AND user_id = ?", guildID, userID).
		First(&activity).Error

	if err == gorm.ErrRecordNotFound {
		return 0, 0, nil
	}
	if err != nil {
		return 0, 0, err
	}

	var higherCount int64
	err = a.db.WithContext(ctx).
		Model(&models.UserActivity{}).
		Where("guild_id = ? AND is_bot = ? AND total_messages > ?",
			guildID, false, activity.TotalMessages).
		Count(&higherCount).Error

	if err != nil {
		return 0, 0, err
	}

	rank = int(higherCount) + 1
	messageCount = activity.TotalMessages

	return rank, messageCount, nil
}
