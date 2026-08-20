package commands

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/rs/zerolog/log"
)

const (
	werewolfVoteID = "ww:vote:"
	dayVoteWindow  = 30 * time.Second

	dayDiscussBase     = 45 * time.Second
	dayDiscussPerAlive = 15 * time.Second
)

// dayDiscussWindowFor returns the day-discussion window for a given number
// of alive players. Text discussion takes longer to read/type than voice,
// and bigger games have more people to accuse/defend, so the window scales
// with the lobby size instead of staying flat.
func dayDiscussWindowFor(aliveCount int) time.Duration {
	return dayDiscussBase + dayDiscussPerAlive*time.Duration(aliveCount)
}

const (
	voteSkipValue = "__skip__"
	voteSkipLabel = "Không treo cổ ai"
)

// voteSelectMenu is targetSelectMenu's day-vote-specific counterpart: same
// shape, plus one extra "skip" option so players can deliberately abstain
// from lynching anyone instead of only reaching "nobody lynched" via a
// genuine tie.
func voteSelectMenu(customID, placeholder string, targets []*player) []discordgo.MessageComponent {
	options := make([]discordgo.SelectMenuOption, 0, len(targets)+1)
	for _, p := range targets {
		options = append(options, discordgo.SelectMenuOption{
			Label: truncateLabel(p.Label),
			Value: p.UserID,
		})
	}
	options = append(options, discordgo.SelectMenuOption{
		Label: voteSkipLabel,
		Value: voteSkipValue,
	})
	minValues := 1
	return []discordgo.MessageComponent{
		discordgo.ActionsRow{
			Components: []discordgo.MessageComponent{
				discordgo.SelectMenu{
					MenuType:    discordgo.StringSelectMenu,
					CustomID:    customID,
					Placeholder: placeholder,
					Options:     options,
					MinValues:   &minValues,
					MaxValues:   1,
				},
			},
		},
	}
}

func (cmd *WerewolfCommand) runDayDiscuss(s *discordgo.Session, channelID string, seq uint64) {
	var gameFound bool
	var round int
	var aliveCount int
	var summary string
	var guildID string
	var aliveThreadID string
	func() {
		cmd.mu.Lock()
		defer cmd.mu.Unlock()
		game := cmd.gameIfCurrent(channelID, seq)
		if game == nil {
			return
		}
		gameFound = true
		game.Phase = phaseDayDiscuss
		round = game.Round
		for _, p := range game.Players {
			if p.Alive {
				aliveCount++
			}
		}
		summary = aliveDeadSummary(game)
		guildID = game.GuildID
		aliveThreadID = game.AliveThreadID
	}()
	if !gameFound {
		return
	}
	unmuteAliveThread(s, guildID, aliveThreadID)

	discussWindow := dayDiscussWindowFor(aliveCount)

	content := fmt.Sprintf("☀️ **Ngày %d.** Thảo luận trong %s trước khi bỏ phiếu.\n\n%s", round, formatRemaining(discussWindow), summary)
	if aliveThreadID != "" {
		content += fmt.Sprintf("\n\n💬 Thảo luận tại <#%s>", aliveThreadID)
	}
	if _, err := s.ChannelMessageSendComplex(channelID, &discordgo.MessageSend{
		Content:         content,
		AllowedMentions: &discordgo.MessageAllowedMentions{},
	}); err != nil {
		log.Error().Err(err).Msg("werewolf: failed to post day-discuss announcement")
	}

	time.Sleep(discussWindow)

	stillGoing := func() bool {
		cmd.mu.Lock()
		defer cmd.mu.Unlock()
		return cmd.gameIfCurrent(channelID, seq) != nil
	}()
	if !stillGoing {
		return
	}

	cmd.runDayVote(s, channelID, seq)
}

