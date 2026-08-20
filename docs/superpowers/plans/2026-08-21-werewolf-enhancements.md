# Werewolf Enhancements Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add six enhancements to the live `/werewolf` feature: bot-only main channel, a private alive-players discussion thread, alive/dead reminders on every phase transition, an explicit skip-vote option, reworked (generally faster, wolf-count-scaled) timers, and a full round-by-round history recap at game end.

**Architecture:** All changes are additive to the existing `werewolfGame` struct and its phase functions (`internal/discord/commands/werewolf.go`, `werewolf_night.go`, `werewolf_day.go`). No new files, no new commands, no database changes — the feature stays entirely in-memory. New Discord permission dependency: the bot needs `Manage Channels` in addition to the already-required `Manage Threads`.

**Tech Stack:** Go, discordgo v0.29.0, existing `sync.Mutex`-guarded `map[string]*werewolfGame` pattern.

## Global Constraints

- Every terminal path (win, lobby cancel, panic-recovery in `startPreflight`) must clean up **all** new state this plan adds: the channel permission overwrite, the alive-discussion thread, and (implicitly, via `delete(cmd.games, ...)`) `game.History`. See spec section "Cleanup on every terminal path."
- Every `discordgo.MessageSend`/`InteractionResponseData` whose `Content` contains a `<@id>` mention must set `AllowedMentions` explicitly (existing project-wide rule — see `pingOnce`/`respondComponentEphemeralMentioned` in the existing code).
- No DB/migration changes — werewolf remains fully in-memory per the original design.
- Follow the existing code's Vietnamese-language player-facing strings; log messages stay in English (matches current file conventions).
- Reference spec: `docs/superpowers/specs/2026-08-21-werewolf-enhancements-design.md`.
- After editing any file in a task, run `gofmt -l <file>` on every file you touched in that task; if it lists the file, run `gofmt -w <file>` before that task's final build/test step. (Struct-literal and `var`-block alignment shown in this plan's code blocks is illustrative, not gofmt-exact — gofmt reformats it automatically.)

---

### Task 1: New phase timings, including wolf-count-scaled night window

**Files:**
- Modify: `internal/discord/commands/werewolf_day.go:12-26` (constants + `dayDiscussWindowFor`)
- Modify: `internal/discord/commands/werewolf_night.go:11-16` (constants), `werewolf_night.go:83-156` (`runNightPhase`), `werewolf_night.go:261-295` (`waitForNightActions`)
- Test: `internal/discord/commands/werewolf_test.go`

**Interfaces:**
- Produces: `wolfDiscussWindowFor(wolfCount int) time.Duration` (new, in `werewolf_night.go`), used only within this task.
- Modifies: `nightActionWindow` now means "solo (seer/doctor) window" (60s, was 90s). `dayDiscussBase` (45s, was 90s), `dayDiscussPerAlive` (15s, was 30s), `dayVoteWindow` (30s, was 90s).
- `waitForNightActions(s *discordgo.Session, channelID string, deadline time.Time, totalWindow time.Duration, seq uint64)` — signature gains a `totalWindow time.Duration` parameter (previously used the `nightActionWindow` constant directly for its halfway-reminder calculation; now the caller passes the actual window length, which may be longer than the solo window when wolves are present).

- [ ] **Step 1: Update day-phase timing constants**

In `internal/discord/commands/werewolf_day.go`, replace:

```go
const (
	werewolfVoteID = "ww:vote:"
	dayVoteWindow  = 90 * time.Second

	dayDiscussBase     = 90 * time.Second
	dayDiscussPerAlive = 30 * time.Second
)
```

with:

```go
const (
	werewolfVoteID = "ww:vote:"
	dayVoteWindow  = 30 * time.Second

	dayDiscussBase     = 45 * time.Second
	dayDiscussPerAlive = 15 * time.Second
)
```

- [ ] **Step 2: Add a unit test for `dayDiscussWindowFor`'s new values**

`dayDiscussWindowFor` already exists and is unchanged in logic — only its constants moved. Add this test to `internal/discord/commands/werewolf_test.go` (append at the end of the file):

```go
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
```

Add `"time"` to the test file's imports (currently only imports `"testing"`):

```go
import (
	"testing"
	"time"
)
```

- [ ] **Step 3: Run the new test to verify it passes**

Run: `go test ./internal/discord/commands/... -run TestDayDiscussWindowFor -v`
Expected: `PASS`

- [ ] **Step 4: Update night-phase timing constants and add the wolf-scaling function**

In `internal/discord/commands/werewolf_night.go`, replace:

```go
const (
	werewolfWolfTargetID    = "ww:wolftarget:"
	werewolfSeerCheckID     = "ww:seercheck:"
	werewolfDoctorProtectID = "ww:doctorprotect:"
	nightActionWindow       = 90 * time.Second
)
```

with:

```go
const (
	werewolfWolfTargetID    = "ww:wolftarget:"
	werewolfSeerCheckID     = "ww:seercheck:"
	werewolfDoctorProtectID = "ww:doctorprotect:"

	// nightActionWindow is the solo window for the Seer and Doctor, who each
	// act alone. Wolves get their own, longer window — see
	// wolfDiscussWindowFor — since they coordinate as a group in a shared
	// thread and benefit from more time as their numbers grow.
	nightActionWindow = 60 * time.Second

	wolfDiscussBase    = 60 * time.Second
	wolfDiscussPerWolf = 20 * time.Second
)

// wolfDiscussWindowFor returns the night-discussion window for the wolves'
// private thread, given how many wolves are alive this round.
func wolfDiscussWindowFor(wolfCount int) time.Duration {
	return wolfDiscussBase + wolfDiscussPerWolf*time.Duration(wolfCount)
}
```

- [ ] **Step 5: Add a unit test for `wolfDiscussWindowFor`**

Append to `internal/discord/commands/werewolf_test.go`:

```go
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
```

- [ ] **Step 6: Run both new tests to verify they pass**

Run: `go test ./internal/discord/commands/... -run 'TestDayDiscussWindowFor|TestWolfDiscussWindowFor' -v`
Expected: both `PASS`

- [ ] **Step 7: Compute the effective night deadline as the max of the solo and wolf windows**

In `internal/discord/commands/werewolf_night.go`, `runNightPhase` currently ends with:

```go
	cmd.startWolfThread(s, channelID, round, wolves, nonWolfTargets, seq)
	if seer != nil {
		cmd.dmSeerPrompt(s, channelID, seer, seerTargets)
	}
	if doctor != nil {
		cmd.dmDoctorPrompt(s, channelID, doctor, doctorTargets)
	}

	deadline := time.Now().Add(nightActionWindow)
	cmd.waitForNightActions(s, channelID, deadline, seq)

	cmd.resolveNight(s, channelID, seq)
}
```

Replace the last three lines (from `deadline :=` to the `waitForNightActions` call) with:

```go
	totalWindow := nightActionWindow
	if w := wolfDiscussWindowFor(len(wolves)); len(wolves) > 0 && w > totalWindow {
		totalWindow = w
	}
	deadline := time.Now().Add(totalWindow)
	cmd.waitForNightActions(s, channelID, deadline, totalWindow, seq)

	cmd.resolveNight(s, channelID, seq)
}
```

- [ ] **Step 8: Update `waitForNightActions` to take the actual window length instead of the constant**

In `internal/discord/commands/werewolf_night.go`, replace:

```go
func (cmd *WerewolfCommand) waitForNightActions(s *discordgo.Session, channelID string, deadline time.Time, seq uint64) {
	reminded := false
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
			done = cmd.nightActionsComplete(game)
		}()
		if !gameFound || done || time.Now().After(deadline) {
			return
		}

		remaining := time.Until(deadline)
		if !reminded && remaining <= nightActionWindow/2 {
			reminded = true
			if _, err := s.ChannelMessageSendComplex(channelID, &discordgo.MessageSend{
				Content:         fmt.Sprintf("⏳ Còn %s để mọi người hành động đêm nay.", formatRemaining(remaining)),
				AllowedMentions: &discordgo.MessageAllowedMentions{},
			}); err != nil {
				log.Error().Err(err).Msg("werewolf: failed to post night reminder")
			}
		}

		time.Sleep(2 * time.Second)
	}
}
```

with:

