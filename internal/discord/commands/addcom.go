package commands

import (
	"context"
	"errors"
	"strings"

	"github.com/bwmarrin/discordgo"
	"github.com/rs/zerolog/log"

	"github.com/phuc16/VCG-super-bot/internal/services/customcmd"
)

// AddcomCommand handles the /addcom slash command
type AddcomCommand struct {
	svc *customcmd.Service
}

// NewAddcomCommand creates a new AddcomCommand
func NewAddcomCommand(svc *customcmd.Service) *AddcomCommand {
	return &AddcomCommand{svc: svc}
}

// Definition returns the Discord application command definition
func (cmd *AddcomCommand) Definition() *discordgo.ApplicationCommand {
	return &discordgo.ApplicationCommand{
		Name:        "addcom",
		Description: "Thêm keyword command (Admin only)",
		Options: []*discordgo.ApplicationCommandOption{
			{
				Type:        discordgo.ApplicationCommandOptionString,
				Name:        "keyword",
				Description: "Keyword (không cần dấu /), ví dụ: Kami",
				Required:    true,
			},
			{
				Type:        discordgo.ApplicationCommandOptionString,
				Name:        "response",
				Description: "Nội dung bot sẽ reply, ví dụ: Xinh đẹp",
				Required:    true,
			},
		},
	}
}

// Handle processes the /addcom interaction
func (cmd *AddcomCommand) Handle(s *discordgo.Session, i *discordgo.InteractionCreate) {
	if !hasAdminPermission(i.Member) {
		respondError(s, i, "Bạn cần quyền Administrator hoặc Manage Server để dùng lệnh này.")
		return
	}

	options := parseOptions(i.ApplicationCommandData().Options)
	keyword, _ := options["keyword"].(string)
	response, _ := options["response"].(string)

	keyword = strings.TrimPrefix(strings.TrimSpace(keyword), "/")
	if keyword == "" {
		respondError(s, i, "Keyword không được để trống.")
		return
	}
	if response == "" {
		respondError(s, i, "Response không được để trống.")
		return
	}

	err := cmd.svc.Add(context.Background(), i.GuildID, keyword, response, i.Member.User.ID)
	if errors.Is(err, customcmd.ErrAlreadyExists) {
		respondError(s, i, "Keyword `"+keyword+"` đã tồn tại. Dùng `/delcom` để xóa trước.")
		return
	}
	if err != nil {
		log.Error().Err(err).Str("guild_id", i.GuildID).Str("trigger", keyword).Msg("Failed to add custom command")
		respondError(s, i, "Có lỗi xảy ra khi thêm keyword.")
		return
	}

	respondSuccess(s, i, "✅ Đã thêm keyword `"+keyword+"` → **"+response+"**\nNhắn `/"+keyword+"` để dùng.")
}
