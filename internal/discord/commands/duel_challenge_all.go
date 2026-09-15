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
	tournamentJoinID      = "duelt:join:"
	tournamentLeaveID     = "duelt:leave:"
	tournamentCancelID    = "duelt:cancel:"
	tournamentChampSelect = "duelt:champselect:"
	tournamentChampBetID  = "duelt:champbet:"
	tournamentMinPlayers  = 2
	tournamentMaxPlayers  = 20 // Discord select menus cap at 25 options
	joinCloseBefore       = 30 * time.Minute
	betCloseBefore        = 5 * time.Minute
	minLeadTime           = 35 * time.Minute
	maxLeadTime           = 24 * time.Hour
	startTimeLayout       = "15:04 02/01/2006"
)

// championBet is a spectator's wager on who will win the whole tournament.
type championBet struct {
	BettorID string
	PickedID string
	Amount   int
}

// pendingTournament tracks a scheduled tournament from signup through betting to kickoff.
type pendingTournament struct {
	GuildID      string
	ChannelID    string
	MessageID    string
	StarterID    string
	StartAt      time.Time
	Participants map[string]bool
	JoinOrder    []string
	Labels       map[string]string
	Bets         []championBet
	Cancelled    bool
}

// DuelChallengeAllCommand handles /duel-challenge-all: an admin-scheduled dice tournament.
type DuelChallengeAllCommand struct {
	svc      *duelstats.Service
	timezone string

	mu      sync.Mutex
	pending map[string]*pendingTournament
}

// NewDuelChallengeAllCommand creates a new DuelChallengeAllCommand
func NewDuelChallengeAllCommand(svc *duelstats.Service, timezone string) *DuelChallengeAllCommand {
	return &DuelChallengeAllCommand{svc: svc, timezone: timezone, pending: make(map[string]*pendingTournament)}
}

// Definition returns the Discord application command definition
func (cmd *DuelChallengeAllCommand) Definition() *discordgo.ApplicationCommand {
	return &discordgo.ApplicationCommand{
		Name:        "duel-challenge-all",
		Description: "Lên lịch giải đấu Duel vào một giờ cố định (Admin only)",
		Options: []*discordgo.ApplicationCommandOption{
			{
				Type:        discordgo.ApplicationCommandOptionString,
				Name:        "start",
				Description: "Giờ bắt đầu, định dạng \"HH:MM DD/MM/YYYY\", ví dụ \"20:00 20/08/2026\"",
				Required:    true,
			},
		},
	}
}

// Handle posts the tournament signup message and schedules the join-close,
// bet-close, and start transitions.
func (cmd *DuelChallengeAllCommand) Handle(s *discordgo.Session, i *discordgo.InteractionCreate) {
	if !hasAdminPermission(i.Member) {
		respondError(s, i, "Bạn cần quyền Administrator hoặc Manage Server để dùng lệnh này.")
		return
	}

	options := parseOptions(i.ApplicationCommandData().Options)
	startStr, _ := options["start"].(string)
	startStr = strings.TrimSpace(startStr)

	loc, err := time.LoadLocation(cmd.timezone)
	if err != nil {
		loc = time.UTC
	}
	startAt, err := time.ParseInLocation(startTimeLayout, startStr, loc)
	if err != nil {
		respondError(s, i, fmt.Sprintf("Không đọc được giờ bắt đầu. Dùng định dạng \"HH:MM DD/MM/YYYY\", ví dụ \"20:00 20/08/2026\".\nLỗi: %s", err.Error()))
		return
	}

	lead := time.Until(startAt)
	if lead < minLeadTime {
		respondError(s, i, fmt.Sprintf("Giờ bắt đầu phải cách hiện tại ít nhất %d phút (để có đủ thời gian đăng ký và cược).", int(minLeadTime.Minutes())))
		return
	}
	if lead > maxLeadTime {
		respondError(s, i, fmt.Sprintf("Giờ bắt đầu không được xa quá %d giờ kể từ bây giờ (trạng thái giải đấu chỉ lưu tạm, bot restart giữa chừng sẽ mất).", int(maxLeadTime.Hours())))
		return
	}

	starterID := i.Member.User.ID
	tournamentID := i.Interaction.ID

	pt := &pendingTournament{
		GuildID:      i.GuildID,
		ChannelID:    i.ChannelID,
		StarterID:    starterID,
		StartAt:      startAt,
		Participants: map[string]bool{},
		JoinOrder:    []string{},
		Labels:       map[string]string{},
	}

	err = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Content:         cmd.signupContent(pt),
			Components:      signupButtons(tournamentID, false),
			AllowedMentions: &discordgo.MessageAllowedMentions{},
		},
	})
	if err != nil {
		log.Error().Err(err).Msg("duel-challenge-all: failed to post signup")
		return
	}

	msg, err := s.InteractionResponse(i.Interaction)
	if err != nil {
		log.Error().Err(err).Msg("duel-challenge-all: failed to fetch signup message")
		return
	}
	pt.MessageID = msg.ID

	cmd.mu.Lock()
	cmd.pending[tournamentID] = pt
	cmd.mu.Unlock()

	go cmd.runLifecycle(s, tournamentID)
}

