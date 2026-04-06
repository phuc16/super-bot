package models

// CustomCommand represents a user-defined keyword command for a guild
type CustomCommand struct {
	BaseModel
	GuildID   string  `gorm:"type:varchar(20);not null;index" json:"guild_id"`
	Trigger   string  `gorm:"column:trigger;type:varchar(100);not null" json:"trigger"`
	Response  string  `gorm:"type:text;not null" json:"response"`
	ImageURL  *string `gorm:"column:image_url;type:varchar(500)" json:"image_url,omitempty"`
	CreatedBy string  `gorm:"type:varchar(20);not null" json:"created_by"`
}

// TableName specifies the table name for CustomCommand
func (CustomCommand) TableName() string {
	return "custom_commands"
}
