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
		Description: "Display bot commands and usage information / Xem hướng dẫn sử dụng bot",
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
			{
				Type:        discordgo.ApplicationCommandOptionString,
				Name:        "lang",
				Description: "Language / Ngôn ngữ (default: Tiếng Việt)",
				Required:    false,
				Choices: []*discordgo.ApplicationCommandOptionChoice{
					{Name: "Tiếng Việt", Value: "vi"},
					{Name: "English", Value: "en"},
				},
			},
		},
	}
}

// Handle processes the command interaction
func (cmd *HelpCommand) Handle(s *discordgo.Session, i *discordgo.InteractionCreate) {
	// Parse options
	options := parseOptions(i.ApplicationCommandData().Options)

	lang := "vi"
	if v, ok := options["lang"].(string); ok && v == "en" {
		lang = "en"
	}

	var embeds []*discordgo.MessageEmbed

	if commandName, ok := options["command"].(string); ok {
		// Show help for specific command (may be more than one embed, e.g. werewolf)
		embeds = cmd.getCommandHelp(commandName, lang)
	} else {
		// Show general help
		embeds = []*discordgo.MessageEmbed{cmd.getGeneralHelp(lang)}
	}

	if len(embeds) == 0 {
		return
	}

	// Discord's 6000-char embed budget applies to the SUM of every embed in
	// one message, not to each embed individually — so multi-embed help
	// (werewolf) can't ship all its embeds in a single response even though
	// each one individually fits under 6000. Send the first as the actual
	// interaction response, then any remaining embeds as separate ephemeral
	// followup messages.
	err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Embeds: []*discordgo.MessageEmbed{embeds[0]},
			Flags:  discordgo.MessageFlagsEphemeral,
		},
	})
	if err != nil {
		log.Error().Err(err).Msg("Failed to send help response")
		return
	}

	for _, embed := range embeds[1:] {
		if _, err := s.FollowupMessageCreate(i.Interaction, false, &discordgo.WebhookParams{
			Embeds: []*discordgo.MessageEmbed{embed},
			Flags:  discordgo.MessageFlagsEphemeral,
		}); err != nil {
			log.Error().Err(err).Msg("Failed to send help followup embed")
		}
	}
}

// getGeneralHelp returns the general help embed
func (cmd *HelpCommand) getGeneralHelp(lang string) *discordgo.MessageEmbed {
	if lang == "vi" {
		return &discordgo.MessageEmbed{
			Title:       "🤖 Discord Super Bot - Hướng dẫn",
			Color:       0x5865F2,
			Description: "Bot tóm tắt nội dung bằng AI và quản lý các hoạt động của server.",
			Fields: []*discordgo.MessageEmbedField{
				{
					Name:   "📤 `/export`",
					Value:  "Xuất và tóm tắt tin nhắn trong kênh\n*Ai cũng dùng được*",
					Inline: false,
				},
				{
					Name:   "🧠 `/addtopic`, `/listtopic`, `/deltopic`, `/posttopic` 🔒",
					Value:  "Quản lý các bài đăng chủ đề do AI viết theo lịch\n*Chỉ admin — xem `/help command:topic lang:vi` để biết chi tiết*",
					Inline: false,
				},
				{
					Name:   "🔑 `/addcom`, `/delcom`, `/listcom` 🔒",
					Value:  "Quản lý lệnh trả lời tự động theo từ khoá\n*Chỉ admin — xem `/help command:custom lang:vi` để biết chi tiết*",
					Inline: false,
				},
				{
					Name:   "🔥 `/roast`",
					Value:  "AI \"cà khịa\" một thành viên dựa trên tin nhắn gần đây của họ\n*Ai cũng dùng được*",
					Inline: false,
				},
				{
					Name:   "⚔️ `/duel`",
					Value:  "Thách đấu xúc xắc với một thành viên khác, có cược điểm và chuỗi thắng\n*Ai cũng dùng được — xem `/help command:duel lang:vi` để biết chi tiết*",
					Inline: false,
				},
				{
					Name:   "🏆 `/duel-leaderboard`",
					Value:  "Xem bảng xếp hạng điểm /duel của server\n*Ai cũng dùng được*",
					Inline: false,
				},
				{
					Name:   "📜 `/duel-history`",
					Value:  "Xem lịch sử đấu /duel gần đây — đối đầu với một người, hoặc các trận gần nhất với bất kỳ ai\n*Ai cũng dùng được*",
					Inline: false,
				},
				{
					Name:   "🔄 `/duel-reset` 🔒",
					Value:  "Reset toàn bộ điểm, thắng/thua, chuỗi thắng của /duel (cần xác nhận)\n*Chỉ admin*",
					Inline: false,
				},
				{
					Name:   "🎪 `/duel-challenge-all` 🔒",
					Value:  "Lên lịch giải đấu Duel loại trực tiếp vào một thời điểm cố định, có cá cược nhà vô địch\n*Chỉ admin mới mở được — ai cũng tham gia được*",
					Inline: false,
				},
				{
					Name:   "🐺 `/werewolf`",
					Value:  "Chơi Ma Sói trong kênh này\n*Ai cũng dùng được — xem `/help command:werewolf lang:vi` để biết chi tiết*",
					Inline: false,
				},
				{
					Name:   "💡 Mẹo",
					Value:  "• Dùng `/help command:<lệnh> lang:vi` để xem chi tiết một lệnh\n• 🔒 đánh dấu lệnh chỉ admin dùng được, cần quyền Administrator hoặc Manage Server\n• Thêm `lang:en` để xem hướng dẫn bằng tiếng Anh",
					Inline: false,
				},
			},
			Footer: &discordgo.MessageEmbedFooter{
				Text: "Dùng /help command:<lệnh> để xem chi tiết",
			},
		}
	}

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
				Value:  "Manage recurring AI-generated topic posts\n*Admin only — use `/help command:topic` for details*",
				Inline: false,
			},
			{
				Name:   "🔑 `/addcom`, `/delcom`, `/listcom` 🔒",
				Value:  "Manage keyword-triggered auto-reply commands\n*Admin only — use `/help command:custom` for details*",
				Inline: false,
			},
			{
				Name:   "🔥 `/roast`",
				Value:  "AI roast a member based on their recent messages\n*Available to all members*",
				Inline: false,
			},
			{
				Name:   "⚔️ `/duel`",
				Value:  "Challenge a member to a dice duel with point wagers and streaks\n*Available to all members — use `/help command:duel` for details*",
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
				Value:  "Play a game of Werewolf (Ma Sói) in this channel\n*Available to all members — use `/help command:werewolf` for details*",
				Inline: false,
			},
			{
				Name:   "💡 Tips",
				Value:  "• Use `/help command:<name>` for detailed information about a specific command\n• 🔒 marks admin-only commands, requiring Administrator or Manage Server permission\n• Add `lang:vi` for Vietnamese",
				Inline: false,
			},
		},
		Footer: &discordgo.MessageEmbedFooter{
			Text: "Use /help command:<name> for detailed information",
		},
	}
}

