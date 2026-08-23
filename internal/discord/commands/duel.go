package commands

import (
	"context"
	"fmt"
	"math/rand"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/rs/zerolog/log"

	"github.com/phuc16/VCG-super-bot/internal/services/duelstats"
)

const (
	defaultWager = 10
	maxWager     = 50

	acceptWindow  = 60 * time.Second
	bettingOpen   = 25 * time.Second
	bettingLock   = 5 * time.Second
	countdownTick = 5 * time.Second

	acceptID    = "duel:accept:"
	declineID   = "duel:decline:"
	betButtonID = "duel:bet:"
	betModalID  = "duel:betmodal:"

	minBetAmount = 1
	maxBetAmount = 100

	rollAnimationFrames = 5
	rollAnimationDelay  = 1 * time.Second
)

// diceFaces maps a die value (1-6) to its Unicode face glyph.
var diceFaces = [6]string{"⚀", "⚁", "⚂", "⚃", "⚄", "⚅"}

func diceFace(value int) string {
	return diceFaces[value-1]
}

func randomDiceFace() string {
	return diceFace(rand.Intn(6) + 1)
}

// rollAnimationFrame renders one frame of the rolling animation with random
// dice faces for both players' two dice each.
func rollAnimationFrame(challengerID, opponentID string) string {
	return fmt.Sprintf(
		"⚔️ <@%s> vs <@%s>\n🎲 Đang lắc xúc xắc...\n\n<@%s>: %s %s   |   <@%s>: %s %s",
		challengerID, opponentID,
		challengerID, randomDiceFace(), randomDiceFace(),
		opponentID, randomDiceFace(), randomDiceFace(),
	)
}

// sideBet is a spectator's wager on who will win a pending duel.
type sideBet struct {
	BettorID string
	PickedID string
	Amount   int
}

// pendingDuel tracks a challenge from posting through acceptance, betting,
// and resolution, plus any spectator side bets placed along the way.
type pendingDuel struct {
	GuildID         string
	ChannelID       string
	MessageID       string
	ChallengerID    string
	ChallengerLabel string
	OpponentID      string
	OpponentLabel   string
	Wager           int

	Accepted    bool
	BetDeadline time.Time // when betting closes (only meaningful once Accepted)

	Bets []sideBet
}

// DuelCommand handles the /duel slash command and its accept/decline/bet buttons
type DuelCommand struct {
	svc *duelstats.Service

	mu      sync.Mutex
	pending map[string]*pendingDuel
}

// NewDuelCommand creates a new DuelCommand
func NewDuelCommand(svc *duelstats.Service) *DuelCommand {
	return &DuelCommand{svc: svc, pending: make(map[string]*pendingDuel)}
}

// Definition returns the Discord application command definition
func (cmd *DuelCommand) Definition() *discordgo.ApplicationCommand {
	maxW := float64(maxWager)
	minW := float64(1)
	return &discordgo.ApplicationCommand{
		Name:        "duel",
		Description: "Thách đấu một thành viên, cược điểm, có streak và bảng xếp hạng",
		Options: []*discordgo.ApplicationCommandOption{
			{
				Type:        discordgo.ApplicationCommandOptionUser,
				Name:        "opponent",
				Description: "Người bị thách đấu",
				Required:    true,
			},
			{
				Type:        discordgo.ApplicationCommandOptionInteger,
				Name:        "wager",
				Description: fmt.Sprintf("Số điểm cược (1-%d, mặc định %d)", maxWager, defaultWager),
				Required:    false,
				MinValue:    &minW,
				MaxValue:    maxW,
			},
		},
	}
}

var drawLines = []string{
	"cả hai cùng gãy, huề trắng.",
	"bất phân thắng bại, tay đôi ai cũng nhục.",
	"hoà — tự đi mà tự sướng với nhau.",
}

var critWinLines = []string{
	"một đòn CHÍ MẠNG, không còn manh giáp!",
	"REKT tuyệt đối, khỏi cần gượng dậy.",
	"nốc ao trong tích tắc, quá phũ.",
}

var normalWinLines = []string{
	"thắng sát nút, may mắn mỉm cười.",
	"hạ gục đối thủ trong danh dự.",
	"chiến thắng vừa đủ để vênh mặt.",
}

