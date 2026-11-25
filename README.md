<a id="readme-top"></a>

[![Contributors][contributors-shield]][contributors-url]
[![Forks][forks-shield]][forks-url]
[![Stargazers][stars-shield]][stars-url]
[![Issues][issues-shield]][issues-url]

<br />
<div align="center">
  <h3 align="center">Discord Super Bot</h3>

  <p align="center">
    A Discord bot for communities: Werewolf game, point duels, AI message summaries, custom keyword commands and scheduled AI posts. Written in Go.
    <br />
    <a href="docs/guide.md"><strong>Explore the docs »</strong></a>
    <br />
    <br />
    <a href="#usage">Commands</a>
    &middot;
    <a href="https://github.com/phuc16/super-bot/issues/new">Report Bug</a>
    &middot;
    <a href="https://github.com/phuc16/super-bot/issues/new">Request Feature</a>
  </p>
</div>

<details>
  <summary>Table of Contents</summary>
  <ol>
    <li>
      <a href="#about-the-project">About The Project</a>
      <ul>
        <li><a href="#built-with">Built With</a></li>
      </ul>
    </li>
    <li>
      <a href="#getting-started">Getting Started</a>
      <ul>
        <li><a href="#prerequisites">Prerequisites</a></li>
        <li><a href="#installation">Installation</a></li>
      </ul>
    </li>
    <li><a href="#usage">Usage</a></li>
    <li><a href="#configuration">Configuration</a></li>
    <li><a href="#project-structure">Project Structure</a></li>
    <li><a href="#roadmap">Roadmap</a></li>
    <li><a href="#contributing">Contributing</a></li>
    <li><a href="#license">License</a></li>
    <li><a href="#contact">Contact</a></li>
    <li><a href="#acknowledgments">Acknowledgments</a></li>
  </ol>
</details>

## About The Project

Discord Super Bot is a community bot driven by slash commands:

- **Werewolf (`/werewolf`)**: a full social-deduction game for 5-20 players with 11 roles, played directly in Discord using private threads for the table and the wolf pack.
- **Duel (`/duel`)**: dice challenges with point wagers, streaks, a leaderboard and scheduled tournaments.
- **Export & AI summary (`/export`)**: export channel messages with a GPT summary, protected by a daily budget and a rate limit.
- **Keyword commands (`/addcom`)**: create your own `/name` command that replies with text and/or an image (images are stored in the database, not as expiring links).
- **AI topics (`/addtopic`)**: the bot writes and posts about a given topic on a schedule.
- **Roast (`/roast`)**: AI playfully roasts a member based on their recent messages.

It also ships automatic migrations on startup, health checks (`/health`, `/ready`, `/live`) and Docker Compose files for development and production.

<p align="right">(<a href="#readme-top">back to top</a>)</p>

### Built With

* [![Go][Go]][Go-url]
* [![MySQL][MySQL]][MySQL-url]
* [![Docker][Docker]][Docker-url]
* [![OpenAI][OpenAI]][OpenAI-url]