// getCommandHelp returns help for a specific command, in the requested
// language. Werewolf's help is long enough (11 roles, full game-flow and
// win-condition detail) that it needs two embeds to stay under Discord's
// per-field (1024 char) and per-embed (6000 char) limits — every other
// command fits comfortably in one.
func (cmd *HelpCommand) getCommandHelp(commandName, lang string) []*discordgo.MessageEmbed {
	if commandName == "werewolf" {
		return cmd.getWerewolfHelp(lang)
	}
	if lang == "vi" {
		return []*discordgo.MessageEmbed{cmd.getCommandHelpVI(commandName)}
	}
	return []*discordgo.MessageEmbed{cmd.getCommandHelpEN(commandName)}
}

// getCommandHelpEN returns the English help embed for a specific command.
func (cmd *HelpCommand) getCommandHelpEN(commandName string) *discordgo.MessageEmbed {
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

	default:
		return cmd.getGeneralHelp("en")
	}
}

// getWerewolfHelp returns the werewolf help as two embeds (overview +
// roles/limits) — the full role-by-role detail doesn't fit Discord's
// single-embed 6000-char budget alongside everything else this command
// needs to explain.
func (cmd *HelpCommand) getWerewolfHelp(lang string) []*discordgo.MessageEmbed {
	if lang == "vi" {
		return []*discordgo.MessageEmbed{
			{
				Title:       "🐺 Lệnh `/werewolf` (1/2) — Cách chơi",
				Color:       0x5865F2,
				Description: "Chơi Ma Sói với tối đa 11 vai trò thuộc hai phe — Sói và Dân.",
				Fields: []*discordgo.MessageEmbedField{
					{
						Name:   "Lệnh con",
						Value:  "• `/werewolf start reveal-on-death:true|false custom-roles:true|false` - Mở bàn chơi trong kênh này; custom-roles mở màn hình chọn vai trò tuỳ chỉnh (chỉ người mở bàn thấy) sau khi khoá danh sách người chơi, thay vì dùng bảng vai trò mặc định theo số người\n• `/werewolf status` - Xem giai đoạn/vòng hiện tại mà không lộ vai trò ai\n• `/werewolf leave` - Rời bàn (khi chưa bắt đầu), hoặc rời ván đang chạy (coi như bị loại)\n• `/werewolf force-stop` - [Quản trị viên] Buộc dừng ván đang chạy trong kênh này ở bất kỳ giai đoạn nào (không cần là người mở bàn)",
						Inline: false,
					},
					{
						Name:   "Trình tự ván chơi",
						Value:  fmt.Sprintf("1️⃣ **Mở bàn** — ai cũng Tham gia/Rời đi được; người mở bàn bấm Bắt đầu khi đủ %d-%d người.\n2️⃣ **(Tuỳ chọn) Vai trò tuỳ chỉnh** — nếu `custom-roles:true`, người mở bàn tự chọn số lượng từng vai trò qua màn hình riêng trước khi chia bài.\n3️⃣ **Nhận vai trò** — mỗi người được nhắn tin riêng vai trò của mình, kèm ghi chú ngắn cách vai trò đó hoạt động.\n4️⃣ **Ban đêm** — bầy Sói cùng bỏ phiếu chọn mục tiêu cắn trong thread riêng; Tiên Tri/Bảo Vệ/Phù Thủy/Thợ Săn/Kẻ Phá Hoại hành động qua tin nhắn riêng; Thần Tình Yêu hành động qua tin nhắn riêng chỉ đêm đầu tiên.\n5️⃣ **Ban ngày** — thảo luận trong thread riêng của người còn sống, rồi bỏ phiếu treo cổ (hoặc bỏ qua).\n6️⃣ Lặp lại đêm/ngày đến khi một phe thắng, sau đó đăng toàn bộ lịch sử ván đấu theo từng vòng.", werewolfMinPlayers, werewolfMaxPlayers),
						Inline: false,
					},
					{
						Name:   "Luật thắng thua",
						Value:  "🏡 **Phe Dân thắng** khi toàn bộ phe Sói đã chết.\n🐺 **Phe Sói thắng** khi số người phe Sói ≥ số người còn lại — không cần giết hết, chỉ cần không còn bị áp đảo.\nPhe (chứ không phải tên vai trò) quyết định ai thắng cùng ai. Thông báo kết thúc ván liệt kê rõ từng người thắng kèm vai trò của họ, không chỉ nói phe nào thắng.",
						Inline: false,
					},
					{
						Name:   "Luật thắng thua — theo từng vai trò",
						Value:  "**Phe Sói (thắng cùng nhau):** 🐺 Sói thường, 🐺👶 Sói Con, 🎭 Kẻ Phản Bội — thuộc phe Sói ngay từ đầu.\n🐺✨ Sói Nguyền — bắt đầu thuộc phe **Dân** (thắng cùng Dân nếu ván kết thúc trước khi biến hình); nếu bị cắn mà sống sót, chuyển sang phe **Sói** và từ đó thắng cùng Sói.\n**Phe Dân (thắng cùng nhau):** 👤 Dân Làng, 🔮 Tiên Tri, 💉 Bảo Vệ, 🧙 Phù Thủy, 🏹 Thợ Săn, 🤫 Kẻ Phá Hoại, 💘 Thần Tình Yêu — luôn thuộc phe Dân, luôn thắng khi phe Dân thắng dù bản thân còn sống hay đã chết vào cuối ván.\n💘 **Cặp đôi khác phe**: nếu Thần Tình Yêu ghép 2 người khác phe (1 Sói + 1 Dân), họ tách thành phe thứ ba — chỉ thắng nếu cả hai sống đến khi là 2 người cuối cùng. Thần Tình Yêu luôn thắng theo họ (dù còn sống hay đã chết, kể cả khi không nằm trong cặp đôi đó).",
						Inline: false,
					},
					{
						Name:   "Thảo luận & bỏ phiếu",
						Value:  "**Thảo luận ban ngày**: người còn sống trò chuyện trong một thread riêng — thời gian tăng theo số người còn lại. Kênh chính bị khoá chỉ cho bot đăng, chỉ hiện thông báo giai đoạn/kết quả.\n**Bỏ phiếu ban ngày**: chọn người muốn treo cổ, hoặc chọn \"Không treo cổ ai\" (bỏ qua) — bỏ qua chỉ là một lựa chọn như bất kỳ ai khác. **Không cần quá bán hay số phiếu tối thiểu**, chỉ cần nhiều phiếu nhất là thắng. Không ai bị treo cổ nếu \"bỏ qua\" nhiều phiếu nhất, hoặc nếu hoà phiếu tuyệt đối giữa các lựa chọn dẫn đầu.\n**Ban đêm (Sói)**: bầy Sói bỏ phiếu trong thread riêng — mỗi Sói bỏ phiếu 1 lần (khoá, không đổi được), quyết định theo cách tương tự (nhiều phiếu nhất, không quá bán); hoà phiếu thì đêm đó không ai chết.",
						Inline: false,
					},
				},
			},
			{
				Title: "🐺 Lệnh `/werewolf` (2/2) — Vai trò & Giới hạn",
				Color: 0x5865F2,
				Fields: []*discordgo.MessageEmbedField{
					{
						Name:   "Vai trò phe Sói",
						Value:  "*(Thắng cùng phe Sói: Sói thường, Sói Con, Kẻ Phản Bội luôn vậy; Sói Nguyền chỉ sau khi biến hình — trước đó thắng cùng phe Dân.)*\n🐺 **Sói thường** — bỏ phiếu cùng bầy mỗi đêm để chọn mục tiêu cắn.\n🐺👶 **Sói Con** — bỏ phiếu như Sói thường; nếu chết (bất kỳ lý do gì), đêm *kế tiếp* bầy Sói cắn thêm 1 mục tiêu mỗi Sói Con đã chết.\n🐺✨ **Sói Nguyền** — ban đầu là Dân thường (Tiên Tri soi \"không phải Sói\"). Bị cắn mà không ai cứu thì không chết — biến thành Sói, bỏ phiếu cùng bầy từ đó.\n🎭 **Kẻ Phản Bội** — biết ai là Sói nhưng không bỏ phiếu cắn, không có hành động ban đêm; Tiên Tri soi \"không phải Sói\" — kể cả bầy Sói cũng không biết, có thể lỡ cắn chết nó. Nếu mọi Sói khác chết mà nó còn sống, tự động thành Sói thường, bỏ phiếu cắn từ đêm đó.",
						Inline: false,
					},
					{
						Name:   "Vai trò đặc biệt phe Dân",
						Value:  "*(6 vai trò dưới đây thắng cùng phe Dân — trừ Thần Tình Yêu khi ghép đôi khác phe.)*\n🔮 **Tiên Tri** — DM mỗi đêm: soi một người, biết có phải Sói không.\n💉 **Bảo Vệ** — DM mỗi đêm: bảo vệ một người khỏi Sói cắn (không trùng người 2 đêm liền).\n🧙 **Phù Thủy** — DM mỗi đêm sau khi biết ai bị cắn: cứu người bị cắn (1 lần/ván) và/hoặc đầu độc ai khác (1 lần/ván, vượt qua Bảo Vệ).\n🏹 **Thợ Săn** — DM chọn mục tiêu bất cứ lúc nào; nếu chết, mục tiêu đó chết theo. Đổi được qua các đêm.\n🤫 **Kẻ Phá Hoại** — DM mỗi đêm: chọn người bị câm lặng ở thread thảo luận *hôm sau*.\n💘 **Thần Tình Yêu** — DM chỉ đêm đầu: chọn 2 người ghép đôi (được chọn cả mình), biết vai trò nhau, không trò chuyện trực tiếp qua bot. Một người chết thì người kia chết theo.",
						Inline: false,
					},
					{
						Name:   "Dân thường",
						Value:  "👤 *(thắng cùng phe Dân)* Không có khả năng đặc biệt — sống sót, thảo luận, và bỏ phiếu.",
						Inline: false,
					},
					{
						Name:   "Vai trò tương tác với bot ra sao",
						Value:  "🐺 Sói thường / Sói Con / Sói Nguyền (sau khi lộ diện): hành động cùng nhau mỗi đêm trong thread riêng của bầy Sói.\n🔮 Tiên Tri / 💉 Bảo Vệ / 🧙 Phù Thủy / 🏹 Thợ Săn / 🤫 Kẻ Phá Hoại: được nhắn tin riêng mỗi đêm để chọn hành động.\n💘 Thần Tình Yêu: chỉ được nhắn tin riêng vào đêm đầu tiên.\n🎭 Kẻ Phản Bội: được nhắn tin riêng biết ai là Sói khi bắt đầu ván, và lại mỗi đêm — không có hành động ban đêm.\n👤 Dân thường: không có hành động riêng — chỉ thảo luận và bỏ phiếu cùng mọi người ban ngày.\nMọi người thảo luận công khai trong thread của người còn sống vào ban ngày; kênh chính chỉ hiện thông báo giai đoạn/kết quả bỏ phiếu.",
						Inline: false,
					},
					{
						Name:   "Khoá kênh",
						Value:  "Bot khoá kênh chính chỉ cho bot đăng bài trong suốt ván đấu — mọi trò chuyện thật sự diễn ra trong hai thread riêng: một cho bầy Sói vào ban đêm, một cho tất cả người còn sống vào ban ngày. Kênh được mở lại tự động ngay khi ván kết thúc hoặc bị huỷ. Cần bot có quyền **Manage Roles** trong kênh này.",
						Inline: false,
					},
					{
						Name:   "Lưu ý về quyền riêng tư",
						Value:  "Cả hai thread đều riêng tư với người chơi khác, nhưng ai có quyền \"Manage Threads\" trên server (thường là admin/mod) vẫn xem được — đây là giới hạn của hệ thống quyền Discord, không phải lỗi của bot.",
						Inline: false,
					},
					{
						Name:   "Giới hạn",
						Value:  "Chỉ lưu trong bộ nhớ — bot khởi động lại giữa ván sẽ mất ván đó, giống `/duel-challenge-all`. Bỏ lỡ hành động/bỏ phiếu 2 lần liên tiếp sẽ tự động bị loại. Mỗi kênh chỉ chạy một ván tại một thời điểm. Tiên Tri, Bảo Vệ, Phù Thủy, Thợ Săn, Kẻ Phá Hoại, Thần Tình Yêu, và Kẻ Phản Bội tối đa 1 người mỗi vai trò mỗi ván, kể cả khi dùng custom-roles — hệ thống chỉ xử lý người đầu tiên của mỗi vai trò đó.",
						Inline: false,
					},
				},
			},
		}
	}

	return []*discordgo.MessageEmbed{
		{
			Title:       "🐺 `/werewolf` Command (1/2) — How to Play",
			Color:       0x5865F2,
			Description: "Play a game of Werewolf (Ma Sói) with up to 11 roles across two factions — Wolves and Villagers.",
			Fields: []*discordgo.MessageEmbedField{
				{
					Name:   "Subcommands",
					Value:  "• `/werewolf start reveal-on-death:true|false custom-roles:true|false` - Open a lobby in this channel; custom-roles opens an ephemeral role-count editor for the starter after the roster locks, instead of the default player-count table\n• `/werewolf status` - Check the current phase/round without leaking roles\n• `/werewolf leave` - Leave the lobby, or leave a running game (counts as being eliminated)\n• `/werewolf force-stop` - [Admin] Force-stop whatever game is running in this channel, at any phase (doesn't need to be the starter)",
					Inline: false,
				},
				{
					Name:   "Game flow",
					Value:  fmt.Sprintf("1️⃣ **Lobby** — anyone Join/Leave; the starter clicks Start once %d-%d have joined.\n2️⃣ **(Optional) Custom roles** — with `custom-roles:true`, the starter picks exact role counts via an ephemeral editor before dealing.\n3️⃣ **Role reveal** — everyone is DMed their role privately, with a short note on how that role acts.\n4️⃣ **Night** — the wolf pack votes on a kill target together in a private thread; Seer/Guard/Witch/Hunter/Disruptor act via DM; Cupid acts via DM on the first night only.\n5️⃣ **Day** — discuss in a private alive-players thread, then vote to lynch (or skip).\n6️⃣ Repeat night/day until one side wins, then a full round-by-round recap posts.", werewolfMinPlayers, werewolfMaxPlayers),
					Inline: false,
				},
				{
					Name:   "Winning rule",
					Value:  "🏡 **Villagers win** the moment every wolf-faction player is dead.\n🐺 **Wolves win** the moment wolf-faction players are at least as many as everyone else still alive — they don't need to eliminate everyone, just stop being outnumbered.\nFaction (not role name) decides who wins with whom. The end-game announcement names every winner individually with their role, not just which side won.",
					Inline: false,
				},
				{
					Name:   "Winning rule — by role",
					Value:  "**Wolf faction (win together):** 🐺 Werewolf, 🐺👶 Wolf Cub, 🎭 Traitor — wolf-faction from the start.\n🐺✨ Cursed Wolf — starts **villager**-faction (wins with villagers if the game ends before it converts); if it's bitten and survives, it flips to **wolf**-faction and from then on wins with the wolves instead.\n**Villager faction (win together):** 👤 Villager, 🔮 Seer, 💉 Guard, 🧙 Witch, 🏹 Hunter, 🤫 Disruptor, 💘 Cupid — always villager-faction, always win when villagers win, regardless of whether they're personally still alive at the end.\n💘 **Mixed-faction couple**: if Cupid pairs two players from opposing factions (1 Wolf + 1 Villager), they split off into their own third party — winning only if both survive down to being the last 2 players left. Cupid always wins alongside them too (alive or dead, even when not part of that couple).",
					Inline: false,
				},
				{
					Name:   "Discussion & voting",
					Value:  "**Day-discuss**: everyone still alive talks in a private thread — the window scales with how many players are left. The main channel is locked to bot-only chat and only posts phase/result announcements.\n**Day-vote**: pick who to lynch from a select menu, or explicitly choose \"Không treo cổ ai\" (skip). \"Skip\" is just another candidate in the tally, counted the exact same way as a player — there's **no majority/quorum required**, just a plain plurality: whoever (or whichever choice) gets strictly the most votes wins. Nobody is lynched if Skip gets the most votes outright, **or** if there's an exact tie for the most votes between any candidates (two players, or a player and Skip).\n**Night (wolves)**: the pack votes together in their own private thread — every wolf votes once (locked in, no changing your mind), and the pack's target is decided the same way (plurality, no quorum); an exact tie among wolves means no kill that night.",
					Inline: false,
				},
			},
		},
		{
			Title: "🐺 `/werewolf` Command (2/2) — Roles & Limits",
			Color: 0x5865F2,
			Fields: []*discordgo.MessageEmbedField{
				{
					Name:   "Wolf-side roles",
					Value:  "🐺 **Werewolf** *(wins with the wolves)* — votes with the pack each night to pick a kill target.\n🐺👶 **Wolf Cub** *(wins with the wolves)* — votes like a Werewolf; if it dies (any cause), the pack gets one extra bite target the *following* night, per Wolf Cub that died.\n🐺✨ **Cursed Wolf** *(wins with the villagers until it converts, then wins with the wolves)* — starts as a normal villager (the Seer reads it as \"not a wolf\"). If the pack bites it and nobody saves it, it doesn't die — it converts into a wolf and joins the pack's vote from then on.\n🎭 **Traitor** *(always wins with the wolves, even while disguised as a villager)* — knows who the wolves are, but has no night action and doesn't vote with the pack. The Seer reads it as \"not a wolf\" too — even the wolves don't know, and can accidentally bite it dead. If every other wolf dies while the Traitor is still alive, it automatically becomes a full Werewolf and starts voting with whichever wolves are left.",
					Inline: false,
				},
				{
					Name:   "Villager-side special roles",
					Value:  "*(All 6 roles below win with the villagers — Cupid is the one exception when it pairs a mixed-faction couple, see \"Winning rule — by role\".)*\n🔮 **Seer** — DM each night: check one player, learn if they're wolf-faction or not.\n💉 **Guard** — DM each night: protect one player from the wolves' bite (not the same person twice in a row).\n🧙 **Witch** — DM each night, after the bite is known: heal the bitten player (once per game) and/or poison anyone else (once per game, bypasses the Guard).\n🏹 **Hunter** — DM: pick a target any time; if they die, that target dies too. Can be changed on a later night.\n🤫 **Disruptor** — DM each night: pick someone to be muted in the day-discuss thread the *following* day.\n💘 **Cupid** — DM on the first night only: pick exactly 2 players to fall in love (may include themself), who learn each other's role but not who Cupid is, and can't chat directly through the bot. If either dies, the other dies too.",
					Inline: false,
				},
				{
					Name:   "Villager",
					Value:  "👤 *(wins with the villagers)* No special ability — survive, discuss, and vote.",
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
		},
	}
}

// getCommandHelpVI returns the Vietnamese help embed for a specific command.
func (cmd *HelpCommand) getCommandHelpVI(commandName string) *discordgo.MessageEmbed {
	switch commandName {
	case "export":
		return &discordgo.MessageEmbed{
			Title:       "📤 Lệnh `/export`",
			Color:       0x5865F2,
			Description: "Xuất tin nhắn trong kênh kèm bản tóm tắt do GPT-4 tạo ra.",
			Fields: []*discordgo.MessageEmbedField{
				{
					Name:   "Cách dùng",
					Value:  "**Xuất theo khoảng thời gian:**\n`/export type:date-range from:\"YYYY-MM-DD HH:MM\" end:\"YYYY-MM-DD HH:MM\"`\n\n**Tin nhắn chưa đọc:**\n`/export type:unread`",
					Inline: false,
				},
				{
					Name:   "Tham số",
					Value:  "• `type` - Kiểu xuất (date-range hoặc unread)\n• `from` - Thời điểm bắt đầu (chỉ dùng với date-range)\n• `end` - Thời điểm kết thúc (chỉ dùng với date-range)",
					Inline: false,
				},
				{
					Name:   "Ví dụ",
					Value:  "`/export type:date-range from:\"2025-10-20\" end:\"2025-10-27\"`\n`/export type:date-range from:\"2025-10-20 14:00\" end:\"2025-10-27 18:00\"`\n`/export type:unread`",
					Inline: false,
				},
				{
					Name:   "Giới hạn tần suất",
					Value:  "3 lần xuất / người / giờ",
					Inline: true,
				},
				{
					Name:   "Số tin nhắn tối đa",
					Value:  "10.000 tin nhắn",
					Inline: true,
				},
			},
			Footer: &discordgo.MessageEmbedFooter{
				Text: "Thời gian xử lý tuỳ theo số lượng tin nhắn (thường 10-60 giây)",
			},
		}

	case "topic":
		return &discordgo.MessageEmbed{
			Title:       "🧠 Lệnh Chủ đề AI 🔒",
			Color:       0xED4245,
			Description: "Quản lý các bài đăng chủ đề do AI viết theo lịch định kỳ. **Chỉ admin**",
			Fields: []*discordgo.MessageEmbedField{
				{
					Name:   "`/addtopic`",
					Value:  "Thêm một chủ đề để bot viết bài định kỳ\n`/addtopic topic:\"lịch sử Việt Nam\" channel:#general interval:60 mention:@Tâm`",
					Inline: false,
				},
				{
					Name:   "`/listtopic`",
					Value:  "Liệt kê tất cả chủ đề đã đặt cho server, có đánh số để dùng với `/deltopic`",
					Inline: false,
				},
				{
					Name:   "`/deltopic`",
					Value:  "Xoá một chủ đề theo số thứ tự từ `/listtopic`\n`/deltopic number:2`",
					Inline: false,
				},
				{
					Name:   "`/posttopic`",
					Value:  "Đăng ngay một đoạn văn do AI viết về một chủ đề bất kỳ, không lưu lại làm chủ đề định kỳ\n`/posttopic topic:\"con mèo\" channel:#general`",
					Inline: false,
				},
				{
					Name:   "Tham số",
					Value:  "• `topic` - Nội dung AI sẽ viết về\n• `channel` - Kênh bot sẽ đăng bài\n• `interval` - Số phút giữa các lần đăng (chỉ `/addtopic`, mặc định 5)\n• `mention` - Người sẽ được tag trong bài viết; dùng `{mention}` trong nội dung chủ đề làm chỗ chèn",
					Inline: false,
				},
				{
					Name:   "Quyền cần có",
					Value:  "Administrator hoặc Manage Server",
					Inline: false,
				},
			},
		}

	case "custom":
		return &discordgo.MessageEmbed{
			Title:       "🔑 Lệnh Tuỳ Chỉnh 🔒",
			Color:       0xED4245,
			Description: "Quản lý các lệnh trả lời tự động theo từ khoá. **Chỉ admin** mới thêm/xoá được; ai cũng kích hoạt được từ khoá có sẵn.",
			Fields: []*discordgo.MessageEmbedField{
				{
					Name:   "`/addcom`",
					Value:  "Thêm một từ khoá để bot trả lời, kèm chữ và/hoặc ảnh\n`/addcom keyword:Kami response:\"hi there\"`",
					Inline: false,
				},
				{
					Name:   "`/delcom`",
					Value:  "Xoá một lệnh từ khoá\n`/delcom keyword:Kami`",
					Inline: false,
				},
				{
					Name:   "`/listcom`",
					Value:  "Liệt kê tất cả lệnh từ khoá đã đặt cho server",
					Inline: false,
				},
				{
					Name:   "Tham số",
					Value:  "• `keyword` - Từ khoá kích hoạt, không có dấu `/` phía trước\n• `response` - Nội dung trả lời (không bắt buộc nếu có đính kèm ảnh)\n• `image` - Ảnh đính kèm để trả lời (không bắt buộc)",
					Inline: false,
				},
				{
					Name:   "Quyền cần có",
					Value:  "Administrator hoặc Manage Server (để thêm/xoá) — ai cũng kích hoạt được từ khoá có sẵn",
					Inline: false,
				},
			},
		}

	case "roast":
		return &discordgo.MessageEmbed{
			Title:       "🔥 Lệnh `/roast`",
			Color:       0x5865F2,
			Description: "AI \"cà khịa\" một thành viên, dựa trên tin nhắn gần đây của họ trong kênh.",
			Fields: []*discordgo.MessageEmbedField{
				{
					Name:   "Cách dùng",
					Value:  "`/roast target:@user`",
					Inline: false,
				},
				{
					Name:   "Cách hoạt động",
					Value:  "Lấy tin nhắn gần đây của người được nhắm tới trong kênh này rồi nhờ GPT chê dựa trên đó. Nếu không tìm thấy tin nhắn gần đây, sẽ dùng một câu cà khịa chung chung (nhưng vẫn buồn cười).",
					Inline: false,
				},
				{
					Name:   "Giới hạn tần suất",
					Value:  "5 lần / người / phút",
					Inline: true,
				},
			},
		}

	case "duel":
		return &discordgo.MessageEmbed{
			Title:       "⚔️ Lệnh `/duel`",
			Color:       0x5865F2,
			Description: "Thách đấu công khai bằng xúc xắc với một thành viên khác, có cược điểm, chuỗi thắng, và lịch sử được lưu lại.",
			Fields: []*discordgo.MessageEmbedField{
				{
					Name:   "Cách dùng",
					Value:  "`/duel opponent:@user wager:20`",
					Inline: false,
				},
				{
					Name:   "Cách hoạt động",
					Value:  "Đăng một lời thách đấu công khai kèm nút Chấp nhận/Từ chối — chỉ người bị thách mới trả lời được, trong vòng 60 giây. Khi chấp nhận, cả hai bên tung 2 xúc xắc kèm hiệu ứng ngắn; tổng điểm cao hơn thắng (bằng điểm là hoà). Ra lục lục (hai số 6) là chí mạng, nhân đôi số điểm cược chuyển đi.",
					Inline: false,
				},
				{
					Name:   "Điểm & Chuỗi thắng",
					Value:  "Mỗi người bắt đầu với 100 điểm trên mỗi server. Người thắng lấy số điểm cược từ người thua (giới hạn theo số dư của họ, không ai bị âm điểm). Thắng/thua/hoà và chuỗi thắng được lưu lại lâu dài — xem bảng xếp hạng bằng `/duel-leaderboard`.",
					Inline: false,
				},
				{
					Name:   "Cá cược của người xem",
					Value:  fmt.Sprintf("Bất kỳ ai không phải hai người thách đấu đều có thể cược ai thắng khi trận đấu đang chờ chấp nhận, qua các nút cược dưới tin thách đấu (%d-%d điểm, mỗi người 1 lượt cược). Trả thưởng theo kiểu pari-mutuel: tiền cược của bên thua được chia cho bên thắng theo tỉ lệ cược. Trận bị từ chối/hết hạn hoặc hoà sẽ hoàn lại toàn bộ tiền cược.", minBetAmount, maxBetAmount),
					Inline: false,
				},
				{
					Name:   "Tham số",
					Value:  fmt.Sprintf("• `opponent` - Người muốn thách đấu\n• `wager` - Số điểm cược (1-%d, mặc định %d)", maxWager, defaultWager),
					Inline: false,
				},
				{
					Name:   "Giới hạn tần suất",
					Value:  "10 lần thách đấu / người / phút",
					Inline: true,
				},
			},
		}

	case "duel-leaderboard":
		return &discordgo.MessageEmbed{
			Title:       "🏆 Lệnh `/duel-leaderboard`",
			Color:       0x5865F2,
			Description: "Xem bảng xếp hạng /duel của server.",
			Fields: []*discordgo.MessageEmbedField{
				{
					Name:   "Cách dùng",
					Value:  "`/duel-leaderboard`",
					Inline: false,
				},
				{
					Name:   "Hiển thị",
					Value:  "Top 10 theo điểm, cộng thêm hạng của bạn (kèm dấu 🔥 chuỗi thắng / 🥶 chuỗi thua) nếu bạn không nằm trong top 10.",
					Inline: false,
				},
			},
		}

	case "duel-history":
		return &discordgo.MessageEmbed{
			Title:       "📜 Lệnh `/duel-history`",
			Color:       0x5865F2,
			Description: "Xem lịch sử đấu /duel của bạn.",
			Fields: []*discordgo.MessageEmbedField{
				{
					Name:   "Cách dùng",
					Value:  "`/duel-history` — 10 trận gần nhất với bất kỳ ai\n`/duel-history opponent:@user` — thành tích đối đầu với người đó",
					Inline: false,
				},
				{
					Name:   "Hiển thị",
					Value:  "Có `opponent`: tổng thắng/thua/hoà đối đầu với người đó, kèm 5 trận gần nhất giữa hai người. Không có: 10 trận gần nhất với bất kỳ đối thủ nào. Cả hai đều hiện tổng xúc xắc, mức cược, và kết quả từng trận.",
					Inline: false,
				},
			},
		}

	case "duel-reset":
		return &discordgo.MessageEmbed{
			Title:       "🔄 Lệnh `/duel-reset` 🔒",
			Color:       0xED4245,
			Description: "Reset điểm, thắng/thua/hoà, và chuỗi thắng /duel của mọi người trong server này. **Chỉ admin**",
			Fields: []*discordgo.MessageEmbedField{
				{
					Name:   "Cách dùng",
					Value:  "`/duel-reset`\n`/duel-reset wipe_history:true`",
					Inline: false,
				},
				{
					Name:   "Cần xác nhận",
					Value:  "Đăng một lời nhắc Xác nhận/Huỷ — chỉ admin đã chạy lệnh mới xác nhận được, trong vòng 30 giây. Hành động này không thể hoàn tác.",
					Inline: false,
				},
				{
					Name:   "Tham số",
					Value:  "• `wipe_history` - Xoá luôn toàn bộ lịch sử trận đấu `/duel-history` (mặc định: false, giữ lại lịch sử)",
					Inline: false,
				},
				{
					Name:   "Quyền cần có",
					Value:  "Administrator hoặc Manage Server",
					Inline: false,
				},
			},
		}

	case "duel-challenge-all":
		return &discordgo.MessageEmbed{
			Title:       "🎪 Lệnh `/duel-challenge-all` 🔒",
			Color:       0xED4245,
			Description: "Lên lịch giải đấu xúc xắc loại trực tiếp vào một thời điểm cố định — ai cũng tham gia được, người xem có thể cược nhà vô địch, sau đó bot tự chạy toàn bộ nhánh đấu. **Chỉ admin** mới mở được.",
			Fields: []*discordgo.MessageEmbedField{
				{
					Name:   "Cách dùng",
					Value:  "`/duel-challenge-all start:\"20:00 20/08/2026\"`",
					Inline: false,
				},
				{
					Name:   "Mốc thời gian",
					Value:  fmt.Sprintf("• Đăng ký mở từ lúc đăng bài đến **%d phút** trước giờ bắt đầu (Tham gia / Rời đi)\n• Cược nhà vô địch mở trong **%d phút** tiếp theo, đóng **%d phút** trước giờ bắt đầu (chọn từ danh sách người đã tham gia)\n• Khoá lại trong %d phút cuối, sau đó nhánh đấu tự chạy", int(joinCloseBefore.Minutes()), int((joinCloseBefore - betCloseBefore).Minutes()), int(betCloseBefore.Minutes()), int(betCloseBefore.Minutes())),
					Inline: false,
				},
				{
					Name:   "Cách nhánh đấu hoạt động",
					Value:  "Người chơi được xáo trộn vào một nhánh đấu. Mỗi trận là một lượt `/duel` thật (không cược điểm) — hoà thì tung lại ngay vì nhánh đấu không thể tiếp tục khi hoà. Số người lẻ sẽ có một người được miễn đấu ngẫu nhiên mỗi vòng. Kết quả đăng theo từng vòng cho đến khi có nhà vô địch, sau đó tiền cược nhà vô địch được trả theo kiểu pari-mutuel (hoặc hoàn lại nếu không ai đoán đúng).",
					Inline: false,
				},
				{
					Name:   "Giới hạn",
					Value:  fmt.Sprintf("%d-%d người chơi. Giờ bắt đầu phải cách hiện tại %d phút – %d giờ (trạng thái giải đấu chỉ lưu trong bộ nhớ — sẽ mất nếu bot khởi động lại trước khi giải kết thúc, nên tránh deploy lại giữa chừng). Chỉ người tổ chức mới huỷ được.", tournamentMinPlayers, tournamentMaxPlayers, int(minLeadTime.Minutes()), int(maxLeadTime.Hours())),
					Inline: false,
				},
			},
		}

	default:
		return cmd.getGeneralHelp("vi")
	}
}