func (cmd *WerewolfCommand) runDayVote(s *discordgo.Session, channelID string, seq uint64) {
	var gameFound bool
	var alive []*player
	var summary string
	var guildID string
	var aliveThreadID string
	func() {
		cmd.mu.Lock()
		defer cmd.mu.Unlock()
		game := cmd.gameIfCurrent(channelID, seq)
		if game == nil {
			return
		}
		gameFound = true
		game.Phase = phaseDayVote
		game.Votes = make(map[string]string)
		for _, id := range game.JoinOrder {
			if p := game.Players[id]; p.Alive {
				alive = append(alive, p)
			}
		}
		summary = aliveDeadSummary(game)
		guildID = game.GuildID
		aliveThreadID = game.AliveThreadID
	}()
	if !gameFound {
		return
	}
	muteAliveThread(s, guildID, aliveThreadID)

	if _, err := s.ChannelMessageSendComplex(channelID, &discordgo.MessageSend{
		Content:    fmt.Sprintf("🗳️ **Bỏ phiếu treo cổ.** Chọn người bạn nghi ngờ, hoặc bỏ qua.\n\n%s", summary),
		Components: voteSelectMenu(werewolfVoteID+channelID, "Chọn người để treo cổ", alive),
	}); err != nil {
		log.Error().Err(err).Msg("werewolf: failed to post vote select")
	}

	// Players are chatting in the alive-discussion thread, not watching the
	// (locked, bot-only) main channel — point them back to it now that
	// voting has actually started there.
	if aliveThreadID != "" {
		if _, err := s.ChannelMessageSendComplex(aliveThreadID, &discordgo.MessageSend{
			Content:         fmt.Sprintf("🗳️ Bắt đầu bỏ phiếu! Vào <#%s> để bỏ phiếu treo cổ.", channelID),
			AllowedMentions: &discordgo.MessageAllowedMentions{},
		}); err != nil {
			log.Error().Err(err).Msg("werewolf: failed to post vote-start pointer in alive thread")
		}
	}

	deadline := time.Now().Add(dayVoteWindow)
	cmd.waitForVotes(s, channelID, deadline, len(alive), seq)

	cmd.resolveVote(s, channelID, seq)
}

func (cmd *WerewolfCommand) waitForVotes(s *discordgo.Session, channelID string, deadline time.Time, aliveCount int, seq uint64) {
	for {
		var gameFound, done bool
		func() {
			cmd.mu.Lock()
			defer cmd.mu.Unlock()
			game := cmd.gameIfCurrent(channelID, seq)
			if game == nil {
				return
			}
			gameFound = true
			done = len(game.Votes) >= aliveCount
		}()
		if !gameFound || done || time.Now().After(deadline) {
			return
		}
		time.Sleep(2 * time.Second)
	}
}

func (cmd *WerewolfCommand) resolveVote(s *discordgo.Session, channelID string, seq uint64) {
	var (
		gameFound     bool
		lynchedPlayer *player
		revealOnDeath bool
		summary       string
		aliveThreadID string
	)
	func() {
		cmd.mu.Lock()
		defer cmd.mu.Unlock()
		game := cmd.gameIfCurrent(channelID, seq)
		if game == nil {
			return
		}
		gameFound = true
		aliveThreadID = game.AliveThreadID

		var alive []*player
		for _, id := range game.JoinOrder {
			if p := game.Players[id]; p.Alive {
				alive = append(alive, p)
			}
		}
		for _, p := range alive {
			if _, voted := game.Votes[p.UserID]; voted {
				game.AFKStrikes[p.UserID] = 0
			} else {
				game.AFKStrikes[p.UserID]++
			}
		}

		top, tie := tallyVotes(game.Votes)
		if !tie && len(top) == 1 {
			if target, exists := game.Players[top[0]]; exists && target.Alive {
				target.Alive = false
				lynchedPlayer = target
			}
		}
		revealOnDeath = game.RevealOnDeath
		summary = aliveDeadSummary(game)

		if n := len(game.History); n > 0 {
			entry := &game.History[n-1]
			entry.VoteBreakdown = make(map[string][]string)
			for voterID, targetID := range game.Votes {
				label := voteSkipLabel
				if targetID != voteSkipValue {
					if target, exists := game.Players[targetID]; exists {
						label = target.Label
					} else {
						label = "?"
					}
				}
				voterLabel := "?"
				if voter, exists := game.Players[voterID]; exists {
					voterLabel = voter.Label
				}
				entry.VoteBreakdown[label] = append(entry.VoteBreakdown[label], voterLabel)
			}
			if lynchedPlayer != nil {
				entry.LynchedLabel = lynchedPlayer.Label
			}
		}
	}()
	if !gameFound {
		return
	}
	if lynchedPlayer != nil {
		removeFromAliveThread(s, aliveThreadID, lynchedPlayer.UserID)
	}

	var resultMsg string
	switch {
	case lynchedPlayer != nil && revealOnDeath:
		resultMsg = fmt.Sprintf("⚖️ **%s** bị treo cổ. Vai trò: %s.", lynchedPlayer.Label, lynchedPlayer.Role.label())
	case lynchedPlayer != nil:
		resultMsg = fmt.Sprintf("⚖️ **%s** bị treo cổ.", lynchedPlayer.Label)
	default:
		resultMsg = "🤷 Không đủ số phiếu áp đảo, không ai bị treo cổ hôm nay."
	}

	if _, err := s.ChannelMessageSendComplex(channelID, &discordgo.MessageSend{
		Content:         resultMsg + "\n\n" + summary,
		AllowedMentions: &discordgo.MessageAllowedMentions{},
	}); err != nil {
		log.Error().Err(err).Msg("werewolf: failed to post vote result")
	}

	if cmd.checkAndHandleGameEnd(s, channelID, seq) {
		return
	}
	cmd.runNightPhase(s, channelID, seq)
}