// Handle processes the /duel interaction: posts a public challenge with
// Accept/Decline buttons and starts a live countdown until the opponent responds.
func (cmd *DuelCommand) Handle(s *discordgo.Session, i *discordgo.InteractionCreate) {
	options := parseOptions(i.ApplicationCommandData().Options)
	opponentID, _ := options["opponent"].(string)
	if opponentID == "" {
		respondError(s, i, "Không xác định được đối thủ.")
		return
	}

	wager := clampWager(options["wager"])
	challengerID := i.Member.User.ID

	if opponentID == challengerID {
		respondError(s, i, "Tự đấu với chính mình thì thắng thua gì cũng nhục như nhau.")
		return
	}

	opponentUser := i.ApplicationCommandData().Resolved.Users[opponentID]
	if opponentUser == nil {
		respondError(s, i, "Không tìm thấy đối thủ này.")
		return
	}
	if opponentUser.Bot {
		respondError(s, i, "Bot không nhận đấu, nó gian lận không đỡ nổi.")
		return
	}

	challengerLabel := displayLabel(i.Member.User)
	opponentLabel := displayLabel(opponentUser)

	challengeID := i.Interaction.ID

	err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Content:         acceptCountdownContent(challengerID, opponentID, wager, acceptWindow),
			Components:      acceptButtons(challengeID, false),
			AllowedMentions: &discordgo.MessageAllowedMentions{Users: []string{opponentID}},
		},
	})
	if err != nil {
		log.Error().Err(err).Msg("duel: failed to send challenge")
		return
	}

	msg, err := s.InteractionResponse(i.Interaction)
	if err != nil {
		log.Error().Err(err).Msg("duel: failed to fetch challenge message")
		return
	}

	pd := &pendingDuel{
		GuildID:         i.GuildID,
		ChannelID:       i.ChannelID,
		MessageID:       msg.ID,
		ChallengerID:    challengerID,
		ChallengerLabel: challengerLabel,
		OpponentID:      opponentID,
		OpponentLabel:   opponentLabel,
		Wager:           wager,
	}

	cmd.mu.Lock()
	cmd.pending[challengeID] = pd
	cmd.mu.Unlock()

	go cmd.runAcceptCountdown(s, challengeID)
}

// runAcceptCountdown edits the challenge message every countdownTick with the
// remaining time to respond, until accepted, declined, or the window expires.
func (cmd *DuelCommand) runAcceptCountdown(s *discordgo.Session, challengeID string) {
	deadline := time.Now().Add(acceptWindow)
	for {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			cmd.expire(s, challengeID)
			return
		}
		wait := countdownTick
		if remaining < wait {
			wait = remaining
		}
		time.Sleep(wait)

		cmd.mu.Lock()
		pd, stillPending := cmd.pending[challengeID]
		accepted := stillPending && pd.Accepted
		cmd.mu.Unlock()
		if !stillPending || accepted {
			return // handled elsewhere (declined / accepted / expired)
		}

		remaining = time.Until(deadline)
		secs := int(remaining.Round(time.Second).Seconds())
		if secs < 0 {
			secs = 0
		}
		content := acceptCountdownContent(pd.ChallengerID, pd.OpponentID, pd.Wager, time.Duration(secs)*time.Second)
		if _, err := s.ChannelMessageEditComplex(&discordgo.MessageEdit{
			Channel:         pd.ChannelID,
			ID:              pd.MessageID,
			Content:         &content,
			AllowedMentions: &discordgo.MessageAllowedMentions{},
		}); err != nil {
			log.Error().Err(err).Msg("duel: failed to edit accept countdown")
		}
	}
}

