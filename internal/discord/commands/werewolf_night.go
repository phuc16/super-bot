package commands

import (
	"fmt"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/rs/zerolog/log"
)

const (
	werewolfWolfTargetID    = "ww:wolftarget:"
	werewolfSeerCheckID     = "ww:seercheck:"
	werewolfDoctorProtectID = "ww:doctorprotect:"
	nightActionWindow       = 90 * time.Second
)

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
		if p.Alive && p.Role != roleWerewolf {
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

// runNightPhase resets this round's night state, opens the wolf thread and
// seer/doctor DMs, waits for actions (or the timer), then resolves the night.
func (cmd *WerewolfCommand) runNightPhase(s *discordgo.Session, channelID string) {
	cmd.mu.Lock()
	game, ok := cmd.games[channelID]
	if !ok {
		cmd.mu.Unlock()
		return
	}
	game.Phase = phaseNight
	game.Round++
	game.NightTarget = ""
	game.SeerActed = false
	game.DoctorProtect = ""

	seq := game.Seq
	round := game.Round
	wolves := copyPlayers(alivePlayersOfRole(game, roleWerewolf))
	nonWolfTargets := copyPlayers(aliveNonWolves(game))

	var seer *player
	var seerTargets []*player
	if seers := alivePlayersOfRole(game, roleSeer); len(seers) > 0 {
		seer = copyPlayer(seers[0])
		for _, id := range game.JoinOrder {
			p := game.Players[id]
			if p.Alive && p.UserID != seer.UserID {
				seerTargets = append(seerTargets, copyPlayer(p))
			}
		}
	}

	var doctor *player
	var doctorTargets []*player
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
	cmd.mu.Unlock()

	if _, err := s.ChannelMessageSendComplex(channelID, &discordgo.MessageSend{
		Content:         fmt.Sprintf("🌙 **Đêm %d bắt đầu.** Ma Sói, Tiên Tri, Bác Sĩ đang hành động trong tin nhắn riêng...", round),
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

	deadline := time.Now().Add(nightActionWindow)
	cmd.waitForNightActions(s, channelID, deadline, seq)

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

	cmd.mu.Lock()
	if g := cmd.gameIfCurrent(channelID, seq); g != nil {
		g.WolfThreadID = thread.ID
	}
	cmd.mu.Unlock()

	for _, w := range wolves {
		if err := s.ThreadMemberAdd(thread.ID, w.UserID); err != nil {
			log.Error().Err(err).Str("user_id", w.UserID).Msg("werewolf: failed to add wolf to thread")
		}
	}

	if _, err := s.ChannelMessageSendComplex(thread.ID, &discordgo.MessageSend{
		Content:    "🐺 Chọn mục tiêu để tấn công đêm nay. Lượt chọn đầu tiên sẽ được tính.",
		Components: targetSelectMenu(werewolfWolfTargetID+channelID, "Chọn mục tiêu", targets),
	}); err != nil {
		log.Error().Err(err).Msg("werewolf: failed to post wolf target select")
	}
}

// dmWolvesIndependently is the fallback when the wolf thread can't be
// created: each wolf gets the same target-picker in their own DM instead.
// The first submitted pick still stands (handleWolfTarget enforces that).
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
	if len(alivePlayersOfRole(game, roleWerewolf)) > 0 && game.NightTarget == "" {
		return false
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
func (cmd *WerewolfCommand) waitForNightActions(s *discordgo.Session, channelID string, deadline time.Time, seq uint64) {
	reminded := false
	for {
		cmd.mu.Lock()
		game := cmd.gameIfCurrent(channelID, seq)
		var done bool
		if game != nil {
			done = cmd.nightActionsComplete(game)
		}
		cmd.mu.Unlock()
		if game == nil || done || time.Now().After(deadline) {
			return
		}

		remaining := time.Until(deadline)
		if !reminded && remaining <= nightActionWindow/2 {
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
	cmd.mu.Lock()
	game := cmd.gameIfCurrent(channelID, seq)
	if game == nil {
		cmd.mu.Unlock()
		return
	}

	var killedID string
	saved := false
	if game.NightTarget != "" {
		if game.NightTarget == game.DoctorProtect {
			saved = true
		} else if target, exists := game.Players[game.NightTarget]; exists && target.Alive {
			target.Alive = false
			killedID = game.NightTarget
		}
	}

	if game.NightTarget == "" {
		for _, w := range alivePlayersOfRole(game, roleWerewolf) {
			game.AFKStrikes[w.UserID]++
		}
	} else {
		for _, w := range alivePlayersOfRole(game, roleWerewolf) {
			game.AFKStrikes[w.UserID] = 0
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
	threadID := game.WolfThreadID
	game.WolfThreadID = ""
	revealOnDeath := game.RevealOnDeath
	var killedPlayer *player
	if killedID != "" {
		killedPlayer = game.Players[killedID]
	}
	cmd.mu.Unlock()

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
	default:
		resultMsg = "😴 Không ai chết trong đêm qua."
	}

	if _, err := s.ChannelMessageSendComplex(channelID, &discordgo.MessageSend{
		Content:         resultMsg,
		AllowedMentions: &discordgo.MessageAllowedMentions{},
	}); err != nil {
		log.Error().Err(err).Msg("werewolf: failed to post night result")
	}

	if cmd.checkAndHandleGameEnd(s, channelID) {
		return
	}
	cmd.runDayDiscuss(s, channelID)
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
	alreadyLocked := game.NightTarget != ""
	if !alreadyLocked {
		game.NightTarget = targetID
	}
	locked := game.NightTarget
	cmd.mu.Unlock()

	if alreadyLocked {
		respondComponentEphemeralMentioned(s, i, fmt.Sprintf("Cả bầy đã chốt mục tiêu: <@%s>.", locked))
		return
	}
	respondComponentEphemeralMentioned(s, i, fmt.Sprintf("✅ Đã chốt mục tiêu: <@%s>.", targetID))
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
	game.SeerActed = true
	target := game.Players[targetID]
	isWolf := target != nil && target.Role == roleWerewolf
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
	game.DoctorProtect = targetID
	cmd.mu.Unlock()

	respondComponentEphemeralMentioned(s, i, fmt.Sprintf("✅ Đã chọn bảo vệ: <@%s>.", targetID))
}
