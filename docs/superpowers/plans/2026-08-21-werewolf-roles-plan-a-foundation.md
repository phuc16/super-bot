# Werewolf Extended Roles — Plan A: Foundation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Lay the foundation every later extended-roles plan depends on: a `Faction` model separate from `Role`, the full 11-role catalogue (constants + labels + point values), the fixed-ratio table extended to 5-20 players, and a real plurality vote replacing the wolf pack's current first-click-wins mechanic.

**Architecture:** Additive to the existing `werewolfGame`/`player` structs and `werewolf.go`/`werewolf_night.go`. No new files — both changes are tightly coupled to code that already lives in those two files (role/win-condition types in `werewolf.go`; wolf-thread flow in `werewolf_night.go`).

**Tech Stack:** Go, discordgo v0.29.0, existing `sync.Mutex`-guarded `map[string]*werewolfGame` pattern.

## Global Constraints

- This is Plan A of five sequential plans implementing `docs/superpowers/specs/2026-08-21-werewolf-roles-design.md` (spec sections 1, 2, and 4a). Later plans (Witch+Hunter, Cursed Wolf+Traitor+Wolf Cub, Disruptor+Cupid+progress notifications, manual role-pick+help guide) build on this one — this plan must leave the game fully playable and correct on its own before any of them land.
- **Do not implement any of the 7 new roles' mechanics in this plan** — only their constants/labels/faction/point-value entries. The fixed-ratio table in this plan stays limited to Werewolf/Villager/Seer/Doctor (today's role set) scaled up to 20 players — later plans extend the table further as each new role's mechanic actually lands, so the game stays genuinely playable at every player count after every plan.
- Every `discordgo.MessageSend`/`InteractionResponseData`/`MessageEdit` whose `Content` contains a `<@id>` mention must set `AllowedMentions` explicitly (existing project-wide rule).
- No DB/migration changes — werewolf remains fully in-memory.
- After editing any file, run `gofmt -l <file>` on every file you touched; if it lists the file, run `gofmt -w <file>` before your final build/test step.
- Every read/write of `*werewolfGame`/`*player` fields inside the interactive engine (`werewolf.go`, `werewolf_night.go`, `werewolf_day.go`) must happen strictly inside the existing `cmd.mu.Lock()`/`defer cmd.mu.Unlock()` critical sections — this codebase has a documented history of concurrency bugs from unguarded reads/writes.

---

### Task 1: Faction model, extended role catalogue, and the 5-20 player fixed-ratio table

**Files:**
- Modify: `internal/discord/commands/werewolf.go` (role catalogue, `checkWinCondition`, `assignRoles`, `rolesForPlayerCount`, `werewolfMaxPlayers`)
- Modify: `internal/discord/commands/werewolf_test.go` (existing win-condition tests need one field added; new tests for the extended table and role points)

**Interfaces:**
- Produces: `type faction int` with `factionVillager`/`factionWolf` constants; `factionForRole(r role) faction`; `player.Faction faction` field; `rolePoints map[role]int`; role constants `roleWolfCub`, `roleCursedWolf`, `roleTraitor`, `roleWitch`, `roleHunter`, `roleDisruptor`, `roleCupid` (added to the existing `role` iota block, values and labels only — no behavior); `werewolfMaxPlayers = 20`.
- Consumes: nothing new from other tasks (this is the foundation task).

- [ ] **Step 1: Write failing tests for the extended fixed-ratio table**

In `internal/discord/commands/werewolf_test.go`, replace the existing `TestRolesForPlayerCount` and `TestRolesForPlayerCountOutOfRange`:

```go
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
		{11, roleRatio{Werewolves: 3, Seers: 1, Doctors: 1}},
		{12, roleRatio{Werewolves: 3, Seers: 1, Doctors: 1}},
		{13, roleRatio{Werewolves: 3, Seers: 1, Doctors: 1}},
		{14, roleRatio{Werewolves: 3, Seers: 1, Doctors: 1}},
		{15, roleRatio{Werewolves: 4, Seers: 1, Doctors: 1}},
		{16, roleRatio{Werewolves: 4, Seers: 1, Doctors: 1}},
		{17, roleRatio{Werewolves: 4, Seers: 1, Doctors: 1}},
		{18, roleRatio{Werewolves: 5, Seers: 1, Doctors: 1}},
		{19, roleRatio{Werewolves: 5, Seers: 1, Doctors: 1}},
		{20, roleRatio{Werewolves: 5, Seers: 1, Doctors: 1}},
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
```