// HandleComponent processes Accept/Decline/Bet button clicks for pending duels.
func (cmd *DuelCommand) HandleComponent(s *discordgo.Session, i *discordgo.InteractionCreate) {
	customID := i.MessageComponentData().CustomID

	if strings.HasPrefix(customID, betButtonID) {
		cmd.handleBetButton(s, i, strings.TrimPrefix(customID, betButtonID))
		return
	}

	var accepted bool
	var challengeID string
	switch {
	case strings.HasPrefix(customID, acceptID):
		accepted = true
		challengeID = strings.TrimPrefix(customID, acceptID)
	case strings.HasPrefix(customID, declineID):
		accepted = false
		challengeID = strings.TrimPrefix(customID, declineID)
	default:
		return
	}

	cmd.mu.Lock()
	pd, ok := cmd.pending[challengeID]
	if ok && pd.Accepted {
		cmd.mu.Unlock()
		respondComponentEphemeral(s, i, "Thách đấu này đã được xử lý rồi.")
		return
	}
	clickerID := i.Member.User.ID
	if ok && clickerID != pd.OpponentID {
		cmd.mu.Unlock()
		respondComponentEphemeral(s, i, "Chỉ người bị thách đấu mới được phản hồi.")
		return
	}
	if ok && accepted {
		pd.Accepted = true
		pd.BetDeadline = time.Now().Add(bettingOpen)
	} else if ok {
		delete(cmd.pending, challengeID)
	}
	cmd.mu.Unlock()

	if !ok {
		respondComponentEphemeral(s, i, "Thách đấu này đã hết hạn hoặc đã được xử lý.")
		return
	}

	if !accepted {
		refundSummary := cmd.refundAllBets(context.Background(), pd)
		_ = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseUpdateMessage,
			Data: &discordgo.InteractionResponseData{
				Content:         fmt.Sprintf("🚫 <@%s> đã từ chối thách đấu từ <@%s>.%s", pd.OpponentID, pd.ChallengerID, refundSummary),
				Components:      []discordgo.MessageComponent{},
				AllowedMentions: &discordgo.MessageAllowedMentions{},
			},
		})
		return
	}

	err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseUpdateMessage,
		Data: &discordgo.InteractionResponseData{
			Content:         bettingCountdownContent(pd, bettingOpen),
			Components:      betButtons(challengeID, pd.ChallengerID, pd.ChallengerLabel, pd.OpponentID, pd.OpponentLabel, false),
			AllowedMentions: &discordgo.MessageAllowedMentions{},
		},
	})
	if err != nil {
		log.Error().Err(err).Msg("duel: failed to ack accept")
		return
	}

	go cmd.runBettingPhase(s, challengeID)
}

// runBettingPhase edits the message every countdownTick with the remaining
// betting time, then locks betting and runs the roll animation + resolution.
func (cmd *DuelCommand) runBettingPhase(s *discordgo.Session, challengeID string) {
	cmd.mu.Lock()
	pd, ok := cmd.pending[challengeID]
	cmd.mu.Unlock()
	if !ok {
		return
	}
	deadline := pd.BetDeadline

	for {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			break
		}
		wait := countdownTick
		if remaining < wait {
			wait = remaining
		}
		time.Sleep(wait)

		remaining = time.Until(deadline)
		secs := int(remaining.Round(time.Second).Seconds())
		if secs <= 0 {
			break
		}

		cmd.mu.Lock()
		betsSnapshot := make([]sideBet, len(pd.Bets))
		copy(betsSnapshot, pd.Bets)
		cmd.mu.Unlock()

		content := bettingCountdownContent(pd, time.Duration(secs)*time.Second) + betPoolSummary(pd, betsSnapshot)
		if _, err := s.ChannelMessageEditComplex(&discordgo.MessageEdit{
			Channel:         pd.ChannelID,
			ID:              pd.MessageID,
			Content:         &content,
			AllowedMentions: &discordgo.MessageAllowedMentions{},
		}); err != nil {
			log.Error().Err(err).Msg("duel: failed to edit betting countdown")
		}
	}

	cmd.mu.Lock()
	betsSnapshot := make([]sideBet, len(pd.Bets))
	copy(betsSnapshot, pd.Bets)
	cmd.mu.Unlock()

	pingIDs := []string{pd.ChallengerID, pd.OpponentID}
	for _, b := range betsSnapshot {
		pingIDs = append(pingIDs, b.BettorID)
	}
	pingText, allowedMentions := pingOnce(pingIDs)

	lockedContent := fmt.Sprintf("🔒 Cược đã đóng! %s chuẩn bị đấu...", pingText)
	components := []discordgo.MessageComponent{}
	if _, err := s.ChannelMessageEditComplex(&discordgo.MessageEdit{
		Channel:         pd.ChannelID,
		ID:              pd.MessageID,
		Content:         &lockedContent,
		Components:      &components,
		AllowedMentions: allowedMentions,
	}); err != nil {
		log.Error().Err(err).Msg("duel: failed to lock betting")
	}

	cmd.animateAndResolve(s, challengeID)
}

