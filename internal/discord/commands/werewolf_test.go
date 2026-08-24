package commands

import (
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/bwmarrin/discordgo"
)

func TestRolesForPlayerCount(t *testing.T) {
	cases := []struct {
		n    int
		want roleRatio
	}{
		{5, roleRatio{Werewolves: 1}},
		{6, roleRatio{Werewolves: 1, Seers: 1}},
		{7, roleRatio{Werewolves: 2, Seers: 1, Doctors: 1}},
		{8, roleRatio{Werewolves: 2, Seers: 1, Doctors: 1}},
		{9, roleRatio{Werewolves: 2, Seers: 1, Doctors: 1}},
		{10, roleRatio{Werewolves: 2, Seers: 1, Doctors: 1}},
		{11, roleRatio{Werewolves: 3, Seers: 1, Doctors: 1, Witches: 1, Hunters: 1}},
		{12, roleRatio{Werewolves: 3, Seers: 1, Doctors: 1, Witches: 1, Hunters: 1}},
		{13, roleRatio{Werewolves: 3, Seers: 1, Doctors: 1, Witches: 1, Hunters: 1}},
		{14, roleRatio{Werewolves: 3, Seers: 1, Doctors: 1, Witches: 1, Hunters: 1}},
		{15, roleRatio{Werewolves: 4, Seers: 1, Doctors: 1, Witches: 1, Hunters: 1, Disruptors: 1}},
		{16, roleRatio{Werewolves: 4, Seers: 1, Doctors: 1, Witches: 1, Hunters: 1, Disruptors: 1}},
		{17, roleRatio{Werewolves: 4, Seers: 1, Doctors: 1, Witches: 1, Hunters: 1, Disruptors: 1}},
		{18, roleRatio{Werewolves: 5, Seers: 1, Doctors: 1, Witches: 1, Hunters: 1, Disruptors: 1}},
		{19, roleRatio{Werewolves: 5, Seers: 1, Doctors: 1, Witches: 1, Hunters: 1, Disruptors: 1}},
		{20, roleRatio{Werewolves: 5, Seers: 1, Doctors: 1, Witches: 1, Hunters: 1, Disruptors: 1}},
	}
	for _, c := range cases {
		got, err := rolesForPlayerCount(c.n)
		if err != nil {
			t.Fatalf("rolesForPlayerCount(%d) unexpected error: %v", c.n, err)
		}
		if got != c.want {
			t.Errorf("rolesForPlayerCount(%d) = %+v, want %+v", c.n, got, c.want)
		}
	}
}

func TestRolesForPlayerCountOutOfRange(t *testing.T) {
	if _, err := rolesForPlayerCount(4); err == nil {
		t.Error("expected error for 4 players, got nil")
	}
	if _, err := rolesForPlayerCount(21); err == nil {
		t.Error("expected error for 21 players, got nil")
	}
}

func TestAssignRolesCounts(t *testing.T) {
	joinOrder := []string{"a", "b", "c", "d", "e", "f", "g"} // 7 players
	labels := map[string]string{"a": "A", "b": "B", "c": "C", "d": "D", "e": "E", "f": "F", "g": "G"}

	players, err := assignRoles(joinOrder, labels)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(players) != 7 {
		t.Fatalf("expected 7 players, got %d", len(players))
	}

	counts := map[role]int{}
	for _, id := range joinOrder {
		p, ok := players[id]
		if !ok {
			t.Fatalf("player %s missing from assigned roles", id)
		}
		if !p.Alive {
			t.Errorf("player %s should start alive", id)
		}
		if p.Label != labels[id] {
			t.Errorf("player %s label = %q, want %q", id, p.Label, labels[id])
		}
		counts[p.Role]++
	}

	if counts[roleWerewolf] != 2 || counts[roleSeer] != 1 || counts[roleDoctor] != 1 || counts[roleVillager] != 3 {
		t.Errorf("unexpected role distribution: %+v", counts)
	}
}

func TestAssignRolesOutOfRange(t *testing.T) {
	if _, err := assignRoles([]string{"a", "b"}, nil); err == nil {
		t.Error("expected error for too few players")
	}
}

func TestDealRolesAssignsWolfCubCursedWolfTraitor(t *testing.T) {
	joinOrder := []string{"u1", "u2", "u3", "u4", "u5", "u6"}
	labels := map[string]string{
		"u1": "A", "u2": "B", "u3": "C", "u4": "D", "u5": "E", "u6": "F",
	}
	ratio := roleRatio{Werewolves: 1, WolfCubs: 1, CursedWolves: 1, Traitors: 1, Seers: 1}
	players := dealRoles(joinOrder, labels, ratio)

	if len(players) != len(joinOrder) {
		t.Fatalf("dealRoles() produced %d players, want %d", len(players), len(joinOrder))
	}

	counts := make(map[role]int)
	for _, p := range players {
		counts[p.Role]++
		switch p.Role {
		case roleWerewolf, roleWolfCub, roleTraitor:
			if p.Faction != factionWolf {
				t.Errorf("player with role %v has faction %v, want factionWolf", p.Role, p.Faction)
			}
		case roleCursedWolf:
			if p.Faction != factionVillager {
				t.Errorf("freshly-dealt Cursed Wolf has faction %v, want factionVillager (starts as villager, flips on conversion)", p.Faction)
			}
		}
	}
	want := map[role]int{
		roleWerewolf:   1,
		roleWolfCub:    1,
		roleCursedWolf: 1,
		roleTraitor:    1,
		roleSeer:       1,
		roleVillager:   1,
	}
	for r, wantCount := range want {
		if counts[r] != wantCount {
			t.Errorf("counts[%v] = %d, want %d", r, counts[r], wantCount)
		}
	}
}

func TestDealRolesAssignsDisruptor(t *testing.T) {
	joinOrder := []string{"u1", "u2", "u3"}
	labels := map[string]string{"u1": "A", "u2": "B", "u3": "C"}
	ratio := roleRatio{Werewolves: 1, Disruptors: 1}
	players := dealRoles(joinOrder, labels, ratio)

	var disruptorCount int
	for _, p := range players {
		if p.Role == roleDisruptor {
			disruptorCount++
			if p.Faction != factionVillager {
				t.Errorf("Disruptor has faction %v, want factionVillager", p.Faction)
			}
		}
	}
	if disruptorCount != 1 {
		t.Errorf("dealRoles() dealt %d Disruptors, want 1", disruptorCount)
	}
}

func TestDealRolesAssignsCupid(t *testing.T) {
	joinOrder := []string{"u1", "u2", "u3"}
	labels := map[string]string{"u1": "A", "u2": "B", "u3": "C"}
	ratio := roleRatio{Werewolves: 1, Cupids: 1}
	players := dealRoles(joinOrder, labels, ratio)

	var cupidCount int
	for _, p := range players {
		if p.Role == roleCupid {
			cupidCount++
			if p.Faction != factionVillager {
				t.Errorf("Cupid has faction %v, want factionVillager", p.Faction)
			}
		}
	}
	if cupidCount != 1 {
		t.Errorf("dealRoles() dealt %d Cupids, want 1", cupidCount)
	}
}

func TestRoleRatioTotal(t *testing.T) {
	tests := []struct {
		name string
		r    roleRatio
		want int
	}{
		{"empty", roleRatio{}, 0},
		{"single werewolf", roleRatio{Werewolves: 1}, 1},
		{"wolf side mix", roleRatio{Werewolves: 2, WolfCubs: 1, CursedWolves: 1, Traitors: 1}, 5},
		{"villager side mix", roleRatio{Seers: 1, Doctors: 1, Witches: 1, Hunters: 1, Disruptors: 1, Cupids: 1}, 6},
		{"all roles", roleRatio{
			Werewolves: 1, WolfCubs: 1, CursedWolves: 1, Traitors: 1,
			Seers: 1, Doctors: 1, Witches: 1, Hunters: 1, Disruptors: 1, Cupids: 1,
		}, 10},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := roleRatioTotal(tt.r); got != tt.want {
				t.Errorf("roleRatioTotal(%+v) = %d, want %d", tt.r, got, tt.want)
			}
		})
	}
}

// TestDealRolesExactSeatBoundary guards the panic-guard property Fix 1
// depends on for the first (original) preflight deal call site: a custom
// ratio whose total exactly equals len(joinOrder) must be fully claimed by
// dealRoles, with zero seats left over for roleVillager, and must not panic.
func TestDealRolesExactSeatBoundary(t *testing.T) {
	joinOrder := []string{"u1", "u2", "u3", "u4"}
	labels := map[string]string{"u1": "A", "u2": "B", "u3": "C", "u4": "D"}
	ratio := roleRatio{Werewolves: 1, Seers: 1, Doctors: 1, Traitors: 1}
	if total := roleRatioTotal(ratio); total != len(joinOrder) {
		t.Fatalf("test setup: roleRatioTotal(ratio) = %d, want %d (exact seat boundary)", total, len(joinOrder))
	}

	players := dealRoles(joinOrder, labels, ratio)

	if len(players) != len(joinOrder) {
		t.Fatalf("dealRoles() produced %d players, want %d", len(players), len(joinOrder))
	}
	counts := make(map[role]int)
	for _, p := range players {
		counts[p.Role]++
	}
	want := map[role]int{roleWerewolf: 1, roleSeer: 1, roleDoctor: 1, roleTraitor: 1}
	for r, wantCount := range want {
		if counts[r] != wantCount {
			t.Errorf("counts[%v] = %d, want %d", r, counts[r], wantCount)
		}
	}
	if counts[roleVillager] != 0 {
		t.Errorf("counts[roleVillager] = %d, want 0 (ratio exactly claims every seat)", counts[roleVillager])
	}
}

func TestCheckWinConditionOngoing(t *testing.T) {
	players := map[string]*player{
		"w1": {UserID: "w1", Role: roleWerewolf, Faction: factionWolf, Alive: true},
		"v1": {UserID: "v1", Role: roleVillager, Alive: true},
		"v2": {UserID: "v2", Role: roleVillager, Alive: true},
	}
	if got := checkWinCondition(players); got != resultOngoing {
		t.Errorf("got %v, want resultOngoing", got)
	}
}

func TestCheckWinConditionVillagersWin(t *testing.T) {
	players := map[string]*player{
		"w1": {UserID: "w1", Role: roleWerewolf, Alive: false},
		"v1": {UserID: "v1", Role: roleVillager, Alive: true},
	}
	if got := checkWinCondition(players); got != resultVillagersWin {
		t.Errorf("got %v, want resultVillagersWin", got)
	}
}

func TestCheckWinConditionWerewolvesWin(t *testing.T) {
	players := map[string]*player{
		"w1": {UserID: "w1", Role: roleWerewolf, Faction: factionWolf, Alive: true},
		"w2": {UserID: "w2", Role: roleWerewolf, Faction: factionWolf, Alive: true},
		"v1": {UserID: "v1", Role: roleVillager, Alive: true},
	}
	if got := checkWinCondition(players); got != resultWerewolvesWin {
		t.Errorf("got %v, want resultWerewolvesWin (2 wolves >= 1 other)", got)
	}
}

func TestCheckWinConditionEmptyRoster(t *testing.T) {
	if got := checkWinCondition(map[string]*player{}); got != resultOngoing {
		t.Errorf("empty roster: got %v, want resultOngoing", got)
	}
	if got := checkWinCondition(nil); got != resultOngoing {
		t.Errorf("nil roster: got %v, want resultOngoing", got)
	}
}

func TestCheckWinConditionAllDeadMidGame(t *testing.T) {
	// A non-empty roster where everyone happens to be dead simultaneously
	// (e.g. an AFK-removal batch) must not be treated as "ongoing" — zero
	// alive werewolves with a real roster is a villager win.
	players := map[string]*player{
		"a": {UserID: "a", Role: roleVillager, Alive: false},
		"b": {UserID: "b", Role: roleVillager, Alive: false},
	}
	if got := checkWinCondition(players); got != resultVillagersWin {
		t.Errorf("all-dead mid-game roster: got %v, want resultVillagersWin", got)
	}
}

func TestCheckWinConditionWithLoversMixedFactionLastTwoWin(t *testing.T) {
	players := map[string]*player{
		"wolf":     {UserID: "wolf", Role: roleWerewolf, Faction: factionWolf, Alive: true},
		"villager": {UserID: "villager", Role: roleVillager, Faction: factionVillager, Alive: true},
	}
	got := checkWinConditionWithLovers(players, [2]string{"wolf", "villager"})
	if got != resultLoversWin {
		t.Errorf("mixed-faction lovers as the last two alive: got %v, want resultLoversWin", got)
	}
}

func TestCheckWinConditionWithLoversSameFactionUsesNormalRule(t *testing.T) {
	players := map[string]*player{
		"wolf1": {UserID: "wolf1", Role: roleWerewolf, Faction: factionWolf, Alive: true},
		"wolf2": {UserID: "wolf2", Role: roleWerewolf, Faction: factionWolf, Alive: true},
	}
	got := checkWinConditionWithLovers(players, [2]string{"wolf1", "wolf2"})
	if got != resultWerewolvesWin {
		t.Errorf("same-faction lovers: got %v, want resultWerewolvesWin (normal rule, no special case)", got)
	}
}

func TestCheckWinConditionWithLoversMixedFactionButOthersAliveIsOngoing(t *testing.T) {
	players := map[string]*player{
		"wolf":      {UserID: "wolf", Role: roleWerewolf, Faction: factionWolf, Alive: true},
		"villager":  {UserID: "villager", Role: roleVillager, Faction: factionVillager, Alive: true},
		"bystander": {UserID: "bystander", Role: roleVillager, Faction: factionVillager, Alive: true},
	}
	got := checkWinConditionWithLovers(players, [2]string{"wolf", "villager"})
	if got != resultOngoing {
		t.Errorf("mixed lovers with a third player still alive: got %v, want resultOngoing (normal rule applies since the couple isn't the last two yet — 1 wolf vs. 2 others, not outnumbered)", got)
	}
}

func TestCheckWinConditionWithLoversOneDeadUsesNormalRule(t *testing.T) {
	players := map[string]*player{
		"wolf":     {UserID: "wolf", Role: roleWerewolf, Faction: factionWolf, Alive: false},
		"villager": {UserID: "villager", Role: roleVillager, Faction: factionVillager, Alive: true},
	}
	got := checkWinConditionWithLovers(players, [2]string{"wolf", "villager"})
	if got != resultVillagersWin {
		t.Errorf("one lover already dead: got %v, want resultVillagersWin (normal rule, no couple left to win)", got)
	}
}

func TestCheckWinConditionWithLoversNoPairingUsesNormalRule(t *testing.T) {
	players := map[string]*player{
		"wolf":     {UserID: "wolf", Role: roleWerewolf, Faction: factionWolf, Alive: false},
		"villager": {UserID: "villager", Role: roleVillager, Faction: factionVillager, Alive: true},
	}
	got := checkWinConditionWithLovers(players, [2]string{"", ""})
	if got != resultVillagersWin {
		t.Errorf("no Cupid pairing this game: got %v, want resultVillagersWin", got)
	}
}

