package events

import (
	"bytes"
	"context"
	"strings"

	"github.com/bwmarrin/discordgo"
	"github.com/rs/zerolog/log"

	"github.com/phuc16/VCG-super-bot/internal/services/customcmd"
)

// MessageHandler handles Discord MESSAGE_CREATE events
type MessageHandler struct {
	customcmds *customcmd.Service
}

// NewMessageHandler creates a new message handler
func NewMessageHandler(customcmds *customcmd.Service) *MessageHandler {
	return &MessageHandler{customcmds: customcmds}
}

// Handle processes MESSAGE_CREATE events
func (h *MessageHandler) Handle(s *discordgo.Session, m *discordgo.MessageCreate) {
	if m.Author.Bot {
		return
	}
	if m.GuildID == "" {
		return
	}

	// Check if message is a custom keyword command (e.g. "/Kami")
	content := strings.TrimSpace(m.Content)
	if !strings.HasPrefix(content, "/") {
		return
	}

	trigger := strings.ToLower(strings.TrimPrefix(content, "/"))
	if trigger == "" || strings.Contains(trigger, " ") {
		return
	}

	cmd, err := h.customcmds.Get(context.Background(), m.GuildID, trigger)
	if err != nil {
		log.Error().Err(err).Str("guild_id", m.GuildID).Str("trigger", trigger).Msg("Failed to look up custom command")
		return
	}
	if cmd == nil {
		return
	}

	ref := &discordgo.MessageReference{
		MessageID: m.ID,
		ChannelID: m.ChannelID,
		GuildID:   m.GuildID,
	}

	msg := &discordgo.MessageSend{
		Content:   cmd.Response,
		Reference: ref,
	}
	// Re-upload the bytes as a fresh attachment every time, rather than
	// pointing an embed at a stored URL. Discord's attachment links are
	// signed and expire, and the ones /addcom used to save came from
	// /ephemeral-attachments/, which Discord does not retain at all — so
	// the old approach rendered an empty embed box within hours of the
	// command being created. Uploading makes the reply self-contained.
	//
	// Legacy rows with only ImageURL set are deliberately ignored: that
	// link is already dead, and posting it looks like a bot failure rather
	// than a command that needs its image re-added.
	if cmd.HasImage() {
		name := cmd.AttachmentName()
		msg.Files = []*discordgo.File{{
			Name:   name,
			Reader: bytes.NewReader(cmd.ImageData),
		}}
		msg.Embeds = []*discordgo.MessageEmbed{
			{Image: &discordgo.MessageEmbedImage{URL: "attachment://" + name}},
		}
	}
	if _, err := s.ChannelMessageSendComplex(m.ChannelID, msg); err != nil {
		log.Error().Err(err).Str("channel_id", m.ChannelID).Msg("Failed to send keyword reply")
	}
}

// Register registers the handler with the Discord session
func (h *MessageHandler) Register(s *discordgo.Session) {
	s.AddHandler(h.Handle)
	log.Info().Msg("Message handler registered")
}
