package commands

import (
	"fmt"
)

const (
	werewolfMinPlayers = 5
	werewolfMaxPlayers = 12
)

// roleRatio describes how many of each special role a game of a given size gets.
type roleRatio struct {
	Werewolves int
	Seers      int
	Doctors    int
}

// rolesForPlayerCount returns the role ratio for a given player count, or an
// error if the count is outside [werewolfMinPlayers, werewolfMaxPlayers].
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
	default: // 11-12
		return roleRatio{Werewolves: 3, Seers: 1, Doctors: 1}, nil
	}
}
