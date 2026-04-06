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
				Description: "Nội dung bot sẽ reply (để trống nếu chỉ reply ảnh)",
				Required:    false,
			},
			{
				Type:        discordgo.ApplicationCommandOptionAttachment,
				Name:        "image",
				Description: "Ảnh bot sẽ reply kèm theo (tùy chọn)",
				Required:    false,
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

	data := i.ApplicationCommandData()
	options := parseOptions(data.Options)

	keyword, _ := options["keyword"].(string)
	keyword = strings.TrimPrefix(strings.TrimSpace(keyword), "/")
	if keyword == "" {
		respondError(s, i, "Keyword không được để trống.")
		return
	}

	response, _ := options["response"].(string)

	// Resolve image attachment URL if provided
	var imageURL string
	if attachID, ok := options["image"].(string); ok && attachID != "" {
		if attach, ok := data.Resolved.Attachments[attachID]; ok {
			imageURL = attach.URL
		}
	}

	if response == "" && imageURL == "" {
		respondError(s, i, "Phải có ít nhất `response` hoặc `image`.")
		return
	}

	err := cmd.svc.Add(context.Background(), i.GuildID, keyword, response, i.Member.User.ID, imageURL)
	if errors.Is(err, customcmd.ErrAlreadyExists) {
		respondError(s, i, "Keyword `"+keyword+"` đã tồn tại. Dùng `/delcom` để xóa trước.")
		return
	}
	if err != nil {
		log.Error().Err(err).Str("guild_id", i.GuildID).Str("trigger", keyword).Msg("Failed to add custom command")
		respondError(s, i, "Có lỗi xảy ra khi thêm keyword.")
		return
	}

	desc := "`" + keyword + "`"
	if response != "" {
		desc += " → **" + response + "**"
	}
	if imageURL != "" {
		desc += " + ảnh"
	}
	respondSuccess(s, i, "✅ Đã thêm keyword "+desc+"\nNhắn `/"+keyword+"` để dùng.")
}
