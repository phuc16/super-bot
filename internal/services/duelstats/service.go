package duelstats

import (
	"context"
	"errors"
	"math/rand"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/phuc16/VCG-super-bot/internal/models"
)

const StartingPoints = 100

// Service manages persistent /duel win/loss records and point wagers.
type Service struct {
	db *gorm.DB
}

// NewService creates a new Service
func NewService(db *gorm.DB) *Service {
	return &Service{db: db}
}

// Result describes the outcome of a single duel: both sides roll two 6-sided
// dice, higher sum wins (exact tie is a draw) — symmetric and fair, with the
// natural variance of real dice instead of a flat coinflip.
type Result struct {
	ChallengerDice [2]int
	OpponentDice   [2]int
	ChallengerSum  int
	OpponentSum    int
	ChallengerWon  bool
	Draw           bool
	Critical       bool // winner rolled double sixes
	Transfer       int
	Challenger     models.DuelStat
	Opponent       models.DuelStat
}

func rollTwoDice() (int, int) {
	return rand.Intn(6) + 1, rand.Intn(6) + 1
}

// Resolve rolls a duel between challenger and opponent and atomically applies
// the outcome to both players' persistent records.
func (s *Service) Resolve(ctx context.Context, guildID, challengerID, opponentID string, wager int) (*Result, error) {
	var result Result

	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		challenger, err := getOrCreateLocked(tx, guildID, challengerID)
		if err != nil {
			return err
		}
		opponent, err := getOrCreateLocked(tx, guildID, opponentID)
		if err != nil {
			return err
		}

		a1, a2 := rollTwoDice()
		b1, b2 := rollTwoDice()
		result.ChallengerDice = [2]int{a1, a2}
		result.OpponentDice = [2]int{b1, b2}
		result.ChallengerSum = a1 + a2
		result.OpponentSum = b1 + b2

		switch {
		case result.ChallengerSum == result.OpponentSum:
			result.Draw = true
			challenger.Draws++
			opponent.Draws++
			challenger.CurrentStreak = 0
			opponent.CurrentStreak = 0
		case result.ChallengerSum > result.OpponentSum:
			result.ChallengerWon = true
			result.Critical = a1 == 6 && a2 == 6
			applyWinLoss(challenger, opponent, wager, result.Critical, &result.Transfer)
		default:
			result.ChallengerWon = false
			result.Critical = b1 == 6 && b2 == 6
			applyWinLoss(opponent, challenger, wager, result.Critical, &result.Transfer)
		}

		if err := tx.Save(challenger).Error; err != nil {
			return err
		}
		if err := tx.Save(opponent).Error; err != nil {
			return err
		}

		match := models.DuelMatch{
			GuildID:         guildID,
			ChallengerID:    challengerID,
			OpponentID:      opponentID,
			Draw:            result.Draw,
			Critical:        result.Critical,
			Wager:           wager,
			Transfer:        result.Transfer,
			ChallengerDice1: a1,
			ChallengerDice2: a2,
			OpponentDice1:   b1,
			OpponentDice2:   b2,
		}
		if !result.Draw {
			winnerID := opponentID
			if result.ChallengerWon {
				winnerID = challengerID
			}
			match.WinnerID = &winnerID
		}
		if err := tx.Create(&match).Error; err != nil {
			return err
		}

		result.Challenger = *challenger
		result.Opponent = *opponent
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &result, nil
}

// applyWinLoss mutates winner/loser in place: transfers points (capped by the
// loser's balance so no one goes negative), updates win/loss counts, and
// updates streaks (reset to 1/-1 if the streak direction flips).
func applyWinLoss(winner, loser *models.DuelStat, wager int, critical bool, transferOut *int) {
	multiplier := 1
	if critical {
		multiplier = 2
	}
	transfer := wager * multiplier
	if transfer > loser.Points {
		transfer = loser.Points
	}
	if transfer < 0 {
		transfer = 0
	}

	winner.Points += transfer
	loser.Points -= transfer
	winner.Wins++
	loser.Losses++

	if winner.CurrentStreak >= 0 {
		winner.CurrentStreak++
	} else {
		winner.CurrentStreak = 1
	}
	if loser.CurrentStreak <= 0 {
		loser.CurrentStreak--
	} else {
		loser.CurrentStreak = -1
	}

	*transferOut = transfer
}

// getOrCreateLocked fetches a user's duel stats within tx, row-locked for
// update, creating a default record if none exists yet.
func getOrCreateLocked(tx *gorm.DB, guildID, userID string) (*models.DuelStat, error) {
	var stat models.DuelStat
	err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("guild_id = ? AND user_id = ?", guildID, userID).
		First(&stat).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		stat = models.DuelStat{
			GuildID: guildID,
			UserID:  userID,
			Points:  StartingPoints,
		}
		if err := tx.Create(&stat).Error; err != nil {
			return nil, err
		}
		return &stat, nil
	}
	if err != nil {
		return nil, err
	}
	return &stat, nil
}

