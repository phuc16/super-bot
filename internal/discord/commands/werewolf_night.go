package commands

import (
	"fmt"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/rs/zerolog/log"
)

const (
	werewolfWolfTargetID    = "ww:wolftarget:"
	werewolfSeerCheckID     = "ww:seercheck:"
	werewolfDoctorProtectID = "ww:doctorprotect:"

	// nightActionWindow is the solo window for the Seer and Doctor, who each
	// act alone. Wolves get their own, longer window — see
	// wolfDiscussWindowFor — since they coordinate as a group in a shared
	// thread and benefit from more time as their numbers grow.
	nightActionWindow = 60 * time.Second

	wolfDiscussBase    = 60 * time.Second
	wolfDiscussPerWolf = 20 * time.Second
)

// wolfDiscussWindowFor returns the night-discussion window for the wolves'
// private thread, given how many wolves are alive this round.
func wolfDiscussWindowFor(wolfCount int) time.Duration {
	return wolfDiscussBase + wolfDiscussPerWolf*time.Duration(wolfCount)
}

// tallyWolfVotes resolves the wolf pack's plurality vote into up to topN
// kill targets. Targets are picked one slot at a time, highest remaining
// vote count first. The moment any slot's fill produces a tie among the
// remaining candidates, that slot is dropped and no further slots are
// filled — this plan only ever calls this with topN=1, so this
// simplification is untested at topN>1; a topN=2 consumer (e.g. a future
// "pack gets 2 kills" mechanic) should re-examine whether an exact 2-way
// tie for the top 2 slots combined (e.g. counts of 2,2,1) should still
// select both tied targets rather than producing zero kills. ok is false
// only when there were no votes at all, or the very first slot ties.
func tallyWolfVotes(votes map[string]string, topN int) (targets []string, ok bool) {
	counts := make(map[string]int)
	for _, target := range votes {
		counts[target]++
	}
	if len(counts) == 0 {
		return nil, false
	}

	remaining := make(map[string]int, len(counts))
	for target, c := range counts {
		remaining[target] = c
	}

	for len(targets) < topN && len(remaining) > 0 {
		maxCount := 0
		for _, c := range remaining {
			if c > maxCount {
				maxCount = c
			}
		}
		var tied []string
		for target, c := range remaining {
			if c == maxCount {
				tied = append(tied, target)
			}
		}
		if len(tied) > 1 {
			// Tie for this slot: it's dropped, and no lower-count target
			// can un-tie it, so stop entirely rather than continuing to a
			// slot that would out-rank a tied target.
			break
		}
		targets = append(targets, tied[0])
		delete(remaining, tied[0])
	}

	return targets, len(targets) > 0
}

// validWolfVotes filters raw votes down to the ones that should actually
// count toward the kill tally: cast by a still-alive wolf, for a
// still-alive target. A departed/lynched wolf's vote can no longer decide
// the kill, and a vote for a target who died some other way before
// resolution can't silently produce a no-kill with a misleading recap —
// both are dropped here rather than reaching tallyWolfVotes at all. Does
// NOT affect the AFK-strike check in resolveNight, which intentionally
// still reads the raw, unfiltered votes map — a wolf who voted for a
// since-dead target still acted and should not be struck.
func validWolfVotes(votes map[string]string, aliveWolves []*player, allPlayers map[string]*player) map[string]string {
	aliveWolfIDs := make(map[string]bool, len(aliveWolves))
	for _, w := range aliveWolves {
		aliveWolfIDs[w.UserID] = true
	}
	valid := make(map[string]string, len(votes))
	for voterID, targetID := range votes {
		if !aliveWolfIDs[voterID] {
			continue
		}
		if target, exists := allPlayers[targetID]; exists && target.Alive {
			valid[voterID] = targetID
		}
	}
	return valid
}