// handleBetButton opens a modal asking the spectator how many points to bet
// on the given side. Only allowed once the duel is accepted and before the
// betting deadline.
func (cmd *DuelCommand) handleBetButton(s *discordgo.Session, i *discordgo.InteractionCreate, rest string) {
	parts := strings.SplitN(rest, ":", 2)
	if len(parts) != 2 {
		return
	}
	challengeID, pickedID := parts[0], parts[1]

	cmd.mu.Lock()
	pd, ok := cmd.pending[challengeID]
	var alreadyBet, bettingClosed bool
	if ok {
		bettingClosed = !pd.Accepted || time.Now().After(pd.BetDeadline)
		for _, b := range pd.Bets {
			if b.BettorID == i.Member.User.ID {
				alreadyBet = true
				break
			}
		}
	}
	cmd.mu.Unlock()

	if !ok {
		respondComponentEphemeral(s, i, "Thách đấu này đã hết hạn hoặc đã được xử lý.")
		return
	}
	if bettingClosed {
		respondComponentEphemeral(s, i, "Cược đã đóng cho trận này.")
		return
	}
	if i.Member.User.ID == pd.ChallengerID || i.Member.User.ID == pd.OpponentID {
		respondComponentEphemeral(s, i, "Người trong trận đấu không được cá cược thêm.")
		return
	}
	if alreadyBet {
		respondComponentEphemeral(s, i, "Bạn đã cược trận này rồi.")
		return
	}

	pickedLabel := pd.OpponentLabel
	if pickedID == pd.ChallengerID {
		pickedLabel = pd.ChallengerLabel
	}

	err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseModal,
		Data: &discordgo.InteractionResponseData{
			CustomID: betModalID + challengeID + ":" + pickedID,
			Title:    fmt.Sprintf("Cược cho %s", pickedLabel),
			Components: []discordgo.MessageComponent{
				discordgo.ActionsRow{
					Components: []discordgo.MessageComponent{
						discordgo.TextInput{
							CustomID:    "amount",
							Label:       fmt.Sprintf("Số điểm cược (%d-%d)", minBetAmount, maxBetAmount),
							Style:       discordgo.TextInputShort,
							Required:    true,
							MinLength:   1,
							MaxLength:   4,
							Placeholder: fmt.Sprintf("%d", defaultWager),
						},
					},
				},
			},
		},
	})
	if err != nil {
		log.Error().Err(err).Msg("duel: failed to open bet modal")
	}
}

