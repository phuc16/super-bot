# Werewolf (Ma Sói) — Design Spec

Status: Approved for planning
Date: 2026-08-20
Branch: `feature/werewolf`

## Summary

A Discord party-game feature (`/werewolf`) implementing a simplified Werewolf/Mafia
ruleset: Werewolves, Villagers, Seer, Doctor. Runs entirely in-memory per channel,
following the same architectural conventions established by `/duel-challenge-all`
(mutex-guarded pending state, a single driving goroutine walking through phases,
Discord buttons/select menus/modals for interaction, live countdown edits).

## Non-goals (v1)

- No persistence across bot restarts (see "Persistence" below).
- No additional roles beyond Werewolf/Villager/Seer/Doctor (no Hunter/Witch/Cupid).
- No configurable role ratios — fixed table by player count.
- No game history/stats/leaderboard.
- No `/werewolf-history` — out of scope, revisit if the game proves popular.

## Persistence

**In-memory only**, matching `/duel-challenge-all`. A bot restart mid-game silently
loses all game state (no resume, no automatic cancellation message — the last
message in the channel simply stops updating). This is an explicit, accepted
trade-off: the alternative (serializing full game state — roles, alive/dead,
phase, pending night actions — and reconstructing Discord message/button state on
boot) is a meaningfully bigger build than v1 warrants. Same operational discipline
as tournaments applies: don't redeploy the bot while a Werewolf game is running.
This must be documented in `/help werewolf` exactly like the `/duel-challenge-all`
in-memory caveat.

## Data Model

New package `internal/discord/commands/werewolf.go` (split into
`werewolf_night.go` / `werewolf_vote.go` if it grows large — no fixed line
threshold, split when a single file stops being easy to hold in context).

```go
type WerewolfCommand struct {
    mu    sync.Mutex
    games map[string]*werewolfGame // keyed by channelID — one game per channel at a time
}

type gamePhase int

const (
    phaseLobby gamePhase = iota
    phaseNight
    phaseDayDiscuss
    phaseDayVote
    phaseEnded
)

type role int

const (
    roleVillager role = iota
    roleWerewolf
    roleSeer
    roleDoctor
)

type player struct {
    UserID string
    Label  string
    Role   role
    Alive  bool
}

type werewolfGame struct {
    GuildID, ChannelID, StarterID string
    MessageID      string
    RevealOnDeath  bool
    Phase          gamePhase
    Round          int
    Players        map[string]*player // userID -> player
    JoinOrder      []string            // lobby join order; frozen once game starts
    WolfThreadID   string
    NightTarget    string              // wolf-chosen kill target, this round
    SeerChecked    map[string]bool     // who the seer has already checked (cumulative)
    DoctorProtect  string              // doctor's pick this round
    DoctorLastPick string              // last round's pick, can't repeat back-to-back
    Votes          map[string]string   // voterID -> targetID, reset each day
    AFKStrikes     map[string]int
}
```

No new DB tables. No changes to any existing model/migration.

## Roles & Win Conditions

Role ratio table (5-12 players):

| Players | Werewolves | Seer | Doctor | Villagers |
|---|---|---|---|---|
| 5 | 1 | — | — | 4 |
| 6 | 1 | 1 | — | 4 |
| 7-8 | 2 | 1 | 1 | rest |
| 9-10 | 2 | 1 | 1 | rest |
| 11-12 | 3 | 1 | 1 | rest |

Seer/Doctor only appear once there are enough Villagers to make them meaningful
(6+ players). Wolf count follows roughly ⅓-of-players, rounded so Villagers always
start in the majority.

**Win conditions**, checked after every death (night kill, day lynch, or
AFK-removal):
- **Villagers win** when all werewolves are dead.
- **Werewolves win** when werewolves ≥ remaining non-wolves (they can no longer
  be out-voted).

Role assignment: shuffle `JoinOrder`, deal roles per the table, DM each player
their role privately as part of the pre-flight DM check (see below).

## Phase Flow

```
LOBBY → (pre-flight DM check) → NIGHT → DAY_DISCUSS → DAY_VOTE → (win check) → NIGHT → ...
                                                                        ↓ win check true
                                                                      ENDED
```

### LOBBY
`/werewolf start reveal-on-death:true|false` (reveal-on-death defaults to `true`).
Starter/admin only to start the command; anyone can Join/Leave (same permission
split as `/duel-challenge-all`). Posts a message with **Tham gia** / **Rời đi** /
**Bắt đầu** (starter-only, enabled once ≥5 joined) / **Huỷ** (starter-only)
buttons. No fixed signup timer — the starter manually kicks things off once ready
(unlike the tournament's scheduled-time model; Werewolf is meant to be played
right now, not scheduled ahead).

Errors if a game is already running in that channel (one game per channel).
Joining/leaving closes the instant the starter clicks "Bắt đầu" — `JoinOrder` is
frozen at that point and the Join/Leave buttons are disabled on the lobby
message.

### Pre-flight DM check
Once the starter clicks "Bắt đầu": roles are assigned, and the bot attempts to DM
every player their role. Anyone unreachable is called out by name in the lobby
channel with a 60s grace window to fix their DM privacy settings, then the bot
retries. Still unreachable after the grace window → that player is dropped and
role ratios are recalculated for the smaller player count; if that drops the
game below 5 players, the game is cancelled with an explanatory message.

