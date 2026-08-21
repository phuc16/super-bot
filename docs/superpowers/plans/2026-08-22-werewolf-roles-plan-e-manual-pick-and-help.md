# Werewolf Extended Roles — Plan E: Manual Role-Pick UI + `/help werewolf` Guide Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add `/werewolf start custom-roles:true`'s manual role-count editor (spec §3b) and update `/help werewolf` with a per-role interaction guide (spec §6). This is the fifth and final plan implementing `docs/superpowers/specs/2026-08-21-werewolf-roles-design.md` — it's also the plan that makes every role Plans C and D shipped (Wolf Cub, Cursed Wolf, Traitor, Cupid) reachable in a real game for the first time, since none of them are ever dealt by the fixed table.

**Architecture:** A new game phase (`phaseRoleEdit`) sits between the lobby closing and preflight starting, active only when `custom-roles:true`. The starter picks counts via two ephemeral, self-contained editor messages (wolf-side, villager-side), each independently re-rendering itself in place on every click — deliberately *not* kept in cross-message sync (documented below), since confirm-time validation is authoritative regardless of what's currently on screen. On confirm (or on a timeout fallback to the fixed table), the resulting counts feed `dealRoles` directly instead of `rolesForPlayerCount`. A new file, `werewolf_rolepick.go`, houses this whole subsystem — it's a genuinely separate concern from `werewolf.go`'s lobby/preflight orchestration and `werewolf_night.go`'s in-game action handlers, and keeping it apart avoids growing either of those already-large files further.

**Tech Stack:** Go 1.24 (builtin `min`/`max` used), discordgo v0.29.0, existing `sync.Mutex`-guarded `map[string]*werewolfGame` pattern.

## Global Constraints

- This is Plan E of five, the last one. Plans A-D are already complete and merged into `feature/werewolf`. Do not re-implement anything from them.
- **Decided design call (user-confirmed):** Seer, Doctor, Witch, Hunter, Disruptor, Cupid, and Traitor are capped at a maximum of **1** each in the manual editor. The engine's `runNightPhase` (and every one of those roles' handlers) only ever acts on the *first* alive player of that role — `alivePlayersOfRole(game, roleX)[0]` — for every one of these seven; a second one dealt would silently get no DM at all. This has been latent and unreachable since the fixed table never deals more than 1 of any of them; the manual editor is the first place it would become reachable, so this plan caps it at the UI level rather than attempting the much larger rework (looping every one of these seven roles' night-action wiring over N actors instead of 1) that would be needed to lift the cap. Werewolf, Wolf Cub, Cursed Wolf, and Villager are **not** capped this way — Werewolf/Wolf Cub already share the plural wolf-vote mechanism (Plan C), Cursed Wolf has no action of its own to duplicate, and Villager has no mechanic at all.
- Every `discordgo.MessageSend`/`InteractionResponseData`/`MessageEdit`/`WebhookParams` whose `Content` contains a `<@id>` mention must set `AllowedMentions` explicitly. None of this plan's new messages embed a mention (they're role-count UI and documentation), but check this on every new send site anyway.
- No DB/migration changes — werewolf remains fully in-memory. The manual-pick editor's transient state (`game.PendingRoleCounts`) lives on `werewolfGame` and is freed the same way every other per-game field is (`delete(cmd.games, channelID)` on every terminal path).
- After editing any file, run `gofmt -l <file>` on every file you touched; if it lists the file, run `gofmt -w <file>` before your final build/test step.
- Every read/write of `*werewolfGame`/`*player` fields inside the interactive engine must happen strictly inside the existing `cmd.mu.Lock()`/`defer cmd.mu.Unlock()` critical sections.
- Any code reading `*player`/`*werewolfGame` state after a `cmd.mu.Unlock()` must read it from a snapshot taken before the unlock, never a live pointer.
- **Abandoned-editor fallback (spec leaves this to the plan):** if the starter doesn't confirm within a timeout, the game falls back to the fixed table (`rolesForPlayerCount`) and proceeds automatically — it does not abort the game. The handoff from "still editing" to "past editing" (confirm vs. timeout) must happen as a single atomic check-and-transition under `cmd.mu` (read `game.Phase == phaseRoleEdit` and immediately write `game.Phase = phasePreflight` in the same locked section) so exactly one of {a confirm click, the timeout goroutine} ever wins and starts `startPreflight` — never both.
- Cross-task carry-forward items from Plans C/D that this plan resolves: (1) the Traitor roster DM only ever addresses a single Traitor (`traitors[0]`) — covered by this plan's 1-cap decision above, so it's a non-issue rather than something to fix; (2) `startPreflight`'s role-count announcement re-derives from `rolesForPlayerCount(len(joinOrder))` instead of counting the actually-dealt roster — Task 4 fixes this now that it matters (a custom-roles game's announcement would otherwise show the *wrong* counts, not just stale ones).

---

### Task 1: `custom-roles` option, `phaseRoleEdit`, and the game-state fields

**Files:**
- Modify: `internal/discord/commands/werewolf.go` (`Definition`, `handleStart`, `gamePhase`, `werewolfGame`, `handleLeave`)
- Test: `internal/discord/commands/werewolf_test.go`

**Interfaces:**
- Produces: `game.CustomRoles bool`, `phaseRoleEdit gamePhase`, `game.PendingRoleCounts map[role]int` — read by Task 3's handlers.

