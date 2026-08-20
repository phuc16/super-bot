# Werewolf Feature Enhancements — Design Spec

Date: 2026-08-21
Status: Approved
Builds on: `docs/superpowers/specs/2026-08-20-werewolf-design.md` and its implementation (branch `feature/werewolf`)

## Context

The `/werewolf` game is live in-memory, one game per channel, following the lobby → preflight → night ↔ day loop described in the original design doc. This spec adds six enhancements requested after initial implementation:

1. Lock the main game channel to bot-only chat while a game is active.
2. A private "alive players" discussion thread, separate from the existing wolf-only thread.
3. An alive/dead reminder on every phase-transition announcement.
4. An explicit "skip vote" option in the lynch vote.
5. Reworked phase timers (generally faster), plus a new wolf-count-scaled night-discussion window.
6. A full round-by-round history recap posted when the game ends, including seer checks.

None of these change the core state machine (lobby → preflight → night → day-discuss → day-vote → loop/end) or the in-memory-only persistence model. All are additive to `werewolfGame` and its phase functions.

## 1. Channel lock (bot-only chat)

**Goal:** while a game is running, only the bot can post in the game channel — all real conversation happens in threads (wolf thread at night, alive-thread during the day).

**Mechanism:** Discord permission overwrites on the game channel, scoped to the `@everyone` role:

- On `/werewolf start` (lobby creation, `handleStart`), before creating the lobby, the bot calls `s.State.Guild(...)`/`s.ChannelPermissionSet(channelID, guildID, discordgo.PermissionOverwriteTypeRole, deny: PermissionSendMessages, allow: 0)` — i.e. it adds/replaces the `@everyone` overwrite on that channel to deny `SEND_MESSAGES`. It does **not** touch any other role's or member's overwrites on the channel.
- Before applying the overwrite, the bot snapshots whatever `@everyone` overwrite already existed on that channel (if any) — this could be "no overwrite," an "allow" overwrite, or an existing "deny" overwrite from other bot config. Store this snapshot (as a `*discordgo.PermissionOverwrite` pointer, nil meaning "no overwrite existed") on the `werewolfGame` struct as `PriorChannelOverwrite`.
- **Restoration** happens on every terminal path (win, lobby cancel, panic-recovery) using the snapshot:
  - If `PriorChannelOverwrite` was nil, call `s.ChannelPermissionDelete(channelID, "@everyone role ID")` to remove the overwrite entirely.
  - Otherwise, call `s.ChannelPermissionSet(...)` again with the snapshotted allow/deny values, putting it back exactly as it was.
- **Permission requirement:** this requires the bot to hold `Manage Channels` on the guild/channel. `handleStart` checks this via the bot's own permissions in the channel (`s.State.UserChannelPermissions` or equivalent) before creating the lobby; if missing, it responds with an ephemeral error ("Bot thiếu quyền Manage Channels để khoá kênh khi chơi.") and does not create the lobby. This is a new precondition alongside the existing DM-preflight check.
- **Bot's own messages are unaffected** — permission overwrites never block a bot's own sends when the bot has `Manage Channels`/`Administrator`, and separately Discord always lets a channel's own thread creator etc. post; no special-casing needed for the bot's `ChannelMessageSendComplex` calls in the main channel.
- Applying the lock happens once, at lobby creation (not at "Bắt đầu" click) — the lobby-join/leave conversation is exactly the kind of chat that should still happen normally in a *not-yet-restricted* channel... **decision:** actually, lock at lobby creation would prevent players from being able to type at all, including casual chat unrelated to joining (which is fine, they join via button, not by typing) — but it would also block the starter's own follow-up messages. Given the goal is "no real chat in the main channel once players are grouped into threads," and the lobby/join phase has no thread yet, **the lock is applied at lobby creation** (`handleStart`), matching "the moment `/werewolf start` is run, the channel goes bot-only until the game ends or is cancelled." Joining/leaving/cancelling still work via buttons, which are unaffected by `SEND_MESSAGES` permission.

## 2. Alive-players discussion thread

**Goal:** a place for alive players to actually type during the day-discussion phase, since the main channel is now bot-only.