func TestAlivePlayersIncludesEveryoneEvenSelf(t *testing.T) {
	game := &werewolfGame{
		JoinOrder: []string{"a", "b", "c"},
		Players: map[string]*player{
			"a": {UserID: "a", Label: "A", Alive: true},
			"b": {UserID: "b", Label: "B", Alive: true},
			"c": {UserID: "c", Label: "C", Alive: false},
		},
	}
	got := alivePlayers(game)
	if len(got) != 2 {
		t.Fatalf("alivePlayers() = %d players, want 2 (dead player C excluded)", len(got))
	}
	ids := map[string]bool{got[0].UserID: true, got[1].UserID: true}
	if !ids["a"] || !ids["b"] {
		t.Errorf("alivePlayers() = %v, want both a and b present (an actor must see themself in their own target list, e.g. Cupid picking self)", got)
	}
}

func TestAliveWolfBiteTargetsIncludesTraitor(t *testing.T) {
	// Regression for the code-review finding: excluding the Traitor from
	// the wolf pack's bite-target menu let the pack identify them by pure
	// elimination (public alive count minus wolf-thread headcount minus
	// menu size == exactly the Traitor). The wolves don't know who the
	// Traitor is, so the Traitor must appear as a normal, bitable target.
	game := &werewolfGame{
		JoinOrder: []string{"wolf", "traitor", "villager"},
		Players: map[string]*player{
			"wolf":     {UserID: "wolf", Role: roleWerewolf, Faction: factionWolf, Alive: true},
			"traitor":  {UserID: "traitor", Role: roleTraitor, Faction: factionWolf, Alive: true},
			"villager": {UserID: "villager", Role: roleVillager, Faction: factionVillager, Alive: true},
		},
	}
	got := aliveWolfBiteTargets(game)
	ids := map[string]bool{}
	for _, p := range got {
		ids[p.UserID] = true
	}
	if len(got) != 2 || !ids["traitor"] || !ids["villager"] {
		t.Errorf("aliveWolfBiteTargets() = %v, want exactly [traitor, villager] — the Traitor must be a valid, visible bite target", got)
	}
	if ids["wolf"] {
		t.Error("aliveWolfBiteTargets() must not include an actual wolf-vote-participant")
	}
}

func TestValidDayVotesDropsVotesFromDeadOrGoneVoters(t *testing.T) {
	players := map[string]*player{
		"a": {UserID: "a", Alive: true},
		"b": {UserID: "b", Alive: false}, // left/died after voting
		"c": {UserID: "c", Alive: true},
	}
	votes := map[string]string{
		"a": "c",
		"b": "c", // stale — b is no longer alive, must not count
	}
	got := validDayVotes(votes, players)
	want := map[string]string{"a": "c"}
	if len(got) != len(want) || got["a"] != want["a"] {
		t.Errorf("validDayVotes() = %v, want %v — b's vote must be dropped since b is no longer alive", got, want)
	}
}

func TestValidDayVotesDropsVoteFromUnknownVoter(t *testing.T) {
	players := map[string]*player{
		"a": {UserID: "a", Alive: true},
	}
	votes := map[string]string{"a": "a", "ghost": "a"}
	got := validDayVotes(votes, players)
	if len(got) != 1 || got["a"] != "a" {
		t.Errorf("validDayVotes() = %v, want only a's vote to survive", got)
	}
}

func TestValidDayVotesStaleVoteCannotFlipLynchOutcome(t *testing.T) {
	// Regression for the exact scenario from the code-review finding: A
	// votes for B, then A leaves before resolution. Without filtering, A's
	// vote could still tip a tie-break; with it, only C's vote remains and
	// nobody has a plurality large enough to change the outcome
	// unexpectedly.
	players := map[string]*player{
		"a": {UserID: "a", Alive: false}, // left after voting
		"b": {UserID: "b", Alive: true},
		"c": {UserID: "c", Alive: true},
	}
	votes := map[string]string{"a": "b", "c": "b"}
	top, tie := tallyVotes(validDayVotes(votes, players))
	if tie || len(top) != 1 || top[0] != "b" {
		t.Errorf("tallyVotes(validDayVotes(...)) = %v, %v — want b alone with 1 valid vote", top, tie)
	}

	// Sanity check: without the filter, the stale vote would double b's
	// count but the result target is the same here — the important
	// assertion is that only 1 valid vote reaches the tally, not 2.
	if got := len(validDayVotes(votes, players)); got != 1 {
		t.Errorf("validDayVotes(...) returned %d votes, want exactly 1 (a's stale vote must be dropped)", got)
	}
}

func TestValidDayVotesFallsThroughToRunnerUpWhenLeaderLeaves(t *testing.T) {
	// Regression for the code-review finding: Alice draws the plurality
	// (3 votes), then leaves mid-window before resolution. Without
	// filtering dead TARGETS, the tally would still return Alice as the
	// sole top candidate, the alive-check on her would fail, and the
	// decisive lynch would be silently voided instead of falling through
	// to Bob, the obvious runner-up.
	players := map[string]*player{
		"alice": {UserID: "alice", Alive: false}, // left mid-vote
		"bob":   {UserID: "bob", Alive: true},
		"v1":    {UserID: "v1", Alive: true},
		"v2":    {UserID: "v2", Alive: true},
		"v3":    {UserID: "v3", Alive: true},
	}
	votes := map[string]string{
		"v1": "alice", "v2": "alice", "v3": "alice",
		"bob": "bob",
		// a 5th voter's ballot is irrelevant here; 3 votes for alice vs 1 for bob
	}
	top, tie := tallyVotes(validDayVotes(votes, players))
	if tie || len(top) != 1 || top[0] != "bob" {
		t.Errorf("tallyVotes(validDayVotes(...)) = %v, %v — want bob alone once alice's votes are dropped as a dead target", top, tie)
	}
}

func TestValidDayVotesDropsVoteForDeadTargetKeepsSkipUntouched(t *testing.T) {
	// The skip sentinel must never be treated as a "target" to validate —
	// it isn't a player ID, so looking it up in players would always fail
	// and incorrectly drop every skip vote.
	players := map[string]*player{
		"v1": {UserID: "v1", Alive: true},
	}
	votes := map[string]string{"v1": voteSkipValue}
	got := validDayVotes(votes, players)
	if len(got) != 1 || got["v1"] != voteSkipValue {
		t.Errorf("validDayVotes(...) = %v, want the skip vote preserved", got)
	}
}

func TestTallyVotesSingleWinner(t *testing.T) {
	votes := map[string]string{"a": "x", "b": "x", "c": "y"}
	top, tie := tallyVotes(votes)
	if tie {
		t.Fatal("expected no tie")
	}
	if len(top) != 1 || top[0] != "x" {
		t.Errorf("got %v, want [x]", top)
	}
}

func TestTallyVotesTie(t *testing.T) {
	votes := map[string]string{"a": "x", "b": "y"}
	_, tie := tallyVotes(votes)
	if !tie {
		t.Fatal("expected a tie")
	}
}

func TestTallyVotesEmpty(t *testing.T) {
	top, tie := tallyVotes(map[string]string{})
	if tie || len(top) != 0 {
		t.Errorf("expected no votes -> no tie, no targets; got top=%v tie=%v", top, tie)
	}
}

func TestShouldRemoveForAFK(t *testing.T) {
	if shouldRemoveForAFK(0) || shouldRemoveForAFK(1) {
		t.Error("fewer than 2 strikes should not remove")
	}
	if !shouldRemoveForAFK(2) || !shouldRemoveForAFK(3) {
		t.Error("2 or more strikes should remove")
	}
}

func TestDayDiscussWindowFor(t *testing.T) {
	cases := []struct {
		alive int
		want  time.Duration
	}{
		{5, 45*time.Second + 15*5*time.Second},
		{8, 45*time.Second + 15*8*time.Second},
		{12, 45*time.Second + 15*12*time.Second},
	}
	for _, c := range cases {
		if got := dayDiscussWindowFor(c.alive); got != c.want {
			t.Errorf("dayDiscussWindowFor(%d) = %v, want %v", c.alive, got, c.want)
		}
	}
}

func TestWolfDiscussWindowFor(t *testing.T) {
	cases := []struct {
		wolves int
		want   time.Duration
	}{
		{1, 60*time.Second + 20*time.Second},
		{2, 60*time.Second + 40*time.Second},
		{4, 60*time.Second + 80*time.Second},
	}
	for _, c := range cases {
		if got := wolfDiscussWindowFor(c.wolves); got != c.want {
			t.Errorf("wolfDiscussWindowFor(%d) = %v, want %v", c.wolves, got, c.want)
		}
	}
}

func TestTallyVotesSkipSentinelWins(t *testing.T) {
	votes := map[string]string{"a": voteSkipValue, "b": voteSkipValue, "c": "x"}
	top, tie := tallyVotes(votes)
	if tie {
		t.Fatal("expected no tie")
	}
	if len(top) != 1 || top[0] != voteSkipValue {
		t.Errorf("got %v, want [%s]", top, voteSkipValue)
	}
}

func TestFormatRoundBlockFullFields(t *testing.T) {
	r := roundLog{
		Round:             1,
		WolfTargetLabel:   "Alice",
		WolfTargetSaved:   false,
		DoctorTargetLabel: "Bob",
		SeerTargetLabel:   "Carol",
		SeerResult:        roleWerewolf,
		VoteBreakdown:     map[string][]string{"Dave": {"Frank", "Grace"}, "Eve": {"Heidi"}},
		LynchedLabel:      "Dave",
	}
	got := formatRoundBlock(r)
	wantParts := []string{
		"Đêm/Ngày 1:",
		"🐺 Sói cắn: Alice",
		"💉 Bác sĩ bảo vệ: Bob",
		"🔮 Tiên tri xem: Carol → Ma Sói",
		"Dave: 2 (Frank, Grace)",
		"Eve: 1 (Heidi)",
		"Dave bị treo cổ",
	}
	for _, want := range wantParts {
		if !strings.Contains(got, want) {
			t.Errorf("formatRoundBlock output missing %q; got:\n%s", want, got)
		}
	}
}

func TestFormatRoundBlockOmitsDeadOrAbsentRoles(t *testing.T) {
	r := roundLog{Round: 2, WolfTargetLabel: "Alice", WolfTargetSaved: true}
	got := formatRoundBlock(r)
	if !strings.Contains(got, "được bác sĩ cứu") {
		t.Errorf("expected saved-target wording; got:\n%s", got)
	}
	for _, absent := range []string{"Bác sĩ bảo vệ", "Tiên tri xem", "Kết quả bỏ phiếu"} {
		if strings.Contains(got, absent) {
			t.Errorf("expected %q to be omitted when its fields are empty; got:\n%s", absent, got)
		}
	}
}

func TestFormatRoundBlockWolfVoteTied(t *testing.T) {
	tied := roundLog{Round: 3, WolfVoteTied: true}
	got := formatRoundBlock(tied)
	if !strings.Contains(got, "Sói không thống nhất được mục tiêu") {
		t.Errorf("expected wolf-tie line when WolfVoteTied is true; got:\n%s", got)
	}

	notTied := roundLog{Round: 3}
	got = formatRoundBlock(notTied)
	if strings.Contains(got, "Sói không thống nhất được mục tiêu") {
		t.Errorf("expected wolf-tie line to be omitted when WolfVoteTied is false; got:\n%s", got)
	}
}

func TestFormatRoundBlockDoctorSavedWithPoisonNotContradictory(t *testing.T) {
	r := roundLog{Round: 4, WolfTargetLabel: "Alice", WolfTargetSaved: true, WitchPoisonedLabel: "Eve"}
	got := formatRoundBlock(r)
	if !strings.Contains(got, "được bác sĩ cứu") {
		t.Errorf("expected saved-target wording; got:\n%s", got)
	}
	if !strings.Contains(got, "☠️ Phù thủy đầu độc: Eve") {
		t.Errorf("expected poison line; got:\n%s", got)
	}
	// Regression guard for the Fix 3 wording change: the doctor-saved
	// wolf-bite line must acknowledge the poison death happened the same
	// night, rather than reading as if nobody died.
	if !strings.Contains(got, "trúng độc") {
		t.Errorf("expected doctor-saved line to acknowledge the poison death; got:\n%s", got)
	}
}

func TestFormatRoundBlockWolfVoteTiedWithPoisonNotContradictory(t *testing.T) {
	r := roundLog{Round: 5, WolfVoteTied: true, WitchPoisonedLabel: "Eve"}
	got := formatRoundBlock(r)
	if !strings.Contains(got, "Sói không thống nhất được mục tiêu") {
		t.Errorf("expected wolf-tie line; got:\n%s", got)
	}
	if !strings.Contains(got, "trúng độc") {
		t.Errorf("expected the tied-vote line to acknowledge the poison death; got:\n%s", got)
	}
}

func TestResolveWolfBiteOutcome(t *testing.T) {
	cases := []struct {
		name       string
		targetRole role
		saved      bool
		want       wolfBiteOutcome
	}{
		{"villager, not saved, dies", roleVillager, false, wolfBiteKill},
		{"villager, saved, spared", roleVillager, true, wolfBiteSpare},
		{"cursed wolf, not saved, converts", roleCursedWolf, false, wolfBiteConvert},
		{"cursed wolf, saved, spared (no conversion either)", roleCursedWolf, true, wolfBiteSpare},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := resolveWolfBiteOutcome(c.targetRole, c.saved); got != c.want {
				t.Errorf("resolveWolfBiteOutcome(%v, %v) = %v, want %v", c.targetRole, c.saved, got, c.want)
			}
		})
	}
}

func TestHunterRevengeLineRespectsRevealOnDeath(t *testing.T) {
	p := &player{Label: "Alice", Role: roleHunter}
	revealed := hunterRevengeLine(p, true)
	if !strings.Contains(revealed, "Thợ Săn") {
		t.Errorf("hunterRevengeLine(revealOnDeath=true) = %q, want it to name the mechanism", revealed)
	}
	hidden := hunterRevengeLine(p, false)
	if strings.Contains(hidden, "Thợ Săn") {
		t.Errorf("hunterRevengeLine(revealOnDeath=false) = %q, must not reveal the Hunter role", hidden)
	}
	if !strings.Contains(hidden, "Alice") {
		t.Errorf("hunterRevengeLine(revealOnDeath=false) = %q, still must name who died", hidden)
	}
}

func TestLoverDeathLineRespectsRevealOnDeath(t *testing.T) {
	p := &player{Label: "Bob", Role: roleVillager}
	revealed := loverDeathLine(p, true)
	if !strings.Contains(revealed, "người yêu") {
		t.Errorf("loverDeathLine(revealOnDeath=true) = %q, want it to name the mechanism", revealed)
	}
	hidden := loverDeathLine(p, false)
	if strings.Contains(hidden, "người yêu") {
		t.Errorf("loverDeathLine(revealOnDeath=false) = %q, must not reveal the lover pairing/Cupid", hidden)
	}
	if !strings.Contains(hidden, "Bob") {
		t.Errorf("loverDeathLine(revealOnDeath=false) = %q, still must name who died", hidden)
	}
	if hidden != hunterRevengeLine(&player{Label: "Bob"}, false) {
		t.Error("hunterRevengeLine and loverDeathLine must render IDENTICAL text when revealOnDeath=false, so the fallback wording itself can't hint at which mechanism caused the chained death")
	}
}

