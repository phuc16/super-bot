# Werewolf Extended Roles — Plan C: Wolf Cub + Cursed Wolf + Traitor Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add the Wolf Cub, Cursed Wolf, and Traitor roles to `/werewolf`, on top of Plan A (faction model, extended catalogue, plurality wolf vote) and Plan B (Witch, Hunter). This is the third of five sequential plans implementing `docs/superpowers/specs/2026-08-21-werewolf-roles-design.md`.

**Architecture:** Wolf Cub folds into the *existing* wolf-vote mechanism rather than getting its own prompt — but every site that currently gathers "the wolves" by exact `Role == roleWerewolf` match has to widen to include Wolf Cub (and, later in a game, a converted Cursed Wolf), so this plan starts with that foundational fix before touching any role-specific mechanic. Cursed Wolf is a small, single-decision addition to the wolf-bite resolution switch. Wolf Cub's actual special behavior (a bonus second kill target the round after it dies) is the biggest task — it turns the wolf-bite resolution from "one target" into "one or two targets," which ripples through the Witch's heal decision (she now sometimes has to pick *which* of two bitten players to save) and the night recap. Traitor is purely a one-way information feed (a DM listing the current wolf roster, re-sent only when that roster changes) with no vote of its own.

**Tech Stack:** Go, discordgo v0.29.0, existing `sync.Mutex`-guarded `map[string]*werewolfGame` pattern.

## Global Constraints