// wolfVoteSummary renders the live per-target vote count and per-wolf
// current pick, shown (and re-edited on every vote) inside the private wolf
// thread so the pack can coordinate openly with each other — nothing here
// is ever posted outside that thread.
func wolfVoteSummary(votes map[string]string, wolves, targets []*player) string {
	labelByID := make(map[string]string, len(targets))
	for _, t := range targets {
		labelByID[t.UserID] = t.Label
	}

	counts := make(map[string]int)
	for _, targetID := range votes {
		counts[targetID]++
	}

	var b strings.Builder
	b.WriteString("**Số phiếu:**")
	if len(counts) == 0 {
		b.WriteString(" chưa có phiếu nào.")
	} else {
		for _, t := range targets {
			if c := counts[t.UserID]; c > 0 {
				fmt.Fprintf(&b, "\n%s: %d phiếu", t.Label, c)
			}
		}
	}

	b.WriteString("\n\n**Lượt chọn:**")
	for _, w := range wolves {
		pick := "Chưa chọn"
		if targetID, voted := votes[w.UserID]; voted {
			if label, ok := labelByID[targetID]; ok {
				pick = label
			} else {
				pick = "(mục tiêu đã chết)"
			}
		}
		fmt.Fprintf(&b, "\n%s: %s", w.Label, pick)
	}

	return b.String()
}

// wolfVoteMessageContent renders the wolf-thread target-select message
// body shared by startWolfThread (initial post) and handleWolfTarget
// (live-edited on every new vote).
func wolfVoteMessageContent(pingText, summary string) string {
	return fmt.Sprintf(
		"🐺 %s Chọn mục tiêu để tấn công đêm nay. Mỗi Sói chỉ được bỏ phiếu 1 lần, hãy bàn bạc trong thread trước khi chọn.\n\n%s",
		pingText, summary,
	)
}

func alivePlayersOfRole(game *werewolfGame, r role) []*player {
	var result []*player
	for _, id := range game.JoinOrder {
		p := game.Players[id]
		if p.Alive && p.Role == r {
			result = append(result, p)
		}
	}
	return result
}

func aliveNonWolves(game *werewolfGame) []*player {
	var result []*player
	for _, id := range game.JoinOrder {
		p := game.Players[id]
		if p.Alive && !p.isWolf() {
			result = append(result, p)
		}
	}
	return result
}

