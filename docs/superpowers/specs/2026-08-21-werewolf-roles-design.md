# Werewolf Extended Roles — Design Spec

Date: 2026-08-21
Status: Approved
Builds on: `docs/superpowers/specs/2026-08-20-werewolf-design.md` (original feature) and `docs/superpowers/specs/2026-08-21-werewolf-enhancements-design.md` (channel lock, threads, timers, recap — already shipped)

## Context

The user provided a complete, working reference implementation (a Vue "pass-the-phone" local game-master app) covering 11 roles, their exact mechanics, a night-action order, win condition, and a manual role-count editor with a live "balance score" indicator. This spec ports that reference's mechanics into our async, Discord-native `/werewolf` engine (Go, discordgo), adapting anywhere the reference's single-shared-device assumptions don't apply (notably: no "decoy timing" is needed, since our DMs/threads are already private per-player; but Witch gains a real sequencing dependency that didn't exist before).

Ten new roles are added on top of the two currently implemented (Werewolf, Seer — the existing "Doctor" is kept as the same role, redocumented as "Guard/Bảo vệ" to match the reference's naming and point value):

**Wolf-side:** Sói con (Wolf Cub), Sói nguyền (Cursed Wolf), Kẻ phản bội (Traitor)
**Villager-side:** Phù thủy (Witch), Thợ săn (Hunter), Kẻ phá hoại (Disruptor), Thần tình yêu (Cupid)
**Already implemented, renamed only:** Bác Sĩ → also documented as Guard/Bảo vệ (no code change — same mechanic, same struct field, this is purely a naming/points-table entry)
**Unchanged:** Werewolf (Sói thường), Villager (Dân thường), Seer (Tiên tri)

## 1. Faction model

Add `type faction int` with `factionVillager`/`factionWolf`, and a `Faction faction` field on `player`, computed once at role assignment and mutated exactly once for Cursed Wolf (on conversion). `checkWinCondition` is rewritten to count by `p.Faction` instead of `p.Role == roleWerewolf` — this is the only change to win-condition *logic*; the `aliveWolves == 0` / `aliveWolves >= aliveOthers` comparison itself is unchanged.

Faction assignment at role-deal time:
- `roleWerewolf`, `roleWolfCub`, `roleTraitor` → `factionWolf`
- everything else, including `roleCursedWolf` → `factionVillager`

`roleCursedWolf`'s `Faction` flips to `factionWolf` the moment it converts (see section 4). No other role ever changes faction.

## 2. Role point values (manual-pick UI only — not used by the fixed table's runtime logic)

| Role | Points | Role | Points |
|---|---|---|---|
| Sói thường | -6 | Dân thường | +1 |
| Sói con | -8 | Tiên tri | +3 |
| Sói nguyền | -4 | Bảo vệ (Bác Sĩ) | +3 |
| Kẻ phản bội | -6 | Phù thủy | +4 |
| | | Thợ săn | +3 |
| | | Kẻ phá hoại | -2 |
| | | Thần tình yêu | -3 |

A constant `map[role]int` in the codebase, e.g. `rolePoints`, used only to compute the manual-pick UI's live balance readout (`Math.abs(score) <= 3-5` reads as "balanced" — exact threshold decided in the plan, not load-bearing here).

## 3. Two role-selection modes

### 3a. Default: extended fixed table

`rolesForPlayerCount(n)` is extended to cover **5-20 players** (raising `werewolfMaxPlayers` from 12 to 20), composed **only** from Werewolf, Villager, Seer, Guard, Witch, Hunter, Disruptor — never Wolf Cub, Cursed Wolf, Traitor, or Cupid (matching the reference's own preset behavior, where those four never appear in a built-in bracket either). The exact table is authored in the implementation plan with real numbers per player count, following the balance philosophy (each row's point sum kept small) but is a fixed lookup, not a runtime search — same architecture as today, just a bigger table and a richer `roleRatio` struct (gains `Witches`, `Hunters`, `Disruptors` fields alongside the existing `Werewolves`/`Seers`/`Doctors`).

### 3b. New: manual role-pick mode

`/werewolf start` gains a new option, `custom-roles: bool` (default false). When true, after the starter clicks "Bắt đầu" (roster now frozen), instead of immediately calling `rolesForPlayerCount` and dealing roles, the bot shows the starter an ephemeral role-count editor:

- Two ephemeral messages (to stay under Discord's 25-component-per-message limit): one for the 4 wolf-side roles (Werewolf, Wolf Cub, Cursed Wolf, Traitor), one for the 7 villager-side roles (Villager, Seer, Guard, Witch, Hunter, Disruptor, Cupid) — mirroring the reference's "Phe Sói" / "Phe Dân" split.
- Each role gets a row: label, current count, −/+ buttons (Discord buttons, not free-text input — no modal needed since counts are small integers).
- A running total and a live balance-score line, computed from `rolePoints`, shown in the message content (edited in place on every click, same `InteractionResponseUpdateMessage` pattern used for the existing lobby).
- A "Xác nhận" (Confirm) button, enabled only when `sum(counts) == playerCount` and `wolfSideCount >= 1` and `villagerSideCount >= 1` — same validation shape as `setupValid` in the reference.
- On confirm, this custom `roleRatio`-equivalent (a `map[role]int`) is used instead of `rolesForPlayerCount`'s output for this game's role assignment; everything downstream (shuffle, deal, DM, night order) is identical regardless of which mode produced the counts.
- This editor state (`pendingRoleCounts map[role]int`) lives on the `werewolfGame` struct only for the duration of this one confirmation step — once confirmed, the game proceeds exactly like the fixed-table path and this field is no longer read. Abandoning the editor (closing Discord, going AFK) needs a fallback: **out of scope for this spec** — the plan should decide a sane timeout (e.g. falls back to the fixed table, or the existing `preflightGracePeriod`-style abort path) but this doc doesn't mandate one.

## 4. Night order and the new sequencing requirement

Current engine sends Wolf-thread + Seer-DM + Doctor-DM prompts **in parallel**, then waits once. This spec adds five more actors (Wolf Cub folds into the existing wolf vote — no separate prompt; Cursed Wolf has no action; Traitor's info-DM is not a nightly action — see 4c; Witch, Hunter, Disruptor, Cupid are new prompts).

**The only real ordering dependency:** Witch must know the wolves' resolved bite target before deciding whether to save. Every other new role (Hunter's revenge-target pick, Disruptor's mute-target pick, Cupid's night-1 pairing) has no such dependency and is sent in parallel with the wolf vote, same as Seer/Guard today.

Revised per-round flow:
1. **Parallel, as today plus new prompts:** wolf-thread vote, Seer DM, Guard DM, Hunter DM (skip if Hunter already dead — no DM to a dead player), Disruptor DM (skip if dead). Cupid DM only on round 1 (skip every subsequent round — one-time role).
2. Wait for all of the above (existing `waitForNightActions`/wolf-window logic, extended to also gate on Hunter/Disruptor if alive — they're optional actors so a miss is just an AFK strike like Seer/Guard today, not a blocker).
3. Resolve the wolf vote's target(s) (section 4a) — extended to check Cursed Wolf conversion before applying death (section 4b).
4. **Sequential, new:** send Witch her DM now that the bite target is known; wait for her response (or the (shorter, since it's a single fast decision) timeout).
5. Apply Witch's save/poison, apply Hunter's revenge-chain if Hunter died this round (wolf-killed or poisoned), apply Cupid's lover-death chain if either lover died, apply Disruptor's mute for tomorrow's day-discuss, check win condition, announce, proceed to day.

### 4a. Wolf pack voting (replaces first-click-wins)

Today's `handleWolfTarget` is first-click-wins: whoever votes first locks the pack's target for the round, and other wolves' differing clicks are silently rejected. With player counts going up to 20 (packs of 4-6+ wolves), this stops being fair. Replace it with a real plurality vote:

- `game.WolfVotes map[string]string` (wolf userID → target userID), replacing the single `game.NightTarget string`. Any wolf can vote or change their vote any time before the deadline (`handleWolfTarget` always overwrites, never rejects a re-vote).
- After every vote, the bot edits a single message in the wolf thread (the same message the target-select menu is attached to) to show a live breakdown: per-target vote count, and a per-wolf list of who's currently voted for whom — matching the reference's transparency level, but scoped to the private wolf thread instead of a shared screen, so nothing leaks outside it.
- At resolution: tally `game.WolfVotes`. The target(s) with the most votes win — normally 1 target, or 2 if the Wolf-Cub bonus bite is active this round (section 4b's flag), in which case the top 2 distinct targets by vote count are both attacked.
- **Tie handling:** if the top vote count is shared by more than the number of targets needed (i.e., a genuine tie for the last kill slot), that slot produces no kill — consistent with how the existing day-lynch vote already treats ties as "nobody dies." (With the normal 1-target case, any tie for first place means no kill at all that night.)
- The existing `nightActionsComplete`/AFK-strike logic for wolves changes from "did `NightTarget` get set" to "did every alive wolf cast a vote" — a wolf who never votes this round gets an AFK strike, same as today's semantics, just keyed off the new map instead of the single string.

### 4b. Wolf Cub
On `roleWolfCub`'s death (any cause — wolf-kill doesn't apply to itself, so this means poison/lynch/AFK/leave/hunter-revenge), set a one-round flag (`game.WolfBonusBiteNextNight bool`) consumed and cleared at the start of the *next* night's wolf-vote resolution, allowing that round's tally (section 4a) to select the **top 2** targets instead of 1.

### 4c. Cursed Wolf
Starts `factionVillager`. Seer checks against `p.Faction` (not `p.Role`), so pre-conversion the Seer sees "not wolf". If one of the wolf vote's resolved targets is a `roleCursedWolf` player **and** they are not protected (Guard) and not saved (Witch) that round: instead of dying, `p.Faction = factionWolf` — no death, no history "killed" entry, but the history log should note the conversion happened (new `roundLog.CursedWolfConverted string` field, player label). If they *are* protected/saved, nothing happens (no conversion, no death) — same as the reference.

### 4d. Traitor
No nightly action. On role assignment, if any `roleTraitor` player exists, DM them once with the current wolf roster (Werewolf + Wolf Cub labels — **not** Cursed Wolf, matching the reference's filter, since Cursed Wolf isn't "really" a wolf until conversion). Re-DM only when the wolf roster changes — i.e., after a Cursed Wolf conversion (section 4c) or after any wolf-faction player's `Alive` flips to false. This requires a small helper called from every death-application site, but only actually sends a DM when a live Traitor exists and the previous/current wolf-roster label sets differ.

Seer checks a `roleTraitor` player as "is wolf" (their `Faction` is `factionWolf`, so this falls out of the existing Seer-check-by-faction logic with no special-casing).

### 4e. Witch
See section 4 (night-order) for the sequencing. DM content: "Sói cắn: **{label(s)}** đêm nay" (listing both targets if the Wolf-Cub bonus bite is active, or "Không ai bị cắn" if the vote produced no valid target) plus two buttons: "💊 Cứu" (only enabled/shown if `!WitchHealUsed` and there was at least one valid bite target — heals exactly one of the bitten targets if there are two) and "☠️ Đầu độc" (opens a target-select menu of all alive players except herself, only if `!WitchPoisonUsed`), plus a "Không làm gì" option. Poison **bypasses** Guard's protection (applies unconditionally once chosen) — the wolf-bite death does not. Both potions are single-use per game (`game.WitchHealUsed`, `game.WitchPoisonUsed bool`), never reset.

### 4f. Hunter
Nightly DM (while alive) with a target-select menu; re-picking overwrites the previous pick (`game.HunterTarget string`, persists across rounds until changed — matches the reference). On Hunter's death by **any** cause (wolf-kill, poison, lynch, AFK-removal, voluntary leave), if `game.HunterTarget` is set and that player is currently alive, they die too (reason: revenge-kill), applied in the same resolution step/message as the triggering death — including the day-vote lynch case (per your answer: immediate, not delayed to "next morning").

### 4g. Disruptor
Nightly DM (while alive) with a target-select menu (any alive player except self). The chosen player is muted in the alive-discussion thread for the *next* day-discuss phase only — a **per-member** thread permission overwrite (`PermissionOverwriteTypeMember`, not the existing `@everyone`-scoped overwrite `muteAliveThread` uses), applied when day-discuss starts and removed when it ends (vote phase re-mutes everyone via the existing `@everyone` overwrite anyway, so the per-member overwrite only matters during the one day-discuss window it targets). `game.DisruptorTarget string`, reset to `""` each night before the new pick.

### 4h. Cupid
DMs sent only on round 1, to every living player pair-selection... no — DMed once to the Cupid player only, with a 2-target select flow (pick target 1, then target 2, matching the reference's click-to-select-then-click-again-to-deselect UX, adapted to two sequential single-select Discord menus or one multi-select menu with `MaxValues: 2`). On confirm, `game.LoverIDs [2]string` is set and **both lovers are DMed** who their partner is (per your answer — deviating from the reference's "secret" house rule). Lover-death chain: wherever a player's `Alive` flips to `false` (the same four sites Task 7 already touches for alive-thread removal — lynch, wolf-kill/poison, AFK-removal, manual leave), after applying that death, check if the dying player has a living lover via `game.LoverIDs`; if so, apply a second death (reason: lover-death) to that partner, in the same resolution step. Chains only one level deep (a lover's lover-death does not itself have a lover, since `LoverIDs` only ever names the original two).

### 4i. Public per-role progress notifications

Every night, as each acting role finishes deciding, the bot posts a short, identity-free progress line to the **main channel** (not the wolf thread, not DMs) — e.g. "🛡️ Bảo vệ đã quyết định xong.", "🐺 Bầy Sói đã quyết định xong.", "🧙 Phù thủy đã quyết định xong.", "🏹 Thợ săn đã quyết định xong.", "🤫 Kẻ phá hoại đã quyết định xong.", "💘 Thần tình yêu đã quyết định xong." — never naming the acting player or revealing their choice, so everyone in the (locked, bot-only) main channel can watch night progress without waiting blindly, while the actual decision stays private to the DM/thread it was made in.

"Decided" is the same moment each role's *existing* private confirmation already fires, so this is one extra `ChannelMessageSendComplex` call (`AllowedMentions: &discordgo.MessageAllowedMentions{}`, no mentions in the text) added alongside each role's current ephemeral/DM confirmation, not a new tracking mechanism:

- **Wolf pack:** since votes are changeable anytime (section 4a), there's no single "locked" moment anymore — post once, the moment every alive wolf has cast at least one vote (i.e., the same condition `nightActionsComplete` now uses for the wolf-vote-complete check). Not re-posted if a wolf changes their vote afterward.
- **Seer:** on `handleSeerCheck` success.
- **Guard:** on `handleDoctorProtect`/Guard-equivalent success.
- **Witch:** on her explicit confirm (heal / poison / "không làm gì" — any of the three finalizes and posts).
- **Hunter:** on the *first* successful target pick this round (subsequent silent re-picks the same round do not re-post — avoids spamming the channel every time Hunter changes their mind).
- **Disruptor:** same first-pick-this-round rule as Hunter.
- **Cupid:** on confirm, round 1 only (never posts again — the role has no action after night 1).
- **Traitor:** no notification — they have no nightly decision to make, only the one-time (or roster-change) info DM from section 4c.

This does not change any timing/AFK-strike logic — it's purely an additional public announcement layered on top of the existing private confirmation flow for each role.

## 5. Recap extensions

`roundLog` (already exists, populated by `resolveNight`/`resolveVote`) gains:
- `WolfBonusBite bool` — this round used the Wolf-Cub 2-target bite.
- `CursedWolfConverted string` — label, if a conversion happened this round.
- `WitchSavedLabel string`, `WitchPoisonedLabel string`.
- `HunterRevengeLabel string` — who the Hunter took down with them, if triggered this round (night or day).
- `DisruptorMutedLabel string` — who was muted for the following day-discuss.
- `LoverDeathLabel string` — who died from the lover-chain this round, if any.

`formatRoundBlock` (pure, already unit-tested) gets one new conditional line per field, same omit-if-empty pattern as today.

## 6. `/help werewolf` interaction guide

Extend the existing `/help werewolf` embed (or add a field/second embed if it grows too large for one) with a per-role breakdown answering, for every role: **when does the bot DM you, and where do you chat/act** — e.g.:

> 🐺 **Sói / Sói con / Sói nguyền (sau khi lộ diện):** Hành động trong thread riêng của bầy Sói mỗi đêm.
> 🔮 **Tiên tri / 🛡️ Bảo vệ / 🧙 Phù thủy / 🏹 Thợ săn / 🤫 Kẻ phá hoại:** Bot nhắn tin riêng (DM) mỗi đêm để bạn chọn hành động.
> 💘 **Thần tình yêu:** Bot nhắn tin riêng chỉ đêm đầu tiên.
> 🎭 **Kẻ phản bội:** Bot nhắn tin riêng cho biết ai là Sói khi bắt đầu ván (và mỗi khi danh sách Sói thay đổi) — không có hành động ban đêm.
> 👤 **Dân thường:** Không có hành động riêng — thảo luận cùng mọi người ban ngày.
> Mọi người thảo luận công khai tại thread "🏡 Thảo luận" ban ngày; kênh chính chỉ dùng để xem thông báo pha (đêm/ngày/bỏ phiếu) và bỏ phiếu.

This is documentation-only, following the same pattern as the existing `/help werewolf` update from the enhancements round.

## 7. Cleanup on every terminal path (carried-forward requirement)

Every new piece of per-game state this spec adds (`WolfBonusBiteNextNight`, `WitchHealUsed`/`WitchPoisonUsed`, `HunterTarget`, `DisruptorTarget`, `LoverIDs`, the manual-pick editor's transient `pendingRoleCounts`) lives on `werewolfGame` and is freed automatically by the existing `delete(cmd.games, channelID)` on every terminal path — no new Discord-side artifacts to clean up (no new threads or channel overwrites *except* Disruptor's per-member thread overwrite, which must be explicitly removed once the muted day-discuss window ends, and — defensively — on every terminal path too, in case a game ends mid-mute).

## Out of scope

- The manual-pick editor's abandon/timeout behavior (flagged in 3b — plan must decide, not mandated here).
- Any change to the existing fixed-table player counts below 12 (5-12 stays close to today's shape; only 13-20 are genuinely new rows).
- Persisting any of this across bot restarts — still fully in-memory.
- A "reveal all roles including who was Traitor/Cursed-Wolf's original faction" recap nuance beyond what section 5 already lists — the existing final role-reveal already shows everyone's `Role`, which is sufficient.