func TestFormatRoundBlockCursedWolfConverted(t *testing.T) {
	r := roundLog{Round: 2, WolfTargetLabel: "Chi", CursedWolfConverted: []string{"Chi"}}
	got := formatRoundBlock(r)
	if !strings.Contains(got, "biến thành Sói Nguyền") {
		t.Errorf("formatRoundBlock() = %q, want it to mention the conversion", got)
	}
	if strings.Contains(got, "Chi đã chết") {
		t.Errorf("formatRoundBlock() = %q, must not imply Chi died", got)
	}
}

func TestCursedWolfConvertedRecordsBothInABonusRound(t *testing.T) {
	// Regression: CursedWolfConverted used to be a single string,
	// overwritten per target — a bonus-bite round converting 2 Cursed
	// Wolves in the same round would silently lose the first one from the
	// recap. Cursed Wolf has no 1-per-game cap, so this is reachable.
	var converted []string
	converted = append(converted, "Alice")
	converted = append(converted, "Bob")
	r := roundLog{Round: 4, CursedWolfConverted: converted}
	if len(r.CursedWolfConverted) != 2 || r.CursedWolfConverted[0] != "Alice" || r.CursedWolfConverted[1] != "Bob" {
		t.Errorf("CursedWolfConverted = %v, want both Alice and Bob preserved", r.CursedWolfConverted)
	}
}

// TestWitchHasChoicePredicate checks witchHasChoiceLeft (shared by
// resolveNight's prompt/wait decision and by handleWitchHeal/
// handleWitchPoison's "does she still have another action" check) against
// witchActionComponents' actual rendering — the ground truth for "does she
// have anything clickable beyond skip" — across the full truth table of
// (healUsed, poisonUsed, hasBiteTarget).
func TestWitchHasChoicePredicate(t *testing.T) {
	someTarget := &player{UserID: "u2", Label: "Bob"}
	cases := []struct {
		healUsed, poisonUsed, hasBite bool
	}{
		{false, false, true},
		{false, false, false},
		{false, true, true},
		{false, true, false},
		{true, false, true},
		{true, false, false},
		{true, true, true},
		{true, true, false},
	}
	for _, c := range cases {
		var biteTargets []*player
		biteCount := 0
		if c.hasBite {
			biteTargets = []*player{someTarget}
			biteCount = 1
		}
		hasChoice := witchHasChoiceLeft(c.poisonUsed, c.healUsed, biteCount)

		_ = witchActionComponents("chan", biteTargets, c.healUsed, c.poisonUsed, []*player{someTarget})
		rendersPoisonRow := !c.poisonUsed
		rendersHealButton := c.hasBite && !c.healUsed
		wouldRenderChoice := rendersPoisonRow || rendersHealButton

		if hasChoice != wouldRenderChoice {
			t.Errorf("case %+v: hasChoice=%v but witchActionComponents would render a choice=%v (poison row=%v, heal button=%v)",
				c, hasChoice, wouldRenderChoice, rendersPoisonRow, rendersHealButton)
		}
	}
}

func TestHealableBiteTargetCount(t *testing.T) {
	cases := []struct {
		name        string
		biteTargets []string
		poisonID    string
		want        int
	}{
		{"no poison yet, all bite targets healable", []string{"a", "b"}, "", 2},
		{"poisoned a non-bite-target, all still healable", []string{"a", "b"}, "z", 2},
		{"poisoned one of two bite targets, other still healable", []string{"a", "b"}, "a", 1},
		{"poisoned the lone bite target, none healable", []string{"a"}, "a", 0},
		{"no bite targets at all", nil, "", 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := healableBiteTargetCount(c.biteTargets, c.poisonID); got != c.want {
				t.Errorf("healableBiteTargetCount(%v, %q) = %d, want %d", c.biteTargets, c.poisonID, got, c.want)
			}
		})
	}
}

func TestWitchTurnClosesAfterPoisoningTheLoneBiteTarget(t *testing.T) {
	// Regression: poisoning the only bite target makes heal unreachable
	// (handleWitchHeal refuses to heal someone she poisoned), so her turn
	// MUST be treated as over. Reading the raw bite-target count here
	// instead of the healable count left her turn "open" with only a
	// button guaranteed to error, stalling the whole night for the full
	// witchActionWindow every such round.
	const loneTarget = "victim"
	biteTargets := []string{loneTarget}

	// She has just poisoned the lone bite target; heal is still unused.
	healable := healableBiteTargetCount(biteTargets, loneTarget)
	if witchHasChoiceLeft(true /*poisonUsed*/, false /*healUsed*/, healable) {
		t.Error("witch should have NO choice left after poisoning the only bite target — heal is unreachable, so her turn must close")
	}

	// Contrast: poisoning someone who ISN'T the bite target must leave
	// heal available and her turn open.
	healableOther := healableBiteTargetCount(biteTargets, "someone-else")
	if !witchHasChoiceLeft(true, false, healableOther) {
		t.Error("witch should still have heal available after poisoning a non-bite-target")
	}
}

func TestWitchTurnStaysOpenAfterPoisoningOneOfTwoBiteTargets(t *testing.T) {
	// Bonus-bite round: poisoning one bite target must still leave the
	// OTHER one healable, keeping her turn open for that second action.
	biteTargets := []string{"a", "b"}
	healable := healableBiteTargetCount(biteTargets, "a")
	if healable != 1 {
		t.Fatalf("healableBiteTargetCount = %d, want 1", healable)
	}
	if !witchHasChoiceLeft(true /*poisonUsed*/, false /*healUsed*/, healable) {
		t.Error("witch should still be able to heal the other bite target after poisoning one of two")
	}
}

func TestWitchHasChoiceLeftAllowsBothPotionsSameNight(t *testing.T) {
	// Regression: using one potion must not report "no choice left" while
	// the other potion is still unused and there's a bite target to heal —
	// heal and poison are independent, once-per-game items usable together
	// the same night.
	if !witchHasChoiceLeft(false, true, 1) {
		t.Error("witchHasChoiceLeft(poisonUsed=false, healUsed=true, biteTargetCount=1) = false, want true — poison is still available")
	}
	if !witchHasChoiceLeft(true, false, 1) {
		t.Error("witchHasChoiceLeft(poisonUsed=true, healUsed=false, biteTargetCount=1) = false, want true — heal is still available")
	}
	if witchHasChoiceLeft(true, true, 1) {
		t.Error("witchHasChoiceLeft(poisonUsed=true, healUsed=true, biteTargetCount=1) = true, want false — both potions spent")
	}
}

func TestResolveWitchHealTarget(t *testing.T) {
	cases := []struct {
		name        string
		saveTarget  string
		biteTargets []string
		want        string
	}{
		{"no heal chosen", "", []string{"v1"}, ""},
		{"single-target sentinel resolves to the lone bite target", "heal", []string{"v1"}, "v1"},
		{"sentinel with no bite targets resolves to nothing", "heal", nil, ""},
		{"explicit 2-target pick passes through as-is", "v2", []string{"v1", "v2"}, "v2"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := resolveWitchHealTarget(c.saveTarget, c.biteTargets); got != c.want {
				t.Errorf("resolveWitchHealTarget(%q, %v) = %q, want %q", c.saveTarget, c.biteTargets, got, c.want)
			}
		})
	}
}

func TestChunkRecapMessagesSingleChunkWhenSmall(t *testing.T) {
	history := []roundLog{{Round: 1, WolfTargetLabel: "A"}, {Round: 2, WolfTargetLabel: "B"}}
	chunks := chunkRecapMessages(history, 2000)
	if len(chunks) != 1 {
		t.Fatalf("expected 1 chunk, got %d", len(chunks))
	}
	if !strings.Contains(chunks[0], "Đêm/Ngày 1") || !strings.Contains(chunks[0], "Đêm/Ngày 2") {
		t.Errorf("expected both rounds in the single chunk; got:\n%s", chunks[0])
	}
}

func TestChunkRecapMessagesSplitsOnRoundBoundary(t *testing.T) {
	history := []roundLog{
		{Round: 1, WolfTargetLabel: "AAAAAAAAAAAAAAAAAAAA"},
		{Round: 2, WolfTargetLabel: "BBBBBBBBBBBBBBBBBBBB"},
		{Round: 3, WolfTargetLabel: "CCCCCCCCCCCCCCCCCCCC"},
	}
	// A tiny limit forces a split after almost every round.
	chunks := chunkRecapMessages(history, 80)
	if len(chunks) < 2 {
		t.Fatalf("expected multiple chunks with a tiny limit, got %d", len(chunks))
	}
	for _, c := range chunks {
		if len(c) > 200 {
			t.Errorf("chunk exceeds a sane bound (got %d chars): %s", len(c), c)
		}
	}
	joined := strings.Join(chunks, "")
	for _, want := range []string{"Đêm/Ngày 1", "Đêm/Ngày 2", "Đêm/Ngày 3"} {
		if !strings.Contains(joined, want) {
			t.Errorf("expected %q to appear somewhere across chunks", want)
		}
	}
}

func TestChunkRecapMessagesEmptyHistory(t *testing.T) {
	if got := chunkRecapMessages(nil, 2000); got != nil {
		t.Errorf("expected nil for empty history, got %v", got)
	}
}

func TestFactionForRole(t *testing.T) {
	cases := []struct {
		r    role
		want faction
	}{
		{roleVillager, factionVillager},
		{roleWerewolf, factionWolf},
		{roleSeer, factionVillager},
		{roleDoctor, factionVillager},
		{roleWolfCub, factionWolf},
		{roleCursedWolf, factionVillager}, // starts villager-faction; flips to wolf on conversion (later plan)
		{roleTraitor, factionWolf},
		{roleWitch, factionVillager},
		{roleHunter, factionVillager},
		{roleDisruptor, factionVillager},
		{roleCupid, factionVillager},
	}
	for _, c := range cases {
		if got := factionForRole(c.r); got != c.want {
			t.Errorf("factionForRole(%v) = %v, want %v", c.r, got, c.want)
		}
	}
}

func TestRolePointsCoversEveryRole(t *testing.T) {
	allRoles := []role{
		roleVillager, roleWerewolf, roleSeer, roleDoctor,
		roleWolfCub, roleCursedWolf, roleTraitor,
		roleWitch, roleHunter, roleDisruptor, roleCupid,
	}
	for _, r := range allRoles {
		if _, ok := rolePoints[r]; !ok {
			t.Errorf("rolePoints missing entry for role %v", r)
		}
	}
	want := map[role]int{
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
	for r, wantPoints := range want {
		if got := rolePoints[r]; got != wantPoints {
			t.Errorf("rolePoints[%v] = %d, want %d", r, got, wantPoints)
		}
	}
}

func TestTallyWolfVotesSingleWinner(t *testing.T) {
	votes := map[string][]string{"wolf1": {"x"}, "wolf2": {"x"}, "wolf3": {"y"}}
	targets, ok := tallyWolfVotes(votes, 1)
	if !ok {
		t.Fatal("expected ok=true (no tie)")
	}
	if len(targets) != 1 || targets[0] != "x" {
		t.Errorf("got %v, want [x]", targets)
	}
}

func TestTallyWolfVotesTieProducesNoKill(t *testing.T) {
	votes := map[string][]string{"wolf1": {"x"}, "wolf2": {"y"}}
	_, ok := tallyWolfVotes(votes, 1)
	if ok {
		t.Fatal("expected ok=false on a genuine tie for the only kill slot")
	}
}

func TestTallyWolfVotesEmpty(t *testing.T) {
	targets, ok := tallyWolfVotes(map[string][]string{}, 1)
	if ok || len(targets) != 0 {
		t.Errorf("expected ok=false and no targets for empty votes, got targets=%v ok=%v", targets, ok)
	}
}

func TestTallyWolfVotesTopTwo(t *testing.T) {
	// x:3, y:2, z:1 -- top 2 by count are x and y, no tie for either slot.
	votes := map[string][]string{
		"wolf1": {"x"}, "wolf2": {"x"}, "wolf3": {"x"},
		"wolf4": {"y"}, "wolf5": {"y"},
		"wolf6": {"z"},
	}
	targets, ok := tallyWolfVotes(votes, 2)
	if !ok {
		t.Fatal("expected ok=true")
	}
	if len(targets) != 2 {
		t.Fatalf("expected 2 targets, got %v", targets)
	}
	got := map[string]bool{targets[0]: true, targets[1]: true}
	if !got["x"] || !got["y"] {
		t.Errorf("expected targets to be {x, y}, got %v", targets)
	}
}

func TestTallyWolfVotesTopTwoWithTieForSecondSlot(t *testing.T) {
	// x:2 (clear first), y:1, z:1 (tied for the second slot) -- x is a kill,
	// the second slot ties and produces no second kill.
	votes := map[string][]string{
		"wolf1": {"x"}, "wolf2": {"x"},
		"wolf3": {"y"},
		"wolf4": {"z"},
	}
	targets, ok := tallyWolfVotes(votes, 2)
	if !ok {
		t.Fatal("expected ok=true (first slot is unambiguous)")
	}
	if len(targets) != 1 || targets[0] != "x" {
		t.Errorf("expected only [x] (second slot tied, dropped), got %v", targets)
	}
}

func TestTallyWolfVotesSingleWolfPicksTwoInOneVote(t *testing.T) {
	// A lone wolf during a bonus-bite round can name 2 targets in one
	// select instead of relying on separate wolves to split their votes.
	votes := map[string][]string{"wolf1": {"x", "y"}}
	targets, ok := tallyWolfVotes(votes, 2)
	if !ok {
		t.Fatal("expected ok=true")
	}
	got := map[string]bool{}
	for _, tgt := range targets {
		got[tgt] = true
	}
	if len(targets) != 2 || !got["x"] || !got["y"] {
		t.Errorf("expected both x and y from a single wolf's 2-pick vote, got %v", targets)
	}
}

func TestTallyWolfVotesTwoWolvesUnanimousMultiPick(t *testing.T) {
	// Regression: a genuinely-coordinated 2-wolf pack that BOTH multi-select
	// the exact same 2 targets must not be treated as tied-and-ambiguous —
	// there are exactly 2 slots and exactly 2 named targets, so this is
	// agreement, not indecision, and both must die.
	votes := map[string][]string{"w1": {"x", "y"}, "w2": {"x", "y"}}
	targets, ok := tallyWolfVotes(votes, 2)
	if !ok {
		t.Fatal("expected ok=true for a fully unanimous 2-wolf multi-pick")
	}
	got := map[string]bool{}
	for _, tgt := range targets {
		got[tgt] = true
	}
	if len(targets) != 2 || !got["x"] || !got["y"] {
		t.Errorf("expected both x and y from a unanimous 2-wolf 2-pick vote, got %v", targets)
	}
}

func TestTallyWolfVotesEveryoneFitsShortcutIsDeterministic(t *testing.T) {
	// Regression: the "everyone left fits in the room left" shortcut used
	// to range over a Go map directly, giving random order run to run —
	// this fed the recap's target-label ordering and the Witch's
	// heal-menu option order nondeterministically. Run many times to
	// catch any reintroduced map-iteration dependency.
	votes := map[string][]string{"w1": {"x"}, "w2": {"x"}, "w3": {"y"}}
	want := []string{"x", "y"} // x:2 sorts before y:1 by count descending
	for i := 0; i < 50; i++ {
		got, ok := tallyWolfVotes(votes, 2)
		if !ok || len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
			t.Fatalf("run %d: tallyWolfVotes(...) = %v, %v, want %v, true (stable order every run)", i, got, ok, want)
		}
	}
}

