package commands

import (
	"github.com/bwmarrin/discordgo"
	"github.com/rs/zerolog/log"
)

// HelpCommand handles the /help slash command
type HelpCommand struct{}

// NewHelpCommand creates a new HelpCommand
func NewHelpCommand() *HelpCommand {
	return &HelpCommand{}
}

// Definition returns the command definition for Discord
func (cmd *HelpCommand) Definition() *discordgo.ApplicationCommand {
	return &discordgo.ApplicationCommand{
		Name:        "help",
		Description: "Display bot commands and usage information",
		Options: []*discordgo.ApplicationCommandOption{
			{
				Type:        discordgo.ApplicationCommandOptionString,
				Name:        "command",
				Description: "Get help for a specific command",
				Required:    false,
				Choices: []*discordgo.ApplicationCommandOptionChoice{
					{Name: "export", Value: "export"},
					{Name: "activity", Value: "activity"},
					{Name: "report", Value: "report"},
					{Name: "inactive", Value: "inactive"},
					{Name: "config", Value: "config"},
				},
			},
		},
	}
}

// Handle processes the command interaction
func (cmd *HelpCommand) Handle(s *discordgo.Session, i *discordgo.InteractionCreate) {
	// Parse options
	options := parseOptions(i.ApplicationCommandData().Options)

	var embed *discordgo.MessageEmbed

	if commandName, ok := options["command"].(string); ok {
		// Show help for specific command
		embed = cmd.getCommandHelp(commandName)
	} else {
		// Show general help
		embed = cmd.getGeneralHelp()
	}

	err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Embeds: []*discordgo.MessageEmbed{embed},
			Flags:  discordgo.MessageFlagsEphemeral,
		},
	})
	if err != nil {
		log.Error().Err(err).Msg("Failed to send help response")
	}
}

// getGeneralHelp returns the general help embed
func (cmd *HelpCommand) getGeneralHelp() *discordgo.MessageEmbed {
	return &discordgo.MessageEmbed{
		Title:       "🤖 Discord Activity Bot - Help",
		Color:       0x5865F2, // Discord blurple
		Description: "A bot for tracking Discord server activity, generating AI summaries, and managing engagement.",
		Fields: []*discordgo.MessageEmbedField{
			{
				Name:   "📤 `/export`",
				Value:  "Export and summarize channel messages\n*Available to all members*",
				Inline: false,
			},
			{
				Name:   "📊 `/activity`",
				Value:  "View activity statistics and leaderboard\n*Available to all members*",
				Inline: false,
			},
			{
				Name:   "😴 `/inactive`",
				Value:  "List inactive users based on threshold\n*Available to all members*",
				Inline: false,
			},
			{
				Name:   "📋 `/report` 🔒",
				Value:  "Generate and post weekly activity reports\n*Admin only*",
				Inline: false,
			},
			{
				Name:   "⚙️ `/config` 🔒",
				Value:  "Configure bot settings for your server\n*Admin only*",
				Inline: false,
			},
			{
				Name:   "💡 Tips",
				Value:  "• Use `/help <command>` for detailed information about a specific command\n• Admin commands require Administrator or Manage Server permission\n• The bot tracks messages and voice activity automatically",
				Inline: false,
			},
		},
		Footer: &discordgo.MessageEmbedFooter{
			Text: "Use /help <command> for detailed information",
		},
	}
}