// runLifecycle drives the tournament through signup -> betting -> locked ->
// start, with a live countdown edited periodically at each phase.
func (cmd *DuelChallengeAllCommand) runLifecycle(s *discordgo.Session, tournamentID string) {
	cmd.mu.Lock()
	pt := cmd.pending[tournamentID]
	cmd.mu.Unlock()
	if pt == nil {
		return
	}

	joinCloseAt := pt.StartAt.Add(-joinCloseBefore)
	betCloseAt := pt.StartAt.Add(-betCloseBefore)

	// Phase 1: signup, until joinCloseAt.
	if !cmd.countdownLoop(s, tournamentID, joinCloseAt, func(remaining time.Duration) {
		cmd.mu.Lock()
		pt, ok := cmd.pending[tournamentID]
		cmd.mu.Unlock()
		if !ok {
			return
		}
		content := cmd.signupContent(pt) + fmt.Sprintf("\n⏳ Đóng đăng ký sau **%s**.", formatRemaining(remaining))
		cmd.editMessage(s, pt, content, nil)
	}) {
		return // cancelled mid-phase
	}

	cmd.mu.Lock()
	pt, ok := cmd.pending[tournamentID]
	if ok {
		count := len(pt.JoinOrder)
		if count < tournamentMinPlayers {
			delete(cmd.pending, tournamentID)
			cmd.mu.Unlock()
			content := fmt.Sprintf("⌛ Đăng ký kết thúc — không đủ %d người tham gia (%d người), huỷ giải đấu.", tournamentMinPlayers, count)
			cmd.editMessage(s, pt, content, []discordgo.MessageComponent{})
			return
		}
	}
	cmd.mu.Unlock()
	if !ok {
		return
	}

	// Phase 2: betting on the champion, until betCloseAt.
	champComponents := championSelectComponents(tournamentID, pt.JoinOrder, pt.Labels, false)
	cmd.editMessage(s, pt, cmd.bettingPhaseContent(pt, nil, betCloseAt.Sub(time.Now())), champComponents)

	if !cmd.countdownLoop(s, tournamentID, betCloseAt, func(remaining time.Duration) {
		cmd.mu.Lock()
		pt, ok := cmd.pending[tournamentID]
		var betsSnapshot []championBet
		if ok {
			betsSnapshot = make([]championBet, len(pt.Bets))
			copy(betsSnapshot, pt.Bets)
		}
		cmd.mu.Unlock()
		if !ok {
			return
		}
		content := cmd.bettingPhaseContent(pt, betsSnapshot, remaining)
		cmd.editMessage(s, pt, content, nil)
	}) {
		return
	}

	cmd.mu.Lock()
	pt, ok = cmd.pending[tournamentID]
	cmd.mu.Unlock()
	if !ok {
		return
	}

	// Phase 3: locked, waiting for kickoff.
	lockedContent := fmt.Sprintf("🔒 Cược đã đóng. Giải đấu **%d người** bắt đầu sau %s.", len(pt.JoinOrder), formatRemaining(time.Until(pt.StartAt)))
	cmd.editMessage(s, pt, lockedContent, []discordgo.MessageComponent{})

	if remaining := time.Until(pt.StartAt); remaining > 0 {
		time.Sleep(remaining)
	}

	cmd.mu.Lock()
	pt, ok = cmd.pending[tournamentID]
	if ok {
		delete(cmd.pending, tournamentID)
	}
	cmd.mu.Unlock()
	if !ok || pt.Cancelled {
		return
	}

	cmd.runTournament(s, pt)
}

