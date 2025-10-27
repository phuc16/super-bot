package commands

import (
	"context"
	"fmt"
	"strings"

	"github.com/bwmarrin/discordgo"
	"github.com/rs/zerolog/log"

	configService "github.com/phuc16/VCG-super-bot/internal/services/config"
)

// ConfigCommand handles the /config slash command and subcommands
type ConfigCommand struct {
	configService *configService.Service
}

// NewConfigCommand creates a new ConfigCommand
func NewConfigCommand(service *configService.Service) *ConfigCommand {
	return &ConfigCommand{
		configService: service,
	}
}

// Definition returns the command definition for Discord
func (cmd *ConfigCommand) Definition() *discordgo.ApplicationCommand {
	return &discordgo.ApplicationCommand{
		Name:        "config",
		Description: "Configure bot settings (Admin only)",
		Options: []*discordgo.ApplicationCommandOption{
			{
				Type:        discordgo.ApplicationCommandOptionSubCommand,
				Name:        "show",
				Description: "Display current configuration",
			},
			{
				Type:        discordgo.ApplicationCommandOptionSubCommand,
				Name:        "set-reports-channel",
				Description: "Set the channel for weekly reports",
				Options: []*discordgo.ApplicationCommandOption{
					{
						Type:        discordgo.ApplicationCommandOptionChannel,
						Name:        "channel",
						Description: "The channel to post reports to",
						Required:    true,
						ChannelTypes: []discordgo.ChannelType{
							discordgo.ChannelTypeGuildText,
						},
					},
				},
			},
			{
				Type:        discordgo.ApplicationCommandOptionSubCommand,
				Name:        "set-active-role",
				Description: "Set the name for the 'Active of the Week' role",
				Options: []*discordgo.ApplicationCommandOption{
					{
						Type:        discordgo.ApplicationCommandOptionString,
						Name:        "name",
						Description: "The role name (e.g., 'Active of the Week')",
						Required:    true,
						MaxLength:   100,
					},
				},
			},
			{
				Type:        discordgo.ApplicationCommandOptionSubCommand,
				Name:        "set-inactive-threshold",
				Description: "Set inactivity threshold in days",
				Options: []*discordgo.ApplicationCommandOption{
					{
						Type:        discordgo.ApplicationCommandOptionInteger,
						Name:        "days",
						Description: "Number of days (7-90)",
						Required:    true,
						MinValue:    floatPtr(7),
						MaxValue:    90,
					},
				},
			},
			{
				Type:        discordgo.ApplicationCommandOptionSubCommand,
				Name:        "set-schedule",
				Description: "Set weekly schedule (advanced)",
				Options: []*discordgo.ApplicationCommandOption{
					{
						Type:        discordgo.ApplicationCommandOptionString,
						Name:        "cron",
						Description: "Cron expression (e.g., '0 0 * * 1' for Monday 00:00)",
						Required:    true,
					},
					{
						Type:        discordgo.ApplicationCommandOptionString,
						Name:        "timezone",
						Description: "Timezone (e.g., 'America/New_York', 'UTC')",
						Required:    false,
					},
				},
			},
		},
	}
}

// Handle processes the command interaction
func (cmd *ConfigCommand) Handle(s *discordgo.Session, i *discordgo.InteractionCreate) {
	// Check admin permissions
	if !hasAdminPermission(i.Member) {
		respondError(s, i, "❌ This command requires Administrator or Manage Server permission.")
		return
	}

	guildID := i.GuildID
	ctx := context.Background()

	// Get subcommand
	options := i.ApplicationCommandData().Options
	if len(options) == 0 {
		respondError(s, i, "❌ No subcommand specified")
		return
	}

	subcommand := options[0]

	switch subcommand.Name {
	case "show":
		cmd.handleShow(s, i, guildID, ctx)
	case "set-reports-channel":
		cmd.handleSetReportsChannel(s, i, guildID, ctx, subcommand.Options)
	case "set-active-role":
		cmd.handleSetActiveRole(s, i, guildID, ctx, subcommand.Options)
	case "set-inactive-threshold":
		cmd.handleSetInactiveThreshold(s, i, guildID, ctx, subcommand.Options)
	case "set-schedule":
		cmd.handleSetSchedule(s, i, guildID, ctx, subcommand.Options)
	default:
		respondError(s, i, fmt.Sprintf("❌ Unknown subcommand: %s", subcommand.Name))
	}
}

