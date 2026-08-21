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
			Description: "Play a game of Werewolf (Ma Sói) with up to 11 roles across two factions — Wolves and Villagers. Werewolf, Wolf Cub, Cursed Wolf, Traitor, Villager, Seer, Guard/Doctor, Witch, Hunter, Disruptor, Cupid.",
			Fields: []*discordgo.MessageEmbedField{
				{
					Name:   "Subcommands",
					Value:  "• `/werewolf start reveal-on-death:true|false custom-roles:true|false` - Open a lobby in this channel; custom-roles opens an ephemeral role-count editor for the starter after the roster locks, instead of the default player-count table\n• `/werewolf status` - Check the current phase/round without leaking roles\n• `/werewolf leave` - Leave the lobby, or leave a running game (counts as being eliminated)",
					Inline: false,
				},
				{
					Name:   "Game flow",
					Value:  fmt.Sprintf("1️⃣ **Lobby** — anyone Join/Leave; the starter clicks Start once %d-%d have joined.\n2️⃣ **(Optional) Custom roles** — with `custom-roles:true`, the starter picks exact role counts via an ephemeral editor before dealing.\n3️⃣ **Role reveal** — everyone is DMed their role privately, with a short note on how that role acts.\n4️⃣ **Night** — the wolf pack votes on a kill target together in a private thread; Seer/Doctor/Witch/Hunter/Disruptor act via DM; Cupid acts via DM on the first night only.\n5️⃣ **Day** — discuss in a private alive-players thread, then vote to lynch (or skip).\n6️⃣ Repeat night/day until one side wins, then a full round-by-round recap posts.", werewolfMinPlayers, werewolfMaxPlayers),
					Inline: false,
				},
				{
					Name:   "Winning rule",
					Value:  "🏡 **Villagers win** the moment every wolf-faction player is dead.\n🐺 **Wolves win** the moment wolf-faction players are at least as many as everyone else still alive — they don't need to eliminate everyone, just stop being outnumbered.\nFaction, not role name, decides this: Wolf Cub and Traitor are wolf-faction from the start; Cursed Wolf starts as a normal villager and only becomes a wolf if it's bitten by the pack and survives (converts instead of dying).",
					Inline: false,
				},
				{
					Name:   "Discussion & voting",
					Value:  "**Day-discuss**: everyone still alive talks in a private thread — the window scales with how many players are left. The main channel is locked to bot-only chat and only posts phase/result announcements.\n**Day-vote**: pick who to lynch from a select menu, or explicitly choose \"Không treo cổ ai\" (don't lynch anyone). Most votes wins; an exact tie, or enough skip votes, means nobody is lynched that day.\n**Night (wolves)**: the pack votes together in their own private thread — every wolf votes once (locked in, no changing your mind), and the pack's target is decided by simple majority; a tie among wolves means no kill that night.",
					Inline: false,
				},
				{
					Name:   "Wolf-side roles",
					Value:  "🐺 **Werewolf** — votes with the pack each night to pick a kill target.\n🐺👶 **Wolf Cub** — votes like a Werewolf; if it dies (any cause), the pack gets a bonus 2-target bite the *following* night.\n🐺✨ **Cursed Wolf** — starts as a normal villager (the Seer reads it as \"not a wolf\"). If the pack bites it and nobody saves it, it doesn't die — it converts into a wolf and joins the pack's vote from then on.\n🎭 **Traitor** — knows who the wolves are (DMed the roster at game start, and again every night after) but has no night action and doesn't vote with the pack.",
					Inline: false,
				},
				{
					Name:   "Villager-side special roles",
					Value:  "🔮 **Seer** — DM each night: check one player, learn if they're wolf-faction or not.\n💉 **Guard/Doctor** — DM each night: protect one player from the wolves' bite (can't protect the same person two nights in a row).\n🧙 **Witch** — DM each night, after the bite is known: heal the bitten player (once per game) and/or poison anyone else (once per game, bypasses the Guard's protection).\n🏹 **Hunter** — DM: pick a target any time; if the Hunter dies (any cause), that target dies too. The pick persists and can be changed on a later night.\n🤫 **Disruptor** — DM each night: pick someone to be muted in the day-discuss thread the *following* day.\n💘 **Cupid** — DM on the first night only: pick exactly 2 players to fall in love. If either dies, the other dies too, immediately.",
					Inline: false,
				},
				{
					Name:   "Villager",
					Value:  "👤 No special ability — survive, discuss, and vote.",
					Inline: false,
				},
				{
					Name:   "Roles & interaction guide",
					Value:  "🐺 Werewolf / Wolf Cub / Cursed Wolf (once revealed): act together each night in a private wolf thread.\n🔮 Seer / 💉 Guard / 🧙 Witch / 🏹 Hunter / 🤫 Disruptor: get a private DM each night to choose their action.\n💘 Cupid: gets a private DM only on the very first night.\n🎭 Traitor: gets a DM naming the current wolves at game start, and again every night after — no night action of their own.\n👤 Villager: no special action — just discuss and vote during the day.\nEveryone chats publicly in the alive-players thread during the day; the main channel only shows phase/vote announcements.",
					Inline: false,
				},
				{
					Name:   "Channel lock",
					Value:  "The bot locks the main channel to bot-only chat for the duration of the game — all real conversation happens in two private threads: one for the werewolves at night, and one for all alive players during the day. The channel is restored automatically the moment the game ends or is cancelled. Requires the bot to have **Manage Roles** in this channel.",
					Inline: false,
				},
				{
					Name:   "Privacy note",
					Value:  "Both threads are private from other players, but anyone with the server's \"Manage Threads\" permission (usually admins/mods) can still see them — that's a Discord permission-model limit, not a bot bug.",
					Inline: false,
				},
				{
					Name:   "Limits",
					Value:  "In-memory only — a bot restart while a game is running loses it, same as `/duel-challenge-all`. Missing 2 actions/votes in a row auto-removes a player. One game per channel at a time. Seer, Guard, Witch, Hunter, Disruptor, Cupid, and Traitor are capped at 1 each per game, even with custom-roles — the engine only acts on the first of each.",
					Inline: false,
				},
			},
		}

	default:
		return cmd.getGeneralHelp()
	}
}