```go
func (cmd *WerewolfCommand) waitForNightActions(s *discordgo.Session, channelID string, deadline time.Time, totalWindow time.Duration, seq uint64) {
	reminded := false
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
			done = cmd.nightActionsComplete(game)
		}()
		if !gameFound || done || time.Now().After(deadline) {
			return
		}

		remaining := time.Until(deadline)
		if !reminded && remaining <= totalWindow/2 {
			reminded = true
			if _, err := s.ChannelMessageSendComplex(channelID, &discordgo.MessageSend{
				Content:         fmt.Sprintf("⏳ Còn %s để mọi người hành động đêm nay.", formatRemaining(remaining)),
				AllowedMentions: &discordgo.MessageAllowedMentions{},
			}); err != nil {
				log.Error().Err(err).Msg("werewolf: failed to post night reminder")
			}
		}

		time.Sleep(2 * time.Second)
	}
}
```

- [ ] **Step 9: Build and run the full package test suite**

Run: `go build ./... && go vet ./internal/discord/commands/... && go test ./internal/discord/commands/... -v`
Expected: build succeeds, all tests `PASS` (including the two new ones).

- [ ] **Step 10: Commit**

```bash
git add internal/discord/commands/werewolf_day.go internal/discord/commands/werewolf_night.go internal/discord/commands/werewolf_test.go
git commit -m "werewolf: rework phase timings, add wolf-count-scaled night window

Night (seer/doctor solo): 90s -> 60s
Day discuss: 90s+30s/alive -> 45s+15s/alive
Day vote: 90s -> 30s
New: wolves get their own window in their private thread, 60s+20s/wolf,
replacing the flat solo window just for them."
```

---

### Task 2: Skip-vote option

**Files:**
- Modify: `internal/discord/commands/werewolf_day.go:75-109` (`runDayVote`), `werewolf_day.go:296-326` (`handleVote`)
- Test: `internal/discord/commands/werewolf_test.go`

**Interfaces:**
- Produces: `voteSkipValue = "__skip__"` (constant), `voteSkipLabel = "Không treo cổ ai"` (constant), `voteSelectMenu(customID, placeholder string, targets []*player) []discordgo.MessageComponent` (new function in `werewolf_day.go`) — Task 5 (history wiring) reads `voteSkipValue`/`voteSkipLabel` to render the vote tally.
- Consumes: `targetSelectMenu` (existing, in `werewolf_night.go`) is used as a reference for `voteSelectMenu`'s shape but is not modified — the night-action menus (wolf/seer/doctor) do not get a skip option.

- [ ] **Step 1: Add the skip constants and a vote-specific select menu builder**