**Mechanism:** mirrors the existing wolf-thread lifecycle (`startWolfThread` in `werewolf_night.go`) as a new, separate thread:

- Created once, right after role assignment succeeds in `startPreflight` (same moment the wolf thread's creation is scheduled for night — but this thread is created **once for the whole game**, not once per night). Named e.g. `🏡-thao-luan-<short-id>`.
- Created as a private thread (`discordgo.ChannelTypeGuildPrivateThread`), same "Manage Threads" caveat already accepted for the wolf thread (visible-to-server-admins-with-Manage-Threads caveat carries over unchanged).
- Every player in `JoinOrder` is added via `ThreadMemberAdd` at creation.
- Stored as `game.AliveThreadID` on `werewolfGame`.
- **Membership shrinks on death:** every place that currently sets `p.Alive = false` (lynch in `resolveVote`, wolf-kill in `resolveNight`, AFK-removal in `checkAndHandleGameEnd`, manual leave in `handleLeave`) also calls `s.ThreadMemberRemove(game.AliveThreadID, p.UserID)` for that player, best-effort (log on error, don't fail the phase transition over it).
- **Deleted on every terminal path** alongside the wolf thread (win, cancel, panic-recovery) — same cleanup call site as `WolfThreadID` deletion, just a second `ChannelDelete` for `AliveThreadID`.
- Used only for player chat; the bot does **not** post phase-transition announcements into it — those stay in the main (now-locked) channel, per item 3 below. Players are expected to glance at the main channel for phase state and use the thread purely to talk.

## 3. Alive/dead reminder on every phase transition

Every existing phase-transition message the bot posts in the main channel — night start, day-discuss start, vote start, night resolution, vote resolution — gets one appended line, computed fresh from `game.Players`/`game.JoinOrder` at send time:

```
🟢 Còn sống: A, B, C
💀 Đã chết: D, E
```

("Đã chết" section omitted entirely if nobody has died yet.) This is a small shared helper, e.g. `aliveDeadSummary(game *werewolfGame) string`, called from each announcement site (`runNightPhase`, `runDayDiscuss`, `runDayVote`, `resolveNight`, `resolveVote`) and appended to the existing `Content` string.

## 4. Skip-vote option

The day-vote select menu (`targetSelectMenu`, called from `runDayVote`) gets one extra option appended after the player list: label "Không treo cổ ai", with a fixed sentinel value (e.g. the literal string `"__skip__"`, chosen to never collide with a Discord snowflake user ID).

- `handleVote`'s target-validation (added in the prior fix round) special-cases this sentinel: it's always "valid" regardless of `game.Players` contents, and is recorded into `game.Votes[voterID] = "__skip__"` like any other vote.
- `tallyVotes` needs no logic change — it already tallies by target-string plurality/tie; `"__skip__"` just participates as one more possible "target."
- `resolveVote`'s existing lookup `game.Players[top[0]]` naturally returns `exists == false` for the sentinel, so the "nobody lynched" branch already fires correctly with zero code changes beyond the menu option and the vote-recording special-case.
- The vote-result message's "no one lynched" wording stays the same generic message — it does not need to distinguish "genuine tie" from "skip won" for now (out of scope; not requested).

## 5. New timings

| Phase | Old | New |
|---|---|---|
| Night (seer/doctor solo window) | 90s | **60s** (`nightActionWindow`) |
| Wolf night-discussion window (private thread) | same 90s as above | **60s + 20s × wolfCount** (new `wolfDiscussWindowFor(wolfCount int)`) |
| Day discuss | 90s + 30s×alive | **45s + 15s × alive** (`dayDiscussBase = 45s`, `dayDiscussPerAlive = 15s`) |
| Day vote | 90s | **30s** (`dayVoteWindow`) |
| Pre-flight grace | 60s | unchanged |

**Wolves get their own window, separate from seer/doctor.** Today `nightActionWindow` (90s) is a single constant used as the deadline for all three night roles via `waitForNightActions`/`nightActionsComplete`. Splitting wolves out means `waitForNightActions` needs two deadlines instead of one: it already loops polling `nightActionsComplete` every 2s and returning early the moment all required actions are in (fast-forward-on-all-done, unchanged) — it now takes `wolfDeadline` and `soloDeadline` (for seer+doctor), and treats "time's up" as "past *both* deadlines," while individual role prompts (the DM/thread messages themselves) state their own window length. Practically: since the wolf window is normally ≥ the 60s solo window (60+20×wolfCount ≥ 60 for wolfCount ≥ 1... wait wolfCount=0 is not a valid game state, minimum is 1, giving 80s ≥ 60s), the *effective* wait is just `max(wolfDeadline, soloDeadline)`, computed once up front — no change to the polling loop's shape, just how the single `deadline` value passed into it is computed.

**Fast-forward-on-all-done is preserved and unchanged** for both night actions and voting — this already exists (`waitForNightActions`/`waitForVotes` poll every 2s and return the instant all required actions/votes are in) and needs no new code, just confirmation it still applies with the new deadlines.

## 6. Full end-of-game history recap

**Data model:** add to `werewolfGame`:

```go
type roundLog struct {
    Round           int
    WolfTargetLabel string // "" if no target chosen (shouldn't happen post-preflight, but defensive)
    WolfTargetSaved bool   // true if doctor's protect matched the wolf target
    DoctorTargetLabel string // "" if doctor is dead/absent this round
    SeerTargetLabel   string // "" if seer is dead/absent this round
    SeerResult        role  // zero value meaningless if SeerTargetLabel == ""
    VoteBreakdown     map[string][]string // target label (or "Không treo cổ ai") -> voter labels who chose it
    LynchedLabel      string // "" if nobody was lynched this round
}
```

`game.History []roundLog` accumulates one entry per completed round (both night and day folded into one entry per round number, since they're always paired). Populated incrementally:

- `resolveNight` fills in `WolfTargetLabel`, `WolfTargetSaved`, `DoctorTargetLabel`, `SeerTargetLabel`, `SeerResult` for the current round and appends a new (mostly-empty) `roundLog` to `game.History` — or, simpler: `runNightPhase` appends a fresh `roundLog{Round: game.Round}` at night start, and `resolveNight` fills its night-side fields by mutating `game.History[len(game.History)-1]` (always the current round, since rounds only append forward).
- `resolveVote` fills in `VoteBreakdown` and `LynchedLabel` on that same last entry. `VoteBreakdown` is keyed by *target* label (or "Không treo cổ ai" for the skip sentinel), with the value being every voter's label who chose that target — this is a deliberate choice over a plain `target -> count` tally: the recap must show **who voted for whom**, not just aggregate counts, so the full voter list is the data actually needed, and the count is just `len(voters)`.

**Recap rendering:** `announceGameEnd` (already the single terminal-announcement function) gets a new section appended after the existing final-role reveal, iterating `game.History` in order:

```
📜 **Lịch sử ván đấu**

**Đêm/Ngày 1:**
🐺 Sói cắn: <label> (bị bác sĩ cứu / đã chết)
💉 Bác sĩ bảo vệ: <label>
🔮 Tiên tri xem: <label> → <Sói/Dân Làng>
🗳️ Kết quả bỏ phiếu:
A: 2 (X, Y)
B: 1 (Z)
Không treo cổ ai: 1 (W)
→ A bị treo cổ

**Đêm/Ngày 2:**
...
```

Each vote-tally line names every voter who picked that target, not just the count — this is the point of `VoteBreakdown` over a simple tally map.

Any field that's empty (role already dead / role not in this game's ratio) is omitted from that round's block. If the resulting content exceeds Discord's 2000-char message limit for large games with many rounds, split into multiple `ChannelMessageSendComplex` calls, chunking by whole round boundaries (never mid-round) — there is no existing reusable chunking helper elsewhere in the codebase (the export feature's 2000-char handling in `internal/services/export/formatter.go` is inline and package-private), so this is a small new local helper in `werewolf_day.go`, e.g. `chunkRoundsByMessageLimit(rounds []string, limit int) []string` that greedily packs formatted per-round blocks into ≤`limit`-char message bodies.

**Scope note:** the recap is posted once, at game end, in the main channel (which is about to be unlocked anyway) — not into either thread, since both threads are deleted as part of the same cleanup.

## Testing

- Pure-function additions (`wolfDiscussWindowFor`, the tally-with-skip-sentinel behavior — though `tallyVotes` itself is unchanged) get unit tests following the existing `werewolf_test.go` pattern.
- `roundLog` population and recap formatting: since this is deterministic string/struct building from already-known role/vote data, add focused unit tests that build a `game.History` by hand and assert the pure formatting/chunking helpers (factored out of `announceGameEnd` for testability) produce the expected sections/omissions, including the per-voter `VoteBreakdown` detail — this is new pure logic, not Discord-coupled, so it's testable unlike the rest of the interactive engine.
- Channel-permission lock/restore, thread creation/membership, and the end-to-end timer changes remain manually verified against a real Discord server, per the original design's testing philosophy (interactive Discord I/O isn't unit tested).

## Cleanup on every terminal path

There are two distinct kinds of state this spec adds, and they must both be torn down at the same cleanup point that already deletes `cmd.games[channelID]` and the wolf thread — but for different reasons, and the primary one is **not** Discord:

**1. Bot-process (VPS) memory — the primary concern.** `game.History` (a slice that grows one entry per round, holding labels/maps/role values for every night and day of the game) and every other field on `werewolfGame` live in the Go process's own heap on the machine running the bot, not on Discord's servers. A game that never gets `delete()`d from `cmd.games` is a real, unbounded memory leak in the bot's own process — a bug that grows worse the longer the bot runs and the more games are played, eventually pressuring the VPS's memory and potentially affecting every other feature (`/duel`, `/export`, etc.) sharing that process. The existing `delete(cmd.games, channelID)` call is what actually fixes this: once a `*werewolfGame` is unreachable from `cmd.games`, Go's garbage collector reclaims its entire heap allocation — `History` included — with no further action needed. **This is the cleanup step with the highest actual cost if it's ever skipped on a code path**, since a leaked game isn't just one struct: it's every round's history, every player, every map, retained in VPS RAM for as long as the bot process runs.
- **`game.History` must never be persisted anywhere** (no DB, no file) — it only ever gets read once, by `announceGameEnd`, immediately before the struct becomes unreachable and eligible for GC. Persisting it would defeat the point (still in-memory-only by design) and would itself become a second, disk-backed leak.

**2. Discord-side artifacts — a distinct, still-critical concern, but not a memory leak.** These live on Discord's servers, not the VPS, so they don't grow the bot process's memory footprint — but leaving them behind is still a real user-facing incident:
- **Channel permission overwrite** (item 1) — restored from `PriorChannelOverwrite` (or deleted if it was nil). A game that ends without this running leaves a Discord channel permanently muted for `@everyone`.
- **Alive-players thread** (item 2) — `game.AliveThreadID` deleted via `ChannelDelete`, at the same call site that already deletes `game.WolfThreadID`. An orphaned thread doesn't leak VPS memory, but it does clutter the server and confuse players about whether a game is still running.

Both categories must be handled on **all three terminal paths** identified in the original spec and confirmed present in the current code: normal win (`checkAndHandleGameEnd`), lobby cancellation (`handleCancelLobby`), and the panic-recovery handler in `startPreflight`. The implementation plan must show the channel-permission restore, the second `ChannelDelete` call, and the `delete(cmd.games, channelID)` all present at each of those three sites, not just one of them — the panic-recovery path in particular is easy to forget since it was added in a later fix round, after the other two cleanup patterns were already established at the other two sites. Confirm via code inspection (not just testing) that no code path can return out of `startPreflight`, `checkAndHandleGameEnd`, or `handleCancelLobby` without reaching its `delete(cmd.games, channelID)` call — that line is the one that actually frees the VPS memory; the Discord-side cleanup calls are important but secondary to it.

## Out of scope

- Distinguishing "genuine tie" from "skip won" in the vote-result wording.
- Any UI to let an admin manually re-open the channel without the game ending (if the lock ever gets stuck, that's a bug to fix, not a feature to add).
- Persisting history across bot restarts — still fully in-memory, lost with the rest of the game state on crash/restart, consistent with the original design.
