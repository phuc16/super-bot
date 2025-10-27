package commands

import (
	"context"
	"fmt"
	"strings"

	"github.com/bwmarrin/discordgo"
	"github.com/rs/zerolog/log"

	"github.com/phuc16/VCG-super-bot/internal/services/activity"
)

// InactiveCommand handles the /inactive slash command
type InactiveCommand struct {
	detector *activity.InactivityDetector
}

// NewInactiveCommand creates a new InactiveCommand
func NewInactiveCommand(detector *activity.InactivityDetector) *InactiveCommand {
	return &InactiveCommand{
		detector: detector,
	}
}

// Definition returns the command definition for Discord
func (cmd *InactiveCommand) Definition() *discordgo.ApplicationCommand {
	return &discordgo.ApplicationCommand{
		Name:        "inactive",
		Description: "View users who have been inactive for a specified period",
		Options: []*discordgo.ApplicationCommandOption{
			{
				Type:        discordgo.ApplicationCommandOptionInteger,
				Name:        "days",
				Description: "Number of days of inactivity (default: 30, min: 7, max: 90)",
				Required:    false,
				MinValue:    ptrFloat64(7),
				MaxValue:    90,
			},
			{
				Type:        discordgo.ApplicationCommandOptionString,
				Name:        "format",
				Description: "Display format",
				Required:    false,
				Choices: []*discordgo.ApplicationCommandOptionChoice{
					{Name: "Summary (top 10)", Value: "summary"},
					{Name: "Detailed List", Value: "detailed"},
				},
			},
		},
	}
}

// Handle processes the command interaction
func (cmd *InactiveCommand) Handle(s *discordgo.Session, i *discordgo.InteractionCreate) {
	// Parse options
	options := parseOptions(i.ApplicationCommandData().Options)

	// Get threshold (default 30 days)
	thresholdDays := 30
	if days, ok := options["days"].(int64); ok {
		thresholdDays = int(days)
	}

	// Validate threshold
	if err := activity.ValidateThreshold(thresholdDays); err != nil {
		respondError(s, i, fmt.Sprintf("❌ Invalid threshold: %s", err.Error()))
		return
	}

	// Get format (default summary)
	format := "summary"
	if fmt, ok := options["format"].(string); ok {
		format = fmt
	}

	guildID := i.GuildID
	ctx := context.Background()

	// Acknowledge the command
	err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseDeferredChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Content: "⏳ Checking for inactive users...",
			Flags:   discordgo.MessageFlagsEphemeral,
		},
	})
	if err != nil {
		log.Error().Err(err).Msg("Failed to send deferred response")
		return
	}

	// Process inactivity check asynchronously
	go cmd.processInactivityCheck(s, i, guildID, thresholdDays, format, ctx)
}

// processInactivityCheck performs the inactivity detection
func (cmd *InactiveCommand) processInactivityCheck(s *discordgo.Session, i *discordgo.InteractionCreate, guildID string, thresholdDays int, format string, ctx context.Context) {
	// Get inactive users
	inactiveUsers, err := cmd.detector.DetectInactiveUsers(ctx, guildID, thresholdDays)
	if err != nil {
		log.Error().Err(err).
			Str("guild_id", guildID).
			Int("threshold_days", thresholdDays).
			Msg("Failed to detect inactive users")
		followUpError(s, i, fmt.Sprintf("Failed to check for inactive users: %s", err.Error()))
		return
	}

	// Get statistics
	stats, err := cmd.detector.GetInactivityStats(ctx, guildID, thresholdDays)
	if err != nil {
		log.Error().Err(err).Msg("Failed to get inactivity stats")
		stats = map[string]interface{}{} // Continue without stats
	}

	// Format response based on requested format
	var response string
	switch format {
	case "detailed":
		response = cmd.formatDetailedView(inactiveUsers, thresholdDays, stats)
	case "summary":
		fallthrough
	default:
		response = cmd.formatSummaryView(inactiveUsers, thresholdDays, stats)
	}

	// Send follow-up message
	followUpSuccess(s, i, response)
}

