package commands

import (
	"context"
	"strings"

	"github.com/bwmarrin/discordgo"
	"github.com/rs/zerolog/log"

	"github.com/phuc16/VCG-super-bot/internal/services/customcmd"
)

// ListcomCommand handles the /listcom slash command
type ListcomCommand struct {
	svc *customcmd.Service
}

// NewListcomCommand creates a new ListcomCommand
func NewListcomCommand(svc *customcmd.Service) *ListcomCommand {
	return &ListcomCommand{svc: svc}
}

// Definition returns the Discord application command definition
func (cmd *ListcomCommand) Definition() *discordgo.ApplicationCommand {
	return &discordgo.ApplicationCommand{
		Name:        "listcom",
		Description: "Xem danh sách các keyword command",
	}
}

// Handle processes the /listcom interaction
func (cmd *ListcomCommand) Handle(s *discordgo.Session, i *discordgo.InteractionCreate) {
	cmds, err := cmd.svc.List(context.Background(), i.GuildID)
	if err != nil {
		log.Error().Err(err).Str("guild_id", i.GuildID).Msg("Failed to list custom commands")
		respondError(s, i, "Có lỗi xảy ra khi lấy danh sách keyword.")
		return
	}

	if len(cmds) == 0 {
		respondSuccess(s, i, "Chưa có keyword nào được thêm. Dùng `/addcom` để thêm.")
		return
	}

	var sb strings.Builder
	sb.WriteString("**Danh sách keyword commands:**\n")
	for _, c := range cmds {
		sb.WriteString("`/")
		sb.WriteString(c.Trigger)
		sb.WriteString("` → ")
		sb.WriteString(c.Response)
		sb.WriteString("\n")
	}

	err = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Content: sb.String(),
			Flags:   discordgo.MessageFlagsEphemeral,
		},
	})
	if err != nil {
		log.Error().Err(err).Msg("Failed to send listcom response")
	}
}