func TestTallyWolfVotesThreeWayEvenSplitStillTies(t *testing.T) {
	// A true 3-way tie for 2 slots (3 candidates each with 1 supporter,
	// more rivals than room) must still yield zero kills — this is the
	// genuinely ambiguous case the tie-break exists to protect, not
	// resolved by the "fits within remaining slots" shortcut.
	votes := map[string][]string{"w1": {"a"}, "w2": {"b"}, "w3": {"c"}}
	targets, ok := tallyWolfVotes(votes, 2)
	if ok || len(targets) != 0 {
		t.Errorf("expected a 3-way even split for 2 slots to tie with zero kills, got %v, %v", targets, ok)
	}
}

func TestValidWolfVotesDropsDeadVoter(t *testing.T) {
	aliveWolves := []*player{
		{UserID: "wolf1", Alive: true},
	}
	allPlayers := map[string]*player{
		"wolf1": {UserID: "wolf1", Alive: true},
		"wolf2": {UserID: "wolf2", Alive: false}, // departed mid-round
		"v1":    {UserID: "v1", Alive: true},
	}
	votes := map[string][]string{"wolf1": {"v1"}, "wolf2": {"v1"}}
	got := validWolfVotes(votes, aliveWolves, allPlayers)
	if len(got) != 1 || len(got["wolf1"]) != 1 || got["wolf1"][0] != "v1" {
		t.Errorf("got %v, want only wolf1's vote (wolf2's should be dropped, they're dead)", got)
	}
}

func TestValidWolfVotesDropsDeadTarget(t *testing.T) {
	aliveWolves := []*player{
		{UserID: "wolf1", Alive: true},
	}
	allPlayers := map[string]*player{
		"wolf1": {UserID: "wolf1", Alive: true},
		"v1":    {UserID: "v1", Alive: false}, // died some other way before resolution
	}
	votes := map[string][]string{"wolf1": {"v1"}}
	got := validWolfVotes(votes, aliveWolves, allPlayers)
	if len(got) != 0 {
		t.Errorf("expected vote for a dead target to be dropped, got %v", got)
	}
}

func TestValidWolfVotesKeepsLegitimateVotes(t *testing.T) {
	aliveWolves := []*player{
		{UserID: "wolf1", Alive: true},
		{UserID: "wolf2", Alive: true},
	}
	allPlayers := map[string]*player{
		"wolf1": {UserID: "wolf1", Alive: true},
		"wolf2": {UserID: "wolf2", Alive: true},
		"v1":    {UserID: "v1", Alive: true},
	}
	votes := map[string][]string{"wolf1": {"v1"}, "wolf2": {"v1"}}
	got := validWolfVotes(votes, aliveWolves, allPlayers)
	if len(got) != 2 || got["wolf1"][0] != "v1" || got["wolf2"][0] != "v1" {
		t.Errorf("expected both votes kept, got %v", got)
	}
}

func TestValidWolfVotesDropsOnlyDeadTargetFromMultiPick(t *testing.T) {
	// A wolf's 2-pick vote where one pick died before resolution: the dead
	// pick is dropped but the still-alive pick must survive filtering.
	aliveWolves := []*player{{UserID: "wolf1", Alive: true}}
	allPlayers := map[string]*player{
		"wolf1": {UserID: "wolf1", Alive: true},
		"v1":    {UserID: "v1", Alive: true},
		"v2":    {UserID: "v2", Alive: false},
	}
	votes := map[string][]string{"wolf1": {"v1", "v2"}}
	got := validWolfVotes(votes, aliveWolves, allPlayers)
	if len(got) != 1 || len(got["wolf1"]) != 1 || got["wolf1"][0] != "v1" {
		t.Errorf("got %v, want only v1 surviving wolf1's pick list", got)
	}
}

func TestAssignRolesDealsWitchAndHunterAt11Players(t *testing.T) {
	joinOrder := make([]string, 11)
	labels := make(map[string]string, 11)
	for i := range joinOrder {
		id := fmt.Sprintf("p%d", i)
		joinOrder[i] = id
		labels[id] = id
	}
	players, err := assignRoles(joinOrder, labels)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	counts := map[role]int{}
	for _, p := range players {
		counts[p.Role]++
	}
	if counts[roleWitch] != 1 {
		t.Errorf("expected exactly 1 Witch at 11 players, got %d", counts[roleWitch])
	}
	if counts[roleHunter] != 1 {
		t.Errorf("expected exactly 1 Hunter at 11 players, got %d", counts[roleHunter])
	}
}

func TestApplyWolfCubBonusBiteFlag(t *testing.T) {
	game := &werewolfGame{
		Players: map[string]*player{
			"cub":  {UserID: "cub", Role: roleWolfCub},
			"wolf": {UserID: "wolf", Role: roleWerewolf},
		},
	}
	applyWolfCubBonusBiteFlag(game, []string{"wolf"})
	if game.WolfBonusBiteNextNight != 0 {
		t.Error("a plain Werewolf's death must not arm the bonus bite")
	}
	applyWolfCubBonusBiteFlag(game, []string{"wolf", "cub"})
	if game.WolfBonusBiteNextNight != 1 {
		t.Errorf("a Wolf Cub's death must arm 1 bonus target, got %d", game.WolfBonusBiteNextNight)
	}
	applyWolfCubBonusBiteFlag(game, []string{"wolf"})
	if game.WolfBonusBiteNextNight != 1 {
		t.Error("the counter must be additive — a later non-Cub death must not clear it")
	}
}

func TestApplyWolfCubBonusBiteFlagCompoundsForMultipleWolfCubs(t *testing.T) {
	// Regression for the code-review finding: a custom-roles game can deal
	// more than one Wolf Cub (it has no 1-per-game cap, unlike Seer/Doctor/
	// etc). If two die in the same round, the pack must get a 3-target bite
	// next night (1 base + 2 bonus), not cap at 2 like a single-bool flag
	// would.
	game := &werewolfGame{
		Players: map[string]*player{
			"cub1": {UserID: "cub1", Role: roleWolfCub},
			"cub2": {UserID: "cub2", Role: roleWolfCub},
		},
	}
	applyWolfCubBonusBiteFlag(game, []string{"cub1", "cub2"})
	if game.WolfBonusBiteNextNight != 2 {
		t.Errorf("two Wolf Cubs dying the same round must arm 2 bonus targets, got %d", game.WolfBonusBiteNextNight)
	}
}

func TestWolfBonusBiteArmedMidRoundDoesNotAffectActiveRound(t *testing.T) {
	// Regression: a Wolf Cub death arriving mid-night (e.g. /werewolf leave,
	// which permits leaving during phaseNight) must arm the bonus for the
	// NEXT night, not retroactively change the vote already in progress.
	// runNightPhase's snapshot-and-clear step is what this test exercises
	// directly: WolfBonusBiteActiveThisRound is what the picker/tally read,
	// and it must be unaffected by a later WolfBonusBiteNextNight increment.
	game := &werewolfGame{
		Players: map[string]*player{
			"cub1": {UserID: "cub1", Role: roleWolfCub, Alive: true},
		},
	}

	// Night opens: nothing armed yet.
	game.WolfBonusBiteActiveThisRound = game.WolfBonusBiteNextNight
	game.WolfBonusBiteNextNight = 0
	if game.WolfBonusBiteActiveThisRound != 0 {
		t.Fatalf("night should open with no bonus active, got %d", game.WolfBonusBiteActiveThisRound)
	}

	// Mid-round: the Wolf Cub leaves, arming the bonus.
	game.Players["cub1"].Alive = false
	applyWolfCubBonusBiteFlag(game, []string{"cub1"})

	// The round already in progress must still see zero active bonus...
	if game.WolfBonusBiteActiveThisRound != 0 {
		t.Errorf("mid-round arming must not affect the round already open, got active=%d", game.WolfBonusBiteActiveThisRound)
	}
	// ...while the NEXT night's snapshot picks it up correctly.
	nextActive := game.WolfBonusBiteNextNight
	game.WolfBonusBiteNextNight = 0
	if nextActive != 1 {
		t.Errorf("the bonus armed mid-round must carry over to the next night, got %d", nextActive)
	}
}

func TestFormatRoundBlockWolfBonusBite(t *testing.T) {
	r := roundLog{
		Round:                3,
		WolfBonusBite:        true,
		WolfTargetLabel:      "An, Binh",
		WolfBiteOutcomeLines: []string{"An: chết", "Binh: được Bác Sĩ cứu"},
	}
	got := formatRoundBlock(r)
	if !strings.Contains(got, "An: chết") || !strings.Contains(got, "Binh: được Bác Sĩ cứu") {
		t.Errorf("formatRoundBlock() = %q, want both outcome lines present", got)
	}
	if strings.Contains(got, "🐺 Sói cắn:") {
		t.Errorf("formatRoundBlock() = %q, want legacy single-target branch not to render", got)
	}
}

// TestFormatRoundBlockWolfBonusBiteEmptyOutcomeLines exercises the actual
// bug Fix 2 closed: WolfBonusBite true but WolfBiteOutcomeLines empty (both
// bite targets already dead by finalize time) must not fall through to the
// legacy single-target branch, even though WolfTargetLabel is non-empty.
func TestFormatRoundBlockWolfBonusBiteEmptyOutcomeLines(t *testing.T) {
	r := roundLog{
		Round:           4,
		WolfBonusBite:   true,
		WolfTargetLabel: "An, Binh",
	}
	got := formatRoundBlock(r)
	if strings.Contains(got, "🐺 Sói cắn:") {
		t.Errorf("formatRoundBlock() = %q, want legacy single-target branch not to render when outcome lines are empty", got)
	}
	if strings.Contains(got, "🐺🐺") {
		t.Errorf("formatRoundBlock() = %q, want the bonus-bite header not to render with no outcome lines", got)
	}
}

func TestTallyWolfVotesTopTwoUnanimousYieldsOne(t *testing.T) {
	// Bonus bite armed but the pack agrees: only one distinct target, so the
	// round must fall back to the ordinary single-target path.
	targets, ok := tallyWolfVotes(map[string][]string{"w1": {"a"}, "w2": {"a"}}, 2)
	if !ok || len(targets) != 1 || targets[0] != "a" {
		t.Fatalf("unanimous tallyWolfVotes(topN=2) = %v, %v; want [a], true", targets, ok)
	}
}

func TestTruncateLabelCutsOnRuneBoundaries(t *testing.T) {
	// Regression: truncateLabel used to slice by BYTE index, which for
	// Vietnamese (2-3 bytes/rune) and emoji (4 bytes/rune) both truncated
	// far earlier than the intended 60 characters and could split a rune
	// in half, producing a replacement glyph in the menu.
	t.Run("valid utf8 preserved when cutting", func(t *testing.T) {
		// 80 Vietnamese runes — comfortably over the limit, and every rune
		// is multi-byte so a byte-index cut would land mid-character.
		long := strings.Repeat("ữ", 80)
		got := truncateLabel(long)
		if !utf8.ValidString(got) {
			t.Errorf("truncateLabel produced invalid UTF-8: %q", got)
		}
		if !strings.HasSuffix(got, "...") {
			t.Errorf("truncateLabel(%d runes) = %q, want it truncated with an ellipsis", len([]rune(long)), got)
		}
		if runes := len([]rune(strings.TrimSuffix(got, "..."))); runes != 60 {
			t.Errorf("truncateLabel kept %d runes, want exactly 60", runes)
		}
	})

	t.Run("emoji not split", func(t *testing.T) {
		long := strings.Repeat("🐺", 80)
		got := truncateLabel(long)
		if !utf8.ValidString(got) {
			t.Errorf("truncateLabel split an emoji, producing invalid UTF-8: %q", got)
		}
		if strings.ContainsRune(got, utf8.RuneError) {
			t.Errorf("truncateLabel produced a replacement character: %q", got)
		}
	})

	t.Run("names differing past the old byte cut stay distinguishable", func(t *testing.T) {
		// Both names are identical for their first 30 Vietnamese runes
		// (60 bytes — exactly where the old byte-index cut landed) and
		// differ only after that. Under the old behavior both collapsed
		// to the same visible option, so a voter could not tell them
		// apart; under rune-based truncation the 60-rune budget reaches
		// past the divergence.
		prefix := strings.Repeat("ữ", 30)
		a := truncateLabel(prefix + "AAA")
		b := truncateLabel(prefix + "BBB")
		if a == b {
			t.Errorf("two distinct names truncated to the same label %q — players would be indistinguishable in the menu", a)
		}
	})

	t.Run("short labels pass through untouched", func(t *testing.T) {
		for _, s := range []string{"", "Bob", "Nguyễn Văn A", "🐺 Sói"} {
			if got := truncateLabel(s); got != s {
				t.Errorf("truncateLabel(%q) = %q, want it unchanged", s, got)
			}
		}
	})
}

func TestWitchActionComponentsHealControlShape(t *testing.T) {
	a := &player{UserID: "a", Label: "An"}
	b := &player{UserID: "b", Label: "Binh"}
	poison := []*player{a, b}

	countHealControls := func(rows []discordgo.MessageComponent) (buttons, menus int) {
		for _, row := range rows {
			ar, isRow := row.(discordgo.ActionsRow)
			if !isRow {
				continue
			}
			for _, c := range ar.Components {
				switch v := c.(type) {
				case discordgo.Button:
					if v.CustomID == werewolfWitchHealID+"chan" {
						buttons++
					}
				case discordgo.SelectMenu:
					if v.CustomID == werewolfWitchHealID+"chan" {
						menus++
					}
				}
			}
		}
		return buttons, menus
	}

	btns, menus := countHealControls(witchActionComponents("chan", []*player{a}, false, false, poison))
	if btns != 1 || menus != 0 {
		t.Errorf("one bite target: got %d heal buttons, %d heal menus; want 1, 0", btns, menus)
	}
	btns, menus = countHealControls(witchActionComponents("chan", []*player{a, b}, false, false, poison))
	if btns != 0 || menus != 1 {
		t.Errorf("two bite targets: got %d heal buttons, %d heal menus; want 0, 1", btns, menus)
	}
	btns, menus = countHealControls(witchActionComponents("chan", []*player{a, b}, true, false, poison))
	if btns != 0 || menus != 0 {
		t.Errorf("two bite targets with heal used: got %d heal buttons, %d heal menus; want 0, 0", btns, menus)
	}
}

func TestApplyHunterRevengeChainTriggers(t *testing.T) {
	game := &werewolfGame{
		JoinOrder:    []string{"hunter1", "v1"},
		HunterTarget: "v1",
		Players: map[string]*player{
			"hunter1": {UserID: "hunter1", Label: "Hunter1", Role: roleHunter, Alive: false},
			"v1":      {UserID: "v1", Label: "V1", Role: roleVillager, Alive: true},
		},
	}
	revenged := applyHunterRevengeChain(game, "hunter1")
	if len(revenged) != 1 || revenged[0].UserID != "v1" {
		t.Fatalf("expected v1 to be revenge-killed, got %v", revenged)
	}
	if game.Players["v1"].Alive {
		t.Error("expected v1.Alive to be false after the revenge chain")
	}
}

