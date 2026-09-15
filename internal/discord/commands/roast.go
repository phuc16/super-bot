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

// RoastCommand handles the /roast slash command
type RoastCommand struct {
	gptClient *gpt.Client
}

// NewRoastCommand creates a new RoastCommand
func NewRoastCommand(gptClient *gpt.Client) *RoastCommand {
	return &RoastCommand{gptClient: gptClient}
}

// Definition returns the Discord application command definition
func (cmd *RoastCommand) Definition() *discordgo.ApplicationCommand {
	return &discordgo.ApplicationCommand{
		Name:        "roast",
		Description: "AI roast một thành viên dựa trên tin nhắn gần đây của họ",
		Options: []*discordgo.ApplicationCommandOption{
			{
				Type:        discordgo.ApplicationCommandOptionUser,
				Name:        "target",
				Description: "Người bị roast",
				Required:    true,
			},
		},
	}
}

// Handle processes the /roast interaction
func (cmd *RoastCommand) Handle(s *discordgo.Session, i *discordgo.InteractionCreate) {
	options := parseOptions(i.ApplicationCommandData().Options)
	targetID, _ := options["target"].(string)
	if targetID == "" {
		respondError(s, i, "Không xác định được người bị roast.")
		return
	}

	if targetID == i.Member.User.ID {
		respondError(s, i, "Tự roast bản thân thì đi tâm sự với bot khác nhé.")
		return
	}

	targetUser := i.ApplicationCommandData().Resolved.Users[targetID]
	if targetUser == nil {
		respondError(s, i, "Không tìm thấy người dùng này.")
		return
	}
	if targetUser.Bot {
		respondError(s, i, "Không roast bot được, chúng nó không có cảm xúc đâu.")
		return
	}

	targetName := targetUser.Username
	if member, ok := i.ApplicationCommandData().Resolved.Members[targetID]; ok && member.Nick != "" {
		targetName = member.Nick
	} else if targetUser.GlobalName != "" {
		targetName = targetUser.GlobalName
	}

	channelID := i.ChannelID

	// Defer immediately — GPT call takes time
	if err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseDeferredChannelMessageWithSource,
	}); err != nil {
		log.Error().Err(err).Msg("roast: failed to defer response")
		return
	}

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()

		recentQuotes := fetchRecentQuotesFromUser(s, channelID, targetID, 15)

		userPrompt := "Roast " + targetName + "."
		if len(recentQuotes) > 0 {
			userPrompt += " Đây là vài tin nhắn gần đây của " + targetName + " để lấy chất liệu:\n" + strings.Join(recentQuotes, "\n")
		} else {
			userPrompt += " Không có tin nhắn gần đây nào của người này, roast chung chung nhưng vẫn phải hài."
		}

		messages := []openai.ChatCompletionMessage{
			{Role: openai.ChatMessageRoleSystem, Content: gpt.RoastSystemPromptVI},
			{Role: openai.ChatMessageRoleUser, Content: userPrompt},
		}

		content, _, err := cmd.gptClient.CreateChatCompletionWithRetry(ctx, messages)
		if err != nil {
			log.Error().Err(err).Msg("roast: GPT call failed")
			followUpError(s, i, "GPT call thất bại: "+err.Error())
			return
		}

		_, _ = s.FollowupMessageCreate(i.Interaction, true, &discordgo.WebhookParams{
			Content: "🔥 <@" + targetID + "> " + content,
		})
	}()
}

// fetchRecentQuotesFromUser pulls up to maxQuotes recent, non-empty messages
// authored by targetID from the channel's recent history.
func fetchRecentQuotesFromUser(s *discordgo.Session, channelID, targetID string, maxQuotes int) []string {
	msgs, err := s.ChannelMessages(channelID, 100, "", "", "")
	if err != nil {
		log.Warn().Err(err).Msg("roast: failed to fetch channel history")
		return nil
	}

	var quotes []string
	for _, m := range msgs {
		if m.Author == nil || m.Author.ID != targetID {
			continue
		}
		content := strings.TrimSpace(m.Content)
		if content == "" {
			continue
		}
		quotes = append(quotes, "- "+content)
		if len(quotes) >= maxQuotes {
			break
		}
	}
	return quotes
}
