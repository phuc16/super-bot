package events

import (
	"github.com/bwmarrin/discordgo"
	"github.com/rs/zerolog/log"

	"github.com/phuc16/VCG-super-bot/internal/services/activity"
)

// MessageHandler handles Discord MESSAGE_CREATE events
type MessageHandler struct {
	tracker *activity.Tracker
}

// NewMessageHandler creates a new message handler
func NewMessageHandler(tracker *activity.Tracker) *MessageHandler {
	return &MessageHandler{
		tracker: tracker,
	}
}

// Handle processes MESSAGE_CREATE events
func (h *MessageHandler) Handle(s *discordgo.Session, m *discordgo.MessageCreate) {
	// Skip if message is from a bot
	if m.Author.Bot {
		return
	}

	// Skip DMs (messages without a GuildID)
	if m.GuildID == "" {
		return
	}

	// Create activity event
	event := activity.ActivityEvent{
		Type:      activity.EventTypeMessage,
		GuildID:   m.GuildID,
		UserID:    m.Author.ID,
		Username:  m.Author.Username,
		AvatarURL: m.Author.AvatarURL(""),
		ChannelID: m.ChannelID,
		Timestamp: m.Timestamp,
	}

	// Send to tracker
	if err := h.tracker.Track(event); err != nil {
		log.Error().
			Err(err).
			Str("guild_id", m.GuildID).
			Str("user_id", m.Author.ID).
			Str("channel_id", m.ChannelID).
			Msg("Failed to track message event")
	}
}

// Register registers the handler with the Discord session
func (h *MessageHandler) Register(s *discordgo.Session) {
	s.AddHandler(h.Handle)
	log.Info().Msg("Message handler registered")
}
