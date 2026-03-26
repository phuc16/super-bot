package aitopic

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"

	"github.com/phuc16/VCG-super-bot/internal/models"
)

// ErrNoTopics is returned when there are no topics due for posting
var ErrNoTopics = errors.New("no topics due")

// Service manages AI content topics
type Service struct {
	db *gorm.DB
}

// NewService creates a new Service
func NewService(db *gorm.DB) *Service {
	return &Service{db: db}
}

// Add creates a new topic for the guild
func (s *Service) Add(ctx context.Context, guildID, topic, channelID, createdBy string, intervalMinutes int, mentionUserID string) error {
	t := &models.AITopic{
		GuildID:         guildID,
		Topic:           topic,
		ChannelID:       channelID,
		CreatedBy:       createdBy,
		IntervalMinutes: intervalMinutes,
	}
	if mentionUserID != "" {
		t.MentionUserID = &mentionUserID
	}
	return s.db.WithContext(ctx).Create(t).Error
}

// Delete removes a topic by its ID
func (s *Service) Delete(ctx context.Context, guildID, id string) error {
	result := s.db.WithContext(ctx).Unscoped().
		Where("guild_id = ? AND id = ?", guildID, id).
		Delete(&models.AITopic{})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return errors.New("topic not found")
	}
	return nil
}

// List returns all topics for a guild
func (s *Service) List(ctx context.Context, guildID string) ([]models.AITopic, error) {
	var topics []models.AITopic
	err := s.db.WithContext(ctx).
		Where("guild_id = ? AND deleted_at IS NULL", guildID).
		Order("created_at ASC").
		Find(&topics).Error
	return topics, err
}

// GetDueTopics returns all topics that are due to post (never posted, or interval has elapsed)
func (s *Service) GetDueTopics(ctx context.Context, guildID string) ([]models.AITopic, error) {
	now := time.Now().UTC()

	var topics []models.AITopic
	err := s.db.WithContext(ctx).
		Where("guild_id = ? AND deleted_at IS NULL", guildID).
		Where("last_posted_at IS NULL OR last_posted_at <= ?",
			now.Add(-time.Minute)).// will filter per-topic below
		Find(&topics).Error
	if err != nil {
		return nil, err
	}

	// Filter: only those whose interval has elapsed
	var due []models.AITopic
	for _, t := range topics {
		if t.LastPostedAt == nil {
			due = append(due, t)
		} else if now.Sub(*t.LastPostedAt) >= time.Duration(t.IntervalMinutes)*time.Minute {
			due = append(due, t)
		}
	}
	return due, nil
}

// MarkPosted updates last_posted_at to the given scheduled time for a topic
func (s *Service) MarkPosted(ctx context.Context, id string, scheduledTime time.Time) error {
	return s.db.WithContext(ctx).
		Model(&models.AITopic{}).
		Where("id = ?", id).
		Update("last_posted_at", scheduledTime).Error
}