// countdownLoop edits the message via onTick at an adaptive interval until
// deadline, or returns false early if the tournament was cancelled/removed.
func (cmd *DuelChallengeAllCommand) countdownLoop(s *discordgo.Session, tournamentID string, deadline time.Time, onTick func(remaining time.Duration)) bool {
	for {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return true
		}
		wait := nextTick(remaining)
		if remaining < wait {
			wait = remaining
		}
		time.Sleep(wait)

		cmd.mu.Lock()
		pt, ok := cmd.pending[tournamentID]
		cancelled := ok && pt.Cancelled
		cmd.mu.Unlock()
		if !ok || cancelled {
			return false
		}

		remaining = time.Until(deadline)
		if remaining <= 0 {
			return true
		}
		onTick(remaining)
	}
}

// nextTick picks an adaptive countdown-edit interval so long waits don't spam edits.
func nextTick(remaining time.Duration) time.Duration {
	switch {
	case remaining > 10*time.Minute:
		return 2 * time.Minute
	case remaining > 1*time.Minute:
		return 30 * time.Second
	default:
		return 10 * time.Second
	}
}

func formatRemaining(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	d = d.Round(time.Second)
	if d >= time.Minute {
		return fmt.Sprintf("%dp%02ds", int(d.Minutes()), int(d.Seconds())%60)
	}
	return fmt.Sprintf("%ds", int(d.Seconds()))
}

func (cmd *DuelChallengeAllCommand) editMessage(s *discordgo.Session, pt *pendingTournament, content string, components []discordgo.MessageComponent) {
	edit := &discordgo.MessageEdit{
		Channel:         pt.ChannelID,
		ID:              pt.MessageID,
		Content:         &content,
		AllowedMentions: &discordgo.MessageAllowedMentions{},
	}
	if components != nil {
		edit.Components = &components
	}
	if _, err := s.ChannelMessageEditComplex(edit); err != nil {
		log.Error().Err(err).Msg("duel-challenge-all: failed to edit message")
	}
}

// HandleComponent processes Join/Leave/Cancel/champion-select clicks. Returns
// true if it handled the interaction (customID matched this command).
func (cmd *DuelChallengeAllCommand) HandleComponent(s *discordgo.Session, i *discordgo.InteractionCreate) bool {
	customID := i.MessageComponentData().CustomID

	switch {
	case strings.HasPrefix(customID, tournamentJoinID):
		cmd.handleJoinLeave(s, i, strings.TrimPrefix(customID, tournamentJoinID), true)
		return true
	case strings.HasPrefix(customID, tournamentLeaveID):
		cmd.handleJoinLeave(s, i, strings.TrimPrefix(customID, tournamentLeaveID), false)
		return true
	case strings.HasPrefix(customID, tournamentCancelID):
		cmd.handleCancel(s, i, strings.TrimPrefix(customID, tournamentCancelID))
		return true
	case strings.HasPrefix(customID, tournamentChampSelect):
		cmd.handleChampionSelect(s, i, strings.TrimPrefix(customID, tournamentChampSelect))
		return true
	default:
		return false
	}
}