// HandleModalSubmit processes the bet-amount modal submission: validates and
// escrows the bet, then updates the public challenge message.
func (cmd *DuelCommand) HandleModalSubmit(s *discordgo.Session, i *discordgo.InteractionCreate) {
	data := i.ModalSubmitData()
	if !strings.HasPrefix(data.CustomID, betModalID) {
		return
	}
	rest := strings.TrimPrefix(data.CustomID, betModalID)
	parts := strings.SplitN(rest, ":", 2)
	if len(parts) != 2 {
		return
	}
	challengeID, pickedID := parts[0], parts[1]

	var amountStr string
	if len(data.Components) > 0 {
		if row, ok := data.Components[0].(*discordgo.ActionsRow); ok && len(row.Components) > 0 {
			if input, ok := row.Components[0].(*discordgo.TextInput); ok {
				amountStr = input.Value
			}
		}
	}
	amount, convErr := strconv.Atoi(strings.TrimSpace(amountStr))
	if convErr != nil || amount < minBetAmount || amount > maxBetAmount {
		respondComponentEphemeral(s, i, fmt.Sprintf("Số điểm cược phải là số nguyên từ %d đến %d.", minBetAmount, maxBetAmount))
		return
	}

	cmd.mu.Lock()
	pd, ok := cmd.pending[challengeID]
	bettingClosed := !ok || !pd.Accepted || time.Now().After(pd.BetDeadline)
	cmd.mu.Unlock()
	if !ok || bettingClosed {
		respondComponentEphemeral(s, i, "Cược đã đóng hoặc trận đấu này đã hết hạn.")
		return
	}

	bettorID := i.Member.User.ID
	okEscrow, err := cmd.svc.EscrowPoints(context.Background(), pd.GuildID, bettorID, amount)
	if err != nil {
		log.Error().Err(err).Msg("duel: failed to escrow bet")
		respondComponentEphemeral(s, i, "Có lỗi xảy ra khi đặt cược.")
		return
	}
	if !okEscrow {
		respondComponentEphemeral(s, i, "Bạn không đủ điểm để đặt cược này.")
		return
	}

	cmd.mu.Lock()
	// Re-check under lock: the duel may have just resolved while we were escrowing.
	pd, ok = cmd.pending[challengeID]
	stillOpen := ok && pd.Accepted && time.Now().Before(pd.BetDeadline)
	if stillOpen {
		pd.Bets = append(pd.Bets, sideBet{BettorID: bettorID, PickedID: pickedID, Amount: amount})
	}
	betsSnapshot := []sideBet{}
	if ok {
		betsSnapshot = make([]sideBet, len(pd.Bets))
		copy(betsSnapshot, pd.Bets)
	}
	cmd.mu.Unlock()

	if !stillOpen {
		// Betting closed in the tiny window while we were escrowing — refund immediately.
		if err := cmd.svc.CreditPoints(context.Background(), pd.GuildID, bettorID, amount); err != nil {
			log.Error().Err(err).Msg("duel: failed to refund late bet")
		}
		respondComponentEphemeral(s, i, "Cược đã đóng ngay lúc bạn gửi, đã hoàn lại điểm.")
		return
	}

	respondComponentEphemeral(s, i, fmt.Sprintf("✅ Đã cược **%d điểm** cho <@%s>.", amount, pickedID))

	content := bettingCountdownContent(pd, time.Until(pd.BetDeadline).Round(time.Second)) + betPoolSummary(pd, betsSnapshot)
	if _, err := s.ChannelMessageEditComplex(&discordgo.MessageEdit{
		Channel:         pd.ChannelID,
		ID:              pd.MessageID,
		Content:         &content,
		AllowedMentions: &discordgo.MessageAllowedMentions{},
	}); err != nil {
		log.Error().Err(err).Msg("duel: failed to update message with bet pool")
	}
}

// betPoolSummary renders the accumulated side-bet pool, or "" if empty.
func betPoolSummary(pd *pendingDuel, bets []sideBet) string {
	if len(bets) == 0 {
		return ""
	}
	totals := map[string]int{}
	for _, b := range bets {
		totals[b.PickedID] += b.Amount
	}
	var lines strings.Builder
	lines.WriteString(fmt.Sprintf(
		"\n\n📊 **Cược ngoài lề (%d người):** %s: %d điểm | %s: %d điểm\n",
		len(bets), pd.ChallengerLabel, totals[pd.ChallengerID], pd.OpponentLabel, totals[pd.OpponentID],
	))
	for _, b := range bets {
		pickedLabel := pd.OpponentLabel
		if b.PickedID == pd.ChallengerID {
			pickedLabel = pd.ChallengerLabel
		}
		lines.WriteString(fmt.Sprintf("• <@%s> cược **%d điểm** cho %s\n", b.BettorID, b.Amount, pickedLabel))
	}
	return lines.String()
}

// refundAllBets returns escrowed points to every bettor (duel declined/expired
// before resolving) and returns a short summary string.
func (cmd *DuelCommand) refundAllBets(ctx context.Context, pd *pendingDuel) string {
	cmd.mu.Lock()
	bets := make([]sideBet, len(pd.Bets))
	copy(bets, pd.Bets)
	cmd.mu.Unlock()

	if len(bets) == 0 {
		return ""
	}
	for _, b := range bets {
		if err := cmd.svc.CreditPoints(ctx, pd.GuildID, b.BettorID, b.Amount); err != nil {
			log.Error().Err(err).Msg("duel: failed to refund bet")
		}
	}
	return fmt.Sprintf("\n\n💰 Đã hoàn lại cược cho %d người.", len(bets))
}

