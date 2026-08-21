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
	werewolfHunterTargetID  = "ww:huntertarget:"
	werewolfWitchHealID     = "ww:witchheal:"
	werewolfWitchPoisonID   = "ww:witchpoison:"
	werewolfWitchSkipID     = "ww:witchskip:"

	// nightActionWindow is the solo window for the Seer and Doctor, who each
	// act alone. Wolves get their own, longer window — see
	// wolfDiscussWindowFor — since they coordinate as a group in a shared
	// thread and benefit from more time as their numbers grow.
	nightActionWindow = 60 * time.Second

	// witchActionWindow is deliberately short — she's making one fast
	// decision after already knowing who was bitten, not coordinating with
	// anyone else the way the wolf pack does.
	witchActionWindow = 30 * time.Second

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
// filled. The Wolf-Cub bonus bite calls this with topN=2: an exact 2-way
// tie for the top slot (e.g. counts of 2,2,1) therefore still yields zero
// targets rather than selecting both tied players — deliberate, so a split
// pack never gets a free double kill out of its own indecision. ok is false
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

// aliveWolfVoters returns every alive player who currently votes in the
// wolf pack's nightly kill vote — Werewolf, Wolf Cub, and (once converted)
// Cursed Wolf, but never Traitor. Use this instead of
// alivePlayersOfRole(game, roleWerewolf) for wolf-thread membership,
// voting validation, and AFK-gating; alivePlayersOfRole itself is still
// correct for anything that genuinely means "exactly roleWerewolf" (there
// is no such use left in this file after this task).
func aliveWolfVoters(game *werewolfGame) []*player {
	var result []*player
	for _, id := range game.JoinOrder {
		p := game.Players[id]
		if p.Alive && p.wolfVoteParticipant() {
			result = append(result, p)
		}
	}
	return result
}

// aliveExcept returns every alive player in game except the one identified
// by excludeID (typically the acting player themself). Order follows
// game.JoinOrder, same as the other alive-roster helpers in this file.
func aliveExcept(game *werewolfGame, excludeID string) []*player {
	var result []*player
	for _, id := range game.JoinOrder {
		p := game.Players[id]
		if p.Alive && p.UserID != excludeID {
			result = append(result, p)
		}
	}
	return result
}

// applyHunterRevengeChain checks whether the player who just died (deadID)
// was the Hunter with a locked-in target; if so, kills that target too
// (unless already dead). Note: game.HunterTarget is a single game-scoped
// field, not per-Hunter, so if a second Hunter were ever dealt (not
// possible with today's ratio tables, which deal exactly one), this
// cannot produce a genuine multi-Hunter chain — it would resolve the same
// shared target twice and safely no-op the second time via the
// already-dead guard below, never mis-killing, but also never revenging
// more than once per invocation. Must be called with cmd.mu held. Returns
// value-copy snapshots of everyone killed, safe to read after unlocking
// — callers append these to their own kill lists for alive-thread removal,
// win-condition re-checks, and announcements.
func applyHunterRevengeChain(game *werewolfGame, deadID string) []*player {
	var revenged []*player
	queue := []string{deadID}
	for len(queue) > 0 {
		id := queue[0]
		queue = queue[1:]
		dead, ok := game.Players[id]
		if !ok || dead.Role != roleHunter {
			continue
		}
		targetID := game.HunterTarget
		if targetID == "" {
			continue
		}
		target, exists := game.Players[targetID]
		if !exists || !target.Alive {
			continue
		}
		target.Alive = false
		revenged = append(revenged, copyPlayer(target))
		queue = append(queue, targetID)
	}
	return revenged
}

// wolfBiteOutcome is what happens to one wolf-vote target at resolution
// time.
type wolfBiteOutcome int

const (
	wolfBiteSpare wolfBiteOutcome = iota
	wolfBiteKill
	wolfBiteConvert
)

