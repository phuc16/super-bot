# Contributing to Discord Super Bot

Thanks for your interest in contributing! Here's how you can help.

## Getting Started

1. Fork the repository
2. Clone your fork: `git clone https://github.com/phuc16/super-bot.git`
3. Create a branch: `git checkout -b feature/your-feature`
4. Make your changes
5. Test your changes
6. Commit: `git commit -m "Add your feature"`
7. Push: `git push origin feature/your-feature`
8. Open a Pull Request

## Development Setup

```bash
# Install dependencies
go mod download

# Setup environment
cp env.example .env
# Edit .env with your test credentials

# Start database
docker-compose -f docker/docker-compose.yml up -d mysql

# Run the bot
go run cmd/bot/main.go
```

## Project Structure

```
cmd/bot/              # Main application
internal/
  discord/
    commands/         # One file per slash command
    events/           # MESSAGE_CREATE handler (keyword commands)
  gpt/                # OpenAI integration
  services/           # Business logic
  scheduler/          # Background tasks
  storage/            # Database + migrator
  config/             # Configuration
  models/             # Data models
pkg/logger/           # Logging
scripts/migrations/   # SQL migrations
```

`/werewolf` is large enough to be split by game phase rather than kept in one
file: `werewolf.go` (state, lobby, teardown), `werewolf_night.go`,
`werewolf_day.go`, `werewolf_rolepick.go`.

## Testing

```bash
go build ./...
go vet ./internal/... ./cmd/...
go test ./... -count=1
go test ./internal/discord/commands/ -race   # game logic is concurrent
```

Most logic that can be tested without a live Discord connection is: role
dealing, vote tallies, win conditions, death chains, message chunking. Tests
live beside the code they cover.

Two conventions worth knowing before adding tests here:

- **Discord's limits are measured in runes, not bytes.** Display names in
  this project are largely Vietnamese (2–3 bytes per character) or contain
  emoji (4 bytes), so a byte-based length check both truncates far too early
  and can split a character in half. Several tests exist purely to pin this.
- **A guard is only worth adding if you have seen it fail.** When you add a
  test for a bug, reintroduce the bug and confirm the test catches it before
  committing. Several tests here were written against bugs that recurred
  across refactors; the ones that hold are the ones that were verified this
  way.

> **Known issue:** `go test ./...` currently fails in `internal/config` — 
> `config.go` has a non-constant format string that `go vet` rejects, and vet
> runs as part of `go test`. It is unrelated to any feature work; if it trips
> you up, it is not your change.

## Database Migrations

Migrations are plain SQL in `scripts/migrations/`, applied automatically on
startup in filename order. Each needs a matching pair:

```
NNN_short_description_up.sql
NNN_short_description_down.sql
```

Use the next free `NNN`. Say in a comment what the migration is for and why —
particularly if `down` loses data, which is often the case when a column
starts holding content rather than a reference to it.

## Coding Standards

- Follow standard Go conventions
- Use `gofmt` for formatting
- Add comments for exported functions
- Keep functions focused and testable
- Use structured logging with zerolog

## Pull Request Checklist

- [ ] `gofmt` clean, `go build` and `go vet` pass
- [ ] Functions are documented
- [ ] Tests added for new logic, and verified to fail without the change
- [ ] No breaking changes (or documented)
- [ ] Database migrations included, with a matching `_down.sql` (if needed)
- [ ] New bot permissions documented in the README (if needed)

## Reporting Issues

When reporting bugs, please include:

- Go version
- Operating system
- Steps to reproduce
- Expected vs actual behavior
- Error messages or logs

## Feature Requests

For new features:

- Describe the use case
- Explain why it's useful
- Suggest an implementation approach

## Questions?

Open an issue for questions or discussions.

## License

By contributing, you agree that your contributions will be licensed under the MIT License.
