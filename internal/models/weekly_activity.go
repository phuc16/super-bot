package models

import (
	"time"

	"github.com/google/uuid"
)

// WeeklyActivity stores archived weekly activity data for historical reporting
type WeeklyActivity struct {
	ID        uuid.UUID  `gorm:"type:char(36);primaryKey" json:"id"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
	DeletedAt *time.Time `gorm:"index" json:"deleted_at,omitempty"`

	// Week identification
	GuildID   string    `gorm:"type:varchar(100);not null;index:idx_weekly_activity_guild_week" json:"guild_id"`
	Year      int       `gorm:"not null;index:idx_weekly_activity_guild_week" json:"year"`
	WeekNum   int       `gorm:"not null;index:idx_weekly_activity_guild_week" json:"week_num"` // ISO week number (1-53)
	WeekStart time.Time `gorm:"not null" json:"week_start"`
	WeekEnd   time.Time `gorm:"not null" json:"week_end"`

	// User identification
	UserID   string `gorm:"type:varchar(100);not null;index" json:"user_id"`
	Username string `gorm:"type:varchar(255);not null" json:"username"`

	// Activity metrics
	MessageCount int  `gorm:"not null" json:"message_count"`
	VoiceMinutes int  `gorm:"default:0;not null" json:"voice_minutes"`
	Rank         int  `gorm:"not null" json:"rank"` // 1-based rank for that week
	ReceivedRole bool `gorm:"default:false;not null" json:"received_role"`

	// Metadata
	AvatarURL string `gorm:"type:varchar(500)" json:"avatar_url,omitempty"`
}

// BeforeCreate sets the ID before creating the record
func (wa *WeeklyActivity) BeforeCreate() error {
	if wa.ID == uuid.Nil {
		wa.ID = uuid.New()
	}
	return nil
}

// TableName specifies the table name for WeeklyActivity
func (WeeklyActivity) TableName() string {
	return "weekly_activities"
}

// GetISOWeek returns the ISO week year and week number for a given time
func GetISOWeek(t time.Time) (year, week int) {
	return t.ISOWeek()
}

// GetWeekBounds returns the start and end times for a given ISO week
func GetWeekBounds(year, week int) (start, end time.Time) {
	// Start with January 4th of the year (always in week 1)
	jan4 := time.Date(year, time.January, 4, 0, 0, 0, 0, time.UTC)

	// Find the Monday of week 1
	daysFromMonday := int(jan4.Weekday()) - int(time.Monday)
	if daysFromMonday < 0 {
		daysFromMonday += 7
	}
	week1Monday := jan4.AddDate(0, 0, -daysFromMonday)

	// Add weeks to get to the target week
	start = week1Monday.AddDate(0, 0, (week-1)*7)
	end = start.AddDate(0, 0, 7).Add(-time.Second) // End is 23:59:59 of Sunday

	return start, end
}