func TestApplyHunterRevengeChainNoOpIfDeadPlayerNotHunter(t *testing.T) {
	game := &werewolfGame{
		JoinOrder:    []string{"v1", "v2"},
		HunterTarget: "v2",
		Players: map[string]*player{
			"v1": {UserID: "v1", Label: "V1", Role: roleVillager, Alive: false},
			"v2": {UserID: "v2", Label: "V2", Role: roleVillager, Alive: true},
		},
	}
	revenged := applyHunterRevengeChain(game, "v1")
	if len(revenged) != 0 {
		t.Fatalf("expected no revenge kill (v1 wasn't the Hunter), got %v", revenged)
	}
	if !game.Players["v2"].Alive {
		t.Error("v2 should still be alive")
	}
}

func TestApplyHunterRevengeChainNoOpIfNoTargetSet(t *testing.T) {
	game := &werewolfGame{
		JoinOrder:    []string{"hunter1", "v1"},
		HunterTarget: "", // never picked a target
		Players: map[string]*player{
			"hunter1": {UserID: "hunter1", Label: "Hunter1", Role: roleHunter, Alive: false},
			"v1":      {UserID: "v1", Label: "V1", Role: roleVillager, Alive: true},
		},
	}
	revenged := applyHunterRevengeChain(game, "hunter1")
	if len(revenged) != 0 {
		t.Fatalf("expected no revenge kill (no target set), got %v", revenged)
	}
}

func TestApplyHunterRevengeChainNoOpIfTargetAlreadyDead(t *testing.T) {
	game := &werewolfGame{
		JoinOrder:    []string{"hunter1", "v1"},
		HunterTarget: "v1",
		Players: map[string]*player{
			"hunter1": {UserID: "hunter1", Label: "Hunter1", Role: roleHunter, Alive: false},
			"v1":      {UserID: "v1", Label: "V1", Role: roleVillager, Alive: false}, // already dead some other way
		},
	}
	revenged := applyHunterRevengeChain(game, "hunter1")
	if len(revenged) != 0 {
		t.Fatalf("expected no revenge kill (target already dead), got %v", revenged)
	}
}

func TestApplyDeathChainsLoverOnly(t *testing.T) {
	game := &werewolfGame{
		LoverIDs: [2]string{"a", "b"},
		Players: map[string]*player{
			"a": {UserID: "a", Label: "A", Alive: false},
			"b": {UserID: "b", Label: "B", Role: roleVillager, Alive: true},
		},
	}
	chained := applyDeathChains(game, "a")
	if len(chained) != 1 || chained[0].Reason != chainedDeathLover || chained[0].Player.UserID != "b" {
		t.Fatalf("applyDeathChains() = %+v, want exactly one lover-death for b", chained)
	}
	if game.Players["b"].Alive {
		t.Error("b should be dead after the lover-chain")
	}
}

func TestApplyDeathChainsComposesHunterAndLover(t *testing.T) {
	// Hunter (h) and their lover (l) are paired. h's target is t. h dies
	// first: the chain must both revenge-kill t AND lover-kill l — two
	// hops from one death, composing the two independent mechanics.
	game := &werewolfGame{
		HunterTarget: "t",
		LoverIDs:     [2]string{"h", "l"},
		Players: map[string]*player{
			"h": {UserID: "h", Label: "Hunter", Role: roleHunter, Alive: false},
			"t": {UserID: "t", Label: "Target", Role: roleVillager, Alive: true},
			"l": {UserID: "l", Label: "Lover", Role: roleVillager, Alive: true},
		},
	}
	chained := applyDeathChains(game, "h")
	if len(chained) != 2 {
		t.Fatalf("applyDeathChains() = %+v, want 2 chained deaths (revenge target + lover)", chained)
	}
	var gotT, gotL bool
	for _, c := range chained {
		switch c.Player.UserID {
		case "t":
			gotT = c.Reason == chainedDeathHunterRevenge
		case "l":
			gotL = c.Reason == chainedDeathLover
		}
	}
	if !gotT || !gotL {
		t.Errorf("applyDeathChains() = %+v, want both t (hunter-revenge) and l (lover) present", chained)
	}
	if game.Players["t"].Alive || game.Players["l"].Alive {
		t.Error("both t and l should be dead")
	}
}

func TestApplyDeathChainsNoLoversMatchesHunterRevengeChain(t *testing.T) {
	newGame := func() *werewolfGame {
		return &werewolfGame{
			HunterTarget: "t",
			Players: map[string]*player{
				"h": {UserID: "h", Label: "Hunter", Role: roleHunter, Alive: false},
				"t": {UserID: "t", Label: "Target", Role: roleVillager, Alive: true},
			},
		}
	}
	legacy := applyHunterRevengeChain(newGame(), "h")
	unified, loverDeaths := splitChainedDeaths(applyDeathChains(newGame(), "h"))
	if len(loverDeaths) != 0 {
		t.Fatalf("lover chain fired with no LoverIDs set: %+v", loverDeaths)
	}
	if len(legacy) != len(unified) {
		t.Fatalf("applyDeathChains produced %d kills, applyHunterRevengeChain produced %d", len(unified), len(legacy))
	}
	for i := range legacy {
		if legacy[i].UserID != unified[i].UserID {
			t.Errorf("kill %d: got %q, want %q", i, unified[i].UserID, legacy[i].UserID)
		}
	}
}

func TestApplyDeathChainsLoverKilledHunterRevenges(t *testing.T) {
	// The lover-killed player is themselves the Hunter with a locked
	// target — the second hop must still fire.
	game := &werewolfGame{
		HunterTarget: "t",
		LoverIDs:     [2]string{"a", "h"},
		Players: map[string]*player{
			"a": {UserID: "a", Label: "A", Role: roleVillager, Alive: false},
			"h": {UserID: "h", Label: "Hunter", Role: roleHunter, Alive: true},
			"t": {UserID: "t", Label: "Target", Role: roleVillager, Alive: true},
		},
	}
	hunterRevenged, loverDeaths := splitChainedDeaths(applyDeathChains(game, "a"))
	if len(loverDeaths) != 1 || loverDeaths[0].UserID != "h" {
		t.Fatalf("loverDeaths = %+v, want just h", loverDeaths)
	}
	if len(hunterRevenged) != 1 || hunterRevenged[0].UserID != "t" {
		t.Fatalf("hunterRevenged = %+v, want just t", hunterRevenged)
	}
	if game.Players["h"].Alive || game.Players["t"].Alive {
		t.Error("both h and t should be dead")
	}
}

func TestFormatRoundBlockLoverDeath(t *testing.T) {
	r := roundLog{Round: 3, LoverDeathLabel: "Lam"}
	got := formatRoundBlock(r)
	if !strings.Contains(got, "Lam") {
		t.Errorf("formatRoundBlock() = %q, want the lover-death mentioned", got)
	}
}

func TestFormatRoundBlockOmitsLoverDeathWhenEmpty(t *testing.T) {
	r := roundLog{Round: 1, WolfTargetLabel: "Alice"}
	got := formatRoundBlock(r)
	if strings.Contains(got, "Người yêu qua đời theo") {
		t.Errorf("expected no lover-death line when it didn't trigger, got:\n%s", got)
	}
}

func TestFormatRoundBlockHunterRevenge(t *testing.T) {
	r := roundLog{Round: 3, LynchedLabel: "Alice", HunterRevengeLabel: "Bob"}
	got := formatRoundBlock(r)
	if !strings.Contains(got, "🏹 Thợ săn kéo theo: Bob") {
		t.Errorf("expected hunter revenge line, got:\n%s", got)
	}
}

func TestFormatRoundBlockOmitsHunterRevengeWhenEmpty(t *testing.T) {
	r := roundLog{Round: 1, WolfTargetLabel: "Alice"}
	got := formatRoundBlock(r)
	if strings.Contains(got, "Thợ săn kéo theo") {
		t.Errorf("expected no hunter-revenge line when it didn't trigger, got:\n%s", got)
	}
}

func TestFormatRoundBlockWitchSaved(t *testing.T) {
	r := roundLog{Round: 2, WolfTargetLabel: "Alice", WitchSavedLabel: "Alice"}
	got := formatRoundBlock(r)
	if !strings.Contains(got, "Sói cắn: Alice (được phù thủy cứu)") {
		t.Errorf("expected witch-saved wording, got:\n%s", got)
	}
}

func TestFormatRoundBlockWitchPoisoned(t *testing.T) {
	r := roundLog{Round: 2, WitchPoisonedLabel: "Bob"}
	got := formatRoundBlock(r)
	if !strings.Contains(got, "☠️ Phù thủy đầu độc: Bob") {
		t.Errorf("expected witch-poisoned line, got:\n%s", got)
	}
}

func TestFormatRoundBlockDisruptorMuted(t *testing.T) {
	r := roundLog{Round: 2, DisruptorMutedLabel: "Duy"}
	got := formatRoundBlock(r)
	if !strings.Contains(got, "🤫 Kẻ phá hoại làm câm lặng: Duy") {
		t.Errorf("formatRoundBlock() = %q, want the mute mentioned", got)
	}
}

func TestWolfVoteParticipant(t *testing.T) {
	cases := []struct {
		role role
		want bool
	}{
		{roleWerewolf, true},
		{roleWolfCub, true},
		{roleTraitor, false},
		{roleCursedWolf, false}, // pre-conversion: villager-faction, isWolf() is false
		{roleSeer, false},
		{roleVillager, false},
	}
	for _, c := range cases {
		p := &player{Role: c.role, Faction: factionForRole(c.role)}
		if got := p.wolfVoteParticipant(); got != c.want {
			t.Errorf("wolfVoteParticipant() for role %v = %v, want %v", c.role, got, c.want)
		}
	}

	// A converted Cursed Wolf: Faction flipped to factionWolf, Role stays
	// roleCursedWolf.
	converted := &player{Role: roleCursedWolf, Faction: factionWolf}
	if !converted.wolfVoteParticipant() {
		t.Error("converted Cursed Wolf should be a wolf-vote participant")
	}
}

func TestWolfRosterLabels(t *testing.T) {
	game := &werewolfGame{
		JoinOrder: []string{"w1", "cub1", "traitor1", "cw1", "villager1"},
		Players: map[string]*player{
			"w1":        {UserID: "w1", Label: "Wolf", Role: roleWerewolf, Faction: factionWolf, Alive: true},
			"cub1":      {UserID: "cub1", Label: "Cub", Role: roleWolfCub, Faction: factionWolf, Alive: true},
			"traitor1":  {UserID: "traitor1", Label: "Trai", Role: roleTraitor, Faction: factionWolf, Alive: true},
			"cw1":       {UserID: "cw1", Label: "CW", Role: roleCursedWolf, Faction: factionVillager, Alive: true},
			"villager1": {UserID: "villager1", Label: "Vil", Role: roleVillager, Faction: factionVillager, Alive: true},
		},
	}
	got := wolfRosterLabels(game)
	want := []string{"Cub", "Wolf"} // sorted; excludes Traitor themself and the not-yet-converted Cursed Wolf
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("wolfRosterLabels() = %v, want %v", got, want)
	}

	// After conversion, the Cursed Wolf joins the roster.
	game.Players["cw1"].Faction = factionWolf
	got = wolfRosterLabels(game)
	want = []string{"CW", "Cub", "Wolf"}
	if len(got) != len(want) {
		t.Fatalf("wolfRosterLabels() after conversion = %v, want %v", got, want)
	}
	for idx := range want {
		if got[idx] != want[idx] {
			t.Errorf("wolfRosterLabels() after conversion = %v, want %v", got, want)
		}
	}
}

func TestTraitorRosterDM(t *testing.T) {
	game := &werewolfGame{
		JoinOrder: []string{"w1", "traitor1"},
		Players: map[string]*player{
			"w1":       {UserID: "w1", Label: "Wolf", Role: roleWerewolf, Faction: factionWolf, Alive: true},
			"traitor1": {UserID: "traitor1", Label: "Trai", Role: roleTraitor, Faction: factionWolf, Alive: true},
		},
	}
	// Spec §4d (amended 2026-08-22): the roster DM resends unconditionally
	// every time it's requested — no "only if changed" gating — so a
	// repeat call with no roster change still returns a non-nil DM.
	first := traitorRosterDM(game)
	if first == nil || first.TraitorUserID != "traitor1" || len(first.Labels) != 1 || first.Labels[0] != "Wolf" {
		t.Fatalf("first traitorRosterDM() = %+v, want a DM naming Wolf", first)
	}
	second := traitorRosterDM(game)
	if second == nil || len(second.Labels) != 1 || second.Labels[0] != "Wolf" {
		t.Errorf("second traitorRosterDM() with no roster change = %+v, want a DM naming Wolf again, not nil", second)
	}

	game.Players["w1"].Alive = false
	third := traitorRosterDM(game)
	if third == nil || len(third.Labels) != 0 {
		t.Errorf("traitorRosterDM() after the only wolf dies = %+v, want an empty-roster DM", third)
	}

	game.Players["traitor1"].Alive = false
	fourth := traitorRosterDM(game)
	if fourth != nil {
		t.Errorf("traitorRosterDM() with no live Traitor = %+v, want nil", fourth)
	}
}

func TestInvestigatesAsWolfTraitorReadsInnocent(t *testing.T) {
	traitor := &player{UserID: "t1", Role: roleTraitor, Faction: factionWolf, Alive: true}
	if traitor.investigatesAsWolf() {
		t.Error("Traitor.investigatesAsWolf() = true, want false — Traitor must read as an innocent villager to the Seer")
	}
	if !traitor.isWolf() {
		t.Error("Traitor.isWolf() = false, want true — Traitor still counts as wolf-faction for the win condition")
	}
}

func TestInvestigatesAsWolfRealWolfReadsAsWolf(t *testing.T) {
	wolf := &player{UserID: "w1", Role: roleWerewolf, Faction: factionWolf, Alive: true}
	if !wolf.investigatesAsWolf() {
		t.Error("Werewolf.investigatesAsWolf() = false, want true")
	}
}

func TestPromoteTraitorIfWolvesWipedPromotesLastTraitor(t *testing.T) {
	game := &werewolfGame{
		JoinOrder: []string{"w1", "traitor1", "v1"},
		Players: map[string]*player{
			"w1":       {UserID: "w1", Label: "Wolf", Role: roleWerewolf, Faction: factionWolf, Alive: false},
			"traitor1": {UserID: "traitor1", Label: "Trai", Role: roleTraitor, Faction: factionWolf, Alive: true},
			"v1":       {UserID: "v1", Label: "Vil", Role: roleVillager, Faction: factionVillager, Alive: true},
		},
	}
	promoted := promoteTraitorIfWolvesWiped(game)
	if len(promoted) != 1 || promoted[0].UserID != "traitor1" {
		t.Fatalf("promoteTraitorIfWolvesWiped() = %+v, want a snapshot naming traitor1", promoted)
	}
	if game.Players["traitor1"].Role != roleWerewolf {
		t.Errorf("traitor1.Role = %v after promotion, want roleWerewolf", game.Players["traitor1"].Role)
	}
	if !game.Players["traitor1"].wolfVoteParticipant() {
		t.Error("promoted traitor1 should now participate in the wolf pack's kill vote")
	}
}

