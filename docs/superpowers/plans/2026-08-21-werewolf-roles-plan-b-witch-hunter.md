# Werewolf Extended Roles — Plan B: Witch + Hunter Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add the Witch and Hunter roles to `/werewolf`, on top of Plan A's foundation (faction model, extended role catalogue, plurality wolf vote). This is the second of five sequential plans implementing `docs/superpowers/specs/2026-08-21-werewolf-roles-design.md`.

**Architecture:** Hunter is additive — a nightly DM target-pick plus a revenge-kill chain wired into every existing death-application site. Witch requires a real structural change: the night phase gains a second, *sequential* stage after the wolf vote resolves (she must know the bite target before deciding), which didn't exist before — every prior night role acts in parallel.

**Tech Stack:** Go, discordgo v0.29.0, existing `sync.Mutex`-guarded `map[string]*werewolfGame` pattern.

## Global Constraints

- This is Plan B of five. Plan A (faction model, 11-role catalogue, extended 5-20 table, plurality wolf vote) is already complete and merged into this branch. Do not re-implement anything from Plan A.
- Per spec §3a, the fixed-ratio table stays limited to roles with real mechanics — Wolf Cub, Cursed Wolf, Traitor, Disruptor, Cupid still have zero behavior until Plans C/D. This plan is the one that introduces Witch and Hunter into the table for the first time (at the same player counts where Plan A already introduced 3 wolves — spec §3a's own philosophy, not a hard requirement this plan must re-derive from scratch).
- Every `discordgo.MessageSend`/`InteractionResponseData`/`MessageEdit` whose `Content` contains a `<@id>` mention must set `AllowedMentions` explicitly.
- No DB/migration changes — werewolf remains fully in-memory.
- After editing any file, run `gofmt -l <file>` on every file you touched; if it lists the file, run `gofmt -w <file>` before your final build/test step.
- Every read/write of `*werewolfGame`/`*player` fields inside the interactive engine (`werewolf.go`, `werewolf_night.go`, `werewolf_day.go`) must happen strictly inside the existing `cmd.mu.Lock()`/`defer cmd.mu.Unlock()` critical sections — this codebase has a documented history of concurrency bugs from unguarded reads/writes.
- Per spec §4f: Hunter's revenge-kill applies on death **by any cause** — wolf-kill, Witch poison, day-lynch, AFK-removal, and voluntary leave — applied immediately in the same resolution step as the triggering death (no "next morning" delay, even for the day-lynch case).

---

### Task 1: Struct fields, extended `roleRatio`, and the table entries that introduce Witch + Hunter

**Files:**
- Modify: `internal/discord/commands/werewolf.go` (`roleRatio`, `rolesForPlayerCount`, `assignRoles`, `startPreflight`'s role-count announcement)
- Modify: `internal/discord/commands/werewolf_test.go` (update `TestRolesForPlayerCount`'s table)

**Interfaces:**
- Produces: `roleRatio.Witches int`, `roleRatio.Hunters int` — consumed by `assignRoles` (this task) and read nowhere else yet.
- Consumes: `roleWitch`, `roleHunter` (already exist from Plan A, unused until now).

- [ ] **Step 1: Write the failing test for the updated table**

In `internal/discord/commands/werewolf_test.go`, replace `TestRolesForPlayerCount`'s cases (leave the function signature and the rest of the file untouched):

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
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/discord/commands/... -run TestRolesForPlayerCount -v`
Expected: compile error (`roleRatio` has no field `Witches`/`Hunters` yet) or a value mismatch — either way, it must not pass yet.

- [ ] **Step 3: Extend `roleRatio` and `rolesForPlayerCount`**

In `internal/discord/commands/werewolf.go`, replace:

```go
// roleRatio describes how many of each special role a game of a given size gets.
type roleRatio struct {
	Werewolves int
	Seers      int
	Doctors    int
}
```

with:

```go
// roleRatio describes how many of each special role a game of a given size gets.
type roleRatio struct {
	Werewolves int
	Seers      int
	Doctors    int
	Witches    int
	Hunters    int
}
```

Replace `rolesForPlayerCount`:

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
		return roleRatio{Werewolves: 3, Seers: 1, Doctors: 1, Witches: 1, Hunters: 1}, nil
	case n <= 17:
		return roleRatio{Werewolves: 4, Seers: 1, Doctors: 1, Witches: 1, Hunters: 1}, nil
	default: // 18-20
		return roleRatio{Werewolves: 5, Seers: 1, Doctors: 1, Witches: 1, Hunters: 1}, nil
	}
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./internal/discord/commands/... -run TestRolesForPlayerCount -v`
Expected: `PASS`.

- [ ] **Step 5: Deal Witch and Hunter in `assignRoles`**

In `internal/discord/commands/werewolf.go`, replace:

```go
	assign(ratio.Werewolves, roleWerewolf)
	assign(ratio.Seers, roleSeer)
	assign(ratio.Doctors, roleDoctor)
	for ; idx < len(shuffled); idx++ {
```

with:

```go
	assign(ratio.Werewolves, roleWerewolf)
	assign(ratio.Seers, roleSeer)
	assign(ratio.Doctors, roleDoctor)
	assign(ratio.Witches, roleWitch)
	assign(ratio.Hunters, roleHunter)
	for ; idx < len(shuffled); idx++ {
```

- [ ] **Step 6: Add a test confirming `assignRoles` actually deals Witch/Hunter at a count that has them**

Append to `internal/discord/commands/werewolf_test.go`:

```go
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
```

Add `"fmt"` to this test file's imports if not already present (check the current import block — it currently has `"strings"`, `"testing"`, `"time"`; add `"fmt"` to that list).

- [ ] **Step 7: Run the test to verify it passes**

Run: `go test ./internal/discord/commands/... -run TestAssignRolesDealsWitchAndHunterAt11Players -v`
Expected: `PASS`.

- [ ] **Step 8: Update the role-count breakdown announcement**

In `internal/discord/commands/werewolf.go`, `startPreflight` currently has:

```go
	pingText, allowedMentions := pingOnce(joinOrder)
	roleCounts := "" // best-effort: if this somehow errors, just omit the breakdown rather than failing the announcement
	if ratio, err := rolesForPlayerCount(len(joinOrder)); err == nil {
		villagers := len(joinOrder) - ratio.Werewolves - ratio.Seers - ratio.Doctors
		roleCounts = fmt.Sprintf("\n🐺 Sói: %d · 🔮 Tiên Tri: %d · 💉 Bác Sĩ: %d · 👤 Dân Làng: %d",
			ratio.Werewolves, ratio.Seers, ratio.Doctors, villagers)
	}
```

Replace with:

```go
	pingText, allowedMentions := pingOnce(joinOrder)
	roleCounts := "" // best-effort: if this somehow errors, just omit the breakdown rather than failing the announcement
	if ratio, err := rolesForPlayerCount(len(joinOrder)); err == nil {
		villagers := len(joinOrder) - ratio.Werewolves - ratio.Seers - ratio.Doctors - ratio.Witches - ratio.Hunters
		roleCounts = fmt.Sprintf("\n🐺 Sói: %d · 🔮 Tiên Tri: %d · 💉 Bác Sĩ: %d · 🧙 Phù Thủy: %d · 🏹 Thợ Săn: %d · 👤 Dân Làng: %d",
			ratio.Werewolves, ratio.Seers, ratio.Doctors, ratio.Witches, ratio.Hunters, villagers)
	}
```

- [ ] **Step 9: Build and run the full test suite**

Run: `go build ./... && go vet ./internal/discord/commands/... && go test ./internal/discord/commands/... -v`
Expected: build succeeds, all tests `PASS`.

- [ ] **Step 10: Commit**

```bash
git add internal/discord/commands/werewolf.go internal/discord/commands/werewolf_test.go
git commit -m "werewolf: deal Witch and Hunter starting at 11 players

roleRatio gains Witches/Hunters fields; rolesForPlayerCount's 11-20
rows now include exactly 1 of each. No behavior yet -- that's Tasks 2
(Hunter) and 3 (Witch)."
```

---

### Task 2: Hunter — nightly target pick, and a revenge-kill chain wired into the day-lynch, AFK-removal, and voluntary-leave death paths

**Files:**
- Modify: `internal/discord/commands/werewolf.go` (`werewolfGame` struct: `HunterTarget`, `HunterActedThisRound`; `roundLog`: `HunterRevengeLabel`; `HandleComponent` dispatch; `handleLeave`)
- Modify: `internal/discord/commands/werewolf_night.go` (`runNightPhase`'s reset+snapshot block, `nightActionsComplete`, AFK-strike loop in `resolveNight` — wait, this task does NOT touch `resolveNight`'s wolf-kill path, see note below; new `dmHunterPrompt`, `handleHunterTarget`, `applyHunterRevengeChain`, `aliveExcept`)
- Modify: `internal/discord/commands/werewolf_day.go` (`resolveVote`, `checkAndHandleGameEnd`, `formatRoundBlock`)
- Test: `internal/discord/commands/werewolf_test.go`

**Interfaces:**
- Produces: `applyHunterRevengeChain(game *werewolfGame, deadID string) []*player` (in `werewolf_night.go`, must be called with `cmd.mu` held — returns value-copy snapshots safe to read after unlocking) — Task 3 (Witch) will call this too, from its own new death-application path (Witch poison), which doesn't exist yet.
- Produces: `aliveExcept(game *werewolfGame, excludeID string) []*player` (in `werewolf_night.go`) — a small helper for "every alive player except this one," used by Hunter's target list now and reusable by Witch's poison target list in Task 3.
- Note: this task intentionally does **not** touch `resolveNight`'s wolf-kill path or add Hunter to `runNightPhase`'s existing parallel-prompt list beyond what's needed to send his own DM — Task 3 restructures `resolveNight` into two stages anyway, and will wire the wolf-kill/poison death paths into `applyHunterRevengeChain` itself, reusing what this task builds.

- [ ] **Step 1: Add Hunter's game-state fields and the recap field**

In `internal/discord/commands/werewolf.go`, replace:

```go
	DoctorProtect                 string                         // doctor's pick this round
	DoctorLastPick                string                         // last round's pick — can't repeat back-to-back
	Votes                         map[string]string              // voterID -> targetID, reset each day
```

with:

```go
	DoctorProtect                 string                         // doctor's pick this round
	DoctorLastPick                string                         // last round's pick — can't repeat back-to-back
	HunterTarget                  string                         // persists across rounds until Hunter explicitly changes it
	HunterActedThisRound          bool                           // reset each round; tracks whether Hunter (re-)picked this round, for nightActionsComplete/AFK
	Votes                         map[string]string              // voterID -> targetID, reset each day
```

Replace `roundLog`:

```go
type roundLog struct {
	Round             int
	WolfTargetLabel   string
	WolfTargetSaved   bool
	WolfVoteTied      bool
	DoctorTargetLabel string
	SeerTargetLabel   string
	SeerResult        role
	VoteBreakdown     map[string][]string // target label (or voteSkipLabel) -> voter labels who chose it
	LynchedLabel      string
}
```

with:

```go
type roundLog struct {
	Round             int
	WolfTargetLabel   string
	WolfTargetSaved   bool
	WolfVoteTied      bool
	DoctorTargetLabel string
	SeerTargetLabel   string
	SeerResult        role
	VoteBreakdown     map[string][]string // target label (or voteSkipLabel) -> voter labels who chose it
	LynchedLabel      string
	HunterRevengeLabel string // who died from Hunter's revenge this round, "" if it didn't trigger
}
```

- [ ] **Step 2: Add the Hunter target-select custom ID and dispatch case**

In `internal/discord/commands/werewolf_night.go`, replace:

```go
const (
	werewolfWolfTargetID    = "ww:wolftarget:"
	werewolfSeerCheckID     = "ww:seercheck:"
	werewolfDoctorProtectID = "ww:doctorprotect:"
```

with:

```go
const (
	werewolfWolfTargetID    = "ww:wolftarget:"
	werewolfSeerCheckID     = "ww:seercheck:"
	werewolfDoctorProtectID = "ww:doctorprotect:"
	werewolfHunterTargetID  = "ww:huntertarget:"
```

In `internal/discord/commands/werewolf.go`, `HandleComponent` currently ends:

```go
	case strings.HasPrefix(customID, werewolfDoctorProtectID):
		cmd.handleDoctorProtect(s, i, strings.TrimPrefix(customID, werewolfDoctorProtectID))
	case strings.HasPrefix(customID, werewolfVoteID):
		cmd.handleVote(s, i, strings.TrimPrefix(customID, werewolfVoteID))
	}
}
```

Replace with:

```go
	case strings.HasPrefix(customID, werewolfDoctorProtectID):
		cmd.handleDoctorProtect(s, i, strings.TrimPrefix(customID, werewolfDoctorProtectID))
	case strings.HasPrefix(customID, werewolfHunterTargetID):
		cmd.handleHunterTarget(s, i, strings.TrimPrefix(customID, werewolfHunterTargetID))
	case strings.HasPrefix(customID, werewolfVoteID):
		cmd.handleVote(s, i, strings.TrimPrefix(customID, werewolfVoteID))
	}
}
```

- [ ] **Step 3: Add `aliveExcept` and `applyHunterRevengeChain`**

In `internal/discord/commands/werewolf_night.go`, add these two functions right after `aliveNonWolves`:

```go
// aliveExcept returns every alive player in game except the one identified
// by excludeID (typically the acting player themself). Order follows
// game.JoinOrder, same as the other alive-roster helpers in this file.
func aliveExcept(game *werewolfGame, excludeID string) []*player {
	var result []*player
	for _, id := range game.JoinOrder {
		p := game.Players[id]
		if p.Alive && p.UserID != excludeID {
			result = append(result, p)
		}
	}
	return result
}

// applyHunterRevengeChain checks whether the player who just died (deadID)
// was the Hunter with a locked-in target; if so, kills that target too
// (unless already dead), and recurses in case the newly-killed target was
// ALSO a Hunter with their own target (unreachable today, since every
// player count deals exactly one Hunter, but this stays correct if that
// ever changes). Must be called with cmd.mu held. Returns value-copy
// snapshots of everyone killed by the chain, safe to read after unlocking
// — callers append these to their own kill lists for alive-thread removal,
// win-condition re-checks, and announcements.
func applyHunterRevengeChain(game *werewolfGame, deadID string) []*player {
	var revenged []*player
	queue := []string{deadID}
	for len(queue) > 0 {
		id := queue[0]
		queue = queue[1:]
		dead, ok := game.Players[id]
		if !ok || dead.Role != roleHunter {
			continue
		}
		targetID := game.HunterTarget
		if targetID == "" {
			continue
		}
		target, exists := game.Players[targetID]
		if !exists || !target.Alive {
			continue
		}
		target.Alive = false
		revenged = append(revenged, copyPlayer(target))
		queue = append(queue, targetID)
	}
	return revenged
}
```

- [ ] **Step 4: Write failing tests for `applyHunterRevengeChain`**

Append to `internal/discord/commands/werewolf_test.go`:

```go
func TestApplyHunterRevengeChainTriggers(t *testing.T) {
	game := &werewolfGame{
		JoinOrder:   []string{"hunter1", "v1"},
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
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `go test ./internal/discord/commands/... -run TestApplyHunterRevengeChain -v`
Expected: all 4 `PASS`.

- [ ] **Step 6: Add the Hunter DM prompt and its handler**

In `internal/discord/commands/werewolf_night.go`, add right after `dmDoctorPrompt`:

```go
func (cmd *WerewolfCommand) dmHunterPrompt(s *discordgo.Session, channelID string, hunter *player, targets []*player) {
	dm, err := s.UserChannelCreate(hunter.UserID)
	if err != nil {
		log.Error().Err(err).Msg("werewolf: failed to open DM for hunter")
		return
	}
	if _, err := s.ChannelMessageSendComplex(dm.ID, &discordgo.MessageSend{
		Content:    "🏹 Chọn người bạn muốn chỉ định — nếu bạn chết (bất kỳ lý do gì), người này sẽ chết theo. Có thể đổi lại vào đêm sau nếu còn sống.",
		Components: targetSelectMenu(werewolfHunterTargetID+channelID, "Chỉ định mục tiêu", targets),
	}); err != nil {
		log.Error().Err(err).Msg("werewolf: failed to DM hunter prompt")
	}
}
```

Add `handleHunterTarget` right after `handleDoctorProtect`:

```go
func (cmd *WerewolfCommand) handleHunterTarget(s *discordgo.Session, i *discordgo.InteractionCreate, channelID string) {
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
	clicker, isHunter := game.Players[clickerID]
	if !isHunter || clicker.Role != roleHunter || !clicker.Alive {
		cmd.mu.Unlock()
		respondComponentEphemeral(s, i, "Chỉ Thợ Săn còn sống mới được chỉ định.")
		return
	}
	if target, exists := game.Players[targetID]; !exists || !target.Alive {
		cmd.mu.Unlock()
		respondComponentEphemeral(s, i, "Mục tiêu không còn hợp lệ.")
		return
	}
	game.HunterTarget = targetID
	game.HunterActedThisRound = true
	cmd.mu.Unlock()

	respondComponentEphemeralMentioned(s, i, fmt.Sprintf("✅ Đã chỉ định: <@%s>.", targetID))
}
```

- [ ] **Step 7: Wire Hunter into `runNightPhase`'s parallel prompts**

In `internal/discord/commands/werewolf_night.go`, `runNightPhase`'s `var` block currently is:

```go
	var (
		gameFound      bool
		round          int
		summary        string
		guildID        string
		aliveThreadID  string
		wolves         []*player
		nonWolfTargets []*player
		seer           *player
		seerTargets    []*player
		doctor         *player
		doctorTargets  []*player
	)
```

Replace with:

```go
	var (
		gameFound      bool
		round          int
		summary        string
		guildID        string
		aliveThreadID  string
		wolves         []*player
		nonWolfTargets []*player
		seer           *player
		seerTargets    []*player
		doctor         *player
		doctorTargets  []*player
		hunter         *player
		hunterTargets  []*player
	)
```

Inside the locked block, replace:

```go
		game.WolfVotes = make(map[string]string)
		game.SeerActed = false
		game.SeerTarget = ""
		game.SeerResult = roleVillager
		game.DoctorProtect = ""
		game.History = append(game.History, roundLog{Round: game.Round})
```

with:

```go
		game.WolfVotes = make(map[string]string)
		game.SeerActed = false
		game.SeerTarget = ""
		game.SeerResult = roleVillager
		game.DoctorProtect = ""
		game.HunterActedThisRound = false
		game.History = append(game.History, roundLog{Round: game.Round})
```

(`HunterTarget` is deliberately NOT reset here — it persists across rounds until the Hunter explicitly changes it, per spec §4f.)

Still inside the same locked block, replace:

```go
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
	}()
```

with:

```go
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

		if hunters := alivePlayersOfRole(game, roleHunter); len(hunters) > 0 {
			hunter = copyPlayer(hunters[0])
			hunterTargets = copyPlayers(aliveExcept(game, hunter.UserID))
		}
	}()
```

After the locked block, replace:

```go
	cmd.startWolfThread(s, channelID, round, wolves, nonWolfTargets, seq)
	if seer != nil {
		cmd.dmSeerPrompt(s, channelID, seer, seerTargets)
	}
	if doctor != nil {
		cmd.dmDoctorPrompt(s, channelID, doctor, doctorTargets)
	}
```

with:

```go
	cmd.startWolfThread(s, channelID, round, wolves, nonWolfTargets, seq)
	if seer != nil {
		cmd.dmSeerPrompt(s, channelID, seer, seerTargets)
	}
	if doctor != nil {
		cmd.dmDoctorPrompt(s, channelID, doctor, doctorTargets)
	}
	if hunter != nil {
		cmd.dmHunterPrompt(s, channelID, hunter, hunterTargets)
	}
```

- [ ] **Step 8: Gate night completion on Hunter having acted**

In `internal/discord/commands/werewolf_night.go`, replace `nightActionsComplete`:

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
	if len(alivePlayersOfRole(game, roleHunter)) > 0 && !game.HunterActedThisRound {
		return false
	}
	return true
}
```

- [ ] **Step 9: Add Hunter's AFK-strike loop to `resolveNight`**

In `internal/discord/commands/werewolf_night.go`, `resolveNight`'s locked block has three AFK-strike loops (wolves, seer, doctor) ending with:

```go
		for _, p := range alivePlayersOfRole(game, roleDoctor) {
			if game.DoctorProtect != "" {
				game.AFKStrikes[p.UserID] = 0
			} else {
				game.AFKStrikes[p.UserID]++
			}
		}
```

Add a fourth loop right after it:

```go
		for _, p := range alivePlayersOfRole(game, roleDoctor) {
			if game.DoctorProtect != "" {
				game.AFKStrikes[p.UserID] = 0
			} else {
				game.AFKStrikes[p.UserID]++
			}
		}
		for _, p := range alivePlayersOfRole(game, roleHunter) {
			if game.HunterActedThisRound {
				game.AFKStrikes[p.UserID] = 0
			} else {
				game.AFKStrikes[p.UserID]++
			}
		}
```

- [ ] **Step 10: Wire the revenge chain into `resolveVote`'s lynch**

In `internal/discord/commands/werewolf_day.go`, `resolveVote`'s `var` block:

```go
	var (
		gameFound     bool
		lynchedPlayer *player
		revealOnDeath bool
		summary       string
		aliveThreadID string
	)
```

becomes:

```go
	var (
		gameFound       bool
		lynchedPlayer   *player
		hunterRevenged  []*player
		revealOnDeath   bool
		summary         string
		aliveThreadID   string
	)
```

Inside the locked block, replace:

```go
		top, tie := tallyVotes(game.Votes)
		if !tie && len(top) == 1 {
			if target, exists := game.Players[top[0]]; exists && target.Alive {
				target.Alive = false
				lynchedPlayer = target
			}
		}
		revealOnDeath = game.RevealOnDeath
		summary = aliveDeadSummary(game)
```

with:

```go
		top, tie := tallyVotes(game.Votes)
		if !tie && len(top) == 1 {
			if target, exists := game.Players[top[0]]; exists && target.Alive {
				target.Alive = false
				lynchedPlayer = target
			}
		}
		if lynchedPlayer != nil {
			hunterRevenged = applyHunterRevengeChain(game, lynchedPlayer.UserID)
		}
		revealOnDeath = game.RevealOnDeath
		summary = aliveDeadSummary(game)
```

Still inside the locked block, replace:

```go
			if lynchedPlayer != nil {
				entry.LynchedLabel = lynchedPlayer.Label
			}
		}
	}()
	if !gameFound {
		return
	}
	if lynchedPlayer != nil {
		removeFromAliveThread(s, aliveThreadID, lynchedPlayer.UserID)
	}
```

with:

```go
			if lynchedPlayer != nil {
				entry.LynchedLabel = lynchedPlayer.Label
			}
			if len(hunterRevenged) > 0 {
				entry.HunterRevengeLabel = hunterRevenged[0].Label
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
```

Finally, in the same function, replace the result-message assembly:

```go
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
		Content:         resultMsg + "\n\n" + summary,
		AllowedMentions: &discordgo.MessageAllowedMentions{},
	}); err != nil {
		log.Error().Err(err).Msg("werewolf: failed to post vote result")
	}
```

with:

```go
	var resultMsg string
	switch {
	case lynchedPlayer != nil && revealOnDeath:
		resultMsg = fmt.Sprintf("⚖️ **%s** bị treo cổ. Vai trò: %s.", lynchedPlayer.Label, lynchedPlayer.Role.label())
	case lynchedPlayer != nil:
		resultMsg = fmt.Sprintf("⚖️ **%s** bị treo cổ.", lynchedPlayer.Label)
	default:
		resultMsg = "🤷 Không đủ số phiếu áp đảo, không ai bị treo cổ hôm nay."
	}
	for _, p := range hunterRevenged {
		resultMsg += fmt.Sprintf("\n🏹 **%s** cũng chết theo vì Thợ Săn.", p.Label)
	}

	if _, err := s.ChannelMessageSendComplex(channelID, &discordgo.MessageSend{
		Content:         resultMsg + "\n\n" + summary,
		AllowedMentions: &discordgo.MessageAllowedMentions{},
	}); err != nil {
		log.Error().Err(err).Msg("werewolf: failed to post vote result")
	}
```

- [ ] **Step 11: Wire the revenge chain into `checkAndHandleGameEnd`'s AFK-removal batch**

In `internal/discord/commands/werewolf_day.go`, `checkAndHandleGameEnd`'s `var` block:

```go
	var (
		gameFound       bool
		ended           bool
		removedNames    []string
		removedIDs      []string
		threadID        string
		result          gameResult
		endedGame       *werewolfGame
		aliveThreadID   string
		guildID         string
		priorOverwrite  *discordgo.PermissionOverwrite
		channelUnlocked bool
	)
```

becomes:

```go
	var (
		gameFound       bool
		ended           bool
		removedNames    []string
		removedIDs      []string
		hunterRevenged  []*player
		threadID        string
		result          gameResult
		endedGame       *werewolfGame
		aliveThreadID   string
		guildID         string
		priorOverwrite  *discordgo.PermissionOverwrite
		channelUnlocked bool
	)
```

Inside the locked block, replace:

```go
		for _, id := range game.JoinOrder {
			p := game.Players[id]
			if p.Alive && shouldRemoveForAFK(game.AFKStrikes[id]) {
				p.Alive = false
				removedNames = append(removedNames, p.Label)
				removedIDs = append(removedIDs, id)
			}
		}

		result = checkWinCondition(game.Players)
```

with:

```go
		for _, id := range game.JoinOrder {
			p := game.Players[id]
			if p.Alive && shouldRemoveForAFK(game.AFKStrikes[id]) {
				p.Alive = false
				removedNames = append(removedNames, p.Label)
				removedIDs = append(removedIDs, id)
			}
		}
		for _, id := range removedIDs {
			hunterRevenged = append(hunterRevenged, applyHunterRevengeChain(game, id)...)
		}
		if len(hunterRevenged) > 0 && len(game.History) > 0 {
			game.History[len(game.History)-1].HunterRevengeLabel = hunterRevenged[0].Label
		}

		result = checkWinCondition(game.Players)
```

(Placing the revenge loop before `checkWinCondition` is what makes the revenge-killed player's death actually count toward the win check.)

Further down the same function, replace:

```go
	if !ended {
		for _, id := range removedIDs {
			removeFromAliveThread(s, aliveThreadID, id)
		}
		if len(removedNames) > 0 {
			cmd.announceAFKRemovals(s, channelID, removedNames)
		}
		return false
	}
```

with:

```go
	if !ended {
		for _, id := range removedIDs {
			removeFromAliveThread(s, aliveThreadID, id)
		}
		for _, p := range hunterRevenged {
			removeFromAliveThread(s, aliveThreadID, p.UserID)
		}
		if len(removedNames) > 0 {
			cmd.announceAFKRemovals(s, channelID, removedNames)
		}
		cmd.announceHunterRevenge(s, channelID, hunterRevenged)
		return false
	}
```

(The `ended` branch's `announceGameEnd` already reveals every player's final `Alive` status, so a separate revenge announcement isn't needed there — only the "game continues" branch needs its own notice.)

- [ ] **Step 12: Add `announceHunterRevenge` and wire the revenge chain into `handleLeave`**

In `internal/discord/commands/werewolf_day.go`, add right after `announceAFKRemovals`:

```go
func (cmd *WerewolfCommand) announceHunterRevenge(s *discordgo.Session, channelID string, revenged []*player) {
	for _, p := range revenged {
		if _, err := s.ChannelMessageSendComplex(channelID, &discordgo.MessageSend{
			Content:         fmt.Sprintf("🏹 **%s** cũng chết theo vì Thợ Săn.", p.Label),
			AllowedMentions: &discordgo.MessageAllowedMentions{},
		}); err != nil {
			log.Error().Err(err).Msg("werewolf: failed to announce hunter revenge")
		}
	}
}
```

In `internal/discord/commands/werewolf.go`, `handleLeave`'s mid-game branch currently is:

```go
	p, inGame := game.Players[userID]
	if !inGame || !p.Alive {
		cmd.mu.Unlock()
		respondError(s, i, "Bạn không ở trong ván đang chạy này.")
		return
	}
	p.Alive = false
	revealOnDeath := game.RevealOnDeath
	channelID := game.ChannelID
	seq := game.Seq
	aliveThreadID := game.AliveThreadID
	cmd.mu.Unlock()

	removeFromAliveThread(s, aliveThreadID, userID)
	respondSuccess(s, i, "Đã rời ván. Bạn sẽ được coi như đã bị loại.")

	if cmd.checkAndHandleGameEnd(s, channelID, seq) {
		return
	}

	msg := fmt.Sprintf("💨 **%s** đã rời ván.", p.Label)
	if revealOnDeath {
		msg = fmt.Sprintf("💨 **%s** đã rời ván. Vai trò: %s.", p.Label, p.Role.label())
	}
	if _, err := s.ChannelMessageSendComplex(channelID, &discordgo.MessageSend{
		Content:         msg,
		AllowedMentions: &discordgo.MessageAllowedMentions{},
	}); err != nil {
		log.Error().Err(err).Msg("werewolf: failed to announce voluntary leave")
	}
}
```

Replace with:

```go
	p, inGame := game.Players[userID]
	if !inGame || !p.Alive {
		cmd.mu.Unlock()
		respondError(s, i, "Bạn không ở trong ván đang chạy này.")
		return
	}
	p.Alive = false
	hunterRevenged := applyHunterRevengeChain(game, userID)
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

	msg := fmt.Sprintf("💨 **%s** đã rời ván.", p.Label)
	if revealOnDeath {
		msg = fmt.Sprintf("💨 **%s** đã rời ván. Vai trò: %s.", p.Label, p.Role.label())
	}
	for _, hp := range hunterRevenged {
		msg += fmt.Sprintf("\n🏹 **%s** cũng chết theo vì Thợ Săn.", hp.Label)
	}
	if _, err := s.ChannelMessageSendComplex(channelID, &discordgo.MessageSend{
		Content:         msg,
		AllowedMentions: &discordgo.MessageAllowedMentions{},
	}); err != nil {
		log.Error().Err(err).Msg("werewolf: failed to announce voluntary leave")
	}
}
```

- [ ] **Step 13: Add the recap line for Hunter's revenge**

In `internal/discord/commands/werewolf_day.go`, `formatRoundBlock` currently ends its field-by-field rendering with the vote-breakdown block. Add a new conditional line right after the `SeerTargetLabel` block and before the `VoteBreakdown` block:

```go
	if r.SeerTargetLabel != "" {
		fmt.Fprintf(&b, "\n🔮 Tiên tri xem: %s → %s", r.SeerTargetLabel, r.SeerResult.label())
	}
	if r.HunterRevengeLabel != "" {
		fmt.Fprintf(&b, "\n🏹 Thợ săn kéo theo: %s", r.HunterRevengeLabel)
	}
	if len(r.VoteBreakdown) > 0 {
```

(This replaces the existing `if r.SeerTargetLabel != "" { ... }\n\tif len(r.VoteBreakdown) > 0 {` two-block transition with the three-block version above — the `SeerTargetLabel` block's own body is unchanged, only what comes after it changes.)

- [ ] **Step 14: Add a unit test for the new recap line**

Append to `internal/discord/commands/werewolf_test.go`, following the existing `TestFormatRoundBlock*` pattern:

```go
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
```

- [ ] **Step 15: Build and run the full test suite**

Run: `go build ./... && go vet ./internal/discord/commands/... && go test ./internal/discord/commands/... -v`
Expected: build succeeds, all tests `PASS`.

- [ ] **Step 16: Commit**

```bash
git add internal/discord/commands/werewolf.go internal/discord/commands/werewolf_night.go internal/discord/commands/werewolf_day.go internal/discord/commands/werewolf_test.go
git commit -m "werewolf: add Hunter — nightly target pick, revenge-kill on any death

Wired into the three death paths that already exist before Witch's
restructuring lands: day-lynch (resolveVote), AFK-removal batch
(checkAndHandleGameEnd), and voluntary leave (handleLeave).
applyHunterRevengeChain is deliberately not wired into resolveNight's
wolf-kill path yet -- Task 3 restructures that function anyway and
will call this same helper for both the wolf-kill and the new Witch-
poison death paths."
```

---

### Task 3: Witch — sequential night stage, heal/poison, and the wolf-kill/poison revenge wiring

**Files:**
- Modify: `internal/discord/commands/werewolf.go` (`werewolfGame` struct: `WitchHealUsed`, `WitchPoisonUsed`, `WitchSaveTarget`, `WitchPoisonTarget`, `WitchActedThisRound`; `roundLog`: `WitchSavedLabel`, `WitchPoisonedLabel`; `HandleComponent` dispatch)
- Modify: `internal/discord/commands/werewolf_night.go` (`runNightPhase`'s reset block; **rewrites `resolveNight`** into a two-stage flow; new `dmWitchPrompt`, `handleWitchHeal`, `handleWitchPoison`, `handleWitchSkip`, `waitForWitchAction`, `finalizeNightResolution`)
- Modify: `internal/discord/commands/werewolf_day.go` (`formatRoundBlock`)
- Test: `internal/discord/commands/werewolf_test.go`

**Interfaces:**
- Consumes: `applyHunterRevengeChain`, `aliveExcept`, `copyPlayer`/`copyPlayers` (Task 2 and earlier).
- Produces: nothing new consumed by a later task in this plan — this is the last coded task before manual verification.

- [ ] **Step 1: Add Witch's game-state fields and recap fields**

In `internal/discord/commands/werewolf.go`, replace:

```go
	HunterTarget                  string                         // persists across rounds until Hunter explicitly changes it
	HunterActedThisRound          bool                           // reset each round; tracks whether Hunter (re-)picked this round, for nightActionsComplete/AFK
	Votes                         map[string]string              // voterID -> targetID, reset each day
```

with:

```go
	HunterTarget                  string                         // persists across rounds until Hunter explicitly changes it
	HunterActedThisRound          bool                           // reset each round; tracks whether Hunter (re-)picked this round, for nightActionsComplete/AFK
	WitchHealUsed                 bool                           // lifetime flag — the heal potion is single-use per game
	WitchPoisonUsed               bool                           // lifetime flag — the poison potion is single-use per game
	WitchSaveTarget               string                         // this round's heal pick, "" if not saving; reset each round
	WitchPoisonTarget             string                         // this round's poison pick, "" if not poisoning; reset each round
	WitchActedThisRound           bool                           // reset each round; true once she confirms any choice (heal/poison/skip)
	Votes                         map[string]string              // voterID -> targetID, reset each day
```

Replace `roundLog`:

```go
	LynchedLabel      string
	HunterRevengeLabel string // who died from Hunter's revenge this round, "" if it didn't trigger
}
```

with:

```go
	LynchedLabel        string
	HunterRevengeLabel  string // who died from Hunter's revenge this round, "" if it didn't trigger
	WitchSavedLabel     string // who the Witch healed this round, "" if she didn't heal
	WitchPoisonedLabel  string // who the Witch poisoned this round, "" if she didn't poison
}
```

- [ ] **Step 2: Add Witch's three action custom IDs and their dispatch cases**

In `internal/discord/commands/werewolf_night.go`, replace:

```go
const (
	werewolfWolfTargetID    = "ww:wolftarget:"
	werewolfSeerCheckID     = "ww:seercheck:"
	werewolfDoctorProtectID = "ww:doctorprotect:"
	werewolfHunterTargetID  = "ww:huntertarget:"
```

with:

```go
const (
	werewolfWolfTargetID    = "ww:wolftarget:"
	werewolfSeerCheckID     = "ww:seercheck:"
	werewolfDoctorProtectID = "ww:doctorprotect:"
	werewolfHunterTargetID  = "ww:huntertarget:"
	werewolfWitchHealID     = "ww:witchheal:"
	werewolfWitchPoisonID   = "ww:witchpoison:"
	werewolfWitchSkipID     = "ww:witchskip:"

	// witchActionWindow is deliberately short — she's making one fast
	// decision after already knowing who was bitten, not coordinating with
	// anyone else the way the wolf pack does.
	witchActionWindow = 30 * time.Second
```

In `internal/discord/commands/werewolf.go`, `HandleComponent` currently ends:

```go
	case strings.HasPrefix(customID, werewolfHunterTargetID):
		cmd.handleHunterTarget(s, i, strings.TrimPrefix(customID, werewolfHunterTargetID))
	case strings.HasPrefix(customID, werewolfVoteID):
		cmd.handleVote(s, i, strings.TrimPrefix(customID, werewolfVoteID))
	}
}
```

Replace with:

```go
	case strings.HasPrefix(customID, werewolfHunterTargetID):
		cmd.handleHunterTarget(s, i, strings.TrimPrefix(customID, werewolfHunterTargetID))
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
```

- [ ] **Step 3: Reset Witch's per-round fields in `runNightPhase`**

In `internal/discord/commands/werewolf_night.go`, replace:

```go
		game.HunterActedThisRound = false
		game.History = append(game.History, roundLog{Round: game.Round})
```

with:

```go
		game.HunterActedThisRound = false
		game.WitchSaveTarget = ""
		game.WitchPoisonTarget = ""
		game.WitchActedThisRound = false
		game.History = append(game.History, roundLog{Round: game.Round})
```

(`WitchHealUsed`/`WitchPoisonUsed` are lifetime flags and are never reset here.)

- [ ] **Step 4: Build the Witch DM message content and select menu**

Add to `internal/discord/commands/werewolf_night.go`, right after `dmHunterPrompt`:

```go
// witchActionComponents builds the Witch's action message: an optional
// poison target-select row (omitted if she's already used it), and a
// button row with a heal option (omitted if there's no valid bite target,
// or she's already used it) plus an always-present skip button.
func witchActionComponents(channelID string, biteTargetID string, healUsed, poisonUsed bool, poisonTargets []*player) []discordgo.MessageComponent {
	var rows []discordgo.MessageComponent
	if !poisonUsed {
		rows = append(rows, targetSelectMenu(werewolfWitchPoisonID+channelID, "☠️ Đầu độc ai đó (bỏ qua nếu không muốn)", poisonTargets)[0])
	}
	buttons := []discordgo.MessageComponent{}
	if biteTargetID != "" && !healUsed {
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

func (cmd *WerewolfCommand) dmWitchPrompt(s *discordgo.Session, channelID string, witch *player, biteTargetID, biteTargetLabel string, healUsed, poisonUsed bool, poisonTargets []*player) {
	dm, err := s.UserChannelCreate(witch.UserID)
	if err != nil {
		log.Error().Err(err).Msg("werewolf: failed to open DM for witch")
		return
	}
	bittenText := "Không ai bị cắn đêm nay."
	if biteTargetLabel != "" {
		bittenText = fmt.Sprintf("Sói cắn: **%s** đêm nay.", biteTargetLabel)
	}
	if _, err := s.ChannelMessageSendComplex(dm.ID, &discordgo.MessageSend{
		Content:    fmt.Sprintf("🧙 %s Bạn muốn cứu, đầu độc ai đó, hay không làm gì?", bittenText),
		Components: witchActionComponents(channelID, biteTargetID, healUsed, poisonUsed, poisonTargets),
	}); err != nil {
		log.Error().Err(err).Msg("werewolf: failed to DM witch prompt")
	}
}
```

- [ ] **Step 5: Add the three Witch action handlers**

Add to `internal/discord/commands/werewolf_night.go`, right after `handleHunterTarget`:

```go
func (cmd *WerewolfCommand) handleWitchHeal(s *discordgo.Session, i *discordgo.InteractionCreate, channelID string) {
	clickerID := interactionUserID(i)

	cmd.mu.Lock()
	game, ok := cmd.games[channelID]
	if !ok || game.Phase != phaseNight {
		cmd.mu.Unlock()
		respondComponentEphemeral(s, i, "Đêm nay đã kết thúc rồi.")
		return
	}
	clicker, isWitch := game.Players[clickerID]
	if !isWitch || clicker.Role != roleWitch || !clicker.Alive {
		cmd.mu.Unlock()
		respondComponentEphemeral(s, i, "Chỉ Phù Thủy còn sống mới được cứu.")
		return
	}
	if game.WitchActedThisRound {
		cmd.mu.Unlock()
		respondComponentEphemeral(s, i, "Bạn đã hành động đêm nay rồi.")
		return
	}
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
}

func (cmd *WerewolfCommand) handleWitchPoison(s *discordgo.Session, i *discordgo.InteractionCreate, channelID string) {
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
	clicker, isWitch := game.Players[clickerID]
	if !isWitch || clicker.Role != roleWitch || !clicker.Alive {
		cmd.mu.Unlock()
		respondComponentEphemeral(s, i, "Chỉ Phù Thủy còn sống mới được đầu độc.")
		return
	}
	if game.WitchActedThisRound {
		cmd.mu.Unlock()
		respondComponentEphemeral(s, i, "Bạn đã hành động đêm nay rồi.")
		return
	}
	if game.WitchPoisonUsed {
		cmd.mu.Unlock()
		respondComponentEphemeral(s, i, "Bạn đã dùng bình độc rồi.")
		return
	}
	if target, exists := game.Players[targetID]; !exists || !target.Alive {
		cmd.mu.Unlock()
		respondComponentEphemeral(s, i, "Mục tiêu không còn hợp lệ.")
		return
	}
	game.WitchPoisonTarget = targetID
	game.WitchPoisonUsed = true
	game.WitchActedThisRound = true
	cmd.mu.Unlock()

	respondComponentEphemeralMentioned(s, i, fmt.Sprintf("☠️ Bạn đã đầu độc: <@%s>.", targetID))
}

func (cmd *WerewolfCommand) handleWitchSkip(s *discordgo.Session, i *discordgo.InteractionCreate, channelID string) {
	clickerID := interactionUserID(i)

	cmd.mu.Lock()
	game, ok := cmd.games[channelID]
	if !ok || game.Phase != phaseNight {
		cmd.mu.Unlock()
		respondComponentEphemeral(s, i, "Đêm nay đã kết thúc rồi.")
		return
	}
	clicker, isWitch := game.Players[clickerID]
	if !isWitch || clicker.Role != roleWitch || !clicker.Alive {
		cmd.mu.Unlock()
		respondComponentEphemeral(s, i, "Chỉ Phù Thủy còn sống mới được chọn.")
		return
	}
	if game.WitchActedThisRound {
		cmd.mu.Unlock()
		respondComponentEphemeral(s, i, "Bạn đã hành động đêm nay rồi.")
		return
	}
	game.WitchActedThisRound = true
	cmd.mu.Unlock()

	respondComponentEphemeral(s, i, "✅ Bạn chọn không làm gì đêm nay.")
}
```

Note `game.WitchSaveTarget` is set to the literal string `"heal"` (a sentinel, not a player ID) — the actual healed player is always whoever the wolves bit, resolved at finalize time from `resolvedTarget`, since the Witch's prompt doesn't let her pick a different heal target (the only valid heal target is the bite victim, per spec §4d — "💊 Cứu" heals the bitten target, it's not a free-choice heal).

- [ ] **Step 6: Add `waitForWitchAction`**

Add to `internal/discord/commands/werewolf_night.go`, right after `waitForNightActions`:

```go
// waitForWitchAction blocks until the Witch has acted (heal/poison/skip) or
// the deadline passes. No halfway reminder — this window is short and she
// already has all the information she needs the moment she's prompted.
func (cmd *WerewolfCommand) waitForWitchAction(s *discordgo.Session, channelID string, deadline time.Time, seq uint64) {
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
			done = game.WitchActedThisRound
		}()
		if !gameFound || done || time.Now().After(deadline) {
			return
		}
		time.Sleep(2 * time.Second)
	}
}
```

- [ ] **Step 7: Rewrite `resolveNight` into a two-stage flow**

Replace the entire current `resolveNight` function:

```go
// resolveNight applies the kill/save outcome, records AFK strikes for
// anyone who didn't act, deletes this round's wolf thread, posts the
// result, then hands off to the win-check and (if the game continues) the
// day-discuss phase.
func (cmd *WerewolfCommand) resolveNight(s *discordgo.Session, channelID string, seq uint64) {
	var (
		gameFound     bool
		threadID      string
		revealOnDeath bool
		killedPlayer  *player
		saved         bool
		wolfVoteTied  bool
		summary       string
		aliveThreadID string
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

		aliveWolves := alivePlayersOfRole(game, roleWerewolf)
		validVotes := validWolfVotes(game.WolfVotes, aliveWolves, game.Players)
		wolfTargets, tallyOK := tallyWolfVotes(validVotes, 1)
		wolfVoteTied = !tallyOK && len(validVotes) > 0
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

		for _, w := range aliveWolves {
			if _, voted := game.WolfVotes[w.UserID]; voted {
				game.AFKStrikes[w.UserID] = 0
			} else {
				game.AFKStrikes[w.UserID]++
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
		for _, p := range alivePlayersOfRole(game, roleHunter) {
			if game.HunterActedThisRound {
				game.AFKStrikes[p.UserID] = 0
			} else {
				game.AFKStrikes[p.UserID]++
			}
		}

		game.DoctorLastPick = game.DoctorProtect
		threadID = game.WolfThreadID
		game.WolfThreadID = ""
		game.WolfTargetMessageID = ""
		revealOnDeath = game.RevealOnDeath
		summary = aliveDeadSummary(game)
		if killedID != "" {
			killedPlayer = game.Players[killedID]
		}

		if n := len(game.History); n > 0 {
			entry := &game.History[n-1]
			entry.WolfTargetLabel = wolfTargetLabel
			entry.WolfTargetSaved = saved
			entry.WolfVoteTied = wolfVoteTied
			entry.DoctorTargetLabel = doctorTargetLabel
			if game.SeerTarget != "" {
				if target, exists := game.Players[game.SeerTarget]; exists {
					entry.SeerTargetLabel = target.Label
				}
				entry.SeerResult = game.SeerResult
			}
		}
	}()
	if !gameFound {
		return
	}
	if killedPlayer != nil {
		removeFromAliveThread(s, aliveThreadID, killedPlayer.UserID)
	}

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
	case wolfVoteTied:
		resultMsg = "🐺 Bầy Sói không thống nhất được mục tiêu — không ai chết trong đêm qua."
	default:
		resultMsg = "😴 Không ai chết trong đêm qua."
	}

	if _, err := s.ChannelMessageSendComplex(channelID, &discordgo.MessageSend{
		Content:         resultMsg + "\n\n" + summary,
		AllowedMentions: &discordgo.MessageAllowedMentions{},
	}); err != nil {
		log.Error().Err(err).Msg("werewolf: failed to post night result")
	}

	if cmd.checkAndHandleGameEnd(s, channelID, seq) {
		return
	}
	cmd.runDayDiscuss(s, channelID, seq)
}
```

with this two-stage version:

```go
// resolveNight resolves the wolf vote and the AFK strikes for wolves/
// seer/doctor/hunter WITHOUT applying the wolf-bite death yet, deletes the
// wolf thread, then — if a Witch is alive — DMs her the bite outcome and
// waits for her decision before finalizing anything. finalizeNightResolution
// does the actual death application, Witch poison, Hunter revenge chain,
// recap, and result message, then hands off exactly like the old single-
// stage resolveNight used to.
func (cmd *WerewolfCommand) resolveNight(s *discordgo.Session, channelID string, seq uint64) {
	var (
		gameFound         bool
		threadID          string
		resolvedTarget    string
		wolfTargetLabel   string
		wolfVoteTied      bool
		doctorSaved       bool
		aliveThreadID     string
		witch             *player
		witchPoisonTargets []*player
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

		aliveWolves := alivePlayersOfRole(game, roleWerewolf)
		validVotes := validWolfVotes(game.WolfVotes, aliveWolves, game.Players)
		wolfTargets, tallyOK := tallyWolfVotes(validVotes, 1)
		wolfVoteTied = !tallyOK && len(validVotes) > 0
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
		doctorSaved = resolvedTarget != "" && resolvedTarget == game.DoctorProtect

		for _, w := range aliveWolves {
			if _, voted := game.WolfVotes[w.UserID]; voted {
				game.AFKStrikes[w.UserID] = 0
			} else {
				game.AFKStrikes[w.UserID]++
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
		for _, p := range alivePlayersOfRole(game, roleHunter) {
			if game.HunterActedThisRound {
				game.AFKStrikes[p.UserID] = 0
			} else {
				game.AFKStrikes[p.UserID]++
			}
		}

		game.DoctorLastPick = game.DoctorProtect
		threadID = game.WolfThreadID
		game.WolfThreadID = ""
		game.WolfTargetMessageID = ""

		if witches := alivePlayersOfRole(game, roleWitch); len(witches) > 0 {
			witch = copyPlayer(witches[0])
			witchPoisonTargets = copyPlayers(aliveExcept(game, witch.UserID))
		}

		if n := len(game.History); n > 0 {
			entry := &game.History[n-1]
			entry.WolfTargetLabel = wolfTargetLabel
			entry.WolfVoteTied = wolfVoteTied
			entry.DoctorTargetLabel = doctorTargetLabel
			if game.SeerTarget != "" {
				if target, exists := game.Players[game.SeerTarget]; exists {
					entry.SeerTargetLabel = target.Label
				}
				entry.SeerResult = game.SeerResult
			}
		}
	}()
	if !gameFound {
		return
	}

	if threadID != "" {
		if _, err := s.ChannelDelete(threadID); err != nil {
			log.Error().Err(err).Msg("werewolf: failed to delete wolf thread")
		}
	}

	if witch != nil {
		var healUsed, poisonUsed bool
		func() {
			cmd.mu.Lock()
			defer cmd.mu.Unlock()
			if g := cmd.gameIfCurrent(channelID, seq); g != nil {
				healUsed = g.WitchHealUsed
				poisonUsed = g.WitchPoisonUsed
			}
		}()
		cmd.dmWitchPrompt(s, channelID, witch, resolvedTarget, wolfTargetLabel, healUsed, poisonUsed, witchPoisonTargets)
		deadline := time.Now().Add(witchActionWindow)
		cmd.waitForWitchAction(s, channelID, deadline, seq)
	}

	cmd.finalizeNightResolution(s, channelID, seq, resolvedTarget, doctorSaved, wolfVoteTied, aliveThreadID)
}

// finalizeNightResolution applies the wolf-bite death (accounting for
// Doctor protection and Witch's heal), applies Witch's poison if she chose
// one, chains Hunter's revenge off of either death, builds the recap
// fields and the public result message, then hands off to the win-check
// and day-discuss exactly like the old single-stage resolveNight used to.
func (cmd *WerewolfCommand) finalizeNightResolution(s *discordgo.Session, channelID string, seq uint64, resolvedTarget string, doctorSaved, wolfVoteTied bool, aliveThreadID string) {
	var (
		gameFound      bool
		revealOnDeath  bool
		killedPlayer   *player
		poisonedPlayer *player
		witchHealed    bool
		hunterRevenged []*player
		summary        string
	)
	func() {
		cmd.mu.Lock()
		defer cmd.mu.Unlock()
		game := cmd.gameIfCurrent(channelID, seq)
		if game == nil {
			return
		}
		gameFound = true

		witchHealed = resolvedTarget != "" && game.WitchSaveTarget == "heal"
		saved := doctorSaved || witchHealed

		var killedID string
		if resolvedTarget != "" && !saved {
			if target, exists := game.Players[resolvedTarget]; exists && target.Alive {
				target.Alive = false
				killedID = resolvedTarget
			}
		}

		var poisonedID string
		if game.WitchPoisonTarget != "" {
			if target, exists := game.Players[game.WitchPoisonTarget]; exists && target.Alive {
				target.Alive = false
				poisonedID = game.WitchPoisonTarget
			}
		}

		if killedID != "" {
			hunterRevenged = append(hunterRevenged, applyHunterRevengeChain(game, killedID)...)
		}
		if poisonedID != "" {
			hunterRevenged = append(hunterRevenged, applyHunterRevengeChain(game, poisonedID)...)
		}

		revealOnDeath = game.RevealOnDeath
		summary = aliveDeadSummary(game)
		if killedID != "" {
			killedPlayer = game.Players[killedID]
		}
		if poisonedID != "" {
			poisonedPlayer = game.Players[poisonedID]
		}

		if n := len(game.History); n > 0 {
			entry := &game.History[n-1]
			entry.WolfTargetSaved = doctorSaved
			if witchHealed {
				if target, exists := game.Players[resolvedTarget]; exists {
					entry.WitchSavedLabel = target.Label
				}
			}
			if poisonedPlayer != nil {
				entry.WitchPoisonedLabel = poisonedPlayer.Label
			}
			if len(hunterRevenged) > 0 {
				entry.HunterRevengeLabel = hunterRevenged[0].Label
			}
		}
	}()
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
	case doctorSaved:
		resultMsg = "🛡️ Bác Sĩ đã cứu sống mục tiêu đêm qua — không ai chết!"
	case witchHealed:
		resultMsg = "🧙 Phù Thủy đã cứu sống mục tiêu đêm qua — không ai chết!"
	case wolfVoteTied:
		resultMsg = "🐺 Bầy Sói không thống nhất được mục tiêu — không ai chết trong đêm qua."
	default:
		resultMsg = "😴 Không ai chết trong đêm qua."
	}
	if poisonedPlayer != nil {
		if revealOnDeath {
			resultMsg += fmt.Sprintf("\n☠️ **%s** trúng độc của Phù Thủy. Vai trò: %s.", poisonedPlayer.Label, poisonedPlayer.Role.label())
		} else {
			resultMsg += fmt.Sprintf("\n☠️ **%s** trúng độc của Phù Thủy.", poisonedPlayer.Label)
		}
	}
	for _, p := range hunterRevenged {
		resultMsg += fmt.Sprintf("\n🏹 **%s** cũng chết theo vì Thợ Săn.", p.Label)
	}

	if _, err := s.ChannelMessageSendComplex(channelID, &discordgo.MessageSend{
		Content:         resultMsg + "\n\n" + summary,
		AllowedMentions: &discordgo.MessageAllowedMentions{},
	}); err != nil {
		log.Error().Err(err).Msg("werewolf: failed to post night result")
	}

	if cmd.checkAndHandleGameEnd(s, channelID, seq) {
		return
	}
	cmd.runDayDiscuss(s, channelID, seq)
}
```

- [ ] **Step 8: Add the recap lines for Witch's heal/poison**

In `internal/discord/commands/werewolf_day.go`, `formatRoundBlock` currently renders the wolf-target line as:

```go
	if r.WolfTargetLabel != "" {
		if r.WolfTargetSaved {
			fmt.Fprintf(&b, "\n🐺 Sói cắn: %s (được bác sĩ cứu)", r.WolfTargetLabel)
		} else {
			fmt.Fprintf(&b, "\n🐺 Sói cắn: %s", r.WolfTargetLabel)
		}
	} else if r.WolfVoteTied {
		b.WriteString("\n🐺 Sói không thống nhất được mục tiêu.")
	}
```

Replace with:

```go
	if r.WolfTargetLabel != "" {
		switch {
		case r.WolfTargetSaved:
			fmt.Fprintf(&b, "\n🐺 Sói cắn: %s (được bác sĩ cứu)", r.WolfTargetLabel)
		case r.WitchSavedLabel != "":
			fmt.Fprintf(&b, "\n🐺 Sói cắn: %s (được phù thủy cứu)", r.WolfTargetLabel)
		default:
			fmt.Fprintf(&b, "\n🐺 Sói cắn: %s", r.WolfTargetLabel)
		}
	} else if r.WolfVoteTied {
		b.WriteString("\n🐺 Sói không thống nhất được mục tiêu.")
	}
	if r.WitchPoisonedLabel != "" {
		fmt.Fprintf(&b, "\n☠️ Phù thủy đầu độc: %s", r.WitchPoisonedLabel)
	}
```

- [ ] **Step 9: Add unit tests for the new recap lines**

Append to `internal/discord/commands/werewolf_test.go`:

```go
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
```

- [ ] **Step 10: Build and run the full test suite**

Run: `go build ./... && go vet ./internal/discord/commands/... && go test ./internal/discord/commands/... -v`
Expected: build succeeds, all tests `PASS`. This task's core restructuring (`resolveNight`/`finalizeNightResolution`) is Discord-coupled and not independently unit-tested beyond the pure `formatRoundBlock` additions, consistent with this project's testing philosophy — verified manually in Task 4.

- [ ] **Step 11: Commit**

```bash
git add internal/discord/commands/werewolf.go internal/discord/commands/werewolf_night.go internal/discord/commands/werewolf_day.go internal/discord/commands/werewolf_test.go
git commit -m "werewolf: add Witch — sequential night stage, heal/poison

resolveNight now resolves the wolf vote and deletes the wolf thread,
then (if a Witch is alive) DMs her the bite outcome and waits for her
decision via the new finalizeNightResolution, before applying any
death. Poison bypasses Doctor protection; heal only ever targets the
wolf-bite victim (not a free choice). Both potions single-use per
game. Hunter's revenge chain (Task 2) now also fires off the wolf-kill
and poison death paths, not just lynch/AFK/leave."
```

---

### Task 4: Public per-role progress notifications (spec §4i — catches up Wolf pack/Seer/Guard from Plan A, plus this plan's Hunter/Witch)

Spec §4i requires a short, identity-free "X đã quyết định xong" progress line posted to the main channel the moment each night-role finishes deciding, so players can watch night progress without waiting blindly. This was never implemented in Plan A. This task adds it for every role that has a mechanic today (Wolf pack, Seer, Guard, Hunter, Witch) — Disruptor and Cupid get theirs when Plan D builds them.

**Files:**
- Modify: `internal/discord/commands/werewolf_night.go` (`handleWolfTarget`, `handleSeerCheck`, `handleDoctorProtect`, `handleHunterTarget`, `handleWitchHeal`, `handleWitchPoison`, `handleWitchSkip`)
- Modify: `internal/discord/commands/werewolf.go` (new `announceRoleDecided` helper)

**Interfaces:**
- Produces: `announceRoleDecided(s *discordgo.Session, channelID, roleLabel string)` (in `werewolf.go`) — a tiny, reusable helper; no other task depends on it.

- [ ] **Step 1: Add the shared announcement helper**

In `internal/discord/commands/werewolf.go`, add right after `aliveDeadSummary`:

```go
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
```

- [ ] **Step 2: Wolf pack — announce once, the moment every alive wolf has voted**

In `internal/discord/commands/werewolf_night.go`, `handleWolfTarget` currently has:

```go
	game.WolfVotes[clickerID] = targetID
	seq := game.Seq
	cmd.mu.Unlock()

	respondComponentEphemeralMentioned(s, i, fmt.Sprintf("✅ Bạn đã chọn: <@%s>. Phiếu đã khóa, không thể đổi.", targetID))
```

Replace with:

```go
	game.WolfVotes[clickerID] = targetID
	allVoted := true
	for _, w := range alivePlayersOfRole(game, roleWerewolf) {
		if _, voted := game.WolfVotes[w.UserID]; !voted {
			allVoted = false
			break
		}
	}
	seq := game.Seq
	cmd.mu.Unlock()

	respondComponentEphemeralMentioned(s, i, fmt.Sprintf("✅ Bạn đã chọn: <@%s>. Phiếu đã khóa, không thể đổi.", targetID))
	if allVoted {
		announceRoleDecided(s, channelID, "🐺 Bầy Sói")
	}
```

- [ ] **Step 3: Seer**

In `internal/discord/commands/werewolf_night.go`, `handleSeerCheck` currently ends:

```go
	respondComponentEphemeralMentioned(s, i, fmt.Sprintf("🔮 <@%s> %s.", targetID, verdict))
}
```

Replace with:

```go
	respondComponentEphemeralMentioned(s, i, fmt.Sprintf("🔮 <@%s> %s.", targetID, verdict))
	announceRoleDecided(s, channelID, "🔮 Tiên Tri")
}
```

- [ ] **Step 4: Guard/Doctor**

In `internal/discord/commands/werewolf_night.go`, `handleDoctorProtect` currently ends:

```go
	respondComponentEphemeralMentioned(s, i, fmt.Sprintf("✅ Đã chọn bảo vệ: <@%s>.", targetID))
}
```

Replace with:

```go
	respondComponentEphemeralMentioned(s, i, fmt.Sprintf("✅ Đã chọn bảo vệ: <@%s>.", targetID))
	announceRoleDecided(s, channelID, "🩺 Bác Sĩ")
}
```

- [ ] **Step 5: Hunter — announce only on the first successful pick this round**

In `internal/discord/commands/werewolf_night.go`, `handleHunterTarget` currently has:

```go
	game.HunterTarget = targetID
	game.HunterActedThisRound = true
	cmd.mu.Unlock()

	respondComponentEphemeralMentioned(s, i, fmt.Sprintf("✅ Đã chỉ định: <@%s>.", targetID))
}
```

Replace with:

```go
	firstActionThisRound := !game.HunterActedThisRound
	game.HunterTarget = targetID
	game.HunterActedThisRound = true
	cmd.mu.Unlock()

	respondComponentEphemeralMentioned(s, i, fmt.Sprintf("✅ Đã chỉ định: <@%s>.", targetID))
	if firstActionThisRound {
		announceRoleDecided(s, channelID, "🏹 Thợ Săn")
	}
}
```

- [ ] **Step 6: Witch — announce from whichever of the three actions she takes**

In `internal/discord/commands/werewolf_night.go`, `handleWitchHeal` currently ends:

```go
	cmd.mu.Unlock()

	respondComponentEphemeral(s, i, "✅ Bạn đã chọn cứu người bị cắn đêm nay.")
}
```

Replace with:

```go
	cmd.mu.Unlock()

	respondComponentEphemeral(s, i, "✅ Bạn đã chọn cứu người bị cắn đêm nay.")
	announceRoleDecided(s, channelID, "🧙 Phù Thủy")
}
```

`handleWitchPoison` currently ends:

```go
	cmd.mu.Unlock()

	respondComponentEphemeralMentioned(s, i, fmt.Sprintf("☠️ Bạn đã đầu độc: <@%s>.", targetID))
}
```

Replace with:

```go
	cmd.mu.Unlock()

	respondComponentEphemeralMentioned(s, i, fmt.Sprintf("☠️ Bạn đã đầu độc: <@%s>.", targetID))
	announceRoleDecided(s, channelID, "🧙 Phù Thủy")
}
```

`handleWitchSkip` currently ends:

```go
	cmd.mu.Unlock()

	respondComponentEphemeral(s, i, "✅ Bạn chọn không làm gì đêm nay.")
}
```

Replace with:

```go
	cmd.mu.Unlock()

	respondComponentEphemeral(s, i, "✅ Bạn chọn không làm gì đêm nay.")
	announceRoleDecided(s, channelID, "🧙 Phù Thủy")
}
```

- [ ] **Step 7: Build and run the full test suite**

Run: `go build ./... && go vet ./internal/discord/commands/... && go test ./internal/discord/commands/... -v`
Expected: build succeeds, all tests `PASS`. This task is purely additive announcements on existing, already-tested control flow — no new pure logic, so no new unit tests, consistent with this project's testing philosophy (verified manually in Task 5).

- [ ] **Step 8: Commit**

```bash
git add internal/discord/commands/werewolf.go internal/discord/commands/werewolf_night.go
git commit -m "werewolf: add public per-role progress notifications (spec §4i)

Catches up Wolf pack/Seer/Guard, which Plan A shipped without this --
plus this plan's own Hunter/Witch. Disruptor/Cupid get theirs when
Plan D builds them. Never names the acting player or reveals their
choice, just that role's action is complete for the round."
```

---

### Task 5: Manual end-to-end verification (no code — hand this to the user)

This task has no code changes and requires a real Discord server with the bot invited, which a subagent cannot exercise. Flag this task to the user rather than dispatching it.

Checklist to hand over:

- [ ] Run an 11+ player game. Confirm the role-assigned announcement's breakdown includes "🧙 Phù Thủy: 1 · 🏹 Thợ Săn: 1".
- [ ] Confirm Hunter gets a nightly DM target-select, can change their pick on a later night while still alive, and that going quiet (never re-picking) does NOT accrue AFK strikes or block night completion — per spec §4f, Hunter is exempt from that gating since re-picking is optional.
- [ ] Confirm Witch's DM only arrives *after* the night-result would otherwise be known (i.e. after the wolf vote resolves and the wolf thread is deleted) — she should see who was bitten (or "Không ai bị cắn"), with a poison target-select menu (only present if she hasn't used it) and a heal button (only present if there's a bite target and she hasn't used it) plus a skip button.
- [ ] Confirm heal actually prevents the wolf-bite death (message says "🧙 Phù Thủy đã cứu sống mục tiêu..."), and that it's single-use — try to heal again on a later night and confirm the button is gone.
- [ ] Confirm poison kills the chosen target even if Doctor protected them that same night (poison bypasses protection), and that it's single-use similarly.
- [ ] Deliberately arrange for the Hunter to die (wolf-kill, Witch poison, day-lynch, and — if you can force it — AFK-removal or voluntary `/werewolf leave`) with a locked-in `HunterTarget`, and confirm the target dies too each time, with a "🏹 ... cũng chết theo vì Thợ Săn." message and a matching recap line.
- [ ] Run a full game to completion and check the final recap: every round with a Witch save/poison or Hunter revenge should show the corresponding new lines, and rounds without them should not mention Witch/Hunter at all.
- [ ] Confirm the main channel shows "✅ [role] đã quyết định xong." for Wolf pack (once all wolves have voted), Seer, Guard, Hunter (first pick only, not on later re-picks), and Witch (whichever of heal/poison/skip she used) — each posted exactly once per round, never naming who acted or what they chose.