- [ ] **Step 1: Add the command option**

In `internal/discord/commands/werewolf.go`, `Definition`'s `"start"` subcommand `Options`, add one more option after `reveal-on-death`:

```go
					{
						Type:        discordgo.ApplicationCommandOptionBoolean,
						Name:        "custom-roles",
						Description: "Tự chọn số lượng từng vai trò thay vì dùng bảng mặc định (mặc định: không)",
						Required:    false,
					},
```

- [ ] **Step 2: Read it in `handleStart` and store it on the game**

In `handleStart`, next to the existing `revealOnDeath` option read:

```go
	customRoles := false
	if v, ok := options["custom-roles"].(bool); ok {
		customRoles = v
	}
```

Add `CustomRoles: customRoles,` to the `game := &werewolfGame{...}` literal.

- [ ] **Step 3: Add the phase and game-state fields**

In `gamePhase`'s `const` block, add `phaseRoleEdit` right after `phasePreflight`:

```go
	phasePreflight
	// phaseRoleEdit is entered instead of phasePreflight when the starter
	// chose custom-roles:true — the starter picks role counts via the
	// ephemeral editor (werewolf_rolepick.go) before the game proceeds to
	// the normal preflight (role assignment + DM) flow.
	phaseRoleEdit
	phaseNight
```