// getCommandHelp returns help for a specific command
func (cmd *HelpCommand) getCommandHelp(commandName string) *discordgo.MessageEmbed {
	switch commandName {
	case "export":
		return &discordgo.MessageEmbed{
			Title:       "📤 `/export` Command",
			Color:       0x5865F2,
			Description: "Export channel messages with AI-generated summaries using GPT-4.",
			Fields: []*discordgo.MessageEmbedField{
				{
					Name:   "Usage",
					Value:  "**Date Range Export:**\n`/export type:date-range from:\"YYYY-MM-DD HH:MM\" end:\"YYYY-MM-DD HH:MM\"`\n\n**Unread Messages:**\n`/export type:unread`",
					Inline: false,
				},
				{
					Name:   "Parameters",
					Value:  "• `type` - Export type (date-range or unread)\n• `from` - Start date/time (date-range only)\n• `end` - End date/time (date-range only)",
					Inline: false,
				},
				{
					Name:   "Examples",
					Value:  "`/export type:date-range from:\"2025-10-20\" end:\"2025-10-27\"`\n`/export type:date-range from:\"2025-10-20 14:00\" end:\"2025-10-27 18:00\"`\n`/export type:unread`",
					Inline: false,
				},
				{
					Name:   "Rate Limit",
					Value:  "3 exports per user per hour",
					Inline: true,
				},
				{
					Name:   "Max Messages",
					Value:  "10,000 messages",
					Inline: true,
				},
			},
			Footer: &discordgo.MessageEmbedFooter{
				Text: "Processing time varies based on message count (typically 10-60 seconds)",
			},
		}

	case "activity":
		return &discordgo.MessageEmbed{
			Title:       "📊 `/activity` Command",
			Color:       0x5865F2,
			Description: "View your activity statistics and server leaderboard.",
			Fields: []*discordgo.MessageEmbedField{
				{
					Name:   "Usage",
					Value:  "**Personal Stats:**\n`/activity view:personal`\n\n**Server Overview:**\n`/activity view:overview`\n\n**Leaderboard:**\n`/activity view:leaderboard`",
					Inline: false,
				},
				{
					Name:   "What's Tracked",
					Value:  "• Message count (current week, previous week, total)\n• Voice channel participation\n• Last activity timestamp\n• Server rank",
					Inline: false,
				},
				{
					Name:   "Active Role",
					Value:  "Top 5 users by weekly message count receive the \"Active of the Week\" role (configurable). Role is assigned automatically every Monday at 00:00.",
					Inline: false,
				},
			},
			Footer: &discordgo.MessageEmbedFooter{
				Text: "Activity is tracked automatically in all public channels",
			},
		}

	case "inactive":
		return &discordgo.MessageEmbed{
			Title:       "😴 `/inactive` Command",
			Color:       0x5865F2,
			Description: "Identify users who have been inactive for a specified period.",
			Fields: []*discordgo.MessageEmbedField{
				{
					Name:   "Usage",
					Value:  "**Default Threshold (30 days):**\n`/inactive`\n\n**Custom Threshold:**\n`/inactive days:45`\n\n**Detailed View:**\n`/inactive format:detailed`",
					Inline: false,
				},
				{
					Name:   "Parameters",
					Value:  "• `days` - Inactivity threshold (7-90 days, default: 30)\n• `format` - Display format (summary or detailed)",
					Inline: false,
				},
				{
					Name:   "Display Formats",
					Value:  "• **Summary** - Top 10 longest inactive users\n• **Detailed** - Up to 20 users with full details",
					Inline: false,
				},
				{
					Name:   "Note",
					Value:  "This command is report-only and does not take any automated actions. Bot accounts are excluded from results.",
					Inline: false,
				},
			},
		}

	case "report":
		return &discordgo.MessageEmbed{
			Title:       "📋 `/report` Command 🔒",
			Color:       0xED4245, // Red for admin commands
			Description: "Generate and post weekly activity reports to the configured reports channel. **Admin only**",
			Fields: []*discordgo.MessageEmbedField{
				{
					Name:   "Usage",
					Value:  "**Current Week:**\n`/report period:current`\n\n**Last Week:**\n`/report period:last`\n\n**Generate Without Posting:**\n`/report period:current post:false`",
					Inline: false,
				},
				{
					Name:   "Parameters",
					Value:  "• `period` - Which week to report (current or last)\n• `post` - Post to reports channel (true/false, default: true)",
					Inline: false,
				},
				{
					Name:   "Report Contents",
					Value:  "• Week number and year\n• Total active users\n• Total messages\n• Top 5 users ranking\n• User statistics",
					Inline: false,
				},
				{
					Name:   "Automation",
					Value:  "Weekly reports are automatically generated and posted every Monday at 00:05 (configurable via `/config`).",
					Inline: false,
				},
				{
					Name:   "Required Permission",
					Value:  "Administrator or Manage Server",
					Inline: true,
				},
				{
					Name:   "Configuration",
					Value:  "Set reports channel with `/config set-reports-channel`",
					Inline: true,
				},
			},
		}

	case "config":
		return &discordgo.MessageEmbed{
			Title:       "⚙️ `/config` Command 🔒",
			Color:       0xED4245, // Red for admin commands
			Description: "Configure bot settings for your server. **Admin only**",
			Fields: []*discordgo.MessageEmbedField{
				{
					Name:   "Subcommands",
					Value:  "• `/config show` - Display current configuration\n• `/config set-reports-channel` - Set reports channel\n• `/config set-active-role` - Set active role name\n• `/config set-inactive-threshold` - Set inactivity threshold\n• `/config set-schedule` - Set weekly schedule (advanced)",
					Inline: false,
				},
				{
					Name:   "Examples",
					Value:  "`/config show`\n`/config set-reports-channel channel:#reports`\n`/config set-active-role name:\"Top Contributor\"`\n`/config set-inactive-threshold days:45`",
					Inline: false,
				},
				{
					Name:   "Configurable Settings",
					Value:  "• Reports channel for weekly reports\n• Active role name (default: \"Active of the Week\")\n• Inactivity threshold (7-90 days, default: 30)\n• Weekly schedule cron expression\n• Timezone for scheduling",
					Inline: false,
				},
				{
					Name:   "Required Permission",
					Value:  "Administrator or Manage Server",
					Inline: false,
				},
			},
			Footer: &discordgo.MessageEmbedFooter{
				Text: "Changes to weekly schedule require bot restart",
			},
		}

	default:
		return cmd.getGeneralHelp()
	}
}
