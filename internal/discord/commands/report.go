package commands

import (
	"context"
	"fmt"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/rs/zerolog/log"

	"github.com/phuc16/VCG-super-bot/internal/models"
	configService "github.com/phuc16/VCG-super-bot/internal/services/config"
	"github.com/phuc16/VCG-super-bot/internal/services/reporting"
)

// ReportCommand handles the /report slash command
type ReportCommand struct {
	generator     *reporting.Generator
	poster        *reporting.Poster
	configService *configService.Service
}

// NewReportCommand creates a new ReportCommand
func NewReportCommand(generator *reporting.Generator, poster *reporting.Poster, configService *configService.Service) *ReportCommand {
	return &ReportCommand{
		generator:     generator,
		poster:        poster,
		configService: configService,
	}
}

// Definition returns the command definition for Discord
func (cmd *ReportCommand) Definition() *discordgo.ApplicationCommand {
	return &discordgo.ApplicationCommand{
		Name:        "report",
		Description: "Generate and view weekly activity reports (Admin only)",
		Options: []*discordgo.ApplicationCommandOption{
			{
				Type:        discordgo.ApplicationCommandOptionString,
				Name:        "period",
				Description: "Which week to report on",
				Required:    false,
				Choices: []*discordgo.ApplicationCommandOptionChoice{
					{Name: "Current Week", Value: "current"},
					{Name: "Last Week", Value: "last"},
				},
			},
			{
				Type:        discordgo.ApplicationCommandOptionBoolean,
				Name:        "post",
				Description: "Post report to configured reports channel (default: true)",
				Required:    false,
			},
		},
	}
}

// Handle processes the command interaction
func (cmd *ReportCommand) Handle(s *discordgo.Session, i *discordgo.InteractionCreate) {
	// Check admin permissions
	if !cmd.hasAdminPermission(i.Member) {
		respondError(s, i, "❌ This command requires Administrator or Manage Server permission.")
		return
	}

	// Parse options
	options := parseOptions(i.ApplicationCommandData().Options)

	period, ok := options["period"].(string)
	if !ok {
		period = "current" // Default to current week
	}

	postToChannel := true
	if post, ok := options["post"].(bool); ok {
		postToChannel = post
	}

	guildID := i.GuildID
	userID := i.Member.User.ID
	ctx := context.Background()

	// Acknowledge the command
	err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseDeferredChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Content: "⏳ Generating report...",
			Flags:   discordgo.MessageFlagsEphemeral,
		},
	})
	if err != nil {
		log.Error().Err(err).Msg("Failed to send deferred response")
		return
	}

	// Generate report asynchronously
	go cmd.processReport(s, i, guildID, userID, period, postToChannel, ctx)
}

// processReport generates and optionally posts the report
func (cmd *ReportCommand) processReport(s *discordgo.Session, i *discordgo.InteractionCreate, guildID string, userID string, period string, postToChannel bool, ctx context.Context) {
	// Determine which week to report on
	var year, week int
	now := time.Now()

	switch period {
	case "last":
		lastWeek := now.AddDate(0, 0, -7)
		year, week = lastWeek.ISOWeek()
	case "current":
		fallthrough
	default:
		year, week = now.ISOWeek()
	}

	// Generate report
	var report *models.WeeklyReport
	var err error

	if period == "current" {
		// Generate from current activity
		report, err = cmd.generator.GenerateCurrentWeekReport(ctx, guildID, userID)
	} else {
		// Generate from archived data
		report, err = cmd.generator.GenerateWeeklyReport(ctx, guildID, year, week, userID)
	}

	if err != nil {
		log.Error().Err(err).
			Str("guild_id", guildID).
			Str("period", period).
			Msg("Failed to generate report")
		followUpError(s, i, fmt.Sprintf("Failed to generate report: %s", err.Error()))
		return
	}

	// Post to channel if requested
	if postToChannel {
		// Get configured reports channel
		channelID, err := cmd.getReportsChannelID(ctx, guildID)
		if err != nil || channelID == "" {
			followUpError(s, i, "⚠️ Reports channel not configured. Report generated but not posted.\nUse `/config set-reports-channel` to configure.")
			return
		}

		err = cmd.poster.PostReport(ctx, report, channelID)
		if err != nil {
			log.Error().Err(err).
				Str("report_id", report.ID.String()).
				Str("channel_id", channelID).
				Msg("Failed to post report")
			followUpError(s, i, fmt.Sprintf("Failed to post report: %s", err.Error()))
			return
		}

		followUpSuccess(s, i, fmt.Sprintf("✅ Report generated and posted to <#%s>!\n\nWeek %d, %d • %d active users • %d messages",
			channelID, report.WeekNum, report.Year, report.ActiveUsers, report.TotalMessages))
	} else {
		// Just show summary
		followUpSuccess(s, i, fmt.Sprintf("✅ Report generated!\n\nWeek %d, %d • %d active users • %d messages\n\nReport ID: %s",
			report.WeekNum, report.Year, report.ActiveUsers, report.TotalMessages, report.ID.String()))
	}
}

// hasAdminPermission checks if the user has admin or manage server permission
func (cmd *ReportCommand) hasAdminPermission(member *discordgo.Member) bool {
	if member == nil {
		return false
	}

	permissions := int64(member.Permissions)

	// Check for Administrator or Manage Server permission
	hasAdmin := (permissions & discordgo.PermissionAdministrator) != 0
	hasManageServer := (permissions & discordgo.PermissionManageServer) != 0

	return hasAdmin || hasManageServer
}

// getReportsChannelID retrieves the configured reports channel ID for the guild
func (cmd *ReportCommand) getReportsChannelID(ctx context.Context, guildID string) (string, error) {
	config, err := cmd.configService.Get(ctx, guildID)
	if err != nil {
		return "", fmt.Errorf("failed to get server config: %w", err)
	}

	if config.ReportsChannelID == nil || *config.ReportsChannelID == "" {
		return "", fmt.Errorf("reports channel not configured")
	}

	return *config.ReportsChannelID, nil
}
