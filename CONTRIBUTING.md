# Contributing to VCG Super Bot

Thanks for your interest in contributing! Here's how you can help.

## Getting Started

1. Fork the repository
2. Clone your fork: `git clone https://github.com/phuc16/VCG-super-bot.git`
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
  discord/            # Discord client & commands
  gpt/                # OpenAI integration
  services/           # Business logic
  scheduler/          # Background tasks
  storage/            # Database
  config/             # Configuration
  models/             # Data models
pkg/logger/           # Logging
scripts/migrations/   # SQL migrations
```

## Coding Standards

- Follow standard Go conventions
- Use `gofmt` for formatting
- Add comments for exported functions
- Keep functions focused and testable
- Use structured logging with zerolog

## Pull Request Checklist

- [ ] Code follows Go conventions
- [ ] Functions are documented
- [ ] Changes have been tested
- [ ] No breaking changes (or documented)
- [ ] Database migrations included (if needed)

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
