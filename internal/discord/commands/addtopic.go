package commands

import (
	"context"
	"fmt"
	"strings"

	"github.com/bwmarrin/discordgo"
	"github.com/rs/zerolog/log"

	"github.com/phuc16/VCG-super-bot/internal/services/aitopic"
)

// AddtopicCommand handles the /addtopic slash command
type AddtopicCommand struct {
	svc *aitopic.Service
}

// NewAddtopicCommand creates a new AddtopicCommand
func NewAddtopicCommand(svc *aitopic.Service) *AddtopicCommand {
	return &AddtopicCommand{svc: svc}
}

// Definition returns the Discord application command definition
func (cmd *AddtopicCommand) Definition() *discordgo.ApplicationCommand {
	minInterval := float64(1)
	return &discordgo.ApplicationCommand{
		Name:        "addtopic",
		Description: "Thêm topic để bot tự viết AI định kỳ (Admin only)",
		Options: []*discordgo.ApplicationCommandOption{
			{
				Type:        discordgo.ApplicationCommandOptionString,
				Name:        "topic",
				Description: "Chủ đề bot sẽ viết về, ví dụ: lịch sử Việt Nam",
				Required:    true,
			},
			{
				Type:        discordgo.ApplicationCommandOptionChannel,
				Name:        "channel",
				Description: "Channel để bot post nội dung",
				Required:    true,
				ChannelTypes: []discordgo.ChannelType{
					discordgo.ChannelTypeGuildText,
				},
			},
			{
				Type:        discordgo.ApplicationCommandOptionInteger,
				Name:        "interval",
				Description: "Chu kỳ đăng (phút), mặc định 5",
				Required:    false,
				MinValue:    &minInterval,
			},
			{
				Type:        discordgo.ApplicationCommandOptionUser,
				Name:        "mention",
				Description: "Tag user này khi bot post (ví dụ: @Tâm)",
				Required:    false,
			},
		},
	}
}

// Handle processes the /addtopic interaction
func (cmd *AddtopicCommand) Handle(s *discordgo.Session, i *discordgo.InteractionCreate) {
	if !hasAdminPermission(i.Member) {
		respondError(s, i, "Bạn cần quyền Administrator hoặc Manage Server để dùng lệnh này.")
		return
	}

	options := parseOptions(i.ApplicationCommandData().Options)
	topic, _ := options["topic"].(string)
	topic = strings.TrimSpace(topic)
	if topic == "" {
		respondError(s, i, "Topic không được để trống.")
		return
	}

	channelID, _ := options["channel"].(string)
	if channelID == "" {
		respondError(s, i, "Channel không hợp lệ.")
		return
	}

	intervalMinutes := 5
	if iv, ok := options["interval"].(float64); ok && iv > 0 {
		intervalMinutes = int(iv)
	}

	mentionUserID, _ := options["mention"].(string)

	err := cmd.svc.Add(context.Background(), i.GuildID, topic, channelID, i.Member.User.ID, intervalMinutes, mentionUserID)
	if err != nil {
		log.Error().Err(err).Str("guild_id", i.GuildID).Msg("Failed to add AI topic")
		respondError(s, i, "Có lỗi xảy ra khi thêm topic.")
		return
	}

	msg := fmt.Sprintf("✅ Đã thêm topic: **%s**\nBot sẽ post AI content vào <#%s> mỗi **%d phút**.", topic, channelID, intervalMinutes)
	if mentionUserID != "" {
		msg += fmt.Sprintf("\nSẽ tag <@%s> khi post.", mentionUserID)
	}
	respondSuccess(s, i, msg)
}
