package commands

import (
	"context"
	"fmt"
	"strings"

	"github.com/bwmarrin/discordgo"
	"github.com/rs/zerolog/log"

	"github.com/phuc16/VCG-super-bot/internal/services/aitopic"
)

// ListtopicCommand handles the /listtopic slash command
type ListtopicCommand struct {
	svc *aitopic.Service
}

// NewListtopicCommand creates a new ListtopicCommand
func NewListtopicCommand(svc *aitopic.Service) *ListtopicCommand {
	return &ListtopicCommand{svc: svc}
}

// Definition returns the Discord application command definition
func (cmd *ListtopicCommand) Definition() *discordgo.ApplicationCommand {
	return &discordgo.ApplicationCommand{
		Name:        "listtopic",
		Description: "Xem danh sách topic AI",
	}
}

// Handle processes the /listtopic interaction
func (cmd *ListtopicCommand) Handle(s *discordgo.Session, i *discordgo.InteractionCreate) {
	topics, err := cmd.svc.List(context.Background(), i.GuildID)
	if err != nil {
		log.Error().Err(err).Str("guild_id", i.GuildID).Msg("Failed to list AI topics")
		respondError(s, i, "Có lỗi xảy ra.")
		return
	}

	if len(topics) == 0 {
		respondSuccess(s, i, "Chưa có topic nào. Dùng `/addtopic` để thêm.")
		return
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("**Danh sách AI topics (%d):**\n", len(topics)))
	for idx, t := range topics {
		preview := t.Topic
		if len([]rune(preview)) > 80 {
			preview = string([]rune(preview)[:80]) + "..."
		}
		sb.WriteString(fmt.Sprintf("`%d.` %s → <#%s>\n", idx+1, preview, t.ChannelID))
	}
	sb.WriteString("\nDùng `/deltopic number:<số>` để xóa.")

	err = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Content: sb.String(),
			Flags:   discordgo.MessageFlagsEphemeral,
		},
	})
	if err != nil {
		log.Error().Err(err).Msg("Failed to send listtopic response")
	}
}
