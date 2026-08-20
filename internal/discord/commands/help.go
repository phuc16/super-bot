package commands

import (
	"fmt"

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
					{Name: "config", Value: "config"},
					{Name: "topic", Value: "topic"},
					{Name: "custom", Value: "custom"},
					{Name: "roast", Value: "roast"},
					{Name: "duel", Value: "duel"},
					{Name: "duel-leaderboard", Value: "duel-leaderboard"},
					{Name: "duel-history", Value: "duel-history"},
					{Name: "duel-reset", Value: "duel-reset"},
					{Name: "duel-challenge-all", Value: "duel-challenge-all"},
					{Name: "werewolf", Value: "werewolf"},
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
		Title:       "🤖 Discord Super Bot - Help",
		Color:       0x5865F2, // Discord blurple
		Description: "A bot for generating AI summaries and managing server engagement.",
		Fields: []*discordgo.MessageEmbedField{
			{
				Name:   "📤 `/export`",
				Value:  "Export and summarize channel messages\n*Available to all members*",
				Inline: false,
			},
			{
				Name:   "⚙️ `/config` 🔒",
				Value:  "Configure bot settings for your server\n*Admin only*",
				Inline: false,
			},
			{
				Name:   "🧠 `/addtopic`, `/listtopic`, `/deltopic`, `/posttopic` 🔒",
				Value:  "Manage recurring AI-generated topic posts\n*Admin only — use `/help topic` for details*",
				Inline: false,
			},
			{
				Name:   "🔑 `/addcom`, `/delcom`, `/listcom` 🔒",
				Value:  "Manage keyword-triggered auto-reply commands\n*Admin only — use `/help custom` for details*",
				Inline: false,
			},
			{
				Name:   "🔥 `/roast`",
				Value:  "AI roast a member based on their recent messages\n*Available to all members*",
				Inline: false,
			},
			{
				Name:   "⚔️ `/duel`",
				Value:  "Challenge a member to a dice duel with point wagers and streaks\n*Available to all members — use `/help duel` for details*",
				Inline: false,
			},
			{
				Name:   "🏆 `/duel-leaderboard`",
				Value:  "View the server's /duel rankings by points\n*Available to all members*",
				Inline: false,
			},
			{
				Name:   "📜 `/duel-history`",
				Value:  "View your recent /duel matches — head-to-head with one member, or your last matches with anyone\n*Available to all members*",
				Inline: false,
			},
			{
				Name:   "🔄 `/duel-reset` 🔒",
				Value:  "Reset everyone's /duel points, wins/losses, and streaks (requires confirmation)\n*Admin only*",
				Inline: false,
			},
			{
				Name:   "🎪 `/duel-challenge-all` 🔒",
				Value:  "Schedule a single-elimination Duel tournament at a fixed time, with champion betting\n*Admin only to start — anyone can join*",
				Inline: false,
			},
			{
				Name:   "🐺 `/werewolf`",
				Value:  "Play a game of Werewolf (Ma Sói) in this channel\n*Available to all members — use `/help werewolf` for details*",
				Inline: false,
			},
			{
				Name:   "💡 Tips",
				Value:  "• Use `/help <command>` for detailed information about a specific command\n• 🔒 marks admin-only commands, requiring Administrator or Manage Server permission",
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

	case "topic":
		return &discordgo.MessageEmbed{
			Title:       "🧠 AI Topic Commands 🔒",
			Color:       0xED4245, // Red for admin commands
			Description: "Manage recurring AI-generated topic posts. **Admin only**",
			Fields: []*discordgo.MessageEmbedField{
				{
					Name:   "`/addtopic`",
					Value:  "Add a topic the bot writes about on a recurring schedule\n`/addtopic topic:\"lịch sử Việt Nam\" channel:#general interval:60 mention:@Tâm`",
					Inline: false,
				},
				{
					Name:   "`/listtopic`",
					Value:  "List all configured topics for this server, numbered for use with `/deltopic`",
					Inline: false,
				},
				{
					Name:   "`/deltopic`",
					Value:  "Remove a topic by its number from `/listtopic`\n`/deltopic number:2`",
					Inline: false,
				},
				{
					Name:   "`/posttopic`",
					Value:  "Immediately post one AI-generated paragraph about any topic, without saving it as recurring\n`/posttopic topic:\"con mèo\" channel:#general`",
					Inline: false,
				},
				{
					Name:   "Parameters",
					Value:  "• `topic` - What the AI should write about\n• `channel` - Where the bot posts\n• `interval` - Minutes between posts (`/addtopic` only, default 5)\n• `mention` - User to tag in the generated text; use `{mention}` in the topic text as the placeholder",
					Inline: false,
				},
				{
					Name:   "Required Permission",
					Value:  "Administrator or Manage Server",
					Inline: false,
				},
			},
		}

	case "custom":
		return &discordgo.MessageEmbed{
			Title:       "🔑 Custom Command 🔒",
			Color:       0xED4245, // Red for admin commands
			Description: "Manage keyword-triggered auto-reply commands. **Admin only** to add/remove; anyone can trigger them.",
			Fields: []*discordgo.MessageEmbedField{
				{
					Name:   "`/addcom`",
					Value:  "Add a keyword the bot replies to, with text and/or an image\n`/addcom keyword:Kami response:\"hi there\"`",
					Inline: false,
				},
				{
					Name:   "`/delcom`",
					Value:  "Remove a keyword command\n`/delcom keyword:Kami`",
					Inline: false,
				},
				{
					Name:   "`/listcom`",
					Value:  "List all configured keyword commands for this server",
					Inline: false,
				},
				{
					Name:   "Parameters",
					Value:  "• `keyword` - Trigger word, without a leading `/`\n• `response` - Text reply (optional if an image is attached)\n• `image` - Attachment to reply with (optional)",
					Inline: false,
				},
				{
					Name:   "Required Permission",
					Value:  "Administrator or Manage Server (to add/remove) — anyone can trigger existing keywords",
					Inline: false,
				},
			},
		}

	case "roast":
		return &discordgo.MessageEmbed{
			Title:       "🔥 `/roast` Command",
			Color:       0x5865F2,
			Description: "AI-generated roast of a member, using their recent messages in the channel as material.",
			Fields: []*discordgo.MessageEmbedField{
				{
					Name:   "Usage",
					Value:  "`/roast target:@user`",
					Inline: false,
				},
				{
					Name:   "How it works",
					Value:  "Pulls the target's recent messages from this channel and asks GPT to roast them based on it. Falls back to a generic (but still funny) roast if no recent messages are found.",
					Inline: false,
				},
				{
					Name:   "Rate Limit",
					Value:  "5 uses per user per minute",
					Inline: true,
				},
			},
		}

	case "duel":
		return &discordgo.MessageEmbed{
			Title:       "⚔️ `/duel` Command",
			Color:       0x5865F2,
			Description: "Challenge another member to a public dice duel with point wagers, win/loss streaks, and a persistent record.",
			Fields: []*discordgo.MessageEmbedField{
				{
					Name:   "Usage",
					Value:  "`/duel opponent:@user wager:20`",
					Inline: false,
				},
				{
					Name:   "How it works",
					Value:  "Posts a public challenge with Accept/Decline buttons — only the challenged member can respond, within 60 seconds. On accept, both sides roll two dice each with a short animation; higher total wins (exact tie is a draw). Rolling double sixes is a critical hit, doubling the wager transferred.",
					Inline: false,
				},
				{
					Name:   "Points & Streaks",
					Value:  "Everyone starts with 100 points per server. Winner takes the wager from the loser (capped at their balance, so no one goes negative). Win/loss/draw record and streaks are tracked persistently — view standings with `/duel-leaderboard`.",
					Inline: false,
				},
				{
					Name:   "Spectator betting",
					Value:  fmt.Sprintf("Anyone except the two duelists can bet on who wins while the challenge is pending, via the bet buttons under the challenge message (%d-%d points, one bet per person). Payouts are pari-mutuel: the losing side's stakes are split among winners proportional to their bet. Declined/expired challenges and draws refund all bets in full.", minBetAmount, maxBetAmount),
					Inline: false,
				},
				{
					Name:   "Parameters",
					Value:  fmt.Sprintf("• `opponent` - Who to challenge\n• `wager` - Points to bet (1-%d, default %d)", maxWager, defaultWager),
					Inline: false,
				},
				{
					Name:   "Rate Limit",
					Value:  "10 challenges per user per minute",
					Inline: true,
				},
			},
		}

	case "duel-leaderboard":
		return &discordgo.MessageEmbed{
			Title:       "🏆 `/duel-leaderboard` Command",
			Color:       0x5865F2,
			Description: "View the server's /duel rankings.",
			Fields: []*discordgo.MessageEmbedField{
				{
					Name:   "Usage",
					Value:  "`/duel-leaderboard`",
					Inline: false,
				},
				{
					Name:   "Shows",
					Value:  "Top 10 by points, plus your own rank (with 🔥 win streak / 🥶 loss streak indicators) if you're outside the top 10.",
					Inline: false,
				},
			},
		}

	case "duel-history":
		return &discordgo.MessageEmbed{
			Title:       "📜 `/duel-history` Command",
			Color:       0x5865F2,
			Description: "View your /duel match history.",
			Fields: []*discordgo.MessageEmbedField{
				{
					Name:   "Usage",
					Value:  "`/duel-history` — your last 10 matches with anyone\n`/duel-history opponent:@user` — head-to-head record against that person",
					Inline: false,
				},
				{
					Name:   "Shows",
					Value:  "With `opponent`: your overall win/loss/draw tally against them, plus the last 5 matches between you. Without it: your last 10 matches against any opponent. Both show dice totals, wager, and outcome per match.",
					Inline: false,
				},
			},
		}

	case "duel-reset":
		return &discordgo.MessageEmbed{
			Title:       "🔄 `/duel-reset` Command 🔒",
			Color:       0xED4245, // Red for admin commands
			Description: "Reset every member's /duel points, win/loss/draw record, and streak in this server. **Admin only**",
			Fields: []*discordgo.MessageEmbedField{
				{
					Name:   "Usage",
					Value:  "`/duel-reset`\n`/duel-reset wipe_history:true`",
					Inline: false,
				},
				{
					Name:   "Confirmation required",
					Value:  "Posts a Confirm/Cancel prompt — only the admin who ran the command can confirm, within 30 seconds. This action cannot be undone.",
					Inline: false,
				},
				{
					Name:   "Parameters",
					Value:  "• `wipe_history` - Also delete all `/duel-history` match records (default: false, keeps history)",
					Inline: false,
				},
				{
					Name:   "Required Permission",
					Value:  "Administrator or Manage Server",
					Inline: false,
				},
			},
		}

	case "duel-challenge-all":
		return &discordgo.MessageEmbed{
			Title:       "🎪 `/duel-challenge-all` Command 🔒",
			Color:       0xED4245, // Red for admin commands
			Description: "Schedule a single-elimination dice tournament at a fixed time — anyone can join, spectators can bet on the champion, then the bot runs the whole bracket automatically. **Admin only** to start.",
			Fields: []*discordgo.MessageEmbedField{
				{
					Name:   "Usage",
					Value:  "`/duel-challenge-all start:\"20:00 20/08/2026\"`",
					Inline: false,
				},
				{
					Name:   "Timeline",
					Value:  fmt.Sprintf("• Signup open from posting until **%d min** before start (Join / Rời đi)\n• Champion betting open for the next **%d min**, closing **%d min** before start (pick from a dropdown of joined players)\n• Locked for the final %d min, then the bracket runs automatically", int(joinCloseBefore.Minutes()), int((joinCloseBefore - betCloseBefore).Minutes()), int(betCloseBefore.Minutes()), int(betCloseBefore.Minutes())),
					Inline: false,
				},
				{
					Name:   "How the bracket works",
					Value:  "Players are shuffled into a bracket. Each match is a real `/duel` roll (no wager) — a draw is immediately rerolled since a bracket can't advance on a tie. Odd player counts get a random bye each round. Results post round-by-round until a champion is crowned, then champion bets are paid out pari-mutuel (or refunded if nobody picked correctly).",
					Inline: false,
				},
				{
					Name:   "Limits",
					Value:  fmt.Sprintf("%d-%d players. Start time must be %d min – %d hours from now (tournament state is in-memory only — it will be lost if the bot restarts before it finishes, so avoid redeploying mid-tournament). Only the organizer can cancel.", tournamentMinPlayers, tournamentMaxPlayers, int(minLeadTime.Minutes()), int(maxLeadTime.Hours())),
					Inline: false,
				},
			},
		}

	case "werewolf":
		return &discordgo.MessageEmbed{
			Title:       "🐺 `/werewolf` Command",
			Color:       0x5865F2,
			Description: "Play a game of Werewolf (Ma Sói): Werewolves, Villagers, a Seer, and a Doctor.",
			Fields: []*discordgo.MessageEmbedField{
				{
					Name:   "Subcommands",
					Value:  "• `/werewolf start reveal-on-death:true|false` - Open a lobby in this channel\n• `/werewolf status` - Check the current phase/round without leaking roles\n• `/werewolf leave` - Leave the lobby, or leave a running game (counts as being eliminated)",
					Inline: false,
				},
				{
					Name:   "How it works",
					Value:  fmt.Sprintf("Anyone can Join/Leave the lobby; the starter clicks Start once %d-%d have joined. Roles are DMed privately. Each night the Werewolves get a private thread to pick a kill target, the Seer and Doctor act via DM. Each day: discuss, then vote to lynch — a tie means nobody is lynched. Repeats until one side wins.", werewolfMinPlayers, werewolfMaxPlayers),
					Inline: false,
				},
				{
					Name:   "Privacy note",
					Value:  "The werewolves' coordination thread is private from other players, but anyone with the server's \"Manage Threads\" permission (usually admins/mods) can still see it — that's a Discord permission-model limit, not a bot bug.",
					Inline: false,
				},
				{
					Name:   "Limits",
					Value:  "In-memory only — a bot restart while a game is running loses it, same as `/duel-challenge-all`. Missing 2 actions/votes in a row auto-removes a player. One game per channel at a time.",
					Inline: false,
				},
			},
		}

	default:
		return cmd.getGeneralHelp()
	}
}