// formatSummaryView formats the inactive users as a summary (top 10)
func (cmd *InactiveCommand) formatSummaryView(users []activity.InactiveUser, thresholdDays int, stats map[string]interface{}) string {
	var builder strings.Builder

	builder.WriteString(fmt.Sprintf("😴 **Inactive Users Report**\n"))
	builder.WriteString(fmt.Sprintf("*Users inactive for %d+ days*\n\n", thresholdDays))

	// Add statistics
	if len(stats) > 0 {
		totalUsers := getInt64(stats, "total_users")
		inactiveCount := getInt64(stats, "inactive_users")
		percentage := getFloat64(stats, "inactive_percentage")

		builder.WriteString(fmt.Sprintf("📊 **Statistics**\n"))
		builder.WriteString(fmt.Sprintf("Total Users: %d\n", totalUsers))
		builder.WriteString(fmt.Sprintf("Inactive: %d (%.1f%%)\n\n", inactiveCount, percentage))
	}

	if len(users) == 0 {
		builder.WriteString("✅ No inactive users found!\n")
		builder.WriteString(fmt.Sprintf("All members have been active within the last %d days.", thresholdDays))
		return builder.String()
	}

	// Show top 10
	displayCount := 10
	if len(users) < displayCount {
		displayCount = len(users)
	}

	builder.WriteString(fmt.Sprintf("**Top %d Longest Inactive:**\n\n", displayCount))

	for i := 0; i < displayCount; i++ {
		user := users[i]
		builder.WriteString(fmt.Sprintf("• **%s**\n", user.Username))
		builder.WriteString(fmt.Sprintf("  Last seen: %s (%d days ago)\n\n",
			user.LastActivityAt.Format("Jan 2, 2006"), user.DaysInactive))
	}

	// Show count if there are more
	if len(users) > displayCount {
		builder.WriteString(fmt.Sprintf("_...and %d more inactive users_\n", len(users)-displayCount))
		builder.WriteString(fmt.Sprintf("_Use `/inactive days:%d format:detailed` for full list_", thresholdDays))
	}

	return builder.String()
}

// formatDetailedView formats all inactive users
func (cmd *InactiveCommand) formatDetailedView(users []activity.InactiveUser, thresholdDays int, stats map[string]interface{}) string {
	var builder strings.Builder

	builder.WriteString(fmt.Sprintf("😴 **Detailed Inactive Users Report**\n"))
	builder.WriteString(fmt.Sprintf("*Users inactive for %d+ days*\n\n", thresholdDays))

	// Add statistics
	if len(stats) > 0 {
		totalUsers := getInt64(stats, "total_users")
		inactiveCount := getInt64(stats, "inactive_users")
		percentage := getFloat64(stats, "inactive_percentage")

		builder.WriteString(fmt.Sprintf("📊 **Statistics**\n"))
		builder.WriteString(fmt.Sprintf("Total: %d | Inactive: %d (%.1f%%)\n\n", totalUsers, inactiveCount, percentage))
	}

	if len(users) == 0 {
		builder.WriteString("✅ No inactive users found!\n")
		return builder.String()
	}

	builder.WriteString(fmt.Sprintf("**All %d Inactive Users:**\n\n", len(users)))

	// Show all users (limit to first 20 to avoid Discord message limits)
	displayCount := len(users)
	if displayCount > 20 {
		displayCount = 20
	}

	for i := 0; i < displayCount; i++ {
		user := users[i]
		builder.WriteString(fmt.Sprintf("%d. **%s** - %d days\n",
			i+1, user.Username, user.DaysInactive))
	}

	if len(users) > displayCount {
		builder.WriteString(fmt.Sprintf("\n_Showing %d of %d inactive users_\n", displayCount, len(users)))
		builder.WriteString("_(Discord message limit reached)_")
	}

	return builder.String()
}
