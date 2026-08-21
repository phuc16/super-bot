package commands

import (
	"fmt"
	"math/rand"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/rs/zerolog/log"
)

// werewolfSeqCounter hands out a unique, monotonically increasing Seq to
// every werewolfGame created, regardless of channel. It lets long-sleeping
// goroutines (day-discuss window, night-action wait, preflight grace period)
// detect that the game occupying "their" channel key by the time they wake
// is not the same game they started with — see werewolfGame.Seq.
var werewolfSeqCounter uint64

const (
	werewolfMinPlayers = 5
	werewolfMaxPlayers = 20
)

// roleRatio describes how many of each special role a game of a given size gets.
type roleRatio struct {
	Werewolves   int
	Seers        int
	Doctors      int
	Witches      int
	Hunters      int
	Disruptors   int
	WolfCubs     int
	CursedWolves int
	Traitors     int
	Cupids       int
}

// rolesForPlayerCount returns the role ratio for a given player count, or an
// error if the count is outside [werewolfMinPlayers, werewolfMaxPlayers].
func rolesForPlayerCount(n int) (roleRatio, error) {
	switch {
	case n < werewolfMinPlayers:
		return roleRatio{}, fmt.Errorf("cần tối thiểu %d người, hiện có %d", werewolfMinPlayers, n)
	case n > werewolfMaxPlayers:
		return roleRatio{}, fmt.Errorf("tối đa %d người, hiện có %d", werewolfMaxPlayers, n)
	case n == 5:
		return roleRatio{Werewolves: 1}, nil
	case n == 6:
		return roleRatio{Werewolves: 1, Seers: 1}, nil
	case n <= 10:
		return roleRatio{Werewolves: 2, Seers: 1, Doctors: 1}, nil
	case n <= 14:
		return roleRatio{Werewolves: 3, Seers: 1, Doctors: 1, Witches: 1, Hunters: 1}, nil
	case n <= 17:
		return roleRatio{Werewolves: 4, Seers: 1, Doctors: 1, Witches: 1, Hunters: 1, Disruptors: 1}, nil
	default: // 18-20
		return roleRatio{Werewolves: 5, Seers: 1, Doctors: 1, Witches: 1, Hunters: 1, Disruptors: 1}, nil
	}
}

type role int

const (
	roleVillager role = iota
	roleWerewolf
	roleSeer
	roleDoctor
	roleWolfCub
	roleCursedWolf
	roleTraitor
	roleWitch
	roleHunter
	roleDisruptor
	roleCupid
)

func (r role) label() string {
	switch r {
	case roleWerewolf:
		return "Ma Sói"
	case roleSeer:
		return "Tiên Tri"
	case roleDoctor:
		return "Bác Sĩ"
	case roleWolfCub:
		return "Sói Con"
	case roleCursedWolf:
		return "Sói Nguyền"
	case roleTraitor:
		return "Kẻ Phản Bội"
	case roleWitch:
		return "Phù Thủy"
	case roleHunter:
		return "Thợ Săn"
	case roleDisruptor:
		return "Kẻ Phá Hoại"
	case roleCupid:
		return "Thần Tình Yêu"
	default:
		return "Dân Làng"
	}
}

// faction determines win-condition membership, independent of role — every
// role's faction matches intuition except Cursed Wolf, which starts
// villager-faction and flips to wolf-faction on conversion (a later plan);
// that's exactly why Faction is a stored, mutable field on player rather
// than derived from Role on every read.
type faction int

const (
	factionVillager faction = iota
	factionWolf
)

// factionForRole returns the faction a freshly-dealt player of role r
// starts in. Only used at deal time (assignRoles) — Cursed Wolf's Faction
// is mutated directly at runtime afterward, not re-derived from this.
func factionForRole(r role) faction {
	switch r {
	case roleWerewolf, roleWolfCub, roleTraitor:
		return factionWolf
	default:
		return factionVillager
	}
}

// rolePoints is the manual role-pick UI's balance-score input (a later
// plan) — not used by rolesForPlayerCount's runtime logic, which stays a
// fixed lookup table.
var rolePoints = map[role]int{
	roleWerewolf:   -6,
	roleWolfCub:    -8,
	roleCursedWolf: -4,
	roleTraitor:    -6,
	roleVillager:   1,
	roleSeer:       3,
	roleDoctor:     3,
	roleWitch:      4,
	roleHunter:     3,
	roleDisruptor:  -2,
	roleCupid:      -3,
}

type player struct {
	UserID  string
	Label   string
	Role    role
	Faction faction
	Alive   bool
}

// isWolf reports whether p currently counts as wolf-faction for win
// condition and role-check purposes. Prefer this over comparing Role
// directly, since Wolf Cub/Traitor (wolf-faction, non-Werewolf roles) and
// Cursed Wolf (starts villager-faction, flips later) all need Faction-based
// checks, not Role-based ones.
func (p *player) isWolf() bool {
	return p.Faction == factionWolf
}

// wolfVoteParticipant reports whether p currently votes in the wolf pack's
// nightly kill vote. Every wolf-faction player votes except the Traitor,
// who has no night action (spec §4d) — a converted Cursed Wolf (Faction
// flipped by a prior night's bite resolution) becomes a participant
// starting the next night they're prompted, same as a Wolf Cub from deal
// time.
func (p *player) wolfVoteParticipant() bool {
	return p.isWolf() && p.Role != roleTraitor
}

// wolfRosterLabels returns the labels of every alive wolf-faction player
// except the Traitor themself — this is what the Traitor's roster DM
// shows them. A not-yet-converted Cursed Wolf is villager-faction, so
// they're naturally excluded until conversion flips Faction to
// factionWolf; no Role-based special-casing needed.
func wolfRosterLabels(game *werewolfGame) []string {
	var labels []string
	for _, id := range game.JoinOrder {
		p := game.Players[id]
		if p.Alive && p.isWolf() && p.Role != roleTraitor {
			labels = append(labels, p.Label)
		}
	}
	sort.Strings(labels)
	return labels
}

// wolfRosterDM is what traitorRosterDM hands back for the caller to send
// after releasing cmd.mu.
type wolfRosterDM struct {
	TraitorUserID string
	Labels        []string
}

// traitorRosterDM must be called with cmd.mu held. Returns non-nil only
// when a live Traitor exists — spec §4d (amended 2026-08-22): the roster
// DM resends unconditionally once per night (plus once at role
// assignment), not only when the roster actually changed, so the Traitor
// never has to guess whether something shifted since their last DM.
func traitorRosterDM(game *werewolfGame) *wolfRosterDM {
	traitors := alivePlayersOfRole(game, roleTraitor)
	if len(traitors) == 0 {
		return nil
	}
	return &wolfRosterDM{TraitorUserID: traitors[0].UserID, Labels: wolfRosterLabels(game)}
}

