package commands

import (
	"context"
	"errors"
	"strings"

	"github.com/bwmarrin/discordgo"
	"github.com/rs/zerolog/log"

	"github.com/phuc16/VCG-super-bot/internal/services/customcmd"
)

// DelcomCommand handles the /delcom slash command
type DelcomCommand struct {
	svc *customcmd.Service
}

// NewDelcomCommand creates a new DelcomCommand
func NewDelcomCommand(svc *customcmd.Service) *DelcomCommand {
	return &DelcomCommand{svc: svc}
}

// Definition returns the Discord application command definition
func (cmd *DelcomCommand) Definition() *discordgo.ApplicationCommand {
	return &discordgo.ApplicationCommand{
		Name:        "delcom",
		Description: "Xóa keyword command (Admin only)",
		Options: []*discordgo.ApplicationCommandOption{
			{
				Type:        discordgo.ApplicationCommandOptionString,
				Name:        "keyword",
				Description: "Keyword cần xóa (không cần dấu /)",
				Required:    true,
			},
		},
	}
}

// Handle processes the /delcom interaction
func (cmd *DelcomCommand) Handle(s *discordgo.Session, i *discordgo.InteractionCreate) {
	if !hasAdminPermission(i.Member) {
		respondError(s, i, "Bạn cần quyền Administrator hoặc Manage Server để dùng lệnh này.")
		return
	}

	options := parseOptions(i.ApplicationCommandData().Options)
	keyword, _ := options["keyword"].(string)
	keyword = strings.TrimPrefix(strings.TrimSpace(keyword), "/")
	if keyword == "" {
		respondError(s, i, "Keyword không được để trống.")
		return
	}

	err := cmd.svc.Delete(context.Background(), i.GuildID, keyword)
	if errors.Is(err, customcmd.ErrNotFound) {
		respondError(s, i, "Keyword `"+keyword+"` không tồn tại.")
		return
	}
	if err != nil {
		log.Error().Err(err).Str("guild_id", i.GuildID).Str("trigger", keyword).Msg("Failed to delete custom command")
		respondError(s, i, "Có lỗi xảy ra khi xóa keyword.")
		return
	}

	respondSuccess(s, i, "✅ Đã xóa keyword `"+keyword+"`.")
}
