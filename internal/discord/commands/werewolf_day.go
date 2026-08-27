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
	dayVoteWindow  = 60 * time.Second

	dayDiscussBase     = 90 * time.Second
	dayDiscussPerAlive = 18 * time.Second

	// dayDiscussMax caps the discussion window for the same reason
	// wolfDiscussMax caps the night one: it keeps the worst-case night+day
	// cycle bounded regardless of roster size.
	dayDiscussMax = 450 * time.Second
)

// dayDiscussWindowFor returns the day-discussion window for a given number
// of alive players. Text discussion takes longer to read/type than voice,
// and bigger games have more people to accuse/defend, so the window scales
// with the lobby size instead of staying flat.
func dayDiscussWindowFor(aliveCount int) time.Duration {
	window := dayDiscussBase + dayDiscussPerAlive*time.Duration(aliveCount)
	if window > dayDiscussMax {
		return dayDiscussMax
	}
	return window
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
	var aliveThreadID string
	var wolfThreadID string
	var mutedUserID string
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
		aliveThreadID = game.AliveThreadID
		wolfThreadID = game.WolfThreadID
		mutedUserID = game.DisruptorTarget
		// Clear unconditionally before this round's apply. runDayVote also
		// clears it after removing the overwrite, so in the normal flow
		// this is already "" — but resetting here too means a round whose
		// apply FAILS can never inherit the previous round's value and
		// have runDayVote try to remove an overwrite that isn't there.
		game.DisruptorMutedUserID = ""

		if target, exists := game.Players[mutedUserID]; mutedUserID != "" && exists && target.Alive {
			if n := len(game.History); n > 0 {
				game.History[n-1].DisruptorMutedLabel = target.Label
			}
		} else {
			mutedUserID = ""
		}
	}()
	if !gameFound {
		return
	}
	// The wolf thread now persists for the whole game (see startWolfThread's
	// doc comment) instead of being deleted every night, so it must be
	// explicitly locked here at the moment day begins — otherwise the pack
	// could keep silently coordinating in it all day/vote, undetectable
	// from the public alive-thread discussion. startWolfThread unlocks it
	// again (alongside unarchiving) the next time night opens.
	setWolfThreadLocked(s, wolfThreadID, true)
	setAliveThreadLocked(s, aliveThreadID, false)
	if mutedUserID != "" {
		// The overwrite goes on the PARENT channel, not on aliveThreadID.
		// Threads carry no overwrites of their own — a member's effective
		// SEND_MESSAGES_IN_THREADS inside a thread resolves from the parent
		// channel — so the previous thread-keyed version was rejected by the
		// API on every call and the Disruptor silenced nobody, ever.
		//
		// Denying both bits covers both discussion venues: IN_THREADS for
		// the normal alive-thread case, and SEND_MESSAGES for the fallback
		// where thread creation failed and the main channel was unlocked for
		// discussion instead. Scoping to the parent also silences the target
		// in the wolf thread, which is harmless — that thread is locked for
		// everyone throughout day-discuss anyway (just above).
		const disruptorDeny = discordgo.PermissionSendMessagesInThreads | discordgo.PermissionSendMessages
		if err := s.ChannelPermissionSet(channelID, mutedUserID, discordgo.PermissionOverwriteTypeMember, 0, disruptorDeny); err != nil {
			log.Error().Err(err).Str("user_id", mutedUserID).Msg("werewolf: failed to apply disruptor per-member mute")
		} else {
			// Record who actually got muted, so runDayVote removes exactly
			// this overwrite rather than re-deriving the target from
			// DisruptorTarget + current aliveness — that re-derivation
			// silently skipped the removal whenever the target died during
			// the discussion window, leaving the overwrite orphaned.
			// Recorded only on success: a failed apply has nothing to undo.
			func() {
				cmd.mu.Lock()
				defer cmd.mu.Unlock()
				if g := cmd.gameIfCurrent(channelID, seq); g != nil {
					g.DisruptorMutedUserID = mutedUserID
				}
			}()
		}
	}

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
	var aliveThreadID string
	var mutedUserID string
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
		game.DayVoteClosed = false
		for _, id := range game.JoinOrder {
			if p := game.Players[id]; p.Alive {
				alive = append(alive, p)
			}
		}
		summary = aliveDeadSummary(game)
		aliveThreadID = game.AliveThreadID
		// Read exactly who runDayDiscuss actually muted, rather than
		// re-deriving it from DisruptorTarget + current aliveness. Those
		// two gates are the same predicate at different instants, so a
		// target who died during the discussion window would have had the
		// overwrite applied and then skipped at removal, orphaning it on
		// the thread. Cleared here so it's never removed twice.
		mutedUserID = game.DisruptorMutedUserID
		game.DisruptorMutedUserID = ""
	}()
	if !gameFound {
		return
	}
	setAliveThreadLocked(s, aliveThreadID, true)
	if mutedUserID != "" {
		removeDisruptorMute(s, channelID, mutedUserID)
	}

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
	cmd.waitForVotes(s, channelID, deadline, seq)

	cmd.resolveVote(s, channelID, seq)
}

