package commands

import (
	"fmt"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/rs/zerolog/log"
)

const (
	werewolfVoteID   = "ww:vote:"
	dayDiscussWindow = 3 * time.Minute
	dayVoteWindow    = 90 * time.Second
)

func (cmd *WerewolfCommand) runDayDiscuss(s *discordgo.Session, channelID string) {
	cmd.mu.Lock()
	game, ok := cmd.games[channelID]
	if !ok {
		cmd.mu.Unlock()
		return
	}
	game.Phase = phaseDayDiscuss
	round := game.Round
	seq := game.Seq
	cmd.mu.Unlock()

	if _, err := s.ChannelMessageSendComplex(channelID, &discordgo.MessageSend{
		Content:         fmt.Sprintf("☀️ **Ngày %d.** Thảo luận trong %s trước khi bỏ phiếu.", round, formatRemaining(dayDiscussWindow)),
		AllowedMentions: &discordgo.MessageAllowedMentions{},
	}); err != nil {
		log.Error().Err(err).Msg("werewolf: failed to post day-discuss announcement")
	}

	time.Sleep(dayDiscussWindow)

	cmd.mu.Lock()
	stillGoing := cmd.gameIfCurrent(channelID, seq) != nil
	cmd.mu.Unlock()
	if !stillGoing {
		return
	}

	cmd.runDayVote(s, channelID)
}

func (cmd *WerewolfCommand) runDayVote(s *discordgo.Session, channelID string) {
	cmd.mu.Lock()
	game, ok := cmd.games[channelID]
	if !ok {
		cmd.mu.Unlock()
		return
	}
	game.Phase = phaseDayVote
	game.Votes = make(map[string]string)
	seq := game.Seq
	var alive []*player
	for _, id := range game.JoinOrder {
		if p := game.Players[id]; p.Alive {
			alive = append(alive, p)
		}
	}
	cmd.mu.Unlock()

	if _, err := s.ChannelMessageSendComplex(channelID, &discordgo.MessageSend{
		Content:    "🗳️ **Bỏ phiếu treo cổ.** Chọn người bạn nghi ngờ.",
		Components: targetSelectMenu(werewolfVoteID+channelID, "Chọn người để treo cổ", alive),
	}); err != nil {
		log.Error().Err(err).Msg("werewolf: failed to post vote select")
	}

	deadline := time.Now().Add(dayVoteWindow)
	cmd.waitForVotes(s, channelID, deadline, len(alive), seq)

	cmd.resolveVote(s, channelID, seq)
}

func (cmd *WerewolfCommand) waitForVotes(s *discordgo.Session, channelID string, deadline time.Time, aliveCount int, seq uint64) {
	for {
		cmd.mu.Lock()
		game := cmd.gameIfCurrent(channelID, seq)
		var done bool
		if game != nil {
			done = len(game.Votes) >= aliveCount
		}
		cmd.mu.Unlock()
		if game == nil || done || time.Now().After(deadline) {
			return
		}
		time.Sleep(2 * time.Second)
	}
}

func (cmd *WerewolfCommand) resolveVote(s *discordgo.Session, channelID string, seq uint64) {
	cmd.mu.Lock()
	game := cmd.gameIfCurrent(channelID, seq)
	if game == nil {
		cmd.mu.Unlock()
		return
	}

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
	var lynchedPlayer *player
	if !tie && len(top) == 1 {
		if target, exists := game.Players[top[0]]; exists {
			target.Alive = false
			lynchedPlayer = target
		}
	}
	revealOnDeath := game.RevealOnDeath
	cmd.mu.Unlock()

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
		Content:         resultMsg,
		AllowedMentions: &discordgo.MessageAllowedMentions{},
	}); err != nil {
		log.Error().Err(err).Msg("werewolf: failed to post vote result")
	}

	if cmd.checkAndHandleGameEnd(s, channelID) {
		return
	}
	cmd.runNightPhase(s, channelID)
}

// checkAndHandleGameEnd applies AFK-strike removals, then checks the win
// condition. If the game has ended, it announces the result, reveals every
// role, deletes any remaining wolf thread, deletes the game from the
// pending map, and returns true — callers must stop processing when true.
func (cmd *WerewolfCommand) checkAndHandleGameEnd(s *discordgo.Session, channelID string) bool {
	cmd.mu.Lock()
	game, ok := cmd.games[channelID]
	if !ok {
		cmd.mu.Unlock()
		return true
	}

	var removedNames []string
	for _, id := range game.JoinOrder {
		p := game.Players[id]
		if p.Alive && shouldRemoveForAFK(game.AFKStrikes[id]) {
			p.Alive = false
			removedNames = append(removedNames, p.Label)
		}
	}

	result := checkWinCondition(game.Players)
	if result == resultOngoing {
		cmd.mu.Unlock()
		if len(removedNames) > 0 {
			cmd.announceAFKRemovals(s, channelID, removedNames)
		}
		return false
	}

	threadID := game.WolfThreadID
	delete(cmd.games, channelID)
	cmd.mu.Unlock()

	if threadID != "" {
		if _, err := s.ChannelDelete(threadID); err != nil {
			log.Error().Err(err).Msg("werewolf: failed to delete wolf thread on game end")
		}
	}

	cmd.announceGameEnd(s, game, result, removedNames)
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
	game.Votes[voterID] = targetID
	cmd.mu.Unlock()

	respondComponentEphemeralMentioned(s, i, fmt.Sprintf("✅ Đã bỏ phiếu cho <@%s>.", targetID))
}