// sendTraitorRosterDM sends dm's roster to the Traitor. No-op if dm is nil
// (traitorRosterDM found no live Traitor) — every call site can pass its
// result through unconditionally.
func (cmd *WerewolfCommand) sendTraitorRosterDM(s *discordgo.Session, dm *wolfRosterDM) {
	if dm == nil {
		return
	}
	channel, err := s.UserChannelCreate(dm.TraitorUserID)
	if err != nil {
		log.Error().Err(err).Msg("werewolf: failed to open DM for traitor roster update")
		return
	}
	rosterText := "Không có Sói nào còn sống."
	if len(dm.Labels) > 0 {
		rosterText = strings.Join(dm.Labels, ", ")
	}
	if _, err := s.ChannelMessageSend(channel.ID, fmt.Sprintf("🎭 Danh sách Sói hiện tại: %s", rosterText)); err != nil {
		log.Error().Err(err).Msg("werewolf: failed to DM traitor roster update")
	}
}

// dealRoles shuffles a copy of joinOrder and deals roles per ratio, filling
// every remaining seat with roleVillager. Factored out of assignRoles so
// Plan E's manual role-pick mode can call it directly with a
// user-constructed ratio instead of always going through
// rolesForPlayerCount.
func dealRoles(joinOrder []string, labels map[string]string, ratio roleRatio) map[string]*player {
	shuffled := make([]string, len(joinOrder))
	copy(shuffled, joinOrder)
	rand.Shuffle(len(shuffled), func(i, j int) { shuffled[i], shuffled[j] = shuffled[j], shuffled[i] })

	players := make(map[string]*player, len(shuffled))
	idx := 0
	assign := func(count int, r role) {
		for k := 0; k < count; k++ {
			id := shuffled[idx]
			idx++
			players[id] = &player{UserID: id, Label: labels[id], Role: r, Faction: factionForRole(r), Alive: true}
		}
	}
	assign(ratio.Werewolves, roleWerewolf)
	assign(ratio.WolfCubs, roleWolfCub)
	assign(ratio.CursedWolves, roleCursedWolf)
	assign(ratio.Traitors, roleTraitor)
	assign(ratio.Seers, roleSeer)
	assign(ratio.Doctors, roleDoctor)
	assign(ratio.Witches, roleWitch)
	assign(ratio.Hunters, roleHunter)
	assign(ratio.Disruptors, roleDisruptor)
	assign(ratio.Cupids, roleCupid)
	for ; idx < len(shuffled); idx++ {
		id := shuffled[idx]
		players[id] = &player{UserID: id, Label: labels[id], Role: roleVillager, Faction: factionForRole(roleVillager), Alive: true}
	}
	return players
}

// assignRoles is dealRoles for the fixed-table path: it derives the ratio
// from player count instead of accepting one directly.
func assignRoles(joinOrder []string, labels map[string]string) (map[string]*player, error) {
	ratio, err := rolesForPlayerCount(len(joinOrder))
	if err != nil {
		return nil, err
	}
	return dealRoles(joinOrder, labels, ratio), nil
}

type gameResult int

const (
	resultOngoing gameResult = iota
	resultVillagersWin
	resultWerewolvesWin
)

// checkWinCondition: villagers win when every werewolf is dead; werewolves
// win once they're no longer outnumbered by everyone else still alive.
func checkWinCondition(players map[string]*player) gameResult {
	if len(players) == 0 {
		// No roster assigned yet (lobby phase) — there's nothing to win or
		// lose, so don't report either side as having won.
		return resultOngoing
	}
	aliveWolves, aliveOthers := 0, 0
	for _, p := range players {
		if !p.Alive {
			continue
		}
		if p.Faction == factionWolf {
			aliveWolves++
		} else {
			aliveOthers++
		}
	}
	switch {
	case aliveWolves == 0:
		return resultVillagersWin
	case aliveWolves >= aliveOthers:
		return resultWerewolvesWin
	default:
		return resultOngoing
	}
}

// tallyVotes counts votes and returns every target tied for the most votes.
// isTie is true when more than one target shares the highest count — the
// caller treats that as "nobody lynched."
func tallyVotes(votes map[string]string) (topTargets []string, isTie bool) {
	counts := make(map[string]int)
	for _, target := range votes {
		counts[target]++
	}
	max := 0
	for _, c := range counts {
		if c > max {
			max = c
		}
	}
	if max == 0 {
		return nil, false
	}
	for target, c := range counts {
		if c == max {
			topTargets = append(topTargets, target)
		}
	}
	return topTargets, len(topTargets) > 1
}

const afkRemovalThreshold = 2

func shouldRemoveForAFK(strikes int) bool {
	return strikes >= afkRemovalThreshold
}

type gamePhase int

const (
	phaseLobby gamePhase = iota
	// phasePreflight covers the brief window between the "Bắt đầu" click and
	// the first night phase actually starting: roles are being assigned and
	// DMed out. It exists so the lobby is unambiguously closed the instant
	// "Bắt đầu" is clicked, rather than staying in phaseLobby (and therefore
	// joinable/leavable) through up to a minute of DM sends and grace period.
	phasePreflight
	phaseNight
	phaseDayDiscuss
	phaseDayVote
)

// roundLog captures everything that happened in one round (night + day) for
// the end-of-game recap. Populated incrementally: runNightPhase appends a
// fresh entry when a round starts, resolveNight fills the night-side
// fields, and resolveVote fills the day-side fields on that same entry.
// Zero-value fields (a role that's already dead, or not in this game's
// ratio) are omitted when rendering.
type roundLog struct {
	Round               int
	WolfTargetLabel     string
	WolfTargetSaved     bool
	WolfVoteTied        bool
	DoctorTargetLabel   string
	SeerTargetLabel     string
	SeerResult          role
	VoteBreakdown       map[string][]string // target label (or voteSkipLabel) -> voter labels who chose it
	LynchedLabel        string
	HunterRevengeLabel  string // who died from Hunter's revenge this round, "" if it didn't trigger
	WitchSavedLabel     string // who the Witch healed this round, "" if she didn't heal
	WitchPoisonedLabel  string // who the Witch poisoned this round, "" if she didn't poison
	CursedWolfConverted string // label, if a not-saved Cursed Wolf converted to wolf-faction this round instead of dying
	DisruptorMutedLabel string // who was muted for the following day-discuss, "" if the Disruptor didn't act (or doesn't exist) this round

	WolfBonusBite        bool     // this round used the Wolf-Cub 2-target bite
	WolfBiteOutcomeLines []string // one pre-formatted line per resolved target this round, used only when WolfBonusBite is true — see formatRoundBlock
}

