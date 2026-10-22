# Discord Super Bot

**English** | [Tiếng Việt](README.vi.md)

A Discord bot for communities: Werewolf game, points duels, AI message summaries, custom keyword commands, and scheduled AI posts.

## Features

- **Werewolf (`/werewolf`)**: a full social-deduction game for 5–20 players with 11 roles, played directly in Discord
- **Duel (`/duel`)**: dice challenges with point wagers, streaks, a leaderboard, and scheduled tournaments
- **Export & AI summary (`/export`)**: export channel messages with a GPT summary
- **Keyword commands (`/addcom`)**: create your own `/name` command that replies with text and/or an image
- **AI topics (`/addtopic`)**: the bot writes and posts about a given topic on a schedule
- **Roast (`/roast`)**: AI playfully roasts a member based on their recent messages

---

## Requirements

- Go 1.24+
- MySQL 8.0+ (or Docker)
- Discord bot token
- OpenAI API key

---

## Setup

### Step 1: Create the Discord bot

1. Go to the [Discord Developer Portal](https://discord.com/developers/applications) → **New Application**
2. Open the **Bot** tab → **Add Bot** → copy the **Token** (this is `DISCORD_TOKEN`)
3. Enable the **Privileged Gateway Intents**:
   - ✅ Server Members Intent
   - ✅ Message Content Intent
4. Open **OAuth2 → URL Generator**:
   - Scopes: `bot`, `applications.commands`
   - Bot Permissions: see [Required bot permissions](#required-bot-permissions) below
5. Open the generated URL and add the bot to your server

### Step 2: Get the server ID

1. In Discord, go to **Settings → Advanced** and enable **Developer Mode**
2. Right-click the server name → **Copy Server ID** (this is `DISCORD_GUILD_ID`)

### Step 3: Configure the environment

```bash
git clone https://github.com/phuc16/super-bot.git
cd super-bot
go mod download

cp env.example .env
```

Edit `.env`. You **must** set these two values:

```env
DISCORD_TOKEN=your_bot_token
DISCORD_GUILD_ID=your_server_id
```

See [Environment Variables](#environment-variables) below for every setting.

### Step 4: Start the database

**With Docker (recommended):**

```bash
docker-compose -f docker/docker-compose.yml up -d mysql
```

**Or install MySQL yourself** and create the database and user matching the `DB_*` values in `.env`.

### Step 5: Run the bot

**Development:**

```bash
go run cmd/bot/main.go
```

**Production (build a binary):**

```bash
go build -o bot ./cmd/bot
./bot
```

**Docker (runs both the bot and MySQL):**

```bash
docker-compose -f docker/docker-compose.yml up -d
docker-compose -f docker/docker-compose.yml logs -f bot
```

The bot runs database migrations automatically on startup.

---

## Required bot permissions

| Permission | Used for |
|------------|----------|
| View Channel | Reading channels |
| Send Messages | Posting announcements and results |
| Read Message History | `/export` and `/roast` read past messages |
| Manage Roles | Locking the channel during Werewolf, muting the Saboteur's target |
| Create Private Threads | Private threads for the whole table and for the wolf pack |
| Send Messages in Threads | Sending bite votes and summaries in threads |
| Manage Threads | Locking threads by day/night, adding/removing members, deleting threads when the game ends |

**Werewolf needs all 7 permissions above.** `/werewolf start` checks before opening a table and reports exactly which permission is missing, instead of starting a game that breaks halfway.

---

## Switching to another server

Change two lines in `.env` and restart the bot:

```env
DISCORD_TOKEN=new_bot_token
DISCORD_GUILD_ID=new_server_id
```

---

## Commands

| Command | Description | Access |
|---------|-------------|--------|
| `/werewolf` | Werewolf game | Everyone |
| `/duel` | Dice challenge with a point wager | Everyone |
| `/duel-leaderboard` | Duel leaderboard | Everyone |
| `/duel-history` | Your duel history | Everyone |
| `/duel-challenge-all` | Schedule a duel tournament | Admin |
| `/duel-reset` | Reset all duel points on the server | Admin |
| `/export` | Export messages with an AI summary | Everyone |
| `/roast` | AI roasts a member | Everyone |
| `/addcom` | Add a keyword command | Admin |
| `/delcom` | Delete a keyword command | Admin |
| `/listcom` | List keyword commands | Everyone |
| `/addtopic` | Add a topic for the bot to write about periodically | Admin |
| `/deltopic` | Delete an AI topic | Admin |
| `/listtopic` | List AI topics | Everyone |
| `/posttopic` | Post an AI piece on any topic right now | Admin |
| `/help` | Show help (`lang:vi` or `lang:en`) | Everyone |

### `/werewolf` details

```
/werewolf start                                  # open a table
/werewolf start reveal-on-death:false            # don't reveal roles on death
/werewolf start custom-roles:true                # pick the count of each role
/werewolf status                                 # show the running game
/werewolf leave                                  # leave the game
/werewolf force-stop                             # cancel the game (Admin)
```

- **5–20 players.** Each channel runs one game at a time; multiple channels can run in parallel.
- **11 roles:** Villager, Werewolf, Seer, Doctor, Witch, Hunter, Wolf Cub, Cursed Wolf, Traitor, Saboteur, Cupid.
- `reveal-on-death` (default `true`): whether the dead player's role is announced. It also affects the role list shown in night announcements.
- `custom-roles` (default `false`): the host picks the count of each role in a dedicated panel. This mode does **not** check balance: you can skew heavily toward one side.
- See `/help werewolf` for each role and the win conditions.

### `/export` details

```
/export type:recent hours:24 language:vi
/export type:date_range from:"2026-01-01" end:"2026-01-07"
/export type:since_last
```

- `type`: `recent`, `date_range`, or `since_last`
- `hours`: how many hours to look back; used with `type:recent`
- `from` / `end`: `YYYY-MM-DD` or `YYYY-MM-DD HH:MM`; used with `type:date_range` (`end` defaults to now)
- `language`: `vi` (default) or `en`
- `public`: `true` to post publicly instead of only visible to you

### `/addcom` details

```
/addcom keyword:gei response:"cái đồ đáng iu" image:<file>
```

Then typing `/gei` in the channel makes the bot reply.

Images are **stored in the database**, not as links. Discord attachment links carry a signature that expires after a few hours and cannot be renewed; the old version stored links, so images disappeared after a short while. Maximum 8 MB per image.

---

## Environment Variables

| Variable | Required | Default | Description |
|----------|----------|---------|-------------|
| `DISCORD_TOKEN` | ✅ | — | Discord bot token |
| `DISCORD_GUILD_ID` | ✅ | — | Discord server ID |
| `OPENAI_API_KEY` | ✅* | — | OpenAI API key (*not needed if `MOCK_GPT_API=true`) |
| `DB_HOST` | | `localhost` | MySQL host |
| `DB_PORT` | | `3306` | MySQL port |
| `DB_USER` | | `superbot` | MySQL user |
| `DB_PASSWORD` | | *(empty)* | MySQL password |
| `DB_NAME` | | `superbot_dev` | Database name |
| `APP_ENV` | | `development` | `development` / `staging` / `production` |
| `LOG_LEVEL` | | `info` | `debug` / `info` / `warn` / `error` |
| `PORT` | | `8080` | HTTP health check port |
| `TIMEZONE` | | `UTC` | Timezone (for example `Asia/Ho_Chi_Minh`) |
| `GPT_MODEL` | | `gpt-4o-mini` | OpenAI model |
| `GPT_MAX_TOKENS` | | `4096` | Maximum tokens |
| `GPT_DAILY_BUDGET_USD` | | `0.034` | Daily GPT budget (USD) |
| `GPT_MAX_CONCURRENT_REQUESTS` | | `5` | Concurrent GPT requests |
| `GPT_REQUEST_TIMEOUT_SECONDS` | | `90` | Timeout per GPT request (seconds) |
| `RATE_LIMIT_EXPORT_PER_HOUR` | | `3` | `/export` uses per hour |
| `MOCK_GPT_API` | | `false` | `true` to test without an OpenAI key |
| `DEBUG_MODE` | | `false` | Enable debug mode |

All `DB_*` variables have dev defaults, but you **must override them in production**. `GPT_DAILY_BUDGET_USD` defaults very low (`0.034`); raise it if you use AI commands heavily, otherwise the bot stops calling GPT once the day's budget is spent.

---

## Database Migrations

Migrations run automatically at startup, reading the SQL files in `scripts/migrations/`
in filename order. To run them manually:

```bash
./bot migrate up        # apply pending migrations
./bot migrate down      # roll back the latest migration
./bot migrate version   # show the current version
```

Werewolf game state lives **only in memory**: restarting the bot mid-game loses that game. On startup the bot unlocks channels that were locked when it went down.

---

## Background Tasks

| Task | Schedule | Description |
|------|----------|-------------|
| AI posts | Every minute | Checks for due topics (`/addtopic`) and posts |

Duel tournaments scheduled with `/duel-challenge-all` run in memory, not through the scheduler, so they are also lost on restart.

---

## Health Check

The bot exposes endpoints on port 8080:

```
GET /health   # Overall status (database + Discord + GPT)
GET /ready    # Readiness check
GET /live     # Liveness check
```

---

## Project structure

```
.
├── cmd/bot/              # Entry point
├── internal/
│   ├── discord/
│   │   ├── commands/     # One file per slash command; werewolf split by phase
│   │   └── events/       # MESSAGE_CREATE handler (keyword commands)
│   ├── gpt/              # OpenAI integration
│   ├── services/         # Business logic
│   ├── scheduler/        # Background tasks
│   ├── storage/          # Database layer + migrator
│   ├── config/           # Configuration
│   └── models/           # Data models
├── pkg/logger/           # Logging
├── scripts/migrations/   # SQL migrations
└── docker/               # Docker config
```
