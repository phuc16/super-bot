package commands

import (
	"context"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/rs/zerolog/log"
	openai "github.com/sashabaranov/go-openai"

	"github.com/phuc16/VCG-super-bot/internal/gpt"
)

// PosttopicCommand handles the /posttopic slash command
type PosttopicCommand struct {
	gptClient *gpt.Client
}

// NewPosttopicCommand creates a new PosttopicCommand
func NewPosttopicCommand(gptClient *gpt.Client) *PosttopicCommand {
	return &PosttopicCommand{gptClient: gptClient}
}

// Definition returns the Discord application command definition
func (cmd *PosttopicCommand) Definition() *discordgo.ApplicationCommand {
	return &discordgo.ApplicationCommand{
		Name:        "posttopic",
		Description: "Gửi ngay 1 đoạn AI về chủ đề bất kỳ vào channel (Admin only)",
		Options: []*discordgo.ApplicationCommandOption{
			{
				Type:        discordgo.ApplicationCommandOptionString,
				Name:        "topic",
				Description: "Chủ đề để AI viết về",
				Required:    true,
			},
			{
				Type:        discordgo.ApplicationCommandOptionChannel,
				Name:        "channel",
				Description: "Channel để bot gửi nội dung",
				Required:    true,
				ChannelTypes: []discordgo.ChannelType{
					discordgo.ChannelTypeGuildText,
				},
			},
			{
				Type:        discordgo.ApplicationCommandOptionUser,
				Name:        "mention",
				Description: "Tag user này trong đoạn văn (tùy chọn)",
				Required:    false,
			},
		},
	}
}

// Handle processes the /posttopic interaction
func (cmd *PosttopicCommand) Handle(s *discordgo.Session, i *discordgo.InteractionCreate) {
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

	mentionUserID, _ := options["mention"].(string)

	// Defer immediately — GPT call takes time
	err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseDeferredChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Flags: discordgo.MessageFlagsEphemeral,
		},
	})
	if err != nil {
		log.Error().Err(err).Msg("posttopic: failed to defer response")
		return
	}

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()

		topicText := topic
		if mentionUserID != "" {
			topicText = strings.ReplaceAll(topicText, "{mention}", "<@"+mentionUserID+">")
		}

		messages := []openai.ChatCompletionMessage{
			{Role: openai.ChatMessageRoleSystem, Content: gpt.TopicSystemPromptVI},
			{Role: openai.ChatMessageRoleUser, Content: "Viết một đoạn văn về: " + topicText},
		}

		content, _, err := cmd.gptClient.CreateChatCompletionWithRetry(ctx, messages)
		if err != nil {
			log.Error().Err(err).Msg("posttopic: GPT call failed")
			followUpError(s, i, "GPT call thất bại: "+err.Error())
			return
		}

		if _, err := s.ChannelMessageSend(channelID, content); err != nil {
			log.Error().Err(err).Str("channel_id", channelID).Msg("posttopic: failed to send message")
			followUpError(s, i, "Không thể gửi vào <#"+channelID+">: "+err.Error())
			return
		}

		_, _ = s.FollowupMessageCreate(i.Interaction, true, &discordgo.WebhookParams{
			Content: "✅ Đã gửi vào <#" + channelID + ">.",
			Flags:   discordgo.MessageFlagsEphemeral,
		})
	}()
}