// werewolfGame tracks one in-progress Werewolf game for a single channel,
// from lobby signup through to the final win check. Entirely in-memory —
// deleted from WerewolfCommand.games the instant it ends or is cancelled.
type werewolfGame struct {
	GuildID, ChannelID, StarterID string
	MessageID                     string
	Seq                           uint64 // unique creation-order token; see werewolfSeqCounter
	RevealOnDeath                 bool
	Phase                         gamePhase
	Round                         int
	Players                       map[string]*player             // userID -> player
	JoinOrder                     []string                       // frozen once the game leaves phaseLobby
	WolfThreadID                  string                         // this round's wolf-coordination thread, "" between nights
	WolfTargetMessageID           string                         // the target-select message in the wolf thread, live-edited as votes come in
	WolfVotes                     map[string]string              // wolfUserID -> targetUserID this round, locked once cast — no re-voting
	SeerActed                     bool                           // whether the seer has acted this round
	SeerTarget                    string                         // who the seer checked this round, "" if SeerActed is false
	SeerResult                    role                           // that check's result — meaningless if SeerTarget == ""
	DoctorProtect                 string                         // doctor's pick this round
	DoctorLastPick                string                         // last round's pick — can't repeat back-to-back
	HunterTarget                  string                         // persists across rounds until Hunter explicitly changes it
	HunterActedThisRound          bool                           // reset each round; true once Hunter has made at least one pick this round — gates the progress announcement to the first pick only
	DisruptorTarget               string                         // this round's mute pick, "" if none — reset each night before the new pick, and read again unchanged by the FOLLOWING day-discuss/day-vote phase transition to apply/remove the per-member mute
	DisruptorActedThisRound       bool                           // reset each round; true once the Disruptor has made at least one pick this round — gates the progress announcement to the first pick only, mirroring Hunter's pre-AFK-exemption shape
	WitchHealUsed                 bool                           // lifetime flag — the heal potion is single-use per game
	WitchPoisonUsed               bool                           // lifetime flag — the poison potion is single-use per game
	WitchSaveTarget               string                         // this round's heal pick, "" if not saving; reset each round. Two encodings: the literal sentinel "heal" (1-target case, resolved against the lone bite target at finalize time), or an explicit target user ID (2-target case, chosen via the select menu in handleWitchHeal)
	WitchPoisonTarget             string                         // this round's poison pick, "" if not poisoning; reset each round
	WitchActedThisRound           bool                           // reset each round; true once she confirms any choice (heal/poison/skip)
	WolfBonusBiteNextNight        bool                           // armed when a Wolf Cub dies (any cause); consumed at the start of the next night's wolf-vote resolution to select 2 targets instead of 1
	NightBiteTargetIDs            []string                       // this round's resolved wolf-vote target(s) — 1 normally, 2 if the bonus bite fired — set in resolveNight, read by handleWitchHeal to validate a 2-target select-menu pick, reset each round in runNightPhase
	WitchPromptedThisRound        bool                           // reset each round; true once THIS round's bite-outcome DM has been sent — gates against a stale prior-round DM being actionable
	NightActionsClosed            bool                           // reset each round; set true once resolveNight's stage 1 (wolf tally, AFK strikes, DoctorLastPick) has finished — gates wolf/seer/doctor handlers against a late click landing during the Witch's stage-2 window. Does NOT gate Witch or Hunter handlers.
	Votes                         map[string]string              // voterID -> targetID, reset each day
	AFKStrikes                    map[string]int                 // resets to 0 on any successful action, +1 on a miss
	History                       []roundLog                     // one entry per completed round, for the end-of-game recap
	PriorOverwrite                *discordgo.PermissionOverwrite // @everyone's channel overwrite before this game locked it; nil if there was none
	AliveThreadID                 string                         // private thread all players use for day-discussion chat; "" before it's created
	ChannelUnlocked               bool                           // true once unlockChannel has already run for this game (e.g. the alive-thread-creation fallback below) — terminal cleanup paths must check this before calling unlockChannel again
}

// WerewolfCommand handles /werewolf and all its button/select interactions.
type WerewolfCommand struct {
	mu    sync.Mutex
	games map[string]*werewolfGame // keyed by channel ID — one game per channel
}

// NewWerewolfCommand creates a new WerewolfCommand
func NewWerewolfCommand() *WerewolfCommand {
	return &WerewolfCommand{games: make(map[string]*werewolfGame)}
}

// gameIfCurrent returns the game at channelID only if it still exists AND
// matches seq. Callers use this after waking from a sleep to make sure the
// game they were tracking hasn't ended and been replaced by a new one in
// the same channel in the meantime. Must be called with cmd.mu held.
func (cmd *WerewolfCommand) gameIfCurrent(channelID string, seq uint64) *werewolfGame {
	game, ok := cmd.games[channelID]
	if !ok || game.Seq != seq {
		return nil
	}
	return game
}

// respondComponentEphemeralMentioned is respondComponentEphemeral's
// werewolf-local counterpart for responses whose content embeds a <@id>
// mention: it explicitly sets an empty AllowedMentions allow-list so the
// ephemeral response can never trigger a real ping.
func respondComponentEphemeralMentioned(s *discordgo.Session, i *discordgo.InteractionCreate, message string) {
	err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Content:         message,
			Flags:           discordgo.MessageFlagsEphemeral,
			AllowedMentions: &discordgo.MessageAllowedMentions{},
		},
	})
	if err != nil {
		log.Error().Err(err).Msg("werewolf: failed to send ephemeral component response")
	}
}

// interactionUserID returns the acting user's ID whether the interaction
// came from a guild channel (i.Member set) or a DM (i.User set — night
// actions happen in DMs, where i.Member is always nil).
func interactionUserID(i *discordgo.InteractionCreate) string {
	if i.Member != nil && i.Member.User != nil {
		return i.Member.User.ID
	}
	if i.User != nil {
		return i.User.ID
	}
	return ""
}

const (
	werewolfJoinID   = "ww:join:"
	werewolfLeaveID  = "ww:leave:"
	werewolfStartID  = "ww:startgame:"
	werewolfCancelID = "ww:cancel:"
)

func werewolfLobbyContent(game *werewolfGame) string {
	names := make([]string, len(game.JoinOrder))
	for idx, id := range game.JoinOrder {
		names[idx] = nameTag(id)
	}
	joined := "*chưa có ai*"
	if len(names) > 0 {
		joined = strings.Join(names, ", ")
	}
	return fmt.Sprintf(
		"🐺 <@%s> mở bàn **Ma Sói**! Bấm \"Tham gia\" để vào bàn, \"Rời đi\" nếu đổi ý.\nCần %d-%d người. Người mở bàn bấm \"Bắt đầu\" khi đủ người.\n\n**Đã tham gia (%d):** %s",
		game.StarterID, werewolfMinPlayers, werewolfMaxPlayers, len(names), joined,
	)
}

// aliveDeadSummary renders a compact two-line alive/dead roster snapshot,
// appended to phase-transition announcements so players don't have to run
// /werewolf status to check who's still in. Must be called with cmd.mu
// held — it reads game.Players/game.JoinOrder directly.
func aliveDeadSummary(game *werewolfGame) string {
	var alive, dead []string
	for _, id := range game.JoinOrder {
		p, ok := game.Players[id]
		if !ok {
			continue
		}
		if p.Alive {
			alive = append(alive, p.Label)
		} else {
			dead = append(dead, p.Label)
		}
	}
	summary := fmt.Sprintf("🟢 Còn sống: %s", strings.Join(alive, ", "))
	if len(dead) > 0 {
		summary += fmt.Sprintf("\n💀 Đã chết: %s", strings.Join(dead, ", "))
	}
	return summary
}

// announceRoleDecided posts a short, identity-free progress line to the
// main channel the moment a night-role finishes deciding, so players can
// watch night progress without waiting blindly — it never names the
// acting player or reveals their choice.
func announceRoleDecided(s *discordgo.Session, channelID, roleLabel string) {
	if _, err := s.ChannelMessageSendComplex(channelID, &discordgo.MessageSend{
		Content:         fmt.Sprintf("✅ %s đã quyết định xong.", roleLabel),
		AllowedMentions: &discordgo.MessageAllowedMentions{},
	}); err != nil {
		log.Error().Err(err).Str("role", roleLabel).Msg("werewolf: failed to post role-decided notification")
	}
}