// waitForVotes blocks until every still-alive player has voted, or the
// deadline passes.
//
// The alive count is RECOMPUTED each tick rather than taken as a snapshot
// parameter. A player who leaves mid-vote without having voted would
// otherwise make the early-exit unreachable — the survivors can only ever
// produce (aliveAtStart - 1) entries — so the round would sit out the full
// window in dead air even though everyone still in the game had voted.
// Votes from players who have since died are also discounted here, for the
// same reason: they can't be part of "everyone still playing has voted".
// (validDayVotes independently drops those votes from the tally itself.)
func (cmd *WerewolfCommand) waitForVotes(s *discordgo.Session, channelID string, deadline time.Time, seq uint64) {
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

			aliveNow, votedAndAlive := 0, 0
			for id, p := range game.Players {
				if !p.Alive {
					continue
				}
				aliveNow++
				if _, voted := game.Votes[id]; voted {
					votedAndAlive++
				}
			}
			// aliveNow == 0 can't happen while the game is running, but
			// guard anyway so an empty roster ends the wait instead of
			// spinning to the deadline.
			done = aliveNow == 0 || votedAndAlive >= aliveNow
		}()
		if !gameFound || done || time.Now().After(deadline) {
			return
		}
		time.Sleep(2 * time.Second)
	}
}

// validDayVotes filters votes down to voters who are still alive at
// resolution time, AND drops any vote whose target has died/left before
// the window closed — mirrors validWolfVotes' rationale for the night wolf
// vote on both counts: a player who cast a day-vote and then left can no
// longer decide the lynch, and a vote for someone who died some other way
// before resolution can't silently cancel out an otherwise-decisive tally
// (the caller re-tallies over what's left, so this naturally falls through
// to the next-highest live candidate rather than voiding the lynch
// outright). The skip sentinel is never filtered as a "target" here since
// it isn't a player ID. Does NOT affect the AFK-strike check above (which
// reads the raw votes map over the currently-alive roster) or the
// VoteBreakdown recap (which intentionally shows every vote actually cast,
// for a transparent public record) — only the tally that decides who gets
// lynched.
func validDayVotes(votes map[string]string, players map[string]*player) map[string]string {
	valid := make(map[string]string, len(votes))
	for voterID, targetID := range votes {
		voter, exists := players[voterID]
		if !exists || !voter.Alive {
			continue
		}
		if targetID != voteSkipValue {
			target, exists := players[targetID]
			if !exists || !target.Alive {
				continue
			}
		}
		valid[voterID] = targetID
	}
	return valid
}