func targetSelectMenu(customID, placeholder string, targets []*player) []discordgo.MessageComponent {
	options := make([]discordgo.SelectMenuOption, 0, len(targets))
	for _, p := range targets {
		options = append(options, discordgo.SelectMenuOption{
			Label: truncateLabel(p.Label),
			Value: p.UserID,
		})
	}
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

// copyPlayer returns a fresh *player holding a value copy of p, safe to read
// without cmd.mu held — unlike p itself, which handleLeave (and other
// handlers) can mutate concurrently at any time.
func copyPlayer(p *player) *player {
	cp := *p
	return &cp
}

func copyPlayers(ps []*player) []*player {
	out := make([]*player, len(ps))
	for idx, p := range ps {
		out[idx] = copyPlayer(p)
	}
	return out
}

func idsOf(ps []*player) []string {
	ids := make([]string, len(ps))
	for idx, p := range ps {
		ids[idx] = p.UserID
	}
	return ids
}

// runNightPhase resets this round's night state, opens the wolf thread and
// seer/doctor DMs, waits for actions (or the timer), then resolves the night.
func (cmd *WerewolfCommand) runNightPhase(s *discordgo.Session, channelID string, seq uint64) {
	var (
		gameFound      bool
		round          int
		summary        string
		guildID        string
		aliveThreadID  string
		wolves         []*player
		nonWolfTargets []*player
		seer           *player
		seerTargets    []*player
		doctor         *player
		doctorTargets  []*player
	)
	func() {
		cmd.mu.Lock()
		defer cmd.mu.Unlock()
		game := cmd.gameIfCurrent(channelID, seq)
		if game == nil {
			return
		}
		gameFound = true
		game.Phase = phaseNight
		game.Round++
		game.WolfVotes = make(map[string]string)
		game.SeerActed = false
		game.SeerTarget = ""
		game.SeerResult = roleVillager
		game.DoctorProtect = ""
		game.History = append(game.History, roundLog{Round: game.Round})

		round = game.Round
		summary = aliveDeadSummary(game)
		guildID = game.GuildID
		aliveThreadID = game.AliveThreadID
		wolves = copyPlayers(alivePlayersOfRole(game, roleWerewolf))
		nonWolfTargets = copyPlayers(aliveNonWolves(game))

		if seers := alivePlayersOfRole(game, roleSeer); len(seers) > 0 {
			seer = copyPlayer(seers[0])
			for _, id := range game.JoinOrder {
				p := game.Players[id]
				if p.Alive && p.UserID != seer.UserID {
					seerTargets = append(seerTargets, copyPlayer(p))
				}
			}
		}

		if doctors := alivePlayersOfRole(game, roleDoctor); len(doctors) > 0 {
			doctor = copyPlayer(doctors[0])
			lastPick := game.DoctorLastPick
			for _, id := range game.JoinOrder {
				p := game.Players[id]
				if p.Alive && p.UserID != lastPick {
					doctorTargets = append(doctorTargets, copyPlayer(p))
				}
			}
		}
	}()
	if !gameFound {
		return
	}
	muteAliveThread(s, guildID, aliveThreadID)

	if _, err := s.ChannelMessageSendComplex(channelID, &discordgo.MessageSend{
		Content:         fmt.Sprintf("🌙 **Đêm %d bắt đầu.** Ma Sói, Tiên Tri, Bác Sĩ đang hành động trong tin nhắn riêng...\n\n%s", round, summary),
		AllowedMentions: &discordgo.MessageAllowedMentions{},
	}); err != nil {
		log.Error().Err(err).Msg("werewolf: failed to post night announcement")
	}

	cmd.startWolfThread(s, channelID, round, wolves, nonWolfTargets, seq)
	if seer != nil {
		cmd.dmSeerPrompt(s, channelID, seer, seerTargets)
	}
	if doctor != nil {
		cmd.dmDoctorPrompt(s, channelID, doctor, doctorTargets)
	}

	totalWindow := nightActionWindow
	if w := wolfDiscussWindowFor(len(wolves)); len(wolves) > 0 && w > totalWindow {
		totalWindow = w
	}
	deadline := time.Now().Add(totalWindow)
	cmd.waitForNightActions(s, channelID, deadline, totalWindow, seq)

	cmd.resolveNight(s, channelID, seq)
}

// startWolfThread and the DM prompts below all take pre-snapshotted player
// data (copyPlayer'd under cmd.mu by the caller) rather than the live
// *werewolfGame — handleLeave can mutate live player/game state concurrently
// at any time during the night, so these must not read it unguarded.
func (cmd *WerewolfCommand) startWolfThread(s *discordgo.Session, channelID string, round int, wolves, targets []*player, seq uint64) {
	if len(wolves) == 0 {
		return
	}

	thread, err := s.ThreadStartComplex(channelID, &discordgo.ThreadStart{
		Name:                fmt.Sprintf("🐺 Sói bàn đêm %d", round),
		Type:                discordgo.ChannelTypeGuildPrivateThread,
		AutoArchiveDuration: 60,
		Invitable:           false,
	})
	if err != nil {
		log.Error().Err(err).Msg("werewolf: failed to create wolf thread, falling back to independent DMs")
		cmd.dmWolvesIndependently(s, channelID, wolves, targets)
		return
	}

	func() {
		cmd.mu.Lock()
		defer cmd.mu.Unlock()
		if g := cmd.gameIfCurrent(channelID, seq); g != nil {
			g.WolfThreadID = thread.ID
		}
	}()

	for _, w := range wolves {
		if err := s.ThreadMemberAdd(thread.ID, w.UserID); err != nil {
			log.Error().Err(err).Str("user_id", w.UserID).Msg("werewolf: failed to add wolf to thread")
		}
	}

	pingText, allowedMentions := pingOnce(idsOf(wolves))
	content := wolfVoteMessageContent(pingText, wolfVoteSummary(nil, wolves, targets))
	msg, err := s.ChannelMessageSendComplex(thread.ID, &discordgo.MessageSend{
		Content:         content,
		Components:      targetSelectMenu(werewolfWolfTargetID+channelID, "Chọn mục tiêu", targets),
		AllowedMentions: allowedMentions,
	})
	if err != nil {
		log.Error().Err(err).Msg("werewolf: failed to post wolf target select")
		return
	}

	func() {
		cmd.mu.Lock()
		defer cmd.mu.Unlock()
		if g := cmd.gameIfCurrent(channelID, seq); g != nil {
			g.WolfTargetMessageID = msg.ID
		}
	}()
}

// dmWolvesIndependently is the fallback when the wolf thread can't be
// created: each wolf gets the same target-picker in their own DM instead.
// Votes are locked once cast — handleWolfTarget rejects any further pick
// from a wolf who has already voted this round.
func (cmd *WerewolfCommand) dmWolvesIndependently(s *discordgo.Session, channelID string, wolves, targets []*player) {
	for _, w := range wolves {
		dm, err := s.UserChannelCreate(w.UserID)
		if err != nil {
			log.Error().Err(err).Str("user_id", w.UserID).Msg("werewolf: failed to open DM for wolf fallback")
			continue
		}
		if _, err := s.ChannelMessageSendComplex(dm.ID, &discordgo.MessageSend{
			Content:    "🐺 Chọn mục tiêu để tấn công đêm nay.",
			Components: targetSelectMenu(werewolfWolfTargetID+channelID, "Chọn mục tiêu", targets),
		}); err != nil {
			log.Error().Err(err).Str("user_id", w.UserID).Msg("werewolf: failed to DM wolf fallback prompt")
		}
	}
}

func (cmd *WerewolfCommand) dmSeerPrompt(s *discordgo.Session, channelID string, seer *player, targets []*player) {
	dm, err := s.UserChannelCreate(seer.UserID)
	if err != nil {
		log.Error().Err(err).Msg("werewolf: failed to open DM for seer")
		return
	}
	if _, err := s.ChannelMessageSendComplex(dm.ID, &discordgo.MessageSend{
		Content:    "🔮 Chọn một người để soi.",
		Components: targetSelectMenu(werewolfSeerCheckID+channelID, "Chọn người để soi", targets),
	}); err != nil {
		log.Error().Err(err).Msg("werewolf: failed to DM seer prompt")
	}
}

func (cmd *WerewolfCommand) dmDoctorPrompt(s *discordgo.Session, channelID string, doctor *player, targets []*player) {
	dm, err := s.UserChannelCreate(doctor.UserID)
	if err != nil {
		log.Error().Err(err).Msg("werewolf: failed to open DM for doctor")
		return
	}
	if _, err := s.ChannelMessageSendComplex(dm.ID, &discordgo.MessageSend{
		Content:    "💉 Chọn một người để bảo vệ đêm nay (không được trùng người đêm trước).",
		Components: targetSelectMenu(werewolfDoctorProtectID+channelID, "Chọn người để bảo vệ", targets),
	}); err != nil {
		log.Error().Err(err).Msg("werewolf: failed to DM doctor prompt")
	}
}

func (cmd *WerewolfCommand) nightActionsComplete(game *werewolfGame) bool {
	wolves := alivePlayersOfRole(game, roleWerewolf)
	if len(wolves) > 0 {
		for _, w := range wolves {
			if _, voted := game.WolfVotes[w.UserID]; !voted {
				return false
			}
		}
	}
	if len(alivePlayersOfRole(game, roleSeer)) > 0 && !game.SeerActed {
		return false
	}
	if len(alivePlayersOfRole(game, roleDoctor)) > 0 && game.DoctorProtect == "" {
		return false
	}
	return true
}

// waitForNightActions blocks until every required night-actor has acted or
// the deadline passes. Posts one public reminder at the halfway point if
// the night is still waiting on someone.
func (cmd *WerewolfCommand) waitForNightActions(s *discordgo.Session, channelID string, deadline time.Time, totalWindow time.Duration, seq uint64) {
	reminded := false
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
			done = cmd.nightActionsComplete(game)
		}()
		if !gameFound || done || time.Now().After(deadline) {
			return
		}

		remaining := time.Until(deadline)
		if !reminded && remaining <= totalWindow/2 {
			reminded = true
			if _, err := s.ChannelMessageSendComplex(channelID, &discordgo.MessageSend{
				Content:         fmt.Sprintf("⏳ Còn %s để mọi người hành động đêm nay.", formatRemaining(remaining)),
				AllowedMentions: &discordgo.MessageAllowedMentions{},
			}); err != nil {
				log.Error().Err(err).Msg("werewolf: failed to post night reminder")
			}
		}

		time.Sleep(2 * time.Second)
	}
}

