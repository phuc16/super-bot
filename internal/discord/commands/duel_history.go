package commands

import (
	"context"
	"fmt"
	"strings"

	"github.com/bwmarrin/discordgo"
	"github.com/rs/zerolog/log"

	"github.com/phuc16/VCG-super-bot/internal/models"
	"github.com/phuc16/VCG-super-bot/internal/services/duelstats"
)

// DuelHistoryCommand handles the /duel-history slash command
type DuelHistoryCommand struct {
	svc *duelstats.Service
}

// NewDuelHistoryCommand creates a new DuelHistoryCommand
func NewDuelHistoryCommand(svc *duelstats.Service) *DuelHistoryCommand {
	return &DuelHistoryCommand{svc: svc}
}

// Definition returns the Discord application command definition
func (cmd *DuelHistoryCommand) Definition() *discordgo.ApplicationCommand {
	return &discordgo.ApplicationCommand{
		Name:        "duel-history",
		Description: "Xem lịch sử đấu /duel của bạn — với 1 người cụ thể, hoặc gần đây với bất kỳ ai",
		Options: []*discordgo.ApplicationCommandOption{
			{
				Type:        discordgo.ApplicationCommandOptionUser,
				Name:        "opponent",
				Description: "Xem đối đầu với người này (bỏ trống để xem các trận gần đây với bất kỳ ai)",
				Required:    false,
			},
		},
	}
}

// Handle processes the /duel-history interaction
func (cmd *DuelHistoryCommand) Handle(s *discordgo.Session, i *discordgo.InteractionCreate) {
	options := parseOptions(i.ApplicationCommandData().Options)
	opponentID, hasOpponent := options["opponent"].(string)
	userID := i.Member.User.ID

	if !hasOpponent || opponentID == "" {
		cmd.handleRecent(s, i, userID)
		return
	}

	if opponentID == userID {
		respondError(s, i, "Chọn một người khác để xem lịch sử đối đầu.")
		return
	}
	cmd.handleHeadToHead(s, i, userID, opponentID)
}

// handleHeadToHead shows the win/loss/draw tally and recent matches against one specific opponent.
func (cmd *DuelHistoryCommand) handleHeadToHead(s *discordgo.Session, i *discordgo.InteractionCreate, userID, opponentID string) {
	ctx := context.Background()

	winsA, winsB, draws, err := cmd.svc.HeadToHead(ctx, i.GuildID, userID, opponentID)
	if err != nil {
		log.Error().Err(err).Msg("duel-history: failed to get head-to-head")
		respondError(s, i, "Có lỗi xảy ra.")
		return
	}

	if winsA == 0 && winsB == 0 && draws == 0 {
		respondSuccess(s, i, fmt.Sprintf("Bạn chưa từng đấu với %s.", nameTag(opponentID)))
		return
	}

	recent, err := cmd.svc.RecentMatches(ctx, i.GuildID, userID, opponentID, 5)
	if err != nil {
		log.Error().Err(err).Msg("duel-history: failed to get recent matches")
		respondError(s, i, "Có lỗi xảy ra.")
		return
	}

	var b strings.Builder
	fmt.Fprintf(&b, "⚔️ **Đối đầu**: %s vs %s\n", nameTag(userID), nameTag(opponentID))
	fmt.Fprintf(&b, "**%d** thắng — **%d** thua — **%d** hoà\n\n", winsA, winsB, draws)
	b.WriteString("**5 trận gần nhất:**\n")
	for _, m := range recent {
		b.WriteString(formatMatchLine(userID, m))
	}

	cmd.respond(s, i, b.String())
}

// handleRecent shows the user's most recent matches against any opponent.
func (cmd *DuelHistoryCommand) handleRecent(s *discordgo.Session, i *discordgo.InteractionCreate, userID string) {
	ctx := context.Background()

	recent, err := cmd.svc.RecentMatchesForUser(ctx, i.GuildID, userID, 10)
	if err != nil {
		log.Error().Err(err).Msg("duel-history: failed to get recent matches")
		respondError(s, i, "Có lỗi xảy ra.")
		return
	}
	if len(recent) == 0 {
		respondSuccess(s, i, "Bạn chưa đấu trận `/duel` nào cả.")
		return
	}

	var b strings.Builder
	fmt.Fprintf(&b, "📜 **Lịch sử đấu gần đây của** %s\n\n", nameTag(userID))
	for _, m := range recent {
		b.WriteString(formatMatchLine(userID, m))
	}

	cmd.respond(s, i, b.String())
}

func (cmd *DuelHistoryCommand) respond(s *discordgo.Session, i *discordgo.InteractionCreate, content string) {
	err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Content:         content,
			AllowedMentions: &discordgo.MessageAllowedMentions{},
		},
	})
	if err != nil {
		log.Error().Err(err).Msg("duel-history: failed to send response")
	}
}

// formatMatchLine renders one line of match history from userID's perspective,
// deriving the actual opponent from the match itself.
func formatMatchLine(userID string, m models.DuelMatch) string {
	opponentID := m.OpponentID
	userDice, oppDice := m.ChallengerDice1, m.ChallengerDice2
	oppDice1, oppDice2 := m.OpponentDice1, m.OpponentDice2
	if m.ChallengerID != userID {
		opponentID = m.ChallengerID
		userDice, oppDice = m.OpponentDice1, m.OpponentDice2
		oppDice1, oppDice2 = m.ChallengerDice1, m.ChallengerDice2
	}
	userSum := userDice + oppDice
	oppSum := oppDice1 + oppDice2

	outcome := "🤝 Hoà"
	switch {
	case m.Draw:
		outcome = "🤝 Hoà"
	case m.WinnerID != nil && *m.WinnerID == userID:
		outcome = "✅ Thắng"
	default:
		outcome = "❌ Thua"
	}
	critNote := ""
	if m.Critical {
		critNote = " 💥"
	}

	return fmt.Sprintf(
		"%s — bạn %d vs %s %d (cược %d)%s — %s\n",
		outcome, userSum, nameTag(opponentID), oppSum, m.Wager, critNote, m.CreatedAt.Format("02/01 15:04"),
	)
}