// everyoneOverwrite returns the channel's existing @everyone permission
// overwrite, or nil if none exists. A guild's @everyone role ID always
// equals the guild's own ID — that's the Discord convention this relies on.
func everyoneOverwrite(s *discordgo.Session, guildID, channelID string) (*discordgo.PermissionOverwrite, error) {
	channel, err := s.State.Channel(channelID)
	if err != nil || channel == nil {
		channel, err = s.Channel(channelID)
		if err != nil {
			return nil, err
		}
	}
	for _, ow := range channel.PermissionOverwrites {
		if ow.Type == discordgo.PermissionOverwriteTypeRole && ow.ID == guildID {
			owCopy := *ow
			return &owCopy, nil
		}
	}
	return nil, nil
}

// lockChannelToBotOnly denies @everyone SEND_MESSAGES on the game channel,
// preserving whatever else @everyone already allowed/denied there. Returns
// the prior overwrite (nil if none existed) so unlockChannel can restore it
// exactly once the game ends.
func lockChannelToBotOnly(s *discordgo.Session, guildID, channelID string) (*discordgo.PermissionOverwrite, error) {
	prior, err := everyoneOverwrite(s, guildID, channelID)
	if err != nil {
		return nil, err
	}
	deny := int64(discordgo.PermissionSendMessages)
	allow := int64(0)
	if prior != nil {
		allow = prior.Allow &^ deny
		deny = prior.Deny | deny
	}
	if err := s.ChannelPermissionSet(channelID, guildID, discordgo.PermissionOverwriteTypeRole, allow, deny); err != nil {
		return nil, err
	}
	return prior, nil
}

// unlockChannel restores whatever @everyone permission overwrite existed on
// the channel before lockChannelToBotOnly ran. prior == nil means there was
// none, so the overwrite this game added is deleted outright. Best-effort:
// logs on failure rather than propagating an error, since every caller is
// already on a terminal cleanup path with nothing left to roll back to.
func unlockChannel(s *discordgo.Session, guildID, channelID string, prior *discordgo.PermissionOverwrite) {
	var err error
	if prior == nil {
		err = s.ChannelPermissionDelete(channelID, guildID)
	} else {
		err = s.ChannelPermissionSet(channelID, guildID, discordgo.PermissionOverwriteTypeRole, prior.Allow, prior.Deny)
	}
	if err != nil {
		log.Error().Err(err).Str("channel_id", channelID).Msg("werewolf: failed to restore channel permissions")
	}
}

// muteAliveThread denies @everyone SEND_MESSAGES_IN_THREADS on the alive-
// discussion thread, used during night and vote phases so players can only
// actually chat there during day-discuss. No-op if threadID is empty (the
// thread failed to create, or the game already unlocked everything via the
// createAliveDiscussionThread failure fallback).
func muteAliveThread(s *discordgo.Session, guildID, threadID string) {
	if threadID == "" {
		return
	}
	if err := s.ChannelPermissionSet(threadID, guildID, discordgo.PermissionOverwriteTypeRole, 0, discordgo.PermissionSendMessagesInThreads); err != nil {
		log.Error().Err(err).Str("channel_id", threadID).Msg("werewolf: failed to mute alive-discussion thread")
	}
}

// unmuteAliveThread removes the mute overwrite muteAliveThread applies,
// reopening the thread for day-discuss. Deleting the overwrite (rather than
// setting an explicit allow) restores Discord's default thread permission
// behavior, which is what the thread had immediately after creation.
func unmuteAliveThread(s *discordgo.Session, guildID, threadID string) {
	if threadID == "" {
		return
	}
	if err := s.ChannelPermissionDelete(threadID, guildID); err != nil {
		log.Error().Err(err).Str("channel_id", threadID).Msg("werewolf: failed to unmute alive-discussion thread")
	}
}

// createAliveDiscussionThread creates the private thread all players use for
// day-discussion chat, since the main channel is locked to bot-only during
// the game (see lockChannelToBotOnly). Unlike the wolf thread (recreated
// every night), this one is created once, right after roles are assigned,
// and lives for the whole game — membership shrinks as players die (see
// removeFromAliveThread, called everywhere a player's Alive flag flips to
// false).
func (cmd *WerewolfCommand) createAliveDiscussionThread(s *discordgo.Session, channelID string, joinOrder []string) (string, error) {
	thread, err := s.ThreadStartComplex(channelID, &discordgo.ThreadStart{
		Name:                "🏡 Thảo luận Ma Sói",
		Type:                discordgo.ChannelTypeGuildPrivateThread,
		AutoArchiveDuration: 1440,
		Invitable:           false,
	})
	if err != nil {
		return "", err
	}
	for _, id := range joinOrder {
		if err := s.ThreadMemberAdd(thread.ID, id); err != nil {
			log.Error().Err(err).Str("user_id", id).Msg("werewolf: failed to add player to alive-discussion thread")
		}
	}
	return thread.ID, nil
}

// removeFromAliveThread best-effort removes a player from the alive-
// discussion thread the instant they die. Failures are logged, not fatal —
// the game must proceed regardless of Discord API hiccups here.
func removeFromAliveThread(s *discordgo.Session, threadID, userID string) {
	if threadID == "" {
		return
	}
	if err := s.ThreadMemberRemove(threadID, userID); err != nil {
		log.Error().Err(err).Str("user_id", userID).Msg("werewolf: failed to remove player from alive-discussion thread")
	}
}

func werewolfLobbyButtons(channelID string, disabled bool) []discordgo.MessageComponent {
	return []discordgo.MessageComponent{
		discordgo.ActionsRow{
			Components: []discordgo.MessageComponent{
				discordgo.Button{
					Label:    "Tham gia",
					Style:    discordgo.SuccessButton,
					CustomID: werewolfJoinID + channelID,
					Disabled: disabled,
				},
				discordgo.Button{
					Label:    "Rời đi",
					Style:    discordgo.SecondaryButton,
					CustomID: werewolfLeaveID + channelID,
					Disabled: disabled,
				},
				discordgo.Button{
					Label:    "Bắt đầu",
					Style:    discordgo.PrimaryButton,
					CustomID: werewolfStartID + channelID,
					Disabled: disabled,
				},
				discordgo.Button{
					Label:    "Huỷ",
					Style:    discordgo.DangerButton,
					CustomID: werewolfCancelID + channelID,
					Disabled: disabled,
				},
			},
		},
	}
}