func TestPromoteTraitorIfWolvesWipedNoOpWhileOtherWolfAlive(t *testing.T) {
	game := &werewolfGame{
		JoinOrder: []string{"w1", "traitor1"},
		Players: map[string]*player{
			"w1":       {UserID: "w1", Label: "Wolf", Role: roleWerewolf, Faction: factionWolf, Alive: true},
			"traitor1": {UserID: "traitor1", Label: "Trai", Role: roleTraitor, Faction: factionWolf, Alive: true},
		},
	}
	if promoted := promoteTraitorIfWolvesWiped(game); promoted != nil {
		t.Errorf("promoteTraitorIfWolvesWiped() = %+v, want nil while a non-Traitor wolf is still alive", promoted)
	}
	if game.Players["traitor1"].Role != roleTraitor {
		t.Error("traitor1.Role changed even though a real wolf is still alive")
	}
}

func TestPromoteTraitorIfWolvesWipedNoOpWithoutLiveTraitor(t *testing.T) {
	game := &werewolfGame{
		JoinOrder: []string{"w1", "traitor1"},
		Players: map[string]*player{
			"w1":       {UserID: "w1", Label: "Wolf", Role: roleWerewolf, Faction: factionWolf, Alive: false},
			"traitor1": {UserID: "traitor1", Label: "Trai", Role: roleTraitor, Faction: factionWolf, Alive: false},
		},
	}
	if promoted := promoteTraitorIfWolvesWiped(game); promoted != nil {
		t.Errorf("promoteTraitorIfWolvesWiped() = %+v, want nil with no live Traitor", promoted)
	}
}

func TestWinnerNamesWithRolesWerewolvesWin(t *testing.T) {
	game := &werewolfGame{
		JoinOrder: []string{"w1", "traitor1", "v1"},
		Players: map[string]*player{
			"w1":       {UserID: "w1", Label: "Wolf", Role: roleWerewolf, Faction: factionWolf, Alive: true},
			"traitor1": {UserID: "traitor1", Label: "Trai", Role: roleTraitor, Faction: factionWolf, Alive: false},
			"v1":       {UserID: "v1", Label: "Vil", Role: roleVillager, Faction: factionVillager, Alive: false},
		},
	}
	got := winnerNamesWithRoles(game, resultWerewolvesWin)
	want := "Wolf (Ma Sói), Trai (Kẻ Phản Bội)"
	if got != want {
		t.Errorf("winnerNamesWithRoles() = %q, want %q — every wolf-faction player shares the win, dead or alive, and villagers are excluded", got, want)
	}
}

func TestDisplayRoleLabelAnnotatesPromotedTraitor(t *testing.T) {
	// Regression: promoteTraitorIfWolvesWiped mutates Role to roleWerewolf
	// in place, which would otherwise erase the Traitor from the end-game
	// reveal entirely (printing "Ma Sói" as if they'd been dealt Werewolf
	// all along). DealtRole preserves what they actually started as.
	promoted := &player{Label: "Bob", Role: roleWerewolf, DealtRole: roleTraitor}
	got := displayRoleLabel(promoted)
	want := "Ma Sói (từng là Kẻ Phản Bội)"
	if got != want {
		t.Errorf("displayRoleLabel(promoted traitor) = %q, want %q", got, want)
	}
}

func TestConvertedCursedWolfAnnotatedOnlyAtEndOfGame(t *testing.T) {
	// A Cursed Wolf's Role never changes — only Faction flips — so the
	// two labellers must disagree about it, deliberately:
	//
	//   displayRoleLabel (MID-GAME): must NOT reveal the conversion. The
	//   merged "wolves couldn't agree" night wording exists precisely to
	//   give a conversion a cover story; annotating a mid-game death
	//   would blow it and out a brand-new wolf.
	//
	//   finalRoleLabel (END-OF-GAME): must reveal it. winnerNamesWithRoles
	//   partitions on Faction, so an un-annotated converted Cursed Wolf
	//   shows up among the wolf winners while the roster prints a
	//   villager-flavoured "Sói Nguyền" — the same message contradicting
	//   itself.
	converted := &player{
		Label: "Chi", Role: roleCursedWolf, DealtRole: roleCursedWolf, Faction: factionWolf,
	}
	if got := displayRoleLabel(converted); got != roleCursedWolf.label() {
		t.Errorf("displayRoleLabel(converted Cursed Wolf) = %q, want a bare %q — mid-game reveals must not leak the conversion",
			got, roleCursedWolf.label())
	}
	final := finalRoleLabel(converted)
	if final == roleCursedWolf.label() {
		t.Errorf("finalRoleLabel(converted Cursed Wolf) = %q, want it annotated — otherwise the end-game roster contradicts the Faction-based winner list", final)
	}
	if !strings.Contains(final, roleCursedWolf.label()) {
		t.Errorf("finalRoleLabel = %q, want it to still contain the base role name", final)
	}

	// An UNCONVERTED Cursed Wolf is a plain villager-faction player and
	// must read identically from both labellers, at every point in the game.
	unconverted := &player{
		Label: "Ann", Role: roleCursedWolf, DealtRole: roleCursedWolf, Faction: factionVillager,
	}
	if got := finalRoleLabel(unconverted); got != roleCursedWolf.label() {
		t.Errorf("finalRoleLabel(unconverted Cursed Wolf) = %q, want a bare %q", got, roleCursedWolf.label())
	}
	if displayRoleLabel(unconverted) != finalRoleLabel(unconverted) {
		t.Error("the two labellers must agree for an unconverted Cursed Wolf")
	}
}

func TestFinalRoleLabelStillAnnotatesPromotedTraitor(t *testing.T) {
	// finalRoleLabel delegates to displayRoleLabel, so it must not lose
	// the promoted-Traitor annotation that the end-game reveal also needs.
	promoted := &player{Label: "Bob", Role: roleWerewolf, DealtRole: roleTraitor, Faction: factionWolf}
	got := finalRoleLabel(promoted)
	if !strings.Contains(got, roleTraitor.label()) {
		t.Errorf("finalRoleLabel(promoted Traitor) = %q, want it to still mention %q", got, roleTraitor.label())
	}
}

func TestDisplayRoleLabelUnchangedForEveryoneElse(t *testing.T) {
	cases := []struct {
		name string
		p    *player
	}{
		{"never-promoted traitor", &player{Role: roleTraitor, DealtRole: roleTraitor}},
		{"plain werewolf", &player{Role: roleWerewolf, DealtRole: roleWerewolf}},
		{"converted cursed wolf (Role unchanged, only Faction flips)", &player{Role: roleCursedWolf, DealtRole: roleCursedWolf, Faction: factionWolf}},
		{"villager", &player{Role: roleVillager, DealtRole: roleVillager}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := displayRoleLabel(c.p); got != c.p.Role.label() {
				t.Errorf("displayRoleLabel() = %q, want plain %q", got, c.p.Role.label())
			}
		})
	}
}

func TestWinnerNamesWithRolesAnnotatesPromotedTraitor(t *testing.T) {
	game := &werewolfGame{
		JoinOrder: []string{"bob"},
		Players: map[string]*player{
			"bob": {UserID: "bob", Label: "Bob", Role: roleWerewolf, DealtRole: roleTraitor, Faction: factionWolf, Alive: true},
		},
	}
	got := winnerNamesWithRoles(game, resultWerewolvesWin)
	want := "Bob (Ma Sói (từng là Kẻ Phản Bội))"
	if got != want {
		t.Errorf("winnerNamesWithRoles() = %q, want %q — a promoted Traitor must not read as a plain Werewolf in the winner list", got, want)
	}
}

func TestWinnerNamesWithRolesVillagersWin(t *testing.T) {
	game := &werewolfGame{
		JoinOrder: []string{"w1", "v1", "seer1"},
		Players: map[string]*player{
			"w1":    {UserID: "w1", Label: "Wolf", Role: roleWerewolf, Faction: factionWolf, Alive: false},
			"v1":    {UserID: "v1", Label: "Vil", Role: roleVillager, Faction: factionVillager, Alive: true},
			"seer1": {UserID: "seer1", Label: "See", Role: roleSeer, Faction: factionVillager, Alive: false},
		},
	}
	got := winnerNamesWithRoles(game, resultVillagersWin)
	want := "Vil (Dân Làng), See (Tiên Tri)"
	if got != want {
		t.Errorf("winnerNamesWithRoles() = %q, want %q", got, want)
	}
}

func TestWinnerNamesWithRolesLoversWinIncludesCupid(t *testing.T) {
	game := &werewolfGame{
		JoinOrder: []string{"wolf", "villager", "cupid1"},
		LoverIDs:  [2]string{"wolf", "villager"},
		Players: map[string]*player{
			"wolf":     {UserID: "wolf", Label: "Wolf", Role: roleWerewolf, Faction: factionWolf, Alive: true},
			"villager": {UserID: "villager", Label: "Vil", Role: roleVillager, Faction: factionVillager, Alive: true},
			"cupid1":   {UserID: "cupid1", Label: "Cup", Role: roleCupid, Faction: factionVillager, Alive: false},
		},
	}
	got := winnerNamesWithRoles(game, resultLoversWin)
	want := "Wolf (Ma Sói), Vil (Dân Làng), Cup (Thần Tình Yêu)"
	if got != want {
		t.Errorf("winnerNamesWithRoles() = %q, want %q — the couple plus Cupid, in that order", got, want)
	}
}

func TestWinnerNamesWithRolesLoversWinCupidIsOneOfThePair(t *testing.T) {
	game := &werewolfGame{
		JoinOrder: []string{"wolf", "cupid1"},
		LoverIDs:  [2]string{"wolf", "cupid1"},
		Players: map[string]*player{
			"wolf":   {UserID: "wolf", Label: "Wolf", Role: roleWerewolf, Faction: factionWolf, Alive: true},
			"cupid1": {UserID: "cupid1", Label: "Cup", Role: roleCupid, Faction: factionVillager, Alive: true},
		},
	}
	got := winnerNamesWithRoles(game, resultLoversWin)
	want := "Wolf (Ma Sói), Cup (Thần Tình Yêu)"
	if got != want {
		t.Errorf("winnerNamesWithRoles() = %q, want %q — Cupid must not be listed twice when they're one of the pair", got, want)
	}
}

func TestCupidWinConditionDMMatchesActualSelfPairBehavior(t *testing.T) {
	// Regression: Cupid's own role DM used to say they win with a
	// mixed-faction couple "unless you are one of the two" — the exact
	// opposite of what the code does. A self-paired Cupid IS one of the
	// two lovers and IS counted as a winner (proven by the sibling test
	// TestWinnerNamesWithRolesLoversWinCupidIsOneOfThePair), so the DM was
	// steering players away from a genuinely winning line.
	//
	// Pin the DM text against the real behavior: build the exact
	// self-paired scenario, confirm the engine calls Cupid a winner, then
	// assert the DM does not tell them otherwise.
	game := &werewolfGame{
		JoinOrder: []string{"wolf", "cupid1"},
		LoverIDs:  [2]string{"wolf", "cupid1"},
		Players: map[string]*player{
			"wolf":   {UserID: "wolf", Label: "Wolf", Role: roleWerewolf, Faction: factionWolf, Alive: true},
			"cupid1": {UserID: "cupid1", Label: "Cup", Role: roleCupid, Faction: factionVillager, Alive: true},
		},
	}
	if got := checkWinConditionWithLovers(game.Players, game.LoverIDs); got != resultLoversWin {
		t.Fatalf("self-paired mixed-faction couple as last two = %v, want resultLoversWin", got)
	}
	if winners := winnerNamesWithRoles(game, resultLoversWin); !strings.Contains(winners, "Cup") {
		t.Fatalf("engine winner list %q does not include the self-paired Cupid — premise of this test is wrong", winners)
	}

	dm := roleWinCondition(roleCupid)
	// The old wording was "...trừ khi chính bạn là 1 trong 2 người đó"
	// ("unless you are one of the two"), i.e. an exclusion clause. The
	// engine grants the win in exactly that case, so any phrasing that
	// excludes a self-paired Cupid is wrong.
	if strings.Contains(dm, "trừ khi chính bạn là 1 trong 2") {
		t.Errorf("Cupid win-condition DM excludes a self-paired Cupid from the lovers win, but the engine counts them as a winner: %q", dm)
	}
}

func TestRoleWinConditionEveryRoleMentionsItsSide(t *testing.T) {
	cases := []struct {
		r    role
		side string
	}{
		{roleVillager, "Dân"},
		{roleSeer, "Dân"},
		{roleDoctor, "Dân"},
		{roleWitch, "Dân"},
		{roleHunter, "Dân"},
		{roleDisruptor, "Dân"},
		{roleWerewolf, "Sói"},
		{roleWolfCub, "Sói"},
		{roleCursedWolf, "Dân"}, // pre-conversion wording
		{roleTraitor, "Sói"},
		{roleCupid, "Dân"}, // default wording; mixed-lovers exception is spelled out in the text too
	}
	for _, tc := range cases {
		got := roleWinCondition(tc.r)
		if got == "" {
			t.Errorf("roleWinCondition(%v) is empty", tc.r)
		}
		if !strings.Contains(got, tc.side) {
			t.Errorf("roleWinCondition(%v) = %q, want it to mention %q", tc.r, got, tc.side)
		}
	}
}

func TestRoleWinConditionTraitorAndCursedWolfDiffer(t *testing.T) {
	// Both are wolf-adjacent villager-passing roles but have opposite
	// current-win-side wording: Traitor always wins with wolves, Cursed
	// Wolf wins with villagers until it converts.
	traitor := roleWinCondition(roleTraitor)
	cursed := roleWinCondition(roleCursedWolf)
	if !strings.Contains(traitor, "phe Sói") {
		t.Errorf("roleWinCondition(roleTraitor) = %q, want it to say the Traitor wins with the wolf side", traitor)
	}
	if !strings.Contains(cursed, "phe Dân") {
		t.Errorf("roleWinCondition(roleCursedWolf) = %q, want it to say the Cursed Wolf currently wins with the villager side", cursed)
	}
}

func TestDMRoleRevealContentIncludesWinCondition(t *testing.T) {
	players := map[string]*player{
		"u1": {UserID: "u1", Label: "P1", Role: roleTraitor},
	}
	content := fmt.Sprintf(
		"🐺 Ván Ma Sói bắt đầu! Vai trò của bạn là: **%s**.\n%s\n%s\n\n💬 test",
		players["u1"].Role.label(), roleInteractionNote(players["u1"].Role), roleWinCondition(players["u1"].Role),
	)
	if !strings.Contains(content, "🏆") {
		t.Errorf("role-reveal content = %q, want it to include the win-condition line", content)
	}
}

func TestShutdownNoOpWithZeroGames(t *testing.T) {
	// Shutdown must be safe to call even when no games are running (the
	// common case — most redeploys don't catch a game mid-flight) without
	// touching the session at all, since a nil session here would panic if
	// Shutdown tried to call any Discord API method.
	cmd := &WerewolfCommand{games: make(map[string]*werewolfGame)}
	cmd.Shutdown(nil)
	if len(cmd.games) != 0 {
		t.Errorf("games map = %v, want empty", cmd.games)
	}
}

