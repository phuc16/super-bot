package events

import (
	"github.com/bwmarrin/discordgo"
	"github.com/rs/zerolog/log"

	"github.com/phuc16/VCG-super-bot/internal/services/activity"
)

// VoiceHandler handles Discord VOICE_STATE_UPDATE events
type VoiceHandler struct {
	tracker *activity.Tracker
}

// NewVoiceHandler creates a new voice handler
func NewVoiceHandler(tracker *activity.Tracker) *VoiceHandler {
	return &VoiceHandler{
		tracker: tracker,
	}
}

// Handle processes VOICE_STATE_UPDATE events
func (h *VoiceHandler) Handle(s *discordgo.Session, v *discordgo.VoiceStateUpdate) {
	// Skip if user is a bot
	if v.Member != nil && v.Member.User.Bot {
		return
	}

	// Skip if no guild ID (shouldn't happen for voice)
	if v.GuildID == "" {
		return
	}

	// Get user information
	var userID, username, avatarURL string
	if v.Member != nil && v.Member.User != nil {
		userID = v.Member.User.ID
		username = v.Member.User.Username
		avatarURL = v.Member.User.AvatarURL("")
	} else if v.UserID != "" {
		userID = v.UserID
		// Try to fetch user info if not available
		user, err := s.User(userID)
		if err == nil {
			username = user.Username
			avatarURL = user.AvatarURL("")
		} else {
			username = "Unknown"
		}
	} else {
		log.Warn().Msg("Voice state update with no user information")
		return
	}

	// Determine event type
	eventType := activity.EventTypeVoiceJoin
	if v.ChannelID == "" {
		// User left voice channel
		eventType = activity.EventTypeVoiceLeave
	}

	// Create activity event
	event := activity.ActivityEvent{
		Type:      eventType,
		GuildID:   v.GuildID,
		UserID:    userID,
		Username:  username,
		AvatarURL: avatarURL,
		ChannelID: v.ChannelID,
	}

	// Send to tracker
	if err := h.tracker.Track(event); err != nil {
		log.Error().
			Err(err).
			Str("guild_id", v.GuildID).
			Str("user_id", userID).
			Str("channel_id", v.ChannelID).
			Str("event_type", string(eventType)).
			Msg("Failed to track voice event")
	}
}

// Register registers the handler with the Discord session
func (h *VoiceHandler) Register(s *discordgo.Session) {
	s.AddHandler(h.Handle)
	log.Info().Msg("Voice handler registered")
}
