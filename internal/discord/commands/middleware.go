package commands

import (
	"time"

	"github.com/bwmarrin/discordgo"
	pkg_logger "github.com/phuc16/VCG-super-bot/pkg/logger"
	"github.com/rs/zerolog/log"
)

// CommandHandler is a function type for command handlers
type CommandHandler func(s *discordgo.Session, i *discordgo.InteractionCreate)

// WithLogging wraps a command handler with execution duration logging
func WithLogging(commandName string, handler CommandHandler) CommandHandler {
	return func(s *discordgo.Session, i *discordgo.InteractionCreate) {
		start := time.Now()

		// Extract user info
		userID := ""
		username := ""
		if i.Member != nil && i.Member.User != nil {
			userID = i.Member.User.ID
			username = i.Member.User.Username
		} else if i.User != nil {
			userID = i.User.ID
			username = i.User.Username
		}

		// Log command start
		log.Info().
			Str("command", commandName).
			Str("user_id", userID).
			Str("username", username).
			Str("guild_id", i.GuildID).
			Str("channel_id", i.ChannelID).
			Msg("Command execution started")

		// Execute the handler
		handler(s, i)

		// Log command completion with duration
		duration := time.Since(start)

		// Record metrics
		pkg_logger.RecordCommand(commandName, duration, nil)

		log.Info().
			Str("command", commandName).
			Str("user_id", userID).
			Str("username", username).
			Str("guild_id", i.GuildID).
			Str("channel_id", i.ChannelID).
			Dur("duration_ms", duration).
			Float64("duration_seconds", duration.Seconds()).
			Msg("Command execution completed")
	}
}

// WithPermissionCheck wraps a command handler with permission checking
func WithPermissionCheck(requiredPermission int64, handler CommandHandler) CommandHandler {
	return func(s *discordgo.Session, i *discordgo.InteractionCreate) {
		// Check if user has required permission
		if i.Member == nil {
			respondError(s, i, "This command can only be used in a server")
			return
		}

		permissions := int64(i.Member.Permissions)
		hasPermission := (permissions & requiredPermission) != 0

		if !hasPermission {
			log.Warn().
				Str("user_id", i.Member.User.ID).
				Str("username", i.Member.User.Username).
				Int64("required_permission", requiredPermission).
				Int64("user_permissions", permissions).
				Msg("Permission denied for command")

			respondError(s, i, "❌ You don't have permission to use this command")
			return
		}

		// Execute the handler
		handler(s, i)
	}
}

// WithAdminCheck wraps a command handler with admin permission checking
func WithAdminCheck(handler CommandHandler) CommandHandler {
	return func(s *discordgo.Session, i *discordgo.InteractionCreate) {
		if !hasAdminPermission(i.Member) {
			log.Warn().
				Str("user_id", i.Member.User.ID).
				Str("username", i.Member.User.Username).
				Msg("Admin permission denied for command")

			respondError(s, i, "❌ This command requires Administrator or Manage Server permission")
			return
		}

		// Execute the handler
		handler(s, i)
	}
}

// WithRecovery wraps a command handler with panic recovery
func WithRecovery(commandName string, handler CommandHandler) CommandHandler {
	return func(s *discordgo.Session, i *discordgo.InteractionCreate) {
		defer func() {
			if r := recover(); r != nil {
				log.Error().
					Str("command", commandName).
					Interface("panic", r).
					Msg("Recovered from panic in command handler")

				respondError(s, i, "An unexpected error occurred. Please try again later.")
			}
		}()

		handler(s, i)
	}
}

// ChainMiddleware chains multiple middleware functions together
func ChainMiddleware(handler CommandHandler, middlewares ...func(CommandHandler) CommandHandler) CommandHandler {
	// Apply middlewares in reverse order so they execute in the order specified
	for i := len(middlewares) - 1; i >= 0; i-- {
		handler = middlewares[i](handler)
	}
	return handler
}
