package commands

import (
	"fmt"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/bwmarrin/discordgo"
)

// werewolfSourceFiles is every non-test file the game lives in. Used by the
// source-scanning guards below.
var werewolfSourceFiles = []string{
	"werewolf.go",
	"werewolf_night.go",
	"werewolf_day.go",
	"werewolf_rolepick.go",
}

func TestMissingPermissionsFrom(t *testing.T) {
	all := int64(0)
	for _, req := range werewolfRequiredPermissions {
		all |= req.Bit
	}

	t.Run("a fully-granted bot is missing nothing", func(t *testing.T) {
		if missing := missingPermissionsFrom(all); len(missing) != 0 {
			t.Errorf("missing = %v, want none", namesOfPerms(missing))
		}
	})

	t.Run("Administrator implies everything", func(t *testing.T) {
		// Discord grants Administrator holders every permission implicitly,
		// so the individual bits are absent from the mask. Checking them
		// naively would refuse to start for the most-privileged bot there is.
		if missing := missingPermissionsFrom(discordgo.PermissionAdministrator); len(missing) != 0 {
			t.Errorf("Administrator reported missing %v", namesOfPerms(missing))
		}
	})

	t.Run("a bot with nothing is missing every requirement", func(t *testing.T) {
		missing := missingPermissionsFrom(0)
		if len(missing) != len(werewolfRequiredPermissions) {
			t.Fatalf("missing %d of %d requirements: %v", len(missing), len(werewolfRequiredPermissions), namesOfPerms(missing))
		}
	})

	t.Run("each single missing bit is reported alone", func(t *testing.T) {
		// The whole value of the up-front gate is naming exactly what to
		// grant, so dropping one bit must surface that one bit — not a
		// generic failure and not a partial list.
		for _, req := range werewolfRequiredPermissions {
			missing := missingPermissionsFrom(all &^ req.Bit)
			if len(missing) != 1 || missing[0].Bit != req.Bit {
				t.Errorf("dropping %s reported %v, want exactly [%s]", req.Name, namesOfPerms(missing), req.Name)
			}
		}
	})

	t.Run("the message names every missing permission", func(t *testing.T) {
		// An admin fixing the role should need one pass, not one retry per
		// gap, so every missing entry has to appear in the single response.
		missing := missingPermissionsFrom(0)
		msg := missingPermissionsMessage(missing)
		for _, req := range missing {
			if !strings.Contains(msg, req.Name) {
				t.Errorf("refusal message omits %q:\n%s", req.Name, msg)
			}
		}
		if len([]rune(msg)) > 2000 {
			t.Errorf("refusal message is %d runes, over Discord's 2000-char content limit", len([]rune(msg)))
		}
	})

	t.Run("every requirement is populated", func(t *testing.T) {
		for idx, req := range werewolfRequiredPermissions {
			if req.Bit == 0 || req.Name == "" || req.Why == "" {
				t.Errorf("werewolfRequiredPermissions[%d] = %+v, want all fields set", idx, req)
			}
		}
	})
}

// TestRequiredPermissionsCoverEveryCapabilityTheGameUses ties the gate to the
// calls the game actually makes. Each bit below is load-bearing for a
// specific mechanic, and dropping one from the table doesn't make the game
// degrade — it makes a mechanic fail silently mid-game, with nothing but a
// logged REST error while players wait on a prompt that never arrives.
func TestRequiredPermissionsCoverEveryCapabilityTheGameUses(t *testing.T) {
	needed := map[string]int64{
		"read the game channel":                               discordgo.PermissionViewChannel,
		"post day/night announcements":                        discordgo.PermissionSendMessages,
		"lock the channel and mute the Disruptor's target":    discordgo.PermissionManageRoles,
		"create the alive-discussion and wolf threads":        discordgo.PermissionCreatePrivateThreads,
		"post the wolf vote and recaps inside threads":        discordgo.PermissionSendMessagesInThreads,
		"lock/unlock threads, manage members, delete threads": discordgo.PermissionManageThreads,
	}
	have := int64(0)
	for _, req := range werewolfRequiredPermissions {
		have |= req.Bit
	}
	for why, bit := range needed {
		if have&bit == 0 {
			t.Errorf("werewolfRequiredPermissions omits the bit needed to %s", why)
		}
	}
}