// Definition returns the Discord application command definition
func (cmd *WerewolfCommand) Definition() *discordgo.ApplicationCommand {
	return &discordgo.ApplicationCommand{
		Name:        "werewolf",
		Description: "Trò chơi Ma Sói trong kênh này",
		Options: []*discordgo.ApplicationCommandOption{
			{
				Type:        discordgo.ApplicationCommandOptionSubCommand,
				Name:        "start",
				Description: "Mở bàn chơi Ma Sói mới",
				Options: []*discordgo.ApplicationCommandOption{
					{
						Type:        discordgo.ApplicationCommandOptionBoolean,
						Name:        "reveal-on-death",
						Description: "Tiết lộ vai trò khi người chơi chết (mặc định: có)",
						Required:    false,
					},
				},
			},
			{
				Type:        discordgo.ApplicationCommandOptionSubCommand,
				Name:        "status",
				Description: "Xem trạng thái ván Ma Sói hiện tại trong kênh này",
			},
			{
				Type:        discordgo.ApplicationCommandOptionSubCommand,
				Name:        "leave",
				Description: "Rời bàn hoặc rời ván Ma Sói đang chạy trong kênh này",
			},
		},
	}
}

// Handle dispatches /werewolf's subcommands.
func (cmd *WerewolfCommand) Handle(s *discordgo.Session, i *discordgo.InteractionCreate) {
	options := i.ApplicationCommandData().Options
	if len(options) == 0 {
		respondError(s, i, "Thiếu subcommand.")
		return
	}
	switch options[0].Name {
	case "start":
		cmd.handleStart(s, i, options[0].Options)
	case "status":
		cmd.handleStatus(s, i)
	case "leave":
		cmd.handleLeave(s, i)
	default:
		respondError(s, i, "Subcommand không hợp lệ.")
	}
}

func (cmd *WerewolfCommand) handleStart(s *discordgo.Session, i *discordgo.InteractionCreate, subOptions []*discordgo.ApplicationCommandInteractionDataOption) {
	options := parseOptions(subOptions)
	revealOnDeath := true
	if v, ok := options["reveal-on-death"].(bool); ok {
		revealOnDeath = v
	}

	starterID := i.Member.User.ID
	game := &werewolfGame{
		GuildID:       i.GuildID,
		ChannelID:     i.ChannelID,
		StarterID:     starterID,
		Seq:           atomic.AddUint64(&werewolfSeqCounter, 1),
		RevealOnDeath: revealOnDeath,
		Phase:         phaseLobby,
		Players:       make(map[string]*player),
		JoinOrder:     []string{},
		Votes:         make(map[string]string),
		AFKStrikes:    make(map[string]int),
	}

	// Claim the channel's map slot atomically with the existence check —
	// this closes the TOCTOU window where two near-simultaneous
	// /werewolf start calls in the same channel could both pass the
	// existence check, both lock the channel, and race to overwrite each
	// other's game in the map (see final-review Fix 1). Everything below
	// this point can fail and roll back via rollback(), but nothing below
	// this point can lose the race for the slot itself.
	cmd.mu.Lock()
	if _, exists := cmd.games[i.ChannelID]; exists {
		cmd.mu.Unlock()
		respondError(s, i, "Kênh này đang có ván Ma Sói, đợi ván trước kết thúc đã.")
		return
	}
	cmd.games[i.ChannelID] = game
	// game is now reachable from cmd.games, so its fields (JoinOrder in
	// particular) can be mutated by a concurrent handleJoinLeave from this
	// point on — render the lobby content now, still under the lock, rather
	// than reading game unguarded later.
	lobbyContent := werewolfLobbyContent(game)
	cmd.mu.Unlock()

	// rollback releases the claimed channel slot (only if it's still this
	// exact game — nothing else should have touched it, but check by
	// pointer identity to be safe) and, if unlockGame is true, restores the
	// channel's permission overwrite. Used on every failure path below.
	rollback := func(unlockGame bool) {
		cmd.mu.Lock()
		prior := game.PriorOverwrite
		if cur, ok := cmd.games[i.ChannelID]; ok && cur == game {
			delete(cmd.games, i.ChannelID)
		}
		cmd.mu.Unlock()
		if unlockGame {
			unlockChannel(s, i.GuildID, i.ChannelID, prior)
		}
	}

	botPerms, err := s.UserChannelPermissions(s.State.User.ID, i.ChannelID)
	if err != nil || botPerms&discordgo.PermissionManageRoles == 0 {
		rollback(false)
		respondError(s, i, "Bot cần quyền \"Manage Roles\" trong kênh này để khoá chat khi chơi Ma Sói.")
		return
	}

	priorOverwrite, err := lockChannelToBotOnly(s, i.GuildID, i.ChannelID)
	if err != nil {
		log.Error().Err(err).Msg("werewolf: failed to lock channel")
		rollback(false)
		respondError(s, i, "Không thể khoá kênh để bắt đầu ván Ma Sói, thử lại sau.")
		return
	}
	cmd.mu.Lock()
	game.PriorOverwrite = priorOverwrite
	cmd.mu.Unlock()

	err = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Content:         lobbyContent,
			Components:      werewolfLobbyButtons(i.ChannelID, false),
			AllowedMentions: &discordgo.MessageAllowedMentions{},
		},
	})
	if err != nil {
		rollback(true)
		log.Error().Err(err).Msg("werewolf: failed to post lobby")
		return
	}

	msg, err := s.InteractionResponse(i.Interaction)
	if err != nil {
		rollback(true)
		log.Error().Err(err).Msg("werewolf: failed to fetch lobby message")
		return
	}

	cmd.mu.Lock()
	game.MessageID = msg.ID
	cmd.mu.Unlock()
}

func (cmd *WerewolfCommand) handleStatus(s *discordgo.Session, i *discordgo.InteractionCreate) {
	cmd.mu.Lock()
	game, ok := cmd.games[i.ChannelID]
	var content string
	if ok {
		aliveCount := 0
		for _, p := range game.Players {
			if p.Alive {
				aliveCount++
			}
		}
		phaseLabels := map[gamePhase]string{
			phaseLobby:      "Đang đăng ký",
			phasePreflight:  "Đang chia vai trò",
			phaseNight:      "Ban đêm",
			phaseDayDiscuss: "Thảo luận ban ngày",
			phaseDayVote:    "Bỏ phiếu",
		}
		content = fmt.Sprintf("🐺 Ván hiện tại: **%s**, vòng %d, %d người còn sống / %d người chơi.",
			phaseLabels[game.Phase], game.Round, aliveCount, len(game.Players))
	} else {
		content = "Kênh này hiện không có ván Ma Sói nào."
	}
	cmd.mu.Unlock()

	respondSuccess(s, i, content)
}