func (cmd *WerewolfCommand) resolveVote(s *discordgo.Session, channelID string, seq uint64) {
	var (
		gameFound      bool
		lynchedPlayer  *player
		hunterRevenged []*player
		loverDeaths    []*player
		revealOnDeath  bool
		summary        string
		aliveThreadID  string
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
		game.DayVoteClosed = true

		top, tie := tallyVotes(validDayVotes(game.Votes, game.Players))
		if !tie && len(top) == 1 {
			if target, exists := game.Players[top[0]]; exists && target.Alive {
				target.Alive = false
				lynchedPlayer = copyPlayer(target)
			}
		}
		if lynchedPlayer != nil {
			hunterRevenged, loverDeaths = splitChainedDeaths(applyDeathChains(game, lynchedPlayer.UserID))
		}
		var deadIDs []string
		if lynchedPlayer != nil {
			deadIDs = append(deadIDs, lynchedPlayer.UserID)
		}
		deadIDs = append(deadIDs, idsOf(hunterRevenged)...)
		deadIDs = append(deadIDs, idsOf(loverDeaths)...)
		applyWolfCubBonusBiteFlag(game, deadIDs)
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
			if len(hunterRevenged) > 0 {
				entry.HunterRevengeLabels = append(entry.HunterRevengeLabels, labelsOf(hunterRevenged)...)
			}
			if len(loverDeaths) > 0 {
				entry.LoverDeathLabels = append(entry.LoverDeathLabels, labelsOf(loverDeaths)...)
			}
		}
	}()
	if !gameFound {
		return
	}
	if lynchedPlayer != nil {
		removeFromAliveThread(s, aliveThreadID, lynchedPlayer.UserID)
	}
	for _, p := range hunterRevenged {
		removeFromAliveThread(s, aliveThreadID, p.UserID)
	}
	for _, p := range loverDeaths {
		removeFromAliveThread(s, aliveThreadID, p.UserID)
	}

	var resultMsg string
	switch {
	case lynchedPlayer != nil && revealOnDeath:
		resultMsg = fmt.Sprintf("⚖️ **%s** bị treo cổ. Vai trò: %s.", lynchedPlayer.Label, displayRoleLabel(lynchedPlayer))
	case lynchedPlayer != nil:
		resultMsg = fmt.Sprintf("⚖️ **%s** bị treo cổ.", lynchedPlayer.Label)
	default:
		resultMsg = "🤷 Không đủ số phiếu áp đảo, không ai bị treo cổ hôm nay."
	}
	for _, p := range hunterRevenged {
		resultMsg += "\n" + hunterRevengeLine(p, revealOnDeath)
	}
	for _, p := range loverDeaths {
		resultMsg += "\n" + loverDeathLine(p, revealOnDeath)
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
		removedPlayers  []*player
		removedIDs      []string
		hunterRevenged  []*player
		loverDeaths     []*player
		traitorPromoted []*player
		threadID        string
		result          gameResult
		endedGame       *werewolfGame
		aliveThreadID   string
		guildID         string
		priorOverwrite  *discordgo.PermissionOverwrite
		channelUnlocked bool
		revealOnDeath   bool
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
		revealOnDeath = game.RevealOnDeath

		for _, id := range game.JoinOrder {
			p := game.Players[id]
			if p.Alive && shouldRemoveForAFK(game.AFKStrikes[id]) {
				p.Alive = false
				removedPlayers = append(removedPlayers, copyPlayer(p))
				removedIDs = append(removedIDs, id)
			}
		}
		var chained []chainedDeath
		for _, id := range removedIDs {
			chained = append(chained, applyDeathChains(game, id)...)
		}
		hunterRevenged, loverDeaths = splitChainedDeaths(chained)
		chainDeadIDs := append(append([]string(nil), removedIDs...), idsOf(hunterRevenged)...)
		chainDeadIDs = append(chainDeadIDs, idsOf(loverDeaths)...)
		applyWolfCubBonusBiteFlag(game, chainDeadIDs)
		traitorPromoted = promoteTraitorIfWolvesWiped(game)
		if n := len(game.History); n > 0 {
			if len(hunterRevenged) > 0 {
				game.History[n-1].HunterRevengeLabels = append(game.History[n-1].HunterRevengeLabels, labelsOf(hunterRevenged)...)
			}
			if len(loverDeaths) > 0 {
				game.History[n-1].LoverDeathLabels = append(game.History[n-1].LoverDeathLabels, labelsOf(loverDeaths)...)
			}
		}

		result = checkWinConditionWithLovers(game.Players, game.LoverIDs)
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
		// Only DM the promotion when the game is actually continuing — if
		// this same tick both promotes the Traitor and ends the game
		// (their promotion was the last wolf standing), "you'll vote with
		// the pack from now on" would be confusing noise for a game that's
		// already over instead of the dramatic reveal it's meant to be.
		for _, p := range traitorPromoted {
			dmTraitorPromoted(s, p)
		}
		for _, id := range removedIDs {
			removeFromAliveThread(s, aliveThreadID, id)
		}
		for _, p := range hunterRevenged {
			removeFromAliveThread(s, aliveThreadID, p.UserID)
		}
		for _, p := range loverDeaths {
			removeFromAliveThread(s, aliveThreadID, p.UserID)
		}
		if len(removedPlayers) > 0 {
			cmd.announceAFKRemovals(s, channelID, removedPlayers, revealOnDeath)
		}
		if len(hunterRevenged) > 0 {
			cmd.announceHunterRevenge(s, channelID, hunterRevenged, revealOnDeath)
		}
		if len(loverDeaths) > 0 {
			cmd.announceLoverDeaths(s, channelID, loverDeaths, revealOnDeath)
		}
		return false
	}

	// Routed through the shared teardownGame rather than open-coding the
	// same three steps: this is the path a normal game-end actually takes,
	// so it MUST get teardownGame's unlock-before-delete ordering (a
	// mid-teardown process kill has to leave the channel usable, not
	// locked with no game left in memory to unlock it). Open-coding it here
	// is exactly how the two copies drifted apart before.
	teardownGame(s, gameTeardownInfo{
		channelID:       channelID,
		threadID:        threadID,
		aliveThreadID:   aliveThreadID,
		guildID:         guildID,
		priorOverwrite:  priorOverwrite,
		channelUnlocked: channelUnlocked,
	})

	// Names derived from removedPlayers rather than accumulated in a
	// parallel slice — one source of truth, so the two can't drift.
	cmd.announceGameEnd(s, endedGame, result, labelsOf(removedPlayers))
	return true
}