- This is Plan C of five. Plan A (faction model, 11-role catalogue, extended 5-20 table, plurality wolf vote) and Plan B (Witch, Hunter) are already complete and merged into `feature/werewolf`. Do not re-implement anything from either.
- **Per spec §3a, none of these three roles are ever dealt by `rolesForPlayerCount`** — the fixed table stays limited to Werewolf/Villager/Seer/Guard/Witch/Hunter/Disruptor forever; Wolf Cub, Cursed Wolf, Traitor, and Cupid only ever get dealt through the manual role-pick mode that Plan E adds. **This means nothing this plan builds is reachable in a real game until Plan E ships.** Every mechanic in this plan must still be fully implemented and unit-tested (the spec requires it, and Plan E's manual-pick UI will need `dealRoles` to already support arbitrary counts of these roles) — but Task 6 (manual E2E) cannot be a real Discord playtest the way Plan A/B's could, and says so explicitly.
- Every `discordgo.MessageSend`/`InteractionResponseData`/`MessageEdit` whose `Content` contains a `<@id>` mention must set `AllowedMentions` explicitly.
- No DB/migration changes — werewolf remains fully in-memory.
- After editing any file, run `gofmt -l <file>` on every file you touched; if it lists the file, run `gofmt -w <file>` before your final build/test step.
- Every read/write of `*werewolfGame`/`*player` fields inside the interactive engine (`werewolf.go`, `werewolf_night.go`, `werewolf_day.go`) must happen strictly inside the existing `cmd.mu.Lock()`/`defer cmd.mu.Unlock()` critical sections.
- Use `player.Faction`/`(*player).isWolf()` for any wolf-side-membership check, never `Role == roleWerewolf` directly — Wolf Cub and Traitor are wolf-faction non-Werewolf roles, and Cursed Wolf starts villager-faction and flips mid-game.
- Any code reading `*player`/`*werewolfGame` state after a `cmd.mu.Unlock()` must read it from a `copyPlayer`/`copyPlayers` snapshot taken before the unlock, never a live pointer.

---

### Task 1: `roleRatio` fields + `dealRoles` extraction for Wolf Cub, Cursed Wolf, Traitor

**Files:**
- Modify: `internal/discord/commands/werewolf.go` (`roleRatio`, `assignRoles`)
- Test: `internal/discord/commands/werewolf_test.go`

**Interfaces:**
- Consumes: `roleWolfCub`, `roleCursedWolf`, `roleTraitor` (already exist, unused by dealing until now), `factionForRole` (Plan A, already handles these three correctly).
- Produces: `roleRatio.WolfCubs int`, `roleRatio.CursedWolves int`, `roleRatio.Traitors int` — read nowhere today (never set by `rolesForPlayerCount`), but this is the exact struct Plan E's manual-pick UI will populate and pass through. `dealRoles(joinOrder []string, labels map[string]string, ratio roleRatio) map[string]*player` — a new pure function factored out of `assignRoles`, taking an already-resolved ratio directly. This is what Plan E's manual-pick confirm handler will call instead of `assignRoles`.

- [ ] **Step 1: Extend `roleRatio` and extract `dealRoles`**

In `internal/discord/commands/werewolf.go`, replace the `roleRatio` struct and `assignRoles` function:

```go
// roleRatio describes how many of each special role a game of a given size gets.
type roleRatio struct {
	Werewolves   int
	Seers        int
	Doctors      int
	Witches      int
	Hunters      int
	WolfCubs     int
	CursedWolves int
	Traitors     int
}
```

```go
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
```

- [ ] **Step 2: Write the failing test**

Add to `internal/discord/commands/werewolf_test.go`:

```go
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
```

- [ ] **Step 3: Run test to verify it fails, then passes**

Run: `go test ./internal/discord/commands/... -run TestDealRolesAssignsWolfCubCursedWolfTraitor -v`

Before Step 1's code lands: FAIL (`dealRoles` undefined, `roleRatio` has no `WolfCubs` field).
After: PASS.

- [ ] **Step 4: Run the full suite and commit**

Run: `go build ./... && go vet ./internal/discord/commands/... && go test ./internal/discord/commands/...`

Expected: all pass, including every pre-existing test that calls `assignRoles` — its signature and observable behavior for the fixed-table path are unchanged, only its internals moved into `dealRoles`.

```bash
gofmt -l internal/discord/commands/werewolf.go internal/discord/commands/werewolf_test.go
git add internal/discord/commands/werewolf.go internal/discord/commands/werewolf_test.go
git commit -m "werewolf: extend roleRatio and extract dealRoles for Wolf Cub/Cursed Wolf/Traitor"
```

---

### Task 2: Fold Wolf Cub into the wolf pack's vote

**Files:**
- Modify: `internal/discord/commands/werewolf.go` (`player` methods, near `isWolf`)
- Modify: `internal/discord/commands/werewolf_night.go` (`runNightPhase`, `nightActionsComplete`, `resolveNight`, `handleWolfTarget`)
- Test: `internal/discord/commands/werewolf_test.go`

**Interfaces:**
- Consumes: `player.isWolf()` (Plan A).
- Produces: `(*player).wolfVoteParticipant() bool`, `aliveWolfVoters(game *werewolfGame) []*player` — every later task in this plan (and any future consumer needing "who's currently in the wolf pack's vote") should use these instead of `alivePlayersOfRole(game, roleWerewolf)`.

Today, every place that gathers "the wolves" for thread membership, voting, and AFK-gating filters by exact `Role == roleWerewolf`. Wolf Cub is wolf-faction but a different `Role` value, so as-is it would never be added to the wolf thread, never prompted to vote, and — critically — `handleWolfTarget` would actively reject a Wolf Cub's vote even if one somehow landed in the thread (`clicker.Role != roleWerewolf` check). A converted Cursed Wolf (Task 3) has the same problem from the round after conversion onward. This task fixes the *set* of players who vote; it does not yet add Wolf Cub's own special behavior (Task 4) or Cursed Wolf's conversion (Task 3) — after this task, a Wolf Cub or a converted Cursed Wolf behaves exactly like a Werewolf for voting purposes, nothing more.

- [ ] **Step 1: Add the participant predicate**

In `internal/discord/commands/werewolf.go`, right after the existing `isWolf` method:

```go
// wolfVoteParticipant reports whether p currently votes in the wolf pack's
// nightly kill vote. Every wolf-faction player votes except the Traitor,
// who has no night action (spec §4d) — a converted Cursed Wolf (Faction
// flipped by a prior night's bite resolution) becomes a participant
// starting the next night they're prompted, same as a Wolf Cub from deal
// time.
func (p *player) wolfVoteParticipant() bool {
	return p.isWolf() && p.Role != roleTraitor
}
```

- [ ] **Step 2: Add `aliveWolfVoters` next to the other alive-roster helpers**

In `internal/discord/commands/werewolf_night.go`, right after `aliveNonWolves`:

```go
// aliveWolfVoters returns every alive player who currently votes in the
// wolf pack's nightly kill vote — Werewolf, Wolf Cub, and (once converted)
// Cursed Wolf, but never Traitor. Use this instead of
// alivePlayersOfRole(game, roleWerewolf) for wolf-thread membership,
// voting validation, and AFK-gating; alivePlayersOfRole itself is still
// correct for anything that genuinely means "exactly roleWerewolf" (there
// is no such use left in this file after this task).
func aliveWolfVoters(game *werewolfGame) []*player {
	var result []*player
	for _, id := range game.JoinOrder {
		p := game.Players[id]
		if p.Alive && p.wolfVoteParticipant() {
			result = append(result, p)
		}
	}
	return result
}
```

- [ ] **Step 3: Replace every `alivePlayersOfRole(game, roleWerewolf)` wolf-vote call site**

In `internal/discord/commands/werewolf_night.go`, replace all six occurrences:

`runNightPhase` (building the wolf-thread snapshot):
```go
wolves = copyPlayers(aliveWolfVoters(game))
```

`nightActionsComplete`:
```go
func (cmd *WerewolfCommand) nightActionsComplete(game *werewolfGame) bool {
	wolves := aliveWolfVoters(game)
	if len(wolves) > 0 {
```
(rest of the function body unchanged)

`resolveNight` (the vote-tally section):
```go
aliveWolves := aliveWolfVoters(game)
```

`handleWolfTarget`'s validation:
```go
clicker, isWolf := game.Players[clickerID]
if !isWolf || !clicker.wolfVoteParticipant() || !clicker.Alive {
	cmd.mu.Unlock()
	respondComponentEphemeral(s, i, "Chỉ thành viên phe Sói còn sống mới được chọn mục tiêu.")
	return
}
```
(error text updated since "Chỉ Ma Sói" would now be misleading — Wolf Cub isn't literally `roleWerewolf`)

`handleWolfTarget`'s `allVoted` check:
```go
allVoted := true
for _, w := range aliveWolfVoters(game) {
	if _, voted := game.WolfVotes[w.UserID]; !voted {
		allVoted = false
		break
	}
}
```

`handleWolfTarget`'s post-unlock re-snapshot for the live vote summary:
```go
wolves = copyPlayers(aliveWolfVoters(game))
```

- [ ] **Step 4: Write the failing test**

Add to `internal/discord/commands/werewolf_test.go`:

```go
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
```

- [ ] **Step 5: Run test to verify it fails, then passes**

Run: `go test ./internal/discord/commands/... -run TestWolfVoteParticipant -v`

Before Step 1: FAIL (`wolfVoteParticipant` undefined). After: PASS.

- [ ] **Step 6: Run the full suite and commit**

Run: `go build ./... && go vet ./internal/discord/commands/... && go test ./internal/discord/commands/...`

Expected: all pass, including the existing wolf-vote tests (`TestTallyWolfVotes*`, `TestValidWolfVotes*`) — none of those construct games with `roleWolfCub`/`roleCursedWolf`/`roleTraitor` players today, so their observed behavior with a pure-Werewolf pack is unchanged; `aliveWolfVoters` and `alivePlayersOfRole(game, roleWerewolf)` agree exactly whenever no Wolf Cub/converted Cursed Wolf is present.

```bash
gofmt -l internal/discord/commands/werewolf.go internal/discord/commands/werewolf_night.go internal/discord/commands/werewolf_test.go
git add internal/discord/commands/werewolf.go internal/discord/commands/werewolf_night.go internal/discord/commands/werewolf_test.go
git commit -m "werewolf: fold Wolf Cub (and any converted Cursed Wolf) into the wolf pack's vote"
```

---

### Task 3: Cursed Wolf conversion

**Files:**
- Modify: `internal/discord/commands/werewolf.go` (`roundLog`)
- Modify: `internal/discord/commands/werewolf_night.go` (new `resolveWolfBiteOutcome`, `finalizeNightResolution`)
- Modify: `internal/discord/commands/werewolf_day.go` (`formatRoundBlock`)
- Test: `internal/discord/commands/werewolf_test.go`

**Interfaces:**
- Consumes: `player.Faction`, `factionWolf` (Plan A).
- Produces: `wolfBiteOutcome` (`wolfBiteSpare`/`wolfBiteKill`/`wolfBiteConvert`), `resolveWolfBiteOutcome(targetRole role, saved bool) wolfBiteOutcome` — Task 4 reuses this per-target in a loop when the bonus bite is active. `roundLog.CursedWolfConverted string`.

Spec §4c: if the wolf vote's resolved target is a Cursed Wolf **and** they were not saved (by Doctor or Witch) this round, they convert to wolf-faction instead of dying — no death, but the recap notes the conversion. If they *were* saved, nothing happens at all (no conversion, no death) — identical to today's "saved" outcome for anyone else.

- [ ] **Step 1: Add the pure outcome-decision helper**

In `internal/discord/commands/werewolf_night.go`, right after `applyHunterRevengeChain`:

```go
// wolfBiteOutcome is what happens to one wolf-vote target at resolution
// time.
type wolfBiteOutcome int

const (
	wolfBiteSpare wolfBiteOutcome = iota
	wolfBiteKill
	wolfBiteConvert
)

// resolveWolfBiteOutcome decides a single target's fate: spared (Doctor
// protection or Witch heal covered them — no death, no conversion),
// killed (the ordinary case), or converted (a not-saved Cursed Wolf
// survives by flipping to wolf-faction instead of dying — spec §4c). Pure
// — callers apply the actual Alive/Faction mutation themselves.
func resolveWolfBiteOutcome(targetRole role, saved bool) wolfBiteOutcome {
	switch {
	case saved:
		return wolfBiteSpare
	case targetRole == roleCursedWolf:
		return wolfBiteConvert
	default:
		return wolfBiteKill
	}
}
```

- [ ] **Step 2: Add the recap field**

In `internal/discord/commands/werewolf.go`, add one field to `roundLog` (next to `WitchPoisonedLabel`):

```go
	CursedWolfConverted string // label, if a not-saved Cursed Wolf converted to wolf-faction this round instead of dying
```

- [ ] **Step 3: Wire the outcome into `finalizeNightResolution`**

In `internal/discord/commands/werewolf_night.go`, `finalizeNightResolution` currently has:

```go
	var killedID string
	if resolvedTarget != "" && !saved {
		if target, exists := game.Players[resolvedTarget]; exists && target.Alive {
			target.Alive = false
			killedID = resolvedTarget
		}
	}
```

Replace with:

```go
	var killedID string
	var cursedWolfConvertedLabel string
	if resolvedTarget != "" {
		if target, exists := game.Players[resolvedTarget]; exists && target.Alive {
			switch resolveWolfBiteOutcome(target.Role, saved) {
			case wolfBiteKill:
				target.Alive = false
				killedID = resolvedTarget
			case wolfBiteConvert:
				target.Faction = factionWolf
				cursedWolfConvertedLabel = target.Label
			}
		}
	}
```

(`wolfBiteSpare` needs no case — matches the prior behavior of doing nothing when `saved`.)

Then, in the same function's history-entry block, add one line next to the existing `entry.WitchPoisonedLabel` assignment:

```go
	entry.CursedWolfConverted = cursedWolfConvertedLabel
```

- [ ] **Step 4: Add the recap line**

In `internal/discord/commands/werewolf_day.go`, `formatRoundBlock`'s `WolfTargetLabel` switch currently ends with a `default` case for "bitten and died." Add a case for conversion immediately before `default`:

```go
		case r.CursedWolfConverted != "":
			fmt.Fprintf(&b, "\n🐺 Sói cắn: %s (nhưng đã biến thành Sói Nguyền, không chết)", r.WolfTargetLabel)
```

- [ ] **Step 5: Write the failing tests**

Add to `internal/discord/commands/werewolf_test.go`:

```go
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
```

(`werewolf_test.go` already imports `strings` for other tests in the package — no new import needed.)

- [ ] **Step 6: Run tests to verify they fail, then pass**

Run: `go test ./internal/discord/commands/... -run 'TestResolveWolfBiteOutcome|TestFormatRoundBlockCursedWolfConverted' -v`

Before Step 1/4: FAIL. After: PASS.

- [ ] **Step 7: Run the full suite and commit**

Run: `go build ./... && go vet ./internal/discord/commands/... && go test ./internal/discord/commands/...`

Expected: all pass, including every existing `TestFormatRoundBlock*` test — `CursedWolfConverted` defaults to `""`, so the new `case` is simply never taken for any pre-existing recap, and the prior `default` behavior is preserved byte-for-byte for every round that doesn't involve a Cursed Wolf.

```bash
gofmt -l internal/discord/commands/werewolf.go internal/discord/commands/werewolf_night.go internal/discord/commands/werewolf_day.go internal/discord/commands/werewolf_test.go
git add internal/discord/commands/werewolf.go internal/discord/commands/werewolf_night.go internal/discord/commands/werewolf_day.go internal/discord/commands/werewolf_test.go
git commit -m "werewolf: add Cursed Wolf conversion on an unsaved wolf-bite"
```

---

### Task 4: Wolf Cub's bonus second bite

**Files:**
- Modify: `internal/discord/commands/werewolf.go` (`werewolfGame`, `roundLog`)
- Modify: `internal/discord/commands/werewolf_night.go` (`resolveNight`, `finalizeNightResolution`, `witchActionComponents`, `dmWitchPrompt`, `handleWitchHeal`, `resolveVote`-adjacent helper, `checkAndHandleGameEnd`-adjacent helper, `handleLeave`-adjacent helper)
- Modify: `internal/discord/commands/werewolf_day.go` (`resolveVote`, `checkAndHandleGameEnd`, `formatRoundBlock`)
- Modify: `internal/discord/commands/werewolf.go` (`handleLeave`)
- Test: `internal/discord/commands/werewolf_test.go`

**Interfaces:**
- Consumes: `resolveWolfBiteOutcome` (Task 3), `tallyWolfVotes(votes, topN)` (Plan A — already generic over `topN`, unchanged by this task), `aliveWolfVoters` (Task 2), `aliveExcept`, `copyPlayer`/`copyPlayers`, `applyHunterRevengeChain`.
- Produces: `game.WolfBonusBiteNextNight bool`, `applyWolfCubBonusBiteFlag(game *werewolfGame, deadIDs []string)`, `roundLog.WolfBonusBite bool`, `roundLog.WolfBiteOutcomeLines []string`.

Spec §4b: on a Wolf Cub's death (any cause), the *next* night's wolf vote picks the **top 2** targets instead of 1 — both are attacked. Spec §4e: the Witch's heal, when there were two bite targets, "heals exactly one of the bitten targets if there are two" — she needs to pick which. This task deliberately keeps the existing single-target UX (button, `"heal"` sentinel, and the existing `resultMsg`/`formatRoundBlock` switch cases from Plan B/Task 3) **completely untouched for the 1-target case** — the new select-menu-based heal path and the new outcome-line recap format only ever activate when there are genuinely 2 resolved targets that round, which cannot happen until a Wolf Cub has died at least once (itself unreachable before Plan E). This is a deliberate scoping choice to avoid re-touching already-reviewed 1-target behavior.

- [ ] **Step 1: Add the game-state fields**

In `internal/discord/commands/werewolf.go`, add to `werewolfGame` (next to `WitchPromptedThisRound`):

```go
	WolfBonusBiteNextNight bool // armed when a Wolf Cub dies (any cause); consumed at the start of the next night's wolf-vote resolution to select 2 targets instead of 1
	NightBiteTargetIDs     []string // this round's resolved wolf-vote target(s) — 1 normally, 2 if the bonus bite fired — set in resolveNight, read by handleWitchHeal to validate a 2-target select-menu pick, reset each round in runNightPhase
```

And to `roundLog` (next to `WolfBonusBite`... it doesn't exist yet, add both):

```go
	WolfBonusBite        bool     // this round used the Wolf-Cub 2-target bite
	WolfBiteOutcomeLines []string // one pre-formatted line per resolved target this round, used only when WolfBonusBite is true — see formatRoundBlock
```

- [ ] **Step 2: Add `applyWolfCubBonusBiteFlag`**

In `internal/discord/commands/werewolf_night.go`, right after `hunterRevengeLine`:

```go
// applyWolfCubBonusBiteFlag arms game.WolfBonusBiteNextNight if any of the
// given just-died player IDs was a Wolf Cub. Additive — never clears an
// already-armed flag — so every death-application site (night kill/
// poison/Hunter-revenge, day lynch, AFK removal, voluntary leave) can call
// it independently after applying its own deaths, in any order, without
// needing to coordinate with the others.
func applyWolfCubBonusBiteFlag(game *werewolfGame, deadIDs []string) {
	for _, id := range deadIDs {
		if p, ok := game.Players[id]; ok && p.Role == roleWolfCub {
			game.WolfBonusBiteNextNight = true
			return
		}
	}
}
```

- [ ] **Step 3: Consume the flag and tally up to 2 targets in `resolveNight`**

In `internal/discord/commands/werewolf_night.go`, `resolveNight`'s locked section currently has:

```go
		aliveWolves := aliveWolfVoters(game)
		validVotes := validWolfVotes(game.WolfVotes, aliveWolves, game.Players)
		wolfTargets, tallyOK := tallyWolfVotes(validVotes, 1)
		wolfVoteTied = !tallyOK && len(validVotes) > 0
		if len(wolfTargets) > 0 {
			resolvedTarget = wolfTargets[0]
			if target, exists := game.Players[resolvedTarget]; exists {
				wolfTargetLabel = target.Label
			}
		}
```

Replace with (note the surrounding function's `resolvedTarget`/`wolfTargetLabel` locals become plural — see Step 4 for the full signature change):

```go
		aliveWolves := aliveWolfVoters(game)
		validVotes := validWolfVotes(game.WolfVotes, aliveWolves, game.Players)
		topN := 1
		bonusBite := game.WolfBonusBiteNextNight
		if bonusBite {
			topN = 2
		}
		game.WolfBonusBiteNextNight = false // consumed regardless of outcome — a tie still spends the bonus
		wolfTargets, tallyOK := tallyWolfVotes(validVotes, topN)
		wolfVoteTied = !tallyOK && len(validVotes) > 0
		resolvedTargets = wolfTargets
		for _, id := range resolvedTargets {
			if target, exists := game.Players[id]; exists {
				wolfTargetLabels = append(wolfTargetLabels, target.Label)
			}
		}
```

(`doctorSaved`/`doctorTargetLabel` computation right below stays keyed off `game.DoctorProtect` alone — unrelated to which/how-many targets the wolves picked — no change needed there.)

- [ ] **Step 4: Change `resolveNight`'s locals and downstream calls to plural**

Still in `resolveNight`, change the `var` block at the top of the function from:

```go
	var (
		gameFound          bool
		threadID           string
		resolvedTarget     string
		wolfTargetLabel    string
		wolfVoteTied       bool
		doctorSaved        bool
		aliveThreadID      string
		witch              *player
		witchPoisonTargets []*player
	)
```

to:

```go
	var (
		gameFound          bool
		threadID           string
		resolvedTargets    []string
		wolfTargetLabels   []string
		bonusBite          bool
		wolfVoteTied       bool
		doctorSaved        bool
		aliveThreadID      string
		witch              *player
		witchPoisonTargets []*player
		biteTargetPlayers  []*player
	)
```

In the history-entry block (still inside the same locked section), change:

```go
			entry.WolfTargetLabel = wolfTargetLabel
```

to:

```go
			entry.WolfTargetLabel = strings.Join(wolfTargetLabels, ", ")
			entry.WolfBonusBite = bonusBite
```

Right after the witch-snapshot block (`if witches := alivePlayersOfRole(...)`), add the bite-target snapshot and persist `game.NightBiteTargetIDs` for `handleWitchHeal` to validate against later:

```go
		for _, id := range resolvedTargets {
			if target, exists := game.Players[id]; exists {
				biteTargetPlayers = append(biteTargetPlayers, copyPlayer(target))
			}
		}
		game.NightBiteTargetIDs = append([]string(nil), resolvedTargets...)
```

After the locked closure returns, the Witch-prompt section currently reads:

```go
	if witch != nil {
		var healUsed, poisonUsed, hasChoice bool
		func() {
			cmd.mu.Lock()
			defer cmd.mu.Unlock()
			g := cmd.gameIfCurrent(channelID, seq)
			if g == nil {
				return
			}
			healUsed = g.WitchHealUsed
			poisonUsed = g.WitchPoisonUsed
			hasChoice = !poisonUsed || (!healUsed && resolvedTarget != "")
			if hasChoice {
				g.WitchPromptedThisRound = true
			}
		}()
		if hasChoice {
			cmd.dmWitchPrompt(s, channelID, witch, resolvedTarget, wolfTargetLabel, healUsed, poisonUsed, witchPoisonTargets)
			deadline := time.Now().Add(witchActionWindow)
			cmd.waitForWitchAction(channelID, deadline, seq)
		}
	}

	cmd.finalizeNightResolution(s, channelID, seq, resolvedTarget, doctorSaved, wolfVoteTied, aliveThreadID)
```

Replace the two `resolvedTarget != ""` / `dmWitchPrompt(...)` / final call with their plural equivalents:

```go
	if witch != nil {
		var healUsed, poisonUsed, hasChoice bool
		func() {
			cmd.mu.Lock()
			defer cmd.mu.Unlock()
			g := cmd.gameIfCurrent(channelID, seq)
			if g == nil {
				return
			}
			healUsed = g.WitchHealUsed
			poisonUsed = g.WitchPoisonUsed
			hasChoice = !poisonUsed || (!healUsed && len(resolvedTargets) > 0)
			if hasChoice {
				g.WitchPromptedThisRound = true
			}
		}()
		if hasChoice {
			cmd.dmWitchPrompt(s, channelID, witch, biteTargetPlayers, healUsed, poisonUsed, witchPoisonTargets)
			deadline := time.Now().Add(witchActionWindow)
			cmd.waitForWitchAction(channelID, deadline, seq)
		}
	}

	cmd.finalizeNightResolution(s, channelID, seq, resolvedTargets, wolfVoteTied, aliveThreadID)
```

(`doctorSaved` is no longer passed — `finalizeNightResolution` recomputes it per-target directly from the live `game.DoctorProtect`, since that field is still valid at finalize time and hasn't been reset yet. Drop the now-unused `doctorSaved` local's earlier single-value computation too: remove the `doctorSaved = resolvedTarget != "" && resolvedTarget == game.DoctorProtect` line from the locked section entirely — it's dead once nothing downstream reads it. `bonusBite` local is set right where `topN`/`resolvedTargets` are computed: add `bonusBite = topN == 2` immediately after the `topN` decision in Step 3's replacement.)

- [ ] **Step 5: Rewrite `dmWitchPrompt` and `witchActionComponents` for 1-or-2 bite targets**

In `internal/discord/commands/werewolf_night.go`, replace `witchActionComponents` and `dmWitchPrompt`:

```go
// witchActionComponents builds the Witch's action message. The heal
// control is a button when there's exactly one bite target (unchanged
// from Plan B — she can only be healing "the" bite target, so a plain
// confirm button is enough) and a target-select menu when there are two
// (the Wolf-Cub bonus bite) — she has to pick which one to save. No heal
// control at all if there's no bite target this round, or she's already
// used the potion.
func witchActionComponents(channelID string, biteTargets []*player, healUsed, poisonUsed bool, poisonTargets []*player) []discordgo.MessageComponent {
	var rows []discordgo.MessageComponent
	if !poisonUsed {
		rows = append(rows, targetSelectMenu(werewolfWitchPoisonID+channelID, "☠️ Đầu độc ai đó (bỏ qua nếu không muốn)", poisonTargets)[0])
	}
	if len(biteTargets) == 2 && !healUsed {
		rows = append(rows, targetSelectMenu(werewolfWitchHealID+channelID, "💊 Cứu một trong hai người bị cắn", biteTargets)[0])
	}
	buttons := []discordgo.MessageComponent{}
	if len(biteTargets) == 1 && !healUsed {
		buttons = append(buttons, discordgo.Button{
			Label:    "💊 Cứu người bị cắn",
			Style:    discordgo.SuccessButton,
			CustomID: werewolfWitchHealID + channelID,
		})
	}
	buttons = append(buttons, discordgo.Button{
		Label:    "🚫 Không làm gì",
		Style:    discordgo.SecondaryButton,
		CustomID: werewolfWitchSkipID + channelID,
	})
	rows = append(rows, discordgo.ActionsRow{Components: buttons})
	return rows
}

func (cmd *WerewolfCommand) dmWitchPrompt(s *discordgo.Session, channelID string, witch *player, biteTargets []*player, healUsed, poisonUsed bool, poisonTargets []*player) {
	dm, err := s.UserChannelCreate(witch.UserID)
	if err != nil {
		log.Error().Err(err).Msg("werewolf: failed to open DM for witch")
		return
	}
	bittenText := "Không ai bị cắn đêm nay."
	if len(biteTargets) > 0 {
		labels := make([]string, len(biteTargets))
		for idx, p := range biteTargets {
			labels[idx] = p.Label
		}
		bittenText = fmt.Sprintf("Sói cắn: **%s** đêm nay.", strings.Join(labels, ", "))
	}
	if _, err := s.ChannelMessageSendComplex(dm.ID, &discordgo.MessageSend{
		Content:    fmt.Sprintf("🧙 %s Bạn muốn cứu, đầu độc ai đó, hay không làm gì?", bittenText),
		Components: witchActionComponents(channelID, biteTargets, healUsed, poisonUsed, poisonTargets),
	}); err != nil {
		log.Error().Err(err).Msg("werewolf: failed to DM witch prompt")
	}
}
```

- [ ] **Step 6: Handle the 2-target heal selection in `handleWitchHeal`**

In `internal/discord/commands/werewolf_night.go`, `handleWitchHeal` currently sets `game.WitchSaveTarget = "heal"` unconditionally (a sentinel resolved against the single bite target at finalize time). Change the body between the `WitchHealUsed` check and the final assignment from:

```go
	if game.WitchHealUsed {
		cmd.mu.Unlock()
		respondComponentEphemeral(s, i, "Bạn đã dùng bình cứu rồi.")
		return
	}
	game.WitchSaveTarget = "heal" // sentinel: the actual target is resolved from the wolf-bite target at finalize time
	game.WitchHealUsed = true
	game.WitchActedThisRound = true
	cmd.mu.Unlock()

	respondComponentEphemeral(s, i, "✅ Bạn đã chọn cứu người bị cắn đêm nay.")
	announceRoleDecided(s, channelID, "🧙 Phù Thủy")
```

to:

```go
	if game.WitchHealUsed {
		cmd.mu.Unlock()
		respondComponentEphemeral(s, i, "Bạn đã dùng bình cứu rồi.")
		return
	}
	values := i.MessageComponentData().Values
	if len(values) > 0 {
		// Two-target select-menu path (Wolf-Cub bonus bite): the option
		// list was built from game.NightBiteTargetIDs, but re-validate
		// against it directly rather than trusting the client payload.
		targetID := values[0]
		valid := false
		for _, id := range game.NightBiteTargetIDs {
			if id == targetID {
				valid = true
				break
			}
		}
		if !valid {
			cmd.mu.Unlock()
			respondComponentEphemeral(s, i, "Mục tiêu không còn hợp lệ.")
			return
		}
		game.WitchSaveTarget = targetID
	} else {
		// Single-target button path (Plan B, unchanged): sentinel resolved
		// against the lone bite target at finalize time.
		game.WitchSaveTarget = "heal"
	}
	game.WitchHealUsed = true
	game.WitchActedThisRound = true
	cmd.mu.Unlock()

	respondComponentEphemeral(s, i, "✅ Bạn đã chọn cứu người bị cắn đêm nay.")
	announceRoleDecided(s, channelID, "🧙 Phù Thủy")
```

- [ ] **Step 7: Reset `NightBiteTargetIDs` each round**

In `internal/discord/commands/werewolf_night.go`, `runNightPhase`'s per-round reset block, add next to `game.WitchPromptedThisRound = false`:

```go
		game.NightBiteTargetIDs = nil
```

- [ ] **Step 8: Rewrite `finalizeNightResolution` to loop over 1-or-2 targets**

In `internal/discord/commands/werewolf_night.go`, change the function signature from:

```go
func (cmd *WerewolfCommand) finalizeNightResolution(s *discordgo.Session, channelID string, seq uint64, resolvedTarget string, doctorSaved, wolfVoteTied bool, aliveThreadID string) {
```

to:

```go
func (cmd *WerewolfCommand) finalizeNightResolution(s *discordgo.Session, channelID string, seq uint64, resolvedTargets []string, wolfVoteTied bool, aliveThreadID string) {
```

Replace the whole locked section's kill/poison/outcome-building logic — from the `game.WitchPromptedThisRound = false` line down through the `killedPlayer`/`poisonedPlayer` history-entry writes — with:

```go
		game.WitchPromptedThisRound = false

		var witchHealTarget string
		switch {
		case game.WitchSaveTarget == "heal" && len(resolvedTargets) > 0:
			witchHealTarget = resolvedTargets[0]
		case game.WitchSaveTarget != "" && game.WitchSaveTarget != "heal":
			witchHealTarget = game.WitchSaveTarget
		}

		var killedIDs []string
		var cursedWolfConvertedLabel string
		var biteOutcomeLines []string
		var witchHealedLabel string
		for _, targetID := range resolvedTargets {
			target, exists := game.Players[targetID]
			if !exists || !target.Alive {
				continue
			}
			doctorSavedThis := targetID == game.DoctorProtect
			witchHealedThis := targetID == witchHealTarget && witchHealTarget != ""
			savedThis := doctorSavedThis || witchHealedThis
			switch resolveWolfBiteOutcome(target.Role, savedThis) {
			case wolfBiteKill:
				target.Alive = false
				killedIDs = append(killedIDs, targetID)
				biteOutcomeLines = append(biteOutcomeLines, fmt.Sprintf("%s: chết", target.Label))
			case wolfBiteConvert:
				target.Faction = factionWolf
				cursedWolfConvertedLabel = target.Label
				biteOutcomeLines = append(biteOutcomeLines, fmt.Sprintf("%s: biến thành Sói Nguyền, không chết", target.Label))
			case wolfBiteSpare:
				reason := "Bác Sĩ cứu"
				if witchHealedThis {
					reason = "Phù Thủy cứu"
					witchHealedLabel = target.Label
				}
				biteOutcomeLines = append(biteOutcomeLines, fmt.Sprintf("%s: được %s", target.Label, reason))
			}
		}

		var poisonedID string
		if game.WitchPoisonTarget != "" {
			// Known, accepted edge case: the Witch can poison the same
			// player the wolves bit. If that player already died via the
			// wolf-kill path above, target.Alive is false here and this
			// block silently no-ops — no double-kill, no separate poison
			// recap line — even though WitchPoisonUsed was already set
			// true. The potion is simply burned with no visible effect;
			// this is intentional (carried forward from Plan B).
			if target, exists := game.Players[game.WitchPoisonTarget]; exists && target.Alive {
				target.Alive = false
				poisonedID = game.WitchPoisonTarget
			}
		}

		var hunterRevenged []*player
		for _, id := range killedIDs {
			hunterRevenged = append(hunterRevenged, applyHunterRevengeChain(game, id)...)
		}
		if poisonedID != "" {
			hunterRevenged = append(hunterRevenged, applyHunterRevengeChain(game, poisonedID)...)
		}

		var deadThisResolution []string
		deadThisResolution = append(deadThisResolution, killedIDs...)
		if poisonedID != "" {
			deadThisResolution = append(deadThisResolution, poisonedID)
		}
		for _, p := range hunterRevenged {
			deadThisResolution = append(deadThisResolution, p.UserID)
		}
		applyWolfCubBonusBiteFlag(game, deadThisResolution)

		revealOnDeath = game.RevealOnDeath
		summary = aliveDeadSummary(game)
		killedPlayers = copyPlayers(playersByIDs(game, killedIDs))
		var poisonedPlayer *player
		if poisonedID != "" {
			poisonedPlayer = copyPlayer(game.Players[poisonedID])
		}

		if n := len(game.History); n > 0 {
			entry := &game.History[n-1]
			entry.CursedWolfConverted = cursedWolfConvertedLabel
			if witchHealedLabel != "" {
				entry.WitchSavedLabel = witchHealedLabel
			}
			if poisonedPlayer != nil {
				entry.WitchPoisonedLabel = poisonedPlayer.Label
			}
			if len(hunterRevenged) > 0 {
				entry.HunterRevengeLabel = hunterRevenged[0].Label
			}
			if entry.WolfBonusBite {
				entry.WolfBiteOutcomeLines = biteOutcomeLines
			}
		}
```

This changes the function's local variable set from singular (`killedPlayer *player`) to plural (`killedPlayers []*player`) and drops the standalone `doctorSaved`/`witchHealed` booleans in favor of the per-target loop above — update the function's `var (...)` block accordingly:

```go
	var (
		gameFound      bool
		revealOnDeath  bool
		killedPlayers  []*player
		hunterRevenged []*player
		summary        string
	)
```

(`poisonedPlayer` moves from the outer `var` block to a local declared inside the locked closure, as shown above, since it's fully computed and only used within/after that closure — same lifetime either way, cleaner to declare where it's set. If your diff tooling prefers keeping it in the outer block for symmetry with `killedPlayers`, that's fine too — just be consistent and make sure it's still readable after the unlock.)

Add the tiny helper this references, next to `aliveExcept`:

```go
// playersByIDs looks up each ID in game.Players, skipping any that don't
// exist (shouldn't happen — every ID here came from game.Players itself
// moments earlier — but this keeps the lookup total rather than panicking
// on a theoretical race).
func playersByIDs(game *werewolfGame, ids []string) []*player {
	var result []*player
	for _, id := range ids {
		if p, ok := game.Players[id]; ok {
			result = append(result, p)
		}
	}
	return result
}
```

Now the post-unlock section of `finalizeNightResolution`. Replace:

```go
	if !gameFound {
		return
	}
	if killedPlayer != nil {
		removeFromAliveThread(s, aliveThreadID, killedPlayer.UserID)
	}
	if poisonedPlayer != nil {
		removeFromAliveThread(s, aliveThreadID, poisonedPlayer.UserID)
	}
	for _, p := range hunterRevenged {
		removeFromAliveThread(s, aliveThreadID, p.UserID)
	}

	var resultMsg string
	switch {
	case killedPlayer != nil && revealOnDeath:
		resultMsg = fmt.Sprintf("☠️ **%s** đã chết trong đêm qua. Vai trò: %s.", killedPlayer.Label, killedPlayer.Role.label())
	case killedPlayer != nil:
		resultMsg = fmt.Sprintf("☠️ **%s** đã chết trong đêm qua.", killedPlayer.Label)
	case doctorSaved && poisonedPlayer == nil:
		resultMsg = "🛡️ Bác Sĩ đã cứu sống mục tiêu đêm qua — không ai chết!"
	case doctorSaved:
		resultMsg = "🛡️ Bác Sĩ đã cứu sống mục tiêu bị Sói cắn đêm qua."
	case witchHealed && poisonedPlayer == nil:
		resultMsg = "🧙 Phù Thủy đã cứu sống mục tiêu đêm qua — không ai chết!"
	case witchHealed:
		resultMsg = "🧙 Phù Thủy đã cứu sống mục tiêu bị Sói cắn đêm qua."
	case wolfVoteTied && poisonedPlayer == nil:
		resultMsg = "🐺 Bầy Sói không thống nhất được mục tiêu — không ai chết trong đêm qua."
	case wolfVoteTied:
		resultMsg = "🐺 Bầy Sói không thống nhất được mục tiêu."
	case poisonedPlayer == nil:
		resultMsg = "😴 Không ai chết trong đêm qua."
	default:
		resultMsg = "😴 Không ai chết vì Sói cắn đêm qua."
	}
	if poisonedPlayer != nil {
		if revealOnDeath {
			resultMsg += fmt.Sprintf("\n☠️ **%s** trúng độc của Phù Thủy. Vai trò: %s.", poisonedPlayer.Label, poisonedPlayer.Role.label())
		} else {
			resultMsg += fmt.Sprintf("\n☠️ **%s** trúng độc của Phù Thủy.", poisonedPlayer.Label)
		}
	}
	for _, p := range hunterRevenged {
		resultMsg += "\n" + hunterRevengeLine(p)
	}
```

with (note `poisonedPlayer` needs to be visible here — declare it in the outer `var` block per the note above if you didn't already, or thread it out of the closure the same way `killedPlayers` is):

```go
	if !gameFound {
		return
	}
	for _, p := range killedPlayers {
		removeFromAliveThread(s, aliveThreadID, p.UserID)
	}
	if poisonedPlayer != nil {
		removeFromAliveThread(s, aliveThreadID, poisonedPlayer.UserID)
	}
	for _, p := range hunterRevenged {
		removeFromAliveThread(s, aliveThreadID, p.UserID)
	}

	var resultMsg string
	switch {
	case bonusBite:
		// Two-target round: the existing single-target cascade below
		// can't represent "one target died, the other was saved" — list
		// each target's own outcome instead.
		resultMsg = "🌙 **Kết quả đêm qua:**\n" + strings.Join(biteOutcomeLines, "\n")
	case len(killedPlayers) == 1 && revealOnDeath:
		resultMsg = fmt.Sprintf("☠️ **%s** đã chết trong đêm qua. Vai trò: %s.", killedPlayers[0].Label, killedPlayers[0].Role.label())
	case len(killedPlayers) == 1:
		resultMsg = fmt.Sprintf("☠️ **%s** đã chết trong đêm qua.", killedPlayers[0].Label)
	case cursedWolfConvertedLabel != "" && poisonedPlayer == nil:
		resultMsg = "😴 Không ai chết trong đêm qua."
	case witchHealedLabel != "" && poisonedPlayer == nil:
		resultMsg = "🧙 Phù Thủy đã cứu sống mục tiêu đêm qua — không ai chết!"
	case witchHealedLabel != "":
		resultMsg = "🧙 Phù Thủy đã cứu sống mục tiêu bị Sói cắn đêm qua."
	case len(resolvedTargets) > 0 && poisonedPlayer == nil:
		resultMsg = "🛡️ Bác Sĩ đã cứu sống mục tiêu đêm qua — không ai chết!"
	case len(resolvedTargets) > 0:
		resultMsg = "🛡️ Bác Sĩ đã cứu sống mục tiêu bị Sói cắn đêm qua."
	case wolfVoteTied && poisonedPlayer == nil:
		resultMsg = "🐺 Bầy Sói không thống nhất được mục tiêu — không ai chết trong đêm qua."
	case wolfVoteTied:
		resultMsg = "🐺 Bầy Sói không thống nhất được mục tiêu."
	case poisonedPlayer == nil:
		resultMsg = "😴 Không ai chết trong đêm qua."
	default:
		resultMsg = "😴 Không ai chết vì Sói cắn đêm qua."
	}
	if !bonusBite && poisonedPlayer != nil {
		if revealOnDeath {
			resultMsg += fmt.Sprintf("\n☠️ **%s** trúng độc của Phù Thủy. Vai trò: %s.", poisonedPlayer.Label, poisonedPlayer.Role.label())
		} else {
			resultMsg += fmt.Sprintf("\n☠️ **%s** trúng độc của Phù Thủy.", poisonedPlayer.Label)
		}
	}
	for _, p := range hunterRevenged {
		resultMsg += "\n" + hunterRevengeLine(p)
	}
```

Note this preserves the exact original single-target cascade (doctor-saved / witch-healed / wolf-tied / poison-only / default) byte-for-byte for the `!bonusBite` path — the only change is that `doctorSaved`/`witchHealed` booleans became `len(resolvedTargets) > 0` (equivalent when there's exactly one target and it wasn't killed or converted, which is the only way to reach those branches at all) and `killedPlayer` became `killedPlayers[0]` guarded by `len(killedPlayers) == 1`. The `bonusBite` case is checked first specifically so a 2-target round never falls through into the 1-target cascade's now-ambiguous branches. `bonusBite` itself needs to be threaded from `resolveNight` into `finalizeNightResolution` — add it as a parameter: change the signature from Step 8's start to also take `bonusBite bool`, and update the call site in `resolveNight` (Step 4) to `cmd.finalizeNightResolution(s, channelID, seq, resolvedTargets, bonusBite, wolfVoteTied, aliveThreadID)`.

Finally, `cursedWolfConvertedLabel` and `witchHealedLabel` need to be visible in the post-unlock section the same way `poisonedPlayer`/`bonusBite` are (threaded out of the locked closure via the outer `var` block, same pattern as every other cross-boundary value in this file).

- [ ] **Step 9: Arm the bonus-bite flag from every other death-application site**

In `internal/discord/commands/werewolf_day.go`, `resolveVote`'s locked section — right after the existing `if lynchedPlayer != nil { hunterRevenged = applyHunterRevengeChain(...) }` block — add:

```go
			var deadIDs []string
			if lynchedPlayer != nil {
				deadIDs = append(deadIDs, lynchedPlayer.UserID)
			}
			for _, p := range hunterRevenged {
				deadIDs = append(deadIDs, p.UserID)
			}
			applyWolfCubBonusBiteFlag(game, deadIDs)
```

In `internal/discord/commands/werewolf_day.go`, `checkAndHandleGameEnd`'s locked section — right after the existing `hunterRevenged = append(hunterRevenged, applyHunterRevengeChain(...)...)` loop over `removedIDs` — add:

```go
		var deadIDs []string
		deadIDs = append(deadIDs, removedIDs...)
		for _, p := range hunterRevenged {
			deadIDs = append(deadIDs, p.UserID)
		}
		applyWolfCubBonusBiteFlag(game, deadIDs)
```

In `internal/discord/commands/werewolf.go`, `handleLeave` — right after the existing `hunterRevenged := applyHunterRevengeChain(game, userID)` line — add:

```go
	deadIDs := append([]string{userID}, idsOf(hunterRevenged)...)
	applyWolfCubBonusBiteFlag(game, deadIDs)
```

- [ ] **Step 10: Add the `WolfBonusBite` recap line**

In `internal/discord/commands/werewolf_day.go`, `formatRoundBlock` currently opens with the `WolfTargetLabel`/`WolfVoteTied` switch. Add a branch at the very top of that block, before the existing `if r.WolfTargetLabel != ""` check:

```go
	if r.WolfBonusBite && len(r.WolfBiteOutcomeLines) > 0 {
		b.WriteString("\n🐺🐺 Sói cắn 2 mục tiêu (Sói Con đã ngã xuống đêm trước):")
		for _, line := range r.WolfBiteOutcomeLines {
			fmt.Fprintf(&b, "\n%s", line)
		}
	} else if r.WolfTargetLabel != "" {
```

(change the following `if` to `} else if` — everything inside that original block stays exactly as-is, now just gated to the non-bonus-bite path.)

- [ ] **Step 11: Write the tests**

Add to `internal/discord/commands/werewolf_test.go`:

```go
func TestApplyWolfCubBonusBiteFlag(t *testing.T) {
	game := &werewolfGame{
		Players: map[string]*player{
			"cub":  {UserID: "cub", Role: roleWolfCub},
			"wolf": {UserID: "wolf", Role: roleWerewolf},
		},
	}
	applyWolfCubBonusBiteFlag(game, []string{"wolf"})
	if game.WolfBonusBiteNextNight {
		t.Error("a plain Werewolf's death must not arm the bonus bite")
	}
	applyWolfCubBonusBiteFlag(game, []string{"wolf", "cub"})
	if !game.WolfBonusBiteNextNight {
		t.Error("a Wolf Cub's death must arm the bonus bite")
	}
}

func TestFormatRoundBlockWolfBonusBite(t *testing.T) {
	r := roundLog{
		Round:                3,
		WolfBonusBite:        true,
		WolfBiteOutcomeLines: []string{"An: chết", "Binh: được Bác Sĩ cứu"},
	}
	got := formatRoundBlock(r)
	if !strings.Contains(got, "An: chết") || !strings.Contains(got, "Binh: được Bác Sĩ cứu") {
		t.Errorf("formatRoundBlock() = %q, want both outcome lines present", got)
	}
}
```

- [ ] **Step 12: Run tests to verify they fail, then pass**

Run: `go test ./internal/discord/commands/... -run 'TestApplyWolfCubBonusBiteFlag|TestFormatRoundBlockWolfBonusBite' -v`

Before Step 2/10: FAIL. After: PASS.

- [ ] **Step 13: Run the full suite and commit**

Run: `go build ./... && go vet ./internal/discord/commands/... && go test ./internal/discord/commands/...`

Expected: all pass, including every existing Witch/Hunter/wolf-vote test — none of them construct a Wolf Cub or arm the bonus bite, so `bonusBite` is always `false` and `resolvedTargets` always has length ≤1 in every pre-existing test scenario, keeping the original single-target message cascade byte-identical.

```bash
gofmt -l internal/discord/commands/werewolf.go internal/discord/commands/werewolf_night.go internal/discord/commands/werewolf_day.go internal/discord/commands/werewolf_test.go
git add internal/discord/commands/werewolf.go internal/discord/commands/werewolf_night.go internal/discord/commands/werewolf_day.go internal/discord/commands/werewolf_test.go
git commit -m "werewolf: add Wolf Cub's bonus second bite the night after it dies"
```

---

### Task 5: Traitor's wolf-roster DM

**Files:**
- Modify: `internal/discord/commands/werewolf.go` (`werewolfGame`, `startPreflight`, `handleLeave`)
- Modify: `internal/discord/commands/werewolf_night.go` (`finalizeNightResolution`)
- Modify: `internal/discord/commands/werewolf_day.go` (`resolveVote`, `checkAndHandleGameEnd`)
- Test: `internal/discord/commands/werewolf_test.go`

**Interfaces:**
- Consumes: `player.isWolf()` (Plan A), `alivePlayersOfRole` (existing — genuinely Role-exact here, correct as-is for "find the Traitor").
- Produces: `wolfRosterLabels(game *werewolfGame) []string`, `checkTraitorRosterChange(game *werewolfGame) *wolfRosterChangeDM`, `(cmd *WerewolfCommand) sendTraitorRosterDM(s, dm *wolfRosterChangeDM)`.

Spec §4d: DM the Traitor once at role assignment with the current wolf roster (everyone currently wolf-faction except the Traitor themself — a not-yet-converted Cursed Wolf is villager-faction so is naturally excluded, and a converted one is naturally included, both for free from the same `isWolf()` check used everywhere else in this plan), then re-DM only when that roster actually changes (a death, or a Cursed Wolf conversion).

- [ ] **Step 1: Add the roster-diffing state and helpers**

In `internal/discord/commands/werewolf.go`, add to `werewolfGame` (next to `NightBiteTargetIDs`):

```go
	TraitorLastRosterLabels []string // the wolf roster the Traitor was last DMed, sorted — nil until their first DM; compared against on every wolf-faction membership change to avoid re-sending an unchanged roster
```

Add these three functions right after `wolfVoteParticipant` (needs `sort`, so add `"sort"` to this file's import block alongside the existing `fmt`/`math/rand`/`strings`/`sync`/`sync/atomic`/`time`):

```go
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

// wolfRosterChangeDM is what checkTraitorRosterChange hands back for the
// caller to send after releasing cmd.mu.
type wolfRosterChangeDM struct {
	TraitorUserID string
	Labels        []string
}

// checkTraitorRosterChange must be called with cmd.mu held, after any
// wolf-faction membership change (a death by any cause, or a Cursed Wolf
// conversion). Returns non-nil only when a live Traitor exists AND the
// current wolf roster differs from the last one they were sent — updates
// game.TraitorLastRosterLabels as a side effect, so a repeat call with no
// further change is a cheap no-op. Safe to call unconditionally at the end
// of every death-application site's locked section.
func checkTraitorRosterChange(game *werewolfGame) *wolfRosterChangeDM {
	traitors := alivePlayersOfRole(game, roleTraitor)
	if len(traitors) == 0 {
		return nil
	}
	labels := wolfRosterLabels(game)
	if stringSlicesEqual(labels, game.TraitorLastRosterLabels) {
		return nil
	}
	game.TraitorLastRosterLabels = labels
	return &wolfRosterChangeDM{TraitorUserID: traitors[0].UserID, Labels: append([]string(nil), labels...)}
}

func stringSlicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// sendTraitorRosterDM sends dm's roster to the Traitor. No-op if dm is nil
// (checkTraitorRosterChange found no live Traitor or no change) — every
// call site can pass its result through unconditionally.
func (cmd *WerewolfCommand) sendTraitorRosterDM(s *discordgo.Session, dm *wolfRosterChangeDM) {
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
```

- [ ] **Step 2: Send the initial DM at role assignment**

In `internal/discord/commands/werewolf.go`, `startPreflight`'s `gameFound` closure currently ends with:

```go
	gameFound := func() bool {
		cmd.mu.Lock()
		defer cmd.mu.Unlock()
		game := cmd.gameIfCurrent(channelID, seq)
		if game == nil {
			return false
		}
		game.Players = players
		game.JoinOrder = joinOrder
		return true
	}()
	if !gameFound {
		return
	}
```

Replace with:

```go
	var initialTraitorDM *wolfRosterChangeDM
	gameFound := func() bool {
		cmd.mu.Lock()
		defer cmd.mu.Unlock()
		game := cmd.gameIfCurrent(channelID, seq)
		if game == nil {
			return false
		}
		game.Players = players
		game.JoinOrder = joinOrder
		initialTraitorDM = checkTraitorRosterChange(game)
		return true
	}()
	if !gameFound {
		return
	}
	cmd.sendTraitorRosterDM(s, initialTraitorDM)
```

- [ ] **Step 3: Re-DM on every death-application site**

In `internal/discord/commands/werewolf_night.go`, `finalizeNightResolution`'s locked section — right after the `applyWolfCubBonusBiteFlag(game, deadThisResolution)` call added in Task 4 — add:

```go
		traitorDM := checkTraitorRosterChange(game)
```

Thread `traitorDM *wolfRosterChangeDM` out through the function's outer `var` block the same way `bonusBite`/`poisonedPlayer` already are, and after the `if !gameFound { return }` check in the post-unlock section, add:

```go
	cmd.sendTraitorRosterDM(s, traitorDM)
```

In `internal/discord/commands/werewolf_day.go`, `resolveVote`'s locked section — right after the `applyWolfCubBonusBiteFlag(game, deadIDs)` call added in Task 4 — add:

```go
			traitorDM = checkTraitorRosterChange(game)
```

(declare `traitorDM *wolfRosterChangeDM` in the function's outer `var (...)` block, next to `hunterRevenged`), then after the existing `if !gameFound { return }` check, before the `var resultMsg string` block, add:

```go
	cmd.sendTraitorRosterDM(s, traitorDM)
```

In `internal/discord/commands/werewolf_day.go`, `checkAndHandleGameEnd`'s locked section — right after its own `applyWolfCubBonusBiteFlag(game, deadIDs)` call from Task 4 — add:

```go
		traitorDM = checkTraitorRosterChange(game)
```

(declare `traitorDM *wolfRosterChangeDM` in the function's outer `var (...)` block), then in the `if !ended { ... }` branch (right after the existing `removeFromAliveThread` loops, before the `announceAFKRemovals`/`announceHunterRevenge` calls), add:

```go
		cmd.sendTraitorRosterDM(s, traitorDM)
```

(No corresponding call needed in the `ended` branch — the game is over, and there is deliberately no "final roster" DM; the Traitor already sees everyone's true role in the existing end-of-game reveal.)

In `internal/discord/commands/werewolf.go`, `handleLeave` — right after the `applyWolfCubBonusBiteFlag(game, deadIDs)` line added in Task 4 — add:

```go
	traitorDM := checkTraitorRosterChange(game)
```

then, after `cmd.mu.Unlock()` (alongside the existing `removeFromAliveThread` calls, before `respondSuccess`), add:

```go
	cmd.sendTraitorRosterDM(s, traitorDM)
```

- [ ] **Step 4: Write the tests**

Add to `internal/discord/commands/werewolf_test.go`:

```go
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

func TestCheckTraitorRosterChangeNoOpWhenUnchanged(t *testing.T) {
	game := &werewolfGame{
		JoinOrder: []string{"w1", "traitor1"},
		Players: map[string]*player{
			"w1":       {UserID: "w1", Label: "Wolf", Role: roleWerewolf, Faction: factionWolf, Alive: true},
			"traitor1": {UserID: "traitor1", Label: "Trai", Role: roleTraitor, Faction: factionWolf, Alive: true},
		},
	}
	first := checkTraitorRosterChange(game)
	if first == nil || first.TraitorUserID != "traitor1" || len(first.Labels) != 1 || first.Labels[0] != "Wolf" {
		t.Fatalf("first checkTraitorRosterChange() = %+v, want a DM naming Wolf", first)
	}
	second := checkTraitorRosterChange(game)
	if second != nil {
		t.Errorf("second checkTraitorRosterChange() with no roster change = %+v, want nil", second)
	}

	game.Players["w1"].Alive = false
	third := checkTraitorRosterChange(game)
	if third == nil || len(third.Labels) != 0 {
		t.Errorf("checkTraitorRosterChange() after the only wolf dies = %+v, want an empty-roster DM", third)
	}
}
```

- [ ] **Step 5: Run tests to verify they fail, then pass**

Run: `go test ./internal/discord/commands/... -run 'TestWolfRosterLabels|TestCheckTraitorRosterChangeNoOpWhenUnchanged' -v`

Before Step 1: FAIL (`wolfRosterLabels`/`checkTraitorRosterChange` undefined). After: PASS.

- [ ] **Step 6: Run the full suite and commit**

Run: `go build ./... && go vet ./internal/discord/commands/... && go test ./internal/discord/commands/...`

Expected: all pass. `checkTraitorRosterChange` returns `nil` at every call site in every existing test/game that has no `roleTraitor` player, so every pre-existing code path this task touches (`resolveVote`, `checkAndHandleGameEnd`, `handleLeave`, `finalizeNightResolution`, `startPreflight`) sends no Traitor DM and behaves exactly as before.

```bash
gofmt -l internal/discord/commands/werewolf.go internal/discord/commands/werewolf_night.go internal/discord/commands/werewolf_day.go internal/discord/commands/werewolf_test.go
git add internal/discord/commands/werewolf.go internal/discord/commands/werewolf_night.go internal/discord/commands/werewolf_day.go internal/discord/commands/werewolf_test.go
git commit -m "werewolf: DM the Traitor the wolf roster at deal time and on every roster change"
```

---

### Task 6: Verification (no live-Discord E2E — these roles aren't reachable yet)

**Files:** none (verification only)

Unlike Plan A/B's final task, this one **cannot** be a real Discord playtest handed to the user — per this plan's Global Constraints, Wolf Cub, Cursed Wolf, and Traitor are never dealt by `rolesForPlayerCount`, so there is no way to start a real game containing any of them until Plan E's manual role-pick mode exists. Flag this task to the user rather than dispatching it to a subagent; it's a review-and-confirm step, not new code.

Checklist to hand over:

- [ ] Skim `internal/discord/commands/werewolf_test.go` for the new tests from Tasks 1-5 (`TestDealRolesAssignsWolfCubCursedWolfTraitor`, `TestWolfVoteParticipant`, `TestResolveWolfBiteOutcome`, `TestFormatRoundBlockCursedWolfConverted`, `TestApplyWolfCubBonusBiteFlag`, `TestFormatRoundBlockWolfBonusBite`, `TestWolfRosterLabels`, `TestCheckTraitorRosterChangeNoOpWhenUnchanged`) and confirm they read as genuine behavioral checks, not tests that would pass vacuously.
- [ ] Run `go test ./internal/discord/commands/... -v` once yourself and skim the full output — confirm no test was silently skipped and the full existing Plan A/B suite still passes unchanged (nothing in this plan should have altered any pre-existing test's expected outcome).
- [ ] Confirm the plan's central design decision — Wolf Cub/Cursed Wolf/Traitor are correctly implemented but genuinely unreachable in a real game until Plan E ships — matches what you actually want. If you'd rather have a way to force-deal these roles sooner (e.g., a temporary debug flag) for earlier live testing, that's a scope decision for a new, explicit request — it's out of scope for this plan as written.
- [ ] When Plan E ships and these roles become reachable via the manual role-pick UI, re-open this checklist as a real Discord playtest: verify a Wolf Cub's death arms next night's 2-target bite, the Witch's heal select-menu appears only on a 2-target round, a Cursed Wolf that's bitten-and-not-saved converts instead of dying (and the Seer sees them as "not wolf" before that, "wolf" after), and the Traitor gets exactly one DM at deal time plus exactly one more per actual roster change (not one per night).

---

## Self-Review

**Spec coverage:** §4b (Wolf Cub) → Tasks 2 + 4. §4c (Cursed Wolf) → Task 3. §4d (Traitor) → Task 5. §3a's "these three never appear in the fixed table" constraint → stated in Global Constraints and Task 1 (no `rolesForPlayerCount` changes). §5's `WolfBonusBite`/`CursedWolfConverted` recap fields → Tasks 3 + 4 (the spec's other §5 fields — `WitchSavedLabel`/`WitchPoisonedLabel`/`HunterRevengeLabel` — already exist from Plan B). §4i (public per-role progress notifications) — Wolf Cub/Cursed Wolf have no notification of their own (they act through the existing wolf-vote notification, already shipped in Plan B's Task 4); Traitor explicitly has none per spec §4i's own text ("Traitor: no notification"). §7 (cleanup) — no new Discord-side artifacts (no new threads/overwrites) are introduced by this plan, so nothing new to clean up; all new `werewolfGame` fields are freed by the existing `delete(cmd.games, channelID)` on every terminal path, same as every prior plan's new fields.

**Out of this plan's scope, confirmed correctly deferred:** §3b (manual-pick UI), §4g (Disruptor), §4h (Cupid), §6 (`/help werewolf` guide) — all Plan D/E territory per the five-plan split.
