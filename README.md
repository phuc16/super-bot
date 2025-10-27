# VCG Super Bot

A Discord bot for activity tracking, engagement analytics, and AI-powered message summarization.

## Features

- **Message Export & AI Summarization** - Export conversations with GPT-powered summaries
- **Activity Tracking** - Track user engagement and assign recognition roles
- **Weekly Reports** - Automated engagement analytics posted to Discord
- **Inactivity Detection** - Identify and notify inactive members
- **Server Configuration** - Customizable settings per server

## Prerequisites

- Go 1.21+
- MySQL 8.0+
- Discord Bot Token
- OpenAI API Key

## Quick Start

### 1. Setup Environment

```bash
# Clone and install dependencies
git clone https://github.com/phuc16/VCG-super-bot.git
cd VCG-super-bot
go mod download

# Configure environment
cp env.example .env
# Edit .env with your credentials
```

### 2. Database Setup

```bash
# Start MySQL with Docker
docker-compose -f docker/docker-compose.yml up -d mysql

# Or use your own MySQL instance
# Create database: vcgsuperbot_dev
# Create user: vcgsuperbot
```

**Migration Commands:**
- `bot migrate up` - Run all pending migrations
- `bot migrate down` - Rollback last migration
- `bot migrate version` - Show current migration version (coming soon)

### 3. Run the Bot

```bash
# Development mode
go run cmd/bot/main.go

# Production build
go build -o bot ./cmd/bot
./bot
```

### 4. Deploy with Docker

```bash
# Build and run everything
docker-compose -f docker/docker-compose.yml up -d

# Check logs
docker-compose -f docker/docker-compose.yml logs -f bot
```

## Commands

| Command | Description | Permission |
|---------|-------------|------------|
| `/export` | Export and summarize messages | All users |
| `/activity` | View user activity statistics | All users |
| `/leaderboard` | View top 10 active users and your rank | All users |
| `/report` | Generate/view weekly reports | Admin |
| `/inactive` | Find inactive users | Admin |
| `/config` | Manage server settings | Admin |
| `/help` | Show command help | All users |

## Configuration

Key environment variables:

```bash
# Discord Configuration
DISCORD_TOKEN=your-bot-token
DISCORD_GUILD_ID=your-server-id

# OpenAI Configuration
OPENAI_API_KEY=sk-your_api_key_here
OPENAI_ORG_ID=org-your_org_id_here
GPT_MODEL=gpt-4-turbo-preview
GPT_MAX_TOKENS=4096

# Database Configuration
DB_HOST=localhost
DB_PORT=3306
DB_USER=vcgsuperbot
DB_PASSWORD=dev_password_change_me
DB_NAME=vcgsuperbot_dev

# Application Configuration
APP_ENV=development
LOG_LEVEL=debug
PORT=8080

# Feature Flags
FEATURE_MESSAGE_EXPORT=true
FEATURE_ACTIVITY_TRACKING=true
FEATURE_WEEKLY_REPORTS=true
FEATURE_ROLE_ASSIGNMENT=true

# Rate Limiting
RATE_LIMIT_EXPORT_PER_HOUR=10
RATE_LIMIT_ACTIVITY_PER_MINUTE=20

# GPT Configuration
GPT_MAX_CONCURRENT_REQUESTS=3
GPT_REQUEST_TIMEOUT_SECONDS=90
GPT_DAILY_BUDGET_USD=10.0

# Development Settings
DEBUG_MODE=true
MOCK_GPT_API=true
```

See `env.example` for all options.

## Project Structure

```
.
├── cmd/bot/              # Application entry point
├── internal/
│   ├── discord/          # Discord client & commands
│   ├── gpt/              # OpenAI integration
│   ├── services/         # Business logic
│   ├── scheduler/        # Background tasks
│   ├── storage/          # Database layer
│   ├── config/           # Configuration
│   └── models/           # Data models
├── pkg/logger/           # Logging utilities
├── scripts/migrations/   # Database migrations
└── docker/               # Docker configuration
```

## Health Checks

The bot exposes health check endpoints on port 8080:

- `GET /health` - Overall health status
- `GET /ready` - Readiness check
- `GET /live` - Liveness check

## Development

```bash
# Run locally with hot reload
go run cmd/bot/main.go

# Run tests (when available)
go test ./...

# Build for production
CGO_ENABLED=0 go build -ldflags="-s -w" -o bot ./cmd/bot

# Database Migrations
# Run migrations manually (recommended):
go run cmd/bot/main.go migrate up    # Apply migrations
go run cmd/bot/main.go migrate down  # Rollback last migration

# Or with built binary:
./bot migrate up
./bot migrate down
```

## Database Migrations

Migrations run automatically on bot startup. The bot will:
1. Connect to the database
2. Check for pending migrations
3. Apply them automatically
4. Start normally

### Available Migrations

- `001_initial_schema_up.sql` - Creates initial tables (server_configs, user_activity_records, weekly_activities, message_exports, weekly_reports) with all indexes

### Running Migrations Manually

If you need to run migrations manually (before starting the bot):

```bash
# Apply all pending migrations
DB_PASSWORD=your_password go run cmd/bot/main.go migrate up

# Or with built binary
DB_PASSWORD=your_password ./bot migrate up
```

**Note:** When `MOCK_GPT_API=true`, you don't need OpenAI API key for migrations.

## Background Tasks

Scheduled tasks run automatically:

- **Weekly Reports** - Every Monday at 00:00 (configurable)
- **User Sync** - Daily at 03:00 (syncs usernames/avatars)
- **Data Archival** - Daily at 02:00 (archives 90+ day old data)
- **Cleanup** - Daily at 04:00 (removes soft-deleted records)

## Performance

- Database connection pooling with optimized settings
- Per-user rate limiting to prevent abuse
- GPT request queueing with concurrency limits
- Circuit breaker for external API calls
- Exponential backoff for retries

## Security

- SQL injection protection via parameterized queries
- Admin permission checks on sensitive commands
- Input validation on all user inputs
- Rate limiting (global and per-user)
- Environment variable validation on startup

## Monitoring

- Structured logging with zerolog
- Performance metrics tracking
- Error tracking with unique UUIDs
- Connection pool statistics
- Health check endpoints

## License

MIT

## Contributing

Contributions are welcome! Please feel free to submit a Pull Request.