Also: [discordgo](https://github.com/bwmarrin/discordgo), [Gin](https://github.com/gin-gonic/gin), [GORM](https://gorm.io), [zerolog](https://github.com/rs/zerolog) and [robfig/cron](https://github.com/robfig/cron).

<p align="right">(<a href="#readme-top">back to top</a>)</p>

## Getting Started

To get a local copy running, follow these steps. The [full guide](docs/guide.md) covers permissions, switching servers, migrations and health checks.

### Prerequisites

* Go 1.24+
* MySQL 8.0+ (or Docker)
* A Discord bot token, with **Server Members Intent** and **Message Content Intent** enabled in the [Developer Portal](https://discord.com/developers/applications)
* An OpenAI API key (not needed if `MOCK_GPT_API=true`)

### Installation

1. Create the bot in the Developer Portal and copy its token. Under **OAuth2 → URL Generator** pick the scopes `bot` and `applications.commands`, then invite it to your server. Werewolf needs all 7 permissions listed in [Required bot permissions](docs/guide.md#required-bot-permissions).
2. Enable Developer Mode in Discord, right-click your server and **Copy Server ID**.
3. Clone the repo
   ```sh
   git clone https://github.com/phuc16/super-bot.git
   cd super-bot
   go mod download
   ```
4. Create the config and fill in `DISCORD_TOKEN`, `DISCORD_GUILD_ID` (and `OPENAI_API_KEY`)
   ```sh
   cp env.example .env
   ```
5. Start MySQL
   ```sh
   docker-compose -f docker/docker-compose.yml up -d mysql
   ```
6. Run the bot (migrations run automatically on startup)
   ```sh
   go run cmd/bot/main.go
   ```
   Or run the bot and MySQL together with Docker:
   ```sh
   docker-compose -f docker/docker-compose.yml up -d
   docker-compose -f docker/docker-compose.yml logs -f bot
   ```

<p align="right">(<a href="#readme-top">back to top</a>)</p>

## Usage

| Command | Description | Access |
|---------|-------------|--------|
| `/werewolf` | Werewolf game (`start`, `status`, `leave`, `force-stop`) | Everyone |
| `/duel`, `/duel-leaderboard`, `/duel-history` | Dice challenge, leaderboard, your history | Everyone |
| `/duel-challenge-all`, `/duel-reset` | Schedule a tournament, reset points | Admin |
| `/export` | Export messages with an AI summary | Everyone |
| `/roast` | AI roasts a member | Everyone |
| `/addcom`, `/delcom`, `/listcom` | Manage keyword commands | Admin (`/listcom`: everyone) |
| `/addtopic`, `/deltopic`, `/posttopic`, `/listtopic` | Manage AI topics | Admin (`/listtopic`: everyone) |
| `/help` | Show help (`lang:vi` or `lang:en`) | Everyone |

Examples:

```
/werewolf start custom-roles:true
/export type:recent hours:24 language:en
/addcom keyword:gei response:"so cute" image:<file>
```

Details for each command and the 11 Werewolf roles are in the [full guide](docs/guide.md#commands), or type `/help werewolf` in Discord.

<p align="right">(<a href="#readme-top">back to top</a>)</p>

## Configuration

Required: `DISCORD_TOKEN`, `DISCORD_GUILD_ID`, and `OPENAI_API_KEY` (unless `MOCK_GPT_API=true`). Everything else (`DB_*`, `GPT_*`, `RATE_LIMIT_EXPORT_PER_HOUR`, `TIMEZONE`, ...) has a default; see the [full table](docs/guide.md#environment-variables) and `env.example`.

`GPT_DAILY_BUDGET_USD` defaults very low, and the bot stops calling GPT once the day's budget is spent. Werewolf game state lives only in memory, so restarting the bot mid-game ends that game.

<p align="right">(<a href="#readme-top">back to top</a>)</p>

## Project Structure

```
.
├── cmd/bot/              # Entry point
├── internal/
│   ├── discord/          # commands/ (one file per slash command) and events/
│   ├── gpt/              # OpenAI integration
│   ├── services/         # Business logic
│   ├── scheduler/        # Background tasks
│   ├── storage/          # Database layer + migrator
│   ├── config/           # Configuration
│   └── models/           # Data models
├── pkg/logger/           # Logging
├── scripts/migrations/   # SQL migrations
└── docker/               # Dockerfile and docker-compose
```

<p align="right">(<a href="#readme-top">back to top</a>)</p>

## Roadmap

- [x] Werewolf with 11 roles, private threads and a permission check before a game opens
- [x] Duels, leaderboard and scheduled tournaments
- [x] Export and AI summaries with a budget and rate limit
- [x] Keyword commands with images, scheduled AI topics, health checks, production Docker
- [ ] Persist Werewolf games and duel tournaments across restarts

See the [open issues](https://github.com/phuc16/super-bot/issues) for more.

<p align="right">(<a href="#readme-top">back to top</a>)</p>

## Contributing

Contributions are what make the open source community such an amazing place to learn, inspire, and create. Any contributions you make are **greatly appreciated**. See also [CONTRIBUTING.md](CONTRIBUTING.md).

1. Fork the Project
2. Create your Feature Branch (`git checkout -b feature/AmazingFeature`)
3. Commit your Changes (`git commit -m 'Add some AmazingFeature'`)
4. Push to the Branch (`git push origin feature/AmazingFeature`)
5. Open a Pull Request

<p align="right">(<a href="#readme-top">back to top</a>)</p>

## License

No license file has been added to this repository yet, so all rights are reserved by default. Open an issue if you would like to reuse any part of it.

<p align="right">(<a href="#readme-top">back to top</a>)</p>

## Contact

Phuc Truong Hoang - [@phuc16](https://github.com/phuc16)

Project Link: [https://github.com/phuc16/super-bot](https://github.com/phuc16/super-bot)

<p align="right">(<a href="#readme-top">back to top</a>)</p>

## Acknowledgments

* [Best-README-Template](https://github.com/othneildrew/Best-README-Template)
* [discordgo](https://github.com/bwmarrin/discordgo)
* [Shields.io](https://shields.io)

<p align="right">(<a href="#readme-top">back to top</a>)</p>

<!-- MARKDOWN LINKS & IMAGES -->
[contributors-shield]: https://img.shields.io/github/contributors/phuc16/super-bot.svg?style=for-the-badge
[contributors-url]: https://github.com/phuc16/super-bot/graphs/contributors
[forks-shield]: https://img.shields.io/github/forks/phuc16/super-bot.svg?style=for-the-badge
[forks-url]: https://github.com/phuc16/super-bot/network/members
[stars-shield]: https://img.shields.io/github/stars/phuc16/super-bot.svg?style=for-the-badge
[stars-url]: https://github.com/phuc16/super-bot/stargazers
[issues-shield]: https://img.shields.io/github/issues/phuc16/super-bot.svg?style=for-the-badge
[issues-url]: https://github.com/phuc16/super-bot/issues
[Go]: https://img.shields.io/badge/Go-00ADD8?style=for-the-badge&logo=go&logoColor=white
[Go-url]: https://go.dev/
[MySQL]: https://img.shields.io/badge/MySQL-4479A1?style=for-the-badge&logo=mysql&logoColor=white
[MySQL-url]: https://www.mysql.com/
[Docker]: https://img.shields.io/badge/Docker-2496ED?style=for-the-badge&logo=docker&logoColor=white
[Docker-url]: https://www.docker.com/
[OpenAI]: https://img.shields.io/badge/OpenAI-412991?style=for-the-badge&logo=openai&logoColor=white
[OpenAI-url]: https://openai.com/