// labelsOf extracts display labels from a player slice, for the several
// announcement helpers that only need names.
func labelsOf(players []*player) []string {
	if len(players) == 0 {
		return nil
	}
	labels := make([]string, 0, len(players))
	for _, p := range players {
		labels = append(labels, p.Label)
	}
	return labels
}

// announceAFKRemovals reports players auto-eliminated for inactivity. Like
// every other death announcement in the game (night kill, poison, lynch,
// voluntary leave, hunter revenge, lover death) it honours RevealOnDeath —
// an AFK elimination is still an elimination, and silently concealing its
// role while every other death in the same game publishes one would be an
// information asymmetry, not a design choice.
func (cmd *WerewolfCommand) announceAFKRemovals(s *discordgo.Session, channelID string, removed []*player, revealOnDeath bool) {
	labels := make([]string, 0, len(removed))
	for _, p := range removed {
		if revealOnDeath {
			labels = append(labels, fmt.Sprintf("%s (%s)", p.Label, displayRoleLabel(p)))
		} else {
			labels = append(labels, p.Label)
		}
	}
	content := fmt.Sprintf("💤 %s bị loại vì AFK 2 lượt liên tiếp.", strings.Join(labels, ", "))
	if _, err := s.ChannelMessageSendComplex(channelID, &discordgo.MessageSend{
		Content:         content,
		AllowedMentions: &discordgo.MessageAllowedMentions{},
	}); err != nil {
		log.Error().Err(err).Msg("werewolf: failed to announce AFK removals")
	}
}

