package events

import (
	"context"
	"strings"

	"github.com/bwmarrin/discordgo"
	"github.com/rs/zerolog/log"

	"github.com/phuc16/VCG-super-bot/internal/services/activity"
	"github.com/phuc16/VCG-super-bot/internal/services/customcmd"
)

// MessageHandler handles Discord MESSAGE_CREATE events
type MessageHandler struct {
	tracker    *activity.Tracker
	customcmds *customcmd.Service
}

// NewMessageHandler creates a new message handler
func NewMessageHandler(tracker *activity.Tracker, customcmds *customcmd.Service) *MessageHandler {
	return &MessageHandler{
		tracker:    tracker,
		customcmds: customcmds,
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

	// Check if message is a custom keyword command (e.g. "/Kami")
	content := strings.TrimSpace(m.Content)
	if strings.HasPrefix(content, "/") {
		trigger := strings.ToLower(strings.TrimPrefix(content, "/"))
		// Only treat as keyword if it's a single word (no spaces) — avoids clashing with real slash commands
		if trigger != "" && !strings.Contains(trigger, " ") {
			response, found, err := h.customcmds.GetResponse(context.Background(), m.GuildID, trigger)
			if err != nil {
				log.Error().Err(err).Str("guild_id", m.GuildID).Str("trigger", trigger).Msg("Failed to look up custom command")
			} else if found {
				ref := &discordgo.MessageReference{
					MessageID: m.ID,
					ChannelID: m.ChannelID,
					GuildID:   m.GuildID,
				}
				if _, err := s.ChannelMessageSendReply(m.ChannelID, response, ref); err != nil {
					log.Error().Err(err).Str("channel_id", m.ChannelID).Msg("Failed to send keyword reply")
				}
				// Still track activity below
			}
		}
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
