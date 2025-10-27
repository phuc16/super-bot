package commands

import (
	"context"
	"fmt"
	"strings"

	"github.com/bwmarrin/discordgo"
	"github.com/rs/zerolog/log"

	"github.com/phuc16/VCG-super-bot/internal/services/roles"
)

// LeaderboardCommand handles the /leaderboard slash command
type LeaderboardCommand struct {
	analyzer *roles.Analyzer
}

// NewLeaderboardCommand creates a new leaderboard command handler
func NewLeaderboardCommand(analyzer *roles.Analyzer) *LeaderboardCommand {
	return &LeaderboardCommand{
		analyzer: analyzer,
	}
}

// Definition returns the command definition for Discord
func (cmd *LeaderboardCommand) Definition() *discordgo.ApplicationCommand {
	return &discordgo.ApplicationCommand{
		Name:        "leaderboard",
		Description: "View the top 10 active users and your rank",
		Options: []*discordgo.ApplicationCommandOption{
			{
				Type:        discordgo.ApplicationCommandOptionString,
				Name:        "type",
				Description: "Leaderboard type (weekly or all-time)",
				Required:    false,
				Choices: []*discordgo.ApplicationCommandOptionChoice{
					{Name: "Weekly", Value: "weekly"},
					{Name: "All Time", Value: "alltime"},
				},
			},
		},
	}
}

// Handle processes the command interaction
func (cmd *LeaderboardCommand) Handle(s *discordgo.Session, i *discordgo.InteractionCreate) {
	// Parse options
	options := parseOptions(i.ApplicationCommandData().Options)

	leaderboardType := "weekly" // Default
	if ltype, ok := options["type"].(string); ok {
		leaderboardType = ltype
	}

	guildID := i.GuildID
	userID := i.Member.User.ID
	ctx := context.Background()

	// Defer response (processing takes time)
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

	// Generate leaderboard
	embed := cmd.generateLeaderboard(ctx, guildID, userID, leaderboardType)

	// Send follow-up message
	_, err = s.FollowupMessageCreate(i.Interaction, true, &discordgo.WebhookParams{
		Embeds: []*discordgo.MessageEmbed{embed},
		Flags:  discordgo.MessageFlagsEphemeral,
	})
	if err != nil {
		log.Error().Err(err).Msg("Failed to send follow-up message")
	}
}

// generateLeaderboard creates the leaderboard embed
func (cmd *LeaderboardCommand) generateLeaderboard(ctx context.Context, guildID, userID, lbType string) *discordgo.MessageEmbed {
	var title, description string
	var topUsers []roles.TopUser
	var userRank, userMessageCount int
	var err error

	if lbType == "alltime" {
		title = "🏆 All-Time Leaderboard"
		description = "Top users by total messages sent"
		topUsers, err = cmd.analyzer.GetTopActiveUsersAllTime(ctx, guildID, 10)
		if err != nil {
			log.Error().Err(err).Msg("Failed to get all-time leaderboard")
			topUsers = []roles.TopUser{}
		}
		userRank, userMessageCount, err = cmd.analyzer.GetUserRankAllTime(ctx, guildID, userID)
	} else {
		title = "🏆 Weekly Leaderboard"
		description = "Top users by messages sent this week"
		topUsers, err = cmd.analyzer.GetTopActiveUsers(ctx, guildID, 10)
		if err != nil {
			log.Error().Err(err).Msg("Failed to get weekly leaderboard")
			topUsers = []roles.TopUser{}
		}
		userRank, userMessageCount, err = cmd.analyzer.GetUserRank(ctx, guildID, userID)
	}

	if err != nil {
		log.Error().Err(err).Msg("Failed to get user rank")
	}

	// Build leaderboard text
	var builder strings.Builder

	if len(topUsers) == 0 {
		builder.WriteString("📭 No activity recorded yet.\nStart chatting to appear on the leaderboard!")
	} else {
		for i, user := range topUsers {
			rankEmoji := getRankEmoji(user.Rank)

			// Highlight current user
			if user.UserID == userID {
				builder.WriteString(fmt.Sprintf("%s **#%d** — **YOU** (%d messages) 👈\n",
					rankEmoji, user.Rank, user.MessageCount))
			} else {
				builder.WriteString(fmt.Sprintf("%s **#%d** — %s (%d messages)\n",
					rankEmoji, user.Rank, user.Username, user.MessageCount))
			}

			// Add separator for top 3
			if i == 2 && len(topUsers) > 3 {
				builder.WriteString("---\n")
			}
		}
	}

	// Add user's rank if not in top 10
	var footer string
	if userRank > 0 {
		if userRank <= 10 {
			footer = fmt.Sprintf("Your rank: #%d • %d messages", userRank, userMessageCount)
		} else {
			footer = fmt.Sprintf("You're ranked #%d with %d messages (not in top 10)",
				userRank, userMessageCount)
		}
	} else {
		footer = "You haven't sent any messages this week yet"
	}

	return &discordgo.MessageEmbed{
		Title:       title,
		Description: description,
		Fields: []*discordgo.MessageEmbedField{
			{
				Name:   "Top Users",
				Value:  builder.String(),
				Inline: false,
			},
		},
		Footer: &discordgo.MessageEmbedFooter{
			Text: footer,
		},
		Color: 0x5865F2, // Discord blurple
	}
}