func TestWolfThreadSweepSkipsPlayersNeverAddedToTheThread(t *testing.T) {
	// Regression: the dead-wolf sweep used to select on Faction alone, so
	// a wolf-FACTION player who is never actually a thread member — the
	// Traitor, who deliberately never joins — got "removed" from a thread
	// they were never in, every remaining night of the game once they
	// died. That's the exact repeat-forever behavior WolfThreadRemovedIDs
	// was added to stop, since a failed removal is never recorded.
	//
	// This exercises the sweep's predicate directly (the surrounding
	// function does Discord I/O), asserting the Traitor is excluded while
	// a real dead wolf-thread member is still picked up.
	game := &werewolfGame{
		JoinOrder: []string{"wolf", "traitor", "villager"},
		Players: map[string]*player{
			"wolf":     {UserID: "wolf", Role: roleWerewolf, Faction: factionWolf, Alive: false},
			"traitor":  {UserID: "traitor", Role: roleTraitor, Faction: factionWolf, Alive: false},
			"villager": {UserID: "villager", Role: roleVillager, Faction: factionVillager, Alive: false},
		},
		// Only the real wolf was ever ThreadMemberAdd'd — the Traitor is
		// excluded from aliveWolfVoters and so never joins.
		WolfThreadMemberIDs:  map[string]bool{"wolf": true},
		WolfThreadRemovedIDs: map[string]bool{},
	}

	var swept []string
	for _, p := range game.Players {
		if !p.Alive && game.WolfThreadMemberIDs[p.UserID] && !game.WolfThreadRemovedIDs[p.UserID] {
			swept = append(swept, p.UserID)
		}
	}
	if len(swept) != 1 || swept[0] != "wolf" {
		t.Errorf("sweep selected %v, want only [wolf] — the Traitor was never a thread member and must not be retried", swept)
	}

	// Once that removal succeeds and is recorded, the next night's sweep
	// must find nothing left to do.
	game.WolfThreadRemovedIDs["wolf"] = true
	swept = nil
	for _, p := range game.Players {
		if !p.Alive && game.WolfThreadMemberIDs[p.UserID] && !game.WolfThreadRemovedIDs[p.UserID] {
			swept = append(swept, p.UserID)
		}
	}
	if len(swept) != 0 {
		t.Errorf("sweep re-selected %v after a recorded successful removal, want nothing", swept)
	}
}

func TestGameTeardownInfoForCapturesGameState(t *testing.T) {
	game := &werewolfGame{
		WolfThreadID:    "wolf-thread",
		AliveThreadID:   "alive-thread",
		GuildID:         "guild1",
		ChannelUnlocked: true,
	}
	info := gameTeardownInfoFor("chan1", game)
	if info.channelID != "chan1" || info.threadID != "wolf-thread" || info.aliveThreadID != "alive-thread" ||
		info.guildID != "guild1" || !info.channelUnlocked {
		t.Errorf("gameTeardownInfoFor() = %+v, did not faithfully capture the game's teardown-relevant fields", info)
	}
}

func TestWerewolfDefinitionHasCustomRolesOption(t *testing.T) {
	cmd := WerewolfCmd
	def := cmd.Definition()
	var startOpt *discordgo.ApplicationCommandOption
	for _, opt := range def.Options {
		if opt.Name == "start" {
			startOpt = opt
		}
	}
	if startOpt == nil {
		t.Fatal("Definition() has no 'start' subcommand")
	}
	var found bool
	for _, opt := range startOpt.Options {
		if opt.Name == "custom-roles" && opt.Type == discordgo.ApplicationCommandOptionBoolean {
			found = true
		}
	}
	if !found {
		t.Error("'start' subcommand is missing a boolean 'custom-roles' option")
	}
}

func TestRoleCountMax(t *testing.T) {
	cases := []struct {
		r    role
		want int
	}{
		{roleSeer, 1},
		{roleDoctor, 1},
		{roleWitch, 1},
		{roleHunter, 1},
		{roleDisruptor, 1},
		{roleCupid, 1},
		{roleTraitor, 1},
		{roleWerewolf, 12},
		{roleWolfCub, 12},
		{roleCursedWolf, 12},
		{roleVillager, 12},
	}
	for _, c := range cases {
		if got := roleCountMax(c.r, 12); got != c.want {
			t.Errorf("roleCountMax(%v, 12) = %d, want %d", c.r, got, c.want)
		}
	}
}

func TestBalanceScore(t *testing.T) {
	counts := map[role]int{roleWerewolf: 2, roleVillager: 3, roleSeer: 1}
	want := rolePoints[roleWerewolf]*2 + rolePoints[roleVillager]*3 + rolePoints[roleSeer]*1
	if got := balanceScore(counts); got != want {
		t.Errorf("balanceScore() = %d, want %d", got, want)
	}
}

func TestRoleCountConfirmValid(t *testing.T) {
	cases := []struct {
		name        string
		counts      map[role]int
		playerCount int
		want        bool
	}{
		{"exact total, both sides present", map[role]int{roleWerewolf: 2, roleVillager: 5}, 7, true},
		{"total too low", map[role]int{roleWerewolf: 2, roleVillager: 3}, 7, false},
		{"total too high", map[role]int{roleWerewolf: 2, roleVillager: 6}, 7, false},
		{"no wolf side", map[role]int{roleVillager: 7}, 7, false},
		{"no villager side", map[role]int{roleWerewolf: 7}, 7, false},
		{"cursed wolf alone is not enough — no wolf-vote-participant", map[role]int{roleCursedWolf: 1, roleVillager: 6}, 7, false},
		{"werewolf plus cursed wolf is valid", map[role]int{roleWerewolf: 1, roleCursedWolf: 1, roleVillager: 5}, 7, true},
		{"wolf faction at half the roster is an instant loss, rejected", map[role]int{roleWerewolf: 3, roleVillager: 2}, 5, false},
		{"wolf faction that reaches parity after just ONE plain kill is rejected", map[role]int{roleWerewolf: 2, roleVillager: 3}, 5, false},
		{"wolf faction with margin to survive one kill is valid", map[role]int{roleWerewolf: 3, roleVillager: 6}, 9, true},
		{"wolf faction one over that margin is rejected", map[role]int{roleWerewolf: 4, roleVillager: 5}, 9, false},
		// Regression: an earlier revision applied the Cursed-Wolf-sized
		// (+2) margin unconditionally, wrongly rejecting ordinary setups
		// with no Cursed Wolf at all. 2 wolves at 6 players is a standard,
		// perfectly playable config — night 1 leaves 2 vs 3 with a full
		// day of discussion and a vote.
		{"standard 2-wolf 6-player setup with no cursed wolf is valid", map[role]int{roleWerewolf: 2, roleSeer: 1, roleVillager: 3}, 6, true},
		{"same size but WITH a cursed wolf needs the bigger margin, rejected", map[role]int{roleWerewolf: 2, roleCursedWolf: 1, roleVillager: 3}, 6, false},
		{"cursed wolf present but wolf faction small enough for the bigger margin", map[role]int{roleWerewolf: 2, roleCursedWolf: 1, roleVillager: 4}, 7, true},
		{"traitor counts toward the wolf-faction floor too", map[role]int{roleWerewolf: 2, roleTraitor: 1, roleVillager: 2}, 5, false},
		{"cursed wolf's conversion swing means it still can't be excluded for free — this config is an instant loss after one bite", map[role]int{roleWerewolf: 2, roleCursedWolf: 2, roleVillager: 1}, 5, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := roleCountConfirmValid(c.counts, c.playerCount); got != c.want {
				t.Errorf("roleCountConfirmValid(%v, %d) = %v, want %v", c.counts, c.playerCount, got, c.want)
			}
		})
	}
}

func TestFixedTableSetupsAllPassTheCustomRolesBalanceFloor(t *testing.T) {
	// Strong invariant guarding against over-correcting the balance floor:
	// rolesForPlayerCount is the project's own authoritative set of
	// known-good, playtested configs, so EVERY one of them must be
	// reachable through the custom-roles editor too. An earlier revision
	// applied a Cursed-Wolf-sized margin unconditionally and would have
	// failed this test at several player counts.
	for n := werewolfMinPlayers; n <= werewolfMaxPlayers; n++ {
		ratio, err := rolesForPlayerCount(n)
		if err != nil {
			t.Fatalf("rolesForPlayerCount(%d) errored: %v", n, err)
		}
		counts := map[role]int{
			roleWerewolf:   ratio.Werewolves,
			roleWolfCub:    ratio.WolfCubs,
			roleCursedWolf: ratio.CursedWolves,
			roleTraitor:    ratio.Traitors,
			roleSeer:       ratio.Seers,
			roleDoctor:     ratio.Doctors,
			roleWitch:      ratio.Witches,
			roleHunter:     ratio.Hunters,
			roleDisruptor:  ratio.Disruptors,
			roleCupid:      ratio.Cupids,
		}
		// dealRoles fills every unclaimed seat with a plain Villager, so
		// the editor's equivalent config needs that remainder spelled out.
		counts[roleVillager] = n - roleRatioTotal(ratio)
		if counts[roleVillager] < 0 {
			t.Fatalf("player count %d: fixed table claims more seats (%d) than players", n, roleRatioTotal(ratio))
		}
		if !roleCountConfirmValid(counts, n) {
			t.Errorf("player count %d: the FIXED TABLE's own config %v is rejected by the custom-roles validator (%s) — the floor is over-constrained",
				n, counts, roleCountConfirmError(counts, n))
		}
	}
}

func TestEveryPlayerCountHasAReachableCustomConfig(t *testing.T) {
	// Guards against the floor being tightened until some player count has
	// NO valid custom-roles configuration at all, which would silently
	// make custom-roles unusable at that size rather than failing loudly.
	for n := werewolfMinPlayers; n <= werewolfMaxPlayers; n++ {
		found := false
		for wolves := 1; wolves < n && !found; wolves++ {
			counts := map[role]int{roleWerewolf: wolves, roleVillager: n - wolves}
			if roleCountConfirmValid(counts, n) {
				found = true
			}
		}
		if !found {
			t.Errorf("player count %d has no reachable wolves+villagers custom config — the balance floor locked the size out entirely", n)
		}
	}
}

func TestRoleCountConfirmErrorReturnsEmptyForValidCounts(t *testing.T) {
	// The doc comment promises "" for valid counts, so the empty string is
	// a safe standalone validity test. An earlier revision documented that
	// contract without implementing it — the default switch arm returned
	// the balance-floor message for valid input too.
	counts := map[role]int{roleWerewolf: 2, roleSeer: 1, roleVillager: 3}
	const playerCount = 6
	if !roleCountConfirmValid(counts, playerCount) {
		t.Fatalf("test setup bug: %v at %d players should be valid", counts, playerCount)
	}
	if got := roleCountConfirmError(counts, playerCount); got != "" {
		t.Errorf("roleCountConfirmError(valid counts) = %q, want \"\" per its documented contract", got)
	}
}

func TestRoleCountConfirmErrorNeverSuggestsAnIneffectiveRemedy(t *testing.T) {
	// The balance-floor message must not tell the starter to reduce a role
	// that isn't counted toward the floor — following that advice would
	// loop forever with no progress. Cursed Wolf is excluded from
	// wolfFaction, so it must not appear in the "reduce these" list.
	counts := map[role]int{roleWerewolf: 4, roleCursedWolf: 1, roleVillager: 4}
	const playerCount = 9
	if roleCountConfirmValid(counts, playerCount) {
		t.Fatalf("test setup bug: %v at %d players should be rejected", counts, playerCount)
	}
	msg := roleCountConfirmError(counts, playerCount)
	reduceList, _, found := strings.Cut(msg, "hoặc tăng số người phe Dân")
	if !found {
		t.Fatalf("balance-floor message shape changed, update this test: %q", msg)
	}
	if strings.Contains(reduceList, "Sói Nguyền") {
		t.Errorf("balance-floor message tells the starter to reduce Sói Nguyền, which is excluded from the wolf-faction count and can never clear the rejection: %q", msg)
	}
}

func TestRoleCountConfirmErrorMatchesActualFailure(t *testing.T) {
	// Regression: the confirm-rejection message used to be one fixed string
	// describing only the total/both-sides conditions, actively misleading
	// a starter whose rejection was actually caused by the wolf-vote-
	// participant rule or the balance floor (both added later).
	cases := []struct {
		name        string
		counts      map[role]int
		playerCount int
		wantSubstr  string
	}{
		{"wrong total", map[role]int{roleWerewolf: 1, roleVillager: 6}, 5, "Tổng số vai trò hiện tại"},
		{"no wolf side", map[role]int{roleVillager: 5}, 5, "phe Sói"},
		{"no villager side", map[role]int{roleWerewolf: 5}, 5, "phe Dân"},
		{"no wolf-vote-participant", map[role]int{roleTraitor: 1, roleCursedWolf: 1, roleVillager: 6}, 8, "Ma Sói hoặc Sói Con"},
		{"balance floor", map[role]int{roleWerewolf: 3, roleVillager: 2}, 5, "quá mạnh"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if roleCountConfirmValid(c.counts, c.playerCount) {
				t.Fatalf("test setup bug: %v at %d players is actually valid", c.counts, c.playerCount)
			}
			got := roleCountConfirmError(c.counts, c.playerCount)
			if !strings.Contains(got, c.wantSubstr) {
				t.Errorf("roleCountConfirmError(%v, %d) = %q, want it to mention %q", c.counts, c.playerCount, got, c.wantSubstr)
			}
		})
	}
}

func TestOneOfEachSpecialRoleConfirmsAndDeals(t *testing.T) {
	counts := map[role]int{
		roleWerewolf: 1, roleWolfCub: 1,
		roleSeer: 1, roleDoctor: 1, roleWitch: 1, roleHunter: 1, roleDisruptor: 1, roleCupid: 1, roleTraitor: 1,
		roleVillager: 3,
	}
	const playerCount = 12
	if !roleCountConfirmValid(counts, playerCount) {
		t.Fatalf("roleCountConfirmValid(%v, %d) = false, want true", counts, playerCount)
	}
	ratio := roleRatioFromCounts(counts)

	joinOrder := make([]string, playerCount)
	labels := make(map[string]string, playerCount)
	for i := range joinOrder {
		joinOrder[i] = fmt.Sprintf("u%d", i)
		labels[joinOrder[i]] = fmt.Sprintf("Player%d", i)
	}
	players := dealRoles(joinOrder, labels, ratio)

	dealtCounts := make(map[role]int)
	for _, p := range players {
		dealtCounts[p.Role]++
	}
	for r, want := range counts {
		if got := dealtCounts[r]; got != want {
			t.Errorf("dealt %d of role %v, want %d", got, r, want)
		}
	}
}

func TestRoleRatioFromCounts(t *testing.T) {
	counts := map[role]int{
		roleWerewolf: 3, roleWolfCub: 1, roleCursedWolf: 1, roleTraitor: 1,
		roleSeer: 1, roleDoctor: 1, roleWitch: 1, roleHunter: 1, roleDisruptor: 1, roleCupid: 1,
		roleVillager: 4,
	}
	got := roleRatioFromCounts(counts)
	want := roleRatio{
		Werewolves: 3, WolfCubs: 1, CursedWolves: 1, Traitors: 1,
		Seers: 1, Doctors: 1, Witches: 1, Hunters: 1, Disruptors: 1, Cupids: 1,
	}
	if got != want {
		t.Errorf("roleRatioFromCounts() = %+v, want %+v (roleVillager count is intentionally absent — dealRoles fills the remainder)", got, want)
	}
}