// resolveWolfBiteOutcome decides a single target's fate: spared (Doctor
// protection or Witch heal covered them — no death, no conversion),
// killed (the ordinary case), or converted (a not-saved Cursed Wolf
// survives by flipping to wolf-faction instead of dying — spec §4c). Pure
// — callers apply the actual Alive/Faction mutation themselves.
func resolveWolfBiteOutcome(targetRole role, saved bool) wolfBiteOutcome {
	switch {
	case saved:
		return wolfBiteSpare
	case targetRole == roleCursedWolf:
		return wolfBiteConvert
	default:
		return wolfBiteKill
	}
}

// hunterRevengeLine renders the shared announcement line used wherever a
// Hunter's locked-in target dies alongside them — resolveVote, handleLeave,
// and announceHunterRevenge.
func hunterRevengeLine(p *player) string {
	return fmt.Sprintf("🏹 **%s** cũng chết theo vì Thợ Săn.", p.Label)
}

// applyWolfCubBonusBiteFlag arms game.WolfBonusBiteNextNight if any of the
// given just-died player IDs was a Wolf Cub. Additive — never clears an
// already-armed flag — so every death-application site (night kill/
// poison/Hunter-revenge, day lynch, AFK removal, voluntary leave) can call
// it independently after applying its own deaths, in any order, without
// needing to coordinate with the others. Must be called with cmd.mu held.
func applyWolfCubBonusBiteFlag(game *werewolfGame, deadIDs []string) {
	for _, id := range deadIDs {
		if p, ok := game.Players[id]; ok && p.Role == roleWolfCub {
			game.WolfBonusBiteNextNight = true
			return
		}
	}
}

