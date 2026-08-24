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
		guildID = game.GuildID
		aliveThreadID = game.AliveThreadID
		wolfThreadID = game.WolfThreadID
		mutedUserID = game.DisruptorTarget

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
	unmuteAliveThread(s, guildID, aliveThreadID)
	if mutedUserID != "" && aliveThreadID != "" {
		if err := s.ChannelPermissionSet(aliveThreadID, mutedUserID, discordgo.PermissionOverwriteTypeMember, 0, discordgo.PermissionSendMessagesInThreads); err != nil {
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
	var guildID string
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
		guildID = game.GuildID
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
	muteAliveThread(s, guildID, aliveThreadID)
	if mutedUserID != "" && aliveThreadID != "" {
		if err := s.ChannelPermissionDelete(aliveThreadID, mutedUserID); err != nil {
			log.Error().Err(err).Str("user_id", mutedUserID).Msg("werewolf: failed to remove disruptor per-member mute")
		}
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
				entry.HunterRevengeLabel = hunterRevenged[0].Label
			}
			if len(loverDeaths) > 0 {
				entry.LoverDeathLabel = loverDeaths[0].Label
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
		removedNames    []string
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
				removedNames = append(removedNames, p.Label)
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
				game.History[n-1].HunterRevengeLabel = hunterRevenged[0].Label
			}
			if len(loverDeaths) > 0 {
				game.History[n-1].LoverDeathLabel = loverDeaths[0].Label
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

	cmd.announceGameEnd(s, endedGame, result, removedNames)
	return true
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
		names = append(names, fmt.Sprintf("%s (%s)", p.Label, displayRoleLabel(p)))
	}
	return strings.Join(names, ", ")
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

	var roles strings.Builder
	roles.WriteString("\n\n**Vai trò của mọi người:**\n")
	for _, id := range game.JoinOrder {
		p := game.Players[id]
		status := "còn sống"
		if !p.Alive {
			status = "đã chết"
		}
		fmt.Fprintf(&roles, "• %s — %s (%s)\n", p.Label, displayRoleLabel(p), status)
	}

	afkNote := ""
	if len(afkRemoved) > 0 {
		afkNote = fmt.Sprintf("\n💤 %s bị loại vì AFK trước khi ván kết thúc.", strings.Join(afkRemoved, ", "))
	}

	if _, err := s.ChannelMessageSendComplex(game.ChannelID, &discordgo.MessageSend{
		Content:         fmt.Sprintf("🎉 Ván Ma Sói kết thúc! %s\n🏆 **Người thắng:** %s%s%s", winner, winnersLine, afkNote, roles.String()),
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
	if r.HunterRevengeLabel != "" {
		fmt.Fprintf(&b, "\n🏹 Thợ săn kéo theo: %s", r.HunterRevengeLabel)
	}
	if r.LoverDeathLabel != "" {
		fmt.Fprintf(&b, "\n💔 Người yêu qua đời theo: %s", r.LoverDeathLabel)
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
