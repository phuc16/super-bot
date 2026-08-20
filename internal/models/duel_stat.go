package models

// DuelStat tracks a user's persistent /duel record within a guild
type DuelStat struct {
	BaseModel
	GuildID       string `gorm:"column:guild_id;type:varchar(20);not null;index" json:"guild_id"`
	UserID        string `gorm:"column:user_id;type:varchar(20);not null" json:"user_id"`
	Points        int    `gorm:"column:points;not null;default:100" json:"points"`
	Wins          int    `gorm:"column:wins;not null;default:0" json:"wins"`
	Losses        int    `gorm:"column:losses;not null;default:0" json:"losses"`
	Draws         int    `gorm:"column:draws;not null;default:0" json:"draws"`
	CurrentStreak int    `gorm:"column:current_streak;not null;default:0" json:"current_streak"`
}

// TableName specifies the table name for DuelStat
func (DuelStat) TableName() string {
	return "duel_stats"
}
