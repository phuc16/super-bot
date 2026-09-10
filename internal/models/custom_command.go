package models

// CustomCommand represents a user-defined keyword command for a guild
type CustomCommand struct {
	BaseModel
	GuildID  string `gorm:"type:varchar(20);not null;index" json:"guild_id"`
	Trigger  string `gorm:"column:trigger;type:varchar(100);not null" json:"trigger"`
	Response string `gorm:"type:text;not null" json:"response"`
	// ImageURL is legacy and effectively dead. It held the Discord CDN link
	// of the uploaded attachment, but slash-command attachments are served
	// from /ephemeral-attachments/ with a signed, time-limited URL that
	// Discord does not retain, so every stored value expired within hours.
	// Kept only so the column isn't dropped out from under old rows; the
	// serving path ignores it whenever ImageData is present.
	ImageURL *string `gorm:"column:image_url;type:varchar(500)" json:"image_url,omitempty"`

	// ImageData holds the image bytes themselves, re-uploaded as a fresh
	// attachment on every use. This is what makes a custom command's image
	// survive: nothing about it depends on Discord retaining anything.
	//
	// Excluded from list queries — see customcmd.Service.List. Nothing but
	// the keyword-reply path needs these bytes, and selecting them in a
	// listing would pull every image in the guild into memory to display a
	// list that never shows images.
	ImageData []byte  `gorm:"column:image_data;type:longblob" json:"-"`
	ImageName *string `gorm:"column:image_name;type:varchar(255)" json:"image_name,omitempty"`

	CreatedBy string `gorm:"type:varchar(20);not null" json:"created_by"`
}

// HasImage reports whether this command has usable image bytes. A legacy
// row with only ImageURL set returns false: that link is expired and
// posting it renders an empty embed box.
func (c *CustomCommand) HasImage() bool {
	return len(c.ImageData) > 0
}

// AttachmentName is the filename used when re-uploading the image. Discord
// infers content type from the extension, and the embed references it as
// attachment://<name>, so it must be non-empty and extension-bearing.
func (c *CustomCommand) AttachmentName() string {
	if c.ImageName != nil && *c.ImageName != "" {
		return *c.ImageName
	}
	return "image.png"
}

// TableName specifies the table name for CustomCommand
func (CustomCommand) TableName() string {
	return "custom_commands"
}