func (cmd *WerewolfCommand) announceHunterRevenge(s *discordgo.Session, channelID string, revenged []*player, revealOnDeath bool) {
	for _, p := range revenged {
		if _, err := s.ChannelMessageSendComplex(channelID, &discordgo.MessageSend{
			Content:         hunterRevengeLine(p, revealOnDeath),
			AllowedMentions: &discordgo.MessageAllowedMentions{},
		}); err != nil {
			log.Error().Err(err).Msg("werewolf: failed to announce hunter revenge")
		}
	}
}

func (cmd *WerewolfCommand) announceLoverDeaths(s *discordgo.Session, channelID string, lovers []*player, revealOnDeath bool) {
	for _, p := range lovers {
		if _, err := s.ChannelMessageSendComplex(channelID, &discordgo.MessageSend{
			Content:         loverDeathLine(p, revealOnDeath),
			AllowedMentions: &discordgo.MessageAllowedMentions{},
		}); err != nil {
			log.Error().Err(err).Msg("werewolf: failed to announce lover death")
		}
	}
}

// winnerNamesWithRoles renders "Name (Role)" for every player who shares in
// result's victory, comma-joined — the game-end announcement previously
// only named the winning SIDE, leaving players to cross-reference the
// separate role list themselves to see who actually won. Standard
// Werewolf convention: every member of the winning team shares the win,
// dead or alive, since the team's outcome is what "winning" means (a dead
// Seer whose team wins is still a winner). Lovers-win is the one
// exception, handled separately since that "team" is exactly the pair
// (plus Cupid) rather than a faction.
func winnerNamesWithRoles(game *werewolfGame, result gameResult) string {
	var winners []*player
	switch result {
	case resultVillagersWin:
		for _, id := range game.JoinOrder {
			if p := game.Players[id]; p.Faction == factionVillager {
				winners = append(winners, p)
			}
		}
	case resultWerewolvesWin:
		for _, id := range game.JoinOrder {
			if p := game.Players[id]; p.Faction == factionWolf {
				winners = append(winners, p)
			}
		}
	case resultLoversWin:
		for _, id := range game.LoverIDs {
			if p, exists := game.Players[id]; exists {
				winners = append(winners, p)
			}
		}
		for _, id := range game.JoinOrder {
			p := game.Players[id]
			if p.Role == roleCupid && p.UserID != game.LoverIDs[0] && p.UserID != game.LoverIDs[1] {
				winners = append(winners, p)
			}
		}
	}
	names := make([]string, 0, len(winners))
	for _, p := range winners {
		// finalRoleLabel: this runs only after the game has ended, and the
		// winner list partitions on Faction — so a converted Cursed Wolf
		// must be annotated here or it reads as a villager-side role
		// credited with the wolves' win. See finalRoleLabel's doc comment.
		names = append(names, fmt.Sprintf("%s (%s)", p.Label, finalRoleLabel(p)))
	}
	return strings.Join(names, ", ")
}

// werewolfMessageLimit is the ceiling this package packs messages to. It
// sits under Discord's hard 2000-character cap so a miscount can't turn a
// long game's result into a dropped message.
const werewolfMessageLimit = 1900

// splitIntoMessages packs header followed by lines into as few messages as
// possible, each at most limit runes. Runes, not bytes: Vietnamese display
// names run 2-3 bytes per character and emoji 4, so a byte-based split both
// truncates far too early and can cut a character in half.
//
// A line longer than the limit on its own is clamped rather than dropped —
// losing part of one player's entry beats losing the whole message, which
// is what Discord does with an over-length send.
func splitIntoMessages(header string, lines []string, limit int) []string {
	clamp := func(text string) string {
		if r := []rune(text); len(r) > limit {
			return string(r[:limit])
		}
		return text
	}

	var out []string
	current := clamp(header)
	for _, line := range lines {
		line = clamp(line)
		if len([]rune(current))+len([]rune(line)) > limit {
			if strings.TrimSpace(current) != "" {
				out = append(out, current)
			}
			current = ""
		}
		current += line
	}
	if strings.TrimSpace(current) != "" {
		out = append(out, current)
	}
	return out
}

