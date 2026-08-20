package models

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// DuelMatch is an immutable log entry for a single resolved /duel.
type DuelMatch struct {
	ID              uuid.UUID `gorm:"type:varchar(36);primaryKey" json:"id"`
	GuildID         string    `gorm:"column:guild_id;type:varchar(20);not null;index" json:"guild_id"`
	ChallengerID    string    `gorm:"column:challenger_id;type:varchar(20);not null" json:"challenger_id"`
	OpponentID      string    `gorm:"column:opponent_id;type:varchar(20);not null" json:"opponent_id"`
	WinnerID        *string   `gorm:"column:winner_id;type:varchar(20)" json:"winner_id,omitempty"`
	Draw            bool      `gorm:"column:draw;not null;default:false" json:"draw"`
	Critical        bool      `gorm:"column:critical;not null;default:false" json:"critical"`
	Wager           int       `gorm:"column:wager;not null" json:"wager"`
	Transfer        int       `gorm:"column:transfer;not null" json:"transfer"`
	ChallengerDice1 int       `gorm:"column:challenger_dice1;not null" json:"challenger_dice1"`
	ChallengerDice2 int       `gorm:"column:challenger_dice2;not null" json:"challenger_dice2"`
	OpponentDice1   int       `gorm:"column:opponent_dice1;not null" json:"opponent_dice1"`
	OpponentDice2   int       `gorm:"column:opponent_dice2;not null" json:"opponent_dice2"`
	CreatedAt       time.Time `gorm:"column:created_at;not null;autoCreateTime" json:"created_at"`
}

// TableName specifies the table name for DuelMatch
func (DuelMatch) TableName() string {
	return "duel_matches"
}

// BeforeCreate generates a UUID before insert.
func (m *DuelMatch) BeforeCreate(tx *gorm.DB) error {
	if m.ID == uuid.Nil {
		m.ID = uuid.New()
	}
	return nil
}
