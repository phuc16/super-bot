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
	// DealtRole is the role assigned at deal time and never changed again —
	// unlike Role, which promoteTraitorIfWolvesWiped mutates to
	// roleWerewolf when the Traitor's promotion fires. Recap/reveal
	// rendering reads this (via displayRoleLabel) instead of Role directly,
	// so a promoted Traitor doesn't vanish from the game's own historical
	// record as if they'd been dealt Werewolf all along — see the "Bob was
	// correctly read as innocent by the Seer, then the final reveal calls
	// him a Werewolf with no explanation" class of bug this prevents.
	DealtRole role
}

// displayRoleLabel renders the role to show any time a player's role is
// revealed to the channel — every reveal-on-death line (kill, poison,
// lynch, voluntary leave) as well as the end-of-game reveal/recap: use this
// everywhere p.Role.label() would otherwise be shown to players, never
// p.Role.label() directly. Normally identical to p.Role.label(), but a
// promoted Traitor (DealtRole == roleTraitor, Role since mutated away from
// it) gets an annotated label instead of silently reading as a plain
// Werewolf — including mid-game, where an un-annotated reveal would
// directly contradict an earlier Seer check of that same player.
func displayRoleLabel(p *player) string {
	if p.DealtRole == roleTraitor && p.Role != roleTraitor {
		return fmt.Sprintf("%s (từng là %s)", p.Role.label(), roleTraitor.label())
	}
	return p.Role.label()
}

// finalRoleLabel is displayRoleLabel plus the one annotation that is safe
// ONLY after the game has ended: a Cursed Wolf whose Faction flipped
// mid-game.
//
// The distinction matters. A Cursed Wolf's Role never changes — only its
// Faction does — so displayRoleLabel cannot detect a conversion, and every
// MID-GAME reveal must keep it hidden: the whole point of the merged
// "Bầy Sói không thống nhất được mục tiêu" night wording (see
// finalizeNightResolution) is to give a conversion a permanent cover
// story, and annotating a mid-game death would blow it.
//
// At end of game the opposite is true. Every role is public, and
// winnerNamesWithRoles partitions winners by FACTION — so a converted
// Cursed Wolf gets listed among the wolf winners while the roster prints
// a bare, villager-flavoured "Sói Nguyền", leaving the two halves of the
// same message contradicting each other with nothing to reconcile them.
// Use this at end-of-game reveal sites, displayRoleLabel everywhere else.
func finalRoleLabel(p *player) string {
	if p.DealtRole == roleCursedWolf && p.Faction == factionWolf {
		return displayRoleLabel(p) + " (đã biến thành Sói)"
	}
	return displayRoleLabel(p)
}

// isWolf reports whether p currently counts as wolf-faction for win
// condition and role-check purposes. Prefer this over comparing Role
// directly, since Wolf Cub/Traitor (wolf-faction, non-Werewolf roles) and
// Cursed Wolf (starts villager-faction, flips later) all need Faction-based
// checks, not Role-based ones.
func (p *player) isWolf() bool {
	return p.Faction == factionWolf
}

// investigatesAsWolf reports whether p reads as a werewolf to the Seer's
// night check. This is deliberately NOT the same as isWolf(): per the
// standard Traitor rule, the Traitor is wolf-faction for win-condition
// purposes but passes every standard investigation as an innocent
// villager — only the special conversion below (promoteTraitorIfWolvesWiped)
// ever turns them into something the Seer would flag.
func (p *player) investigatesAsWolf() bool {
	return p.isWolf() && p.Role != roleTraitor
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
			players[id] = &player{UserID: id, Label: labels[id], Role: r, DealtRole: r, Faction: factionForRole(r), Alive: true}
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
		players[id] = &player{UserID: id, Label: labels[id], Role: roleVillager, DealtRole: roleVillager, Faction: factionForRole(roleVillager), Alive: true}
	}
	return players
}

