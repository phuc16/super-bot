package commands

import (
	"fmt"
	"math/rand"
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

type role int

const (
	roleVillager role = iota
	roleWerewolf
	roleSeer
	roleDoctor
)

func (r role) label() string {
	switch r {
	case roleWerewolf:
		return "Ma Sói"
	case roleSeer:
		return "Tiên Tri"
	case roleDoctor:
		return "Bác Sĩ"
	default:
		return "Dân Làng"
	}
}

type player struct {
	UserID string
	Label  string
	Role   role
	Alive  bool
}

// assignRoles shuffles a copy of joinOrder and deals roles per rolesForPlayerCount.
func assignRoles(joinOrder []string, labels map[string]string) (map[string]*player, error) {
	ratio, err := rolesForPlayerCount(len(joinOrder))
	if err != nil {
		return nil, err
	}

	shuffled := make([]string, len(joinOrder))
	copy(shuffled, joinOrder)
	rand.Shuffle(len(shuffled), func(i, j int) { shuffled[i], shuffled[j] = shuffled[j], shuffled[i] })

	players := make(map[string]*player, len(shuffled))
	idx := 0
	assign := func(count int, r role) {
		for k := 0; k < count; k++ {
			id := shuffled[idx]
			idx++
			players[id] = &player{UserID: id, Label: labels[id], Role: r, Alive: true}
		}
	}
	assign(ratio.Werewolves, roleWerewolf)
	assign(ratio.Seers, roleSeer)
	assign(ratio.Doctors, roleDoctor)
	for ; idx < len(shuffled); idx++ {
		id := shuffled[idx]
		players[id] = &player{UserID: id, Label: labels[id], Role: roleVillager, Alive: true}
	}
	return players, nil
}

type gameResult int

const (
	resultOngoing gameResult = iota
	resultVillagersWin
	resultWerewolvesWin
)

// checkWinCondition: villagers win when every werewolf is dead; werewolves
// win once they're no longer outnumbered by everyone else still alive.
func checkWinCondition(players map[string]*player) gameResult {
	aliveWolves, aliveOthers := 0, 0
	for _, p := range players {
		if !p.Alive {
			continue
		}
		if p.Role == roleWerewolf {
			aliveWolves++
		} else {
			aliveOthers++
		}
	}
	switch {
	case aliveWolves == 0:
		return resultVillagersWin
	case aliveWolves >= aliveOthers:
		return resultWerewolvesWin
	default:
		return resultOngoing
	}
}

// tallyVotes counts votes and returns every target tied for the most votes.
// isTie is true when more than one target shares the highest count — the
// caller treats that as "nobody lynched."
func tallyVotes(votes map[string]string) (topTargets []string, isTie bool) {
	counts := make(map[string]int)
	for _, target := range votes {
		counts[target]++
	}
	max := 0
	for _, c := range counts {
		if c > max {
			max = c
		}
	}
	if max == 0 {
		return nil, false
	}
	for target, c := range counts {
		if c == max {
			topTargets = append(topTargets, target)
		}
	}
	return topTargets, len(topTargets) > 1
}

const afkRemovalThreshold = 2

func shouldRemoveForAFK(strikes int) bool {
	return strikes >= afkRemovalThreshold
}