// checkAndHandleGameEnd applies AFK-strike removals, then checks the win
// condition. If the game has ended, it announces the result, reveals every
// role, deletes any remaining wolf thread, deletes the game from the
// pending map, and returns true — callers must stop processing when true.
func (cmd *WerewolfCommand) checkAndHandleGameEnd(s *discordgo.Session, channelID string, seq uint64) bool {
	var (
		gameFound       bool
		ended           bool
		removedNames    []string
		removedIDs      []string
		threadID        string
		result          gameResult
		endedGame       *werewolfGame
		aliveThreadID   string
		guildID         string
		priorOverwrite  *discordgo.PermissionOverwrite
		channelUnlocked bool
	)
	func() {
		cmd.mu.Lock()
		defer cmd.mu.Unlock()
		game := cmd.gameIfCurrent(channelID, seq)
		if game == nil {
			return
		}
		gameFound = true
		aliveThreadID = game.AliveThreadID
		guildID = game.GuildID
		priorOverwrite = game.PriorOverwrite
		channelUnlocked = game.ChannelUnlocked

		for _, id := range game.JoinOrder {
			p := game.Players[id]
			if p.Alive && shouldRemoveForAFK(game.AFKStrikes[id]) {
				p.Alive = false
				removedNames = append(removedNames, p.Label)
				removedIDs = append(removedIDs, id)
			}
		}

		result = checkWinCondition(game.Players)
		if result == resultOngoing {
			return
		}

		ended = true
		threadID = game.WolfThreadID
		endedGame = game
		delete(cmd.games, channelID)
	}()
	if !gameFound {
		return true
	}
	if !ended {
		for _, id := range removedIDs {
			removeFromAliveThread(s, aliveThreadID, id)
		}
		if len(removedNames) > 0 {
			cmd.announceAFKRemovals(s, channelID, removedNames)
		}
		return false
	}

	if threadID != "" {
		if _, err := s.ChannelDelete(threadID); err != nil {
			log.Error().Err(err).Msg("werewolf: failed to delete wolf thread on game end")
		}
	}
	if aliveThreadID != "" {
		if _, err := s.ChannelDelete(aliveThreadID); err != nil {
			log.Error().Err(err).Msg("werewolf: failed to delete alive-discussion thread on game end")
		}
	}
	if !channelUnlocked {
		unlockChannel(s, guildID, channelID, priorOverwrite)
	}

	cmd.announceGameEnd(s, endedGame, result, removedNames)
	return true
}

func (cmd *WerewolfCommand) announceAFKRemovals(s *discordgo.Session, channelID string, names []string) {
	content := fmt.Sprintf("💤 %s bị loại vì AFK 2 lượt liên tiếp.", strings.Join(names, ", "))
	if _, err := s.ChannelMessageSendComplex(channelID, &discordgo.MessageSend{
		Content:         content,
		AllowedMentions: &discordgo.MessageAllowedMentions{},
	}); err != nil {
		log.Error().Err(err).Msg("werewolf: failed to announce AFK removals")
	}
}

func (cmd *WerewolfCommand) announceGameEnd(s *discordgo.Session, game *werewolfGame, result gameResult, afkRemoved []string) {
	winner := "🐺 **Ma Sói thắng!**"
	if result == resultVillagersWin {
		winner = "🏡 **Dân Làng thắng!**"
	}

	var roles strings.Builder
	roles.WriteString("\n\n**Vai trò của mọi người:**\n")
	for _, id := range game.JoinOrder {
		p := game.Players[id]
		status := "còn sống"
		if !p.Alive {
			status = "đã chết"
		}
		fmt.Fprintf(&roles, "• %s — %s (%s)\n", p.Label, p.Role.label(), status)
	}

	afkNote := ""
	if len(afkRemoved) > 0 {
		afkNote = fmt.Sprintf("\n💤 %s bị loại vì AFK trước khi ván kết thúc.", strings.Join(afkRemoved, ", "))
	}

	if _, err := s.ChannelMessageSendComplex(game.ChannelID, &discordgo.MessageSend{
		Content:         fmt.Sprintf("🎉 Ván Ma Sói kết thúc! %s%s%s", winner, afkNote, roles.String()),
		AllowedMentions: &discordgo.MessageAllowedMentions{},
	}); err != nil {
		log.Error().Err(err).Msg("werewolf: failed to announce game end")
	}

	for _, chunk := range chunkRecapMessages(game.History, 1900) {
		if _, err := s.ChannelMessageSendComplex(game.ChannelID, &discordgo.MessageSend{
			Content:         chunk,
			AllowedMentions: &discordgo.MessageAllowedMentions{},
		}); err != nil {
			log.Error().Err(err).Msg("werewolf: failed to post game recap")
		}
	}
}

