CREATE TABLE duel_matches (
    id VARCHAR(36) PRIMARY KEY,
    guild_id VARCHAR(20) NOT NULL,
    challenger_id VARCHAR(20) NOT NULL,
    opponent_id VARCHAR(20) NOT NULL,
    winner_id VARCHAR(20) NULL,
    draw TINYINT(1) NOT NULL DEFAULT 0,
    critical TINYINT(1) NOT NULL DEFAULT 0,
    wager INT NOT NULL,
    transfer INT NOT NULL,
    challenger_dice1 INT NOT NULL,
    challenger_dice2 INT NOT NULL,
    opponent_dice1 INT NOT NULL,
    opponent_dice2 INT NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    INDEX idx_duel_matches_guild (guild_id),
    INDEX idx_duel_matches_pair (guild_id, challenger_id, opponent_id),
    INDEX idx_duel_matches_pair_rev (guild_id, opponent_id, challenger_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