// playersByIDs looks up each ID in game.Players, skipping any that don't
// exist (shouldn't happen — every ID here came from game.Players itself
// moments earlier — but this keeps the lookup total rather than panicking
// on a theoretical race). Must be called with cmd.mu held; the returned
// pointers are live and must be copyPlayer'd before escaping the lock.
func playersByIDs(game *werewolfGame, ids []string) []*player {
	var result []*player
	for _, id := range ids {
		if p, ok := game.Players[id]; ok {
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
		hunter         *player
		hunterTargets  []*player
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
		game.WitchSaveTarget = ""
		game.WitchPoisonTarget = ""
		game.WitchActedThisRound = false
		game.WitchPromptedThisRound = false
		game.NightBiteTargetIDs = nil
		game.HunterActedThisRound = false
		game.NightActionsClosed = false
		game.History = append(game.History, roundLog{Round: game.Round})

		round = game.Round
		summary = aliveDeadSummary(game)
		guildID = game.GuildID
		aliveThreadID = game.AliveThreadID
		wolves = copyPlayers(aliveWolfVoters(game))
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

		if hunters := alivePlayersOfRole(game, roleHunter); len(hunters) > 0 {
			hunter = copyPlayer(hunters[0])
			hunterTargets = copyPlayers(aliveExcept(game, hunter.UserID))
		}
	}()
	if !gameFound {
		return
	}
	muteAliveThread(s, guildID, aliveThreadID)

	if _, err := s.ChannelMessageSendComplex(channelID, &discordgo.MessageSend{
		Content:         fmt.Sprintf("🌙 **Đêm %d bắt đầu.** Ma Sói, Tiên Tri, Bác Sĩ, Thợ Săn đang hành động trong tin nhắn riêng...\n\n%s", round, summary),
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
	if hunter != nil {
		cmd.dmHunterPrompt(s, channelID, hunter, hunterTargets)
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

func (cmd *WerewolfCommand) dmHunterPrompt(s *discordgo.Session, channelID string, hunter *player, targets []*player) {
	dm, err := s.UserChannelCreate(hunter.UserID)
	if err != nil {
		log.Error().Err(err).Msg("werewolf: failed to open DM for hunter")
		return
	}
	if _, err := s.ChannelMessageSendComplex(dm.ID, &discordgo.MessageSend{
		Content:    "🏹 Chọn người bạn muốn chỉ định — nếu bạn chết (bất kỳ lý do gì), người này sẽ chết theo. Có thể đổi lại vào đêm sau nếu còn sống.",
		Components: targetSelectMenu(werewolfHunterTargetID+channelID, "Chỉ định mục tiêu", targets),
	}); err != nil {
		log.Error().Err(err).Msg("werewolf: failed to DM hunter prompt")
	}
}

// witchActionComponents builds the Witch's action message: an optional
// poison target-select row (omitted if she's already used it), and a heal
// control (omitted if there's no valid bite target, or she's already used
// it) plus an always-present skip button. The heal control is a plain
// confirm button when there's exactly one bite target (she can only be
// healing "the" bite target) and a target-select menu when there are two
// (the Wolf-Cub bonus bite) — she has to pick which one to save.
func witchActionComponents(channelID string, biteTargets []*player, healUsed, poisonUsed bool, poisonTargets []*player) []discordgo.MessageComponent {
	var rows []discordgo.MessageComponent
	if !poisonUsed {
		rows = append(rows, targetSelectMenu(werewolfWitchPoisonID+channelID, "☠️ Đầu độc ai đó (bỏ qua nếu không muốn)", poisonTargets)[0])
	}
	if len(biteTargets) > 1 && !healUsed {
		rows = append(rows, targetSelectMenu(werewolfWitchHealID+channelID, "💊 Cứu một trong hai người bị cắn", biteTargets)[0])
	}
	buttons := []discordgo.MessageComponent{}
	if len(biteTargets) == 1 && !healUsed {
		buttons = append(buttons, discordgo.Button{
			Label:    "💊 Cứu người bị cắn",
			Style:    discordgo.SuccessButton,
			CustomID: werewolfWitchHealID + channelID,
		})
	}
	buttons = append(buttons, discordgo.Button{
		Label:    "🚫 Không làm gì",
		Style:    discordgo.SecondaryButton,
		CustomID: werewolfWitchSkipID + channelID,
	})
	rows = append(rows, discordgo.ActionsRow{Components: buttons})
	return rows
}

func (cmd *WerewolfCommand) dmWitchPrompt(s *discordgo.Session, channelID string, witch *player, biteTargets []*player, healUsed, poisonUsed bool, poisonTargets []*player) {
	dm, err := s.UserChannelCreate(witch.UserID)
	if err != nil {
		log.Error().Err(err).Msg("werewolf: failed to open DM for witch")
		return
	}
	bittenText := "Không ai bị cắn đêm nay."
	if len(biteTargets) > 0 {
		labels := make([]string, len(biteTargets))
		for idx, p := range biteTargets {
			labels[idx] = p.Label
		}
		bittenText = fmt.Sprintf("Sói cắn: **%s** đêm nay.", strings.Join(labels, ", "))
	}
	if _, err := s.ChannelMessageSendComplex(dm.ID, &discordgo.MessageSend{
		Content:    fmt.Sprintf("🧙 %s Bạn muốn cứu, đầu độc ai đó, hay không làm gì?", bittenText),
		Components: witchActionComponents(channelID, biteTargets, healUsed, poisonUsed, poisonTargets),
	}); err != nil {
		log.Error().Err(err).Msg("werewolf: failed to DM witch prompt")
	}
}

func (cmd *WerewolfCommand) nightActionsComplete(game *werewolfGame) bool {
	wolves := aliveWolfVoters(game)
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

// waitForWitchAction blocks until the Witch has acted (heal/poison/skip) or
// the deadline passes. No halfway reminder — this window is short and she
// already has all the information she needs the moment she's prompted. Unlike
// waitForNightActions, this has no Discord-facing side effect (no reminder
// message to send), so it takes no *discordgo.Session.
func (cmd *WerewolfCommand) waitForWitchAction(channelID string, deadline time.Time, seq uint64) {
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
			done = game.WitchActedThisRound
		}()
		if !gameFound || done || time.Now().After(deadline) {
			return
		}
		time.Sleep(2 * time.Second)
	}
}

// resolveNight resolves the wolf vote and the AFK strikes for wolves/
// seer/doctor WITHOUT applying the wolf-bite death yet, deletes the wolf
// thread, then — if a Witch is alive — DMs her the bite outcome and waits
// for her decision before finalizing anything. finalizeNightResolution
// does the actual death application, Witch poison, Hunter revenge chain,
// recap, and result message, then hands off exactly like the old single-
// stage resolveNight used to.
func (cmd *WerewolfCommand) resolveNight(s *discordgo.Session, channelID string, seq uint64) {
	var (
		gameFound          bool
		threadID           string
		resolvedTargets    []string
		wolfTargetLabels   []string
		bonusBite          bool
		wolfVoteTied       bool
		aliveThreadID      string
		witch              *player
		witchPoisonTargets []*player
		biteTargetPlayers  []*player
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

		aliveWolves := aliveWolfVoters(game)
		validVotes := validWolfVotes(game.WolfVotes, aliveWolves, game.Players)
		topN := 1
		if game.WolfBonusBiteNextNight {
			topN = 2
		}
		game.WolfBonusBiteNextNight = false // consumed regardless of outcome — a tie still spends the bonus
		wolfTargets, tallyOK := tallyWolfVotes(validVotes, topN)
		// The bonus only actually *fires* when the tally really produced two
		// distinct targets. A unanimous pack with the bonus armed still bites
		// one player, and that round must stay on the ordinary single-target
		// message/recap path.
		bonusBite = len(wolfTargets) > 1
		wolfVoteTied = !tallyOK && len(validVotes) > 0
		resolvedTargets = wolfTargets
		for _, id := range resolvedTargets {
			if target, exists := game.Players[id]; exists {
				wolfTargetLabels = append(wolfTargetLabels, target.Label)
			}
		}
		doctorTargetLabel := ""
		if game.DoctorProtect != "" {
			if target, exists := game.Players[game.DoctorProtect]; exists {
				doctorTargetLabel = target.Label
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

		if witches := alivePlayersOfRole(game, roleWitch); len(witches) > 0 {
			witch = copyPlayer(witches[0])
			witchPoisonTargets = copyPlayers(aliveExcept(game, witch.UserID))
		}

		biteTargetPlayers = copyPlayers(playersByIDs(game, resolvedTargets))
		game.NightBiteTargetIDs = append([]string(nil), resolvedTargets...)

		if n := len(game.History); n > 0 {
			entry := &game.History[n-1]
			entry.WolfTargetLabel = strings.Join(wolfTargetLabels, ", ")
			entry.WolfBonusBite = bonusBite
			entry.WolfVoteTied = wolfVoteTied
			entry.DoctorTargetLabel = doctorTargetLabel
			if game.SeerTarget != "" {
				if target, exists := game.Players[game.SeerTarget]; exists {
					entry.SeerTargetLabel = target.Label
				}
				entry.SeerResult = game.SeerResult
			}
		}

		game.NightActionsClosed = true
	}()
	if !gameFound {
		return
	}

	if threadID != "" {
		if _, err := s.ChannelDelete(threadID); err != nil {
			log.Error().Err(err).Msg("werewolf: failed to delete wolf thread")
		}
	}

	if witch != nil {
		var healUsed, poisonUsed, hasChoice bool
		func() {
			cmd.mu.Lock()
			defer cmd.mu.Unlock()
			g := cmd.gameIfCurrent(channelID, seq)
			if g == nil {
				return
			}
			healUsed = g.WitchHealUsed
			poisonUsed = g.WitchPoisonUsed
			// hasChoice mirrors witchActionComponents exactly: it's true iff
			// at least one of {heal button, poison row} would actually
			// render — poison row renders whenever the potion isn't used
			// yet, and the heal button only when there's a bite target to
			// heal and the potion isn't used yet. Otherwise she has nothing
			// clickable but "skip", so don't bother prompting/waiting.
			hasChoice = !poisonUsed || (!healUsed && len(resolvedTargets) > 0)
			if hasChoice {
				g.WitchPromptedThisRound = true
			}
		}()
		if hasChoice {
			cmd.dmWitchPrompt(s, channelID, witch, biteTargetPlayers, healUsed, poisonUsed, witchPoisonTargets)
			deadline := time.Now().Add(witchActionWindow)
			cmd.waitForWitchAction(channelID, deadline, seq)
		}
	}

	cmd.finalizeNightResolution(s, channelID, seq, resolvedTargets, bonusBite, wolfVoteTied, aliveThreadID)
}

// finalizeNightResolution applies the wolf-bite death (accounting for
// Doctor protection and Witch's heal), applies Witch's poison if she chose
// one, chains Hunter's revenge off of either death, builds the recap
// fields and the public result message, then hands off to the win-check
// and day-discuss exactly like the old single-stage resolveNight used to.
func (cmd *WerewolfCommand) finalizeNightResolution(s *discordgo.Session, channelID string, seq uint64, resolvedTargets []string, bonusBite, wolfVoteTied bool, aliveThreadID string) {
	var (
		gameFound           bool
		revealOnDeath       bool
		killedPlayers       []*player
		poisonedPlayer      *player
		doctorSaved         bool
		witchHealed         bool
		cursedWolfConverted string
		biteOutcomeLines    []string
		hunterRevenged      []*player
		summary             string
	)
	func() {
		cmd.mu.Lock()
		defer cmd.mu.Unlock()
		game := cmd.gameIfCurrent(channelID, seq)
		if game == nil {
			return
		}
		gameFound = true

		// Close the Witch's action window right now, before reading any of
		// her round-scoped picks below. A click that lands after the
		// deadline but before this lock is acquired (waitForWitchAction
		// polls on a 2s sleep, so up to ~2s of slack) would otherwise still
		// be accepted by the handlers, burning a lifetime potion with the
		// round-scoped target then discarded by next round's reset and zero
		// visible effect. The handlers already reject on
		// !WitchPromptedThisRound with "Tin nhắn này đã hết hạn.", so this
		// closes the window completely.
		game.WitchPromptedThisRound = false

		// witchHealTarget resolves the two heal encodings into one target ID:
		// the "heal" sentinel (single-target button path) means "the lone
		// bite target", while any other non-empty value is an explicit ID
		// picked from the two-target select menu.
		var witchHealTarget string
		switch {
		case game.WitchSaveTarget == "heal" && len(resolvedTargets) > 0:
			witchHealTarget = resolvedTargets[0]
		case game.WitchSaveTarget != "" && game.WitchSaveTarget != "heal":
			witchHealTarget = game.WitchSaveTarget
		}
		// doctorSaved/witchHealed keep the exact meaning they had in the
		// single-target implementation (computed from the picks alone, not
		// from whether the bite actually resolved to a spare) so the
		// non-bonus-bite result-message cascade below is unchanged.
		for _, id := range resolvedTargets {
			if id == game.DoctorProtect {
				doctorSaved = true
			}
		}
		witchHealed = len(resolvedTargets) > 0 && game.WitchSaveTarget == "heal"

		var killedIDs []string
		for _, targetID := range resolvedTargets {
			target, exists := game.Players[targetID]
			if !exists || !target.Alive {
				continue
			}
			doctorSavedThis := targetID == game.DoctorProtect
			witchHealedThis := witchHealTarget != "" && targetID == witchHealTarget
			switch resolveWolfBiteOutcome(target.Role, doctorSavedThis || witchHealedThis) {
			case wolfBiteKill:
				target.Alive = false
				killedIDs = append(killedIDs, targetID)
				biteOutcomeLines = append(biteOutcomeLines, fmt.Sprintf("%s: chết", target.Label))
			case wolfBiteConvert:
				target.Faction = factionWolf
				cursedWolfConverted = target.Label
				biteOutcomeLines = append(biteOutcomeLines, fmt.Sprintf("%s: biến thành Sói Nguyền, không chết", target.Label))
			case wolfBiteSpare:
				reason := "Bác Sĩ cứu"
				if witchHealedThis && !doctorSavedThis {
					reason = "Phù Thủy cứu"
				}
				biteOutcomeLines = append(biteOutcomeLines, fmt.Sprintf("%s: được %s", target.Label, reason))
			}
		}

		var poisonedID string
		if game.WitchPoisonTarget != "" {
			// Known, accepted edge case: the Witch can poison the same player
			// the wolves bit (heal/poison are only mutually exclusive with
			// each other for OTHER targets via WitchActedThisRound). If that
			// player already died via the wolf-kill path above, target.Alive
			// is false here and this block silently no-ops — no double-kill,
			// no separate poison recap line — even though WitchPoisonUsed was
			// already set true when she chose the target. The potion is
			// simply burned with no visible effect; this is intentional.
			if target, exists := game.Players[game.WitchPoisonTarget]; exists && target.Alive {
				target.Alive = false
				poisonedID = game.WitchPoisonTarget
			}
		}

		for _, id := range killedIDs {
			hunterRevenged = append(hunterRevenged, applyHunterRevengeChain(game, id)...)
		}
		if poisonedID != "" {
			hunterRevenged = append(hunterRevenged, applyHunterRevengeChain(game, poisonedID)...)
		}

		deadThisResolution := append([]string(nil), killedIDs...)
		if poisonedID != "" {
			deadThisResolution = append(deadThisResolution, poisonedID)
		}
		deadThisResolution = append(deadThisResolution, idsOf(hunterRevenged)...)
		applyWolfCubBonusBiteFlag(game, deadThisResolution)

		revealOnDeath = game.RevealOnDeath
		summary = aliveDeadSummary(game)
		killedPlayers = copyPlayers(playersByIDs(game, killedIDs))
		if poisonedID != "" {
			poisonedPlayer = copyPlayer(game.Players[poisonedID])
		}

		if n := len(game.History); n > 0 {
			entry := &game.History[n-1]
			entry.WolfTargetSaved = doctorSaved
			if witchHealTarget != "" {
				if target, exists := game.Players[witchHealTarget]; exists {
					entry.WitchSavedLabel = target.Label
				}
			}
			if poisonedPlayer != nil {
				entry.WitchPoisonedLabel = poisonedPlayer.Label
			}
			entry.CursedWolfConverted = cursedWolfConverted
			if len(hunterRevenged) > 0 {
				entry.HunterRevengeLabel = hunterRevenged[0].Label
			}
			if bonusBite {
				entry.WolfBiteOutcomeLines = biteOutcomeLines
			}
		}
	}()
	if !gameFound {
		return
	}
	for _, p := range killedPlayers {
		removeFromAliveThread(s, aliveThreadID, p.UserID)
	}
	if poisonedPlayer != nil {
		removeFromAliveThread(s, aliveThreadID, poisonedPlayer.UserID)
	}
	for _, p := range hunterRevenged {
		removeFromAliveThread(s, aliveThreadID, p.UserID)
	}

	var resultMsg string
	switch {
	case bonusBite:
		// Two-target round: the single-target cascade below can't represent
		// "one target died, the other was saved" — list each target's own
		// outcome instead. Only reachable once a Wolf Cub has died.
		resultMsg = "🌙 **Kết quả đêm qua:**\n" + strings.Join(biteOutcomeLines, "\n")
	case len(killedPlayers) == 1 && revealOnDeath:
		resultMsg = fmt.Sprintf("☠️ **%s** đã chết trong đêm qua. Vai trò: %s.", killedPlayers[0].Label, killedPlayers[0].Role.label())
	case len(killedPlayers) == 1:
		resultMsg = fmt.Sprintf("☠️ **%s** đã chết trong đêm qua.", killedPlayers[0].Label)
	case doctorSaved && poisonedPlayer == nil:
		resultMsg = "🛡️ Bác Sĩ đã cứu sống mục tiêu đêm qua — không ai chết!"
	case doctorSaved:
		resultMsg = "🛡️ Bác Sĩ đã cứu sống mục tiêu bị Sói cắn đêm qua."
	case witchHealed && poisonedPlayer == nil:
		resultMsg = "🧙 Phù Thủy đã cứu sống mục tiêu đêm qua — không ai chết!"
	case witchHealed:
		resultMsg = "🧙 Phù Thủy đã cứu sống mục tiêu bị Sói cắn đêm qua."
	case wolfVoteTied && poisonedPlayer == nil:
		resultMsg = "🐺 Bầy Sói không thống nhất được mục tiêu — không ai chết trong đêm qua."
	case wolfVoteTied:
		resultMsg = "🐺 Bầy Sói không thống nhất được mục tiêu."
	case poisonedPlayer == nil:
		resultMsg = "😴 Không ai chết trong đêm qua."
	default:
		resultMsg = "😴 Không ai chết vì Sói cắn đêm qua."
	}
	if poisonedPlayer != nil {
		if revealOnDeath {
			resultMsg += fmt.Sprintf("\n☠️ **%s** trúng độc của Phù Thủy. Vai trò: %s.", poisonedPlayer.Label, poisonedPlayer.Role.label())
		} else {
			resultMsg += fmt.Sprintf("\n☠️ **%s** trúng độc của Phù Thủy.", poisonedPlayer.Label)
		}
	}
	for _, p := range hunterRevenged {
		resultMsg += "\n" + hunterRevengeLine(p)
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
	if !ok || game.Phase != phaseNight || game.NightActionsClosed {
		cmd.mu.Unlock()
		respondComponentEphemeral(s, i, "Đêm nay đã kết thúc rồi.")
		return
	}
	clicker, isWolf := game.Players[clickerID]
	if !isWolf || !clicker.wolfVoteParticipant() || !clicker.Alive {
		cmd.mu.Unlock()
		respondComponentEphemeral(s, i, "Chỉ thành viên phe Sói còn sống mới được chọn mục tiêu.")
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
	allVoted := true
	for _, w := range aliveWolfVoters(game) {
		if _, voted := game.WolfVotes[w.UserID]; !voted {
			allVoted = false
			break
		}
	}
	seq := game.Seq
	cmd.mu.Unlock()

	respondComponentEphemeralMentioned(s, i, fmt.Sprintf("✅ Bạn đã chọn: <@%s>. Phiếu đã khóa, không thể đổi.", targetID))
	if allVoted {
		announceRoleDecided(s, channelID, "🐺 Bầy Sói")
	}

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
	wolves = copyPlayers(aliveWolfVoters(game))
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
	if !ok || game.Phase != phaseNight || game.NightActionsClosed {
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
	announceRoleDecided(s, channelID, "🔮 Tiên Tri")
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
	if !ok || game.Phase != phaseNight || game.NightActionsClosed {
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
	announceRoleDecided(s, channelID, "🩺 Bác Sĩ")
}

func (cmd *WerewolfCommand) handleHunterTarget(s *discordgo.Session, i *discordgo.InteractionCreate, channelID string) {
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
	clicker, isHunter := game.Players[clickerID]
	if !isHunter || clicker.Role != roleHunter || !clicker.Alive {
		cmd.mu.Unlock()
		respondComponentEphemeral(s, i, "Chỉ Thợ Săn còn sống mới được chỉ định.")
		return
	}
	if target, exists := game.Players[targetID]; !exists || !target.Alive {
		cmd.mu.Unlock()
		respondComponentEphemeral(s, i, "Mục tiêu không còn hợp lệ.")
		return
	}
	firstActionThisRound := !game.HunterActedThisRound
	game.HunterTarget = targetID
	game.HunterActedThisRound = true
	cmd.mu.Unlock()

	respondComponentEphemeralMentioned(s, i, fmt.Sprintf("✅ Đã chỉ định: <@%s>.", targetID))
	if firstActionThisRound {
		announceRoleDecided(s, channelID, "🏹 Thợ Săn")
	}
}

func (cmd *WerewolfCommand) handleWitchHeal(s *discordgo.Session, i *discordgo.InteractionCreate, channelID string) {
	clickerID := interactionUserID(i)

	cmd.mu.Lock()
	game, ok := cmd.games[channelID]
	if !ok || game.Phase != phaseNight {
		cmd.mu.Unlock()
		respondComponentEphemeral(s, i, "Đêm nay đã kết thúc rồi.")
		return
	}
	clicker, isWitch := game.Players[clickerID]
	if !isWitch || clicker.Role != roleWitch || !clicker.Alive {
		cmd.mu.Unlock()
		respondComponentEphemeral(s, i, "Chỉ Phù Thủy còn sống mới được cứu.")
		return
	}
	if !game.WitchPromptedThisRound {
		cmd.mu.Unlock()
		respondComponentEphemeral(s, i, "Tin nhắn này đã hết hạn.")
		return
	}
	if game.WitchActedThisRound {
		cmd.mu.Unlock()
		respondComponentEphemeral(s, i, "Bạn đã hành động đêm nay rồi.")
		return
	}
	if game.WitchHealUsed {
		cmd.mu.Unlock()
		respondComponentEphemeral(s, i, "Bạn đã dùng bình cứu rồi.")
		return
	}
	if values := i.MessageComponentData().Values; len(values) > 0 {
		// Two-target select-menu path (Wolf-Cub bonus bite): the option list
		// was built from game.NightBiteTargetIDs, but re-validate against it
		// directly rather than trusting the client payload.
		targetID := values[0]
		valid := false
		for _, id := range game.NightBiteTargetIDs {
			if id == targetID {
				valid = true
				break
			}
		}
		if !valid {
			cmd.mu.Unlock()
			respondComponentEphemeral(s, i, "Mục tiêu không còn hợp lệ.")
			return
		}
		game.WitchSaveTarget = targetID
	} else {
		// Single-target button path: sentinel resolved against the lone bite
		// target at finalize time.
		game.WitchSaveTarget = "heal"
	}
	game.WitchHealUsed = true
	game.WitchActedThisRound = true
	cmd.mu.Unlock()

	respondComponentEphemeral(s, i, "✅ Bạn đã chọn cứu người bị cắn đêm nay.")
	announceRoleDecided(s, channelID, "🧙 Phù Thủy")
}

func (cmd *WerewolfCommand) handleWitchPoison(s *discordgo.Session, i *discordgo.InteractionCreate, channelID string) {
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
	clicker, isWitch := game.Players[clickerID]
	if !isWitch || clicker.Role != roleWitch || !clicker.Alive {
		cmd.mu.Unlock()
		respondComponentEphemeral(s, i, "Chỉ Phù Thủy còn sống mới được đầu độc.")
		return
	}
	if !game.WitchPromptedThisRound {
		cmd.mu.Unlock()
		respondComponentEphemeral(s, i, "Tin nhắn này đã hết hạn.")
		return
	}
	if game.WitchActedThisRound {
		cmd.mu.Unlock()
		respondComponentEphemeral(s, i, "Bạn đã hành động đêm nay rồi.")
		return
	}
	if game.WitchPoisonUsed {
		cmd.mu.Unlock()
		respondComponentEphemeral(s, i, "Bạn đã dùng bình độc rồi.")
		return
	}
	if target, exists := game.Players[targetID]; !exists || !target.Alive {
		cmd.mu.Unlock()
		respondComponentEphemeral(s, i, "Mục tiêu không còn hợp lệ.")
		return
	}
	game.WitchPoisonTarget = targetID
	game.WitchPoisonUsed = true
	game.WitchActedThisRound = true
	cmd.mu.Unlock()

	respondComponentEphemeralMentioned(s, i, fmt.Sprintf("☠️ Bạn đã đầu độc: <@%s>.", targetID))
	announceRoleDecided(s, channelID, "🧙 Phù Thủy")
}

func (cmd *WerewolfCommand) handleWitchSkip(s *discordgo.Session, i *discordgo.InteractionCreate, channelID string) {
	clickerID := interactionUserID(i)

	cmd.mu.Lock()
	game, ok := cmd.games[channelID]
	if !ok || game.Phase != phaseNight {
		cmd.mu.Unlock()
		respondComponentEphemeral(s, i, "Đêm nay đã kết thúc rồi.")
		return
	}
	clicker, isWitch := game.Players[clickerID]
	if !isWitch || clicker.Role != roleWitch || !clicker.Alive {
		cmd.mu.Unlock()
		respondComponentEphemeral(s, i, "Chỉ Phù Thủy còn sống mới được chọn.")
		return
	}
	if !game.WitchPromptedThisRound {
		cmd.mu.Unlock()
		respondComponentEphemeral(s, i, "Tin nhắn này đã hết hạn.")
		return
	}
	if game.WitchActedThisRound {
		cmd.mu.Unlock()
		respondComponentEphemeral(s, i, "Bạn đã hành động đêm nay rồi.")
		return
	}
	game.WitchActedThisRound = true
	cmd.mu.Unlock()

	respondComponentEphemeral(s, i, "✅ Bạn chọn không làm gì đêm nay.")
	announceRoleDecided(s, channelID, "🧙 Phù Thủy")
}