### NIGHT (round N)
- **Werewolves**: bot creates a **private Discord thread**, adds all living
  wolves, posts a target-picker (select menu). Resolves early once a target is
  chosen (first submitted pick stands if there's disagreement — simple
  "whoever clicks first" resolution, no formal wolf-vote-counting).
- **Seer**: DMed a target-picker independently; learns whether the target is a
  werewolf or not. Resolves as soon as they act.
- **Doctor**: DMed a target-picker independently; can't protect the same
  player two nights in a row (`DoctorLastPick` check). Resolves as soon as they
  act.
- All three run concurrently. The phase ends the moment every required actor
  has acted (early-resolve, "approach B"), or a 90s backstop timer elapses,
  whichever comes first. Countdown shown via edits in the wolf thread / DMs,
  reusing the same `countdownTick`-style pattern as `/duel`.
- A player who doesn't act by the timer gets an AFK strike; their action
  defaults to "skip" (no kill target / no check / no protect for that role,
  respectively, if that specific role-holder was the one who missed it).

**Privacy note on the wolf thread**: Discord private threads are invisible to
regular members not added to them, so villagers/seer/doctor genuinely can't see
wolf coordination. However, anyone with the **"Manage Threads"** server
permission (typically admins/mods) can still see and join private threads even
without being added — this is inherent to Discord's permission model and cannot
be overridden by the bot for a temporary game thread. Accepted trade-off,
confirmed by user: fine for a casual friend-group game where admins are trusted
anyway.

**Fallback**: if wolf-thread creation fails (Discord API error), fall back to
independent DM target-pickers for the wolves that night only (first submitted
pick stands), rather than crashing the game.

### DAY_DISCUSS
Public message reveals the night's outcome: who died (name, and role if
`RevealOnDeath` is true) or "nobody died" if the doctor's protect succeeded.
Fixed 3-minute timer, no early-resolve (discussion benefits from the full
window regardless of activity).

### DAY_VOTE
Public message with a select menu listing living players; one vote per living
player, changeable until the phase resolves. Early-resolves once all living
players have voted, 90s backstop timer otherwise. A tie (two or more players
tied for most votes) → **nobody is lynched**, day ends, straight to next
night. AFK non-voters get a strike.

Win check runs after every death (night kill, day lynch, or AFK-removal). If
neither side has won, loop back to NIGHT (round N+1). If a side has won, phase
becomes `phaseEnded`: post the win announcement (with a full role reveal for
every player regardless of the `RevealOnDeath` setting, since the game is over),
archive/delete the wolf thread if one still exists, and **immediately delete the
game's entry from `WerewolfCommand.games`**. This is the only cleanup point —
once the map entry is removed, the entire `werewolfGame` (players, votes, AFK
strikes, night-action state) becomes unreferenced and is garbage collected by
Go normally. The same immediate-delete-from-map step also applies to a
cancelled lobby (not enough players after the DM-check grace window, or the
starter clicking Huỷ) — a game must never remain reachable via `games` past the
point it stops being playable, so memory doesn't grow across repeated games
over the bot's uptime.

### AFK & mid-game leave
2 consecutive AFK strikes (missed night action or missed vote, back-to-back)
→ that player is auto-removed from the game, treated exactly like a death
(including the `RevealOnDeath` setting) and immediately triggers a win-condition
check. A manual `/werewolf leave` command follows the identical removal path.

## Commands & Interactions

- `/werewolf start reveal-on-death:true|false` — starts a lobby in the current
  channel.
- Lobby buttons: Tham gia / Rời đi / Bắt đầu (starter-only) / Huỷ (starter-only).
- Night actions: no slash commands — DM/thread buttons and select menus only.
- Day vote: public select menu (same pattern as the tournament's champion
  picker in `duel_challenge_all.go`).
- `/werewolf status` — ephemeral, shows current phase/round/alive-count without
  leaking roles.
- `/werewolf leave` — voluntarily leave a game you're in (same removal path as
  AFK-out).

## Error Handling (consolidated)

| Situation | Behavior |
|---|---|
| DM-blocked player at game start | 60s grace + retry; drop-and-recalculate ratios if still unreachable; cancel game if that drops below 5 players |
| AFK during night/vote | Auto-skip that action; 2 consecutive AFKs → auto-removed (treated as death) |
| Tie vote | Nobody lynched |
| Mid-game manual leave | Same removal path as AFK-out |
| Bot restart mid-game | Game silently lost — accepted trade-off, documented in `/help werewolf` |
| Wolf-thread creation fails | Fall back to independent DM target-pickers for that night only |
| Other Discord API failures (DM send fails mid-game, etc.) | Logged via `zerolog`, game continues with graceful fallback where possible rather than crashing |

## Testing Approach

No way to unit-test Discord button clicks/DMs directly, so:

- **Unit tests** for pure logic: role-ratio table lookup, win-condition check,
  vote-tally/tie detection, AFK-strike counting — all plain functions taking a
  game state and returning a result, fully testable without Discord.
- **Manual end-to-end testing** in a real Discord server (same as every other
  feature built this session): a full 5-player game (no Seer/Doctor, single
  wolf, exercises the minimal path) and a full 8+ player game (exercises
  Seer/Doctor and the multi-wolf thread), verifying each phase transition,
  early-resolve vs. timer-backstop behavior, and both win conditions.