func (cmd *WerewolfCommand) handleLeave(s *discordgo.Session, i *discordgo.InteractionCreate) {
	userID := i.Member.User.ID

	cmd.mu.Lock()
	game, ok := cmd.games[i.ChannelID]
	if !ok {
		cmd.mu.Unlock()
		respondError(s, i, "Kênh này hiện không có ván Ma Sói nào.")
		return
	}

	if game.Phase == phaseLobby {
		_, inLobby := game.Players[userID]
		if !inLobby {
			cmd.mu.Unlock()
			respondError(s, i, "Bạn không ở trong bàn này.")
			return
		}
		delete(game.Players, userID)
		for idx, id := range game.JoinOrder {
			if id == userID {
				game.JoinOrder = append(game.JoinOrder[:idx], game.JoinOrder[idx+1:]...)
				break
			}
		}
		content := werewolfLobbyContent(game)
		channelID := game.ChannelID
		messageID := game.MessageID
		cmd.mu.Unlock()

		respondSuccess(s, i, "Đã rời bàn.")
		if messageID != "" {
			components := werewolfLobbyButtons(channelID, false)
			if _, err := s.ChannelMessageEditComplex(&discordgo.MessageEdit{
				Channel:         channelID,
				ID:              messageID,
				Content:         &content,
				Components:      &components,
				AllowedMentions: &discordgo.MessageAllowedMentions{},
			}); err != nil {
				log.Error().Err(err).Msg("werewolf: failed to update lobby message after leave")
			}
		}
		return
	}

	if game.Phase == phasePreflight {
		cmd.mu.Unlock()
		respondError(s, i, "Bàn đang được thiết lập, vui lòng chờ ván bắt đầu.")
		return
	}

	p, inGame := game.Players[userID]
	if !inGame || !p.Alive {
		cmd.mu.Unlock()
		respondError(s, i, "Bạn không ở trong ván đang chạy này.")
		return
	}
	p.Alive = false
	leftPlayer := copyPlayer(p)
	hunterRevenged := applyHunterRevengeChain(game, userID)
	applyWolfCubBonusBiteFlag(game, append([]string{userID}, idsOf(hunterRevenged)...))
	if len(hunterRevenged) > 0 && len(game.History) > 0 {
		game.History[len(game.History)-1].HunterRevengeLabel = hunterRevenged[0].Label
	}
	revealOnDeath := game.RevealOnDeath
	channelID := game.ChannelID
	seq := game.Seq
	aliveThreadID := game.AliveThreadID
	cmd.mu.Unlock()

	removeFromAliveThread(s, aliveThreadID, userID)
	for _, hp := range hunterRevenged {
		removeFromAliveThread(s, aliveThreadID, hp.UserID)
	}
	respondSuccess(s, i, "Đã rời ván. Bạn sẽ được coi như đã bị loại.")

	if cmd.checkAndHandleGameEnd(s, channelID, seq) {
		return
	}

	msg := fmt.Sprintf("💨 **%s** đã rời ván.", leftPlayer.Label)
	if revealOnDeath {
		msg = fmt.Sprintf("💨 **%s** đã rời ván. Vai trò: %s.", leftPlayer.Label, leftPlayer.Role.label())
	}
	for _, hp := range hunterRevenged {
		msg += "\n" + hunterRevengeLine(hp)
	}
	if _, err := s.ChannelMessageSendComplex(channelID, &discordgo.MessageSend{
		Content:         msg,
		AllowedMentions: &discordgo.MessageAllowedMentions{},
	}); err != nil {
		log.Error().Err(err).Msg("werewolf: failed to announce voluntary leave")
	}
}

func (cmd *WerewolfCommand) handleJoinLeave(s *discordgo.Session, i *discordgo.InteractionCreate, channelID string, joining bool) {
	cmd.mu.Lock()
	game, ok := cmd.games[channelID]
	if !ok || game.Phase != phaseLobby {
		cmd.mu.Unlock()
		respondComponentEphemeral(s, i, "Bàn này đã đóng đăng ký hoặc kết thúc.")
		return
	}
	clickerID := i.Member.User.ID
	if joining {
		if _, already := game.Players[clickerID]; !already {
			if len(game.JoinOrder) >= werewolfMaxPlayers {
				cmd.mu.Unlock()
				respondComponentEphemeral(s, i, fmt.Sprintf("Bàn đã đủ %d người, không thể tham gia thêm.", werewolfMaxPlayers))
				return
			}
			game.Players[clickerID] = &player{UserID: clickerID, Label: displayLabel(i.Member.User), Alive: true}
			game.JoinOrder = append(game.JoinOrder, clickerID)
		}
	} else {
		if _, already := game.Players[clickerID]; already {
			delete(game.Players, clickerID)
			for idx, id := range game.JoinOrder {
				if id == clickerID {
					game.JoinOrder = append(game.JoinOrder[:idx], game.JoinOrder[idx+1:]...)
					break
				}
			}
		}
	}
	content := werewolfLobbyContent(game)
	cmd.mu.Unlock()

	_ = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseUpdateMessage,
		Data: &discordgo.InteractionResponseData{
			Content:         content,
			Components:      werewolfLobbyButtons(channelID, false),
			AllowedMentions: &discordgo.MessageAllowedMentions{},
		},
	})
}

func (cmd *WerewolfCommand) handleCancelLobby(s *discordgo.Session, i *discordgo.InteractionCreate, channelID string) {
	cmd.mu.Lock()
	game, ok := cmd.games[channelID]
	if !ok {
		cmd.mu.Unlock()
		respondComponentEphemeral(s, i, "Bàn này đã kết thúc rồi.")
		return
	}
	if i.Member.User.ID != game.StarterID && !hasAdminPermission(i.Member) {
		cmd.mu.Unlock()
		respondComponentEphemeral(s, i, "Chỉ người mở bàn hoặc quản trị viên mới được huỷ.")
		return
	}
	// Cancel only fires from the lobby buttons, which are cleared once the
	// game leaves phaseLobby, so WolfThreadID can't be set yet here — but
	// clean it up defensively in case the UI ever races ahead of the phase
	// change, mirroring the cleanup the win/end paths already do.
	threadID := game.WolfThreadID
	aliveThreadID := game.AliveThreadID
	guildID := game.GuildID
	priorOverwrite := game.PriorOverwrite
	delete(cmd.games, channelID)
	cmd.mu.Unlock()

	if threadID != "" {
		if _, err := s.ChannelDelete(threadID); err != nil {
			log.Error().Err(err).Msg("werewolf: failed to delete wolf thread on lobby cancel")
		}
	}
	if aliveThreadID != "" {
		if _, err := s.ChannelDelete(aliveThreadID); err != nil {
			log.Error().Err(err).Msg("werewolf: failed to delete alive-discussion thread on lobby cancel")
		}
	}
	unlockChannel(s, guildID, channelID, priorOverwrite)

	_ = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseUpdateMessage,
		Data: &discordgo.InteractionResponseData{
			Content:    "🚫 Đã huỷ bàn Ma Sói.",
			Components: []discordgo.MessageComponent{},
		},
	})
}

const preflightGracePeriod = 60 * time.Second

func (cmd *WerewolfCommand) handleStartGame(s *discordgo.Session, i *discordgo.InteractionCreate, channelID string) {
	cmd.mu.Lock()
	game, ok := cmd.games[channelID]
	if !ok || game.Phase != phaseLobby {
		cmd.mu.Unlock()
		respondComponentEphemeral(s, i, "Bàn này đã đóng đăng ký hoặc kết thúc.")
		return
	}
	if i.Member.User.ID != game.StarterID {
		cmd.mu.Unlock()
		respondComponentEphemeral(s, i, "Chỉ người mở bàn mới được bắt đầu.")
		return
	}
	if len(game.JoinOrder) < werewolfMinPlayers {
		cmd.mu.Unlock()
		respondComponentEphemeral(s, i, fmt.Sprintf("Cần tối thiểu %d người để bắt đầu.", werewolfMinPlayers))
		return
	}
	joinOrder := make([]string, len(game.JoinOrder))
	copy(joinOrder, game.JoinOrder)
	labels := make(map[string]string, len(game.Players))
	for id, p := range game.Players {
		labels[id] = p.Label
	}
	seq := game.Seq
	game.Phase = phasePreflight
	cmd.mu.Unlock()

	_ = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseUpdateMessage,
		Data: &discordgo.InteractionResponseData{
			Content:    "🐺 Đang chia vai trò và gửi tin nhắn riêng, chờ chút...",
			Components: []discordgo.MessageComponent{},
		},
	})

	go cmd.startPreflight(s, channelID, joinOrder, labels, seq)
}

