package models

import (
	"time"
)

// ExportType defines the type of message export
type ExportType string

const (
	ExportTypeDateRange ExportType = "date_range"
	ExportTypeRecent    ExportType = "recent"
)

// SummaryStatus defines the status of AI summarization
type SummaryStatus string

const (
	SummaryStatusPending   SummaryStatus = "pending"
	SummaryStatusCompleted SummaryStatus = "completed"
	SummaryStatusFailed    SummaryStatus = "failed"
	SummaryStatusSkipped   SummaryStatus = "skipped"
)

// MessageExport represents a message export request
type MessageExport struct {
	BaseModel
	GuildID              string        `gorm:"type:varchar(20);not null;index:idx_guild_requester" json:"guild_id"`
	ChannelID            string        `gorm:"type:varchar(20);not null" json:"channel_id"`
	RequesterUserID      string        `gorm:"type:varchar(20);not null;index:idx_guild_requester" json:"requester_user_id"`
	ExportType           ExportType    `gorm:"type:enum('date_range','recent');not null" json:"export_type"`
	StartTime            *time.Time    `json:"start_time,omitempty"`
	EndTime              *time.Time    `json:"end_time,omitempty"`
	MessageCount         int           `gorm:"not null" json:"message_count"`
	SummaryText          *string       `gorm:"type:text" json:"summary_text,omitempty"`
	SummaryStatus        SummaryStatus `gorm:"type:enum('pending','completed','failed','skipped');not null;index" json:"summary_status"`
	SummaryError         *string       `gorm:"type:text" json:"summary_error,omitempty"`
	GPTTokensUsed        *int          `json:"gpt_tokens_used,omitempty"`
	ProcessingDurationMs *int          `json:"processing_duration_ms,omitempty"`
	CompletedAt          *time.Time    `json:"completed_at,omitempty"`
}

// TableName specifies the table name for MessageExport
func (MessageExport) TableName() string {
	return "message_exports"
}

// DiscordMessage represents a single Discord message for export
type DiscordMessage struct {
	ID             string    `json:"id"`
	Author         string    `json:"author"`
	Content        string    `json:"content"`
	Timestamp      time.Time `json:"timestamp"`
	HasAttach      bool      `json:"has_attachments"`
	ReplyToAuthor  string    `json:"reply_to_author,omitempty"`
	ReplyToContent string    `json:"reply_to_content,omitempty"`
}