In `werewolfGame`, add (next to `StarterID` or any other lobby-scoped field — placement doesn't matter, this is a struct-field addition):

```go
	CustomRoles       bool             // set at /werewolf start; true routes "Bắt đầu" into phaseRoleEdit instead of straight to phasePreflight
	PendingRoleCounts map[role]int     // the manual-pick editor's live counts, nil until phaseRoleEdit starts; read once at confirm/timeout, then no longer meaningful
```

- [ ] **Step 4: Reject leaving during role-edit, same as preflight**

In `handleLeave`, find:

```go
	if game.Phase == phasePreflight {
		cmd.mu.Unlock()
		respondError(s, i, "Bàn đang được thiết lập, vui lòng chờ ván bắt đầu.")
		return
	}
```

Change to:

```go
	if game.Phase == phasePreflight || game.Phase == phaseRoleEdit {
		cmd.mu.Unlock()
		respondError(s, i, "Bàn đang được thiết lập, vui lòng chờ ván bắt đầu.")
		return
	}
```

- [ ] **Step 5: Write a smoke test for the option parsing**

Add to `internal/discord/commands/werewolf_test.go`:

```go
func TestWerewolfDefinitionHasCustomRolesOption(t *testing.T) {
	cmd := NewWerewolfCommand()
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
```

- [ ] **Step 6: Run tests, verify, and commit**

Run: `go build ./... && go vet ./internal/discord/commands/... && go test ./internal/discord/commands/...`

Expected: all pass. `phaseRoleEdit` is a new enum value nothing yet transitions into, so no existing test's behavior changes.

```bash
gofmt -l internal/discord/commands/werewolf.go internal/discord/commands/werewolf_test.go
git add internal/discord/commands/werewolf.go internal/discord/commands/werewolf_test.go
git commit -m "werewolf: add custom-roles option, phaseRoleEdit, and pending-counts state"
```

---

### Task 2: Pure logic for the role-count editor

**Files:**
- Create: `internal/discord/commands/werewolf_rolepick.go`
- Test: `internal/discord/commands/werewolf_test.go`

**Interfaces:**
- Consumes: `rolePoints`, `role.label()` (Plan A), `roleRatio` (Plans A-D).
- Produces: `roleCountWolfSideRoles`/`roleCountVillagerSideRoles []role`, `roleCountMax(r role, playerCount int) int`, `balanceScore(counts map[role]int) int`, `roleCountConfirmValid(counts map[role]int, playerCount int) bool`, `roleRatioFromCounts(counts map[role]int) roleRatio`, `roleCountEditorContent(side string, counts map[role]int, playerCount int) string` — all pure, all consumed by Task 3's Discord-facing handlers.

- [ ] **Step 1: Write the failing tests**

Create `internal/discord/commands/werewolf_test.go` additions (append to the existing file):

```go
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
		{"cursed wolf counts as wolf-side", map[role]int{roleCursedWolf: 1, roleVillager: 6}, 7, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := roleCountConfirmValid(c.counts, c.playerCount); got != c.want {
				t.Errorf("roleCountConfirmValid(%v, %d) = %v, want %v", c.counts, c.playerCount, got, c.want)
			}
		})
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
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/discord/commands/... -run 'TestRoleCountMax|TestBalanceScore|TestRoleCountConfirmValid|TestRoleRatioFromCounts|TestRoleCountEditorContent' -v`

Expected: FAIL (nothing defined yet).

- [ ] **Step 3: Write the implementation**

Create `internal/discord/commands/werewolf_rolepick.go`:

```go
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
```

- [ ] **Step 4: Run tests, verify, and commit**

Run: `go build ./... && go vet ./internal/discord/commands/... && go test ./internal/discord/commands/...`

Expected: all pass.

```bash
gofmt -l internal/discord/commands/werewolf_rolepick.go internal/discord/commands/werewolf_test.go
git add internal/discord/commands/werewolf_rolepick.go internal/discord/commands/werewolf_test.go
git commit -m "werewolf: add pure role-count-editor logic (balance score, validation, ratio conversion)"
```

---

### Task 3: Discord UI — the editor's buttons, handlers, and timeout fallback

**Files:**
- Modify: `internal/discord/commands/werewolf_rolepick.go` (component builders, handlers)
- Modify: `internal/discord/commands/werewolf.go` (`handleStartGame`, `HandleComponent`)
- Test: `internal/discord/commands/werewolf_test.go`

**Interfaces:**
- Consumes: everything from Task 2, `respondComponentEphemeral`, `gameIfCurrent`, `phaseLobby`/`phaseRoleEdit`/`phasePreflight`.
- Produces: `werewolfRoleCountID`/`werewolfRoleCountConfirmID` custom-ID prefixes, `roleCountButtonRows`, `(cmd *WerewolfCommand) sendRoleCountEditor`, `handleRoleCountButton`, `handleRoleCountConfirm`, `waitForRoleCountConfirm`.

The wolf-side message has 4 roles; the villager-side has 7. Both pack 2 roles (4 buttons: ➖➕➖➕) per `ActionsRow`, plus one final row for the "✅ Xác nhận" button — 3 rows for wolf-side, 5 for villager-side (7 roles → rows of 2,2,2,1 + confirm = 5), both within Discord's 5-rows-per-message limit.

- [ ] **Step 1: Add the customID constants and component builder**

In `internal/discord/commands/werewolf_rolepick.go`, add:

```go
const (
	werewolfRoleCountID        = "ww:rolecount:"
	werewolfRoleCountConfirmID = "ww:rolecountconfirm:"

	// roleEditTimeout is how long the starter has to confirm the manual
	// role-count editor before the game falls back to the fixed table
	// automatically (spec §3b leaves this fallback's exact shape to the
	// plan — see this plan's Global Constraints).
	roleEditTimeout = 5 * time.Minute
)

// roleCountButtonRows builds one editor message's component rows: the
// given roles' -/+ buttons, two roles per row, plus a trailing confirm
// row. customID encodes the sign ("inc"/"dec"), the role's int value, and
// the channel ID, colon-separated after the werewolfRoleCountID prefix —
// this is this codebase's first customID needing more than one encoded
// field, so it's parsed with strings.SplitN rather than the simple
// TrimPrefix-only pattern every other werewolf customID uses.
func roleCountButtonRows(channelID string, roles []role, counts map[role]int, playerCount int) []discordgo.MessageComponent {
	var rows []discordgo.MessageComponent
	for start := 0; start < len(roles); start += 2 {
		end := min(start+2, len(roles))
		var buttons []discordgo.MessageComponent
		for _, r := range roles[start:end] {
			current := counts[r]
			buttons = append(buttons,
				discordgo.Button{
					Label:    "➖ " + r.label(),
					Style:    discordgo.SecondaryButton,
					CustomID: fmt.Sprintf("%sdec:%d:%s", werewolfRoleCountID, int(r), channelID),
					Disabled: current <= 0,
				},
				discordgo.Button{
					Label:    "➕ " + r.label(),
					Style:    discordgo.SecondaryButton,
					CustomID: fmt.Sprintf("%sinc:%d:%s", werewolfRoleCountID, int(r), channelID),
					Disabled: current >= roleCountMax(r, playerCount),
				},
			)
		}
		rows = append(rows, discordgo.ActionsRow{Components: buttons})
	}
	rows = append(rows, discordgo.ActionsRow{
		Components: []discordgo.MessageComponent{
			discordgo.Button{
				Label:    "✅ Xác nhận",
				Style:    discordgo.SuccessButton,
				CustomID: werewolfRoleCountConfirmID + channelID,
			},
		},
	})
	return rows
}
```

Add `"time"` and `"github.com/bwmarrin/discordgo"` and `"github.com/rs/zerolog/log"` to this file's imports (alongside the `"fmt"`/`"strings"` from Task 2) — needed by this step and the ones below.

- [ ] **Step 2: Wire `handleStartGame` to branch into the editor**

In `internal/discord/commands/werewolf.go`, `handleStartGame` currently ends with:

```go
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
```

Replace with:

```go
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
			Components: []discordgo.MessageComponent{},
		},
	})
	cmd.sendRoleCountEditor(s, i.Interaction, channelID, playerCount, seq)
}
```

(This changes `startPreflight`'s call signature to take a trailing `*roleRatio` argument — Task 4 makes that change to `startPreflight` itself; for now this reference will not compile until Task 4 lands, which is expected and fine within a single subagent-driven-development task boundary as long as THIS task's own commit compiles — see the note in Task 4 about doing these two tasks' `startPreflight` signature change together if the implementer finds it easier; if you hit a compile error here because `startPreflight` doesn't yet accept the extra argument, that is a signal this task and Task 4 need to be combined into one commit rather than left half-done — use your judgment and either implement Task 4's signature change now too, or coordinate by completing both in this task's session.)

- [ ] **Step 3: Add `sendRoleCountEditor`, `handleRoleCountButton`, `handleRoleCountConfirm`, `waitForRoleCountConfirm`**

In `internal/discord/commands/werewolf_rolepick.go`:

```go
// sendRoleCountEditor sends the two ephemeral editor messages (visible
// only to the starter) as followups to the "Bắt đầu" interaction, then
// starts the abandon-timeout watchdog.
func (cmd *WerewolfCommand) sendRoleCountEditor(s *discordgo.Session, interaction *discordgo.Interaction, channelID string, playerCount int, seq uint64) {
	counts := map[role]int{}
	if _, err := s.FollowupMessageCreate(interaction, true, &discordgo.WebhookParams{
		Content:    roleCountEditorContent("wolf", counts, playerCount),
		Components: roleCountButtonRows(channelID, roleCountWolfSideRoles, counts, playerCount),
		Flags:      discordgo.MessageFlagsEphemeral,
	}); err != nil {
		log.Error().Err(err).Msg("werewolf: failed to send wolf-side role-count editor")
	}
	if _, err := s.FollowupMessageCreate(interaction, true, &discordgo.WebhookParams{
		Content:    roleCountEditorContent("villager", counts, playerCount),
		Components: roleCountButtonRows(channelID, roleCountVillagerSideRoles, counts, playerCount),
		Flags:      discordgo.MessageFlagsEphemeral,
	}); err != nil {
		log.Error().Err(err).Msg("werewolf: failed to send villager-side role-count editor")
	}

	go cmd.waitForRoleCountConfirm(s, channelID, seq)
}

// waitForRoleCountConfirm polls until the starter confirms (game.Phase
// leaves phaseRoleEdit some other way) or roleEditTimeout elapses. On
// timeout, it performs the same phaseRoleEdit -> phasePreflight handoff
// handleRoleCountConfirm performs, atomically, so only one of the two
// ever actually starts startPreflight.
func (cmd *WerewolfCommand) waitForRoleCountConfirm(s *discordgo.Session, channelID string, seq uint64) {
	deadline := time.Now().Add(roleEditTimeout)
	for {
		var stillEditing bool
		func() {
			cmd.mu.Lock()
			defer cmd.mu.Unlock()
			game := cmd.gameIfCurrent(channelID, seq)
			stillEditing = game != nil && game.Phase == phaseRoleEdit
		}()
		if !stillEditing || time.Now().After(deadline) {
			break
		}
		time.Sleep(5 * time.Second)
	}

	var joinOrder []string
	var labels map[string]string
	var timedOut bool
	func() {
		cmd.mu.Lock()
		defer cmd.mu.Unlock()
		game := cmd.gameIfCurrent(channelID, seq)
		if game == nil || game.Phase != phaseRoleEdit {
			return
		}
		timedOut = true
		joinOrder = append([]string(nil), game.JoinOrder...)
		labels = make(map[string]string, len(game.Players))
		for id, p := range game.Players {
			labels[id] = p.Label
		}
		game.Phase = phasePreflight
	}()
	if !timedOut {
		return
	}

	if _, err := s.ChannelMessageSendComplex(channelID, &discordgo.MessageSend{
		Content:         "⏱️ Hết thời gian chọn vai trò tuỳ chỉnh, dùng bảng vai trò mặc định.",
		AllowedMentions: &discordgo.MessageAllowedMentions{},
	}); err != nil {
		log.Error().Err(err).Msg("werewolf: failed to post role-edit-timeout notice")
	}
	go cmd.startPreflight(s, channelID, joinOrder, labels, seq, nil)
}

func (cmd *WerewolfCommand) handleRoleCountButton(s *discordgo.Session, i *discordgo.InteractionCreate, remainder string) {
	parts := strings.SplitN(remainder, ":", 3)
	if len(parts) != 3 {
		return
	}
	sign, roleStr, channelID := parts[0], parts[1], parts[2]
	roleInt, err := strconv.Atoi(roleStr)
	if err != nil {
		return
	}
	r := role(roleInt)

	cmd.mu.Lock()
	game, ok := cmd.games[channelID]
	if !ok || game.Phase != phaseRoleEdit {
		cmd.mu.Unlock()
		respondComponentEphemeral(s, i, "Đã hết thời gian hoặc ván đã bắt đầu.")
		return
	}
	if i.Member.User.ID != game.StarterID {
		cmd.mu.Unlock()
		respondComponentEphemeral(s, i, "Chỉ người mở bàn mới được chọn.")
		return
	}
	playerCount := len(game.JoinOrder)
	current := game.PendingRoleCounts[r]
	switch sign {
	case "inc":
		if current < roleCountMax(r, playerCount) {
			game.PendingRoleCounts[r] = current + 1
		}
	case "dec":
		if current > 0 {
			game.PendingRoleCounts[r] = current - 1
		}
	}
	counts := make(map[role]int, len(game.PendingRoleCounts))
	for k, v := range game.PendingRoleCounts {
		counts[k] = v
	}
	cmd.mu.Unlock()

	side := "wolf"
	roles := roleCountWolfSideRoles
	for _, vr := range roleCountVillagerSideRoles {
		if vr == r {
			side, roles = "villager", roleCountVillagerSideRoles
			break
		}
	}
	if err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseUpdateMessage,
		Data: &discordgo.InteractionResponseData{
			Content:    roleCountEditorContent(side, counts, playerCount),
			Components: roleCountButtonRows(channelID, roles, counts, playerCount),
		},
	}); err != nil {
		log.Error().Err(err).Msg("werewolf: failed to update role-count editor")
	}
}

func (cmd *WerewolfCommand) handleRoleCountConfirm(s *discordgo.Session, i *discordgo.InteractionCreate, channelID string) {
	cmd.mu.Lock()
	game, ok := cmd.games[channelID]
	if !ok || game.Phase != phaseRoleEdit {
		cmd.mu.Unlock()
		respondComponentEphemeral(s, i, "Đã hết thời gian hoặc ván đã bắt đầu.")
		return
	}
	if i.Member.User.ID != game.StarterID {
		cmd.mu.Unlock()
		respondComponentEphemeral(s, i, "Chỉ người mở bàn mới được xác nhận.")
		return
	}
	playerCount := len(game.JoinOrder)
	if !roleCountConfirmValid(game.PendingRoleCounts, playerCount) {
		cmd.mu.Unlock()
		respondComponentEphemeral(s, i, "Tổng số vai trò phải bằng số người chơi, và cần ít nhất 1 vai phe Sói và 1 vai phe Dân.")
		return
	}
	ratio := roleRatioFromCounts(game.PendingRoleCounts)
	joinOrder := append([]string(nil), game.JoinOrder...)
	labels := make(map[string]string, len(game.Players))
	for id, p := range game.Players {
		labels[id] = p.Label
	}
	seq := game.Seq
	game.Phase = phasePreflight
	cmd.mu.Unlock()

	respondComponentEphemeral(s, i, "✅ Đã xác nhận vai trò tuỳ chỉnh!")
	if _, err := s.ChannelMessageSendComplex(channelID, &discordgo.MessageSend{
		Content:         "🐺 Đang chia vai trò và gửi tin nhắn riêng, chờ chút...",
		AllowedMentions: &discordgo.MessageAllowedMentions{},
	}); err != nil {
		log.Error().Err(err).Msg("werewolf: failed to post custom-roles-confirmed notice")
	}
	go cmd.startPreflight(s, channelID, joinOrder, labels, seq, &ratio)
}
```

Add `"strconv"` to this file's imports.

- [ ] **Step 4: Wire the two new customIDs into `HandleComponent`**

In `internal/discord/commands/werewolf.go`, `HandleComponent`, add two cases (the `werewolfRoleCountID` case must come from `strings.TrimPrefix`, then `handleRoleCountButton` does its own further parsing of the trimmed remainder — see Task 3 Step 3):

```go
	case strings.HasPrefix(customID, werewolfRoleCountConfirmID):
		cmd.handleRoleCountConfirm(s, i, strings.TrimPrefix(customID, werewolfRoleCountConfirmID))
	case strings.HasPrefix(customID, werewolfRoleCountID):
		cmd.handleRoleCountButton(s, i, strings.TrimPrefix(customID, werewolfRoleCountID))
```

(No ordering concern here despite the visual similarity: `werewolfRoleCountID` is `"ww:rolecount:"` — with a trailing colon — and `werewolfRoleCountConfirmID` is `"ww:rolecountconfirm:"`, which has `"confirm"` immediately after `"rolecount"` with no colon in between. `strings.HasPrefix("ww:rolecountconfirm:123", "ww:rolecount:")` is `false` — the two constants diverge at the character right after `"rolecount"` (`:` vs. `c`), so `strings.HasPrefix` correctly distinguishes them regardless of which `case` is checked first.)

- [ ] **Step 5: Run tests, verify, and commit**

Run: `go build ./... && go vet ./internal/discord/commands/... && go test ./internal/discord/commands/...`

Note: this task's `handleStartGame` change references `startPreflight(..., nil)`/`startPreflight(..., &ratio)` — if Task 4 hasn't landed yet in your working session, `go build` will fail here. If you're executing tasks strictly in order via subagent-driven-development, Task 4 is next and will make this compile; if you're the implementer for this exact task and the brief says Task 4 is a separate dispatch, coordinate with the controller rather than guessing — but if you're able to see that combining this task's commit with Task 4's `startPreflight` signature change is the only way to keep every commit buildable, do that and note it clearly in your report.

```bash
gofmt -l internal/discord/commands/werewolf.go internal/discord/commands/werewolf_rolepick.go internal/discord/commands/werewolf_test.go
git add internal/discord/commands/werewolf.go internal/discord/commands/werewolf_rolepick.go internal/discord/commands/werewolf_test.go
git commit -m "werewolf: add the manual role-count editor's Discord UI and timeout fallback"
```

---

### Task 4: Wire the confirmed ratio into `startPreflight`; fix the role-count announcement

**Files:**
- Modify: `internal/discord/commands/werewolf.go` (`startPreflight`)
- Modify: `internal/discord/commands/werewolf_rolepick.go` (new `formatRoleCountsAnnouncement`, `roleEmoji`)
- Test: `internal/discord/commands/werewolf_test.go`

**Interfaces:**
- Consumes: `dealRoles`, `assignRoles`, `roleCountWolfSideRoles`/`roleCountVillagerSideRoles` (Task 2).
- Produces: `startPreflight(s, channelID, joinOrder, labels, seq, customRatio *roleRatio)` — the new signature every caller (Task 3's `handleStartGame`, `sendRoleCountEditor`'s timeout path, `handleRoleCountConfirm`) already assumes. `formatRoleCountsAnnouncement(players map[string]*player) string`.

This task fixes a real correctness gap the Plan C/D final reviews already flagged: `startPreflight`'s role-assigned announcement currently re-derives counts from `rolesForPlayerCount(len(joinOrder))` instead of counting what was actually dealt. That's merely imprecise for the fixed-table path (Wolf Cub/Cursed Wolf/Traitor/Cupid are always 0 there, so re-deriving happens to agree) but becomes outright wrong for a custom-roles game, where the announcement must reflect whatever the starter actually picked.

- [ ] **Step 1: Change `startPreflight`'s signature and role-assignment call**

In `internal/discord/commands/werewolf.go`, `startPreflight`'s signature:

```go
func (cmd *WerewolfCommand) startPreflight(s *discordgo.Session, channelID string, joinOrder []string, labels map[string]string, seq uint64) {
```

becomes:

```go
func (cmd *WerewolfCommand) startPreflight(s *discordgo.Session, channelID string, joinOrder []string, labels map[string]string, seq uint64, customRatio *roleRatio) {
```

Where it currently does:

```go
	players, err := assignRoles(joinOrder, labels)
	if err != nil {
		sendPlain("❌ Không thể chia vai trò: " + err.Error())
		cmd.abortGameStillCurrent(s, channelID, seq)
		return
	}
```

Replace with:

```go
	var players map[string]*player
	if customRatio != nil {
		players = dealRoles(joinOrder, labels, *customRatio)
	} else {
		var err error
		players, err = assignRoles(joinOrder, labels)
		if err != nil {
			sendPlain("❌ Không thể chia vai trò: " + err.Error())
			cmd.abortGameStillCurrent(s, channelID, seq)
			return
		}
	}
```

- [ ] **Step 2: Handle the unreachable-players reassignment path**

Further down, `startPreflight` has a second `assignRoles` call for the "some players never got their DM" fallback path:

```go
			reassigned, err := assignRoles(joinOrder, labels)
			if err != nil {
				sendPlain("❌ Không thể chia lại vai trò: " + err.Error())
				cmd.abortGameStillCurrent(s, channelID, seq)
				return
			}
			cmd.dmRoleReveals(s, reassigned, joinOrder)
			players = reassigned
```

This reassignment happens with a *smaller* `joinOrder` than the original (unreachable players were dropped) — a stale custom ratio sized for the original count would no longer sum correctly and `dealRoles`'s `assign()` loop would try to index past the end of the shrunk `shuffled` slice. Deliberately fall back to the fixed table for this one reassignment regardless of whether the original deal used a custom ratio — re-deriving a smaller custom ratio proportionally isn't spec'd and isn't worth the complexity for what's already a rare edge case (some players' DMs failed twice in a row). Leave this block exactly as-is; it already calls `assignRoles`, which is the fixed-table path — no change needed here, but add a one-line comment explaining why:

```go
			// Deliberately always falls back to the fixed table here, even
			// if the original deal used a custom ratio — a custom ratio was
			// sized for the original (larger) joinOrder, and re-deriving a
			// smaller one proportionally isn't worth the complexity for this
			// already-rare double-unreachable-DM edge case.
			reassigned, err := assignRoles(joinOrder, labels)
```

- [ ] **Step 3: Replace the role-count announcement with one derived from the actual roster**

In `internal/discord/commands/werewolf_rolepick.go`, add:

```go
// roleEmoji is the emoji prefix used in player-facing role-count text —
// role.label() itself is plain text, so this is the announcement/editor
// layer's own small mapping, kept separate from role.label() since not
// every caller of label() wants an emoji prefix.
func roleEmoji(r role) string {
	switch r {
	case roleWerewolf:
		return "🐺"
	case roleWolfCub:
		return "🐺👶"
	case roleCursedWolf:
		return "🐺✨"
	case roleTraitor:
		return "🎭"
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
		return "👤"
	}
}

// formatRoleCountsAnnouncement builds the role-count breakdown for
// startPreflight's role-assigned announcement, counting the players map
// that was ACTUALLY dealt — correct for both the fixed table and a
// custom-roles game, unlike re-deriving from rolesForPlayerCount (which
// only ever agrees with reality on the fixed-table path, since that path
// alone is what it can see). Zero-count roles are omitted; Villager
// (whatever's left over) is always listed last if present.
func formatRoleCountsAnnouncement(players map[string]*player) string {
	counts := make(map[role]int)
	for _, p := range players {
		counts[p.Role]++
	}
	var parts []string
	for _, r := range append(append([]role{}, roleCountWolfSideRoles...), roleCountVillagerSideRoles...) {
		if r == roleVillager {
			continue // rendered last, below
		}
		if c := counts[r]; c > 0 {
			parts = append(parts, fmt.Sprintf("%s %s: %d", roleEmoji(r), r.label(), c))
		}
	}
	if c := counts[roleVillager]; c > 0 {
		parts = append(parts, fmt.Sprintf("%s %s: %d", roleEmoji(roleVillager), roleVillager.label(), c))
	}
	return "\n" + strings.Join(parts, " · ")
}
```