// dmRoleReveals DMs each listed player their assigned role, returning the
// IDs of anyone the bot couldn't reach (DMs closed/blocked).
// roleInteractionNote explains, per role, when the bot DMs and where that
// role does its talking — appended to the role-reveal DM so players know
// what to expect without reading /help werewolf.
func roleInteractionNote(r role) string {
	switch r {
	case roleWerewolf, roleWolfCub, roleCursedWolf:
		return "Bạn sẽ hành động trong thread riêng của bầy Sói mỗi đêm."
	case roleSeer, roleDoctor, roleWitch, roleHunter, roleDisruptor:
		return "Bot sẽ nhắn tin riêng (DM) cho bạn mỗi đêm để chọn hành động."
	case roleCupid:
		return "Bot sẽ nhắn tin riêng (DM) cho bạn chỉ vào đêm đầu tiên."
	case roleTraitor:
		return "Bot sẽ nhắn tin riêng cho bạn biết ai là Sói khi bắt đầu ván (và mỗi khi danh sách Sói thay đổi) — bạn không có hành động ban đêm."
	default:
		return "Bạn không có hành động riêng — thảo luận cùng mọi người vào ban ngày."
	}
}

func (cmd *WerewolfCommand) dmRoleReveals(s *discordgo.Session, players map[string]*player, ids []string) []string {
	var unreachable []string
	for _, id := range ids {
		p, ok := players[id]
		if !ok {
			continue
		}
		dm, err := s.UserChannelCreate(id)
		if err != nil {
			unreachable = append(unreachable, id)
			continue
		}
		content := fmt.Sprintf(
			"🐺 Ván Ma Sói bắt đầu! Vai trò của bạn là: **%s**.\n%s\n\n💬 Mọi người thảo luận công khai tại thread \"🏡 Thảo luận Ma Sói\" vào ban ngày; kênh chính chỉ dùng để xem thông báo pha (đêm/ngày/bỏ phiếu) và bỏ phiếu.",
			p.Role.label(), roleInteractionNote(p.Role),
		)
		if _, err := s.ChannelMessageSend(dm.ID, content); err != nil {
			unreachable = append(unreachable, id)
		}
	}
	return unreachable
}

// abortGameStillCurrent deletes the game (if it's still the one identified by
// seq) and restores its channel permission lock. Used by startPreflight's
// several abort-before-night-one paths. Returns true if a game was actually
// found and cleaned up.
func (cmd *WerewolfCommand) abortGameStillCurrent(s *discordgo.Session, channelID string, seq uint64) bool {
	var guildID string
	var priorOverwrite *discordgo.PermissionOverwrite
	var found bool
	func() {
		cmd.mu.Lock()
		defer cmd.mu.Unlock()
		game := cmd.gameIfCurrent(channelID, seq)
		if game == nil {
			return
		}
		guildID = game.GuildID
		priorOverwrite = game.PriorOverwrite
		delete(cmd.games, channelID)
		found = true
	}()
	if found {
		unlockChannel(s, guildID, channelID, priorOverwrite)
	}
	return found
}

