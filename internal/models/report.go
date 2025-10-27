package models

import (
	"database/sql/driver"
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// WeeklyReport stores generated weekly activity reports
type WeeklyReport struct {
	ID        uuid.UUID  `gorm:"type:char(36);primaryKey" json:"id"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
	DeletedAt *time.Time `gorm:"index" json:"deleted_at,omitempty"`

	// Report identification
	GuildID   string    `gorm:"type:varchar(100);not null;index:idx_weekly_reports_guild_week" json:"guild_id"`
	Year      int       `gorm:"not null;index:idx_weekly_reports_guild_week" json:"year"`
	WeekNum   int       `gorm:"not null;index:idx_weekly_reports_guild_week" json:"week_num"`
	WeekStart time.Time `gorm:"not null" json:"week_start"`
	WeekEnd   time.Time `gorm:"not null" json:"week_end"`

	// Report metadata
	TotalUsers      int       `gorm:"not null" json:"total_users"`
	ActiveUsers     int       `gorm:"not null" json:"active_users"`
	TotalMessages   int       `gorm:"not null" json:"total_messages"`
	GeneratedAt     time.Time `gorm:"not null" json:"generated_at"`
	GeneratedByUser string    `gorm:"type:varchar(100)" json:"generated_by_user,omitempty"` // Empty if automated

	// Report content (JSON)
	ReportContent JSONMap   `gorm:"type:json" json:"report_content"` // Full report data with rankings
	Top5Users     JSONArray `gorm:"type:json" json:"top_5_users"`    // Top 5 user IDs for quick access

	// Discord posting
	ChannelID       string     `gorm:"type:varchar(100)" json:"channel_id,omitempty"`
	PostedMessageID string     `gorm:"type:varchar(100);index" json:"posted_message_id,omitempty"`
	PostedAt        *time.Time `json:"posted_at,omitempty"`
}

// BeforeCreate sets the ID before creating the record
func (wr *WeeklyReport) BeforeCreate() error {
	if wr.ID == uuid.Nil {
		wr.ID = uuid.New()
	}
	if wr.GeneratedAt.IsZero() {
		wr.GeneratedAt = time.Now()
	}
	return nil
}

// TableName specifies the table name for WeeklyReport
func (WeeklyReport) TableName() string {
	return "weekly_reports"
}

// JSONMap is a custom type for JSON object storage
type JSONMap map[string]interface{}

// Scan implements the sql.Scanner interface
func (j *JSONMap) Scan(value interface{}) error {
	if value == nil {
		*j = nil
		return nil
	}

	bytes, ok := value.([]byte)
	if !ok {
		return nil
	}

	result := make(map[string]interface{})
	err := json.Unmarshal(bytes, &result)
	*j = result
	return err
}

// Value implements the driver.Valuer interface
func (j JSONMap) Value() (driver.Value, error) {
	if j == nil {
		return nil, nil
	}
	return json.Marshal(j)
}

// JSONArray is a custom type for JSON array storage
type JSONArray []interface{}

// Scan implements the sql.Scanner interface
func (j *JSONArray) Scan(value interface{}) error {
	if value == nil {
		*j = nil
		return nil
	}

	bytes, ok := value.([]byte)
	if !ok {
		return nil
	}

	result := make([]interface{}, 0)
	err := json.Unmarshal(bytes, &result)
	*j = result
	return err
}

// Value implements the driver.Valuer interface
func (j JSONArray) Value() (driver.Value, error) {
	if j == nil {
		return nil, nil
	}
	return json.Marshal(j)
}

// MarkAsPosted updates the report with Discord posting information
func (wr *WeeklyReport) MarkAsPosted(channelID string, messageID string) {
	now := time.Now()
	wr.ChannelID = channelID
	wr.PostedMessageID = messageID
	wr.PostedAt = &now
}