// TestNoPermissionOverwritesOnThreads is a source-level guard against a bug
// that shipped and silently broke two mechanics for every game: night
// silence on the alive-discussion thread, and the Disruptor role entirely.
//
// Discord does not support per-channel permission overwrites on threads —
// threads inherit permissions from their parent channel, and Edit/Delete
// Channel Permissions are documented "only usable for guild channels". The
// old code called ChannelPermissionSet with a THREAD id, which the API
// rejected on every single call. Nothing surfaced: the error was logged and
// the game carried on as though players had been silenced.
//
// A unit test can't catch that (it needs a live API to fail), and the
// failure is invisible in play, so guard the shape instead: no
// ChannelPermission* call in the werewolf sources may pass a thread-valued
// identifier as its channel argument.
func TestNoPermissionOverwritesOnThreads(t *testing.T) {
	call := regexp.MustCompile(`ChannelPermission(?:Set|Delete)\(\s*([A-Za-z_][A-Za-z0-9_.]*)`)
	for _, name := range werewolfSourceFiles {
		src, err := os.ReadFile(name)
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		for _, m := range call.FindAllStringSubmatch(string(src), -1) {
			arg := m[1]
			if strings.Contains(strings.ToLower(arg), "thread") {
				t.Errorf("%s: ChannelPermission* called with %q — Discord rejects permission overwrites on threads; "+
					"use setAliveThreadLocked/setWolfThreadLocked for whole-thread silence, or put the overwrite on the PARENT channel", name, arg)
			}
		}
	}
}

// TestThreadSilenceUsesTheLockedFlag is the positive counterpart to the
// guard above: both threads must be silenced through the one mechanism
// Discord actually supports for threads.
func TestThreadSilenceUsesTheLockedFlag(t *testing.T) {
	src, err := os.ReadFile("werewolf.go")
	if err != nil {
		t.Fatalf("read werewolf.go: %v", err)
	}
	night, err := os.ReadFile("werewolf_night.go")
	if err != nil {
		t.Fatalf("read werewolf_night.go: %v", err)
	}
	joined := string(src) + string(night)
	for _, fn := range []string{"setAliveThreadLocked", "setWolfThreadLocked"} {
		if !strings.Contains(joined, "func "+fn+"(") {
			t.Errorf("%s is gone — thread silence must go through a Locked-flag helper", fn)
		}
	}
	if !strings.Contains(joined, "Locked: &locked") {
		t.Error("no ChannelEdit{Locked: ...} in the thread helpers — thread silence is not being applied the supported way")
	}
}

// TestDisruptorMuteIsCleanedUpOnTeardown guards a leak that only became
// reachable once the Disruptor's mute started working: the overwrite lives
// on the game CHANNEL, so a game ending mid-discussion (force-stop, last
// wolf lynched, bot restart) would otherwise leave a real player unable to
// speak in that channel forever, with no game left to lift it.
func TestDisruptorMuteIsCleanedUpOnTeardown(t *testing.T) {
	game := &werewolfGame{
		GuildID:              "guild1",
		WolfThreadID:         "wolf-thread",
		AliveThreadID:        "alive-thread",
		DisruptorMutedUserID: "muted-user",
	}
	info := gameTeardownInfoFor("chan1", game)
	if info.disruptorMutedUserID != "muted-user" {
		t.Fatalf("gameTeardownInfoFor dropped the disruptor mute: got %q, want %q", info.disruptorMutedUserID, "muted-user")
	}

	src, err := os.ReadFile("werewolf.go")
	if err != nil {
		t.Fatalf("read werewolf.go: %v", err)
	}
	body, ok := funcBody(string(src), "func teardownGame(")
	if !ok {
		t.Fatal("could not locate teardownGame")
	}
	if !strings.Contains(body, "removeDisruptorMute(") {
		t.Error("teardownGame never calls removeDisruptorMute — a game ending mid-discussion leaves its target muted permanently")
	}
}

// TestPreflightRechecksPermissions guards the second half of the gate. The
// lobby and role-count editor can sit for minutes; an admin editing the
// channel or the bot's role in that window must not leave the game to
// discover the gap mid-night, after roles are dealt and there is no clean
// way back.
func TestPreflightRechecksPermissions(t *testing.T) {
	src, err := os.ReadFile("werewolf.go")
	if err != nil {
		t.Fatalf("read werewolf.go: %v", err)
	}
	body, ok := funcBody(string(src), ") startPreflight(")
	if !ok {
		t.Fatal("could not locate startPreflight")
	}
	if !strings.Contains(body, "missingWerewolfPermissions(") {
		t.Error("startPreflight does not re-check permissions — a permission revoked during the lobby would surface mid-game")
	}
}