// startPreflight assigns roles, DMs everyone their role, retries once for
// anyone unreachable (with a public grace-period notice), and drops still-
// unreachable players (recomputing roles for the smaller group) before
// kicking off round 1. Runs in its own goroutine, started from handleStartGame.
func (cmd *WerewolfCommand) startPreflight(s *discordgo.Session, channelID string, joinOrder []string, labels map[string]string, seq uint64) {
	// This goroutine tail-calls the entire rest of the game (night, day
	// discuss, day vote, and back again) — one recover here guards the
	// whole phase chain against a crash taking down the bot process.
	defer func() {
		if r := recover(); r != nil {
			log.Error().
				Interface("panic", r).
				Str("channel_id", channelID).
				Msg("werewolf: recovered from panic in game goroutine")

			var threadID, aliveThreadID, guildID string
			var priorOverwrite *discordgo.PermissionOverwrite
			var cleaned, channelUnlocked bool
			func() {
				cmd.mu.Lock()
				defer cmd.mu.Unlock()
				game := cmd.gameIfCurrent(channelID, seq)
				if game == nil {
					return
				}
				threadID = game.WolfThreadID
				aliveThreadID = game.AliveThreadID
				guildID = game.GuildID
				priorOverwrite = game.PriorOverwrite
				channelUnlocked = game.ChannelUnlocked
				delete(cmd.games, channelID)
				cleaned = true
			}()
			if cleaned {
				if threadID != "" {
					if _, err := s.ChannelDelete(threadID); err != nil {
						log.Error().Err(err).Msg("werewolf: failed to delete wolf thread during panic cleanup")
					}
				}
				if aliveThreadID != "" {
					if _, err := s.ChannelDelete(aliveThreadID); err != nil {
						log.Error().Err(err).Msg("werewolf: failed to delete alive-discussion thread during panic cleanup")
					}
				}
				if !channelUnlocked {
					unlockChannel(s, guildID, channelID, priorOverwrite)
				}
				log.Error().Str("channel_id", channelID).Msg("werewolf: cleaned up game state after panic recovery")
			}
		}
	}()

	sendPlain := func(content string) {
		if _, err := s.ChannelMessageSendComplex(channelID, &discordgo.MessageSend{
			Content:         content,
			AllowedMentions: &discordgo.MessageAllowedMentions{},
		}); err != nil {
			log.Error().Err(err).Msg("werewolf: failed to post preflight message")
		}
	}

	players, err := assignRoles(joinOrder, labels)
	if err != nil {
		sendPlain("❌ Không thể chia vai trò: " + err.Error())
		cmd.abortGameStillCurrent(s, channelID, seq)
		return
	}

	unreachable := cmd.dmRoleReveals(s, players, joinOrder)
	if len(unreachable) > 0 {
		names := make([]string, len(unreachable))
		for idx, id := range unreachable {
			names[idx] = labels[id]
		}
		sendPlain(fmt.Sprintf(
			"⚠️ Không gửi được tin nhắn riêng cho: %s.\nHãy bật DM từ thành viên server (Cài đặt quyền riêng tư) rồi chờ %s để bot thử lại.",
			strings.Join(names, ", "), formatRemaining(preflightGracePeriod),
		))
		time.Sleep(preflightGracePeriod)

		stillCurrent := func() bool {
			cmd.mu.Lock()
			defer cmd.mu.Unlock()
			return cmd.gameIfCurrent(channelID, seq) != nil
		}()
		if !stillCurrent {
			return
		}

		stillUnreachable := cmd.dmRoleReveals(s, players, unreachable)
		if len(stillUnreachable) > 0 {
			for _, id := range stillUnreachable {
				delete(players, id)
			}
			newJoinOrder := make([]string, 0, len(joinOrder))
			for _, id := range joinOrder {
				if _, ok := players[id]; ok {
					newJoinOrder = append(newJoinOrder, id)
				}
			}
			joinOrder = newJoinOrder

			if len(joinOrder) < werewolfMinPlayers {
				sendPlain(fmt.Sprintf("❌ Không đủ %d người sau khi loại người không nhận được tin nhắn riêng, huỷ ván.", werewolfMinPlayers))
				cmd.abortGameStillCurrent(s, channelID, seq)
				return
			}

			reassigned, err := assignRoles(joinOrder, labels)
			if err != nil {
				sendPlain("❌ Không thể chia lại vai trò: " + err.Error())
				cmd.abortGameStillCurrent(s, channelID, seq)
				return
			}
			cmd.dmRoleReveals(s, reassigned, joinOrder)
			players = reassigned
		}
	}

	var initialTraitorDM *wolfRosterDM
	gameFound := func() bool {
		cmd.mu.Lock()
		defer cmd.mu.Unlock()
		game := cmd.gameIfCurrent(channelID, seq)
		if game == nil {
			return false
		}
		game.Players = players
		game.JoinOrder = joinOrder
		initialTraitorDM = traitorRosterDM(game)
		return true
	}()
	if !gameFound {
		return
	}
	cmd.sendTraitorRosterDM(s, initialTraitorDM)

	pingText, allowedMentions := pingOnce(joinOrder)
	roleCounts := "" // best-effort: if this somehow errors, just omit the breakdown rather than failing the announcement
	if ratio, err := rolesForPlayerCount(len(joinOrder)); err == nil {
		villagers := len(joinOrder) - ratio.Werewolves - ratio.Seers - ratio.Doctors - ratio.Witches - ratio.Hunters - ratio.Disruptors
		roleCounts = fmt.Sprintf("\n🐺 Sói: %d · 🔮 Tiên Tri: %d · 💉 Bác Sĩ: %d · 🧙 Phù Thủy: %d · 🏹 Thợ Săn: %d · 🤫 Kẻ Phá Hoại: %d · 👤 Dân Làng: %d",
			ratio.Werewolves, ratio.Seers, ratio.Doctors, ratio.Witches, ratio.Hunters, ratio.Disruptors, villagers)
	}
	if _, err := s.ChannelMessageSendComplex(channelID, &discordgo.MessageSend{
		Content:         fmt.Sprintf("🐺 Vai trò đã được gửi riêng cho từng người! %s%s", pingText, roleCounts),
		AllowedMentions: allowedMentions,
	}); err != nil {
		log.Error().Err(err).Msg("werewolf: failed to post role-assigned announcement")
	}

	aliveThreadID, err := cmd.createAliveDiscussionThread(s, channelID, joinOrder)
	if err != nil {
		log.Error().Err(err).Msg("werewolf: failed to create alive-discussion thread")

		// No clean per-player-DM equivalent exists for a group discussion
		// thread (unlike the wolf thread's dmWolvesIndependently fallback),
		// so instead unlock the channel for the rest of this game — players
		// can at least talk in the now-unlocked main channel rather than
		// being trapped with no discussion venue at all. Mark
		// ChannelUnlocked so every later terminal-cleanup path knows the
		// permission overwrite has already been restored and skips calling
		// unlockChannel a second time.
		var guildID string
		var priorOverwrite *discordgo.PermissionOverwrite
		var stillCurrent bool
		func() {
			cmd.mu.Lock()
			defer cmd.mu.Unlock()
			g := cmd.gameIfCurrent(channelID, seq)
			if g == nil {
				return
			}
			stillCurrent = true
			guildID = g.GuildID
			priorOverwrite = g.PriorOverwrite
			g.ChannelUnlocked = true
		}()
		if stillCurrent {
			unlockChannel(s, guildID, channelID, priorOverwrite)
			if _, err := s.ChannelMessageSendComplex(channelID, &discordgo.MessageSend{
				Content:         "⚠️ Không thể tạo thread thảo luận, kênh sẽ được mở lại để mọi người thảo luận trực tiếp tại đây.",
				AllowedMentions: &discordgo.MessageAllowedMentions{},
			}); err != nil {
				log.Error().Err(err).Msg("werewolf: failed to post alive-thread-fallback notice")
			}
		}
	} else {
		func() {
			cmd.mu.Lock()
			defer cmd.mu.Unlock()
			if g := cmd.gameIfCurrent(channelID, seq); g != nil {
				g.AliveThreadID = aliveThreadID
			}
		}()
	}

	cmd.runNightPhase(s, channelID, seq)
}

// HandleComponent processes every /werewolf button and select-menu click:
// lobby (join/leave/start/cancel), night actions (wolf/seer/doctor), and
// the day vote.
func (cmd *WerewolfCommand) HandleComponent(s *discordgo.Session, i *discordgo.InteractionCreate) {
	customID := i.MessageComponentData().CustomID
	switch {
	case strings.HasPrefix(customID, werewolfJoinID):
		cmd.handleJoinLeave(s, i, strings.TrimPrefix(customID, werewolfJoinID), true)
	case strings.HasPrefix(customID, werewolfLeaveID):
		cmd.handleJoinLeave(s, i, strings.TrimPrefix(customID, werewolfLeaveID), false)
	case strings.HasPrefix(customID, werewolfStartID):
		cmd.handleStartGame(s, i, strings.TrimPrefix(customID, werewolfStartID))
	case strings.HasPrefix(customID, werewolfCancelID):
		cmd.handleCancelLobby(s, i, strings.TrimPrefix(customID, werewolfCancelID))
	case strings.HasPrefix(customID, werewolfWolfTargetID):
		cmd.handleWolfTarget(s, i, strings.TrimPrefix(customID, werewolfWolfTargetID))
	case strings.HasPrefix(customID, werewolfSeerCheckID):
		cmd.handleSeerCheck(s, i, strings.TrimPrefix(customID, werewolfSeerCheckID))
	case strings.HasPrefix(customID, werewolfDoctorProtectID):
		cmd.handleDoctorProtect(s, i, strings.TrimPrefix(customID, werewolfDoctorProtectID))
	case strings.HasPrefix(customID, werewolfHunterTargetID):
		cmd.handleHunterTarget(s, i, strings.TrimPrefix(customID, werewolfHunterTargetID))
	case strings.HasPrefix(customID, werewolfDisruptorTargetID):
		cmd.handleDisruptorTarget(s, i, strings.TrimPrefix(customID, werewolfDisruptorTargetID))
	case strings.HasPrefix(customID, werewolfWitchHealID):
		cmd.handleWitchHeal(s, i, strings.TrimPrefix(customID, werewolfWitchHealID))
	case strings.HasPrefix(customID, werewolfWitchPoisonID):
		cmd.handleWitchPoison(s, i, strings.TrimPrefix(customID, werewolfWitchPoisonID))
	case strings.HasPrefix(customID, werewolfWitchSkipID):
		cmd.handleWitchSkip(s, i, strings.TrimPrefix(customID, werewolfWitchSkipID))
	case strings.HasPrefix(customID, werewolfVoteID):
		cmd.handleVote(s, i, strings.TrimPrefix(customID, werewolfVoteID))
	}
}