func (cmd *DuelChallengeAllCommand) handleJoinLeave(s *discordgo.Session, i *discordgo.InteractionCreate, tournamentID string, joining bool) {
	cmd.mu.Lock()
	pt, ok := cmd.pending[tournamentID]
	if !ok || pt.Cancelled {
		cmd.mu.Unlock()
		respondComponentEphemeral(s, i, "Giải đấu này đã kết thúc hoặc hết hạn đăng ký.")
		return
	}
	clickerID := i.Member.User.ID
	if joining {
		if !pt.Participants[clickerID] {
			if len(pt.JoinOrder) >= tournamentMaxPlayers {
				cmd.mu.Unlock()
				respondComponentEphemeral(s, i, fmt.Sprintf("Giải đấu đã đủ %d người, không thể tham gia thêm.", tournamentMaxPlayers))
				return
			}
			pt.Participants[clickerID] = true
			pt.JoinOrder = append(pt.JoinOrder, clickerID)
			pt.Labels[clickerID] = displayLabel(i.Member.User)
		}
	} else {
		if pt.Participants[clickerID] {
			delete(pt.Participants, clickerID)
			for idx, id := range pt.JoinOrder {
				if id == clickerID {
					pt.JoinOrder = append(pt.JoinOrder[:idx], pt.JoinOrder[idx+1:]...)
					break
				}
			}
		}
	}
	content := cmd.signupContent(pt)
	cmd.mu.Unlock()

	_ = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseUpdateMessage,
		Data: &discordgo.InteractionResponseData{
			Content:         content,
			Components:      signupButtons(tournamentID, false),
			AllowedMentions: &discordgo.MessageAllowedMentions{},
		},
	})
}

func (cmd *DuelChallengeAllCommand) handleCancel(s *discordgo.Session, i *discordgo.InteractionCreate, tournamentID string) {
	cmd.mu.Lock()
	pt, ok := cmd.pending[tournamentID]
	if !ok {
		cmd.mu.Unlock()
		respondComponentEphemeral(s, i, "Giải đấu này đã kết thúc hoặc hết hạn.")
		return
	}
	if i.Member.User.ID != pt.StarterID {
		cmd.mu.Unlock()
		respondComponentEphemeral(s, i, "Chỉ người mở giải đấu mới được huỷ.")
		return
	}
	pt.Cancelled = true
	delete(cmd.pending, tournamentID)
	refundNeeded := len(pt.Bets) > 0
	cmd.mu.Unlock()

	summary := ""
	if refundNeeded {
		summary = cmd.refundChampionBets(context.Background(), pt)
	}

	_ = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseUpdateMessage,
		Data: &discordgo.InteractionResponseData{
			Content:    "🚫 Giải đấu đã bị huỷ." + summary,
			Components: []discordgo.MessageComponent{},
		},
	})
}

func (cmd *DuelChallengeAllCommand) handleChampionSelect(s *discordgo.Session, i *discordgo.InteractionCreate, tournamentID string) {
	values := i.MessageComponentData().Values
	if len(values) == 0 {
		return
	}
	pickedID := values[0]

	cmd.mu.Lock()
	pt, ok := cmd.pending[tournamentID]
	var alreadyBet bool
	if ok {
		for _, b := range pt.Bets {
			if b.BettorID == i.Member.User.ID {
				alreadyBet = true
				break
			}
		}
	}
	cmd.mu.Unlock()

	if !ok {
		respondComponentEphemeral(s, i, "Giải đấu này đã kết thúc hoặc hết hạn cược.")
		return
	}
	if alreadyBet {
		respondComponentEphemeral(s, i, "Bạn đã cược nhà vô địch cho giải đấu này rồi.")
		return
	}

	err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseModal,
		Data: &discordgo.InteractionResponseData{
			CustomID: tournamentChampBetID + tournamentID + ":" + pickedID,
			Title:    "Cược nhà vô địch",
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
		log.Error().Err(err).Msg("duel-challenge-all: failed to open champion bet modal")
	}
}

