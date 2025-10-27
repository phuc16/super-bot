package models

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
)

// EnabledFeatures represents feature flags for the bot
type EnabledFeatures struct {
	Export   bool `json:"export"`
	Tracking bool `json:"tracking"`
	Reports  bool `json:"reports"`
	Roles    bool `json:"roles"`
}

// Scan implements sql.Scanner for EnabledFeatures
func (e *EnabledFeatures) Scan(value interface{}) error {
	bytes, ok := value.([]byte)
	if !ok {
		return fmt.Errorf("failed to scan EnabledFeatures: %v", value)
	}
	return json.Unmarshal(bytes, e)
}

// Value implements driver.Valuer for EnabledFeatures
func (e EnabledFeatures) Value() (driver.Value, error) {
	return json.Marshal(e)
}

// ServerConfig represents per-server configuration
type ServerConfig struct {
	BaseModel
	GuildID                 string          `gorm:"type:varchar(20);uniqueIndex;not null" json:"guild_id"`
	ReportsChannelID        *string         `gorm:"type:varchar(20)" json:"reports_channel_id,omitempty"`
	ActiveRoleName          string          `gorm:"type:varchar(100);not null;default:'Active of the Week'" json:"active_role_name"`
	ActiveRoleID            *string         `gorm:"type:varchar(20)" json:"active_role_id,omitempty"`
	InactivityThresholdDays int             `gorm:"not null;default:30" json:"inactivity_threshold_days"`
	WeeklyScheduleCron      string          `gorm:"type:varchar(50);not null;default:'0 0 * * 1'" json:"weekly_schedule_cron"`
	Timezone                string          `gorm:"type:varchar(50);not null;default:'UTC'" json:"timezone"`
	EnabledFeatures         EnabledFeatures `gorm:"type:json;not null" json:"enabled_features"`
}

// TableName specifies the table name for ServerConfig
func (ServerConfig) TableName() string {
	return "server_configs"
}

// DefaultEnabledFeatures returns the default feature flags
func DefaultEnabledFeatures() EnabledFeatures {
	return EnabledFeatures{
		Export:   true,
		Tracking: true,
		Reports:  true,
		Roles:    true,
	}
}