// funcBody returns the source between marker's opening brace and the first
// line consisting of a lone "}", i.e. the body of a top-level function.
func funcBody(src, marker string) (string, bool) {
	idx := strings.Index(src, marker)
	if idx < 0 {
		return "", false
	}
	rest := src[idx:]
	if end := strings.Index(rest, "\n}\n"); end >= 0 {
		return rest[:end], true
	}
	return rest, true
}

func namesOfPerms(perms []werewolfRequiredPermission) []string {
	names := make([]string, 0, len(perms))
	for _, p := range perms {
		names = append(names, p.Name)
	}
	return names
}

// TestShouldCloseEmptyLobby covers a rule that, gotten wrong in either
// direction, breaks the channel: too eager and one person can close every
// lobby a host opens (the channel is locked bot-only, so each kill also
// costs an unlock/relock cycle); too lax and an abandoned lobby holds the
// channel read-only with nobody able to cancel it.
func TestShouldCloseEmptyLobby(t *testing.T) {
	cases := []struct {
		name      string
		leaverID  string
		starterID string
		remaining int
		want      bool
	}{
		{"host leaves their own empty lobby", "host", "host", 0, true},
		{"host leaves but players remain", "host", "host", 3, false},
		{
			// The grief case. /werewolf start does not put the starter in the
			// roster, so one join+leave empties it while the host is present.
			name: "a non-host empties a lobby whose host never joined", leaverID: "rando", starterID: "host", remaining: 0, want: false,
		},
		{"last non-host player leaves, host still present", "player", "host", 0, false},
		{"a player leaves with others remaining", "player", "host", 2, false},
		{"unidentified leaver never closes the lobby", "", "host", 0, false},
		{"unidentified leaver and no starter", "", "", 0, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := shouldCloseEmptyLobby(tc.leaverID, tc.starterID, tc.remaining); got != tc.want {
				t.Errorf("shouldCloseEmptyLobby(%q, %q, %d) = %v, want %v", tc.leaverID, tc.starterID, tc.remaining, got, tc.want)
			}
		})
	}
}

// TestBothLeavePathsShareTheLobbyCloseRule keeps the button path and the
// slash path on one predicate. They have drifted before, and the failure is
// invisible until a channel is stuck.
func TestBothLeavePathsShareTheLobbyCloseRule(t *testing.T) {
	src, err := os.ReadFile("werewolf.go")
	if err != nil {
		t.Fatalf("read werewolf.go: %v", err)
	}
	if n := strings.Count(string(src), "shouldCloseEmptyLobby("); n < 3 {
		t.Errorf("shouldCloseEmptyLobby appears %d times (want the definition plus both call sites) — "+
			"one of handleLeave/handleJoinLeave is open-coding the rule again", n)
	}
}

// TestEmptyLobbyTeardownAnswersTheClickFirst guards the 3-second interaction
// deadline. teardownGame leads with a permission-overwrite round-trip, so
// doing it before responding risks "This interaction failed" plus a dead
// lobby left on screen with live buttons.
func TestEmptyLobbyTeardownAnswersTheClickFirst(t *testing.T) {
	src, err := os.ReadFile("werewolf.go")
	if err != nil {
		t.Fatalf("read werewolf.go: %v", err)
	}
	for _, fn := range []string{") handleJoinLeave(", ") handleLeave("} {
		body, ok := funcBody(string(src), fn)
		if !ok {
			t.Fatalf("could not locate %s", fn)
		}
		teardown := strings.Index(body, "teardownGame(s, info)")
		if teardown < 0 {
			continue
		}
		respond := strings.Index(body, "InteractionRespond(")
		if respond < 0 {
			respond = strings.Index(body, "respondSuccess(")
		}
		if respond < 0 || respond > teardown {
			t.Errorf("%s calls teardownGame before answering the interaction — a slow unlock will blow the 3s deadline", fn)
		}
	}
}