// HandleModalSubmit processes the champion-bet amount modal.
func (cmd *DuelChallengeAllCommand) HandleModalSubmit(s *discordgo.Session, i *discordgo.InteractionCreate) {
	data := i.ModalSubmitData()
	if !strings.HasPrefix(data.CustomID, tournamentChampBetID) {
		return
	}
	rest := strings.TrimPrefix(data.CustomID, tournamentChampBetID)
	parts := strings.SplitN(rest, ":", 2)
	if len(parts) != 2 {
		return
	}
	tournamentID, pickedID := parts[0], parts[1]

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
	pt, ok := cmd.pending[tournamentID]
	cmd.mu.Unlock()
	if !ok {
		respondComponentEphemeral(s, i, "Giải đấu này đã kết thúc hoặc hết hạn cược.")
		return
	}

	bettorID := i.Member.User.ID
	okEscrow, err := cmd.svc.EscrowPoints(context.Background(), pt.GuildID, bettorID, amount)
	if err != nil {
		log.Error().Err(err).Msg("duel-challenge-all: failed to escrow champion bet")
		respondComponentEphemeral(s, i, "Có lỗi xảy ra khi đặt cược.")
		return
	}
	if !okEscrow {
		respondComponentEphemeral(s, i, "Bạn không đủ điểm để đặt cược này.")
		return
	}

	cmd.mu.Lock()
	pt, ok = cmd.pending[tournamentID]
	var betsSnapshot []championBet
	if ok {
		pt.Bets = append(pt.Bets, championBet{BettorID: bettorID, PickedID: pickedID, Amount: amount})
		betsSnapshot = make([]championBet, len(pt.Bets))
		copy(betsSnapshot, pt.Bets)
	}
	cmd.mu.Unlock()

	if !ok {
		if err := cmd.svc.CreditPoints(context.Background(), pt.GuildID, bettorID, amount); err != nil {
			log.Error().Err(err).Msg("duel-challenge-all: failed to refund late champion bet")
		}
		respondComponentEphemeral(s, i, "Cược đã đóng ngay lúc bạn gửi, đã hoàn lại điểm.")
		return
	}

	pickedLabel := pt.Labels[pickedID]
	if pickedLabel == "" {
		pickedLabel = nameTag(pickedID)
	}
	respondComponentEphemeral(s, i, fmt.Sprintf("✅ Đã cược **%d điểm** cho %s vô địch.", amount, pickedLabel))

	content := cmd.bettingPhaseContent(pt, betsSnapshot, time.Until(pt.StartAt.Add(-betCloseBefore)))
	cmd.editMessage(s, pt, content, nil)
}

// refundChampionBets returns escrowed points to every champion bettor.
func (cmd *DuelChallengeAllCommand) refundChampionBets(ctx context.Context, pt *pendingTournament) string {
	if len(pt.Bets) == 0 {
		return ""
	}
	for _, b := range pt.Bets {
		if err := cmd.svc.CreditPoints(ctx, pt.GuildID, b.BettorID, b.Amount); err != nil {
			log.Error().Err(err).Msg("duel-challenge-all: failed to refund champion bet")
		}
	}
	return fmt.Sprintf("\n\n💰 Đã hoàn lại cược nhà vô địch cho %d người.", len(pt.Bets))
}

// settleChampionBets pays out champion bets pari-mutuel style, or refunds
// everyone if nobody picked the actual champion.
func (cmd *DuelChallengeAllCommand) settleChampionBets(ctx context.Context, pt *pendingTournament, championID string) string {
	if len(pt.Bets) == 0 {
		return ""
	}

	totalPool, winningPool := 0, 0
	for _, b := range pt.Bets {
		totalPool += b.Amount
		if b.PickedID == championID {
			winningPool += b.Amount
		}
	}

	if winningPool == 0 {
		for _, b := range pt.Bets {
			if err := cmd.svc.CreditPoints(ctx, pt.GuildID, b.BettorID, b.Amount); err != nil {
				log.Error().Err(err).Msg("duel-challenge-all: failed to refund champion bet with no winners")
			}
		}
		return fmt.Sprintf("\n\n💰 Không ai cược đúng nhà vô địch, đã hoàn lại cược cho %d người.", len(pt.Bets))
	}

	var lines strings.Builder
	lines.WriteString("\n\n💰 **Kết quả cược vô địch:**\n")
	for _, b := range pt.Bets {
		if b.PickedID != championID {
			lines.WriteString(fmt.Sprintf("<@%s> mất %d điểm cược\n", b.BettorID, b.Amount))
			continue
		}
		payout := b.Amount * totalPool / winningPool
		if err := cmd.svc.CreditPoints(ctx, pt.GuildID, b.BettorID, payout); err != nil {
			log.Error().Err(err).Msg("duel-challenge-all: failed to pay out champion bet")
		}
		lines.WriteString(fmt.Sprintf("<@%s> thắng cược, nhận %d điểm\n", b.BettorID, payout))
	}
	return lines.String()
}