In `internal/discord/commands/werewolf.go`, `startPreflight`, replace:

```go
	pingText, allowedMentions := pingOnce(joinOrder)
	roleCounts := "" // best-effort: if this somehow errors, just omit the breakdown rather than failing the announcement
	if ratio, err := rolesForPlayerCount(len(joinOrder)); err == nil {
		villagers := len(joinOrder) - ratio.Werewolves - ratio.Seers - ratio.Doctors - ratio.Witches - ratio.Hunters - ratio.Disruptors
		roleCounts = fmt.Sprintf("\n🐺 Sói: %d · 🔮 Tiên Tri: %d · 💉 Bác Sĩ: %d · 🧙 Phù Thủy: %d · 🏹 Thợ Săn: %d · 🤫 Kẻ Phá Hoại: %d · 👤 Dân Làng: %d",
			ratio.Werewolves, ratio.Seers, ratio.Doctors, ratio.Witches, ratio.Hunters, ratio.Disruptors, villagers)
	}
```

with:

```go
	pingText, allowedMentions := pingOnce(joinOrder)
	roleCounts := formatRoleCountsAnnouncement(players)
```

(`players` here is the map already populated by Step 1's assignment logic earlier in the same function — confirm it's still in scope at this point in `startPreflight`; if the local variable is named something else by the time you're editing the live file, use that name instead.)

- [ ] **Step 4: Write the test**

Add to `internal/discord/commands/werewolf_test.go`:

```go
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
```

- [ ] **Step 5: Run tests, verify, and commit**

Run: `go build ./... && go vet ./internal/discord/commands/... && go test ./internal/discord/commands/...`

Expected: all pass — this fully resolves the build-time dependency Task 3 left open (`startPreflight`'s new signature), so running the full suite here is the real end-to-end check for both tasks together. Every pre-existing test that reaches `startPreflight` indirectly (there are none that call it directly — it's only reachable through the full interaction flow, which none of the unit tests exercise) is unaffected.

```bash
gofmt -l internal/discord/commands/werewolf.go internal/discord/commands/werewolf_rolepick.go internal/discord/commands/werewolf_test.go
git add internal/discord/commands/werewolf.go internal/discord/commands/werewolf_rolepick.go internal/discord/commands/werewolf_test.go
git commit -m "werewolf: wire custom ratio into startPreflight, fix role-count announcement to count the actual roster"
```

---

### Task 5: `/help werewolf` per-role interaction guide + stale Traitor text fix

**Files:**
- Modify: `internal/discord/commands/help.go`
- Modify: `internal/discord/commands/werewolf.go` (`roleInteractionNote`)
- Test: none (documentation-only; `roleInteractionNote`'s existing behavior for every role except Traitor is unchanged, so no new test is needed beyond a compile/build check)

Spec §6 asks for a per-role "when does the bot DM you, and where do you chat/act" breakdown in `/help werewolf`. The existing embed (`internal/discord/commands/help.go`, the `"werewolf"` case) is in English and predates every role beyond Werewolf/Villager/Seer/Doctor — this task brings it up to date with every role from Plans A-E and documents the new `custom-roles` option.

- [ ] **Step 1: Fix the stale Traitor interaction note**

In `internal/discord/commands/werewolf.go`, `roleInteractionNote`, the `roleTraitor` case currently reads:

```go
	case roleTraitor:
		return "Bot sẽ nhắn tin riêng cho bạn biết ai là Sói khi bắt đầu ván (và mỗi khi danh sách Sói thay đổi) — bạn không có hành động ban đêm."
```

The parenthetical is stale since the 2026-08-22 change to an unconditional nightly resend (`traitorRosterDM`/`sendTraitorRosterDM`, called once per night from `runNightPhase`, not only on a roster change). Change to:

```go
	case roleTraitor:
		return "Bot sẽ nhắn tin riêng cho bạn biết ai là Sói khi bắt đầu ván, và lại mỗi đêm — bạn không có hành động ban đêm."
```

- [ ] **Step 2: Update the `/help werewolf` embed**

In `internal/discord/commands/help.go`, the `"werewolf"` case currently has these fields: `Subcommands`, `How it works`, `Channel lock`, `Privacy note`, `Limits`. Update `Description` and `Subcommands`, and add one new field, `Roles & interaction guide`, right after `How it works`:

Change `Description` from:

```go
			Description: "Play a game of Werewolf (Ma Sói): Werewolves, Villagers, a Seer, and a Doctor.",
```

to:

```go
			Description: "Play a game of Werewolf (Ma Sói) with up to 11 roles: Werewolf, Wolf Cub, Cursed Wolf, Traitor, Villager, Seer, Guard/Doctor, Witch, Hunter, Disruptor, and Cupid.",
```

Change `Subcommands`'s value from:

```go
						Value:  "• `/werewolf start reveal-on-death:true|false` - Open a lobby in this channel\n• `/werewolf status` - Check the current phase/round without leaking roles\n• `/werewolf leave` - Leave the lobby, or leave a running game (counts as being eliminated)",
```

to:

```go
						Value:  "• `/werewolf start reveal-on-death:true|false custom-roles:true|false` - Open a lobby in this channel; custom-roles opens an ephemeral role-count editor for the starter after the roster locks, instead of the default player-count table\n• `/werewolf status` - Check the current phase/round without leaking roles\n• `/werewolf leave` - Leave the lobby, or leave a running game (counts as being eliminated)",
```

Add the new field right after `How it works`:

```go
					{
						Name:   "Roles & interaction guide",
						Value:  "🐺 Werewolf / Wolf Cub / Cursed Wolf (once revealed): act together each night in a private wolf thread.\n🔮 Seer / 💉 Guard / 🧙 Witch / 🏹 Hunter / 🤫 Disruptor: get a private DM each night to choose their action.\n💘 Cupid: gets a private DM only on the very first night.\n🎭 Traitor: gets a DM naming the current wolves at game start, and again every night after — no night action of their own.\n👤 Villager: no special action — just discuss and vote during the day.\nEveryone chats publicly in the alive-players thread during the day; the main channel only shows phase/vote announcements.",
						Inline: false,
					},
```

- [ ] **Step 3: Build and commit**

Run: `go build ./... && go vet ./internal/discord/commands/... && go test ./internal/discord/commands/...`

Expected: all pass (this task changes no logic, only string content).

```bash
gofmt -l internal/discord/commands/help.go internal/discord/commands/werewolf.go
git add internal/discord/commands/help.go internal/discord/commands/werewolf.go
git commit -m "werewolf: update /help werewolf with a per-role interaction guide, fix stale Traitor note"
```

---

### Task 6: Verification (this plan makes everything reachable — full real playtest)

**Files:** none (verification only)

Unlike every prior plan's Task 6, this one is a genuinely complete real-Discord playtest — Plan E's manual-pick mode is what finally makes Wolf Cub, Cursed Wolf, Traitor, and Cupid reachable in an actual game, on top of everything already reachable via the fixed table. Hand this whole checklist to the user; it cannot be dispatched to a subagent.

Checklist to hand over:

- [ ] Run `/werewolf start custom-roles:true` with 7+ players. Confirm the "Bắt đầu" click produces two ephemeral messages only the starter can see (wolf-side, villager-side), each showing live counts, a running total, and a balance score that updates on every ➖/➕ click.
- [ ] Confirm Seer, Doctor, Witch, Hunter, Disruptor, Cupid, and Traitor's ➕ buttons disable at count 1 (can't go to 2) — confirm this by trying to push past it; the button should already be visually disabled.
- [ ] Confirm "✅ Xác nhận" is rejected (ephemeral error, no game-start) when the total doesn't equal the player count, or when one whole side (wolf or villager) has zero roles picked.
- [ ] Deal at least one each of Wolf Cub, Cursed Wolf, Traitor, and Cupid in one game via the editor, then confirm and play a full game:
  - Wolf Cub: dies once (any cause), confirm the *next* wolf-vote round bites 2 targets.
  - Cursed Wolf: gets bitten-and-not-saved at least once, confirm they convert (survive, join the wolf vote from the next night on) rather than dying.
  - Traitor: confirm they get a DM at game start listing the current wolves, and a fresh DM every subsequent night (even with no roster change).
  - Cupid: confirm the round-1-only DM, the exactly-2-picks select, and both paired players getting told who their partner is; kill one of them and confirm the other dies too in the same resolution step.
- [ ] Leave the editor open with no clicks for the full `roleEditTimeout` (5 minutes) and confirm the game automatically falls back to the fixed table and proceeds, with a public notice explaining why.
- [ ] Run a game via the *default* (non-custom) path and confirm the role-assigned announcement's counts exactly match what was actually dealt (this is the fix from Task 4 — should already have been true, just confirm no regression).
- [ ] Run `/help werewolf` and confirm the new "Roles & interaction guide" field renders correctly and isn't truncated (Discord embed field values cap at 1024 characters).
- [ ] Skim `internal/discord/commands/werewolf_test.go` for this plan's new tests (`TestWerewolfDefinitionHasCustomRolesOption`, `TestRoleCountMax`, `TestBalanceScore`, `TestRoleCountConfirmValid`, `TestRoleRatioFromCounts`, `TestRoleCountEditorContentShowsCountsAndBalance`, `TestFormatRoleCountsAnnouncementCountsActualRoster`) and confirm they read as genuine behavioral checks.
- [ ] Confirm `go test ./internal/discord/commands/... -v` passes in full, including every pre-existing test from Plans A-D.
- [ ] Once this checklist passes, re-open Plan C's and Plan D's own Task 6 checklists for the specific roles (Cursed Wolf's Seer-check-before/after-conversion behavior, Wolf Cub's Witch-heal-of-two-targets select menu, the Disruptor per-member thread-mute hard gate from Plan D) that could only be reviewed, not live-tested, until now.

---

## Self-Review

**Spec coverage:** §3b (manual-pick mode) → Tasks 1-4. §6 (`/help werewolf` guide) → Task 5. §7's "no new Discord-side artifacts beyond what's already covered" — this plan adds no new threads/overwrites, only ephemeral messages (self-cleaning, Discord expires them; nothing to explicitly clean up on a terminal path).

**Carry-forward items from Plans C/D, resolved here:** the multi-Traitor/multi-Hunter/multi-Witch/etc. "only `[0]` ever acts" gap → resolved by the 1-cap decision (Global Constraints) rather than by building N-actor support; `startPreflight`'s role-count announcement correctness → resolved by Task 4's `formatRoleCountsAnnouncement`; Cupid never mentioned in the night-start announcement text → still not addressed (cosmetic, Cupid only ever acts on round 1, the general "Ma Sói, Tiên Tri, Bác Sĩ, Thợ Săn, Kẻ Phá Hoại đang hành động..." line predates Cupid's existence) — flag as a trivial optional follow-up, not worth its own task.

**Out of this plan's — and the whole spec's — scope, confirmed correctly deferred:** proper multi-instance support for the 7 currently single-actor-only roles (a real future Plan F if ever wanted, per the user's explicit decision this session); persisting anything across bot restarts (explicitly out of scope per the spec's own "Out of scope" section).