Append a new test for the role/faction/points catalogue at the end of the file:

```go
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
```

Now update the three existing win-condition tests that construct `roleWerewolf` players without a `Faction` field — after Step 3 below, `checkWinCondition` reads `p.Faction` instead of `p.Role == roleWerewolf`, so any test player meant to count as a wolf must set `Faction: factionWolf` explicitly (a dead player's Faction is never read, since the loop skips `!p.Alive` first — only alive-werewolf test players need the new field). Replace:

```go
func TestCheckWinConditionOngoing(t *testing.T) {
	players := map[string]*player{
		"w1": {UserID: "w1", Role: roleWerewolf, Alive: true},
		"v1": {UserID: "v1", Role: roleVillager, Alive: true},
		"v2": {UserID: "v2", Role: roleVillager, Alive: true},
	}
	if got := checkWinCondition(players); got != resultOngoing {
		t.Errorf("got %v, want resultOngoing", got)
	}
}
```

with:

```go
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
```

Replace:

```go
func TestCheckWinConditionWerewolvesWin(t *testing.T) {
	players := map[string]*player{
		"w1": {UserID: "w1", Role: roleWerewolf, Alive: true},
		"w2": {UserID: "w2", Role: roleWerewolf, Alive: true},
		"v1": {UserID: "v1", Role: roleVillager, Alive: true},
	}
	if got := checkWinCondition(players); got != resultWerewolvesWin {
		t.Errorf("got %v, want resultWerewolvesWin (2 wolves >= 1 other)", got)
	}
}
```

with:

```go
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
```

`TestCheckWinConditionVillagersWin` (its werewolf player is `Alive: false`, so its `Faction` is never read) and `TestCheckWinConditionAllDeadMidGame`/`TestCheckWinConditionEmptyRoster` (no werewolf-role players at all) need no changes — leave them exactly as they are.

- [ ] **Step 2: Run the new/updated tests to verify they fail**

Run: `go test ./internal/discord/commands/... -run 'TestRolesForPlayerCount|TestFactionForRole|TestRolePointsCoversEveryRole|TestCheckWinCondition' -v`
Expected: compile error (`faction`, `factionForRole`, `rolePoints`, `player.Faction`, and the new role constants don't exist yet) — this confirms the tests are wired to code that doesn't exist yet.

- [ ] **Step 3: Extend the role catalogue in `werewolf.go`**

Replace the existing role block:

```go
type role int

const (
	roleVillager role = iota
	roleWerewolf
	roleSeer
	roleDoctor
)

func (r role) label() string {
	switch r {
	case roleWerewolf:
		return "Ma Sói"
	case roleSeer:
		return "Tiên Tri"
	case roleDoctor:
		return "Bác Sĩ"
	default:
		return "Dân Làng"
	}
}
```

with:

```go
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
```

- [ ] **Step 4: Add `Faction` to `player` and set it in `assignRoles`**

Replace:

```go
type player struct {
	UserID string
	Label  string
	Role   role
	Alive  bool
}
```

with:

```go
type player struct {
	UserID  string
	Label   string
	Role    role
	Faction faction
	Alive   bool
}
```

In `assignRoles`, replace:

```go
	players := make(map[string]*player, len(shuffled))
	idx := 0
	assign := func(count int, r role) {
		for k := 0; k < count; k++ {
			id := shuffled[idx]
			idx++
			players[id] = &player{UserID: id, Label: labels[id], Role: r, Alive: true}
		}
	}
	assign(ratio.Werewolves, roleWerewolf)
	assign(ratio.Seers, roleSeer)
	assign(ratio.Doctors, roleDoctor)
	for ; idx < len(shuffled); idx++ {
		id := shuffled[idx]
		players[id] = &player{UserID: id, Label: labels[id], Role: roleVillager, Alive: true}
	}
	return players, nil
}
```

with:

```go
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
	assign(ratio.Seers, roleSeer)
	assign(ratio.Doctors, roleDoctor)
	for ; idx < len(shuffled); idx++ {
		id := shuffled[idx]
		players[id] = &player{UserID: id, Label: labels[id], Role: roleVillager, Faction: factionForRole(roleVillager), Alive: true}
	}
	return players, nil
}
```

- [ ] **Step 5: Rewrite `checkWinCondition` to use `Faction`**

Replace:

```go
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
		if p.Role == roleWerewolf {
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
```

with:

```go
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
```

- [ ] **Step 6: Extend `werewolfMaxPlayers` and `rolesForPlayerCount`'s table**

Replace:

```go
const (
	werewolfMinPlayers = 5
	werewolfMaxPlayers = 12
)
```

with:

```go
const (
	werewolfMinPlayers = 5
	werewolfMaxPlayers = 20
)
```

Replace:

```go
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
	default: // 11-12
		return roleRatio{Werewolves: 3, Seers: 1, Doctors: 1}, nil
	}
}
```

with:

```go
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
		return roleRatio{Werewolves: 3, Seers: 1, Doctors: 1}, nil
	case n <= 17:
		return roleRatio{Werewolves: 4, Seers: 1, Doctors: 1}, nil
	default: // 18-20
		return roleRatio{Werewolves: 5, Seers: 1, Doctors: 1}, nil
	}
}
```

- [ ] **Step 7: Run all tests to verify they pass**

Run: `go build ./... && go vet ./internal/discord/commands/... && go test ./internal/discord/commands/... -v`
Expected: build succeeds, all tests `PASS`, including `TestRolesForPlayerCount`, `TestRolesForPlayerCountOutOfRange`, `TestFactionForRole`, `TestRolePointsCoversEveryRole`, and the three updated `TestCheckWinCondition*` tests.

- [ ] **Step 8: Commit**

```bash
git add internal/discord/commands/werewolf.go internal/discord/commands/werewolf_test.go
git commit -m "werewolf: add faction model, extended role catalogue, 5-20 player table

Faction is a stored field separate from Role so Cursed Wolf (a later
plan) can flip factions mid-game without changing its Role. All 11
roles' constants/labels/point-values now exist; only Werewolf, Seer,
Doctor, and Villager are actually dealt by rolesForPlayerCount yet --
the other 7 have no mechanic until their own plan lands. checkWinCondition
now reads Faction instead of Role == roleWerewolf."
```

---

### Task 2: Wolf pack plurality voting (replaces first-click-wins)

**Files:**
- Modify: `internal/discord/commands/werewolf.go` (`werewolfGame` struct: replace `NightTarget string` with `WolfVotes map[string]string`, add `WolfTargetMessageID string`)
- Modify: `internal/discord/commands/werewolf_night.go` (`startWolfThread`, `handleWolfTarget`, `nightActionsComplete`, `resolveNight`, plus a new `tallyWolfVotes` pure function and a new `wolfVoteSummary` rendering helper)
- Test: `internal/discord/commands/werewolf_test.go`

**Interfaces:**
- Consumes: `player.Faction`/`role` catalogue from Task 1 (no new dependency beyond what already existed).
- Produces: `tallyWolfVotes(votes map[string]string, topN int) (targets []string, ok bool)` (pure, in `werewolf_night.go`) — a later plan (Wolf Cub) will call this with `topN=2`; this plan only ever calls it with `topN=1`. `wolfVoteSummary(votes map[string]string, wolves, targets []*player) string` (pure, in `werewolf_night.go`) — renders the live per-target-count and per-wolf-pick breakdown shown in the wolf thread.

- [ ] **Step 1: Write failing tests for `tallyWolfVotes`**

Append to `internal/discord/commands/werewolf_test.go`:

```go
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
```

- [ ] **Step 2: Run the new tests to verify they fail**

Run: `go test ./internal/discord/commands/... -run TestTallyWolfVotes -v`
Expected: compile error (`tallyWolfVotes` doesn't exist yet).

- [ ] **Step 3: Implement `tallyWolfVotes` in `werewolf_night.go`**

Add near the top of `internal/discord/commands/werewolf_night.go`, after the existing `wolfDiscussWindowFor` function:

```go
// tallyWolfVotes resolves the wolf pack's plurality vote into up to topN
// kill targets. Targets are picked one slot at a time, highest remaining
// vote count first; a genuine tie for a slot (two or more targets sharing
// the highest remaining count, when only one slot is left to fill) drops
// that slot entirely rather than picking arbitrarily — consistent with how
// the existing day-lynch vote already treats ties as "nobody dies." ok is
// false only when there were no votes at all, or the very first slot ties.
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
		max := 0
		for _, c := range remaining {
			if c > max {
				max = c
			}
		}
		var tied []string
		for target, c := range remaining {
			if c == max {
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
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/discord/commands/... -run TestTallyWolfVotes -v`
Expected: all `PASS`.

- [ ] **Step 5: Replace `NightTarget` with `WolfVotes` on `werewolfGame`**

In `internal/discord/commands/werewolf.go`, replace:

```go
	WolfThreadID                  string                         // this round's wolf-coordination thread, "" between nights
	NightTarget                   string                         // wolves' locked-in kill target this round
```

with:

```go
	WolfThreadID                  string                         // this round's wolf-coordination thread, "" between nights
	WolfTargetMessageID           string                         // the target-select message in the wolf thread, live-edited as votes come in
	WolfVotes                     map[string]string              // wolfUserID -> targetUserID this round, changeable until the deadline
```

- [ ] **Step 6: Initialize `WolfVotes` each round and rewrite `nightActionsComplete`'s wolf check**

In `internal/discord/commands/werewolf_night.go`, in `runNightPhase`'s locked block, replace:

```go
		game.Phase = phaseNight
		game.Round++
		game.NightTarget = ""
		game.SeerActed = false
```

with:

```go
		game.Phase = phaseNight
		game.Round++
		game.WolfVotes = make(map[string]string)
		game.SeerActed = false
```

Replace `nightActionsComplete`:

```go
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
```

with:

```go
func (cmd *WerewolfCommand) nightActionsComplete(game *werewolfGame) bool {
	wolves := alivePlayersOfRole(game, roleWerewolf)
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
```

- [ ] **Step 7: Add `wolfVoteSummary` and rewrite `startWolfThread` to post a live-editable vote message**

Add to `internal/discord/commands/werewolf_night.go`, right after `tallyWolfVotes`:

```go
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
			}
		}
		fmt.Fprintf(&b, "\n%s: %s", w.Label, pick)
	}

	return b.String()
}
```

Add `"strings"` to this file's imports:

```go
import (
	"fmt"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/rs/zerolog/log"
)
```

Replace `startWolfThread`'s message-posting tail (from the `wolfIDs` line to the end of the function):

```go
	wolfIDs := make([]string, len(wolves))
	for idx, w := range wolves {
		wolfIDs[idx] = w.UserID
	}
	pingText, allowedMentions := pingOnce(wolfIDs)
	if _, err := s.ChannelMessageSendComplex(thread.ID, &discordgo.MessageSend{
		Content:         fmt.Sprintf("🐺 %s Chọn mục tiêu để tấn công đêm nay. Lượt chọn đầu tiên sẽ được tính.", pingText),
		Components:      targetSelectMenu(werewolfWolfTargetID+channelID, "Chọn mục tiêu", targets),
		AllowedMentions: allowedMentions,
	}); err != nil {
		log.Error().Err(err).Msg("werewolf: failed to post wolf target select")
	}
}
```

with:

```go
	wolfIDs := make([]string, len(wolves))
	for idx, w := range wolves {
		wolfIDs[idx] = w.UserID
	}
	pingText, allowedMentions := pingOnce(wolfIDs)
	content := fmt.Sprintf(
		"🐺 %s Chọn mục tiêu để tấn công đêm nay. Có thể đổi phiếu bất cứ lúc nào trước khi hết giờ.\n\n%s",
		pingText, wolfVoteSummary(nil, wolves, targets),
	)
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
```

- [ ] **Step 8: Rewrite `handleWolfTarget` to record a changeable vote and live-edit the summary**

Replace the entire function:

```go
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
	if target, exists := game.Players[targetID]; !exists || !target.Alive {
		cmd.mu.Unlock()
		respondComponentEphemeral(s, i, "Mục tiêu không còn hợp lệ.")
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
```

with:

```go
func (cmd *WerewolfCommand) handleWolfTarget(s *discordgo.Session, i *discordgo.InteractionCreate, channelID string) {
	values := i.MessageComponentData().Values
	if len(values) == 0 {
		return
	}
	targetID := values[0]
	clickerID := interactionUserID(i)

	var threadID, messageID string
	var wolves, targets []*player
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
	if target, exists := game.Players[targetID]; !exists || !target.Alive {
		cmd.mu.Unlock()
		respondComponentEphemeral(s, i, "Mục tiêu không còn hợp lệ.")
		return
	}
	game.WolfVotes[clickerID] = targetID
	threadID = game.WolfThreadID
	messageID = game.WolfTargetMessageID
	wolves = copyPlayers(alivePlayersOfRole(game, roleWerewolf))
	targets = copyPlayers(aliveNonWolves(game))
	votesCopy := make(map[string]string, len(game.WolfVotes))
	for k, v := range game.WolfVotes {
		votesCopy[k] = v
	}
	cmd.mu.Unlock()

	respondComponentEphemeralMentioned(s, i, fmt.Sprintf("✅ Bạn đã chọn: <@%s>. Có thể đổi lại bất cứ lúc nào.", targetID))

	if threadID == "" || messageID == "" {
		return
	}
	summary := wolfVoteSummary(votesCopy, wolves, targets)
	pingText, allowedMentions := pingOnce(idsOf(wolves))
	content := fmt.Sprintf(
		"🐺 %s Chọn mục tiêu để tấn công đêm nay. Có thể đổi phiếu bất cứ lúc nào trước khi hết giờ.\n\n%s",
		pingText, summary,
	)
	if _, err := s.ChannelMessageEditComplex(&discordgo.MessageEdit{
		Channel:         threadID,
		ID:              messageID,
		Content:         &content,
		AllowedMentions: allowedMentions,
	}); err != nil {
		log.Error().Err(err).Msg("werewolf: failed to update wolf vote summary")
	}
}
```

This introduces one new tiny helper, `idsOf`. Add it to `internal/discord/commands/werewolf_night.go` right after `copyPlayers`:

```go
func idsOf(ps []*player) []string {
	ids := make([]string, len(ps))
	for idx, p := range ps {
		ids[idx] = p.UserID
	}
	return ids
}
```

Also update `startWolfThread`'s existing `wolfIDs` construction (Step 7) to reuse this new helper instead of its own inline loop — replace:

```go
	wolfIDs := make([]string, len(wolves))
	for idx, w := range wolves {
		wolfIDs[idx] = w.UserID
	}
	pingText, allowedMentions := pingOnce(wolfIDs)
```

with:

```go
	pingText, allowedMentions := pingOnce(idsOf(wolves))
```

(This applies inside `startWolfThread`, the function you already edited in Step 7 — go back and make this small follow-up simplification there now that `idsOf` exists.)

- [ ] **Step 9: Rewrite `resolveNight`'s wolf-kill resolution to use the tally**

In `internal/discord/commands/werewolf_night.go`, `resolveNight`'s locked block currently computes `wolfTargetLabel`/`killedID`/`saved` from the single `game.NightTarget`. Replace:

```go
		gameFound = true
		aliveThreadID = game.AliveThreadID

		wolfTargetLabel := ""
		if game.NightTarget != "" {
			if target, exists := game.Players[game.NightTarget]; exists {
				wolfTargetLabel = target.Label
			}
		}
		doctorTargetLabel := ""
		if game.DoctorProtect != "" {
			if target, exists := game.Players[game.DoctorProtect]; exists {
				doctorTargetLabel = target.Label
			}
		}

		var killedID string
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
```

with:

```go
		gameFound = true
		aliveThreadID = game.AliveThreadID

		wolfTargets, _ := tallyWolfVotes(game.WolfVotes, 1)
		wolfTargetLabel := ""
		var resolvedTarget string
		if len(wolfTargets) > 0 {
			resolvedTarget = wolfTargets[0]
			if target, exists := game.Players[resolvedTarget]; exists {
				wolfTargetLabel = target.Label
			}
		}
		doctorTargetLabel := ""
		if game.DoctorProtect != "" {
			if target, exists := game.Players[game.DoctorProtect]; exists {
				doctorTargetLabel = target.Label
			}
		}

		var killedID string
		if resolvedTarget != "" {
			if resolvedTarget == game.DoctorProtect {
				saved = true
			} else if target, exists := game.Players[resolvedTarget]; exists && target.Alive {
				target.Alive = false
				killedID = resolvedTarget
			}
		}

		wolves := alivePlayersOfRole(game, roleWerewolf)
		for _, w := range wolves {
			if _, voted := game.WolfVotes[w.UserID]; voted {
				game.AFKStrikes[w.UserID] = 0
			} else {
				game.AFKStrikes[w.UserID]++
			}
		}
```

(The rest of `resolveNight` — Seer/Doctor AFK-strike loops, `game.DoctorLastPick`, `threadID = game.WolfThreadID`, `game.WolfThreadID = ""`, the `roundLog` fill, and everything after the locked block — is unchanged; only this wolf-resolution portion changes.)

- [ ] **Step 10: Build, vet, and run the full test suite**

Run: `go build ./... && go vet ./internal/discord/commands/... && go test ./internal/discord/commands/... -v`
Expected: build succeeds, `go vet` clean, all tests `PASS` — including every pre-existing test (win-condition, tally-votes for the day-lynch vote, AFK, etc.) and the new `TestTallyWolfVotes*` tests. `NightTarget` should no longer appear anywhere (confirm with `grep -rn "NightTarget" internal/discord/commands/` — expect no output).

- [ ] **Step 11: Commit**

```bash
git add internal/discord/commands/werewolf.go internal/discord/commands/werewolf_night.go internal/discord/commands/werewolf_test.go
git commit -m "werewolf: replace wolf first-click-wins with a real plurality vote

game.WolfVotes (wolfID -> targetID) replaces the single NightTarget
string. Any wolf can vote/re-vote until the deadline; handleWolfTarget
live-edits a per-target-count + per-wolf-pick summary into the wolf
thread on every vote. tallyWolfVotes resolves up to topN targets at
night resolution (this plan only ever calls it with topN=1 -- a later
plan wires Wolf Cub's 2-target bonus bite through the same function).
Ties for a kill slot produce no kill for that slot, matching how the
existing day-lynch vote already treats ties."
```

---

### Task 3: Manual end-to-end verification (no code — hand this to the user)

This task has no code changes and requires a real Discord server with the bot invited, which a subagent cannot exercise. Flag this task to the user rather than dispatching it.

Checklist to hand over:

- [ ] Run a 5-player game through to completion. Confirm the single wolf can vote, the thread shows the live "Số phiếu" / "Lượt chọn" summary, and resolution works exactly as before (this player count's role ratio is unchanged from before this plan).
- [ ] Run a game with 3+ wolves (e.g. 15+ players). Confirm: (a) each wolf can vote exactly once — a second click from the same wolf is rejected ("Bạn đã bỏ phiếu rồi, không thể đổi.") rather than changing their pick, and the thread's live "Số phiếu"/"Lượt chọn" summary updates as *other* wolves vote; (b) a clear plurality resolves correctly; (c) deliberately create a tie between two targets (two wolves each vote for a different target and no one else breaks it) and confirm nobody dies that night, with a sensible result message. **Note:** this checklist item was updated after Task 2's review — wolf votes are locked once cast, not changeable; see the corrected design in `docs/superpowers/specs/2026-08-21-werewolf-roles-design.md` §4a. Task 2's step-by-step text below still shows the original (changeable-vote) snippets as a historical record of what was planned before that correction — the shipped code (commits `a6daf8e` + `395296c`) is the authority, not this file.
- [ ] Confirm `/werewolf start` still works normally end-to-end at the previous max of 12 players (unaffected by this plan beyond the vote mechanic).
- [ ] Confirm a 13-20 player game can now be started (previously rejected as "quá đông") and that role counts scale sensibly (more wolves, still exactly 1 Seer and 1 Doctor, rest Villagers — Witch/Hunter/Disruptor/etc. don't appear yet, that's expected until later plans).