// formatRoundBlock renders one round's recap section. Any field left at its
// zero value (a role that's already dead, or not in this game's role
// ratio) is omitted.
func formatRoundBlock(r roundLog) string {
	var b strings.Builder
	fmt.Fprintf(&b, "\n\n**Đêm/Ngày %d:**", r.Round)
	if r.WolfTargetLabel != "" {
		if r.WolfTargetSaved {
			fmt.Fprintf(&b, "\n🐺 Sói cắn: %s (được bác sĩ cứu)", r.WolfTargetLabel)
		} else {
			fmt.Fprintf(&b, "\n🐺 Sói cắn: %s", r.WolfTargetLabel)
		}
	}
	if r.DoctorTargetLabel != "" {
		fmt.Fprintf(&b, "\n💉 Bác sĩ bảo vệ: %s", r.DoctorTargetLabel)
	}
	if r.SeerTargetLabel != "" {
		fmt.Fprintf(&b, "\n🔮 Tiên tri xem: %s → %s", r.SeerTargetLabel, r.SeerResult.label())
	}
	if len(r.VoteBreakdown) > 0 {
		targets := make([]string, 0, len(r.VoteBreakdown))
		for target := range r.VoteBreakdown {
			targets = append(targets, target)
		}
		sort.Strings(targets)
		lines := make([]string, 0, len(targets))
		for _, target := range targets {
			voters := append([]string(nil), r.VoteBreakdown[target]...)
			sort.Strings(voters)
			lines = append(lines, fmt.Sprintf("%s: %d (%s)", target, len(voters), strings.Join(voters, ", ")))
		}
		lynched := "Không ai bị treo cổ"
		if r.LynchedLabel != "" {
			lynched = r.LynchedLabel + " bị treo cổ"
		}
		fmt.Fprintf(&b, "\n🗳️ Kết quả bỏ phiếu:\n%s\n→ %s", strings.Join(lines, "\n"), lynched)
	}
	return b.String()
}

// chunkRecapMessages splits the full round-by-round recap into one or more
// message bodies, each within limit characters, splitting only on whole
// round boundaries (never mid-round). Always returns at least one string
// when history is non-empty.
func chunkRecapMessages(history []roundLog, limit int) []string {
	if len(history) == 0 {
		return nil
	}
	const header = "📜 **Lịch sử ván đấu**"
	var chunks []string
	current := header
	for _, r := range history {
		block := formatRoundBlock(r)
		if len(current)+len(block) > limit && current != header {
			chunks = append(chunks, current)
			current = header
		}
		current += block
	}
	return append(chunks, current)
}

func (cmd *WerewolfCommand) handleVote(s *discordgo.Session, i *discordgo.InteractionCreate, channelID string) {
	values := i.MessageComponentData().Values
	if len(values) == 0 {
		return
	}
	targetID := values[0]
	voterID := interactionUserID(i)

	cmd.mu.Lock()
	game, ok := cmd.games[channelID]
	if !ok || game.Phase != phaseDayVote {
		cmd.mu.Unlock()
		respondComponentEphemeral(s, i, "Vòng bỏ phiếu đã kết thúc rồi.")
		return
	}
	voter, isPlayer := game.Players[voterID]
	if !isPlayer || !voter.Alive {
		cmd.mu.Unlock()
		respondComponentEphemeral(s, i, "Chỉ người chơi còn sống mới được bỏ phiếu.")
		return
	}
	if targetID != voteSkipValue {
		if target, exists := game.Players[targetID]; !exists || !target.Alive {
			cmd.mu.Unlock()
			respondComponentEphemeral(s, i, "Mục tiêu không còn hợp lệ.")
			return
		}
	}
	game.Votes[voterID] = targetID
	cmd.mu.Unlock()

	if targetID == voteSkipValue {
		respondComponentEphemeral(s, i, "✅ Đã bỏ phiếu: Không treo cổ ai.")
		return
	}
	respondComponentEphemeralMentioned(s, i, fmt.Sprintf("✅ Đã bỏ phiếu cho <@%s>.", targetID))
}
