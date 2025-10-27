package activity

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/phuc16/VCG-super-bot/internal/models"
)

// Processor handles batch processing of activity events
type Processor struct {
	db *gorm.DB
}

// NewProcessor creates a new activity processor
func NewProcessor(db *gorm.DB) *Processor {
	return &Processor{db: db}
}

// ProcessBatch processes a batch of activity events
func (p *Processor) ProcessBatch(ctx context.Context, events []ActivityEvent) error {
	if len(events) == 0 {
		return nil
	}

	// Group events by user
	userEvents := make(map[string][]ActivityEvent)
	for _, event := range events {
		key := event.GuildID + ":" + event.UserID
		userEvents[key] = append(userEvents[key], event)
	}

	// Process each user's events
	for key, userEventList := range userEvents {
		if err := p.processUserEvents(ctx, userEventList); err != nil {
			log.Error().
				Err(err).
				Str("user_key", key).
				Int("event_count", len(userEventList)).
				Msg("Failed to process user events")
			// Continue processing other users
			continue
		}
	}

	return nil
}

// processUserEvents processes all events for a single user
func (p *Processor) processUserEvents(ctx context.Context, events []ActivityEvent) error {
	if len(events) == 0 {
		return nil
	}

	// Get first event for user info
	firstEvent := events[0]

	// Get current ISO week
	_, week := time.Now().ISOWeek()

	// Count messages and voice events
	messageCount := 0
	hasVoiceActivity := false
	latestTimestamp := events[0].Timestamp

	for _, event := range events {
		switch event.Type {
		case EventTypeMessage:
			messageCount++
		case EventTypeVoiceJoin:
			hasVoiceActivity = true
		}

		if event.Timestamp.After(latestTimestamp) {
			latestTimestamp = event.Timestamp
		}
	}

	// Create or update user activity record
	var userActivity models.UserActivity

	err := p.db.WithContext(ctx).Where(
		"guild_id = ? AND user_id = ?",
		firstEvent.GuildID,
		firstEvent.UserID,
	).First(&userActivity).Error

	if err == gorm.ErrRecordNotFound {
		// Create new record
		userActivity = models.UserActivity{
			ID:             uuid.New(),
			GuildID:        firstEvent.GuildID,
			UserID:         firstEvent.UserID,
			Username:       firstEvent.Username,
			AvatarURL:      firstEvent.AvatarURL,
			CurrentWeekNum: week,
			IsBot:          false,
		}
	} else if err != nil {
		return err
	}

	// Check if week has rolled over
	if userActivity.CurrentWeekNum != week {
		userActivity.RolloverWeek(week)
	}

	// Update activity
	now := time.Now()
	userActivity.LastActivityAt = &now

	if messageCount > 0 {
		userActivity.TotalMessages += messageCount
		userActivity.CurrentWeekMessages += messageCount
		userActivity.LastMessageAt = &latestTimestamp
	}

	if hasVoiceActivity {
		userActivity.LastVoiceJoinAt = &latestTimestamp
	}

	// Update username and avatar if changed
	userActivity.Username = firstEvent.Username
	if firstEvent.AvatarURL != "" {
		userActivity.AvatarURL = firstEvent.AvatarURL
	}

	// Upsert the record
	err = p.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "guild_id"}, {Name: "user_id"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"username",
			"avatar_url",
			"total_messages",
			"current_week_messages",
			"last_message_at",
			"last_voice_join_at",
			"last_activity_at",
			"current_week_num",
			"updated_at",
		}),
	}).Create(&userActivity).Error

	if err != nil {
		return err
	}

	log.Debug().
		Str("guild_id", firstEvent.GuildID).
		Str("user_id", firstEvent.UserID).
		Int("messages", messageCount).
		Bool("voice_activity", hasVoiceActivity).
		Msg("User activity updated")

	return nil
}