// settleBets pays out side bets pari-mutuel style: the losing side's stakes
// are split among winners proportional to their stake. If a draw, or if
// nobody bet on the actual winner, everyone is refunded.
func (cmd *DuelCommand) settleBets(ctx context.Context, pd *pendingDuel, result *duelstats.Result) string {
	cmd.mu.Lock()
	bets := make([]sideBet, len(pd.Bets))
	copy(bets, pd.Bets)
	cmd.mu.Unlock()

	if len(bets) == 0 {
		return ""
	}

	if result.Draw {
		for _, b := range bets {
			if err := cmd.svc.CreditPoints(ctx, pd.GuildID, b.BettorID, b.Amount); err != nil {
				log.Error().Err(err).Msg("duel: failed to refund bet on draw")
			}
		}
		return fmt.Sprintf("\n\n💰 Trận hoà nên đã hoàn lại cược cho %d người.", len(bets))
	}

	winnerID := pd.OpponentID
	if result.ChallengerWon {
		winnerID = pd.ChallengerID
	}

	totalPool, winningPool := 0, 0
	for _, b := range bets {
		totalPool += b.Amount
		if b.PickedID == winnerID {
			winningPool += b.Amount
		}
	}

	if winningPool == 0 {
		for _, b := range bets {
			if err := cmd.svc.CreditPoints(ctx, pd.GuildID, b.BettorID, b.Amount); err != nil {
				log.Error().Err(err).Msg("duel: failed to refund bet with no winners")
			}
		}
		return fmt.Sprintf("\n\n💰 Không ai cược đúng người thắng, đã hoàn lại cược cho %d người.", len(bets))
	}

	var lines strings.Builder
	lines.WriteString("\n\n💰 **Kết quả cược:**\n")
	for _, b := range bets {
		if b.PickedID != winnerID {
			lines.WriteString(fmt.Sprintf("<@%s> mất %d điểm cược\n", b.BettorID, b.Amount))
			continue
		}
		payout := b.Amount * totalPool / winningPool
		if err := cmd.svc.CreditPoints(ctx, pd.GuildID, b.BettorID, payout); err != nil {
			log.Error().Err(err).Msg("duel: failed to pay out bet")
		}
		lines.WriteString(fmt.Sprintf("<@%s> thắng cược, nhận %d điểm\n", b.BettorID, payout))
	}
	return lines.String()
}

// animateAndResolve plays a few random dice-roll frames, then resolves the
// duel for real and edits the message to the final result.
func (cmd *DuelCommand) animateAndResolve(s *discordgo.Session, challengeID string) {
	cmd.mu.Lock()
	pd, ok := cmd.pending[challengeID]
	cmd.mu.Unlock()
	if !ok {
		return
	}

	for n := 0; n < rollAnimationFrames; n++ {
		time.Sleep(rollAnimationDelay)
		content := rollAnimationFrame(pd.ChallengerID, pd.OpponentID)
		if _, err := s.ChannelMessageEditComplex(&discordgo.MessageEdit{
			Channel:         pd.ChannelID,
			ID:              pd.MessageID,
			Content:         &content,
			AllowedMentions: &discordgo.MessageAllowedMentions{},
		}); err != nil {
			log.Error().Err(err).Msg("duel: failed to edit animation frame")
		}
	}

	ctx := context.Background()
	result, err := cmd.svc.Resolve(ctx, pd.GuildID, pd.ChallengerID, pd.OpponentID, pd.Wager)

	cmd.mu.Lock()
	delete(cmd.pending, challengeID)
	cmd.mu.Unlock()

	if err != nil {
		log.Error().Err(err).Msg("duel: failed to resolve")
		errContent := "❌ Có lỗi xảy ra khi tổ chức trận đấu." + cmd.refundAllBets(ctx, pd)
		_, _ = s.ChannelMessageEditComplex(&discordgo.MessageEdit{
			Channel:         pd.ChannelID,
			ID:              pd.MessageID,
			Content:         &errContent,
			AllowedMentions: &discordgo.MessageAllowedMentions{},
		})
		return
	}

	betSummary := cmd.settleBets(ctx, pd, result)
	content := formatDuelResult(pd.ChallengerID, pd.OpponentID, pd.Wager, result) + betSummary
	if _, err := s.ChannelMessageEditComplex(&discordgo.MessageEdit{
		Channel:         pd.ChannelID,
		ID:              pd.MessageID,
		Content:         &content,
		AllowedMentions: &discordgo.MessageAllowedMentions{},
	}); err != nil {
		log.Error().Err(err).Msg("duel: failed to edit final result")
	}
}

