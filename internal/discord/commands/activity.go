package commands

import (
	"context"
	"fmt"
	"strings"

	"github.com/bwmarrin/discordgo"
	"github.com/rs/zerolog/log"

	"github.com/phuc16/VCG-super-bot/internal/services/roles"
)

// ActivityCommand handles the /activity slash command
type ActivityCommand struct {
	analyzer *roles.Analyzer
}

// NewActivityCommand creates a new ActivityCommand
func NewActivityCommand(analyzer *roles.Analyzer) *ActivityCommand {
	return &ActivityCommand{
		analyzer: analyzer,
	}
}

// Definition returns the command definition for Discord
func (cmd *ActivityCommand) Definition() *discordgo.ApplicationCommand {
	return &discordgo.ApplicationCommand{
		Name:        "activity",
		Description: "View server activity statistics",
		Options: []*discordgo.ApplicationCommandOption{
			{
				Type:        discordgo.ApplicationCommandOptionString,
				Name:        "view",
				Description: "What to view: your stats, server overview, or top users",
				Required:    false,
				Choices: []*discordgo.ApplicationCommandOptionChoice{
					{Name: "My Stats", Value: "user"},
					{Name: "Server Overview", Value: "server"},
					{Name: "Top 10 Users", Value: "top"},
				},
			},
		},
	}
}

// Handle processes the command interaction
func (cmd *ActivityCommand) Handle(s *discordgo.Session, i *discordgo.InteractionCreate) {
	// Parse options
	options := parseOptions(i.ApplicationCommandData().Options)

	view, ok := options["view"].(string)
	if !ok {
		view = "user" // Default to user stats
	}

	guildID := i.GuildID
	userID := i.Member.User.ID
	ctx := context.Background()

	// Acknowledge the command
	err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseDeferredChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Flags: discordgo.MessageFlagsEphemeral,
		},
	})
	if err != nil {
		log.Error().Err(err).Msg("Failed to send deferred response")
		return
	}

	// Generate response based on view type
	var response string
	switch view {
	case "user":
		response = cmd.getUserStats(ctx, guildID, userID)
	case "server":
		response = cmd.getServerStats(ctx, guildID)
	case "top":
		response = cmd.getTopUsers(ctx, guildID)
	default:
		response = "❌ Unknown view type"
	}

	// Send follow-up message
	_, err = s.FollowupMessageCreate(i.Interaction, true, &discordgo.WebhookParams{
		Content: response,
		Flags:   discordgo.MessageFlagsEphemeral,
	})
	if err != nil {
		log.Error().Err(err).Msg("Failed to send followup message")
	}
}

// getUserStats returns statistics for a specific user
func (cmd *ActivityCommand) getUserStats(ctx context.Context, guildID string, userID string) string {
	rank, messageCount, err := cmd.analyzer.GetUserRank(ctx, guildID, userID)
	if err != nil {
		log.Error().Err(err).Str("guild_id", guildID).Str("user_id", userID).Msg("Failed to get user rank")
		return "❌ Failed to retrieve your activity statistics"
	}

	if messageCount == 0 {
		return "📊 **Your Activity Stats**\n\nYou haven't sent any messages this week yet. Start chatting to climb the leaderboard! 🚀"
	}

	// Get total active users for context
	totalActive, err := cmd.analyzer.GetTotalActiveUsers(ctx, guildID)
	if err != nil {
		totalActive = 0
	}

	var rankEmoji string
	switch rank {
	case 1:
		rankEmoji = "🥇"
	case 2:
		rankEmoji = "🥈"
	case 3:
		rankEmoji = "🥉"
	case 4, 5:
		rankEmoji = "🏆"
	default:
		rankEmoji = "📈"
	}

	return fmt.Sprintf(`📊 **Your Activity Stats**

%s **Rank**: #%d of %d active users
💬 **Messages This Week**: %d

%s`,
		rankEmoji,
		rank,
		totalActive,
		messageCount,
		getMotivationalMessage(rank),
	)
}

// getServerStats returns overall server statistics
func (cmd *ActivityCommand) getServerStats(ctx context.Context, guildID string) string {
	stats, err := cmd.analyzer.GetServerStats(ctx, guildID)
	if err != nil {
		log.Error().Err(err).Str("guild_id", guildID).Msg("Failed to get server stats")
		return "❌ Failed to retrieve server statistics"
	}

	// Get top 5 for display
	topUsers, err := cmd.analyzer.GetTopActiveUsers(ctx, guildID, 5)
	if err != nil {
		log.Error().Err(err).Msg("Failed to get top users")
		topUsers = []roles.TopUser{}
	}

	topUsersText := ""
	if len(topUsers) > 0 {
		topUsersText = "\n\n**🏆 Top 5 Active Users:**\n"
		for _, user := range topUsers {
			rankEmoji := getRankEmoji(user.Rank)
			topUsersText += fmt.Sprintf("%s **#%d** - %s (%d messages)\n",
				rankEmoji, user.Rank, user.Username, user.MessageCount)
		}
	}

	return fmt.Sprintf(`📊 **Server Activity Overview**

👥 **Total Users**: %d
✨ **Active This Week**: %d
💬 **Total Messages (All Time)**: %d
📈 **Messages This Week**: %d%s`,
		stats["total_users"],
		stats["active_users"],
		stats["total_messages"],
		stats["total_week_messages"],
		topUsersText,
	)
}

// getTopUsers returns the top 10 users
func (cmd *ActivityCommand) getTopUsers(ctx context.Context, guildID string) string {
	topUsers, err := cmd.analyzer.GetTopActiveUsers(ctx, guildID, 10)
	if err != nil {
		log.Error().Err(err).Str("guild_id", guildID).Msg("Failed to get top users")
		return "❌ Failed to retrieve top users"
	}

	if len(topUsers) == 0 {
		return "📊 **Top Users**\n\nNo activity recorded this week yet!"
	}

	var builder strings.Builder
	builder.WriteString("🏆 **Top Active Users This Week**\n\n")

	for i, user := range topUsers {
		if i >= 10 {
			break // Limit to top 10
		}

		rankEmoji := getRankEmoji(user.Rank)
		builder.WriteString(fmt.Sprintf("%s **#%d** - %s\n💬 %d messages\n\n",
			rankEmoji, user.Rank, user.Username, user.MessageCount))
	}

	builder.WriteString("_Keep chatting to climb the leaderboard!_ 🚀")

	return builder.String()
}

// getMotivationalMessage returns a motivational message based on rank
func getMotivationalMessage(rank int) string {
	switch {
	case rank == 1:
		return "🎉 You're #1! Amazing work! Keep it up to earn the Active of the Week role!"
	case rank <= 3:
		return "🔥 You're in the top 3! Keep going to reach #1!"
	case rank <= 5:
		return "⭐ You're in the top 5! You're on track for the Active of the Week role!"
	case rank <= 10:
		return "💪 You're in the top 10! Keep chatting to break into the top 5!"
	default:
		return "📈 Keep participating to climb the leaderboard!"
	}
}

// getRankEmoji returns an emoji for a given rank
func getRankEmoji(rank int) string {
	switch rank {
	case 1:
		return "🥇"
	case 2:
		return "🥈"
	case 3:
		return "🥉"
	case 4, 5:
		return "🏆"
	default:
		return "📊"
	}
}
