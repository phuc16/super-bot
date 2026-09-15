CREATE TABLE custom_commands (
    id VARCHAR(36) PRIMARY KEY,
    guild_id VARCHAR(20) NOT NULL,
    `trigger` VARCHAR(100) NOT NULL,
    response TEXT NOT NULL,
    created_by VARCHAR(20) NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    deleted_at TIMESTAMP NULL,
    UNIQUE INDEX idx_custom_commands_guild_trigger (guild_id, `trigger`),
    INDEX idx_custom_commands_guild (guild_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