// expire edits the challenge message to show it timed out, if still pending.
func (cmd *DuelCommand) expire(s *discordgo.Session, challengeID string) {
	cmd.mu.Lock()
	pd, ok := cmd.pending[challengeID]
	if ok {
		delete(cmd.pending, challengeID)
	}
	cmd.mu.Unlock()
	if !ok {
		return
	}

	refundSummary := cmd.refundAllBets(context.Background(), pd)
	content := fmt.Sprintf("⌛ Thách đấu từ <@%s> tới <@%s> đã hết hạn (không phản hồi kịp).%s", pd.ChallengerID, pd.OpponentID, refundSummary)
	components := []discordgo.MessageComponent{}
	_, err := s.ChannelMessageEditComplex(&discordgo.MessageEdit{
		Channel:         pd.ChannelID,
		ID:              pd.MessageID,
		Content:         &content,
		Components:      &components,
		AllowedMentions: &discordgo.MessageAllowedMentions{},
	})
	if err != nil {
		log.Error().Err(err).Msg("duel: failed to edit expired challenge message")
	}
}

func acceptCountdownContent(challengerID, opponentID string, wager int, remaining time.Duration) string {
	return fmt.Sprintf(
		"⚔️ <@%s> thách đấu <@%s>! Cược **%d điểm**.\n⏳ Còn **%ds** để <@%s> phản hồi.",
		challengerID, opponentID, wager, int(remaining.Seconds()), opponentID,
	)
}

func bettingCountdownContent(pd *pendingDuel, remaining time.Duration) string {
	return fmt.Sprintf(
		"🥁 <@%s> vs <@%s> đã đồng ý đấu! Cược **%d điểm**.\n⏳ Còn **%ds** để người ngoài đặt cược (khoá cược trước %ds trận đấu).",
		pd.ChallengerID, pd.OpponentID, pd.Wager, int(remaining.Seconds()), int(bettingLock.Seconds()),
	)
}

func acceptButtons(challengeID string, disabled bool) []discordgo.MessageComponent {
	return []discordgo.MessageComponent{
		discordgo.ActionsRow{
			Components: []discordgo.MessageComponent{
				discordgo.Button{
					Label:    "Chấp nhận",
					Style:    discordgo.SuccessButton,
					CustomID: acceptID + challengeID,
					Disabled: disabled,
				},
				discordgo.Button{
					Label:    "Từ chối",
					Style:    discordgo.DangerButton,
					CustomID: declineID + challengeID,
					Disabled: disabled,
				},
			},
		},
	}
}

func betButtons(challengeID, challengerID, challengerLabel, opponentID, opponentLabel string, disabled bool) []discordgo.MessageComponent {
	return []discordgo.MessageComponent{
		discordgo.ActionsRow{
			Components: []discordgo.MessageComponent{
				discordgo.Button{
					Label:    "Cược cho " + truncateLabel(challengerLabel),
					Style:    discordgo.SecondaryButton,
					Emoji:    &discordgo.ComponentEmoji{Name: "💰"},
					CustomID: betButtonID + challengeID + ":" + challengerID,
					Disabled: disabled,
				},
				discordgo.Button{
					Label:    "Cược cho " + truncateLabel(opponentLabel),
					Style:    discordgo.SecondaryButton,
					Emoji:    &discordgo.ComponentEmoji{Name: "💰"},
					CustomID: betButtonID + challengeID + ":" + opponentID,
					Disabled: disabled,
				},
			},
		},
	}
}

func respondComponentEphemeral(s *discordgo.Session, i *discordgo.InteractionCreate, message string) {
	respondComponentEphemeralWithMentions(s, i, message, nil, "duel: failed to send ephemeral component response")
}

// respondComponentEphemeralWithMentions is the shared implementation behind
// respondComponentEphemeral and werewolf's respondComponentEphemeralMentioned
// — extracted so the two callers (which only ever differed in their
// AllowedMentions and log message) don't duplicate the whole
// InteractionRespond call between them.
func respondComponentEphemeralWithMentions(s *discordgo.Session, i *discordgo.InteractionCreate, message string, mentions *discordgo.MessageAllowedMentions, logMsg string) {
	err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Content:         message,
			Flags:           discordgo.MessageFlagsEphemeral,
			AllowedMentions: mentions,
		},
	})
	if err != nil {
		log.Error().Err(err).Msg(logMsg)
	}
}

