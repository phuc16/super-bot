package commands

import (
	"context"
	"fmt"
	"strings"

	"github.com/bwmarrin/discordgo"
	"github.com/rs/zerolog/log"

	"github.com/phuc16/VCG-super-bot/internal/services/aitopic"
)

// DeltopicCommand handles the /deltopic slash command
type DeltopicCommand struct {
	svc *aitopic.Service
}

// NewDeltopicCommand creates a new DeltopicCommand
func NewDeltopicCommand(svc *aitopic.Service) *DeltopicCommand {
	return &DeltopicCommand{svc: svc}
}

// Definition returns the Discord application command definition
func (cmd *DeltopicCommand) Definition() *discordgo.ApplicationCommand {
	return &discordgo.ApplicationCommand{
		Name:        "deltopic",
		Description: "Xóa topic AI (Admin only) — dùng /listtopic để lấy số thứ tự",
		Options: []*discordgo.ApplicationCommandOption{
			{
				Type:        discordgo.ApplicationCommandOptionInteger,
				Name:        "number",
				Description: "Số thứ tự topic trong danh sách /listtopic",
				Required:    true,
				MinValue:    func() *float64 { v := float64(1); return &v }(),
			},
		},
	}
}

// Handle processes the /deltopic interaction
func (cmd *DeltopicCommand) Handle(s *discordgo.Session, i *discordgo.InteractionCreate) {
	if !hasAdminPermission(i.Member) {
		respondError(s, i, "Bạn cần quyền Administrator hoặc Manage Server để dùng lệnh này.")
		return
	}

	options := parseOptions(i.ApplicationCommandData().Options)
	numberF, _ := options["number"].(float64)
	number := int64(numberF)

	topics, err := cmd.svc.List(context.Background(), i.GuildID)
	if err != nil {
		log.Error().Err(err).Str("guild_id", i.GuildID).Msg("Failed to list AI topics")
		respondError(s, i, "Có lỗi xảy ra.")
		return
	}

	idx := int(number) - 1
	if idx < 0 || idx >= len(topics) {
		respondError(s, i, fmt.Sprintf("Số thứ tự không hợp lệ. Hiện có %d topic.", len(topics)))
		return
	}

	target := topics[idx]
	if err := cmd.svc.Delete(context.Background(), i.GuildID, target.ID.String()); err != nil {
		log.Error().Err(err).Str("guild_id", i.GuildID).Msg("Failed to delete AI topic")
		respondError(s, i, "Có lỗi xảy ra khi xóa topic.")
		return
	}

	topicPreview := target.Topic
	if len([]rune(topicPreview)) > 60 {
		topicPreview = string([]rune(topicPreview)[:60]) + "..."
	}
	respondSuccess(s, i, "✅ Đã xóa topic #"+fmt.Sprint(number)+": **"+strings.TrimSpace(topicPreview)+"**")
}