// resolveNight applies the kill/save outcome, records AFK strikes for
// anyone who didn't act, deletes this round's wolf thread, posts the
// result, then hands off to the win-check and (if the game continues) the
// day-discuss phase.
func (cmd *WerewolfCommand) resolveNight(s *discordgo.Session, channelID string, seq uint64) {
	var (
		gameFound     bool
		threadID      string
		revealOnDeath bool
		killedPlayer  *player
		saved         bool
		wolfVoteTied  bool
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

		aliveWolves := alivePlayersOfRole(game, roleWerewolf)
		validVotes := validWolfVotes(game.WolfVotes, aliveWolves, game.Players)
		wolfTargets, tallyOK := tallyWolfVotes(validVotes, 1)
		wolfVoteTied = !tallyOK && len(validVotes) > 0
		wolfTargetLabel := ""
		var resolvedTarget string
		if len(wolfTargets) > 0 {
			resolvedTarget = wolfTargets[0]
			if target, exists := game.Players[resolvedTarget]; exists {
				wolfTargetLabel = target.Label
			}
		}
		doctorTargetLabel := ""
		if game.DoctorProtect != "" {
			if target, exists := game.Players[game.DoctorProtect]; exists {
				doctorTargetLabel = target.Label
			}
		}

		var killedID string
		if resolvedTarget != "" {
			if resolvedTarget == game.DoctorProtect {
				saved = true
			} else if target, exists := game.Players[resolvedTarget]; exists && target.Alive {
				target.Alive = false
				killedID = resolvedTarget
			}
		}

		for _, w := range aliveWolves {
			if _, voted := game.WolfVotes[w.UserID]; voted {
				game.AFKStrikes[w.UserID] = 0
			} else {
				game.AFKStrikes[w.UserID]++
			}
		}
		for _, p := range alivePlayersOfRole(game, roleSeer) {
			if game.SeerActed {
				game.AFKStrikes[p.UserID] = 0
			} else {
				game.AFKStrikes[p.UserID]++
			}
		}
		for _, p := range alivePlayersOfRole(game, roleDoctor) {
			if game.DoctorProtect != "" {
				game.AFKStrikes[p.UserID] = 0
			} else {
				game.AFKStrikes[p.UserID]++
			}
		}

		game.DoctorLastPick = game.DoctorProtect
		threadID = game.WolfThreadID
		game.WolfThreadID = ""
		game.WolfTargetMessageID = ""
		revealOnDeath = game.RevealOnDeath
		summary = aliveDeadSummary(game)
		if killedID != "" {
			killedPlayer = game.Players[killedID]
		}

		if n := len(game.History); n > 0 {
			entry := &game.History[n-1]
			entry.WolfTargetLabel = wolfTargetLabel
			entry.WolfTargetSaved = saved
			entry.WolfVoteTied = wolfVoteTied
			entry.DoctorTargetLabel = doctorTargetLabel
			if game.SeerTarget != "" {
				if target, exists := game.Players[game.SeerTarget]; exists {
					entry.SeerTargetLabel = target.Label
				}
				entry.SeerResult = game.SeerResult
			}
		}
	}()
	if !gameFound {
		return
	}
	if killedPlayer != nil {
		removeFromAliveThread(s, aliveThreadID, killedPlayer.UserID)
	}

	if threadID != "" {
		if _, err := s.ChannelDelete(threadID); err != nil {
			log.Error().Err(err).Msg("werewolf: failed to delete wolf thread")
		}
	}

	var resultMsg string
	switch {
	case killedPlayer != nil && revealOnDeath:
		resultMsg = fmt.Sprintf("☠️ **%s** đã chết trong đêm qua. Vai trò: %s.", killedPlayer.Label, killedPlayer.Role.label())
	case killedPlayer != nil:
		resultMsg = fmt.Sprintf("☠️ **%s** đã chết trong đêm qua.", killedPlayer.Label)
	case saved:
		resultMsg = "🛡️ Bác Sĩ đã cứu sống mục tiêu đêm qua — không ai chết!"
	case wolfVoteTied:
		resultMsg = "🐺 Bầy Sói không thống nhất được mục tiêu — không ai chết trong đêm qua."
	default:
		resultMsg = "😴 Không ai chết trong đêm qua."
	}

	if _, err := s.ChannelMessageSendComplex(channelID, &discordgo.MessageSend{
		Content:         resultMsg + "\n\n" + summary,
		AllowedMentions: &discordgo.MessageAllowedMentions{},
	}); err != nil {
		log.Error().Err(err).Msg("werewolf: failed to post night result")
	}

	if cmd.checkAndHandleGameEnd(s, channelID, seq) {
		return
	}
	cmd.runDayDiscuss(s, channelID, seq)
}