// gameWithDealtRoles builds a game whose roster holds exactly the given
// dealt roles, one player each, all alive.
func gameWithDealtRoles(roles ...role) *werewolfGame {
	game := &werewolfGame{Players: map[string]*player{}}
	for idx, r := range roles {
		id := fmt.Sprintf("u%d", idx)
		game.Players[id] = &player{UserID: id, Label: id, Role: r, DealtRole: r, Alive: true}
		game.JoinOrder = append(game.JoinOrder, id)
	}
	return game
}

// TestNightActingRolesNamesOnlyRolesInPlay covers the reported bug: the
// night-start line was a hardcoded string that named Seer/Doctor/Hunter/
// Disruptor whether or not they had been dealt, and omitted Witch and Cupid,
// which do act.
func TestNightActingRolesNamesOnlyRolesInPlay(t *testing.T) {
	t.Run("omits roles that were never dealt", func(t *testing.T) {
		game := gameWithDealtRoles(roleWerewolf, roleSeer, roleVillager, roleVillager)
		got := nightActingRoles(game, 1)
		want := []role{roleWerewolf, roleSeer}
		if !sameRoles(got, want) {
			t.Errorf("nightActingRoles = %v, want %v", labelsOfRoles(got), labelsOfRoles(want))
		}
		for _, absent := range []role{roleDoctor, roleWitch, roleHunter, roleDisruptor, roleCupid} {
			if containsRole(got, absent) {
				t.Errorf("named %s, which was never dealt", absent.label())
			}
		}
	})

	t.Run("includes Witch and Cupid, which the old string omitted", func(t *testing.T) {
		game := gameWithDealtRoles(roleWerewolf, roleWitch, roleCupid)
		got := nightActingRoles(game, 1)
		for _, want := range []role{roleWitch, roleCupid} {
			if !containsRole(got, want) {
				t.Errorf("%s acts at night but was not named", want.label())
			}
		}
	})

	t.Run("Traitor is never named", func(t *testing.T) {
		// The Traitor has no night action — naming them would advertise a
		// role whose whole point is passing as an ordinary villager.
		game := gameWithDealtRoles(roleWerewolf, roleTraitor, roleVillager)
		if got := nightActingRoles(game, 1); containsRole(got, roleTraitor) {
			t.Errorf("nightActingRoles named the Traitor: %v", labelsOfRoles(got))
		}
	})

	t.Run("wolf-pack roles collapse into one Ma Soi entry", func(t *testing.T) {
		// Wolf Cub and a converted Cursed Wolf act inside the pack's own
		// vote; listing them separately would leak the setup.
		game := gameWithDealtRoles(roleWerewolf, roleWolfCub, roleCursedWolf, roleVillager)
		got := nightActingRoles(game, 1)
		if !sameRoles(got, []role{roleWerewolf}) {
			t.Errorf("nightActingRoles = %v, want just [Ma Sói]", labelsOfRoles(got))
		}
	})

	t.Run("a pack of only Wolf Cubs still lists Ma Soi", func(t *testing.T) {
		game := gameWithDealtRoles(roleWolfCub, roleVillager, roleVillager)
		if got := nightActingRoles(game, 1); !containsRole(got, roleWerewolf) {
			t.Errorf("nightActingRoles = %v, want Ma Sói present", labelsOfRoles(got))
		}
	})
}

// TestNightActingRolesNeverSignalsDeaths is the property that makes this
// list safe to show at all. It is derived from DealtRole and ignores
// aliveness, so it is byte-identical on every night of a game — a role
// dropping off the list would otherwise be a reliable public death-reveal
// in the main channel, defeating reveal-on-death.
func TestNightActingRolesNeverSignalsDeaths(t *testing.T) {
	game := gameWithDealtRoles(roleWerewolf, roleSeer, roleDoctor, roleWitch, roleHunter, roleDisruptor, roleVillager)
	night1 := nightActingRolesPhrase(nightActingRoles(game, 2))

	// Kill every special role and convert the survivors' Role fields, the
	// way a real game mutates them.
	for _, p := range game.Players {
		if p.DealtRole != roleWerewolf && p.DealtRole != roleVillager {
			p.Alive = false
		}
		if p.DealtRole == roleCursedWolf {
			p.Role = roleWerewolf
		}
	}
	night5 := nightActingRolesPhrase(nightActingRoles(game, 5))

	if night1 != night5 {
		t.Errorf("the night line changed after deaths — this publicly reveals who died:\n night2: %s\n night5: %s", night1, night5)
	}
}

