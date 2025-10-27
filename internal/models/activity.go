package models

import (
	"time"

	"github.com/google/uuid"
)

// UserActivity tracks a user's activity across messages and voice channels
type UserActivity struct {
	ID        uuid.UUID  `gorm:"type:char(36);primaryKey" json:"id"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
	DeletedAt *time.Time `gorm:"index" json:"deleted_at,omitempty"`

	// User identification
	GuildID  string `gorm:"type:varchar(100);not null;index:idx_user_activity_guild_user,unique" json:"guild_id"`
	UserID   string `gorm:"type:varchar(100);not null;index:idx_user_activity_guild_user,unique" json:"user_id"`
	Username string `gorm:"type:varchar(255);not null" json:"username"`

	// Activity tracking
	TotalMessages           int        `gorm:"default:0;not null" json:"total_messages"`
	CurrentWeekMessages     int        `gorm:"default:0;not null" json:"current_week_messages"`
	PreviousWeekMessages    int        `gorm:"default:0;not null" json:"previous_week_messages"`
	LastMessageAt           *time.Time `json:"last_message_at,omitempty"`
	LastVoiceJoinAt         *time.Time `json:"last_voice_join_at,omitempty"`
	LastActivityAt          *time.Time `gorm:"index" json:"last_activity_at,omitempty"`
	TotalVoiceMinutes       int        `gorm:"default:0;not null" json:"total_voice_minutes"`
	CurrentWeekVoiceMinutes int        `gorm:"default:0;not null" json:"current_week_voice_minutes"`

	// Role tracking
	HasActiveRole       bool       `gorm:"default:false;not null" json:"has_active_role"`
	ActiveRoleGrantedAt *time.Time `json:"active_role_granted_at,omitempty"`

	// Metadata
	IsBot          bool   `gorm:"default:false;not null" json:"is_bot"`
	AvatarURL      string `gorm:"type:varchar(500)" json:"avatar_url,omitempty"`
	CurrentWeekNum int    `gorm:"not null" json:"current_week_num"` // ISO week number for rolling over
}

// BeforeCreate sets the ID before creating the record
func (ua *UserActivity) BeforeCreate() error {
	if ua.ID == uuid.Nil {
		ua.ID = uuid.New()
	}
	return nil
}

// TableName specifies the table name for UserActivity
func (UserActivity) TableName() string {
	return "user_activity_records"
}

// UpdateActivity updates the activity timestamps and counts
func (ua *UserActivity) UpdateActivity(messageCount int, voiceMinutes int) {
	now := time.Now()
	ua.LastActivityAt = &now
	ua.UpdatedAt = now

	if messageCount > 0 {
		ua.TotalMessages += messageCount
		ua.CurrentWeekMessages += messageCount
		ua.LastMessageAt = &now
	}

	if voiceMinutes > 0 {
		ua.TotalVoiceMinutes += voiceMinutes
		ua.CurrentWeekVoiceMinutes += voiceMinutes
		ua.LastVoiceJoinAt = &now
	}
}

// RolloverWeek moves current week stats to previous week and resets current week
func (ua *UserActivity) RolloverWeek(newWeekNum int) {
	ua.PreviousWeekMessages = ua.CurrentWeekMessages
	ua.CurrentWeekMessages = 0
	ua.CurrentWeekVoiceMinutes = 0
	ua.CurrentWeekNum = newWeekNum
}

// GrantActiveRole marks the user as having the active role
func (ua *UserActivity) GrantActiveRole() {
	now := time.Now()
	ua.HasActiveRole = true
	ua.ActiveRoleGrantedAt = &now
}

// RevokeActiveRole removes the active role from the user
func (ua *UserActivity) RevokeActiveRole() {
	ua.HasActiveRole = false
	ua.ActiveRoleGrantedAt = nil
}
