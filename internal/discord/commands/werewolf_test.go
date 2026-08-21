package commands

import (
	"fmt"
	"strings"
	"testing"
	"time"
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
		{15, roleRatio{Werewolves: 4, Seers: 1, Doctors: 1, Witches: 1, Hunters: 1}},
		{16, roleRatio{Werewolves: 4, Seers: 1, Doctors: 1, Witches: 1, Hunters: 1}},
		{17, roleRatio{Werewolves: 4, Seers: 1, Doctors: 1, Witches: 1, Hunters: 1}},
		{18, roleRatio{Werewolves: 5, Seers: 1, Doctors: 1, Witches: 1, Hunters: 1}},
		{19, roleRatio{Werewolves: 5, Seers: 1, Doctors: 1, Witches: 1, Hunters: 1}},
		{20, roleRatio{Werewolves: 5, Seers: 1, Doctors: 1, Witches: 1, Hunters: 1}},
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

func TestFormatRoundBlockCursedWolfConverted(t *testing.T) {
	r := roundLog{Round: 2, WolfTargetLabel: "Chi", CursedWolfConverted: "Chi"}
	got := formatRoundBlock(r)
	if !strings.Contains(got, "biến thành Sói Nguyền") {
		t.Errorf("formatRoundBlock() = %q, want it to mention the conversion", got)
	}
	if strings.Contains(got, "Chi đã chết") {
		t.Errorf("formatRoundBlock() = %q, must not imply Chi died", got)
	}
}

// TestWitchHasChoicePredicate exercises the same boolean formula used
// (inline, under cmd.mu) in resolveNight to decide whether to prompt/wait
// on the Witch at all. The formula itself is tightly coupled to the locked
// closure in resolveNight and reads live game state, so it isn't extracted
// into a standalone function — instead this test derives the expected value
// from witchActionComponents' actual rendering (the ground truth for "does
// she have anything clickable beyond skip") and checks the formula matches
// it across the full truth table of (healUsed, poisonUsed, hasBiteTarget).
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
		resolvedTarget := ""
		biteTargetID := ""
		if c.hasBite {
			resolvedTarget = "u2"
			biteTargetID = "u2"
		}
		hasChoice := !c.poisonUsed || (!c.healUsed && resolvedTarget != "")

		_ = witchActionComponents("chan", biteTargetID, c.healUsed, c.poisonUsed, []*player{someTarget})
		rendersPoisonRow := !c.poisonUsed
		rendersHealButton := c.hasBite && !c.healUsed
		wouldRenderChoice := rendersPoisonRow || rendersHealButton

		if hasChoice != wouldRenderChoice {
			t.Errorf("case %+v: hasChoice=%v but witchActionComponents would render a choice=%v (poison row=%v, heal button=%v)",
				c, hasChoice, wouldRenderChoice, rendersPoisonRow, rendersHealButton)
		}
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
	votes := map[string]string{"wolf1": "x", "wolf2": "x", "wolf3": "y"}
	targets, ok := tallyWolfVotes(votes, 1)
	if !ok {
		t.Fatal("expected ok=true (no tie)")
	}
	if len(targets) != 1 || targets[0] != "x" {
		t.Errorf("got %v, want [x]", targets)
	}
}

func TestTallyWolfVotesTieProducesNoKill(t *testing.T) {
	votes := map[string]string{"wolf1": "x", "wolf2": "y"}
	_, ok := tallyWolfVotes(votes, 1)
	if ok {
		t.Fatal("expected ok=false on a genuine tie for the only kill slot")
	}
}

func TestTallyWolfVotesEmpty(t *testing.T) {
	targets, ok := tallyWolfVotes(map[string]string{}, 1)
	if ok || len(targets) != 0 {
		t.Errorf("expected ok=false and no targets for empty votes, got targets=%v ok=%v", targets, ok)
	}
}

func TestTallyWolfVotesTopTwo(t *testing.T) {
	// x:3, y:2, z:1 -- top 2 by count are x and y, no tie for either slot.
	votes := map[string]string{
		"wolf1": "x", "wolf2": "x", "wolf3": "x",
		"wolf4": "y", "wolf5": "y",
		"wolf6": "z",
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
	votes := map[string]string{
		"wolf1": "x", "wolf2": "x",
		"wolf3": "y",
		"wolf4": "z",
	}
	targets, ok := tallyWolfVotes(votes, 2)
	if !ok {
		t.Fatal("expected ok=true (first slot is unambiguous)")
	}
	if len(targets) != 1 || targets[0] != "x" {
		t.Errorf("expected only [x] (second slot tied, dropped), got %v", targets)
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
	votes := map[string]string{"wolf1": "v1", "wolf2": "v1"}
	got := validWolfVotes(votes, aliveWolves, allPlayers)
	want := map[string]string{"wolf1": "v1"}
	if len(got) != len(want) || got["wolf1"] != "v1" {
		t.Errorf("got %v, want %v (wolf2's vote should be dropped, they're dead)", got, want)
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
	votes := map[string]string{"wolf1": "v1"}
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
	votes := map[string]string{"wolf1": "v1", "wolf2": "v1"}
	got := validWolfVotes(votes, aliveWolves, allPlayers)
	if len(got) != 2 || got["wolf1"] != "v1" || got["wolf2"] != "v1" {
		t.Errorf("expected both votes kept, got %v", got)
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