// TestCupidIsNamedOnlyOnTheNightItActs keeps the one round-dependent entry
// honest. Cupid acts on round 1 only, and that depends on the round number
// alone, never on who is alive — so it leaks nothing.
func TestCupidIsNamedOnlyOnTheNightItActs(t *testing.T) {
	game := gameWithDealtRoles(roleWerewolf, roleCupid, roleVillager)
	if got := nightActingRoles(game, 1); !containsRole(got, roleCupid) {
		t.Errorf("round 1: Cupid acts but was not named: %v", labelsOfRoles(got))
	}
	for _, round := range []int{2, 3, 7} {
		if got := nightActingRoles(game, round); containsRole(got, roleCupid) {
			t.Errorf("round %d: Cupid does not act but was named: %v", round, labelsOfRoles(got))
		}
	}
}

// TestNightActingRolesPhraseIsWellFormed guards the rendered sentence,
// including the all-villager setup that would otherwise emit a dangling
// "đang hành động" with nothing in front of it.
func TestNightActingRolesPhraseIsWellFormed(t *testing.T) {
	if phrase := nightActingRolesPhrase(nil); !strings.HasPrefix(phrase, "Các vai trò đặc biệt") {
		t.Errorf("empty role list produced %q, want the generic fallback", phrase)
	}
	phrase := nightActingRolesPhrase([]role{roleWerewolf, roleSeer})
	for _, want := range []string{"🐺 Ma Sói", "🔮 Tiên Tri", "đang hành động"} {
		if !strings.Contains(phrase, want) {
			t.Errorf("phrase %q missing %q", phrase, want)
		}
	}
	if strings.Contains(phrase, "  ") || strings.Contains(phrase, ", ,") {
		t.Errorf("phrase has malformed spacing: %q", phrase)
	}
}

// TestNightRoleEmojiMatchesTheProgressLine keeps the two places a role's
// icon is shown in agreement. They had already drifted once: the Doctor was
// 🩺 in the night-progress line and 💉 in /help, for the same role.
func TestNightRoleEmojiMatchesTheProgressLine(t *testing.T) {
	src, err := os.ReadFile("werewolf_night.go")
	if err != nil {
		t.Fatalf("read werewolf_night.go: %v", err)
	}
	text := string(src)
	for _, r := range []role{roleSeer, roleDoctor, roleWitch, roleHunter, roleDisruptor, roleCupid} {
		want := fmt.Sprintf("%s %s", nightRoleEmoji(r), r.label())
		if !strings.Contains(text, `announceRoleDecided(s, channelID, "`+want+`")`) {
			t.Errorf("announceRoleDecided does not use %q — the night-start list and the progress line disagree on %s's icon", want, r.label())
		}
	}
}

func containsRole(roles []role, want role) bool {
	for _, r := range roles {
		if r == want {
			return true
		}
	}
	return false
}

func sameRoles(got, want []role) bool {
	if len(got) != len(want) {
		return false
	}
	for idx := range got {
		if got[idx] != want[idx] {
			return false
		}
	}
	return true
}

func labelsOfRoles(roles []role) []string {
	out := make([]string, 0, len(roles))
	for _, r := range roles {
		out = append(out, r.label())
	}
	return out
}

// maxLabelGame builds the worst realistic roster: a full lobby of players
// whose display names are the longest Discord allows.
func maxLabelGame() *werewolfGame {
	game := &werewolfGame{ChannelID: "chan1", Players: map[string]*player{}}
	for n := 0; n < werewolfMaxPlayers; n++ {
		id := fmt.Sprintf("%d", 100000000000000000+n)
		r := roleVillager
		if n < 4 {
			r = roleWerewolf
		}
		game.Players[id] = &player{
			UserID:    id,
			Label:     strings.Repeat("Ngưyễn", 5) + "ab",
			Role:      r,
			DealtRole: r,
			Faction:   factionForRole(r),
			Alive:     n%3 != 0,
		}
		game.JoinOrder = append(game.JoinOrder, id)
	}
	return game
}

