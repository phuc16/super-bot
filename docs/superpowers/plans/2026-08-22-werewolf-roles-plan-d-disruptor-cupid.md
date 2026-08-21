# Werewolf Extended Roles — Plan D: Disruptor + Cupid Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add the Disruptor and Cupid roles to `/werewolf`, on top of Plans A-C (faction model, plurality wolf vote, Witch, Hunter, Wolf Cub, Cursed Wolf, Traitor). This is the fourth of five sequential plans implementing `docs/superpowers/specs/2026-08-21-werewolf-roles-design.md`.

**Architecture:** Disruptor is additive and reachable in real games today — per spec §3a it's one of the seven roles the *fixed* player-count table deals, so unlike Plan C's three roles, this plan must extend `rolesForPlayerCount`'s actual table rows, not just `dealRoles`. Its mechanic is a nightly re-pickable target whose effect (a per-member thread mute) lands one phase later, during the *next* day-discuss window — a new interaction shape this codebase hasn't needed before (every existing night action's effect lands the same night). Cupid is the opposite: per spec §3a it's explicitly excluded from the fixed table forever, same as Wolf Cub/Cursed Wolf/Traitor — its mechanic (a one-time round-1 pairing plus a lover-death chain) is fully implemented and unit-tested here but stays unreachable in a real game until Plan E's manual role-pick mode ships. The lover-death chain and the existing Hunter-revenge chain can now both fire off the same death, so this plan unifies them into one BFS-style resolver instead of leaving two independent single-hop checks that could each trigger the other without composing correctly.

**Tech Stack:** Go, discordgo v0.29.0, existing `sync.Mutex`-guarded `map[string]*werewolfGame` pattern.

## Global Constraints

- This is Plan D of five. Plans A (faction/catalogue/plurality vote), B (Witch/Hunter), and C (Wolf Cub/Cursed Wolf/Traitor) are already complete and merged into `feature/werewolf`. Do not re-implement anything from them.
- **Disruptor is dealt by `rolesForPlayerCount` (reachable today); Cupid is not, ever** (spec §3a) — same unreachable-until-Plan-E status as Plan C's three roles. Say so explicitly in Task 4's scope.
- The exact player counts at which Disruptor enters the fixed table are a plan-level design call, not spec-mandated with specific numbers: Task 1 adds it at 15+ players (where the wolf count also steps up to 4-5), leaving 5-14 unchanged. This is a judgment call — flag it as such in Task 1's PR description, adjustable later without any other code changes.
- Every `discordgo.MessageSend`/`InteractionResponseData`/`MessageEdit` whose `Content` contains a `<@id>` mention must set `AllowedMentions` explicitly.
- No DB/migration changes — werewolf remains fully in-memory.
- After editing any file, run `gofmt -l <file>` on every file you touched; if it lists the file, run `gofmt -w <file>` before your final build/test step.
- Every read/write of `*werewolfGame`/`*player` fields inside the interactive engine (`werewolf.go`, `werewolf_night.go`, `werewolf_day.go`) must happen strictly inside the existing `cmd.mu.Lock()`/`defer cmd.mu.Unlock()` critical sections.
- Any code reading `*player`/`*werewolfGame` state after a `cmd.mu.Unlock()` must read it from a `copyPlayer`/`copyPlayers` snapshot taken before the unlock, never a live pointer.
- Traitor's roster DM was recently changed (2026-08-22) to resend unconditionally every night via `traitorRosterDM`/`sendTraitorRosterDM`, called once from `runNightPhase`'s locked setup — this plan's Disruptor DM should follow the exact same "computed under lock in `runNightPhase`'s setup closure, sent after unlock alongside the other night-start prompts" shape, not the old change-detection pattern.

---

### Task 1: `roleRatio.Disruptors`, fixed-table rows, and `dealRoles`/`startPreflight` wiring

**Files:**
- Modify: `internal/discord/commands/werewolf.go` (`roleRatio`, `rolesForPlayerCount`, `dealRoles`, `startPreflight`'s role-count announcement)
- Test: `internal/discord/commands/werewolf_test.go`

**Interfaces:**
- Produces: `roleRatio.Disruptors int`, dealt via `dealRoles` and (from this task on) actually populated by `rolesForPlayerCount` at 15+ players.

- [ ] **Step 1: Add the field and deal it**

In `internal/discord/commands/werewolf.go`, add to `roleRatio`:

```go
	Disruptors   int
```

In `dealRoles`, add one line after the existing `assign(ratio.Hunters, roleHunter)`:

```go
	assign(ratio.Disruptors, roleDisruptor)
```

- [ ] **Step 2: Extend the fixed table**

In `rolesForPlayerCount`, change the `n <= 17` and `default` (18-20) cases from:

```go
	case n <= 17:
		return roleRatio{Werewolves: 4, Seers: 1, Doctors: 1, Witches: 1, Hunters: 1}, nil
	default: // 18-20
		return roleRatio{Werewolves: 5, Seers: 1, Doctors: 1, Witches: 1, Hunters: 1}, nil
```

to:

```go
	case n <= 17:
		return roleRatio{Werewolves: 4, Seers: 1, Doctors: 1, Witches: 1, Hunters: 1, Disruptors: 1}, nil
	default: // 18-20
		return roleRatio{Werewolves: 5, Seers: 1, Doctors: 1, Witches: 1, Hunters: 1, Disruptors: 1}, nil
```

(the `n <= 14` case — 11-14 players — is left unchanged; Disruptor only enters at 15+.)

- [ ] **Step 3: Fix the villager-count math and role-count announcement**

In `startPreflight`, find:

```go
	if ratio, err := rolesForPlayerCount(len(joinOrder)); err == nil {
		villagers := len(joinOrder) - ratio.Werewolves - ratio.Seers - ratio.Doctors - ratio.Witches - ratio.Hunters
		roleCounts = fmt.Sprintf("\n🐺 Sói: %d · 🔮 Tiên Tri: %d · 💉 Bác Sĩ: %d · 🧙 Phù Thủy: %d · 🏹 Thợ Săn: %d · 👤 Dân Làng: %d",
			ratio.Werewolves, ratio.Seers, ratio.Doctors, ratio.Witches, ratio.Hunters, villagers)
	}
```

Replace with (this must be fixed now, not deferred — with Disruptor entering the fixed table at 15+ players, the old formula would silently undercount villagers by `ratio.Disruptors` in every real game at that size):

```go
	if ratio, err := rolesForPlayerCount(len(joinOrder)); err == nil {
		villagers := len(joinOrder) - ratio.Werewolves - ratio.Seers - ratio.Doctors - ratio.Witches - ratio.Hunters - ratio.Disruptors
		roleCounts = fmt.Sprintf("\n🐺 Sói: %d · 🔮 Tiên Tri: %d · 💉 Bác Sĩ: %d · 🧙 Phù Thủy: %d · 🏹 Thợ Săn: %d · 🤫 Kẻ Phá Hoại: %d · 👤 Dân Làng: %d",
			ratio.Werewolves, ratio.Seers, ratio.Doctors, ratio.Witches, ratio.Hunters, ratio.Disruptors, villagers)
	}
```

- [ ] **Step 4: Update the table test**

In `internal/discord/commands/werewolf_test.go`, `TestRolesForPlayerCount`'s cases for `n=15` through `n=20` need `Disruptors: 1` added to their `want`. Find the existing cases (they currently read e.g. `{15, roleRatio{Werewolves: 4, Seers: 1, Doctors: 1, Witches: 1, Hunters: 1}}`) and add the field:

```go
		{15, roleRatio{Werewolves: 4, Seers: 1, Doctors: 1, Witches: 1, Hunters: 1, Disruptors: 1}},
		{16, roleRatio{Werewolves: 4, Seers: 1, Doctors: 1, Witches: 1, Hunters: 1, Disruptors: 1}},
		{17, roleRatio{Werewolves: 4, Seers: 1, Doctors: 1, Witches: 1, Hunters: 1, Disruptors: 1}},
		{18, roleRatio{Werewolves: 5, Seers: 1, Doctors: 1, Witches: 1, Hunters: 1, Disruptors: 1}},
		{19, roleRatio{Werewolves: 5, Seers: 1, Doctors: 1, Witches: 1, Hunters: 1, Disruptors: 1}},
		{20, roleRatio{Werewolves: 5, Seers: 1, Doctors: 1, Witches: 1, Hunters: 1, Disruptors: 1}},
```

(cases for `n=5` through `n=14` are unchanged.)

- [ ] **Step 5: Write a dealing test**

Add to `internal/discord/commands/werewolf_test.go`:

```go
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
```

- [ ] **Step 6: Run tests, verify, and commit**

Run: `go build ./... && go vet ./internal/discord/commands/... && go test ./internal/discord/commands/...`

Expected: all pass, including the updated `TestRolesForPlayerCount` and every pre-existing test at player counts below 15 (unchanged).

```bash
gofmt -l internal/discord/commands/werewolf.go internal/discord/commands/werewolf_test.go
git add internal/discord/commands/werewolf.go internal/discord/commands/werewolf_test.go
git commit -m "werewolf: deal Disruptor at 15+ players, fix villager-count math"
```

---

### Task 2: Disruptor mechanic — nightly re-pickable mute target, applied the following day-discuss

**Files:**
- Modify: `internal/discord/commands/werewolf.go` (`werewolfGame`, `roundLog`, `HandleComponent`)
- Modify: `internal/discord/commands/werewolf_night.go` (`runNightPhase`, `nightActionsComplete`, `resolveNight`'s AFK loop, new `dmDisruptorPrompt`/`handleDisruptorTarget`)
- Modify: `internal/discord/commands/werewolf_day.go` (`runDayDiscuss`, `runDayVote`, `formatRoundBlock`)
- Test: `internal/discord/commands/werewolf_test.go`

**Interfaces:**
- Consumes: `aliveExcept`, `alivePlayersOfRole`, `targetSelectMenu`, `announceRoleDecided`, `muteAliveThread`/`unmuteAliveThread` (existing `@everyone`-scoped mute — unrelated, this task adds a *second*, per-member overwrite alongside it).
- Produces: `game.DisruptorTarget string`, `game.DisruptorActedThisRound bool`, `roundLog.DisruptorMutedLabel string`.

Spec §4g: nightly DM (while alive) with a target-select menu (any alive player except self); re-pickable within the same night (spec §4i's "same first-pick-this-round rule as Hunter" only makes sense if a later pick in the same round can silently overwrite an earlier one without re-announcing — this is the same shape Hunter had before Hunter's AFK-exemption amendment, *not* the same as Doctor/Seer's lock-after-first-pick). Unlike Hunter, Disruptor's target does **not** persist across rounds (`game.DisruptorTarget` is spec'd to reset to `""` each night) and Disruptor is **not** exempt from AFK/night-completion gating — a round with no pick is a normal AFK strike, same as Seer/Doctor. The chosen player is muted in the alive-discussion thread for the *next* day-discuss phase only, via a **per-member** permission overwrite (`discordgo.PermissionOverwriteTypeMember`) applied when that day-discuss starts and removed when it ends.

- [ ] **Step 1: Add game/recap state**

In `internal/discord/commands/werewolf.go`, add to `werewolfGame` (next to `HunterTarget`):

```go
	DisruptorTarget        string // this round's mute pick, "" if none — reset each night before the new pick, and read again unchanged by the FOLLOWING day-discuss/day-vote phase transition to apply/remove the per-member mute
	DisruptorActedThisRound bool  // reset each round; true once the Disruptor has made at least one pick this round — gates the progress announcement to the first pick only, mirroring Hunter's pre-AFK-exemption shape
```

Add to `roundLog` (next to `DisruptorMutedLabel` — this is the only new recap field this task needs, per spec §5):

```go
	DisruptorMutedLabel string // who was muted for the following day-discuss, "" if the Disruptor didn't act (or doesn't exist) this round
```

- [ ] **Step 2: Add the custom ID and DM/handler**

In `internal/discord/commands/werewolf_night.go`, add to the existing `const` block of `werewolfXxxID` custom IDs:

```go
	werewolfDisruptorTargetID = "ww:disruptortarget:"
```

Add `dmDisruptorPrompt` right after `dmHunterPrompt`:

```go
func (cmd *WerewolfCommand) dmDisruptorPrompt(s *discordgo.Session, channelID string, disruptor *player, targets []*player) {
	dm, err := s.UserChannelCreate(disruptor.UserID)
	if err != nil {
		log.Error().Err(err).Msg("werewolf: failed to open DM for disruptor")
		return
	}
	if _, err := s.ChannelMessageSendComplex(dm.ID, &discordgo.MessageSend{
		Content:    "🤫 Chọn người bạn muốn làm câm lặng trong buổi thảo luận ngày mai (có thể đổi lại trong đêm nay).",
		Components: targetSelectMenu(werewolfDisruptorTargetID+channelID, "Chọn người để làm câm lặng", targets),
	}); err != nil {
		log.Error().Err(err).Msg("werewolf: failed to DM disruptor prompt")
	}
}
```

Add `handleDisruptorTarget` right after `handleHunterTarget` — deliberately modeled on `handleHunterTarget`'s re-pickable shape, not Doctor's lock-after-first-pick shape, but gated by `game.NightActionsClosed` like Seer/Doctor/wolf (unlike Hunter, which is never gated by it):

```go
func (cmd *WerewolfCommand) handleDisruptorTarget(s *discordgo.Session, i *discordgo.InteractionCreate, channelID string) {
	values := i.MessageComponentData().Values
	if len(values) == 0 {
		return
	}
	targetID := values[0]
	clickerID := interactionUserID(i)

	cmd.mu.Lock()
	game, ok := cmd.games[channelID]
	if !ok || game.Phase != phaseNight || game.NightActionsClosed {
		cmd.mu.Unlock()
		respondComponentEphemeral(s, i, "Đêm nay đã kết thúc rồi.")
		return
	}
	clicker, isDisruptor := game.Players[clickerID]
	if !isDisruptor || clicker.Role != roleDisruptor || !clicker.Alive {
		cmd.mu.Unlock()
		respondComponentEphemeral(s, i, "Chỉ Kẻ Phá Hoại còn sống mới được chọn.")
		return
	}
	if target, exists := game.Players[targetID]; !exists || !target.Alive {
		cmd.mu.Unlock()
		respondComponentEphemeral(s, i, "Mục tiêu không còn hợp lệ.")
		return
	}
	firstActionThisRound := !game.DisruptorActedThisRound
	game.DisruptorTarget = targetID
	game.DisruptorActedThisRound = true
	cmd.mu.Unlock()

	respondComponentEphemeralMentioned(s, i, fmt.Sprintf("✅ Đã chọn: <@%s>.", targetID))
	if firstActionThisRound {
		announceRoleDecided(s, channelID, "🤫 Kẻ Phá Hoại")
	}
}
```

- [ ] **Step 3: Wire into `HandleComponent`**

In `internal/discord/commands/werewolf.go`, `HandleComponent`, add a case next to `werewolfHunterTargetID`:

```go
	case strings.HasPrefix(customID, werewolfDisruptorTargetID):
		cmd.handleDisruptorTarget(s, i, strings.TrimPrefix(customID, werewolfDisruptorTargetID))
```

- [ ] **Step 4: Wire into `runNightPhase`**

In `internal/discord/commands/werewolf_night.go`, `runNightPhase`'s `var (...)` block, add:

```go
		disruptor      *player
		disruptorTargets []*player
```

In the locked closure's per-round reset block, add next to `game.HunterActedThisRound = false`:

```go
		game.DisruptorTarget = ""
		game.DisruptorActedThisRound = false
```

Right after the existing `if hunters := alivePlayersOfRole(...)` block, add:

```go
		if disruptors := alivePlayersOfRole(game, roleDisruptor); len(disruptors) > 0 {
			disruptor = copyPlayer(disruptors[0])
			disruptorTargets = copyPlayers(aliveExcept(game, disruptor.UserID))
		}
```

After the unlock, right after the existing `if hunter != nil { cmd.dmHunterPrompt(...) }` call, add:

```go
	if disruptor != nil {
		cmd.dmDisruptorPrompt(s, channelID, disruptor, disruptorTargets)
	}
```

Update the night-start announcement text from `"Ma Sói, Tiên Tri, Bác Sĩ, Thợ Săn đang hành động..."` to `"Ma Sói, Tiên Tri, Bác Sĩ, Thợ Săn, Kẻ Phá Hoại đang hành động..."`.

- [ ] **Step 5: Gate night completion and AFK strikes on Disruptor**

In `internal/discord/commands/werewolf_night.go`, `nightActionsComplete`, add next to the existing Doctor check:

```go
	if len(alivePlayersOfRole(game, roleDisruptor)) > 0 && game.DisruptorTarget == "" {
		return false
	}
```

In `resolveNight`'s locked closure, add an AFK-strike loop next to the existing Doctor one:

```go
		for _, p := range alivePlayersOfRole(game, roleDisruptor) {
			if game.DisruptorTarget != "" {
				game.AFKStrikes[p.UserID] = 0
			} else {
				game.AFKStrikes[p.UserID]++
			}
		}
```

- [ ] **Step 6: Apply and remove the per-member mute at the day-discuss/day-vote boundary**

In `internal/discord/commands/werewolf_day.go`, `runDayDiscuss`'s locked closure, capture the pick alongside the other snapshots (add `mutedUserID string` to the function's `var` block, and `mutedUserID = game.DisruptorTarget` inside the closure — read directly; `DisruptorTarget` isn't reset until the *next* `runNightPhase`, so at day-discuss-start time it still holds the round that just resolved). Right after the existing `unmuteAliveThread(s, guildID, aliveThreadID)` call, add:

```go
	if mutedUserID != "" && aliveThreadID != "" {
		if err := s.ChannelPermissionSet(aliveThreadID, mutedUserID, discordgo.PermissionOverwriteTypeMember, 0, discordgo.PermissionSendMessagesInThreads); err != nil {
			log.Error().Err(err).Str("user_id", mutedUserID).Msg("werewolf: failed to apply disruptor per-member mute")
		}
	}
```

Also, inside the same locked closure, write the recap field — find the block that reads `if n := len(game.History); ...` for the round's history (if `runDayDiscuss` doesn't currently have one, add a minimal one — check the round's `game.History` entry and set `entry.DisruptorMutedLabel` to the muted player's label if `mutedUserID != ""` and they still exist in `game.Players`, else leave it `""`).

In `internal/discord/commands/werewolf_day.go`, `runDayVote`'s locked closure, read the same field again (add `mutedUserID string` to this function's `var` block too) and, right after the existing `muteAliveThread(s, guildID, aliveThreadID)` call, remove the per-member overwrite unconditionally (harmless if it was never applied or the muted player already left the thread):

```go
	if mutedUserID != "" && aliveThreadID != "" {
		if err := s.ChannelPermissionDelete(aliveThreadID, mutedUserID); err != nil {
			log.Error().Err(err).Str("user_id", mutedUserID).Msg("werewolf: failed to remove disruptor per-member mute")
		}
	}
```

No additional terminal-path cleanup is needed for the per-member overwrite: if the game ends while it's active, the whole alive-discussion thread is deleted on every terminal path already (existing `checkAndHandleGameEnd` behavior), which removes the overwrite along with the channel.

- [ ] **Step 7: Add the recap line**

In `internal/discord/commands/werewolf_day.go`, `formatRoundBlock`, add one line (same omit-if-empty pattern as the rest of the function):

```go
	if r.DisruptorMutedLabel != "" {
		fmt.Fprintf(&b, "\n🤫 Kẻ Phá Hoại làm câm lặng: %s", r.DisruptorMutedLabel)
	}
```

- [ ] **Step 8: Write the tests**

Add to `internal/discord/commands/werewolf_test.go`:

```go
func TestFormatRoundBlockDisruptorMuted(t *testing.T) {
	r := roundLog{Round: 2, DisruptorMutedLabel: "Duy"}
	got := formatRoundBlock(r)
	if !strings.Contains(got, "Duy") || !strings.Contains(got, "câm lặng") {
		t.Errorf("formatRoundBlock() = %q, want the mute mentioned", got)
	}
}
```

(Disruptor's re-pickable-within-a-round shape and the day-discuss/day-vote mute boundary are integration behavior exercised through Discord interaction handlers and network calls — there is no new pure function here beyond the recap line, matching how Plan B/C's own Discord-interaction handlers had no additional unit tests beyond their recap lines and pure helpers.)

- [ ] **Step 9: Run tests, verify, and commit**

Run: `go build ./... && go vet ./internal/discord/commands/... && go test ./internal/discord/commands/...`

Expected: all pass, including every pre-existing test — no pre-existing game has a `roleDisruptor` player, so `nightActionsComplete`'s new gate, the new AFK loop, and the day-discuss/day-vote mute calls are all no-ops (`mutedUserID == ""`) for every one of them.

```bash
gofmt -l internal/discord/commands/werewolf.go internal/discord/commands/werewolf_night.go internal/discord/commands/werewolf_day.go internal/discord/commands/werewolf_test.go
git add internal/discord/commands/werewolf.go internal/discord/commands/werewolf_night.go internal/discord/commands/werewolf_day.go internal/discord/commands/werewolf_test.go
git commit -m "werewolf: add Disruptor — nightly re-pickable mute target for the next day-discuss"
```

---

### Task 3: `roleRatio.Cupids` + `dealRoles` wiring (foundation only — Cupid stays unreachable via the fixed table)

**Files:**
- Modify: `internal/discord/commands/werewolf.go` (`roleRatio`, `dealRoles`)
- Test: `internal/discord/commands/werewolf_test.go`

**Interfaces:**
- Produces: `roleRatio.Cupids int` — never set by `rolesForPlayerCount` (spec §3a explicitly excludes Cupid from the fixed table forever, same as Wolf Cub/Cursed Wolf/Traitor), only ever populated by Plan E's manual role-pick mode.

- [ ] **Step 1: Add the field and deal it**

In `internal/discord/commands/werewolf.go`, add to `roleRatio` (next to `Traitors`):

```go
	Cupids       int
```

In `dealRoles`, add one line after `assign(ratio.Disruptors, roleDisruptor)`:

```go
	assign(ratio.Cupids, roleCupid)
```

Do **not** touch `rolesForPlayerCount` — Cupid must never appear there.

- [ ] **Step 2: Write the dealing test**

Add to `internal/discord/commands/werewolf_test.go`:

```go
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
```

- [ ] **Step 3: Run tests, verify, and commit**

Run: `go build ./... && go vet ./internal/discord/commands/... && go test ./internal/discord/commands/...`

Expected: all pass. `rolesForPlayerCount`'s table is untouched by this task, so `ratio.Cupids` stays `0` for every fixed-table game, exactly as spec §3a requires.

```bash
gofmt -l internal/discord/commands/werewolf.go internal/discord/commands/werewolf_test.go
git add internal/discord/commands/werewolf.go internal/discord/commands/werewolf_test.go
git commit -m "werewolf: add roleRatio.Cupids (dealRoles-only, never via the fixed table)"
```

---

### Task 4: Cupid mechanic — round-1 pairing, both lovers informed

**Files:**
- Modify: `internal/discord/commands/werewolf.go` (`werewolfGame`, `HandleComponent`)
- Modify: `internal/discord/commands/werewolf_night.go` (`runNightPhase`, new `cupidPairSelectMenu`/`dmCupidPrompt`/`handleCupidPair`)
- Test: `internal/discord/commands/werewolf_test.go`

**Interfaces:**
- Consumes: `aliveExcept`, `truncateLabel`, `announceRoleDecided`.
- Produces: `game.LoverIDs [2]string` — read by Task 5's lover-death chain.

Spec §4h: DMed once, only on round 1, to the Cupid player, with a select menu allowing exactly 2 picks at once (`MinValues`/`MaxValues` both 2 — Discord's multi-select, simpler than the reference's sequential click-twice UX and equivalent in outcome). On confirm, both lovers are DMed who their partner is (a deliberate deviation from the reference's "secret" house rule, per an earlier decision in this project). Cupid's action is **not** gated by `nightActionsComplete`/AFK strikes (the spec's per-round "wait for all of the above" list at section 4 never mentions Cupid, unlike its explicit callout of Hunter/Disruptor) — a Cupid who never pairs anyone simply means no lover-death chain exists this game, with no AFK consequence.

- [ ] **Step 1: Add game state**

In `internal/discord/commands/werewolf.go`, add to `werewolfGame` (next to `DisruptorTarget`):

```go
	LoverIDs [2]string // set once by Cupid on round 1; ["", ""] means no pairing was made
```

- [ ] **Step 2: Add the custom ID, select-menu builder, DM, and handler**

In `internal/discord/commands/werewolf_night.go`, add to the `const` block:

```go
	werewolfCupidPairID = "ww:cupidpair:"
```

Add a small dedicated 2-of-N select-menu builder right after `targetSelectMenu` (Cupid's is the only multi-select in this file, so it doesn't reuse `targetSelectMenu`, which is hardcoded to `MaxValues: 1`):

```go
// cupidPairSelectMenu builds Cupid's round-1 pairing select — a single
// multi-select requiring exactly 2 picks, simpler than the reference's
// sequential click-twice UX and equivalent in outcome.
func cupidPairSelectMenu(customID, placeholder string, targets []*player) []discordgo.MessageComponent {
	options := make([]discordgo.SelectMenuOption, 0, len(targets))
	for _, p := range targets {
		options = append(options, discordgo.SelectMenuOption{
			Label: truncateLabel(p.Label),
			Value: p.UserID,
		})
	}
	minValues := 2
	return []discordgo.MessageComponent{
		discordgo.ActionsRow{
			Components: []discordgo.MessageComponent{
				discordgo.SelectMenu{
					MenuType:    discordgo.StringSelectMenu,
					CustomID:    customID,
					Placeholder: placeholder,
					Options:     options,
					MinValues:   &minValues,
					MaxValues:   2,
				},
			},
		},
	}
}

func (cmd *WerewolfCommand) dmCupidPrompt(s *discordgo.Session, channelID string, cupid *player, targets []*player) {
	dm, err := s.UserChannelCreate(cupid.UserID)
	if err != nil {
		log.Error().Err(err).Msg("werewolf: failed to open DM for cupid")
		return
	}
	if _, err := s.ChannelMessageSendComplex(dm.ID, &discordgo.MessageSend{
		Content:    "💘 Chọn đúng 2 người để ghép thành một cặp đôi. Đây là lựa chọn một lần duy nhất, không thể đổi sau khi xác nhận.",
		Components: cupidPairSelectMenu(werewolfCupidPairID+channelID, "Chọn 2 người", targets),
	}); err != nil {
		log.Error().Err(err).Msg("werewolf: failed to DM cupid prompt")
	}
}

func (cmd *WerewolfCommand) handleCupidPair(s *discordgo.Session, i *discordgo.InteractionCreate, channelID string) {
	values := i.MessageComponentData().Values
	if len(values) != 2 {
		respondComponentEphemeral(s, i, "Phải chọn đúng 2 người.")
		return
	}
	clickerID := interactionUserID(i)

	cmd.mu.Lock()
	game, ok := cmd.games[channelID]
	if !ok || game.Phase != phaseNight || game.Round != 1 {
		cmd.mu.Unlock()
		respondComponentEphemeral(s, i, "Đêm nay đã kết thúc rồi.")
		return
	}
	clicker, isCupid := game.Players[clickerID]
	if !isCupid || clicker.Role != roleCupid || !clicker.Alive {
		cmd.mu.Unlock()
		respondComponentEphemeral(s, i, "Chỉ Thần Tình Yêu còn sống mới được ghép đôi.")
		return
	}
	if game.LoverIDs[0] != "" || game.LoverIDs[1] != "" {
		cmd.mu.Unlock()
		respondComponentEphemeral(s, i, "Bạn đã chọn cặp đôi rồi.")
		return
	}
	target1, exists1 := game.Players[values[0]]
	target2, exists2 := game.Players[values[1]]
	if !exists1 || !target1.Alive || !exists2 || !target2.Alive || values[0] == values[1] {
		cmd.mu.Unlock()
		respondComponentEphemeral(s, i, "Mục tiêu không còn hợp lệ.")
		return
	}
	game.LoverIDs = [2]string{values[0], values[1]}
	label1, label2 := target1.Label, target2.Label
	id1, id2 := values[0], values[1]
	cmd.mu.Unlock()

	respondComponentEphemeral(s, i, fmt.Sprintf("💘 Đã ghép: %s ❤️ %s.", label1, label2))
	announceRoleDecided(s, channelID, "💘 Thần Tình Yêu")

	for _, pair := range []struct{ toID, partnerLabel string }{
		{id1, label2},
		{id2, label1},
	} {
		dm, err := s.UserChannelCreate(pair.toID)
		if err != nil {
			log.Error().Err(err).Str("user_id", pair.toID).Msg("werewolf: failed to open DM for lover reveal")
			continue
		}
		if _, err := s.ChannelMessageSend(dm.ID, fmt.Sprintf("💘 Thần Tình Yêu đã ghép bạn với **%s**. Nếu một trong hai người chết, người còn lại cũng chết theo.", pair.partnerLabel)); err != nil {
			log.Error().Err(err).Str("user_id", pair.toID).Msg("werewolf: failed to DM lover reveal")
		}
	}
}
```

- [ ] **Step 3: Wire into `HandleComponent`**

In `internal/discord/commands/werewolf.go`, `HandleComponent`, add:

```go
	case strings.HasPrefix(customID, werewolfCupidPairID):
		cmd.handleCupidPair(s, i, strings.TrimPrefix(customID, werewolfCupidPairID))
```

- [ ] **Step 4: Wire into `runNightPhase`, round 1 only**

In `internal/discord/commands/werewolf_night.go`, `runNightPhase`'s `var (...)` block, add:

```go
		cupid        *player
		cupidTargets []*player
```

Inside the locked closure, right after the existing Disruptor snapshot block, add (note this reads `game.Round` *after* it was incremented earlier in this same closure, so `== 1` correctly means "this is the first night"):

```go
		if game.Round == 1 {
			if cupids := alivePlayersOfRole(game, roleCupid); len(cupids) > 0 {
				cupid = copyPlayer(cupids[0])
				cupidTargets = copyPlayers(aliveExcept(game, cupid.UserID))
			}
		}
```

After the unlock, right after the Disruptor DM call, add:

```go
	if cupid != nil {
		cmd.dmCupidPrompt(s, channelID, cupid, cupidTargets)
	}
```

- [ ] **Step 5: Run tests, verify, and commit**

Run: `go build ./... && go vet ./internal/discord/commands/... && go test ./internal/discord/commands/...`

Expected: all pass. No pre-existing test has a `roleCupid` player, so `runNightPhase`'s new round-1 block never fires for any of them.

```bash
gofmt -l internal/discord/commands/werewolf.go internal/discord/commands/werewolf_night.go
git add internal/discord/commands/werewolf.go internal/discord/commands/werewolf_night.go
git commit -m "werewolf: add Cupid — round-1 pairing, both lovers informed"
```

---

### Task 5: Unify Hunter-revenge and lover-death into one chain resolver

**Files:**
- Modify: `internal/discord/commands/werewolf_night.go` (`applyHunterRevengeChain` split, new `applyDeathChains`)
- Modify: `internal/discord/commands/werewolf.go` (`roundLog`, `handleLeave`)
- Modify: `internal/discord/commands/werewolf_day.go` (`resolveVote`, `checkAndHandleGameEnd`, `formatRoundBlock`)
- Test: `internal/discord/commands/werewolf_test.go`

**Interfaces:**
- Consumes: `applyHunterRevengeChain` (Plan B) — refactored in place; every existing caller of it is replaced with the new `applyDeathChains` in this task, so `applyHunterRevengeChain`'s own exported behavior and existing tests (`TestApplyHunterRevengeChain*`) must stay unchanged.
- Produces: `hunterRevengeStep(game *werewolfGame, deadID string) *player` (single-hop primitive), `applyLoverDeathChain(game *werewolfGame, deadID string) *player` (single-hop, "one level deep" per spec §4h), `chainedDeath{Player *player, Reason chainedDeathReason}`, `applyDeathChains(game *werewolfGame, deadID string) []chainedDeath`, `roundLog.LoverDeathLabel string`, `loverDeathLine(p *player) string`.

Spec §4h's lover-death chain fires at the same four sites `applyHunterRevengeChain` already fires at (lynch, wolf-kill/poison/AFK/leave paths). Since a lover-killed player can themselves be a Hunter with a locked-in target (and a Hunter-revenge-killed player can themselves have a living lover), the two chains can compose — this task unifies them into one resolver instead of leaving two independent single-hop checks that would only catch one hop of a two-mechanic interaction.

- [ ] **Step 1: Split `applyHunterRevengeChain` into a single-hop primitive plus a thin looping wrapper**

In `internal/discord/commands/werewolf_night.go`, `applyHunterRevengeChain` currently does its own internal BFS loop. Extract the per-player check into a new single-hop function, and make the existing function a wrapper over it — this preserves `applyHunterRevengeChain`'s existing exported signature and behavior (and its existing tests) exactly, while giving `applyDeathChains` (Step 2) a primitive it can call without double-looping:

```go
// hunterRevengeStep checks whether the player who just died (deadID) was
// the Hunter with a locked-in target; if so, kills that target too
// (unless already dead) and returns a copy of them. Returns nil if no
// revenge-kill happens. Single-hop — applyHunterRevengeChain loops this
// for its own standalone callers; applyDeathChains (below) uses this
// directly as one of the two chain types it composes. Must be called
// with cmd.mu held.
func hunterRevengeStep(game *werewolfGame, deadID string) *player {
	dead, ok := game.Players[deadID]
	if !ok || dead.Role != roleHunter {
		return nil
	}
	targetID := game.HunterTarget
	if targetID == "" {
		return nil
	}
	target, exists := game.Players[targetID]
	if !exists || !target.Alive {
		return nil
	}
	target.Alive = false
	return copyPlayer(target)
}

// applyHunterRevengeChain loops hunterRevengeStep starting from deadID.
// Note: game.HunterTarget is a single game-scoped field, not per-Hunter,
// so if a second Hunter were ever dealt (not possible with today's ratio
// tables, which deal exactly one), this cannot produce a genuine
// multi-Hunter chain — it would resolve the same shared target twice and
// safely no-op the second time via the already-dead guard, never
// mis-killing, but also never revenging more than once per invocation.
// Must be called with cmd.mu held. Returns value-copy snapshots of
// everyone killed, safe to read after unlocking.
func applyHunterRevengeChain(game *werewolfGame, deadID string) []*player {
	var revenged []*player
	queue := []string{deadID}
	for len(queue) > 0 {
		id := queue[0]
		queue = queue[1:]
		target := hunterRevengeStep(game, id)
		if target == nil {
			continue
		}
		revenged = append(revenged, target)
		queue = append(queue, target.UserID)
	}
	return revenged
}
```

- [ ] **Step 2: Add the lover-death primitive and the unified `applyDeathChains`**

Right after `applyHunterRevengeChain`, add:

```go
// applyLoverDeathChain checks whether the player who just died (deadID)
// has a living lover via game.LoverIDs; if so, kills that lover too and
// returns a copy of them. Single-hop by design (spec §4h: "chains only
// one level deep" — LoverIDs only ever names the original two players, so
// there is nothing further to chain even if this were looped). Must be
// called with cmd.mu held.
func applyLoverDeathChain(game *werewolfGame, deadID string) *player {
	var partnerID string
	switch deadID {
	case game.LoverIDs[0]:
		partnerID = game.LoverIDs[1]
	case game.LoverIDs[1]:
		partnerID = game.LoverIDs[0]
	default:
		return nil
	}
	if partnerID == "" {
		return nil
	}
	partner, exists := game.Players[partnerID]
	if !exists || !partner.Alive {
		return nil
	}
	partner.Alive = false
	return copyPlayer(partner)
}

type chainedDeathReason int

const (
	chainedDeathHunterRevenge chainedDeathReason = iota
	chainedDeathLover
)

// chainedDeath pairs a copy-snapshot of a chain-killed player with why
// they died, so callers can render the right recap/announcement line.
type chainedDeath struct {
	Player *player
	Reason chainedDeathReason
}

// applyDeathChains resolves every secondary death cascading from deadID:
// Hunter's revenge-kill and Cupid's lover-death, repeating until a pass
// produces no new deaths. A revenge-killed or lover-killed player can
// themselves be a Hunter with a locked target, or have a living lover, so
// both chain types must be checked at every hop — this replaces every
// direct call to applyHunterRevengeChain in the death-application sites
// (resolveVote, checkAndHandleGameEnd, handleLeave, finalizeNightResolution)
// with this unified resolver. Must be called with cmd.mu held.
func applyDeathChains(game *werewolfGame, deadID string) []chainedDeath {
	var result []chainedDeath
	seen := map[string]bool{deadID: true}
	queue := []string{deadID}
	for len(queue) > 0 {
		id := queue[0]
		queue = queue[1:]
		if hp := hunterRevengeStep(game, id); hp != nil && !seen[hp.UserID] {
			seen[hp.UserID] = true
			result = append(result, chainedDeath{Player: hp, Reason: chainedDeathHunterRevenge})
			queue = append(queue, hp.UserID)
		}
		if lp := applyLoverDeathChain(game, id); lp != nil && !seen[lp.UserID] {
			seen[lp.UserID] = true
			result = append(result, chainedDeath{Player: lp, Reason: chainedDeathLover})
			queue = append(queue, lp.UserID)
		}
	}
	return result
}

// loverDeathLine renders the shared announcement line for a lover-death,
// mirroring hunterRevengeLine's role in the same call sites.
func loverDeathLine(p *player) string {
	return fmt.Sprintf("💔 **%s** cũng chết theo vì mất người yêu.", p.Label)
}
```

- [ ] **Step 3: Add the recap field**

In `internal/discord/commands/werewolf.go`, add to `roundLog` (next to `HunterRevengeLabel`):

```go
	LoverDeathLabel string // who died from the lover-chain this round, "" if it didn't trigger
```

- [ ] **Step 4: Replace every direct `applyHunterRevengeChain` call site with `applyDeathChains`**

In `internal/discord/commands/werewolf.go`, `handleLeave`, replace:

```go
	hunterRevenged := applyHunterRevengeChain(game, userID)
	applyWolfCubBonusBiteFlag(game, append([]string{userID}, idsOf(hunterRevenged)...))
	if len(hunterRevenged) > 0 && len(game.History) > 0 {
		game.History[len(game.History)-1].HunterRevengeLabel = hunterRevenged[0].Label
	}
```

with:

```go
	chained := applyDeathChains(game, userID)
	hunterRevenged, loverDeaths := splitChainedDeaths(chained)
	applyWolfCubBonusBiteFlag(game, append([]string{userID}, idsOf(hunterRevenged)...))
	if n := len(game.History); n > 0 {
		if len(hunterRevenged) > 0 {
			game.History[n-1].HunterRevengeLabel = hunterRevenged[0].Label
		}
		if len(loverDeaths) > 0 {
			game.History[n-1].LoverDeathLabel = loverDeaths[0].Label
		}
	}
```

Then, further down in the same function, find the announcement-building loop:

```go
	for _, hp := range hunterRevenged {
		msg += "\n" + hunterRevengeLine(hp)
	}
```

and add right after it:

```go
	for _, lp := range loverDeaths {
		msg += "\n" + loverDeathLine(lp)
	}
```

Add the small splitter helper next to `applyDeathChains` in `werewolf_night.go` (used by all four call sites in this task):

```go
// splitChainedDeaths partitions applyDeathChains's result back into the
// two announcement/recap shapes the existing call sites already expect
// (hunterRevengeLine vs loverDeathLine, HunterRevengeLabel vs
// LoverDeathLabel).
func splitChainedDeaths(chained []chainedDeath) (hunterRevenged, loverDeaths []*player) {
	for _, c := range chained {
		switch c.Reason {
		case chainedDeathHunterRevenge:
			hunterRevenged = append(hunterRevenged, c.Player)
		case chainedDeathLover:
			loverDeaths = append(loverDeaths, c.Player)
		}
	}
	return hunterRevenged, loverDeaths
}
```

Apply the analogous replacement in `internal/discord/commands/werewolf_day.go`'s `resolveVote` (the `applyHunterRevengeChain(game, lynchedPlayer.UserID)` call and its `entry.HunterRevengeLabel` write, plus the `for _, p := range hunterRevenged { resultMsg += "\n" + hunterRevengeLine(p) }` loop — mirror the `handleLeave` pattern above exactly, including a `for _, p := range loverDeaths { resultMsg += "\n" + loverDeathLine(p) }` loop and the `removeFromAliveThread` loop extended to cover `loverDeaths` too, same as it already does for `hunterRevenged`).

Apply the analogous replacement in `checkAndHandleGameEnd` (the loop calling `applyHunterRevengeChain(game, id)` for each `removedIDs` entry — change to `applyDeathChains`, accumulate into a combined `chained []chainedDeath` slice across all removed players, then `splitChainedDeaths` once at the end; extend `announceHunterRevenge`'s call site with an equivalent `announceLoverDeaths` for `loverDeaths`, or fold both into one combined announcer — either is fine as long as both message types render).

Apply the analogous replacement in `internal/discord/commands/werewolf_night.go`'s `finalizeNightResolution` (the two `applyHunterRevengeChain` calls — one for `killedIDs`, one for `poisonedID` — both become `applyDeathChains` calls whose results get accumulated into one `chained []chainedDeath` slice via `append`, then split once with `splitChainedDeaths` before building `deadThisResolution`/the recap/the result message; extend the `for _, p := range hunterRevenged { resultMsg += "\n" + hunterRevengeLine(p) }` loop at the end of the result-message building with an equivalent loop for `loverDeaths`/`loverDeathLine`).

- [ ] **Step 5: Add the recap line**

In `internal/discord/commands/werewolf_day.go`, `formatRoundBlock`, add next to the existing `HunterRevengeLabel` line:

```go
	if r.LoverDeathLabel != "" {
		fmt.Fprintf(&b, "\n💔 Người yêu qua đời theo: %s", r.LoverDeathLabel)
	}
```

- [ ] **Step 6: Write the tests**

Add to `internal/discord/commands/werewolf_test.go`:

```go
func TestApplyDeathChainsLoverOnly(t *testing.T) {
	game := &werewolfGame{
		LoverIDs: [2]string{"a", "b"},
		Players: map[string]*player{
			"a": {UserID: "a", Label: "A", Alive: false}, // just died
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
			"h": {UserID: "h", Label: "Hunter", Role: roleHunter, Alive: false}, // just died
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

func TestFormatRoundBlockLoverDeath(t *testing.T) {
	r := roundLog{Round: 3, LoverDeathLabel: "Lam"}
	got := formatRoundBlock(r)
	if !strings.Contains(got, "Lam") {
		t.Errorf("formatRoundBlock() = %q, want the lover-death mentioned", got)
	}
}
```

- [ ] **Step 7: Run the full suite, verify, and commit**

Run: `go build ./... && go vet ./internal/discord/commands/... && go test ./internal/discord/commands/...`

Expected: all pass, including every pre-existing `TestApplyHunterRevengeChain*` test (unaffected — `applyHunterRevengeChain`'s own behavior and signature are unchanged, only its internals now delegate to `hunterRevengeStep`) and every existing test at every one of the four now-`applyDeathChains`-routed call sites (none of them set `LoverIDs`, so `applyLoverDeathChain` never fires for any of them — `applyDeathChains` degenerates to exactly `applyHunterRevengeChain`'s old behavior whenever no lovers exist).

```bash
gofmt -l internal/discord/commands/werewolf.go internal/discord/commands/werewolf_night.go internal/discord/commands/werewolf_day.go internal/discord/commands/werewolf_test.go
git add internal/discord/commands/werewolf.go internal/discord/commands/werewolf_night.go internal/discord/commands/werewolf_day.go internal/discord/commands/werewolf_test.go
git commit -m "werewolf: unify Hunter-revenge and lover-death into applyDeathChains"
```

---

### Task 6: Verification (mixed — Disruptor is live-testable, Cupid isn't)

**Files:** none (verification only)

Unlike Cupid, **Disruptor is reachable today** — it enters the fixed table at 15+ players (Task 1). Hand the Disruptor half of this checklist to the user as a real Discord playtest. Cupid stays unreachable until Plan E, same as Plan C's three roles — flag that half as review-only, same as Plan C's Task 6.

Checklist to hand over:

- [ ] Run a 15+ player game. Confirm the role-assigned announcement's breakdown includes "🤫 Kẻ Phá Hoại: 1" and the villager count is correct (not off by one from before this plan).
- [ ] Confirm the Disruptor gets a nightly DM target-select, can change their pick within the same night without re-triggering the "✅ Kẻ Phá Hoại đã quyết định xong." notification, and accrues an AFK strike if they never pick a round.
- [ ] Confirm the player the Disruptor picked cannot send messages in the alive-discussion thread during the *next* day-discuss window, but everyone else can — and that the mute is gone again once voting starts (or, if it's the same target back-to-back rounds, that the mute correctly re-applies for a second round).
- [ ] Run a full game to completion and check the recap shows "🤫 Kẻ Phá Hoại làm câm lặng: X" for every round the Disruptor acted in.
- [ ] Skim `internal/discord/commands/werewolf_test.go` for the new Cupid/lover-chain tests (`TestDealRolesAssignsCupid`, `TestApplyDeathChainsLoverOnly`, `TestApplyDeathChainsComposesHunterAndLover`, `TestFormatRoundBlockLoverDeath`) and confirm they read as genuine behavioral checks.
- [ ] Confirm `go test ./internal/discord/commands/... -v` passes in full, including every pre-existing test — nothing in this plan should have altered any pre-existing test's expected outcome.
- [ ] When Plan E ships and Cupid becomes reachable via the manual role-pick UI, re-open this checklist as a real Discord playtest for Cupid specifically: verify the round-1-only DM, the exactly-2-picks select menu, both lovers getting informed who their partner is, and a lover-death firing correctly from every one of the four death-application sites (including composing correctly with a Hunter-revenge death, per `TestApplyDeathChainsComposesHunterAndLover`).

---

## Self-Review

**Spec coverage:** §4g (Disruptor) → Tasks 1-2. §4h (Cupid + lover-death chain) → Tasks 3-5. §4i (progress notifications for Disruptor/Cupid) → wired directly inside Tasks 2/4 (first-pick-this-round for Disruptor, round-1-confirm-only for Cupid), matching how Plan C added brand-new roles' notifications inline rather than as a separate catch-up task (Plan B's Task 4 catch-up was only needed because Plan A shipped *before* `announceRoleDecided` existed). §5's `DisruptorMutedLabel`/`LoverDeathLabel` recap fields → Tasks 2 + 5. §3a's "Disruptor is in the fixed table, Cupid never is" → Task 1 (extends the table) vs. Task 3 (deliberately doesn't).

**Out of this plan's scope, confirmed correctly deferred:** §3b (manual-pick UI — Plan E, is what will actually make Cupid reachable), §6 (`/help werewolf` guide — Plan E).

**Carried forward to Plan E's brief** (mirroring Plan C's carry-forward list): Cupid unreachable until the manual-pick UI ships; the role-count announcement fixed in Task 1 will need to switch from re-deriving via `rolesForPlayerCount` to counting the actually-dealt roster once a manual-pick game can bypass the fixed table entirely (same item Plan C already flagged).