In `internal/discord/commands/werewolf_day.go`, add after the existing `const` block (after `dayDiscussWindowFor`'s definition, before `runDayDiscuss`):

```go
const (
	voteSkipValue = "__skip__"
	voteSkipLabel = "Không treo cổ ai"
)

// voteSelectMenu is targetSelectMenu's day-vote-specific counterpart: same
// shape, plus one extra "skip" option so players can deliberately abstain
// from lynching anyone instead of only reaching "nobody lynched" via a
// genuine tie.
func voteSelectMenu(customID, placeholder string, targets []*player) []discordgo.MessageComponent {
	options := make([]discordgo.SelectMenuOption, 0, len(targets)+1)
	for _, p := range targets {
		options = append(options, discordgo.SelectMenuOption{
			Label: truncateLabel(p.Label),
			Value: p.UserID,
		})
	}
	options = append(options, discordgo.SelectMenuOption{
		Label: voteSkipLabel,
		Value: voteSkipValue,
	})
	minValues := 1
	return []discordgo.MessageComponent{
		discordgo.ActionsRow{
			Components: []discordgo.MessageComponent{
				discordgo.SelectMenu{
					MenuType:    discordgo.StringSelectMenu,
					CustomID:    customID,
					Placeholder: placeholder,
					Options:     options,
					MinValues:   &minValues,
					MaxValues:   1,
				},
			},
		},
	}
}
```

- [ ] **Step 2: Use `voteSelectMenu` instead of `targetSelectMenu` in `runDayVote`**

In `internal/discord/commands/werewolf_day.go`, in `runDayVote`, replace:

```go
	if _, err := s.ChannelMessageSendComplex(channelID, &discordgo.MessageSend{
		Content:    "🗳️ **Bỏ phiếu treo cổ.** Chọn người bạn nghi ngờ.",
		Components: targetSelectMenu(werewolfVoteID+channelID, "Chọn người để treo cổ", alive),
	}); err != nil {
```

with:

```go
	if _, err := s.ChannelMessageSendComplex(channelID, &discordgo.MessageSend{
		Content:    "🗳️ **Bỏ phiếu treo cổ.** Chọn người bạn nghi ngờ, hoặc bỏ qua.",
		Components: voteSelectMenu(werewolfVoteID+channelID, "Chọn người để treo cổ", alive),
	}); err != nil {
```

- [ ] **Step 3: Special-case the skip sentinel in `handleVote`**

In `internal/discord/commands/werewolf_day.go`, replace `handleVote`'s body:

```go
func (cmd *WerewolfCommand) handleVote(s *discordgo.Session, i *discordgo.InteractionCreate, channelID string) {
	values := i.MessageComponentData().Values
	if len(values) == 0 {
		return
	}
	targetID := values[0]
	voterID := interactionUserID(i)

	cmd.mu.Lock()
	game, ok := cmd.games[channelID]
	if !ok || game.Phase != phaseDayVote {
		cmd.mu.Unlock()
		respondComponentEphemeral(s, i, "Vòng bỏ phiếu đã kết thúc rồi.")
		return
	}
	voter, isPlayer := game.Players[voterID]
	if !isPlayer || !voter.Alive {
		cmd.mu.Unlock()
		respondComponentEphemeral(s, i, "Chỉ người chơi còn sống mới được bỏ phiếu.")
		return
	}
	if target, exists := game.Players[targetID]; !exists || !target.Alive {
		cmd.mu.Unlock()
		respondComponentEphemeral(s, i, "Mục tiêu không còn hợp lệ.")
		return
	}
	game.Votes[voterID] = targetID
	cmd.mu.Unlock()

	respondComponentEphemeralMentioned(s, i, fmt.Sprintf("✅ Đã bỏ phiếu cho <@%s>.", targetID))
}
```

with:

```go
func (cmd *WerewolfCommand) handleVote(s *discordgo.Session, i *discordgo.InteractionCreate, channelID string) {
	values := i.MessageComponentData().Values
	if len(values) == 0 {
		return
	}
	targetID := values[0]
	voterID := interactionUserID(i)

	cmd.mu.Lock()
	game, ok := cmd.games[channelID]
	if !ok || game.Phase != phaseDayVote {
		cmd.mu.Unlock()
		respondComponentEphemeral(s, i, "Vòng bỏ phiếu đã kết thúc rồi.")
		return
	}
	voter, isPlayer := game.Players[voterID]
	if !isPlayer || !voter.Alive {
		cmd.mu.Unlock()
		respondComponentEphemeral(s, i, "Chỉ người chơi còn sống mới được bỏ phiếu.")
		return
	}
	if targetID != voteSkipValue {
		if target, exists := game.Players[targetID]; !exists || !target.Alive {
			cmd.mu.Unlock()
			respondComponentEphemeral(s, i, "Mục tiêu không còn hợp lệ.")
			return
		}
	}
	game.Votes[voterID] = targetID
	cmd.mu.Unlock()

	if targetID == voteSkipValue {
		respondComponentEphemeral(s, i, "✅ Đã bỏ phiếu: Không treo cổ ai.")
		return
	}
	respondComponentEphemeralMentioned(s, i, fmt.Sprintf("✅ Đã bỏ phiếu cho <@%s>.", targetID))
}
```

Note: `resolveVote`'s existing lookup `game.Players[top[0]]` (in `werewolf_day.go`) already returns `exists == false` for the `voteSkipValue` sentinel, since it's never a key in `game.Players` — the existing "nobody lynched" branch fires correctly with no changes needed there.

- [ ] **Step 4: Add a unit test confirming `tallyVotes` treats the skip sentinel like any other target**

Append to `internal/discord/commands/werewolf_test.go`:

```go
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
```

- [ ] **Step 5: Run the test to verify it passes**

Run: `go test ./internal/discord/commands/... -run TestTallyVotesSkipSentinelWins -v`
Expected: `PASS`

- [ ] **Step 6: Build and run the full package test suite**

Run: `go build ./... && go vet ./internal/discord/commands/... && go test ./internal/discord/commands/... -v`
Expected: build succeeds, all tests `PASS`.

- [ ] **Step 7: Commit**

```bash
git add internal/discord/commands/werewolf_day.go internal/discord/commands/werewolf_test.go
git commit -m "werewolf: add explicit skip-vote option to the day vote

Players can now deliberately vote 'Không treo cổ ai' instead of only
reaching a no-lynch outcome via a genuine tie."
```

---

### Task 3: Alive/dead reminder on every phase-transition announcement

**Files:**
- Modify: `internal/discord/commands/werewolf.go` (add `aliveDeadSummary` helper)
- Modify: `internal/discord/commands/werewolf_night.go:83-156` (`runNightPhase`), `werewolf_night.go:301-393` (`resolveNight`)
- Modify: `internal/discord/commands/werewolf_day.go:28-73` (`runDayDiscuss`), `werewolf_day.go:75-109` (`runDayVote`), `werewolf_day.go:131-194` (`resolveVote`)

**Interfaces:**
- Produces: `aliveDeadSummary(game *werewolfGame) string` (new, in `werewolf.go`) — must be called with `cmd.mu` held, since it reads `game.Players`/`game.JoinOrder`.

- [ ] **Step 1: Add `aliveDeadSummary` to `werewolf.go`**

Add this function to `internal/discord/commands/werewolf.go`, immediately after `werewolfLobbyContent` (before `werewolfLobbyButtons`):

```go
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
```

- [ ] **Step 2: Wire it into `runNightPhase`'s announcement**

In `internal/discord/commands/werewolf_night.go`, `runNightPhase` currently has this locked block:

```go
	var (
		gameFound      bool
		round          int
		wolves         []*player
		nonWolfTargets []*player
		seer           *player
		seerTargets    []*player
		doctor         *player
		doctorTargets  []*player
	)
	func() {
		cmd.mu.Lock()
		defer cmd.mu.Unlock()
		game := cmd.gameIfCurrent(channelID, seq)
		if game == nil {
			return
		}
		gameFound = true
		game.Phase = phaseNight
		game.Round++
		game.NightTarget = ""
		game.SeerActed = false
		game.DoctorProtect = ""

		round = game.Round
```

Change the `var` block to add `summary string`, and add one line after `round = game.Round`:

```go
	var (
		gameFound      bool
		round          int
		summary        string
		wolves         []*player
		nonWolfTargets []*player
		seer           *player
		seerTargets    []*player
		doctor         *player
		doctorTargets  []*player
	)
	func() {
		cmd.mu.Lock()
		defer cmd.mu.Unlock()
		game := cmd.gameIfCurrent(channelID, seq)
		if game == nil {
			return
		}
		gameFound = true
		game.Phase = phaseNight
		game.Round++
		game.NightTarget = ""
		game.SeerActed = false
		game.DoctorProtect = ""

		round = game.Round
		summary = aliveDeadSummary(game)
```

Then update the night-start announcement (still in `runNightPhase`, unchanged surrounding code otherwise):

```go
	if _, err := s.ChannelMessageSendComplex(channelID, &discordgo.MessageSend{
		Content:         fmt.Sprintf("🌙 **Đêm %d bắt đầu.** Ma Sói, Tiên Tri, Bác Sĩ đang hành động trong tin nhắn riêng...", round),
		AllowedMentions: &discordgo.MessageAllowedMentions{},
	}); err != nil {
```

becomes:

```go
	if _, err := s.ChannelMessageSendComplex(channelID, &discordgo.MessageSend{
		Content:         fmt.Sprintf("🌙 **Đêm %d bắt đầu.** Ma Sói, Tiên Tri, Bác Sĩ đang hành động trong tin nhắn riêng...\n\n%s", round, summary),
		AllowedMentions: &discordgo.MessageAllowedMentions{},
	}); err != nil {
```

- [ ] **Step 3: Wire it into `resolveNight`'s result announcement**

In `internal/discord/commands/werewolf_night.go`, `resolveNight`'s `var` block:

```go
	var (
		gameFound     bool
		threadID      string
		revealOnDeath bool
		killedPlayer  *player
		saved         bool
	)
```

becomes:

```go
	var (
		gameFound     bool
		threadID      string
		revealOnDeath bool
		killedPlayer  *player
		saved         bool
		summary       string
	)
```

Inside the locked `func()`, right before its closing `}()`, after the existing `revealOnDeath = game.RevealOnDeath` line, add:

```go
		revealOnDeath = game.RevealOnDeath
		summary = aliveDeadSummary(game)
		if killedID != "" {
```

(i.e. insert `summary = aliveDeadSummary(game)` between the existing `revealOnDeath = game.RevealOnDeath` line and the existing `if killedID != "" {` line — the surrounding lines are unchanged.)

Then update the message send at the bottom of `resolveNight`:

```go
	if _, err := s.ChannelMessageSendComplex(channelID, &discordgo.MessageSend{
		Content:         resultMsg,
		AllowedMentions: &discordgo.MessageAllowedMentions{},
	}); err != nil {
		log.Error().Err(err).Msg("werewolf: failed to post night result")
	}
```

becomes:

```go
	if _, err := s.ChannelMessageSendComplex(channelID, &discordgo.MessageSend{
		Content:         resultMsg + "\n\n" + summary,
		AllowedMentions: &discordgo.MessageAllowedMentions{},
	}); err != nil {
		log.Error().Err(err).Msg("werewolf: failed to post night result")
	}
```

- [ ] **Step 4: Wire it into `runDayDiscuss`'s announcement**

In `internal/discord/commands/werewolf_day.go`, `runDayDiscuss` currently has:

```go
	var gameFound bool
	var round int
	var aliveCount int
	func() {
		cmd.mu.Lock()
		defer cmd.mu.Unlock()
		game := cmd.gameIfCurrent(channelID, seq)
		if game == nil {
			return
		}
		gameFound = true
		game.Phase = phaseDayDiscuss
		round = game.Round
		for _, p := range game.Players {
			if p.Alive {
				aliveCount++
			}
		}
	}()
```

Add a `summary` variable and set it in the same locked block:

```go
	var gameFound bool
	var round int
	var aliveCount int
	var summary string
	func() {
		cmd.mu.Lock()
		defer cmd.mu.Unlock()
		game := cmd.gameIfCurrent(channelID, seq)
		if game == nil {
			return
		}
		gameFound = true
		game.Phase = phaseDayDiscuss
		round = game.Round
		for _, p := range game.Players {
			if p.Alive {
				aliveCount++
			}
		}
		summary = aliveDeadSummary(game)
	}()
```

Then update the day-discuss announcement:

```go
	if _, err := s.ChannelMessageSendComplex(channelID, &discordgo.MessageSend{
		Content:         fmt.Sprintf("☀️ **Ngày %d.** Thảo luận trong %s trước khi bỏ phiếu.", round, formatRemaining(discussWindow)),
		AllowedMentions: &discordgo.MessageAllowedMentions{},
	}); err != nil {
```

becomes:

```go
	if _, err := s.ChannelMessageSendComplex(channelID, &discordgo.MessageSend{
		Content:         fmt.Sprintf("☀️ **Ngày %d.** Thảo luận trong %s trước khi bỏ phiếu.\n\n%s", round, formatRemaining(discussWindow), summary),
		AllowedMentions: &discordgo.MessageAllowedMentions{},
	}); err != nil {
```

- [ ] **Step 5: Wire it into `runDayVote`'s announcement**

In `internal/discord/commands/werewolf_day.go`, `runDayVote`'s locked block:

```go
	var gameFound bool
	var alive []*player
	func() {
		cmd.mu.Lock()
		defer cmd.mu.Unlock()
		game := cmd.gameIfCurrent(channelID, seq)
		if game == nil {
			return
		}
		gameFound = true
		game.Phase = phaseDayVote
		game.Votes = make(map[string]string)
		for _, id := range game.JoinOrder {
			if p := game.Players[id]; p.Alive {
				alive = append(alive, p)
			}
		}
	}()
```

becomes:

```go
	var gameFound bool
	var alive []*player
	var summary string
	func() {
		cmd.mu.Lock()
		defer cmd.mu.Unlock()
		game := cmd.gameIfCurrent(channelID, seq)
		if game == nil {
			return
		}
		gameFound = true
		game.Phase = phaseDayVote
		game.Votes = make(map[string]string)
		for _, id := range game.JoinOrder {
			if p := game.Players[id]; p.Alive {
				alive = append(alive, p)
			}
		}
		summary = aliveDeadSummary(game)
	}()
```

Then update the vote-start announcement:

```go
	if _, err := s.ChannelMessageSendComplex(channelID, &discordgo.MessageSend{
		Content:    "🗳️ **Bỏ phiếu treo cổ.** Chọn người bạn nghi ngờ, hoặc bỏ qua.",
		Components: voteSelectMenu(werewolfVoteID+channelID, "Chọn người để treo cổ", alive),
	}); err != nil {
```

becomes:

```go
	if _, err := s.ChannelMessageSendComplex(channelID, &discordgo.MessageSend{
		Content:    fmt.Sprintf("🗳️ **Bỏ phiếu treo cổ.** Chọn người bạn nghi ngờ, hoặc bỏ qua.\n\n%s", summary),
		Components: voteSelectMenu(werewolfVoteID+channelID, "Chọn người để treo cổ", alive),
	}); err != nil {
```

- [ ] **Step 6: Wire it into `resolveVote`'s result announcement**

In `internal/discord/commands/werewolf_day.go`, `resolveVote`'s `var` block:

```go
	var (
		gameFound     bool
		lynchedPlayer *player
		revealOnDeath bool
	)
```

becomes:

```go
	var (
		gameFound     bool
		lynchedPlayer *player
		revealOnDeath bool
		summary       string
	)
```

Inside the locked `func()`, right before its closing `}()`, after `revealOnDeath = game.RevealOnDeath`, add:

```go
		revealOnDeath = game.RevealOnDeath
		summary = aliveDeadSummary(game)
	}()
```

(replacing the existing `revealOnDeath = game.RevealOnDeath\n\t}()` two-line ending with these three lines).

Then update the vote-result message send:

```go
	if _, err := s.ChannelMessageSendComplex(channelID, &discordgo.MessageSend{
		Content:         resultMsg,
		AllowedMentions: &discordgo.MessageAllowedMentions{},
	}); err != nil {
		log.Error().Err(err).Msg("werewolf: failed to post vote result")
	}
```

becomes:

```go
	if _, err := s.ChannelMessageSendComplex(channelID, &discordgo.MessageSend{
		Content:         resultMsg + "\n\n" + summary,
		AllowedMentions: &discordgo.MessageAllowedMentions{},
	}); err != nil {
		log.Error().Err(err).Msg("werewolf: failed to post vote result")
	}
```

- [ ] **Step 7: Build and run the full package test suite**

Run: `go build ./... && go vet ./internal/discord/commands/... && go test ./internal/discord/commands/... -v`
Expected: build succeeds, all tests `PASS` (this task changes no pure logic, so no new tests — verified by build + existing suite staying green).

- [ ] **Step 8: Commit**

```bash
git add internal/discord/commands/werewolf.go internal/discord/commands/werewolf_night.go internal/discord/commands/werewolf_day.go
git commit -m "werewolf: add alive/dead roster summary to every phase announcement"
```

---

### Task 4: Round-history data model and recap formatting (pure, tested)

**Files:**
- Modify: `internal/discord/commands/werewolf.go` (add `roundLog` type, `History` field on `werewolfGame`)
- Modify: `internal/discord/commands/werewolf_day.go` (add `formatRoundBlock`, `chunkRecapMessages`)
- Test: `internal/discord/commands/werewolf_test.go`

**Interfaces:**
- Produces: `type roundLog struct{...}` (in `werewolf.go`), `game.History []roundLog` field, `formatRoundBlock(r roundLog) string` and `chunkRecapMessages(history []roundLog, limit int) []string` (both in `werewolf_day.go`) — Task 5 populates `game.History` during play and calls `chunkRecapMessages` from `announceGameEnd`.

- [ ] **Step 1: Add the `roundLog` type and `History` field**

In `internal/discord/commands/werewolf.go`, add this type definition immediately before the `werewolfGame` struct definition:

```go
// roundLog captures everything that happened in one round (night + day) for
// the end-of-game recap. Populated incrementally: runNightPhase appends a
// fresh entry when a round starts, resolveNight fills the night-side
// fields, and resolveVote fills the day-side fields on that same entry.
// Zero-value fields (a role that's already dead, or not in this game's
// ratio) are omitted when rendering.
type roundLog struct {
	Round             int
	WolfTargetLabel   string
	WolfTargetSaved   bool
	DoctorTargetLabel string
	SeerTargetLabel   string
	SeerResult        role
	VoteBreakdown     map[string][]string // target label (or voteSkipLabel) -> voter labels who chose it
	LynchedLabel      string
}
```

Then add a `History` field to the `werewolfGame` struct. Replace:

```go
	Votes                         map[string]string  // voterID -> targetID, reset each day
	AFKStrikes                    map[string]int     // resets to 0 on any successful action, +1 on a miss
}
```

with:

```go
	Votes                         map[string]string  // voterID -> targetID, reset each day
	AFKStrikes                    map[string]int     // resets to 0 on any successful action, +1 on a miss
	History                       []roundLog         // one entry per completed round, for the end-of-game recap
}
```

- [ ] **Step 2: Add `formatRoundBlock` and `chunkRecapMessages` to `werewolf_day.go`**

Add `"sort"` to the imports of `internal/discord/commands/werewolf_day.go`:

```go
import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/rs/zerolog/log"
)
```

Then add these two functions, placed immediately after `announceGameEnd` (before `handleVote`):

```go
// formatRoundBlock renders one round's recap section. Any field left at its
// zero value (a role that's already dead, or not in this game's role
// ratio) is omitted.
func formatRoundBlock(r roundLog) string {
	var b strings.Builder
	fmt.Fprintf(&b, "\n\n**Đêm/Ngày %d:**", r.Round)
	if r.WolfTargetLabel != "" {
		if r.WolfTargetSaved {
			fmt.Fprintf(&b, "\n🐺 Sói cắn: %s (được bác sĩ cứu)", r.WolfTargetLabel)
		} else {
			fmt.Fprintf(&b, "\n🐺 Sói cắn: %s", r.WolfTargetLabel)
		}
	}
	if r.DoctorTargetLabel != "" {
		fmt.Fprintf(&b, "\n💉 Bác sĩ bảo vệ: %s", r.DoctorTargetLabel)
	}
	if r.SeerTargetLabel != "" {
		fmt.Fprintf(&b, "\n🔮 Tiên tri xem: %s → %s", r.SeerTargetLabel, r.SeerResult.label())
	}
	if len(r.VoteBreakdown) > 0 {
		targets := make([]string, 0, len(r.VoteBreakdown))
		for target := range r.VoteBreakdown {
			targets = append(targets, target)
		}
		sort.Strings(targets)
		lines := make([]string, 0, len(targets))
		for _, target := range targets {
			voters := append([]string(nil), r.VoteBreakdown[target]...)
			sort.Strings(voters)
			lines = append(lines, fmt.Sprintf("%s: %d (%s)", target, len(voters), strings.Join(voters, ", ")))
		}
		lynched := "Không ai bị treo cổ"
		if r.LynchedLabel != "" {
			lynched = r.LynchedLabel + " bị treo cổ"
		}
		fmt.Fprintf(&b, "\n🗳️ Kết quả bỏ phiếu:\n%s\n→ %s", strings.Join(lines, "\n"), lynched)
	}
	return b.String()
}

// chunkRecapMessages splits the full round-by-round recap into one or more
// message bodies, each within limit characters, splitting only on whole
// round boundaries (never mid-round). Always returns at least one string
// when history is non-empty.
func chunkRecapMessages(history []roundLog, limit int) []string {
	if len(history) == 0 {
		return nil
	}
	const header = "📜 **Lịch sử ván đấu**"
	var chunks []string
	current := header
	for _, r := range history {
		block := formatRoundBlock(r)
		if len(current)+len(block) > limit && current != header {
			chunks = append(chunks, current)
			current = header
		}
		current += block
	}
	return append(chunks, current)
}
```

- [ ] **Step 3: Write failing tests for both functions**

Append to `internal/discord/commands/werewolf_test.go`:

```go
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
```

Add `"strings"` to the test file's imports:

```go
import (
	"strings"
	"testing"
	"time"
)
```

- [ ] **Step 4: Run the tests to verify they fail (functions don't exist yet if run out of order) or pass (if Step 2 already applied)**

Since Step 2 (implementation) comes before Step 3 (tests) in this task's ordering, run:

Run: `go test ./internal/discord/commands/... -run 'TestFormatRoundBlock|TestChunkRecapMessages' -v`
Expected: all `PASS`.

- [ ] **Step 5: Build and run the full package test suite**

Run: `go build ./... && go vet ./internal/discord/commands/... && go test ./internal/discord/commands/... -v`
Expected: build succeeds, all tests `PASS`.

- [ ] **Step 6: Commit**

```bash
git add internal/discord/commands/werewolf.go internal/discord/commands/werewolf_day.go internal/discord/commands/werewolf_test.go
git commit -m "werewolf: add roundLog data model and pure recap-formatting functions

Not wired into gameplay yet -- game.History stays empty until the next
task populates it during play."
```

---

### Task 5: Wire round history into gameplay, seer-check tracking, and post the recap at game end

**Files:**
- Modify: `internal/discord/commands/werewolf.go` (add `SeerTarget`/`SeerResult` fields to `werewolfGame`)
- Modify: `internal/discord/commands/werewolf_night.go` (`runNightPhase`'s reset block, `handleSeerCheck`, `resolveNight`)
- Modify: `internal/discord/commands/werewolf_day.go` (`resolveVote`, `announceGameEnd`)

**Interfaces:**
- Consumes: `roundLog`, `formatRoundBlock`, `chunkRecapMessages` (Task 4); `voteSkipValue`, `voteSkipLabel` (Task 2).
- Produces: `werewolfGame.SeerTarget string` and `werewolfGame.SeerResult role` (new fields — the seer's current-round pick and what it revealed; reset every round alongside `SeerActed`).

- [ ] **Step 1: Add `SeerTarget`/`SeerResult` fields to `werewolfGame`**

In `internal/discord/commands/werewolf.go`, replace:

```go
	SeerActed                     bool               // whether the seer has acted this round
```

with:

```go
	SeerActed                     bool               // whether the seer has acted this round
	SeerTarget                    string             // who the seer checked this round, "" if SeerActed is false
	SeerResult                    role               // that check's result — meaningless if SeerTarget == ""
```

- [ ] **Step 2: Reset the new seer fields each round and append a fresh `roundLog` entry**

In `internal/discord/commands/werewolf_night.go`, `runNightPhase`'s locked block currently has:

```go
		gameFound = true
		game.Phase = phaseNight
		game.Round++
		game.NightTarget = ""
		game.SeerActed = false
		game.DoctorProtect = ""

		round = game.Round
		summary = aliveDeadSummary(game)
```

Replace with:

```go
		gameFound = true
		game.Phase = phaseNight
		game.Round++
		game.NightTarget = ""
		game.SeerActed = false
		game.SeerTarget = ""
		game.DoctorProtect = ""
		game.History = append(game.History, roundLog{Round: game.Round})

		round = game.Round
		summary = aliveDeadSummary(game)
```

- [ ] **Step 3: Record the seer's target and result in `handleSeerCheck`**

In `internal/discord/commands/werewolf_night.go`, `handleSeerCheck` currently has:

```go
	game.SeerActed = true
	isWolf := target.Role == roleWerewolf
	cmd.mu.Unlock()
```

Replace with:

```go
	game.SeerActed = true
	game.SeerTarget = targetID
	game.SeerResult = target.Role
	isWolf := target.Role == roleWerewolf
	cmd.mu.Unlock()
```

- [ ] **Step 4: Fill the current round's history entry in `resolveNight`**

In `internal/discord/commands/werewolf_night.go`, `resolveNight`'s locked block currently starts:

```go
		gameFound = true

		var killedID string
		if game.NightTarget != "" {
			if game.NightTarget == game.DoctorProtect {
				saved = true
			} else if target, exists := game.Players[game.NightTarget]; exists && target.Alive {
				target.Alive = false
				killedID = game.NightTarget
			}
		}
```

Replace with:

```go
		gameFound = true

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
```

Then, still inside the same locked block, immediately before its closing `}()` (after the existing `summary = aliveDeadSummary(game)` line added in Task 3, and after the `if killedID != "" { killedPlayer = game.Players[killedID] }` line), add the history fill:

```go
		if killedID != "" {
			killedPlayer = game.Players[killedID]
		}

		if n := len(game.History); n > 0 {
			entry := &game.History[n-1]
			entry.WolfTargetLabel = wolfTargetLabel
			entry.WolfTargetSaved = saved
			entry.DoctorTargetLabel = doctorTargetLabel
			if game.SeerTarget != "" {
				if target, exists := game.Players[game.SeerTarget]; exists {
					entry.SeerTargetLabel = target.Label
				}
				entry.SeerResult = game.SeerResult
			}
		}
	}()
```

(This replaces the existing `if killedID != "" { killedPlayer = game.Players[killedID] }\n\t}()` two-line ending with the block above.)

- [ ] **Step 5: Fill the vote tally and lynch outcome in `resolveVote`**

In `internal/discord/commands/werewolf_day.go`, `resolveVote`'s locked block currently ends:

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
	}()
```

Replace with:

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

		if n := len(game.History); n > 0 {
			entry := &game.History[n-1]
			entry.VoteBreakdown = make(map[string][]string)
			for voterID, targetID := range game.Votes {
				label := voteSkipLabel
				if targetID != voteSkipValue {
					if target, exists := game.Players[targetID]; exists {
						label = target.Label
					} else {
						label = "?"
					}
				}
				voterLabel := "?"
				if voter, exists := game.Players[voterID]; exists {
					voterLabel = voter.Label
				}
				entry.VoteBreakdown[label] = append(entry.VoteBreakdown[label], voterLabel)
			}
			if lynchedPlayer != nil {
				entry.LynchedLabel = lynchedPlayer.Label
			}
		}
	}()
```

- [ ] **Step 6: Post the recap in `announceGameEnd`**

In `internal/discord/commands/werewolf_day.go`, `announceGameEnd` currently ends:

```go
	if _, err := s.ChannelMessageSendComplex(game.ChannelID, &discordgo.MessageSend{
		Content:         fmt.Sprintf("🎉 Ván Ma Sói kết thúc! %s%s%s", winner, afkNote, roles.String()),
		AllowedMentions: &discordgo.MessageAllowedMentions{},
	}); err != nil {
		log.Error().Err(err).Msg("werewolf: failed to announce game end")
	}
}
```

Replace with:

```go
	if _, err := s.ChannelMessageSendComplex(game.ChannelID, &discordgo.MessageSend{
		Content:         fmt.Sprintf("🎉 Ván Ma Sói kết thúc! %s%s%s", winner, afkNote, roles.String()),
		AllowedMentions: &discordgo.MessageAllowedMentions{},
	}); err != nil {
		log.Error().Err(err).Msg("werewolf: failed to announce game end")
	}

	for _, chunk := range chunkRecapMessages(game.History, 1900) {
		if _, err := s.ChannelMessageSendComplex(game.ChannelID, &discordgo.MessageSend{
			Content:         chunk,
			AllowedMentions: &discordgo.MessageAllowedMentions{},
		}); err != nil {
			log.Error().Err(err).Msg("werewolf: failed to post game recap")
		}
	}
}
```

- [ ] **Step 7: Build and run the full package test suite**

Run: `go build ./... && go vet ./internal/discord/commands/... && go test ./internal/discord/commands/... -v`
Expected: build succeeds, all tests `PASS`. This task wires Discord-coupled state (not independently unit-testable per the project's existing testing philosophy — see spec's Testing section) — verification here is build-clean plus the untouched pure-function suite staying green; full behavior is confirmed in Task 9's manual E2E pass.

- [ ] **Step 8: Commit**

```bash
git add internal/discord/commands/werewolf.go internal/discord/commands/werewolf_night.go internal/discord/commands/werewolf_day.go
git commit -m "werewolf: wire round history into gameplay, post recap at game end

Seer's per-round target/result is now tracked (SeerTarget/SeerResult,
reset each round) so it can appear in the end-of-game recap."
```

---

### Task 6: Lock the main channel to bot-only chat while a game is active

**Files:**
- Modify: `internal/discord/commands/werewolf.go` (add `PriorOverwrite` field, `everyoneOverwrite`/`lockChannelToBotOnly`/`unlockChannel` helpers, wire into `handleStart`)
- Modify: `internal/discord/commands/werewolf_day.go` (`checkAndHandleGameEnd`)
- Modify: `internal/discord/commands/werewolf.go` (`handleCancelLobby`, `startPreflight`'s panic-recovery handler)

**Interfaces:**
- Produces: `lockChannelToBotOnly(s *discordgo.Session, guildID, channelID string) (*discordgo.PermissionOverwrite, error)`, `unlockChannel(s *discordgo.Session, guildID, channelID string, prior *discordgo.PermissionOverwrite)` (both in `werewolf.go`) — called from `handleStart` (lock) and the three terminal-path sites (unlock).
- `werewolfGame.PriorOverwrite *discordgo.PermissionOverwrite` (nil means "no @everyone overwrite existed before this game locked the channel").

- [ ] **Step 1: Add `PriorOverwrite` to `werewolfGame`**

In `internal/discord/commands/werewolf.go`, replace:

```go
	History                       []roundLog         // one entry per completed round, for the end-of-game recap
}
```

with:

```go
	History                       []roundLog                       // one entry per completed round, for the end-of-game recap
	PriorOverwrite                *discordgo.PermissionOverwrite   // @everyone's channel overwrite before this game locked it; nil if there was none
}
```

- [ ] **Step 2: Add the lock/unlock helpers**

Add these functions to `internal/discord/commands/werewolf.go`, immediately after `aliveDeadSummary` (before `werewolfLobbyButtons`):

```go
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
			return ow, nil
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
```

- [ ] **Step 3: Check the bot's permission and apply the lock in `handleStart`**

In `internal/discord/commands/werewolf.go`, `handleStart` currently starts:

```go
func (cmd *WerewolfCommand) handleStart(s *discordgo.Session, i *discordgo.InteractionCreate, subOptions []*discordgo.ApplicationCommandInteractionDataOption) {
	cmd.mu.Lock()
	_, exists := cmd.games[i.ChannelID]
	cmd.mu.Unlock()
	if exists {
		respondError(s, i, "Kênh này đang có ván Ma Sói, đợi ván trước kết thúc đã.")
		return
	}

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

	err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Content:         werewolfLobbyContent(game),
			Components:      werewolfLobbyButtons(i.ChannelID, false),
			AllowedMentions: &discordgo.MessageAllowedMentions{},
		},
	})
	if err != nil {
		log.Error().Err(err).Msg("werewolf: failed to post lobby")
		return
	}
```

Replace with:

```go
func (cmd *WerewolfCommand) handleStart(s *discordgo.Session, i *discordgo.InteractionCreate, subOptions []*discordgo.ApplicationCommandInteractionDataOption) {
	cmd.mu.Lock()
	_, exists := cmd.games[i.ChannelID]
	cmd.mu.Unlock()
	if exists {
		respondError(s, i, "Kênh này đang có ván Ma Sói, đợi ván trước kết thúc đã.")
		return
	}

	botPerms, err := s.UserChannelPermissions(s.State.User.ID, i.ChannelID)
	if err != nil || botPerms&discordgo.PermissionManageChannels == 0 {
		respondError(s, i, "Bot cần quyền \"Manage Channels\" trong kênh này để khoá chat khi chơi Ma Sói.")
		return
	}

	priorOverwrite, err := lockChannelToBotOnly(s, i.GuildID, i.ChannelID)
	if err != nil {
		log.Error().Err(err).Msg("werewolf: failed to lock channel")
		respondError(s, i, "Không thể khoá kênh để bắt đầu ván Ma Sói, thử lại sau.")
		return
	}

	options := parseOptions(subOptions)
	revealOnDeath := true
	if v, ok := options["reveal-on-death"].(bool); ok {
		revealOnDeath = v
	}

	starterID := i.Member.User.ID
	game := &werewolfGame{
		GuildID:        i.GuildID,
		ChannelID:      i.ChannelID,
		StarterID:      starterID,
		Seq:            atomic.AddUint64(&werewolfSeqCounter, 1),
		RevealOnDeath:  revealOnDeath,
		Phase:          phaseLobby,
		Players:        make(map[string]*player),
		JoinOrder:      []string{},
		Votes:          make(map[string]string),
		AFKStrikes:     make(map[string]int),
		PriorOverwrite: priorOverwrite,
	}

	err = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Content:         werewolfLobbyContent(game),
			Components:      werewolfLobbyButtons(i.ChannelID, false),
			AllowedMentions: &discordgo.MessageAllowedMentions{},
		},
	})
	if err != nil {
		unlockChannel(s, i.GuildID, i.ChannelID, priorOverwrite)
		log.Error().Err(err).Msg("werewolf: failed to post lobby")
		return
	}
```

- [ ] **Step 4: Unlock on normal game end**

In `internal/discord/commands/werewolf_day.go`, `checkAndHandleGameEnd`'s locked block currently ends:

```go
		ended = true
		threadID = game.WolfThreadID
		endedGame = game
		delete(cmd.games, channelID)
	}()
	if !gameFound {
		return true
	}
	if !ended {
		if len(removedNames) > 0 {
			cmd.announceAFKRemovals(s, channelID, removedNames)
		}
		return false
	}

	if threadID != "" {
		if _, err := s.ChannelDelete(threadID); err != nil {
			log.Error().Err(err).Msg("werewolf: failed to delete wolf thread on game end")
		}
	}

	cmd.announceGameEnd(s, endedGame, result, removedNames)
	return true
}
```

Replace with:

```go
		ended = true
		threadID = game.WolfThreadID
		endedGame = game
		delete(cmd.games, channelID)
	}()
	if !gameFound {
		return true
	}
	if !ended {
		if len(removedNames) > 0 {
			cmd.announceAFKRemovals(s, channelID, removedNames)
		}
		return false
	}

	if threadID != "" {
		if _, err := s.ChannelDelete(threadID); err != nil {
			log.Error().Err(err).Msg("werewolf: failed to delete wolf thread on game end")
		}
	}
	unlockChannel(s, endedGame.GuildID, channelID, endedGame.PriorOverwrite)

	cmd.announceGameEnd(s, endedGame, result, removedNames)
	return true
}
```

- [ ] **Step 5: Unlock on lobby cancel**

In `internal/discord/commands/werewolf.go`, `handleCancelLobby` currently is:

```go
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
	delete(cmd.games, channelID)
	cmd.mu.Unlock()

	if threadID != "" {
		if _, err := s.ChannelDelete(threadID); err != nil {
			log.Error().Err(err).Msg("werewolf: failed to delete wolf thread on lobby cancel")
		}
	}

	_ = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseUpdateMessage,
		Data: &discordgo.InteractionResponseData{
			Content:    "🚫 Đã huỷ bàn Ma Sói.",
			Components: []discordgo.MessageComponent{},
		},
	})
}
```

Replace with:

```go
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
	guildID := game.GuildID
	priorOverwrite := game.PriorOverwrite
	delete(cmd.games, channelID)
	cmd.mu.Unlock()

	if threadID != "" {
		if _, err := s.ChannelDelete(threadID); err != nil {
			log.Error().Err(err).Msg("werewolf: failed to delete wolf thread on lobby cancel")
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
```

- [ ] **Step 6: Unlock on panic-recovery**

In `internal/discord/commands/werewolf.go`, `startPreflight`'s panic-recovery handler currently is:

```go
		defer func() {
			if r := recover(); r != nil {
				log.Error().
					Interface("panic", r).
					Str("channel_id", channelID).
					Msg("werewolf: recovered from panic in game goroutine")

				var threadID string
				var cleaned bool
				func() {
					cmd.mu.Lock()
					defer cmd.mu.Unlock()
					game := cmd.gameIfCurrent(channelID, seq)
					if game == nil {
						return
					}
					threadID = game.WolfThreadID
					delete(cmd.games, channelID)
					cleaned = true
				}()
				if cleaned {
					if threadID != "" {
						if _, err := s.ChannelDelete(threadID); err != nil {
							log.Error().Err(err).Msg("werewolf: failed to delete wolf thread during panic cleanup")
						}
					}
					log.Error().Str("channel_id", channelID).Msg("werewolf: cleaned up game state after panic recovery")
				}
			}
		}()
```

Replace with:

```go
		defer func() {
			if r := recover(); r != nil {
				log.Error().
					Interface("panic", r).
					Str("channel_id", channelID).
					Msg("werewolf: recovered from panic in game goroutine")

				var threadID, guildID string
				var priorOverwrite *discordgo.PermissionOverwrite
				var cleaned bool
				func() {
					cmd.mu.Lock()
					defer cmd.mu.Unlock()
					game := cmd.gameIfCurrent(channelID, seq)
					if game == nil {
						return
					}
					threadID = game.WolfThreadID
					guildID = game.GuildID
					priorOverwrite = game.PriorOverwrite
					delete(cmd.games, channelID)
					cleaned = true
				}()
				if cleaned {
					if threadID != "" {
						if _, err := s.ChannelDelete(threadID); err != nil {
							log.Error().Err(err).Msg("werewolf: failed to delete wolf thread during panic cleanup")
						}
					}
					unlockChannel(s, guildID, channelID, priorOverwrite)
					log.Error().Str("channel_id", channelID).Msg("werewolf: cleaned up game state after panic recovery")
				}
			}
		}()
```

- [ ] **Step 7: Build and run the full package test suite**

Run: `go build ./... && go vet ./internal/discord/commands/... && go test ./internal/discord/commands/... -v`
Expected: build succeeds, all tests `PASS`. Permission-overwrite behavior against a real Discord server is out of scope for unit tests (no Discord API in `go test`) — verified manually in Task 9.

- [ ] **Step 8: Commit**

```bash
git add internal/discord/commands/werewolf.go internal/discord/commands/werewolf_day.go
git commit -m "werewolf: lock the game channel to bot-only chat while a game runs

Requires the bot to hold Manage Channels on the guild/channel, checked
up front in /werewolf start. The prior @everyone overwrite (if any) is
snapshotted and restored on every terminal path: win, lobby cancel, and
panic-recovery."
```

---

### Task 7: Private alive-players discussion thread

**Files:**
- Modify: `internal/discord/commands/werewolf.go` (`AliveThreadID` field, `createAliveDiscussionThread`/`removeFromAliveThread` helpers, wire into `startPreflight`, `handleLeave`, `handleCancelLobby`, panic-recovery)
- Modify: `internal/discord/commands/werewolf_night.go` (`resolveNight`)
- Modify: `internal/discord/commands/werewolf_day.go` (`resolveVote`, `checkAndHandleGameEnd`)

**Interfaces:**
- Produces: `werewolfGame.AliveThreadID string`, `(cmd *WerewolfCommand) createAliveDiscussionThread(s *discordgo.Session, channelID string, joinOrder []string) (string, error)`, `removeFromAliveThread(s *discordgo.Session, threadID, userID string)` (all in `werewolf.go`).

- [ ] **Step 1: Add `AliveThreadID` to `werewolfGame`**

In `internal/discord/commands/werewolf.go`, replace:

```go
	PriorOverwrite                *discordgo.PermissionOverwrite   // @everyone's channel overwrite before this game locked it; nil if there was none
}
```

with:

```go
	PriorOverwrite                *discordgo.PermissionOverwrite   // @everyone's channel overwrite before this game locked it; nil if there was none
	AliveThreadID                 string                           // private thread all players use for day-discussion chat; "" before it's created
}
```

- [ ] **Step 2: Add the thread creation and removal helpers**

Add these functions to `internal/discord/commands/werewolf.go`, immediately after `unlockChannel`:

```go
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
```

- [ ] **Step 3: Create the thread in `startPreflight`, right after role assignment succeeds**

In `internal/discord/commands/werewolf.go`, `startPreflight` currently ends:

```go
	pingText, allowedMentions := pingOnce(joinOrder)
	if _, err := s.ChannelMessageSendComplex(channelID, &discordgo.MessageSend{
		Content:         fmt.Sprintf("🐺 Vai trò đã được gửi riêng cho từng người! %s", pingText),
		AllowedMentions: allowedMentions,
	}); err != nil {
		log.Error().Err(err).Msg("werewolf: failed to post role-assigned announcement")
	}

	cmd.runNightPhase(s, channelID, seq)
}
```

Replace with:

```go
	pingText, allowedMentions := pingOnce(joinOrder)
	if _, err := s.ChannelMessageSendComplex(channelID, &discordgo.MessageSend{
		Content:         fmt.Sprintf("🐺 Vai trò đã được gửi riêng cho từng người! %s", pingText),
		AllowedMentions: allowedMentions,
	}); err != nil {
		log.Error().Err(err).Msg("werewolf: failed to post role-assigned announcement")
	}

	aliveThreadID, err := cmd.createAliveDiscussionThread(s, channelID, joinOrder)
	if err != nil {
		log.Error().Err(err).Msg("werewolf: failed to create alive-discussion thread")
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
```

- [ ] **Step 4: Remove the lynched player from the thread in `resolveVote`**

In `internal/discord/commands/werewolf_day.go`, `resolveVote`'s `var` block:

```go
	var (
		gameFound     bool
		lynchedPlayer *player
		revealOnDeath bool
		summary       string
	)
```

becomes:

```go
	var (
		gameFound     bool
		lynchedPlayer *player
		revealOnDeath bool
		summary       string
		aliveThreadID string
	)
```

Inside the locked `func()`, add `aliveThreadID = game.AliveThreadID` right after `gameFound = true`:

```go
		gameFound = true
		aliveThreadID = game.AliveThreadID
```

After the locked block returns (i.e. after the `if !gameFound { return }` check, before `var resultMsg string`), add:

```go
	if !gameFound {
		return
	}
	if lynchedPlayer != nil {
		removeFromAliveThread(s, aliveThreadID, lynchedPlayer.UserID)
	}
```

(replacing the existing `if !gameFound {\n\t\treturn\n\t}` with the four lines above).

- [ ] **Step 5: Remove the wolf-killed player from the thread in `resolveNight`**

In `internal/discord/commands/werewolf_night.go`, `resolveNight`'s `var` block:

```go
	var (
		gameFound     bool
		threadID      string
		revealOnDeath bool
		killedPlayer  *player
		saved         bool
		summary       string
	)
```

becomes:

```go
	var (
		gameFound     bool
		threadID      string
		revealOnDeath bool
		killedPlayer  *player
		saved         bool
		summary       string
		aliveThreadID string
	)
```

Inside the locked `func()`, add `aliveThreadID = game.AliveThreadID` right after `gameFound = true`:

```go
		gameFound = true
		aliveThreadID = game.AliveThreadID
```

After the locked block, replace:

```go
	}()
	if !gameFound {
		return
	}

	if threadID != "" {
```

with:

```go
	}()
	if !gameFound {
		return
	}
	if killedPlayer != nil {
		removeFromAliveThread(s, aliveThreadID, killedPlayer.UserID)
	}

	if threadID != "" {
```

- [ ] **Step 6: Remove AFK-removed players from the thread in `checkAndHandleGameEnd`**

In `internal/discord/commands/werewolf_day.go`, `checkAndHandleGameEnd` currently has:

```go
	var (
		gameFound    bool
		ended        bool
		removedNames []string
		threadID     string
		result       gameResult
		endedGame    *werewolfGame
	)
	func() {
		cmd.mu.Lock()
		defer cmd.mu.Unlock()
		game := cmd.gameIfCurrent(channelID, seq)
		if game == nil {
			return
		}
		gameFound = true

		for _, id := range game.JoinOrder {
			p := game.Players[id]
			if p.Alive && shouldRemoveForAFK(game.AFKStrikes[id]) {
				p.Alive = false
				removedNames = append(removedNames, p.Label)
			}
		}
```

Replace with:

```go
	var (
		gameFound     bool
		ended         bool
		removedNames  []string
		removedIDs    []string
		threadID      string
		result        gameResult
		endedGame     *werewolfGame
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

		for _, id := range game.JoinOrder {
			p := game.Players[id]
			if p.Alive && shouldRemoveForAFK(game.AFKStrikes[id]) {
				p.Alive = false
				removedNames = append(removedNames, p.Label)
				removedIDs = append(removedIDs, id)
			}
		}
```

Then, further down in the same function, replace:

```go
	if !gameFound {
		return true
	}
	if !ended {
		if len(removedNames) > 0 {
			cmd.announceAFKRemovals(s, channelID, removedNames)
		}
		return false
	}
```

with:

```go
	if !gameFound {
		return true
	}
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

And in the `ended` branch, add the alive-thread deletion alongside the existing wolf-thread deletion. Replace:

```go
	if threadID != "" {
		if _, err := s.ChannelDelete(threadID); err != nil {
			log.Error().Err(err).Msg("werewolf: failed to delete wolf thread on game end")
		}
	}
	unlockChannel(s, endedGame.GuildID, channelID, endedGame.PriorOverwrite)
```

with:

```go
	if threadID != "" {
		if _, err := s.ChannelDelete(threadID); err != nil {
			log.Error().Err(err).Msg("werewolf: failed to delete wolf thread on game end")
		}
	}
	if endedGame.AliveThreadID != "" {
		if _, err := s.ChannelDelete(endedGame.AliveThreadID); err != nil {
			log.Error().Err(err).Msg("werewolf: failed to delete alive-discussion thread on game end")
		}
	}
	unlockChannel(s, endedGame.GuildID, channelID, endedGame.PriorOverwrite)
```

- [ ] **Step 7: Remove a manually-leaving player from the thread in `handleLeave`**

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
	cmd.mu.Unlock()

	respondSuccess(s, i, "Đã rời ván. Bạn sẽ được coi như đã bị loại.")
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
	revealOnDeath := game.RevealOnDeath
	channelID := game.ChannelID
	seq := game.Seq
	aliveThreadID := game.AliveThreadID
	cmd.mu.Unlock()

	removeFromAliveThread(s, aliveThreadID, userID)
	respondSuccess(s, i, "Đã rời ván. Bạn sẽ được coi như đã bị loại.")
```

- [ ] **Step 8: Clean up the thread on lobby cancel and panic-recovery**

In `internal/discord/commands/werewolf.go`, `handleCancelLobby` currently has:

```go
	threadID := game.WolfThreadID
	guildID := game.GuildID
	priorOverwrite := game.PriorOverwrite
	delete(cmd.games, channelID)
	cmd.mu.Unlock()

	if threadID != "" {
		if _, err := s.ChannelDelete(threadID); err != nil {
			log.Error().Err(err).Msg("werewolf: failed to delete wolf thread on lobby cancel")
		}
	}
	unlockChannel(s, guildID, channelID, priorOverwrite)
```

Replace with:

```go
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
```

And in `startPreflight`'s panic-recovery handler, replace:

```go
				var threadID, guildID string
				var priorOverwrite *discordgo.PermissionOverwrite
				var cleaned bool
				func() {
					cmd.mu.Lock()
					defer cmd.mu.Unlock()
					game := cmd.gameIfCurrent(channelID, seq)
					if game == nil {
						return
					}
					threadID = game.WolfThreadID
					guildID = game.GuildID
					priorOverwrite = game.PriorOverwrite
					delete(cmd.games, channelID)
					cleaned = true
				}()
				if cleaned {
					if threadID != "" {
						if _, err := s.ChannelDelete(threadID); err != nil {
							log.Error().Err(err).Msg("werewolf: failed to delete wolf thread during panic cleanup")
						}
					}
					unlockChannel(s, guildID, channelID, priorOverwrite)
					log.Error().Str("channel_id", channelID).Msg("werewolf: cleaned up game state after panic recovery")
				}
```

with:

```go
				var threadID, aliveThreadID, guildID string
				var priorOverwrite *discordgo.PermissionOverwrite
				var cleaned bool
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
					unlockChannel(s, guildID, channelID, priorOverwrite)
					log.Error().Str("channel_id", channelID).Msg("werewolf: cleaned up game state after panic recovery")
				}
```

- [ ] **Step 9: Build and run the full package test suite**

Run: `go build ./... && go vet ./internal/discord/commands/... && go test ./internal/discord/commands/... -v`
Expected: build succeeds, all tests `PASS`. Thread creation/membership against a real Discord server is verified manually in Task 9.

- [ ] **Step 10: Commit**

```bash
git add internal/discord/commands/werewolf.go internal/discord/commands/werewolf_night.go internal/discord/commands/werewolf_day.go
git commit -m "werewolf: add private alive-players discussion thread

Created once after role assignment; membership shrinks as players die
(lynch, wolf-kill, AFK-removal, manual leave); deleted on every
terminal path alongside the wolf thread and the channel-lock restore."
```

---

### Task 8: Update `/help werewolf` documentation

**Files:**
- Modify: `internal/discord/commands/help.go:414-441`

**Interfaces:**
- None (documentation-only; no new types or functions).

- [ ] **Step 1: Rewrite the werewolf help embed**

In `internal/discord/commands/help.go`, replace the `case "werewolf":` block:

```go
	case "werewolf":
		return &discordgo.MessageEmbed{
			Title:       "🐺 `/werewolf` Command",
			Color:       0x5865F2,
			Description: "Play a game of Werewolf (Ma Sói): Werewolves, Villagers, a Seer, and a Doctor.",
			Fields: []*discordgo.MessageEmbedField{
				{
					Name:   "Subcommands",
					Value:  "• `/werewolf start reveal-on-death:true|false` - Open a lobby in this channel\n• `/werewolf status` - Check the current phase/round without leaking roles\n• `/werewolf leave` - Leave the lobby, or leave a running game (counts as being eliminated)",
					Inline: false,
				},
				{
					Name:   "How it works",
					Value:  fmt.Sprintf("Anyone can Join/Leave the lobby; the starter clicks Start once %d-%d have joined. Roles are DMed privately. Each night the Werewolves get a private thread to pick a kill target, the Seer and Doctor act via DM. Each day: discuss, then vote to lynch — a tie means nobody is lynched. Repeats until one side wins.", werewolfMinPlayers, werewolfMaxPlayers),
					Inline: false,
				},
				{
					Name:   "Privacy note",
					Value:  "The werewolves' coordination thread is private from other players, but anyone with the server's \"Manage Threads\" permission (usually admins/mods) can still see it — that's a Discord permission-model limit, not a bot bug.",
					Inline: false,
				},
				{
					Name:   "Limits",
					Value:  "In-memory only — a bot restart while a game is running loses it, same as `/duel-challenge-all`. Missing 2 actions/votes in a row auto-removes a player. One game per channel at a time.",
					Inline: false,
				},
			},
		}
```

with:

```go
	case "werewolf":
		return &discordgo.MessageEmbed{
			Title:       "🐺 `/werewolf` Command",
			Color:       0x5865F2,
			Description: "Play a game of Werewolf (Ma Sói): Werewolves, Villagers, a Seer, and a Doctor.",
			Fields: []*discordgo.MessageEmbedField{
				{
					Name:   "Subcommands",
					Value:  "• `/werewolf start reveal-on-death:true|false` - Open a lobby in this channel\n• `/werewolf status` - Check the current phase/round without leaking roles\n• `/werewolf leave` - Leave the lobby, or leave a running game (counts as being eliminated)",
					Inline: false,
				},
				{
					Name:   "How it works",
					Value:  fmt.Sprintf("Anyone can Join/Leave the lobby; the starter clicks Start once %d-%d have joined. Roles are DMed privately. Each night the Werewolves get a private thread to pick a kill target (with more time as their numbers grow), the Seer and Doctor act via DM. Each day: discuss in a private alive-players thread, then vote to lynch (or explicitly skip) — a tie or a skip-win means nobody is lynched. Every phase change posts a live alive/dead roster. Repeats until one side wins, then a full round-by-round recap is posted.", werewolfMinPlayers, werewolfMaxPlayers),
					Inline: false,
				},
				{
					Name:   "Channel lock",
					Value:  "The bot locks the main channel to bot-only chat for the duration of the game — all real conversation happens in two private threads: one for the werewolves at night, and one for all alive players during the day. The channel is restored automatically the moment the game ends or is cancelled. Requires the bot to have **Manage Channels** in this channel.",
					Inline: false,
				},
				{
					Name:   "Privacy note",
					Value:  "Both threads are private from other players, but anyone with the server's \"Manage Threads\" permission (usually admins/mods) can still see them — that's a Discord permission-model limit, not a bot bug.",
					Inline: false,
				},
				{
					Name:   "Limits",
					Value:  "In-memory only — a bot restart while a game is running loses it, same as `/duel-challenge-all`. Missing 2 actions/votes in a row auto-removes a player. One game per channel at a time.",
					Inline: false,
				},
			},
		}
```

- [ ] **Step 2: Build to verify**

Run: `go build ./...`
Expected: succeeds.

- [ ] **Step 3: Commit**

```bash
git add internal/discord/commands/help.go
git commit -m "werewolf: update /help werewolf for channel lock, threads, skip-vote, recap"
```

---

### Task 9: Manual end-to-end verification (no code — hand this to the user)

This task has no code changes and requires a real Discord server with the bot invited (and granted **Manage Channels** in addition to the existing **Manage Threads**), which a subagent cannot exercise. Flag this task to the user rather than dispatching it — they run it themselves against a live server.

Checklist to hand over:

- [ ] Run `/werewolf start` in a channel where the bot has Manage Channels + Manage Threads. Confirm the channel immediately becomes un-postable for a regular member (Discord shows "you don't have permission to send messages"), while the bot's own lobby message still posts fine.
- [ ] Join 5 players, click Start. Confirm: (a) the main channel gets a role-assigned announcement, (b) a new private "🏡 Thảo luận Ma Sói" thread appears with all 5 players added, (c) a "🐺 Sói bàn đêm 1" thread appears with only the wolf(s) added.
- [ ] During night 1, confirm the wolf(s)' target-picker in their thread and the seer/doctor's DM prompts arrive; let the timer run out without acting and confirm the ~halfway reminder posts in the main channel with a correct remaining-time estimate.
- [ ] Confirm the night-result and day-discuss/day-vote announcements each include a correct "🟢 Còn sống / 💀 Đã chết" line.
- [ ] During a day vote, confirm the vote menu includes a "Không treo cổ ai" option; have the majority pick it and confirm nobody is lynched.
- [ ] Have a player click "Rời đi"/`/werewolf leave` mid-game; confirm they're removed from the alive-discussion thread immediately.
- [ ] Run an 8-player game through to a win (either side). Confirm: (a) the final role reveal posts, (b) one or more recap message(s) post afterward with a per-round breakdown including wolf kill/save, doctor protect, seer check+result, and vote tally for every round that happened, (c) both threads are deleted, (d) the main channel's normal chat permission is restored (a regular member can post again).
- [ ] Cancel a lobby before it starts; confirm the channel's chat permission is restored immediately (no threads should exist yet at this point).
- [ ] If feasible, deliberately trigger a mid-game panic (e.g. by temporarily breaking something and reverting, or reviewing the panic-recovery code path logically) — otherwise treat this as an acceptable residual risk, same as the original design's Task 7 checklist.