// Leaderboard returns the top duelists for a guild, ordered by points descending.
func (s *Service) Leaderboard(ctx context.Context, guildID string, limit int) ([]models.DuelStat, error) {
	var stats []models.DuelStat
	err := s.db.WithContext(ctx).
		Where("guild_id = ?", guildID).
		Order("points DESC").
		Limit(limit).
		Find(&stats).Error
	return stats, err
}

// Get returns a user's duel stats, or nil if they've never dueled.
func (s *Service) Get(ctx context.Context, guildID, userID string) (*models.DuelStat, error) {
	var stat models.DuelStat
	err := s.db.WithContext(ctx).
		Where("guild_id = ? AND user_id = ?", guildID, userID).
		First(&stat).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &stat, nil
}

// HeadToHead tallies every past match between userA and userB (in either
// direction), returning userA's wins, userB's wins, and draws.
func (s *Service) HeadToHead(ctx context.Context, guildID, userA, userB string) (winsA, winsB, draws int, err error) {
	matches, err := s.pairMatches(ctx, guildID, userA, userB, 0)
	if err != nil {
		return 0, 0, 0, err
	}
	for _, m := range matches {
		switch {
		case m.Draw:
			draws++
		case m.WinnerID != nil && *m.WinnerID == userA:
			winsA++
		case m.WinnerID != nil && *m.WinnerID == userB:
			winsB++
		}
	}
	return winsA, winsB, draws, nil
}

// RecentMatches returns the most recent matches between userA and userB,
// newest first, up to limit.
func (s *Service) RecentMatches(ctx context.Context, guildID, userA, userB string, limit int) ([]models.DuelMatch, error) {
	return s.pairMatches(ctx, guildID, userA, userB, limit)
}

// RecentMatchesForUser returns userID's most recent matches against any
// opponent, newest first, up to limit.
func (s *Service) RecentMatchesForUser(ctx context.Context, guildID, userID string, limit int) ([]models.DuelMatch, error) {
	var matches []models.DuelMatch
	q := s.db.WithContext(ctx).
		Where("guild_id = ? AND (challenger_id = ? OR opponent_id = ?)", guildID, userID, userID).
		Order("created_at DESC")
	if limit > 0 {
		q = q.Limit(limit)
	}
	err := q.Find(&matches).Error
	return matches, err
}

func (s *Service) pairMatches(ctx context.Context, guildID, userA, userB string, limit int) ([]models.DuelMatch, error) {
	var matches []models.DuelMatch
	q := s.db.WithContext(ctx).
		Where(
			"guild_id = ? AND ((challenger_id = ? AND opponent_id = ?) OR (challenger_id = ? AND opponent_id = ?))",
			guildID, userA, userB, userB, userA,
		).
		Order("created_at DESC")
	if limit > 0 {
		q = q.Limit(limit)
	}
	err := q.Find(&matches).Error
	return matches, err
}

// EscrowPoints attempts to deduct amount points from userID's balance for a
// side bet. Returns ok=false (no error) if their balance is insufficient.
func (s *Service) EscrowPoints(ctx context.Context, guildID, userID string, amount int) (ok bool, err error) {
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		stat, err := getOrCreateLocked(tx, guildID, userID)
		if err != nil {
			return err
		}
		if stat.Points < amount {
			ok = false
			return nil
		}
		stat.Points -= amount
		ok = true
		return tx.Save(stat).Error
	})
	return ok, err
}

// CreditPoints adds amount points to userID's balance (e.g. a bet payout or refund).
func (s *Service) CreditPoints(ctx context.Context, guildID, userID string, amount int) error {
	if amount <= 0 {
		return nil
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		stat, err := getOrCreateLocked(tx, guildID, userID)
		if err != nil {
			return err
		}
		stat.Points += amount
		return tx.Save(stat).Error
	})
}

// ResetAll wipes every player's points/wins/losses/draws/streak for a guild
// (hard delete — next duel recreates fresh records at the starting balance).
// If includeHistory is true, past match history for the guild is wiped too.
func (s *Service) ResetAll(ctx context.Context, guildID string, includeHistory bool) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Unscoped().Where("guild_id = ?", guildID).Delete(&models.DuelStat{}).Error; err != nil {
			return err
		}
		if includeHistory {
			if err := tx.Where("guild_id = ?", guildID).Delete(&models.DuelMatch{}).Error; err != nil {
				return err
			}
		}
		return nil
	})
}
