CREATE TABLE ai_topics (
    id VARCHAR(36) PRIMARY KEY,
    guild_id VARCHAR(20) NOT NULL,
    topic TEXT NOT NULL,
    channel_id VARCHAR(20) NOT NULL,
    created_by VARCHAR(20) NOT NULL,
    interval_minutes INT NOT NULL DEFAULT 5,
    last_posted_at TIMESTAMP NULL,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    deleted_at TIMESTAMP NULL,
    INDEX idx_ai_topics_guild (guild_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