func (cmd *WerewolfCommand) announceGameEnd(s *discordgo.Session, game *werewolfGame, result gameResult, afkRemoved []string) {
	winner := "🐺 **Ma Sói thắng!**"
	switch result {
	case resultVillagersWin:
		winner = "🏡 **Dân Làng thắng!**"
	case resultLoversWin:
		// Standard Ultimate Werewolf "mixed-faction lovers" rule: the two
		// lovers win together as their own team, and Cupid wins alongside
		// them too (even if Cupid isn't part of the pair, and even if
		// Cupid has already died) — unless Cupid IS one of the two lovers,
		// in which case they're already named as part of the couple.
		var loverLabels []string
		for _, id := range game.LoverIDs {
			if p, exists := game.Players[id]; exists {
				loverLabels = append(loverLabels, p.Label)
			}
		}
		winner = fmt.Sprintf("💘 **Cặp đôi %s thắng!** Họ sống sót đến cuối cùng dù thuộc hai phe khác nhau.", strings.Join(loverLabels, " & "))
		for _, p := range game.Players {
			if p.Role == roleCupid && p.UserID != game.LoverIDs[0] && p.UserID != game.LoverIDs[1] {
				winner += fmt.Sprintf(" 💘 %s (Thần Tình Yêu) cũng thắng cùng họ.", p.Label)
			}
		}
	}

	winnersLine := winnerNamesWithRoles(game, result)

	roleLines := make([]string, 0, len(game.JoinOrder))
	for _, id := range game.JoinOrder {
		p := game.Players[id]
		status := "còn sống"
		if !p.Alive {
			status = "đã chết"
		}
		// finalRoleLabel, not displayRoleLabel: the end-of-game roster is
		// the one place a Cursed Wolf's conversion may be revealed, and it
		// must be, so this list doesn't contradict the Faction-partitioned
		// winner line printed just above it.
		roleLines = append(roleLines, fmt.Sprintf("• %s — %s (%s)\n", p.Label, finalRoleLabel(p), status))
	}

	afkNote := ""
	if len(afkRemoved) > 0 {
		afkNote = fmt.Sprintf("\n💤 %s bị loại vì AFK trước khi ván kết thúc.", strings.Join(afkRemoved, ", "))
	}

	// Ping every player who joined, winners and losers alike, so the result
	// reaches people who tabbed away during a long game. pingOnce dedupes
	// and returns AllowedMentions scoped to exactly these user IDs — never a
	// role or @everyone ping, no matter what a display name contains.
	pings, allowedMentions := pingOnce(game.JoinOrder)

	// The result and the role roster go out as SEPARATE messages. Together
	// they already reached 1936 runes for a 20-player game with 32-character
	// display names, against Discord's 2000-character cap — and the mention
	// list adds ~440 more. Over the cap the send fails outright and players
	// get no result at all, so the roster is split off and chunked rather
	// than appended.
	header := fmt.Sprintf("🎉 Ván Ma Sói kết thúc! %s\n🏆 **Người thắng:** %s%s", winner, winnersLine, afkNote)
	if pings != "" {
		header += "\n\n" + pings
	}
	for idx, content := range splitIntoMessages(header, nil, werewolfMessageLimit) {
		msg := &discordgo.MessageSend{Content: content, AllowedMentions: &discordgo.MessageAllowedMentions{}}
		// Mentions resolve only in the chunk that actually carries the tags.
		if idx == 0 {
			msg.AllowedMentions = allowedMentions
		}
		if _, err := s.ChannelMessageSendComplex(game.ChannelID, msg); err != nil {
			log.Error().Err(err).Msg("werewolf: failed to announce game end")
		}
	}

	for _, content := range splitIntoMessages("**Vai trò của mọi người:**\n", roleLines, werewolfMessageLimit) {
		if _, err := s.ChannelMessageSendComplex(game.ChannelID, &discordgo.MessageSend{
			Content:         content,
			AllowedMentions: &discordgo.MessageAllowedMentions{},
		}); err != nil {
			log.Error().Err(err).Msg("werewolf: failed to post end-of-game role list")
		}
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
	poisoned := r.WitchPoisonedLabel != ""
	if r.WolfBonusBite {
		if len(r.WolfBiteOutcomeLines) > 0 {
			fmt.Fprintf(&b, "\n🐺🐺 Sói cắn %d mục tiêu (Sói Con đã ngã xuống đêm trước):", len(r.WolfBiteOutcomeLines))
			for _, line := range r.WolfBiteOutcomeLines {
				fmt.Fprintf(&b, "\n%s", line)
			}
		}
	} else if r.WolfTargetLabel != "" {
		switch {
		case r.WolfTargetSaved && poisoned:
			fmt.Fprintf(&b, "\n🐺 Sói cắn: %s (được bác sĩ cứu, nhưng đêm đó vẫn có người chết vì trúng độc)", r.WolfTargetLabel)
		case r.WolfTargetSaved:
			fmt.Fprintf(&b, "\n🐺 Sói cắn: %s (được bác sĩ cứu)", r.WolfTargetLabel)
		case r.WitchSavedLabel != "":
			fmt.Fprintf(&b, "\n🐺 Sói cắn: %s (được phù thủy cứu)", r.WolfTargetLabel)
		case len(r.CursedWolfConverted) > 0:
			fmt.Fprintf(&b, "\n🐺 Sói cắn: %s (nhưng đã biến thành Sói Nguyền, không chết)", r.WolfTargetLabel)
		default:
			fmt.Fprintf(&b, "\n🐺 Sói cắn: %s", r.WolfTargetLabel)
		}
	} else if r.WolfVoteTied && poisoned {
		b.WriteString("\n🐺 Sói không thống nhất được mục tiêu, nhưng đêm đó vẫn có người chết vì trúng độc.")
	} else if r.WolfVoteTied {
		b.WriteString("\n🐺 Sói không thống nhất được mục tiêu.")
	}
	if r.WitchPoisonedLabel != "" {
		fmt.Fprintf(&b, "\n☠️ Phù thủy đầu độc: %s", r.WitchPoisonedLabel)
	}
	if r.DoctorTargetLabel != "" {
		fmt.Fprintf(&b, "\n💉 Bác sĩ bảo vệ: %s", r.DoctorTargetLabel)
	}
	if r.SeerTargetLabel != "" {
		fmt.Fprintf(&b, "\n🔮 Tiên tri xem: %s → %s", r.SeerTargetLabel, r.SeerResult.label())
	}
	if len(r.HunterRevengeLabels) > 0 {
		fmt.Fprintf(&b, "\n🏹 Thợ săn kéo theo: %s", strings.Join(r.HunterRevengeLabels, ", "))
	}
	if len(r.LoverDeathLabels) > 0 {
		fmt.Fprintf(&b, "\n💔 Người yêu qua đời theo: %s", strings.Join(r.LoverDeathLabels, ", "))
	}
	if r.DisruptorMutedLabel != "" {
		fmt.Fprintf(&b, "\n🤫 Kẻ phá hoại làm câm lặng: %s", r.DisruptorMutedLabel)
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

// maxRecapMessages caps how many messages the end-of-game recap posts in
// one burst. game.History grows one entry per round with no upper bound, so
// a long game (many ties, saves and conversions, especially a custom-roles
// game where every special role fires every round) can otherwise produce
// 25+ back-to-back messages. Discord throttles a channel to roughly 5
// messages per 5 seconds, so discordgo would spend ~25s rate-limited while
// burying the channel. A realistic game produces 2-10 messages and never
// reaches this cap; it exists for the pathological tail.
const maxRecapMessages = 15

// chunkRecapMessages splits the full round-by-round recap into message
// bodies, each at most limit CHARACTERS (runes, matching how Discord counts
// a message), and never more than maxRecapMessages of them. Always returns
// at least one string when history is non-empty.
//
// Splitting prefers whole-round boundaries, but will split WITHIN a round
// on line boundaries when a single round doesn't fit. That matters: at 20
// players a round whose votes spread across many targets renders close to
// 2000 characters on its own (measured ~1945 with max-length display
// names), and a round that overflows would otherwise be emitted as one
// oversized chunk, rejected by Discord with 400/50035 — and since the send
// error is only logged, that round would vanish from the recap with
// nothing in the channel indicating anything was lost.
//
// Counting runes rather than bytes is deliberate. Vietnamese text runs 2-3
// bytes per character, so byte-counting would both under-pack messages and,
// worse, give a guarantee in the wrong unit from the one Discord enforces.
func chunkRecapMessages(history []roundLog, limit int) []string {
	if len(history) == 0 {
		return nil
	}
	const header = "📜 **Lịch sử ván đấu**"
	const continued = "📜 **Lịch sử ván đấu** (tiếp)"

	var chunks []string
	current := header
	currentLen := len([]rune(header))
	// startFresh closes the current chunk and begins a new one. Subsequent
	// chunks use the "(tiếp)" header so a reader can tell a continuation
	// from the start of the recap.
	startFresh := func() {
		chunks = append(chunks, current)
		current = continued
		currentLen = len([]rune(continued))
	}

	for _, r := range history {
		block := formatRoundBlock(r)
		blockLen := len([]rune(block))

		// Fast path: the whole round fits in the current chunk, or fits in
		// a fresh one. Keep rounds intact whenever possible.
		if currentLen+blockLen <= limit {
			current += block
			currentLen += blockLen
			continue
		}
		if blockLen+len([]rune(continued)) <= limit {
			startFresh()
			current += block
			currentLen += blockLen
			continue
		}

		// Slow path: this single round is too big for any chunk. Split it
		// line by line. formatRoundBlock builds every line with a leading
		// "\n", so splitting on "\n" and re-adding it preserves the text
		// exactly.
		//
		// maxPiece is what can ever fit in a fresh chunk: the limit minus
		// the continuation header that chunk will carry. A single line
		// longer than that is clamped to it BEFORE deciding whether to
		// start a new chunk — clamping afterwards (or to the full limit)
		// would let header+piece exceed the limit, which is exactly the
		// overflow this whole path exists to prevent.
		maxPiece := limit - len([]rune(continued))
		for _, line := range strings.Split(block, "\n") {
			if line == "" {
				continue // leading empty piece from the block's own "\n\n"
			}
			piece := "\n" + line
			pieceLen := len([]rune(piece))
			if maxPiece > 0 && pieceLen > maxPiece {
				// Only reachable with a display name far past Discord's
				// 32-character cap; trim rather than emit a message
				// Discord will reject outright.
				piece = string([]rune(piece)[:maxPiece])
				pieceLen = maxPiece
			}
			if currentLen+pieceLen > limit {
				startFresh()
			}
			current += piece
			currentLen += pieceLen
		}
	}
	chunks = append(chunks, current)

	if len(chunks) <= maxRecapMessages {
		return chunks
	}
	// Too long: keep the most recent rounds — the ones that decided the
	// game — and replace the dropped prefix with a single notice, so the
	// total stays exactly maxRecapMessages.
	dropped := len(chunks) - (maxRecapMessages - 1)
	notice := fmt.Sprintf(
		"📜 **Lịch sử ván đấu** — ván này quá dài, đã lược bớt %d phần đầu để tránh spam kênh. Dưới đây là các vòng gần nhất:",
		dropped)
	return append([]string{notice}, chunks[dropped:]...)
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
	if !ok || game.Phase != phaseDayVote || game.DayVoteClosed {
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