// runTournament runs a single-elimination bracket to completion, posting each
// round's results as a new message, then settles champion bets.
func (cmd *DuelChallengeAllCommand) runTournament(s *discordgo.Session, pt *pendingTournament) {
	ctx := context.Background()
	players := make([]string, len(pt.JoinOrder))
	copy(players, pt.JoinOrder)
	rand.Shuffle(len(players), func(i, j int) { players[i], players[j] = players[j], players[i] })

	pingIDs := make([]string, 0, len(pt.JoinOrder)+len(pt.Bets))
	pingIDs = append(pingIDs, pt.JoinOrder...)
	for _, b := range pt.Bets {
		pingIDs = append(pingIDs, b.BettorID)
	}
	pingText, allowedMentions := pingOnce(pingIDs)

	if _, err := s.ChannelMessageSendComplex(pt.ChannelID, &discordgo.MessageSend{
		Content:         fmt.Sprintf("🏆 Giải đấu bắt đầu! %s", pingText),
		AllowedMentions: allowedMentions,
	}); err != nil {
		log.Error().Err(err).Msg("duel-challenge-all: failed to post start message")
	}

	round := 1
	for len(players) > 1 {
		var next []string
		var lines strings.Builder
		fmt.Fprintf(&lines, "🏆 **Vòng %d**\n\n", round)

		i := 0
		for i+1 < len(players) {
			a, b := players[i], players[i+1]
			i += 2

			var winner string
			for attempt := 0; attempt < 5; attempt++ {
				result, err := cmd.svc.Resolve(ctx, pt.GuildID, a, b, 0)
				if err != nil {
					log.Error().Err(err).Msg("duel-challenge-all: failed to resolve match")
					winner = a
					break
				}
				if result.Draw {
					lines.WriteString(fmt.Sprintf("🤝 %s (%d) vs %s (%d) — hoà, đấu lại...\n", nameTag(a), result.ChallengerSum, nameTag(b), result.OpponentSum))
					continue
				}
				winner = b
				if result.ChallengerWon {
					winner = a
				}
				critNote := ""
				if result.Critical {
					critNote = " 💥"
				}
				lines.WriteString(fmt.Sprintf("%s (%d) vs %s (%d) → 🏅 %s%s\n", nameTag(a), result.ChallengerSum, nameTag(b), result.OpponentSum, nameTag(winner), critNote))
				break
			}
			if winner == "" {
				winner = a
			}
			next = append(next, winner)
		}
		if i < len(players) {
			bye := players[i]
			lines.WriteString(fmt.Sprintf("💤 %s được miễn đấu (bye), vào thẳng vòng sau\n", nameTag(bye)))
			next = append(next, bye)
		}

		if _, err := s.ChannelMessageSendComplex(pt.ChannelID, &discordgo.MessageSend{
			Content:         lines.String(),
			AllowedMentions: &discordgo.MessageAllowedMentions{},
		}); err != nil {
			log.Error().Err(err).Msg("duel-challenge-all: failed to post round results")
		}

		players = next
		round++
		time.Sleep(1 * time.Second)
	}

	champion := players[0]
	betSummary := cmd.settleChampionBets(ctx, pt, champion)
	if _, err := s.ChannelMessageSendComplex(pt.ChannelID, &discordgo.MessageSend{
		Content:         fmt.Sprintf("🎉👑 **Vô địch giải đấu**: %s !%s", nameTag(champion), betSummary),
		AllowedMentions: &discordgo.MessageAllowedMentions{},
	}); err != nil {
		log.Error().Err(err).Msg("duel-challenge-all: failed to post champion")
	}
}

