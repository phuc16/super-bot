package models

import "time"

// AITopic represents a topic for scheduled AI content generation
type AITopic struct {
	BaseModel
	GuildID         string     `gorm:"column:guild_id;type:varchar(20);not null;index" json:"guild_id"`
	Topic           string     `gorm:"column:topic;type:text;not null" json:"topic"`
	ChannelID       string     `gorm:"column:channel_id;type:varchar(20);not null" json:"channel_id"`
	MentionUserID   *string    `gorm:"column:mention_user_id;type:varchar(20)" json:"mention_user_id,omitempty"`
	CreatedBy       string     `gorm:"column:created_by;type:varchar(20);not null" json:"created_by"`
	IntervalMinutes int        `gorm:"column:interval_minutes;not null;default:5" json:"interval_minutes"`
	LastPostedAt    *time.Time `gorm:"column:last_posted_at" json:"last_posted_at,omitempty"`
}

// TableName specifies the table name for AITopic
func (AITopic) TableName() string {
	return "ai_topics"
}