// handleShow displays the current configuration
func (cmd *ConfigCommand) handleShow(s *discordgo.Session, i *discordgo.InteractionCreate, guildID string, ctx context.Context) {
	config, err := cmd.configService.Get(ctx, guildID)
	if err != nil {
		log.Error().Err(err).Str("guild_id", guildID).Msg("Failed to get config")
		respondError(s, i, "Failed to retrieve configuration")
		return
	}

	// Build response embed
	embed := &discordgo.MessageEmbed{
		Title:       "⚙️ Server Configuration",
		Color:       0x5865F2, // Discord blurple
		Description: "Current bot settings for this server",
		Fields:      []*discordgo.MessageEmbedField{},
		Footer: &discordgo.MessageEmbedFooter{
			Text: "Use /config set-* commands to modify settings",
		},
	}

	// Reports channel
	reportsChannelText := "*Not configured*"
	if config.ReportsChannelID != nil && *config.ReportsChannelID != "" {
		reportsChannelText = fmt.Sprintf("<#%s>", *config.ReportsChannelID)
	}
	embed.Fields = append(embed.Fields, &discordgo.MessageEmbedField{
		Name:   "📊 Reports Channel",
		Value:  reportsChannelText,
		Inline: true,
	})

	// Active role
	activeRoleText := config.ActiveRoleName
	if config.ActiveRoleID != nil && *config.ActiveRoleID != "" {
		activeRoleText = fmt.Sprintf("%s (<@&%s>)", config.ActiveRoleName, *config.ActiveRoleID)
	}
	embed.Fields = append(embed.Fields, &discordgo.MessageEmbedField{
		Name:   "🏆 Active Role",
		Value:  activeRoleText,
		Inline: true,
	})

	// Inactivity threshold
	embed.Fields = append(embed.Fields, &discordgo.MessageEmbedField{
		Name:   "⏰ Inactivity Threshold",
		Value:  fmt.Sprintf("%d days", config.InactivityThresholdDays),
		Inline: true,
	})

	// Schedule
	embed.Fields = append(embed.Fields, &discordgo.MessageEmbedField{
		Name:   "📅 Weekly Schedule",
		Value:  fmt.Sprintf("`%s` (%s)", config.WeeklyScheduleCron, config.Timezone),
		Inline: false,
	})

	// Enabled features
	features := []string{}
	if config.EnabledFeatures.Export {
		features = append(features, "✅ Message Export")
	} else {
		features = append(features, "❌ Message Export")
	}
	if config.EnabledFeatures.Tracking {
		features = append(features, "✅ Activity Tracking")
	} else {
		features = append(features, "❌ Activity Tracking")
	}
	if config.EnabledFeatures.Reports {
		features = append(features, "✅ Weekly Reports")
	} else {
		features = append(features, "❌ Weekly Reports")
	}
	if config.EnabledFeatures.Roles {
		features = append(features, "✅ Role Assignment")
	} else {
		features = append(features, "❌ Role Assignment")
	}

	embed.Fields = append(embed.Fields, &discordgo.MessageEmbedField{
		Name:   "🎛️ Enabled Features",
		Value:  strings.Join(features, "\n"),
		Inline: false,
	})

	err = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Embeds: []*discordgo.MessageEmbed{embed},
			Flags:  discordgo.MessageFlagsEphemeral,
		},
	})
	if err != nil {
		log.Error().Err(err).Msg("Failed to send config show response")
	}
}