func clampWager(raw interface{}) int {
	wager := defaultWager
	if w, ok := raw.(float64); ok {
		wager = int(w)
	}
	if wager < 1 {
		wager = 1
	}
	if wager > maxWager {
		wager = maxWager
	}
	return wager
}

func formatDuelResult(challengerID, opponentID string, wager int, r *duelstats.Result) string {
	header := fmt.Sprintf(
		"⚔️ <@%s> vs <@%s> — cược %d điểm\n🎲 <@%s>: %s %s = **%d**   |   <@%s>: %s %s = **%d**\n",
		challengerID, opponentID, wager,
		challengerID, diceFace(r.ChallengerDice[0]), diceFace(r.ChallengerDice[1]), r.ChallengerSum,
		opponentID, diceFace(r.OpponentDice[0]), diceFace(r.OpponentDice[1]), r.OpponentSum,
	)

	if r.Draw {
		return header + fmt.Sprintf("🤝 %s\n\n**%s** — %d điểm (%d/%d/%d)\n**%s** — %d điểm (%d/%d/%d)",
			pick(drawLines),
			nameTag(challengerID), r.Challenger.Points, r.Challenger.Wins, r.Challenger.Losses, r.Challenger.Draws,
			nameTag(opponentID), r.Opponent.Points, r.Opponent.Wins, r.Opponent.Losses, r.Opponent.Draws,
		)
	}

	winnerID, loserID := challengerID, opponentID
	winnerStat, loserStat := r.Challenger, r.Opponent
	if !r.ChallengerWon {
		winnerID, loserID = opponentID, challengerID
		winnerStat, loserStat = r.Opponent, r.Challenger
	}

	emoji := "🎉"
	line := pick(normalWinLines)
	if r.Critical {
		emoji = "💥"
		line = pick(critWinLines)
	}

	streakNote := ""
	if winnerStat.CurrentStreak >= 3 {
		streakNote = fmt.Sprintf(" 🔥 đang thắng %d trận liên tiếp!", winnerStat.CurrentStreak)
	}

	transferNote := ""
	if r.Transfer > 0 {
		transferNote = fmt.Sprintf(" (+%d/-%d điểm)", r.Transfer, r.Transfer)
	}

	return header + fmt.Sprintf(
		"%s <@%s> %s%s%s\n\n**%s** — %d điểm (%d/%d/%d)\n**%s** — %d điểm (%d/%d/%d)",
		emoji, winnerID, line, transferNote, streakNote,
		nameTag(winnerID), winnerStat.Points, winnerStat.Wins, winnerStat.Losses, winnerStat.Draws,
		nameTag(loserID), loserStat.Points, loserStat.Wins, loserStat.Losses, loserStat.Draws,
	)
}

func nameTag(userID string) string {
	return fmt.Sprintf("<@%s>", userID)
}

// pingOnce builds a mention string and matching AllowedMentions allow-list
// for a deduplicated set of user IDs, for a single deliberate group ping
// (e.g. everyone involved when a duel/tournament actually starts).
func pingOnce(userIDs []string) (string, *discordgo.MessageAllowedMentions) {
	seen := make(map[string]bool, len(userIDs))
	var unique []string
	var tags []string
	for _, id := range userIDs {
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		unique = append(unique, id)
		tags = append(tags, nameTag(id))
	}
	if len(unique) == 0 {
		return "", &discordgo.MessageAllowedMentions{}
	}
	return strings.Join(tags, " "), &discordgo.MessageAllowedMentions{Users: unique}
}

func pick(lines []string) string {
	return lines[rand.Intn(len(lines))]
}

// displayLabel picks the best short label for a user: global name, else username.
func displayLabel(u *discordgo.User) string {
	if u.GlobalName != "" {
		return u.GlobalName
	}
	return u.Username
}

// truncateLabel keeps a button label within Discord's 80-char limit.
func truncateLabel(label string) string {
	const maxLen = 60
	if len(label) > maxLen {
		return label[:maxLen] + "..."
	}
	return label
}