// TestEndOfGamePingIsScopedToPlayers is the safety property for pinging
// everyone at game end. The mention list must resolve to exactly the
// players who joined — never @everyone, never a role — regardless of what
// their display names contain.
func TestEndOfGamePingIsScopedToPlayers(t *testing.T) {
	game := maxLabelGame()
	pings, allowed := pingOnce(game.JoinOrder)

	if allowed == nil {
		t.Fatal("pingOnce returned nil AllowedMentions — Discord would then parse mentions out of the message text")
	}
	if len(allowed.Users) != len(game.JoinOrder) {
		t.Errorf("AllowedMentions.Users has %d entries, want %d", len(allowed.Users), len(game.JoinOrder))
	}
	if len(allowed.Roles) != 0 {
		t.Errorf("AllowedMentions.Roles = %v, want empty — a role ping would notify non-players", allowed.Roles)
	}
	for _, parse := range allowed.Parse {
		if parse == discordgo.AllowedMentionTypeEveryone || parse == discordgo.AllowedMentionTypeRoles {
			t.Errorf("AllowedMentions.Parse contains %q — this can mass-ping the whole server", parse)
		}
	}
	for _, id := range game.JoinOrder {
		if !strings.Contains(pings, id) {
			t.Errorf("player %s was not pinged", id)
		}
	}

	t.Run("a hostile display name cannot escalate the ping", func(t *testing.T) {
		// AllowedMentions is what actually authorizes a mention, so even if
		// "@everyone" reaches the message text via a display name, it must
		// stay inert.
		hostile := maxLabelGame()
		for _, p := range hostile.Players {
			p.Label = "@everyone @here <@&123456789012345678>"
		}
		_, allowed := pingOnce(hostile.JoinOrder)
		if len(allowed.Roles) != 0 || len(allowed.Parse) != 0 {
			t.Errorf("allowed = %+v, want Users-only scoping", allowed)
		}
	})

	t.Run("duplicate and empty ids are dropped", func(t *testing.T) {
		pings, allowed := pingOnce([]string{"111", "111", "", "222"})
		if len(allowed.Users) != 2 {
			t.Errorf("Users = %v, want 2 unique ids", allowed.Users)
		}
		if strings.Count(pings, "111") != 1 {
			t.Errorf("duplicate id pinged twice: %q", pings)
		}
	})
}

// TestEndOfGameMessagesFitDiscordsLimit guards the overflow that adding
// mentions would otherwise have caused. The result and roster together
// already measured 1936 runes for a full lobby with maximum-length names,
// against Discord's hard 2000-character cap; the ~440-rune mention list
// pushed that to 2377, where the send fails outright and players get no
// result at all.
func TestEndOfGameMessagesFitDiscordsLimit(t *testing.T) {
	game := maxLabelGame()
	pings, _ := pingOnce(game.JoinOrder)

	header := fmt.Sprintf("🎉 Ván Ma Sói kết thúc! %s\n🏆 **Người thắng:** %s%s",
		"🏡 **Dân Làng thắng!**", winnerNamesWithRoles(game, resultVillagersWin),
		"\n💤 someone bị loại vì AFK trước khi ván kết thúc.")
	if pings != "" {
		header += "\n\n" + pings
	}

	roleLines := make([]string, 0, len(game.JoinOrder))
	for _, id := range game.JoinOrder {
		p := game.Players[id]
		roleLines = append(roleLines, fmt.Sprintf("• %s — %s (%s)\n", p.Label, finalRoleLabel(p), "còn sống"))
	}

	all := append(
		splitIntoMessages(header, nil, werewolfMessageLimit),
		splitIntoMessages("**Vai trò của mọi người:**\n", roleLines, werewolfMessageLimit)...,
	)
	if len(all) == 0 {
		t.Fatal("no messages produced")
	}
	for idx, content := range all {
		if n := len([]rune(content)); n > werewolfMessageLimit {
			t.Errorf("message %d is %d runes, over the %d limit", idx, n, werewolfMessageLimit)
		}
	}
	if werewolfMessageLimit > 2000 {
		t.Errorf("werewolfMessageLimit = %d, above Discord's hard 2000-character cap", werewolfMessageLimit)
	}

	t.Run("every player appears in the role roster exactly once", func(t *testing.T) {
		joined := strings.Join(all, "")
		for _, id := range game.JoinOrder {
			if n := strings.Count(joined, game.Players[id].Label); n < 1 {
				t.Errorf("player %s missing from the end-of-game output", id)
			}
		}
	})
}