func (cmd *WerewolfCommand) handleWolfTarget(s *discordgo.Session, i *discordgo.InteractionCreate, channelID string) {
	values := i.MessageComponentData().Values
	if len(values) == 0 {
		return
	}
	targetID := values[0]
	clickerID := interactionUserID(i)

	cmd.mu.Lock()
	game, ok := cmd.games[channelID]
	if !ok || game.Phase != phaseNight {
		cmd.mu.Unlock()
		respondComponentEphemeral(s, i, "Đêm nay đã kết thúc rồi.")
		return
	}
	clicker, isWolf := game.Players[clickerID]
	if !isWolf || clicker.Role != roleWerewolf || !clicker.Alive {
		cmd.mu.Unlock()
		respondComponentEphemeral(s, i, "Chỉ Ma Sói còn sống mới được chọn mục tiêu.")
		return
	}
	if _, alreadyVoted := game.WolfVotes[clickerID]; alreadyVoted {
		cmd.mu.Unlock()
		respondComponentEphemeral(s, i, "Bạn đã bỏ phiếu rồi, không thể đổi.")
		return
	}
	if target, exists := game.Players[targetID]; !exists || !target.Alive {
		cmd.mu.Unlock()
		respondComponentEphemeral(s, i, "Mục tiêu không còn hợp lệ.")
		return
	}
	game.WolfVotes[clickerID] = targetID
	seq := game.Seq
	cmd.mu.Unlock()

	respondComponentEphemeralMentioned(s, i, fmt.Sprintf("✅ Bạn đã chọn: <@%s>. Phiếu đã khóa, không thể đổi.", targetID))

	// Re-lock and take a fresh snapshot of the full vote state rather than
	// reusing the one taken above — another wolf may have voted in the
	// meantime, and this closes the gap between "when I voted" and "what I
	// display." This does not eliminate the residual Discord-API
	// network-ordering race between two fully concurrent edits fired by
	// different goroutines (each still reads-then-edits without a shared
	// serialization point) — that risk is accepted here, consistent with
	// other pre-existing unlock/lock-ordering races elsewhere in this
	// codebase.
	var threadID, messageID string
	var wolves, targets []*player
	cmd.mu.Lock()
	game = cmd.gameIfCurrent(channelID, seq)
	if game == nil {
		cmd.mu.Unlock()
		return
	}
	threadID = game.WolfThreadID
	messageID = game.WolfTargetMessageID
	wolves = copyPlayers(alivePlayersOfRole(game, roleWerewolf))
	targets = copyPlayers(aliveNonWolves(game))
	votesCopy := make(map[string]string, len(game.WolfVotes))
	for k, v := range game.WolfVotes {
		votesCopy[k] = v
	}
	cmd.mu.Unlock()

	if threadID == "" || messageID == "" {
		return
	}
	summary := wolfVoteSummary(votesCopy, wolves, targets)
	pingText, allowedMentions := pingOnce(idsOf(wolves))
	content := wolfVoteMessageContent(pingText, summary)
	if _, err := s.ChannelMessageEditComplex(&discordgo.MessageEdit{
		Channel:         threadID,
		ID:              messageID,
		Content:         &content,
		AllowedMentions: allowedMentions,
	}); err != nil {
		log.Error().Err(err).Msg("werewolf: failed to update wolf vote summary")
	}
}