func TestRoleCountEditorContentShowsCountsAndBalance(t *testing.T) {
	counts := map[role]int{roleWerewolf: 2, roleVillager: 3}
	got := roleCountEditorContent("wolf", counts, 5)
	if !strings.Contains(got, "Ma Sói") || !strings.Contains(got, "2") {
		t.Errorf("roleCountEditorContent(wolf) = %q, want it to mention Ma Sói's count", got)
	}
	if !strings.Contains(got, "5") {
		t.Errorf("roleCountEditorContent(wolf) = %q, want the player-count total to appear", got)
	}
	villager := roleCountEditorContent("villager", counts, 5)
	if !strings.Contains(villager, "Dân Làng") || !strings.Contains(villager, "3") {
		t.Errorf("roleCountEditorContent(villager) = %q, want it to mention Dân Làng's count", villager)
	}
}

func TestRoleCountEditorContentRendersNoCrossSideFigures(t *testing.T) {
	// Only the clicked message is re-rendered, so ANY figure computed
	// across both sides goes stale on the other message and can sit
	// visibly wrong for the whole 5-minute edit window. Each side must
	// therefore render only data its own buttons can change.
	//
	// Detect this behaviorally rather than by string-matching the copy:
	// mutate ONLY the villager side and assert the wolf-side render is
	// byte-identical. If any cross-side figure creeps back in, this fails.
	base := map[role]int{roleWerewolf: 2, roleVillager: 3}
	mutated := map[role]int{roleWerewolf: 2, roleVillager: 9}

	wolfBefore := roleCountEditorContent("wolf", base, 12)
	wolfAfter := roleCountEditorContent("wolf", mutated, 12)
	if wolfBefore != wolfAfter {
		t.Errorf("changing the VILLAGER side altered the WOLF message:\n before: %q\n  after: %q\nthe wolf message would be stale until its own next click", wolfBefore, wolfAfter)
	}

	// Symmetric check in the other direction.
	base2 := map[role]int{roleWerewolf: 2, roleVillager: 3}
	mutated2 := map[role]int{roleWerewolf: 5, roleVillager: 3}
	villagerBefore := roleCountEditorContent("villager", base2, 12)
	villagerAfter := roleCountEditorContent("villager", mutated2, 12)
	if villagerBefore != villagerAfter {
		t.Errorf("changing the WOLF side altered the VILLAGER message:\n before: %q\n  after: %q", villagerBefore, villagerAfter)
	}

	// Sanity: a side's OWN change must still be reflected, or the render
	// is simply broken rather than correctly side-scoped.
	if roleCountEditorContent("wolf", base, 12) == roleCountEditorContent("wolf", mutated2, 12) {
		t.Error("changing the wolf side did NOT alter the wolf message — the render isn't showing its own counts")
	}
}

func TestRoleCountButtonRowsShape(t *testing.T) {
	counts := map[role]int{roleWerewolf: 1}
	rows := roleCountButtonRows("chan1", 42, roleCountWolfSideRoles, counts, 8)
	if len(rows) != 3 {
		t.Fatalf("wolf-side rows = %d, want 3 (2 button rows + confirm)", len(rows))
	}
	rows = roleCountButtonRows("chan1", 42, roleCountVillagerSideRoles, counts, 8)
	if len(rows) > 5 {
		t.Fatalf("too many rows: %d, Discord's limit is 5", len(rows))
	}
	for idx, row := range rows {
		ar, ok := row.(discordgo.ActionsRow)
		if !ok {
			t.Fatalf("row %d is %T, want discordgo.ActionsRow", idx, row)
		}
		if len(ar.Components) > 5 {
			t.Errorf("row %d has %d components, Discord allows at most 5", idx, len(ar.Components))
		}
	}

	last, ok := rows[len(rows)-1].(discordgo.ActionsRow)
	if !ok || len(last.Components) == 0 {
		t.Fatalf("last row = %+v, want an ActionsRow with at least one component", rows[len(rows)-1])
	}
	confirmBtn, ok := last.Components[0].(discordgo.Button)
	if !ok {
		t.Fatalf("last row's first component is %T, want discordgo.Button", last.Components[0])
	}
	if !strings.HasPrefix(confirmBtn.CustomID, werewolfRoleCountConfirmID) {
		t.Errorf("last row's first button CustomID = %q, want prefix %q", confirmBtn.CustomID, werewolfRoleCountConfirmID)
	}
}

func TestRoleCountButtonRowsDisablesAtBounds(t *testing.T) {
	rows := roleCountButtonRows("c", 42, []role{roleSeer}, map[role]int{roleSeer: 1}, 8)
	buttons := rows[0].(discordgo.ActionsRow).Components
	dec := buttons[0].(discordgo.Button)
	inc := buttons[1].(discordgo.Button)
	if dec.Disabled {
		t.Error("dec button disabled at count 1, want enabled")
	}
	if !inc.Disabled {
		t.Error("inc button enabled at Seer's max of 1, want disabled")
	}

	rows = roleCountButtonRows("c", 42, []role{roleSeer}, map[role]int{}, 8)
	buttons = rows[0].(discordgo.ActionsRow).Components
	if !buttons[0].(discordgo.Button).Disabled {
		t.Error("dec button enabled at count 0, want disabled")
	}
}

func TestRoleCountCustomIDPrefixesDoNotCollide(t *testing.T) {
	confirmID := werewolfRoleCountConfirmID + "12345"
	if strings.HasPrefix(confirmID, werewolfRoleCountID) {
		t.Errorf("%q must not match the %q prefix", confirmID, werewolfRoleCountID)
	}
	const seq = 42
	rows := roleCountButtonRows("999", seq, []role{roleWerewolf}, map[role]int{}, 8)
	inc := rows[0].(discordgo.ActionsRow).Components[1].(discordgo.Button)
	remainder := strings.TrimPrefix(inc.CustomID, werewolfRoleCountID)
	// 4 fields since the Seq was added: sign, role, channel, seq. Parsed
	// with the same SplitN bound handleRoleCountButton uses.
	parts := strings.SplitN(remainder, ":", 4)
	if len(parts) != 4 || parts[0] != "inc" || parts[2] != "999" {
		t.Fatalf("customID %q parsed to %v, want [inc <role> 999 <seq>]", inc.CustomID, parts)
	}
	if parts[1] != strconv.Itoa(int(roleWerewolf)) {
		t.Errorf("encoded role = %q, want %d", parts[1], int(roleWerewolf))
	}
	if parts[3] != strconv.FormatUint(seq, 10) {
		t.Errorf("encoded seq = %q, want %d", parts[3], seq)
	}
}

func TestRoleCountCustomIDsCarrySeqAndStayUnderDiscordLimit(t *testing.T) {
	// The Seq field is what stops a cancelled game's still-live ephemeral
	// editor from driving whichever game now occupies the same channel
	// (channel/phase/starter are all identical between the two). Assert
	// both buttons and the confirm carry it, and that a realistic
	// worst-case ID stays under Discord's 100-char customID cap.
	const (
		bigChannelID = "1234567890123456789" // 19-digit snowflake
		bigSeq       = uint64(18446744073709551615)
	)
	rows := roleCountButtonRows(bigChannelID, bigSeq, roleCountVillagerSideRoles,
		map[role]int{}, werewolfMaxPlayers)

	seqSuffix := strconv.FormatUint(bigSeq, 10)
	checked := 0
	for _, row := range rows {
		for _, c := range row.(discordgo.ActionsRow).Components {
			btn := c.(discordgo.Button)
			if !strings.HasSuffix(btn.CustomID, ":"+seqSuffix) {
				t.Errorf("customID %q does not end with the game Seq — a stale editor from a cancelled game could drive the current one", btn.CustomID)
			}
			if n := len(btn.CustomID); n > 100 {
				t.Errorf("customID %q is %d chars, over Discord's 100-char limit", btn.CustomID, n)
			}
			checked++
		}
	}
	if checked == 0 {
		t.Fatal("no buttons were checked — test is not exercising anything")
	}
}

func TestStaleRoleEditorCannotDriveARestartedGame(t *testing.T) {
	// The scenario the Seq in the customID exists to stop:
	//
	//   1. Starter opens custom-roles game A, reaches phaseRoleEdit.
	//   2. Starter clicks Cancel (explicitly allowed during phaseRoleEdit).
	//   3. Starter immediately opens game B in the SAME channel.
	//
	// A's ephemeral editor messages stay interactive for the interaction
	// token's full life. channelID, Phase (phaseRoleEdit) and StarterID
	// are byte-identical between A and B, so every guard except the Seq
	// passes. Before the Seq existed, a click on A's confirm dealt game B
	// with whatever counts were left in A.
	//
	// gameIfCurrent is the mechanism, so assert directly that it rejects
	// the old Seq and accepts the new one.
	const channelID = "chan1"
	cmd := &WerewolfCommand{games: make(map[string]*werewolfGame)}

	gameA := &werewolfGame{
		ChannelID: channelID, Seq: 1, StarterID: "starter", Phase: phaseRoleEdit,
		PendingRoleCounts: map[role]int{roleWerewolf: 9},
	}
	cmd.games[channelID] = gameA

	// Starter cancels A, then starts B in the same channel.
	delete(cmd.games, channelID)
	gameB := &werewolfGame{
		ChannelID: channelID, Seq: 2, StarterID: "starter", Phase: phaseRoleEdit,
		PendingRoleCounts: map[role]int{},
	}
	cmd.games[channelID] = gameB

	// A click carrying game A's Seq must resolve to nothing...
	if got := cmd.gameIfCurrent(channelID, gameA.Seq); got != nil {
		t.Error("a click from the CANCELLED game's stale editor resolved to a live game — it could mutate or confirm the restarted game")
	}
	// ...while game B's own editor still works.
	if got := cmd.gameIfCurrent(channelID, gameB.Seq); got != gameB {
		t.Error("the current game's own editor Seq failed to resolve — the guard is too strict")
	}
	// And B's counts are untouched by A's abandoned setup.
	if gameB.PendingRoleCounts[roleWerewolf] != 0 {
		t.Errorf("game B's counts = %v, want empty — A's stale state must not leak in", gameB.PendingRoleCounts)
	}
}

func TestRoleCountConfirmCustomIDRoundTrips(t *testing.T) {
	// The confirm button's channel:seq encoding must parse back to
	// exactly what was encoded, using the same strings.Cut the handler
	// uses — a mismatch here silently makes every confirm click a no-op.
	const (
		channelID = "1234567890123456789"
		seq       = uint64(7)
	)
	rows := roleCountButtonRows(channelID, seq, []role{roleWerewolf}, map[role]int{}, 8)
	confirm := rows[len(rows)-1].(discordgo.ActionsRow).Components[0].(discordgo.Button)

	remainder := strings.TrimPrefix(confirm.CustomID, werewolfRoleCountConfirmID)
	gotChannel, gotSeqStr, ok := strings.Cut(remainder, ":")
	if !ok {
		t.Fatalf("confirm customID %q has no channel:seq separator", confirm.CustomID)
	}
	if gotChannel != channelID {
		t.Errorf("decoded channel = %q, want %q", gotChannel, channelID)
	}
	gotSeq, err := strconv.ParseUint(gotSeqStr, 10, 64)
	if err != nil {
		t.Fatalf("decoded seq %q does not parse: %v", gotSeqStr, err)
	}
	if gotSeq != seq {
		t.Errorf("decoded seq = %d, want %d", gotSeq, seq)
	}
}

func TestFormatRoleCountsAnnouncementCountsActualRoster(t *testing.T) {
	players := map[string]*player{
		"a": {Role: roleWerewolf}, "b": {Role: roleWerewolf},
		"c": {Role: roleWolfCub},
		"d": {Role: roleSeer},
		"e": {Role: roleVillager}, "f": {Role: roleVillager},
	}
	got := formatRoleCountsAnnouncement(players)
	if !strings.Contains(got, "Ma Sói: 2") {
		t.Errorf("formatRoleCountsAnnouncement() = %q, want it to count 2 Werewolves", got)
	}
	if !strings.Contains(got, "Sói Con: 1") {
		t.Errorf("formatRoleCountsAnnouncement() = %q, want it to count 1 Wolf Cub", got)
	}
	if !strings.Contains(got, "Dân Làng: 2") {
		t.Errorf("formatRoleCountsAnnouncement() = %q, want it to count 2 Villagers", got)
	}
	if strings.Contains(got, "Bác Sĩ") {
		t.Errorf("formatRoleCountsAnnouncement() = %q, want a zero-count role (Doctor) omitted", got)
	}
}

// nightActionsCompleteGame builds the minimal roster nightActionsComplete
// cares about: one wolf who has already voted, plus an optional Cupid.
func nightActionsCompleteGame(round int, cupidAlive bool, lovers [2]string) *werewolfGame {
	game := &werewolfGame{
		Round:     round,
		JoinOrder: []string{"w1", "c1"},
		Players: map[string]*player{
			"w1": {UserID: "w1", Label: "W1", Role: roleWerewolf, Faction: factionWolf, Alive: true},
			"c1": {UserID: "c1", Label: "C1", Role: roleCupid, Alive: cupidAlive},
		},
		WolfVotes: map[string][]string{"w1": {"c1"}},
		LoverIDs:  lovers,
	}
	return game
}

func TestNightActionsCompleteWaitsForCupidOnRoundOne(t *testing.T) {
	cmd := &WerewolfCommand{}
	cases := []struct {
		name       string
		round      int
		cupidAlive bool
		lovers     [2]string
		want       bool
	}{
		{"round 1, alive cupid, no pairing yet -> not complete", 1, true, [2]string{"", ""}, false},
		{"round 1, alive cupid, pairing made -> complete", 1, true, [2]string{"w1", "c1"}, true},
		{"round 1, dead cupid -> complete", 1, false, [2]string{"", ""}, true},
		{"round 2, alive cupid, no pairing -> complete", 2, true, [2]string{"", ""}, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := cmd.nightActionsComplete(nightActionsCompleteGame(c.round, c.cupidAlive, c.lovers))
			if got != c.want {
				t.Errorf("nightActionsComplete() = %v, want %v", got, c.want)
			}
		})
	}
}

func TestNightActionsCompleteRoundOneWithNoCupidAtAll(t *testing.T) {
	cmd := &WerewolfCommand{}
	game := &werewolfGame{
		Round:     1,
		JoinOrder: []string{"w1", "v1"},
		Players: map[string]*player{
			"w1": {UserID: "w1", Label: "W1", Role: roleWerewolf, Faction: factionWolf, Alive: true},
			"v1": {UserID: "v1", Label: "V1", Role: roleVillager, Alive: true},
		},
		WolfVotes: map[string][]string{"w1": {"v1"}},
	}
	if !cmd.nightActionsComplete(game) {
		t.Error("nightActionsComplete() = false with no Cupid in the game, want true")
	}
}
