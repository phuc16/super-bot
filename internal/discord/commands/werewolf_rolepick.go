package commands

import (
	"fmt"
	"strings"
)

// roleCountWolfSideRoles and roleCountVillagerSideRoles are the manual-pick
// editor's two fixed groupings (spec §3b's "Phe Sói" / "Phe Dân" split).
// Cursed Wolf is grouped under the wolf side here even though its starting
// Faction is factionVillager (it doesn't flip until a bite conversion) —
// this grouping is a UI/setup-time concept distinct from Faction, matching
// the reference's own preset grouping.
var (
	roleCountWolfSideRoles     = []role{roleWerewolf, roleWolfCub, roleCursedWolf, roleTraitor}
	roleCountVillagerSideRoles = []role{roleVillager, roleSeer, roleDoctor, roleWitch, roleHunter, roleDisruptor, roleCupid}
)

// roleCountMax returns the highest count the manual editor allows for role
// r, given playerCount total seats. Seven roles are capped at 1 — see this
// plan's Global Constraints for why (each is only ever actually acted on
// via the first alive player of that role in the engine today, so a
// second one dealt would silently do nothing). Every other countable role
// is capped only by the total player count, which the confirm-time
// wolf-side/villager-side balance check (roleCountConfirmValid) further
// constrains in practice.
func roleCountMax(r role, playerCount int) int {
	switch r {
	case roleSeer, roleDoctor, roleWitch, roleHunter, roleDisruptor, roleCupid, roleTraitor:
		return 1
	default:
		return playerCount
	}
}

// balanceScore sums rolePoints across every counted role — the manual
// editor's live, informational-only readout (spec §2: no fixed pass/fail
// threshold is load-bearing here).
func balanceScore(counts map[role]int) int {
	score := 0
	for r, c := range counts {
		score += rolePoints[r] * c
	}
	return score
}

// roleCountConfirmValid mirrors the reference's setupValid: the total
// count must exactly match playerCount, and both the wolf-side and
// villager-side groupings must have at least one role counted.
func roleCountConfirmValid(counts map[role]int, playerCount int) bool {
	wolfSide, villagerSide := 0, 0
	for _, r := range roleCountWolfSideRoles {
		wolfSide += counts[r]
	}
	for _, r := range roleCountVillagerSideRoles {
		villagerSide += counts[r]
	}
	return wolfSide+villagerSide == playerCount && wolfSide >= 1 && villagerSide >= 1
}

// roleRatioFromCounts converts the editor's counts into a roleRatio for
// dealRoles. roleVillager has no roleRatio field on purpose — dealRoles
// fills every seat dealRoles's assign() calls don't claim as roleVillager,
// and since roleCountConfirmValid already guarantees the total matches
// playerCount, that remainder is exactly what the editor showed as the
// Villager count.
func roleRatioFromCounts(counts map[role]int) roleRatio {
	return roleRatio{
		Werewolves:   counts[roleWerewolf],
		WolfCubs:     counts[roleWolfCub],
		CursedWolves: counts[roleCursedWolf],
		Traitors:     counts[roleTraitor],
		Seers:        counts[roleSeer],
		Doctors:      counts[roleDoctor],
		Witches:      counts[roleWitch],
		Hunters:      counts[roleHunter],
		Disruptors:   counts[roleDisruptor],
		Cupids:       counts[roleCupid],
	}
}

// roleCountEditorContent renders one side of the editor: that side's
// per-role counts, the combined running total against playerCount, and
// the combined balance score (computed across BOTH sides, since the score
// reflects the whole game's shape, not just this message's side).
//
// Deliberate simplification: only the message that was just clicked gets
// re-rendered (see werewolf_rolepick.go's handleRoleCountButton) — the
// OTHER side's message can show a stale total/score until it's next
// interacted with. This is a display-freshness nit, not a correctness
// issue: confirm-time validation (roleCountConfirmValid) always reads the
// true shared counts, never anything from a message's displayed content.
// Keeping the two messages in perfect sync would require tracking the
// original slash-command interaction's token to edit the other message via
// FollowupMessageEdit — real complexity this feature doesn't need.
func roleCountEditorContent(side string, counts map[role]int, playerCount int) string {
	var roles []role
	var title string
	switch side {
	case "wolf":
		roles = roleCountWolfSideRoles
		title = "🐺 Phe Sói"
	case "villager":
		roles = roleCountVillagerSideRoles
		title = "🏡 Phe Dân"
	}

	total := 0
	for _, r := range roleCountWolfSideRoles {
		total += counts[r]
	}
	for _, r := range roleCountVillagerSideRoles {
		total += counts[r]
	}

	var b strings.Builder
	fmt.Fprintf(&b, "**%s** — chọn số lượng cho mỗi vai trò:\n", title)
	for _, r := range roles {
		fmt.Fprintf(&b, "%s: %d\n", r.label(), counts[r])
	}
	fmt.Fprintf(&b, "\nTổng: %d/%d người · Điểm cân bằng: %d", total, playerCount, balanceScore(counts))
	return b.String()
}