func (cmd *WerewolfCommand) handleSeerCheck(s *discordgo.Session, i *discordgo.InteractionCreate, channelID string) {
	values := i.MessageComponentData().Values
	if len(values) == 0 {
		return
	}
	targetID := values[0]
	clickerID := interactionUserID(i)

	cmd.mu.Lock()
	game, ok := cmd.games[channelID]
	if !ok || game.Phase != phaseNight {
		cmd.mu.Unlock()
		respondComponentEphemeral(s, i, "Đêm nay đã kết thúc rồi.")
		return
	}
	clicker, isSeer := game.Players[clickerID]
	if !isSeer || clicker.Role != roleSeer || !clicker.Alive {
		cmd.mu.Unlock()
		respondComponentEphemeral(s, i, "Chỉ Tiên Tri còn sống mới được soi.")
		return
	}
	if game.SeerActed {
		cmd.mu.Unlock()
		respondComponentEphemeral(s, i, "Bạn đã soi đêm nay rồi.")
		return
	}
	target, exists := game.Players[targetID]
	if !exists || !target.Alive {
		cmd.mu.Unlock()
		respondComponentEphemeral(s, i, "Mục tiêu không còn hợp lệ.")
		return
	}
	game.SeerActed = true
	game.SeerTarget = targetID
	game.SeerResult = target.Role
	isWolf := target.isWolf()
	cmd.mu.Unlock()

	verdict := "KHÔNG PHẢI Ma Sói"
	if isWolf {
		verdict = "LÀ MA SÓI"
	}
	respondComponentEphemeralMentioned(s, i, fmt.Sprintf("🔮 <@%s> %s.", targetID, verdict))
}