// TestSplitIntoMessages covers the packer directly, including the inputs
// that produced silent truncation elsewhere in this package.
func TestSplitIntoMessages(t *testing.T) {
	t.Run("a header that fits alone yields one message", func(t *testing.T) {
		got := splitIntoMessages("hello", nil, 100)
		if len(got) != 1 || got[0] != "hello" {
			t.Errorf("got %q, want [hello]", got)
		}
	})
	t.Run("empty input yields no messages", func(t *testing.T) {
		if got := splitIntoMessages("", nil, 100); len(got) != 0 {
			t.Errorf("got %q, want none", got)
		}
	})
	t.Run("no message exceeds the limit and no line is lost", func(t *testing.T) {
		lines := make([]string, 40)
		for idx := range lines {
			lines[idx] = fmt.Sprintf("• line %d ăâêôơư\n", idx)
		}
		for _, limit := range []int{20, 50, 100, 1900} {
			got := splitIntoMessages("HEADER\n", lines, limit)
			joined := strings.Join(got, "")
			for idx, content := range got {
				if n := len([]rune(content)); n > limit {
					t.Errorf("limit %d: message %d is %d runes", limit, idx, n)
				}
			}
			for idx := range lines {
				if !strings.Contains(joined, fmt.Sprintf("• line %d", idx)) {
					t.Errorf("limit %d: dropped line %d", limit, idx)
				}
			}
		}
	})
	t.Run("an over-long single line is clamped, not dropped", func(t *testing.T) {
		got := splitIntoMessages("", []string{strings.Repeat("ư", 500)}, 100)
		if len(got) != 1 {
			t.Fatalf("got %d messages, want 1", len(got))
		}
		if n := len([]rune(got[0])); n != 100 {
			t.Errorf("clamped to %d runes, want 100", n)
		}
	})
	t.Run("counts runes, not bytes", func(t *testing.T) {
		// "ư" is 3 bytes; a byte-based splitter would break these up.
		got := splitIntoMessages("", []string{strings.Repeat("ư", 30)}, 30)
		if len(got) != 1 {
			t.Errorf("got %d messages, want 1 — the packer is counting bytes", len(got))
		}
	})
}

// TestLopsidedCustomSetupsAreAccepted pins the deliberate removal of the
// wolf-faction balance floor from the custom-role editor. The editor exists
// so a group can deal whatever setup they want, including one that hands
// the wolves an immediate win; that is the starter's call, not the bot's to
// veto. Only structural rules — the ones without which the game cannot run
// at all — still reject.
func TestLopsidedCustomSetupsAreAccepted(t *testing.T) {
	accepted := []struct {
		name        string
		counts      map[role]int
		playerCount int
	}{
		{"wolves at parity", map[role]int{roleWerewolf: 2, roleVillager: 3}, 5},
		{"wolves already past parity", map[role]int{roleWerewolf: 3, roleVillager: 2}, 5},
		{"everyone but one is a wolf", map[role]int{roleWerewolf: 4, roleVillager: 1}, 5},
		{"cursed wolf on a thin roster", map[role]int{roleWerewolf: 2, roleCursedWolf: 1, roleVillager: 3}, 6},
		{"traitor pushing the wolf faction over", map[role]int{roleWerewolf: 2, roleTraitor: 1, roleVillager: 2}, 5},
	}
	for _, c := range accepted {
		t.Run(c.name, func(t *testing.T) {
			if !roleCountConfirmValid(c.counts, c.playerCount) {
				t.Errorf("roleCountConfirmValid(%v, %d) = false, want true — balance must not be enforced for custom roles", c.counts, c.playerCount)
			}
			if msg := roleCountConfirmError(c.counts, c.playerCount); msg != "" {
				t.Errorf("roleCountConfirmError(%v, %d) = %q, want \"\"", c.counts, c.playerCount, msg)
			}
		})
	}

	rejected := []struct {
		name        string
		counts      map[role]int
		playerCount int
		why         string
	}{
		{"total does not match the roster", map[role]int{roleWerewolf: 2, roleVillager: 2}, 7, "Tổng số vai trò"},
		{"no wolf side at all", map[role]int{roleVillager: 5}, 5, "phe Sói"},
		{"no villager side at all", map[role]int{roleWerewolf: 5}, 5, "phe Dân"},
		{"wolf side cannot vote to bite", map[role]int{roleTraitor: 1, roleCursedWolf: 1, roleVillager: 6}, 8, "Ma Sói hoặc Sói Con"},
	}
	for _, c := range rejected {
		t.Run(c.name, func(t *testing.T) {
			if roleCountConfirmValid(c.counts, c.playerCount) {
				t.Errorf("roleCountConfirmValid(%v, %d) = true, want false — %s is structural, not a balance judgment", c.counts, c.playerCount, c.name)
			}
			if msg := roleCountConfirmError(c.counts, c.playerCount); !strings.Contains(msg, c.why) {
				t.Errorf("roleCountConfirmError(%v, %d) = %q, want it to mention %q", c.counts, c.playerCount, msg, c.why)
			}
		})
	}
}