// roleRatioTotal is how many seats a ratio explicitly claims (everything
// dealRoles doesn't claim becomes roleVillager).
func roleRatioTotal(r roleRatio) int {
	return r.Werewolves + r.WolfCubs + r.CursedWolves + r.Traitors +
		r.Seers + r.Doctors + r.Witches + r.Hunters + r.Disruptors + r.Cupids
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
	resultLoversWin
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

// promoteTraitorIfWolvesWiped implements the standard Werewolf "Traitor"
// endgame rule: if every wolf-faction player other than the Traitor has
// died while the Traitor is still alive, the Traitor immediately becomes a
// full Werewolf — gaining the wolf pack's nightly kill vote from then on
// (see wolfVoteParticipant) instead of leaving the wolf side unable to act
// just because its only survivor never had a night action. This never
// changes the win-condition tally itself: the Traitor already counts as
// wolf-faction from deal time (factionForRole), so aliveWolves in
// checkWinCondition already includes them either way. Call with cmd.mu
// held; returns copyPlayer snapshots of every Traitor promoted this call,
// for the caller to DM after releasing the lock (nil if none were).
func promoteTraitorIfWolvesWiped(game *werewolfGame) []*player {
	traitors := alivePlayersOfRole(game, roleTraitor)
	if len(traitors) == 0 {
		return nil
	}
	for _, p := range game.Players {
		if p.Alive && p.isWolf() && p.Role != roleTraitor {
			return nil
		}
	}
	for _, t := range traitors {
		t.Role = roleWerewolf
	}
	return copyPlayers(traitors)
}

// checkWinConditionWithLovers wraps checkWinCondition with the standard
// Cupid "mixed-faction lovers" rule (per Ultimate Werewolf): if Cupid
// paired two players from opposing factions and both are still alive when
// they're the only two players left standing, they win together as their
// own third party instead of either faction winning — overriding the
// normal faction-based result. Lovers from the SAME faction don't need
// this special case: they already win normally whenever their shared
// faction wins, with no extra tracking required.
func checkWinConditionWithLovers(players map[string]*player, loverIDs [2]string) gameResult {
	if loverIDs[0] != "" && loverIDs[1] != "" {
		lover1, ok1 := players[loverIDs[0]]
		lover2, ok2 := players[loverIDs[1]]
		if ok1 && ok2 && lover1.Alive && lover2.Alive && lover1.Faction != lover2.Faction {
			aliveCount := 0
			for _, p := range players {
				if p.Alive {
					aliveCount++
				}
			}
			if aliveCount == 2 {
				return resultLoversWin
			}
		}
	}
	return checkWinCondition(players)
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
	// phaseRoleEdit is entered instead of phasePreflight when the starter
	// chose custom-roles:true — the starter picks role counts via the
	// ephemeral editor (werewolf_rolepick.go) before the game proceeds to
	// the normal preflight (role assignment + DM) flow.
	phaseRoleEdit
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
	HunterRevengeLabels []string // everyone who died from a Hunter's revenge this round. A SLICE, and appended to rather than assigned: a single round's roundLog entry is written by up to four separate death sites (night resolution, day lynch, voluntary leave, AFK sweep), so assigning would let a later site silently erase an earlier one's chained death from the recap
	LoverDeathLabels    []string // everyone who died from the lover-chain this round — same append-not-assign rule as HunterRevengeLabels, for the same reason
	WitchSavedLabel     string   // who the Witch healed this round, "" if she didn't heal
	WitchPoisonedLabel  string   // who the Witch poisoned this round, "" if she didn't poison
	CursedWolfConverted []string // labels of every not-saved Cursed Wolf that converted to wolf-faction this round instead of dying — a slice since Cursed Wolf has no 1-per-game cap, so a bonus-bite round can convert 2+ in the same round
	DisruptorMutedLabel string   // who was muted for the following day-discuss, "" if the Disruptor didn't act (or doesn't exist) this round

	WolfBonusBite        bool     // this round used the Wolf-Cub bonus bite (2+ targets)
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
	CustomRoles                   bool         // set at /werewolf start; true routes "Bắt đầu" into phaseRoleEdit instead of straight to phasePreflight
	PendingRoleCounts             map[role]int // the manual-pick editor's live counts, nil until phaseRoleEdit starts; read once at confirm/timeout, then no longer meaningful
	Phase                         gamePhase
	Round                         int
	Players                       map[string]*player  // userID -> player
	JoinOrder                     []string            // frozen once the game leaves phaseLobby
	WolfThreadID                  string              // the wolf-coordination thread, persists across the whole game once created (see startWolfThread) — "" only before the first night or after the game truly ends
	WolfTargetMessageID           string              // the target-select message in the wolf thread, live-edited as votes come in
	WolfVotes                     map[string][]string // wolfUserID -> target(s) this round, locked once cast — no re-voting. Normally 1 target; during a Wolf-Cub bonus-bite round each wolf may pick up to 1+WolfBonusBiteActiveThisRound targets in one select, each counting as a separate vote toward that target in tallyWolfVotes
	SeerActed                     bool                // whether the seer has acted this round
	SeerTarget                    string              // who the seer checked this round, "" if SeerActed is false
	SeerResult                    role                // that check's result — meaningless if SeerTarget == ""
	DoctorProtect                 string              // doctor's pick this round
	DoctorLastPick                string              // last round's pick — can't repeat back-to-back
	HunterTarget                  string              // persists across rounds until Hunter explicitly changes it — deliberate deviation from canonical Ultimate Werewolf, where the Hunter instead picks reactively, only at the moment they die, with full information at that instant; this project intentionally uses a standing pre-committed pick instead (an earlier design decision, not an oversight — see dmHunterPrompt)
	HunterActedThisRound          bool                // reset each round; true once Hunter has made at least one pick this round — gates the progress announcement to the first pick only
	DisruptorTarget               string              // this round's mute pick, "" if none — reset each night before the new pick, and read again unchanged by the FOLLOWING day-discuss phase transition to decide who to mute
	DisruptorMutedUserID          string              // who currently has a live per-member mute overwrite on the alive thread, "" if nobody. Set by runDayDiscuss only after the overwrite is actually applied, and cleared by runDayVote after removing it. Deliberately NOT re-derived from DisruptorTarget at removal time: the apply and remove gates would then be the same predicate evaluated at two different instants, so a target who died mid-discussion would get the overwrite applied and never removed, orphaning it on the thread forever
	DisruptorActedThisRound       bool                // reset each round; true once the Disruptor has made at least one pick this round — gates the progress announcement to the first pick only, mirroring Hunter's pre-AFK-exemption shape
	LoverIDs                      [2]string           // set once by Cupid on round 1; ["", ""] means no pairing was made
	WitchHealUsed                 bool                // lifetime flag — the heal potion is single-use per game
	WitchPoisonUsed               bool                // lifetime flag — the poison potion is single-use per game
	WitchSaveTarget               string              // this round's heal pick, "" if not saving; reset each round. Two encodings: the literal sentinel "heal" (1-target case, resolved against the lone bite target at finalize time), or an explicit target user ID (2+-target case, chosen via the select menu in handleWitchHeal)
	WitchPoisonTarget             string              // this round's poison pick, "" if not poisoning; reset each round
	WitchActedThisRound           bool                // reset each round; true once her TURN IS OVER — she's skipped, or used a potion and has no remaining choice (see witchHasChoiceLeft). NOT true just because she did something this round — a Witch who uses only one of two available potions leaves this false so she can still take the second action. Gates re-entry into her handlers and waitForWitchAction's completion check.
	WitchDidSomethingThisRound    bool                // reset each round; true the moment she takes ANY action (heal, poison, or skip), even if WitchActedThisRound stays false because she still has a choice left. This is what the AFK-strike check and the one-time announceRoleDecided ping key off — using WitchActedThisRound for either would falsely AFK-strike (and never re-announce) a Witch who legitimately used only one of her two available potions this round.
	WolfBonusBiteNextNight        int                 // extra kill target(s) armed for a FUTURE night's wolf vote, beyond the base 1 — incremented once per Wolf Cub death (any cause) so 2+ Wolf Cubs dying the same round compound correctly. Deliberately NOT read directly by the picker/tally — see WolfBonusBiteActiveThisRound, which snapshots this at night-open so a Wolf Cub death arriving mid-night (e.g. via /werewolf leave) can't retroactively change the bonus for the vote already in progress
	WolfBonusBiteActiveThisRound  int                 // snapshot of WolfBonusBiteNextNight taken once at the start of runNightPhase, then immediately zeroing the live counter — this is what the picker's MaxValues and resolveNight's topN both read for the round, so they always agree with each other and can't be changed by an arming event mid-round; any new arming during the round accrues in WolfBonusBiteNextNight for the round after this one
	// DisruptorSilencedUserID is who the Disruptor silenced for the CURRENT
	// day — barred from both chatting and voting. Distinct from
	// DisruptorMutedUserID, which tracks only whether the Discord permission
	// overwrite actually landed and so must be cleared the moment it is
	// removed: this one has to outlive that removal, because the vote ban
	// still applies during runDayVote, which lifts the overwrite before the
	// ballot even opens. Set in runDayDiscuss, cleared at the start of each
	// night alongside DisruptorTarget.
	DisruptorSilencedUserID string

	WitchHealableTargetIDs []string                       // this round's bite target(s) the Witch could still meaningfully heal: the resolved wolf-vote targets MINUS anyone the Doctor already protected — set in resolveNight, read by handleWitchHeal to validate a pick, reset each round in runNightPhase
	WitchPromptedThisRound bool                           // reset each round; true once THIS round's bite-outcome DM has been sent — gates against a stale prior-round DM being actionable
	NightActionsClosed     bool                           // reset each round; set true once resolveNight's stage 1 (wolf tally, AFK strikes, DoctorLastPick) has finished — gates wolf/seer/doctor/disruptor/hunter handlers against a late click landing during the Witch's stage-2 window. Does NOT gate the Witch's own handlers, which have their own WitchActedThisRound/WitchPromptedThisRound gating for that same window.
	DayVoteClosed          bool                           // reset false at the start of each day-vote round (runDayVote); set true once resolveVote's locked block has tallied and applied the lynch — gates handleVote against a late click landing during resolveVote's post-unlock cleanup/messaging/win-check work, mirroring NightActionsClosed for the day-vote side
	Votes                  map[string]string              // voterID -> targetID, reset each day
	AFKStrikes             map[string]int                 // resets to 0 on any successful action, +1 on a miss
	History                []roundLog                     // one entry per completed round, for the end-of-game recap
	PriorOverwrite         *discordgo.PermissionOverwrite // @everyone's channel overwrite before this game locked it; nil if there was none
	AliveThreadID          string                         // private thread all players use for day-discussion chat; "" before it's created
	ChannelUnlocked        bool                           // true once unlockChannel has already run for this game (e.g. the alive-thread-creation fallback below) — terminal cleanup paths must check this before calling unlockChannel again
	WolfThreadMemberIDs    map[string]bool                // user IDs successfully ThreadMemberAdd'd to the persistent wolf thread. The per-night dead-wolf sweep only considers these: a wolf-FACTION player who was never actually a member (the Traitor, who deliberately never joins; a Cursed Wolf that converted and died before its first wolf night) would otherwise be "removed" from a thread they were never in, every remaining night of the game
	WolfThreadRemovedIDs   map[string]bool                // user IDs already ThreadMemberRemove'd from the persistent wolf thread — startWolfThread's per-night dead-wolf sweep checks this so it doesn't retry (and re-log an error for) a removal that already succeeded on an earlier night
}

// WerewolfCommand handles /werewolf and all its button/select interactions.
type WerewolfCommand struct {
	mu    sync.Mutex
	games map[string]*werewolfGame // keyed by channel ID — one game per channel
}

// WerewolfCmd is the package's single WerewolfCommand instance — a plain
// var instead of a zero-arg NewWerewolfCommand() factory, since it takes no
// configuration (per CLAUDE.md's "no zero-arg factory functions" rule).
var WerewolfCmd = &WerewolfCommand{games: make(map[string]*werewolfGame)}

// canManageLobby reports whether clickerID may use the lobby's restricted
// controls — "Bắt đầu" (start) and "Huỷ" (cancel). Discord renders buttons
// for everyone who can see the message and offers no per-user disabling,
// so this is enforced server-side on click, with an ephemeral rejection
// for anyone else.
//
// Admins are allowed alongside the starter so a lobby whose starter has
// gone AFK, left the server, or simply won't press anything can still be
// started or cleared — otherwise the channel stays locked until a timeout
// with nobody able to intervene. Both controls take the same rule: it
// would be strange for an admin to be able to cancel a lobby but not
// start it.
//
// An empty clickerID (unidentifiable interaction) is always refused, so it
// can never coincidentally match an empty StarterID.
func canManageLobby(clickerID, starterID string, member *discordgo.Member) bool {
	if clickerID == "" {
		return false
	}
	return clickerID == starterID || hasAdminPermission(member)
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
	respondComponentEphemeralWithMentions(s, i, message, &discordgo.MessageAllowedMentions{}, "werewolf: failed to send ephemeral component response")
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
// The alive list renders as <@id> mentions rather than plain labels, so
// living players are visually distinct from dead ones at a glance. Whether
// those mentions actually NOTIFY is the caller's choice, made by what it
// passes as AllowedMentions: the day phases ping (see aliveMentionsFor),
// pulling players out of their DMs and back into the channel, while the
// night announcements render the same highlighted names silently — night
// action happens in DMs, so a notification there would be noise.
//
// Dead players stay as plain labels: mentioning them would ping people who
// are out of the game every time the summary is posted.
func aliveDeadSummary(game *werewolfGame) string {
	var alive, dead []string
	for _, id := range game.JoinOrder {
		p, ok := game.Players[id]
		if !ok {
			continue
		}
		if p.Alive {
			alive = append(alive, nameTag(p.UserID))
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

// aliveMentionsFor returns AllowedMentions scoped to exactly the players
// still alive, authorizing the mentions aliveDeadSummary renders. Scoped to
// Users only — never a role or @everyone — so nobody outside the game is
// notified. Must be called with cmd.mu held.
func aliveMentionsFor(game *werewolfGame) *discordgo.MessageAllowedMentions {
	var ids []string
	for _, id := range game.JoinOrder {
		if p, ok := game.Players[id]; ok && p.Alive {
			ids = append(ids, p.UserID)
		}
	}
	return &discordgo.MessageAllowedMentions{Users: ids}
}

// lastCallWindow is how long before a timed window closes that players get
// a final warning, posted into the room where the conversation is actually
// happening: the alive-players thread by day, the pack's thread at night.
// The main channel's existing halfway reminder is deliberately left alone,
// so the two never fire into the same room.
const lastCallWindow = 30 * time.Second

// postLastCall warns a thread that its window is nearly up. Best-effort and
// silent on failure: this is a courtesy, and a game must never stall or
// abort because a reminder could not be delivered.
func postLastCall(s *discordgo.Session, threadID, text string) {
	if threadID == "" {
		return
	}
	if _, err := s.ChannelMessageSendComplex(threadID, &discordgo.MessageSend{
		Content:         text,
		AllowedMentions: &discordgo.MessageAllowedMentions{},
	}); err != nil {
		log.Error().Err(err).Str("thread_id", threadID).Msg("werewolf: failed to post last-call warning")
	}
}

// sleepWithLastCall waits out window, posting a last-call warning into
// threadID lastCallWindow before the end. A window at or under
// lastCallWindow just sleeps: warning that 30 seconds remain at the moment
// a 30-second window opens is noise, not a warning.
func sleepWithLastCall(s *discordgo.Session, threadID string, window time.Duration, text string) {
	if threadID == "" || window <= lastCallWindow {
		time.Sleep(window)
		return
	}
	time.Sleep(window - lastCallWindow)
	postLastCall(s, threadID, text)
	time.Sleep(lastCallWindow)
}

// nightRoleEmoji is the icon shown next to a role in player-facing text.
// Kept in one place because these were previously inline literals that had
// already drifted — the Doctor appeared as 🩺 in the night-progress line but
// 💉 in /help, for the same role.
func nightRoleEmoji(r role) string {
	switch r {
	case roleWerewolf:
		return "🐺"
	case roleSeer:
		return "🔮"
	case roleDoctor:
		return "💉"
	case roleWitch:
		return "🧙"
	case roleHunter:
		return "🏹"
	case roleDisruptor:
		return "🤫"
	case roleCupid:
		return "💘"
	default:
		return ""
	}
}

// nightActingRoles returns the roles to name in the night-start
// announcement, in a fixed display order. Which roles qualify depends on
// the game's reveal-on-death setting, because that is exactly what decides
// whether "this role is gone" is already public knowledge:
//
//   - reveal-on-death ON: only roles still ALIVE. Every death already
//     announces its role, so the village knows what has been lost — listing
//     five special roles on a night with three players left is simply
//     wrong, and correcting it gives away nothing.
//
//   - reveal-on-death OFF: every role DEALT, alive or not, fixed for the
//     whole game. Here the list is the only signal available, so narrowing
//     it to the living would turn the main channel into a reliable
//     death-reveal and defeat the setting outright.
//
// Roles are identified by DealtRole so a converted Cursed Wolf or promoted
// Traitor doesn't change how the line reads mid-game — conversions are
// private, and neither is announced.
//
// Wolf-pack roles collapse into a single "Ma Sói" entry: Wolf Cub and a
// converted Cursed Wolf act inside the pack's own vote, so naming them
// separately would leak the setup's composition beyond what the pack itself
// reveals. A promoted Traitor counts toward that entry via
// wolfVoteParticipant, so the pack never silently vanishes from the line
// once the last dealt wolf dies. An unpromoted Traitor has no night action
// and is never listed.
//
// Cupid is included only on round 1, the only round it acts. That depends
// solely on the round number, never on who is alive, so it leaks nothing
// under either setting.
//
// Must be called with cmd.mu held.
func nightActingRoles(game *werewolfGame, round int) []role {
	dealt := make(map[role]bool, len(game.Players))
	wolfPack := false
	for _, p := range game.Players {
		if game.RevealOnDeath && !p.Alive {
			continue
		}
		dealt[p.DealtRole] = true
		if p.wolfVoteParticipant() {
			wolfPack = true
		}
	}

	var result []role
	if wolfPack || dealt[roleWerewolf] || dealt[roleWolfCub] || dealt[roleCursedWolf] {
		result = append(result, roleWerewolf)
	}
	for _, r := range []role{roleSeer, roleDoctor, roleWitch, roleHunter, roleDisruptor} {
		if dealt[r] {
			result = append(result, r)
		}
	}
	if dealt[roleCupid] && round == 1 {
		result = append(result, roleCupid)
	}
	return result
}

// nightActingRolesPhrase renders nightActingRoles as the sentence fragment
// the night announcement uses. Falls back to a generic phrase when no
// special role is in play (an all-Villager-plus-wolves game), rather than
// producing a dangling "đang hành động".
func nightActingRolesPhrase(roles []role) string {
	if len(roles) == 0 {
		return "Các vai trò đặc biệt đang hành động trong tin nhắn riêng..."
	}
	names := make([]string, 0, len(roles))
	for _, r := range roles {
		names = append(names, strings.TrimSpace(nightRoleEmoji(r)+" "+r.label()))
	}
	return strings.Join(names, ", ") + " đang hành động trong tin nhắn riêng..."
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

// werewolfRequiredPermission pairs a permission bit the game genuinely
// needs with the human-readable name Discord shows in the role editor, so
// a refusal can name exactly what to grant.
type werewolfRequiredPermission struct {
	Bit  int64
	Name string
	Why  string
}

// werewolfRequiredPermissions is the complete set of channel permissions a
// game exercises, each derived from a specific call the game makes rather
// than guessed — a missing one doesn't degrade gracefully, it breaks a
// mechanic silently mid-game (the bot logs a REST error nobody is reading
// while players wait on a prompt that never arrives). Checked up front at
// /werewolf start and re-checked in startPreflight, because the whole point
// is to never begin a game the bot can't finish.
var werewolfRequiredPermissions = []werewolfRequiredPermission{
	{discordgo.PermissionViewChannel, "View Channel", "đọc kênh"},
	{discordgo.PermissionSendMessages, "Send Messages", "đăng thông báo ngày/đêm"},
	{discordgo.PermissionManageRoles, "Manage Roles", "khoá chat kênh khi chơi và làm câm lặng mục tiêu của Kẻ Phá Hoại"},
	{discordgo.PermissionCreatePrivateThreads, "Create Private Threads", "tạo thread riêng cho cả bàn và cho bầy Sói"},
	{discordgo.PermissionSendMessagesInThreads, "Send Messages in Threads", "gửi phiếu cắn và tóm tắt trong thread"},
	{discordgo.PermissionManageThreads, "Manage Threads", "khoá thread ban đêm/ban ngày, thêm–bớt thành viên và xoá thread khi hết ván"},
}

// missingWerewolfPermissions returns the permissions the bot lacks in
// channelID, in the order werewolfRequiredPermissions declares them. A
// lookup error is reported as everything missing: the game must not start
// on an unverified permission set, since the failure mode of guessing wrong
// is a half-played game that can't be finished or cleaned up.
func missingWerewolfPermissions(s *discordgo.Session, channelID string) ([]werewolfRequiredPermission, error) {
	perms, err := s.UserChannelPermissions(s.State.User.ID, channelID)
	if err != nil {
		return werewolfRequiredPermissions, err
	}
	return missingPermissionsFrom(perms), nil
}

// missingPermissionsFrom is the pure half of missingWerewolfPermissions,
// split out so the bitmask logic is testable without a live Session.
// Administrator short-circuits because Discord treats it as implying every
// other permission, so checking the individual bits would reject a bot that
// can in fact do everything.
func missingPermissionsFrom(perms int64) []werewolfRequiredPermission {
	if perms&discordgo.PermissionAdministrator != 0 {
		return nil
	}
	var missing []werewolfRequiredPermission
	for _, req := range werewolfRequiredPermissions {
		if perms&req.Bit == 0 {
			missing = append(missing, req)
		}
	}
	return missing
}

// missingPermissionsMessage renders the refusal players actually see. It
// lists every missing permission at once rather than the first one, so a
// server admin can fix the role in a single pass instead of rediscovering
// the next gap on each retry.
func missingPermissionsMessage(missing []werewolfRequiredPermission) string {
	lines := make([]string, 0, len(missing))
	for _, req := range missing {
		lines = append(lines, fmt.Sprintf("• **%s** — %s", req.Name, req.Why))
	}
	return "Bot thiếu quyền trong kênh này nên không thể mở ván Ma Sói:\n" +
		strings.Join(lines, "\n") +
		"\n\nNhờ admin cấp đủ các quyền trên cho bot ở kênh này rồi thử lại."
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

// shouldCloseEmptyLobby decides whether a departure just abandoned the
// lobby outright. Shared by the button path (handleJoinLeave) and the slash
// path (handleLeave) so the two can't drift apart on a rule where drifting
// costs a locked channel.
//
// Both conditions matter. An empty roster alone is NOT abandonment:
// /werewolf start doesn't add the starter to the roster, so a brand-new
// lobby legitimately sits at zero players with its host present — closing on
// that alone let one person join-then-leave to kill any lobby whose host
// hadn't joined, repeatedly. And the host leaving alone isn't abandonment
// either; the remaining players get the lobby handed to them instead.
// A host who simply disappears without clicking is lobbyTimeout's job.
func shouldCloseEmptyLobby(leaverID, starterID string, remaining int) bool {
	return remaining == 0 && leaverID != "" && leaverID == starterID
}

// removeDisruptorMute deletes the per-member overwrite runDayDiscuss put on
// the game channel for the Disruptor's target. Called from runDayVote in the
// normal flow, and from teardownGame for a game that ended mid-discussion —
// without that second caller the target stays muted in the channel forever,
// long after the game that muted them is gone.
func removeDisruptorMute(s *discordgo.Session, channelID, userID string) {
	if channelID == "" || userID == "" {
		return
	}
	if err := s.ChannelPermissionDelete(channelID, userID); err != nil {
		log.Error().Err(err).Str("channel_id", channelID).Str("user_id", userID).Msg("werewolf: failed to remove disruptor per-member mute")
	}
}

// setAliveThreadLocked silences or reopens the alive-discussion thread —
// locked through night and vote so players can only actually talk during
// day-discuss, unlocked (and unarchived, in case a long night let Discord
// auto-archive it) when day-discuss opens.
//
// This deliberately uses the thread's `locked` flag rather than a
// permission overwrite. Discord does NOT support per-channel overwrites on
// threads at all: threads inherit permissions from their parent, and
// PUT/DELETE /channels/{id}/permissions is documented "only usable for
// guild channels". The earlier implementation set a
// SEND_MESSAGES_IN_THREADS deny keyed on the thread ID, which the API
// rejected outright every single time — so night silence was a no-op and
// players could keep talking through every night of every game, with only
// a logged REST error to show for it. `locked` is the supported mechanism
// (identical to setWolfThreadLocked) and, like the wolf thread, it blocks
// posting without hiding the thread or its history, so the day's discussion
// stays readable overnight.
//
// No-op if threadID is empty (the thread failed to create, and the game
// fell back to unlocking the main channel for discussion instead).
func setAliveThreadLocked(s *discordgo.Session, threadID string, locked bool) {
	if threadID == "" {
		return
	}
	archived := false
	if _, err := s.ChannelEditComplex(threadID, &discordgo.ChannelEdit{Locked: &locked, Archived: &archived}); err != nil {
		log.Error().Err(err).Str("thread_id", threadID).Bool("locked", locked).Msg("werewolf: failed to set alive-discussion thread lock state")
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

// evictFromWolfThread removes any of userIDs who are current wolf-thread
// members from that thread immediately, and records the successful
// removals so startWolfThread's per-night sweep won't retry them.
//
// Call this from every path that kills someone while the wolf thread may
// still be live (night resolution, voluntary leave). Takes and releases
// cmd.mu itself, so callers must NOT hold it.
func (cmd *WerewolfCommand) evictFromWolfThread(s *discordgo.Session, channelID string, seq uint64, userIDs []string) {
	if len(userIDs) == 0 {
		return
	}
	var threadID string
	var memberIDs, removedIDs map[string]bool
	func() {
		cmd.mu.Lock()
		defer cmd.mu.Unlock()
		g := cmd.gameIfCurrent(channelID, seq)
		if g == nil {
			return
		}
		threadID = g.WolfThreadID
		memberIDs = copyBoolSet(g.WolfThreadMemberIDs)
		removedIDs = copyBoolSet(g.WolfThreadRemovedIDs)
	}()
	if threadID == "" {
		return
	}

	evicted := removeFromWolfThreadNow(s, threadID, memberIDs, removedIDs, userIDs)
	if len(evicted) == 0 {
		return
	}
	cmd.mu.Lock()
	defer cmd.mu.Unlock()
	g := cmd.gameIfCurrent(channelID, seq)
	if g == nil {
		return
	}
	if g.WolfThreadRemovedIDs == nil {
		g.WolfThreadRemovedIDs = make(map[string]bool)
	}
	for _, id := range evicted {
		g.WolfThreadRemovedIDs[id] = true
	}
}

// copyBoolSet snapshots a set-shaped map so it can be read safely after
// cmd.mu is released.
func copyBoolSet(src map[string]bool) map[string]bool {
	if len(src) == 0 {
		return nil
	}
	dst := make(map[string]bool, len(src))
	for k, v := range src {
		dst[k] = v
	}
	return dst
}

// removeFromWolfThreadNow evicts a just-died wolf from the persistent wolf
// thread immediately, rather than waiting for startWolfThread's sweep at
// the top of the NEXT night.
//
// The sweep alone is not enough for a death that happens mid-night: the
// pack's thread is unlocked and its vote tally is being live-edited right
// then, so someone announced as out of the game would otherwise keep
// reading the pack's discussion and watching the current bite being
// decided — and keep that access through the following day too.
//
// Returns the IDs actually removed, for the caller to record in
// WolfThreadRemovedIDs so the next night's sweep doesn't retry them. Only
// attempts users the game believes are current thread members; everyone
// else (villagers, the Traitor, an already-swept wolf) is skipped.
func removeFromWolfThreadNow(s *discordgo.Session, threadID string, memberIDs, removedIDs map[string]bool, userIDs []string) []string {
	if threadID == "" {
		return nil
	}
	var removed []string
	for _, id := range userIDs {
		if !memberIDs[id] || removedIDs[id] {
			continue
		}
		if err := s.ThreadMemberRemove(threadID, id); err != nil {
			log.Error().Err(err).Str("user_id", id).Msg("werewolf: failed to remove departed wolf from wolf thread")
			continue
		}
		removed = append(removed, id)
	}
	return removed
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

// werewolfCancelOnlyButtons is the lobby message's component set during
// phaseRoleEdit: Join/Leave/Start are meaningless once registration closed,
// but Cancel must stay reachable so an AFK starter can't hold the channel
// locked for the whole roleEditTimeout with no way out. Same customID as
// the lobby's own Cancel, so handleCancelLobby handles both.
func werewolfCancelOnlyButtons(channelID string) []discordgo.MessageComponent {
	return []discordgo.MessageComponent{
		discordgo.ActionsRow{
			Components: []discordgo.MessageComponent{
				discordgo.Button{
					Label:    "Huỷ",
					Style:    discordgo.DangerButton,
					CustomID: werewolfCancelID + channelID,
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
		// Guild-only: every subcommand needs a real guild channel (thread
		// creation, @everyone permission overwrites, member lookups), and
		// in a DM context Discord sends no i.Member at all. Restricting it
		// here stops the command from even appearing in DMs; the handlers
		// still guard i.Member defensively via interactionUserID, since a
		// Definition change only takes effect once commands re-register.
		Contexts: &[]discordgo.InteractionContextType{discordgo.InteractionContextGuild},
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
					{
						Type:        discordgo.ApplicationCommandOptionBoolean,
						Name:        "custom-roles",
						Description: "Tự chọn số lượng từng vai trò thay vì dùng bảng mặc định (mặc định: không)",
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
			{
				Type:        discordgo.ApplicationCommandOptionSubCommand,
				Name:        "force-stop",
				Description: "[Quản trị viên] Buộc dừng ván Ma Sói trong kênh này, ở bất kỳ giai đoạn nào",
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
	case "force-stop":
		cmd.handleForceStop(s, i)
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
	customRoles := false
	if v, ok := options["custom-roles"].(bool); ok {
		customRoles = v
	}

	// interactionUserID rather than i.Member.User.ID: the Definition marks
	// this command guild-only, but that only takes effect once commands
	// re-register, and an unguarded dereference here would panic the
	// handler goroutine on a DM-context invocation rather than failing the
	// one interaction.
	starterID := interactionUserID(i)
	if starterID == "" {
		respondError(s, i, "Không xác định được người dùng, thử lại nhé.")
		return
	}
	game := &werewolfGame{
		GuildID:              i.GuildID,
		ChannelID:            i.ChannelID,
		StarterID:            starterID,
		Seq:                  atomic.AddUint64(&werewolfSeqCounter, 1),
		RevealOnDeath:        revealOnDeath,
		CustomRoles:          customRoles,
		Phase:                phaseLobby,
		Players:              make(map[string]*player),
		JoinOrder:            []string{},
		Votes:                make(map[string]string),
		AFKStrikes:           make(map[string]int),
		WolfThreadMemberIDs:  make(map[string]bool),
		WolfThreadRemovedIDs: make(map[string]bool),
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

	missingPerms, err := missingWerewolfPermissions(s, i.ChannelID)
	if err != nil {
		log.Error().Err(err).Str("channel_id", i.ChannelID).Msg("werewolf: failed to resolve bot channel permissions")
	}
	if len(missingPerms) > 0 {
		rollback(false)
		respondError(s, i, missingPermissionsMessage(missingPerms))
		return
	}

	priorOverwrite, err := lockChannelToBotOnly(s, i.GuildID, i.ChannelID)
	if err != nil {
		log.Error().Err(err).Msg("werewolf: failed to lock channel")
		rollback(false)
		respondError(s, i, "Không thể khoá kênh để bắt đầu ván Ma Sói, thử lại sau.")
		return
	}
	stillCurrent := false
	func() {
		cmd.mu.Lock()
		defer cmd.mu.Unlock()
		if cur, ok := cmd.games[i.ChannelID]; ok && cur == game {
			game.PriorOverwrite = priorOverwrite
			stillCurrent = true
		}
	}()
	if !stillCurrent {
		// Same race the wolf/alive-thread fixes guard against, applied to
		// the channel-lock permission overwrite instead of a thread: the
		// game was force-stopped/cancelled/ended in the window between
		// lockChannelToBotOnly's REST call landing and re-acquiring the
		// lock to record its result. Nobody else knows the correct
		// priorOverwrite (it was never written to game state), so restore
		// it here immediately — otherwise the channel is left permanently
		// denied SEND_MESSAGES with no game in the map to undo it, and the
		// next /werewolf start in this channel would capture that leaked
		// deny as ITS OWN "prior" state.
		unlockChannel(s, i.GuildID, i.ChannelID, priorOverwrite)
		respondError(s, i, "Ván này đã bị huỷ trước khi kịp bắt đầu, thử lại nhé.")
		return
	}

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

	stillCurrent = false
	func() {
		cmd.mu.Lock()
		defer cmd.mu.Unlock()
		// Guard by identity, same as the PriorOverwrite write above: if the
		// game was force-stopped/cancelled in this window, force-stop
		// already correctly unlocked the channel (PriorOverwrite was
		// recorded before this point), so there's nothing left to undo —
		// just skip writing into a game struct nobody will read again.
		if cur, ok := cmd.games[i.ChannelID]; ok && cur == game {
			game.MessageID = msg.ID
			stillCurrent = true
		}
	}()
	if stillCurrent {
		go cmd.waitForLobbyExpiry(s, i.ChannelID, game.Seq)
	}
}

// lobbyTimeout is how long a lobby may sit in phaseLobby before the bot
// closes it on its own. /werewolf start denies @everyone SEND_MESSAGES for
// the duration, so an abandoned lobby holds the channel read-only — and
// the starter is not auto-added to the roster, so "everyone left" cleanup
// alone doesn't cover a starter who opens a lobby, never joins, and goes
// offline. This is the backstop for every abandonment shape.
const lobbyTimeout = 15 * time.Minute

// waitForLobbyExpiry closes a lobby that nobody ever started, restoring the
// channel's permissions. Mirrors waitForRoleCountConfirm's watchdog shape:
// its own goroutine, its own recover, and every wake re-checks
// gameIfCurrent(channelID, seq) so it can never act on a game that has
// since ended and been replaced by a different one in the same channel.
func (cmd *WerewolfCommand) waitForLobbyExpiry(s *discordgo.Session, channelID string, seq uint64) {
	defer func() {
		if r := recover(); r != nil {
			log.Error().
				Interface("panic", r).
				Str("channel_id", channelID).
				Msg("werewolf: recovered from panic in lobby-expiry goroutine")
		}
	}()

	deadline := time.Now().Add(lobbyTimeout)
	for {
		var stillWaiting bool
		func() {
			cmd.mu.Lock()
			defer cmd.mu.Unlock()
			g := cmd.gameIfCurrent(channelID, seq)
			stillWaiting = g != nil && g.Phase == phaseLobby
		}()
		// Left phaseLobby (started), or the game is gone (cancelled,
		// force-stopped, emptied) — either way this watchdog is done.
		if !stillWaiting {
			return
		}
		if time.Now().After(deadline) {
			break
		}
		time.Sleep(15 * time.Second)
	}

	var info gameTeardownInfo
	var messageID string
	var expired bool
	func() {
		cmd.mu.Lock()
		defer cmd.mu.Unlock()
		g := cmd.gameIfCurrent(channelID, seq)
		// Re-check the phase under the lock: the starter may have pressed
		// "Bắt đầu" in the gap between the last poll and now.
		if g == nil || g.Phase != phaseLobby {
			return
		}
		info = gameTeardownInfoFor(channelID, g)
		messageID = g.MessageID
		delete(cmd.games, channelID)
		expired = true
	}()
	if !expired {
		return
	}

	teardownGame(s, info)
	if messageID != "" {
		content := fmt.Sprintf("⌛ Bàn Ma Sói đã tự đóng vì không bắt đầu trong %s. Kênh đã được mở khoá.", formatRemaining(lobbyTimeout))
		components := []discordgo.MessageComponent{}
		if _, err := s.ChannelMessageEditComplex(&discordgo.MessageEdit{
			Channel:         channelID,
			ID:              messageID,
			Content:         &content,
			Components:      &components,
			AllowedMentions: &discordgo.MessageAllowedMentions{},
		}); err != nil {
			log.Error().Err(err).Msg("werewolf: failed to close expired lobby message")
		}
	}
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
			phaseRoleEdit:   "Đang chọn vai trò tuỳ chỉnh",
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
	// See handleStart: nil-safe lookup so a DM-context invocation fails the
	// interaction instead of panicking the handler goroutine.
	userID := interactionUserID(i)
	if userID == "" {
		respondError(s, i, "Không xác định được người dùng, thử lại nhé.")
		return
	}

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

		channelID := game.ChannelID
		messageID := game.MessageID

		// A lobby the HOST has walked out of, with nobody left in it, must
		// be torn down rather than left sitting: the channel is locked to
		// bot-only from /werewolf start onward, so an abandoned lobby would
		// hold it read-only with nobody around to cancel it.
		//
		// Gated on the leaver being the host for the same reason as the
		// button path (see handleJoinLeave): the starter isn't in the roster
		// unless they joined, so an empty roster alone doesn't mean the
		// lobby is abandoned. lobbyTimeout covers the case where the host is
		// simply gone without clicking anything.
		if shouldCloseEmptyLobby(userID, game.StarterID, len(game.JoinOrder)) {
			info := gameTeardownInfoFor(channelID, game)
			delete(cmd.games, channelID)
			cmd.mu.Unlock()

			// Respond before teardownGame's REST work, so a slow unlock
			// can't blow the 3-second interaction deadline (see the same
			// note in handleJoinLeave).
			respondSuccess(s, i, "Đã rời bàn. Bàn trống nên đã được đóng lại.")
			teardownGame(s, info)
			if messageID != "" {
				emptyComponents := []discordgo.MessageComponent{}
				emptyContent := "🚫 Bàn Ma Sói đã đóng vì người mở bàn đã rời đi và không còn ai tham gia."
				if _, err := s.ChannelMessageEditComplex(&discordgo.MessageEdit{
					Channel:         channelID,
					ID:              messageID,
					Content:         &emptyContent,
					Components:      &emptyComponents,
					AllowedMentions: &discordgo.MessageAllowedMentions{},
				}); err != nil {
					log.Error().Err(err).Msg("werewolf: failed to close empty lobby message")
				}
			}
			return
		}

		// If the STARTER left, hand the lobby to whoever joined earliest.
		// Otherwise they'd keep exclusive authority over a lobby they're
		// no longer in — able to start a game for other people or cancel
		// it — and if they went offline, nobody but an admin could ever
		// start or clear it.
		starterLeft := userID == game.StarterID
		if starterLeft {
			game.StarterID = game.JoinOrder[0]
		}
		newStarterID := game.StarterID
		content := werewolfLobbyContent(game)
		cmd.mu.Unlock()

		if starterLeft {
			respondSuccess(s, i, "Đã rời bàn. Quyền mở bàn đã chuyển cho người tham gia sớm nhất.")
		} else {
			respondSuccess(s, i, "Đã rời bàn.")
		}
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
		if starterLeft {
			if _, err := s.ChannelMessageSendComplex(channelID, &discordgo.MessageSend{
				Content:         fmt.Sprintf("👑 Người mở bàn đã rời đi — <@%s> giờ là người mở bàn và có thể bấm \"Bắt đầu\" hoặc \"Huỷ\".", newStarterID),
				AllowedMentions: &discordgo.MessageAllowedMentions{Users: []string{newStarterID}},
			}); err != nil {
				log.Error().Err(err).Msg("werewolf: failed to announce lobby host handover")
			}
		}
		return
	}

	if game.Phase == phasePreflight || game.Phase == phaseRoleEdit {
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
	chained := applyDeathChains(game, userID)
	hunterRevenged, loverDeaths := splitChainedDeaths(chained)
	applyWolfCubBonusBiteFlag(game, append(append([]string{userID}, idsOf(hunterRevenged)...), idsOf(loverDeaths)...))
	if n := len(game.History); n > 0 {
		if len(hunterRevenged) > 0 {
			game.History[n-1].HunterRevengeLabels = append(game.History[n-1].HunterRevengeLabels, labelsOf(hunterRevenged)...)
		}
		if len(loverDeaths) > 0 {
			game.History[n-1].LoverDeathLabels = append(game.History[n-1].LoverDeathLabels, labelsOf(loverDeaths)...)
		}
	}
	revealOnDeath := game.RevealOnDeath
	channelID := game.ChannelID
	seq := game.Seq
	aliveThreadID := game.AliveThreadID
	deadNow := append(append([]string{userID}, idsOf(hunterRevenged)...), idsOf(loverDeaths)...)
	cmd.mu.Unlock()

	removeFromAliveThread(s, aliveThreadID, userID)
	for _, hp := range hunterRevenged {
		removeFromAliveThread(s, aliveThreadID, hp.UserID)
	}
	for _, lp := range loverDeaths {
		removeFromAliveThread(s, aliveThreadID, lp.UserID)
	}
	// Evict from the wolf thread NOW rather than leaving it to
	// startWolfThread's sweep at the top of the next night: mid-night the
	// pack's thread is unlocked and its vote tally is being live-edited,
	// so a wolf who just left would otherwise sit there watching the
	// current bite get decided, and keep that access all through the
	// following day.
	cmd.evictFromWolfThread(s, channelID, seq, deadNow)
	respondSuccess(s, i, "Đã rời ván. Bạn sẽ được coi như đã bị loại.")

	if cmd.checkAndHandleGameEnd(s, channelID, seq) {
		return
	}

	msg := fmt.Sprintf("💨 **%s** đã rời ván.", leftPlayer.Label)
	if revealOnDeath {
		msg = fmt.Sprintf("💨 **%s** đã rời ván. Vai trò: %s.", leftPlayer.Label, displayRoleLabel(leftPlayer))
	}
	for _, hp := range hunterRevenged {
		msg += "\n" + hunterRevengeLine(hp, revealOnDeath)
	}
	for _, lp := range loverDeaths {
		msg += "\n" + loverDeathLine(lp, revealOnDeath)
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
	clickerID := interactionUserID(i)
	if clickerID == "" || i.Member == nil || i.Member.User == nil {
		cmd.mu.Unlock()
		respondComponentEphemeral(s, i, "Không xác định được người dùng, thử lại nhé.")
		return
	}
	// actuallyLeft gates the teardown and host-handover below. It must be
	// "this click removed a player who was really in the lobby", NOT just
	// "this was a leave click": the starter is NOT auto-added to the
	// roster by /werewolf start, so a brand-new lobby sits at zero
	// players — and treating any leave click as a departure would let ANY
	// passer-by close a fresh lobby (or hand its host role away) without
	// ever having joined it.
	actuallyLeft := false
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
			actuallyLeft = true
		}
	}

	// Same two rules the /werewolf leave path applies — this is just the
	// button-driven way to leave the same lobby, and the consequences of
	// getting it wrong (a locked channel nobody can free, or a departed
	// starter still holding lobby authority) are identical.
	//
	// The teardown additionally requires that the LEAVER was the host.
	// /werewolf start deliberately doesn't add the starter to the roster, so
	// a fresh lobby sits at zero players — meaning "roster is now empty"
	// alone doesn't imply "nobody is here". Without the host check, one
	// person joining and immediately leaving would close a lobby whose host
	// is present and waiting, and could do it again to every lobby opened
	// after it. An empty lobby whose host is still around stays open;
	// genuine abandonment is what lobbyTimeout is for.
	if actuallyLeft && shouldCloseEmptyLobby(clickerID, game.StarterID, len(game.JoinOrder)) {
		info := gameTeardownInfoFor(channelID, game)
		delete(cmd.games, channelID)
		cmd.mu.Unlock()

		// Answer the click BEFORE teardownGame's REST work. Discord drops an
		// interaction that isn't acknowledged within 3 seconds, and
		// teardownGame leads with a permission-overwrite round-trip — so a
		// slow or rate-limited unlock would show the clicker "This
		// interaction failed" and leave the dead lobby's buttons on screen,
		// even though the game is already gone from cmd.games.
		_ = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseUpdateMessage,
			Data: &discordgo.InteractionResponseData{
				Content:         "🚫 Bàn Ma Sói đã đóng vì người mở bàn đã rời đi và không còn ai tham gia.",
				Components:      []discordgo.MessageComponent{},
				AllowedMentions: &discordgo.MessageAllowedMentions{},
			},
		})
		teardownGame(s, info)
		return
	}
	starterLeft := actuallyLeft && clickerID == game.StarterID
	if starterLeft {
		game.StarterID = game.JoinOrder[0]
	}
	newStarterID := game.StarterID
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
	if starterLeft {
		if _, err := s.ChannelMessageSendComplex(channelID, &discordgo.MessageSend{
			Content:         fmt.Sprintf("👑 Người mở bàn đã rời đi — <@%s> giờ là người mở bàn và có thể bấm \"Bắt đầu\" hoặc \"Huỷ\".", newStarterID),
			AllowedMentions: &discordgo.MessageAllowedMentions{Users: []string{newStarterID}},
		}); err != nil {
			log.Error().Err(err).Msg("werewolf: failed to announce lobby host handover")
		}
	}
}

func (cmd *WerewolfCommand) handleCancelLobby(s *discordgo.Session, i *discordgo.InteractionCreate, channelID string) {
	cmd.mu.Lock()
	game, ok := cmd.games[channelID]
	if !ok {
		cmd.mu.Unlock()
		respondComponentEphemeral(s, i, "Bàn này đã kết thúc rồi.")
		return
	}
	// Cancel stays available through phaseRoleEdit: handleStartGame's
	// custom-roles branch keeps this button on the lobby message precisely
	// so an AFK starter can't lock the channel for the full roleEditTimeout
	// with no escape. Any later phase means the game is genuinely running
	// and must be left to /werewolf leave and the normal end paths.
	if game.Phase != phaseLobby && game.Phase != phaseRoleEdit {
		cmd.mu.Unlock()
		respondComponentEphemeral(s, i, "Ván đã bắt đầu, không thể huỷ.")
		return
	}
	if !canManageLobby(interactionUserID(i), game.StarterID, i.Member) {
		cmd.mu.Unlock()
		respondComponentEphemeral(s, i, "Chỉ người mở bàn hoặc quản trị viên mới được huỷ.")
		return
	}
	// Cancel only fires from the lobby buttons, which are cleared once the
	// game leaves phaseLobby/phaseRoleEdit, so WolfThreadID can't be set yet
	// here — but clean it up defensively in case the UI ever races ahead of
	// the phase change, mirroring the cleanup the win/end paths already do.
	// Routed through the shared teardownGame (rather than open-coding the
	// same steps) so this path can't drift from the others again, and so it
	// inherits the unlock-before-delete ordering and the ChannelUnlocked
	// check — the latter is always false at this phase today, but reading
	// it via gameTeardownInfoFor keeps that an observation rather than an
	// assumption baked into this call site.
	info := gameTeardownInfoFor(channelID, game)
	delete(cmd.games, channelID)
	cmd.mu.Unlock()

	teardownGame(s, info)

	_ = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseUpdateMessage,
		Data: &discordgo.InteractionResponseData{
			Content:    "🚫 Đã huỷ bàn Ma Sói.",
			Components: []discordgo.MessageComponent{},
		},
	})
}

// handleForceStop lets an admin (Administrator or Manage Server permission)
// tear down whatever game is running in this channel, in ANY phase — unlike
// handleCancelLobby, which deliberately only works during
// phaseLobby/phaseRoleEdit and leaves a genuinely-running game to
// /werewolf leave and the normal end paths. This is the escape hatch for
// when a game needs to be aborted outright (stuck player, wrong setup,
// channel needs freeing) regardless of how far it's progressed.
//
// Deleting the game from cmd.games is sufficient on its own to stop every
// in-flight goroutine: the whole night/day phase chain already checks
// cmd.gameIfCurrent(channelID, seq) before acting and quietly returns once
// that lookup fails, exactly as it does when a game ends normally.
// gameTeardownInfo captures everything needed to clean up a game's Discord
// side-effects (threads, channel lock) after it's already been removed
// from cmd.games. Shared by handleForceStop (one game, admin-triggered)
// and Shutdown (every game, process-exit-triggered) so both paths clean up
// identically instead of duplicating the same three steps.
type gameTeardownInfo struct {
	channelID            string
	threadID             string
	aliveThreadID        string
	guildID              string
	priorOverwrite       *discordgo.PermissionOverwrite
	channelUnlocked      bool
	disruptorMutedUserID string
}

func gameTeardownInfoFor(channelID string, game *werewolfGame) gameTeardownInfo {
	return gameTeardownInfo{
		channelID:            channelID,
		threadID:             game.WolfThreadID,
		aliveThreadID:        game.AliveThreadID,
		guildID:              game.GuildID,
		priorOverwrite:       game.PriorOverwrite,
		channelUnlocked:      game.ChannelUnlocked,
		disruptorMutedUserID: game.DisruptorMutedUserID,
	}
}

func teardownGame(s *discordgo.Session, info gameTeardownInfo) {
	// Unlock FIRST, before touching either thread: the channel lock is the
	// one side effect that's actually harmful to leave behind (denies
	// @everyone SEND_MESSAGES with no game left to fix it, and compounds
	// via the next game's PriorOverwrite capture), whereas an orphaned
	// thread is merely untidy. If the process is killed partway through
	// teardown (e.g. Shutdown's grace period expiring mid-loop), this
	// ordering guarantees the important step already landed.
	if !info.channelUnlocked {
		unlockChannel(s, info.guildID, info.channelID, info.priorOverwrite)
	}
	// Second in priority, and for the same reason: a leaked per-member deny
	// outlives the game just as the channel lock would. It targets a
	// different overwrite ID than unlockChannel's @everyone one, so the two
	// don't interfere.
	removeDisruptorMute(s, info.channelID, info.disruptorMutedUserID)
	if info.threadID != "" {
		if _, err := s.ChannelDelete(info.threadID); err != nil {
			log.Error().Err(err).Msg("werewolf: failed to delete wolf thread during teardown")
		}
	}
	if info.aliveThreadID != "" {
		if _, err := s.ChannelDelete(info.aliveThreadID); err != nil {
			log.Error().Err(err).Msg("werewolf: failed to delete alive-discussion thread during teardown")
		}
	}
}

// Shutdown tears down every in-flight game's Discord side-effects (wolf/
// alive-discussion threads, channel lock) before the process exits.
// Without this, a game mid-flight when the bot is redeployed (e.g.
// `docker exec all-in-one supervisorctl restart <service>`) leaves its
// channel permanently denied SEND_MESSAGES for @everyone with no game left
// in memory to fix it via /werewolf force-stop — and the next
// /werewolf start in that channel would capture the leaked deny as ITS OWN
// "prior" state, making the lock self-perpetuating across every future
// game in that channel. Safe to call with zero games running (no-op).
func (cmd *WerewolfCommand) Shutdown(s *discordgo.Session) {
	cmd.mu.Lock()
	infos := make([]gameTeardownInfo, 0, len(cmd.games))
	for channelID, game := range cmd.games {
		infos = append(infos, gameTeardownInfoFor(channelID, game))
	}
	cmd.games = make(map[string]*werewolfGame)
	cmd.mu.Unlock()

	// Fan every game's teardown out concurrently rather than looping
	// serially: each game costs ~4 sequential REST calls, and a SIGTERM
	// grace period (commonly ~10s) is a hard wall a large enough batch of
	// games could exceed one at a time, risking a SIGKILL mid-loop that
	// never reaches the later games' unlockChannel at all. teardownGame's
	// own unlock-before-delete ordering (see its comment) means even a
	// goroutine that gets killed mid-flight has already done the one step
	// that matters for whichever game it was handling.
	var wg sync.WaitGroup
	for _, info := range infos {
		wg.Add(1)
		go func(info gameTeardownInfo) {
			defer wg.Done()
			teardownGame(s, info)
			if _, err := s.ChannelMessageSendComplex(info.channelID, &discordgo.MessageSend{
				Content:         "🛑 Bot khởi động lại — ván Ma Sói đang chạy trong kênh này đã bị huỷ, kênh đã được mở khoá.",
				AllowedMentions: &discordgo.MessageAllowedMentions{},
			}); err != nil {
				log.Error().Err(err).Str("channel_id", info.channelID).Msg("werewolf: failed to announce shutdown teardown")
			}
		}(info)
	}
	wg.Wait()
}

func (cmd *WerewolfCommand) handleForceStop(s *discordgo.Session, i *discordgo.InteractionCreate) {
	if !hasAdminPermission(i.Member) {
		respondError(s, i, "❌ Chỉ quản trị viên (Administrator hoặc Manage Server) mới được dùng lệnh này.")
		return
	}

	channelID := i.ChannelID
	cmd.mu.Lock()
	game, ok := cmd.games[channelID]
	if !ok {
		cmd.mu.Unlock()
		respondError(s, i, "Không có ván Ma Sói nào đang chạy trong kênh này.")
		return
	}
	info := gameTeardownInfoFor(channelID, game)
	delete(cmd.games, channelID)
	cmd.mu.Unlock()

	teardownGame(s, info)

	if err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Content:         fmt.Sprintf("🛑 Quản trị viên <@%s> đã buộc dừng ván Ma Sói trong kênh này.", interactionUserID(i)),
			AllowedMentions: &discordgo.MessageAllowedMentions{},
		},
	}); err != nil {
		log.Error().Err(err).Msg("werewolf: failed to respond to force-stop")
	}
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
	if !canManageLobby(interactionUserID(i), game.StarterID, i.Member) {
		cmd.mu.Unlock()
		respondComponentEphemeral(s, i, "Chỉ người mở bàn hoặc quản trị viên mới được bắt đầu.")
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
	customRoles := game.CustomRoles
	playerCount := len(joinOrder)
	if customRoles {
		game.Phase = phaseRoleEdit
		game.PendingRoleCounts = make(map[role]int)
	} else {
		game.Phase = phasePreflight
	}
	cmd.mu.Unlock()

	if !customRoles {
		_ = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseUpdateMessage,
			Data: &discordgo.InteractionResponseData{
				Content:    "🐺 Đang chia vai trò và gửi tin nhắn riêng, chờ chút...",
				Components: []discordgo.MessageComponent{},
			},
		})
		go cmd.startPreflight(s, channelID, joinOrder, labels, seq, nil)
		return
	}

	_ = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseUpdateMessage,
		Data: &discordgo.InteractionResponseData{
			Content:    "🐺 Người mở bàn đang chọn vai trò tuỳ chỉnh...",
			Components: werewolfCancelOnlyButtons(channelID),
		},
	})
	cmd.sendRoleCountEditor(s, i.Interaction, channelID, playerCount, seq)
}

// dmRoleReveals DMs each listed player their assigned role, returning the
// IDs of anyone the bot couldn't reach (DMs closed/blocked).
// roleInteractionNote explains, per role, when the bot DMs and where that
// role does its talking — appended to the role-reveal DM so players know
// what to expect without reading /help werewolf.
func roleInteractionNote(r role) string {
	switch r {
	case roleWerewolf, roleWolfCub:
		return "Bạn sẽ hành động trong thread riêng của bầy Sói mỗi đêm."
	case roleCursedWolf:
		return "Bạn hiện là Dân thường bình thường — nếu bị Sói cắn mà sống sót, bạn sẽ biến thành Sói Nguyền và vào thread riêng của bầy Sói từ đêm sau."
	case roleSeer, roleDoctor, roleWitch, roleHunter, roleDisruptor:
		return "Bot sẽ nhắn tin riêng (DM) cho bạn mỗi đêm để chọn hành động."
	case roleCupid:
		return "Bot sẽ nhắn tin riêng (DM) cho bạn chỉ vào đêm đầu tiên."
	case roleTraitor:
		return "Bot sẽ nhắn tin riêng cho bạn biết ai là Sói khi bắt đầu ván, và lại mỗi đêm — bạn không có hành động ban đêm."
	default:
		return "Bạn không có hành động riêng — thảo luận cùng mọi người vào ban ngày."
	}
}

// roleWinCondition returns the win-condition explanation shown in the
// role-reveal DM, so every player learns not just how their role acts but
// what actually needs to happen for them to win — this is spelled out in
// full for the whole game in /help werewolf, but a player shouldn't have
// to go re-read that separately just to know their own win condition.
func roleWinCondition(r role) string {
	switch r {
	case roleWerewolf, roleWolfCub:
		return "🏆 Bạn thắng cùng **phe Sói**: khi số người phe Sói còn sống ≥ số người còn lại — không cần giết hết, chỉ cần không còn bị áp đảo."
	case roleCursedWolf:
		return "🏆 Hiện bạn thắng cùng **phe Dân** (như Dân thường). Nếu bị Sói cắn mà sống sót, bạn chuyển sang **phe Sói** và từ đó thắng cùng Sói thay vì Dân."
	case roleTraitor:
		return "🏆 Bạn luôn thắng cùng **phe Sói**, dù Tiên Tri soi bạn ra \"không phải Sói\" và bạn không bỏ phiếu cắn cùng bầy. Nếu mọi Sói khác chết mà bạn còn sống, bạn tự động trở thành Sói thường."
	case roleCupid:
		return "🏆 Bạn thắng cùng **phe Dân** như bình thường — trừ khi 2 người bạn ghép đôi thuộc 2 phe khác nhau và cả hai sống sót đến khi là 2 người cuối cùng, lúc đó bạn luôn thắng cùng họ (dù còn sống hay đã chết, kể cả khi chính bạn là 1 trong 2 người đó)."
	default:
		return "🏆 Bạn thắng cùng **phe Dân**: khi toàn bộ phe Sói đã chết."
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
			"🐺 Ván Ma Sói bắt đầu! Vai trò của bạn là: **%s**.\n%s\n%s\n\n💬 Mọi người thảo luận công khai tại thread \"🏡 Thảo luận Ma Sói\" vào ban ngày; kênh chính chỉ dùng để xem thông báo giai đoạn (đêm/ngày/bỏ phiếu) và bỏ phiếu.",
			p.Role.label(), roleInteractionNote(p.Role), roleWinCondition(p.Role),
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
//
// customRatio is nil for the fixed-table path; non-nil when the starter
// confirmed the manual role-count editor, in which case that exact ratio is
// dealt instead of the rolesForPlayerCount table.
func (cmd *WerewolfCommand) startPreflight(s *discordgo.Session, channelID string, joinOrder []string, labels map[string]string, seq uint64, customRatio *roleRatio) {
	// This goroutine tail-calls the entire rest of the game (night, day
	// discuss, day vote, and back again) — one recover here guards the
	// whole phase chain against a crash taking down the bot process.
	defer func() {
		if r := recover(); r != nil {
			log.Error().
				Interface("panic", r).
				Str("channel_id", channelID).
				Msg("werewolf: recovered from panic in game goroutine")

			var info gameTeardownInfo
			var cleaned bool
			func() {
				cmd.mu.Lock()
				defer cmd.mu.Unlock()
				game := cmd.gameIfCurrent(channelID, seq)
				if game == nil {
					return
				}
				info = gameTeardownInfoFor(channelID, game)
				delete(cmd.games, channelID)
				cleaned = true
			}()
			if cleaned {
				// Shared teardownGame, not an open-coded copy: this is the
				// path that runs when something has ALREADY gone wrong, so
				// it especially must inherit the unlock-before-delete
				// ordering rather than being the one copy that drifts.
				teardownGame(s, info)
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

	// Re-check permissions here, not just at /werewolf start: minutes can
	// pass in the lobby and role-count editor, and an admin editing the
	// channel or the bot's role in that window would otherwise leave the
	// game to discover the gap mid-night, after roles are already dealt and
	// there's no clean way back. Aborting here costs the players a restart;
	// aborting at night costs them the game.
	if missingPerms, err := missingWerewolfPermissions(s, channelID); len(missingPerms) > 0 {
		if err != nil {
			log.Error().Err(err).Str("channel_id", channelID).Msg("werewolf: failed to re-resolve bot channel permissions at preflight")
		}
		sendPlain("❌ " + missingPermissionsMessage(missingPerms))
		cmd.abortGameStillCurrent(s, channelID, seq)
		return
	}

	// deal uses the starter's custom ratio when there is one and it fits the
	// original roster (roleCountConfirmValid already guarantees this sums to
	// the original player count); otherwise it falls back to the fixed
	// table. Only used for the initial deal below — the post-DM-drop
	// reassignment always uses assignRoles directly, never a custom ratio.
	deal := func(order []string) (map[string]*player, error) {
		if customRatio != nil && roleRatioTotal(*customRatio) <= len(order) {
			return dealRoles(order, labels, *customRatio), nil
		}
		return assignRoles(order, labels)
	}

	players, err := deal(joinOrder)
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

			// Deliberately always falls back to the fixed table here — a
			// custom ratio was only guaranteed to fit the original roster
			// size, and re-deriving a smaller custom ratio proportionally
			// isn't worth the complexity. Reusing the custom ratio against a
			// shrunken roster would silently absorb every dropped player out
			// of the villager pool, tilting balance toward the wolves.
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
	roleCounts := formatRoleCountsAnnouncement(players)
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
		stillCurrent := false
		func() {
			cmd.mu.Lock()
			defer cmd.mu.Unlock()
			if g := cmd.gameIfCurrent(channelID, seq); g != nil {
				g.AliveThreadID = aliveThreadID
				stillCurrent = true
			}
		}()
		if !stillCurrent {
			// Same race startWolfThread guards against: the game was
			// force-stopped/cancelled/ended in the window between creating
			// this thread and re-acquiring the lock to record it, so nothing
			// else holds this ID — delete it here instead of leaking it in
			// the guild forever.
			if _, err := s.ChannelDelete(aliveThreadID); err != nil {
				log.Error().Err(err).Str("thread_id", aliveThreadID).Msg("werewolf: failed to delete orphaned alive-discussion thread after game ended mid-creation")
			}
			return
		}
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
	case strings.HasPrefix(customID, werewolfRoleCountConfirmID):
		cmd.handleRoleCountConfirm(s, i, strings.TrimPrefix(customID, werewolfRoleCountConfirmID))
	case strings.HasPrefix(customID, werewolfRoleCountID):
		cmd.handleRoleCountButton(s, i, strings.TrimPrefix(customID, werewolfRoleCountID))
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
	case strings.HasPrefix(customID, werewolfCupidPairID):
		cmd.handleCupidPair(s, i, strings.TrimPrefix(customID, werewolfCupidPairID))
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