// handleSetReportsChannel sets the reports channel
func (cmd *ConfigCommand) handleSetReportsChannel(s *discordgo.Session, i *discordgo.InteractionCreate, guildID string, ctx context.Context, options []*discordgo.ApplicationCommandInteractionDataOption) {
	if len(options) == 0 {
		respondError(s, i, "❌ Channel parameter missing")
		return
	}

	channelID := options[0].ChannelValue(s).ID

	err := cmd.configService.SetReportsChannel(ctx, guildID, channelID)
	if err != nil {
		log.Error().Err(err).Str("guild_id", guildID).Str("channel_id", channelID).Msg("Failed to set reports channel")
		respondError(s, i, fmt.Sprintf("Failed to set reports channel: %s", err.Error()))
		return
	}

	respondSuccess(s, i, fmt.Sprintf("✅ Reports channel set to <#%s>", channelID))
}

// handleSetActiveRole sets the active role name
func (cmd *ConfigCommand) handleSetActiveRole(s *discordgo.Session, i *discordgo.InteractionCreate, guildID string, ctx context.Context, options []*discordgo.ApplicationCommandInteractionDataOption) {
	if len(options) == 0 {
		respondError(s, i, "❌ Role name parameter missing")
		return
	}

	roleName := options[0].StringValue()

	err := cmd.configService.SetActiveRoleName(ctx, guildID, roleName)
	if err != nil {
		log.Error().Err(err).Str("guild_id", guildID).Str("role_name", roleName).Msg("Failed to set active role name")
		respondError(s, i, fmt.Sprintf("Failed to set active role name: %s", err.Error()))
		return
	}

	respondSuccess(s, i, fmt.Sprintf("✅ Active role name set to **%s**\n\n*Note: The role will be created automatically when the weekly task runs*", roleName))
}

// handleSetInactiveThreshold sets the inactivity threshold
func (cmd *ConfigCommand) handleSetInactiveThreshold(s *discordgo.Session, i *discordgo.InteractionCreate, guildID string, ctx context.Context, options []*discordgo.ApplicationCommandInteractionDataOption) {
	if len(options) == 0 {
		respondError(s, i, "❌ Days parameter missing")
		return
	}

	days := int(options[0].IntValue())

	err := cmd.configService.SetInactivityThreshold(ctx, guildID, days)
	if err != nil {
		log.Error().Err(err).Str("guild_id", guildID).Int("days", days).Msg("Failed to set inactivity threshold")
		respondError(s, i, fmt.Sprintf("Failed to set inactivity threshold: %s", err.Error()))
		return
	}

	respondSuccess(s, i, fmt.Sprintf("✅ Inactivity threshold set to **%d days**", days))
}

// handleSetSchedule sets the weekly schedule
func (cmd *ConfigCommand) handleSetSchedule(s *discordgo.Session, i *discordgo.InteractionCreate, guildID string, ctx context.Context, options []*discordgo.ApplicationCommandInteractionDataOption) {
	if len(options) == 0 {
		respondError(s, i, "❌ Cron expression parameter missing")
		return
	}

	cronExpr := options[0].StringValue()
	timezone := "UTC"

	if len(options) > 1 {
		timezone = options[1].StringValue()
	}

	err := cmd.configService.SetWeeklySchedule(ctx, guildID, cronExpr, timezone)
	if err != nil {
		log.Error().Err(err).
			Str("guild_id", guildID).
			Str("cron", cronExpr).
			Str("timezone", timezone).
			Msg("Failed to set weekly schedule")
		respondError(s, i, fmt.Sprintf("Failed to set weekly schedule: %s", err.Error()))
		return
	}

	respondSuccess(s, i, fmt.Sprintf("✅ Weekly schedule set to `%s` (%s)\n\n*Note: The bot must be restarted for schedule changes to take effect*", cronExpr, timezone))
}

// Helper function for float pointer
func floatPtr(f float64) *float64 {
	return &f
}