func (cmd *DuelChallengeAllCommand) signupContent(pt *pendingTournament) string {
	names := make([]string, len(pt.JoinOrder))
	for idx, id := range pt.JoinOrder {
		names[idx] = nameTag(id)
	}
	joined := "*chưa có ai*"
	if len(names) > 0 {
		joined = strings.Join(names, ", ")
	}
	return fmt.Sprintf(
		"🏆 <@%s> lên lịch **giải đấu Duel**, bắt đầu lúc **%s**.\nBấm \"Tham gia\" để vào giải, \"Rời đi\" nếu đổi ý. Đăng ký đóng 30 phút trước giờ đấu.\n\n**Đã tham gia (%d):** %s",
		pt.StarterID, pt.StartAt.Format(startTimeLayout), len(names), joined,
	)
}

func (cmd *DuelChallengeAllCommand) bettingPhaseContent(pt *pendingTournament, bets []championBet, remaining time.Duration) string {
	content := fmt.Sprintf(
		"🏆 Đăng ký đã đóng — **%d người** tham gia giải đấu, bắt đầu lúc **%s**.\n💰 Chọn nhà vô địch bạn dự đoán bên dưới để cược.\n⏳ Còn **%s** để cược.",
		len(pt.JoinOrder), pt.StartAt.Format(startTimeLayout), formatRemaining(remaining),
	)
	if len(bets) == 0 {
		return content
	}
	var lines strings.Builder
	fmt.Fprintf(&lines, "\n\n📊 **Đã cược (%d người):**\n", len(bets))
	for _, b := range bets {
		label := pt.Labels[b.PickedID]
		if label == "" {
			label = nameTag(b.PickedID)
		}
		fmt.Fprintf(&lines, "• <@%s> cược **%d điểm** cho %s\n", b.BettorID, b.Amount, label)
	}
	return content + lines.String()
}

func signupButtons(tournamentID string, disabled bool) []discordgo.MessageComponent {
	return []discordgo.MessageComponent{
		discordgo.ActionsRow{
			Components: []discordgo.MessageComponent{
				discordgo.Button{
					Label:    "Tham gia",
					Style:    discordgo.SuccessButton,
					CustomID: tournamentJoinID + tournamentID,
					Disabled: disabled,
				},
				discordgo.Button{
					Label:    "Rời đi",
					Style:    discordgo.SecondaryButton,
					CustomID: tournamentLeaveID + tournamentID,
					Disabled: disabled,
				},
				discordgo.Button{
					Label:    "Huỷ giải đấu",
					Style:    discordgo.DangerButton,
					CustomID: tournamentCancelID + tournamentID,
					Disabled: disabled,
				},
			},
		},
	}
}

func championSelectComponents(tournamentID string, joinOrder []string, labels map[string]string, disabled bool) []discordgo.MessageComponent {
	options := make([]discordgo.SelectMenuOption, 0, len(joinOrder))
	for _, id := range joinOrder {
		label := labels[id]
		if label == "" {
			label = id
		}
		options = append(options, discordgo.SelectMenuOption{
			Label: truncateLabel(label),
			Value: id,
		})
	}
	minValues := 1
	return []discordgo.MessageComponent{
		discordgo.ActionsRow{
			Components: []discordgo.MessageComponent{
				discordgo.SelectMenu{
					MenuType:    discordgo.StringSelectMenu,
					CustomID:    tournamentChampSelect + tournamentID,
					Placeholder: "Chọn nhà vô địch bạn dự đoán",
					Options:     options,
					Disabled:    disabled,
					MinValues:   &minValues,
					MaxValues:   1,
				},
			},
		},
	}
}