func (cmd *WerewolfCommand) handleDoctorProtect(s *discordgo.Session, i *discordgo.InteractionCreate, channelID string) {
	values := i.MessageComponentData().Values
	if len(values) == 0 {
		return
	}
	targetID := values[0]
	clickerID := interactionUserID(i)

	cmd.mu.Lock()
	game, ok := cmd.games[channelID]
	if !ok || game.Phase != phaseNight {
		cmd.mu.Unlock()
		respondComponentEphemeral(s, i, "Đêm nay đã kết thúc rồi.")
		return
	}
	clicker, isDoctor := game.Players[clickerID]
	if !isDoctor || clicker.Role != roleDoctor || !clicker.Alive {
		cmd.mu.Unlock()
		respondComponentEphemeral(s, i, "Chỉ Bác Sĩ còn sống mới được bảo vệ.")
		return
	}
	if game.DoctorProtect != "" {
		cmd.mu.Unlock()
		respondComponentEphemeral(s, i, "Bạn đã chọn người để bảo vệ đêm nay rồi.")
		return
	}
	if targetID == game.DoctorLastPick {
		cmd.mu.Unlock()
		respondComponentEphemeral(s, i, "Không thể bảo vệ cùng một người hai đêm liên tiếp.")
		return
	}
	if target, exists := game.Players[targetID]; !exists || !target.Alive {
		cmd.mu.Unlock()
		respondComponentEphemeral(s, i, "Mục tiêu không còn hợp lệ.")
		return
	}
	game.DoctorProtect = targetID
	cmd.mu.Unlock()

	respondComponentEphemeralMentioned(s, i, fmt.Sprintf("✅ Đã chọn bảo vệ: <@%s>.", targetID))
}