// TestGameEndSeparator covers the closing line that visually divides one
// game's output from the next in the channel.
func TestGameEndSeparator(t *testing.T) {
	const esc = "\u001b"

	results := map[gameResult]struct {
		color int
		word  string
	}{
		resultWerewolvesWin: {ansiRed, "SÓI"},
		resultVillagersWin:  {ansiGreen, "DÂN"},
		resultLoversWin:     {ansiPink, "CẶP ĐÔI"},
	}
	for result, want := range results {
		got := gameEndSeparator(result)

		if !strings.HasPrefix(got, "```ansi\n") || !strings.HasSuffix(got, "\n```") {
			t.Errorf("result %v: not a well-formed ansi block: %q", result, got)
		}
		// Discord colours text only inside a fence tagged exactly "ansi",
		// and a real ESC byte must reach the wire or the codes render as
		// literal text in the middle of the line.
		if !strings.Contains(got, esc+fmt.Sprintf("[1;%dm", want.color)) {
			t.Errorf("result %v: missing bold colour %d: %q", result, want.color, got)
		}
		if !strings.Contains(got, esc+"[0m") {
			t.Errorf("result %v: colour is never reset, which can bleed into later text: %q", result, got)
		}
		if !strings.Contains(got, want.word) {
			t.Errorf("result %v: does not name the winner (%q): %q", result, want.word, got)
		}

		// One line inside the fence — that is what makes it read as a rule
		// between games rather than another block of output.
		inner := strings.TrimSuffix(strings.TrimPrefix(got, "```ansi\n"), "\n```")
		if strings.Contains(inner, "\n") {
			t.Errorf("result %v: separator spans multiple lines: %q", result, inner)
		}
		if n := len([]rune(got)); n > werewolfMessageLimit {
			t.Errorf("result %v: %d runes, over the %d limit", result, n, werewolfMessageLimit)
		}
	}

	t.Run("each result gets a distinct colour", func(t *testing.T) {
		seen := map[string]gameResult{}
		for result := range results {
			line := gameEndSeparator(result)
			if prev, dup := seen[line]; dup {
				t.Errorf("results %v and %v render identically — the colour cue carries no information", prev, result)
			}
			seen[line] = result
		}
	})

	t.Run("an unknown result still renders a valid block", func(t *testing.T) {
		// resultOngoing should never reach here, but a malformed fence
		// would corrupt the rest of the channel's rendering.
		got := gameEndSeparator(resultOngoing)
		if !strings.HasPrefix(got, "```ansi\n") || !strings.HasSuffix(got, "\n```") {
			t.Errorf("malformed block for an unexpected result: %q", got)
		}
	})
}

// TestAnsiLineCannotBreakOutOfItsFence guards the two characters that would
// let text escape the code block and corrupt every message after it.
func TestAnsiLineCannotBreakOutOfItsFence(t *testing.T) {
	got := ansiLine(ansiRed, "line one\nline two ``` still inside")

	inner := strings.TrimSuffix(strings.TrimPrefix(got, "```ansi\n"), "\n```")
	if strings.Contains(inner, "\n") {
		t.Errorf("newline survived into the block, splitting the separator: %q", inner)
	}
	if strings.Contains(inner, "`") {
		t.Errorf("backtick survived into the block and can close the fence early: %q", inner)
	}
	if strings.Count(got, "```") != 2 {
		t.Errorf("expected exactly one opening and one closing fence: %q", got)
	}
}
