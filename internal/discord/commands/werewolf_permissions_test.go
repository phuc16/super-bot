package commands

import (
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
