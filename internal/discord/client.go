package discord

import (
	"fmt"
	"net/http"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/rs/zerolog/log"
)

// Client wraps the Discord session
type Client struct {
	Session *discordgo.Session
	GuildID string
}

// NewClient creates a new Discord client
func NewClient(token string, guildID string) (*Client, error) {
	// Create Discord session
	session, err := discordgo.New("Bot " + token)
	if err != nil {
		return nil, fmt.Errorf("failed to create Discord session: %w", err)
	}

	// Set intents
	session.Identify.Intents = discordgo.IntentsGuildMessages |
		discordgo.IntentsGuildMembers |
		discordgo.IntentsGuildVoiceStates |
		discordgo.IntentsMessageContent

	// Use a persistent HTTP client with long-lived connections.
	// Default IdleConnTimeout is 90s — connections from startup expire before
	// the first user command arrives, forcing a new TLS handshake that pushes
	// InteractionRespond past Discord's 3s acknowledgment window.
	session.Client = &http.Client{
		Transport: &http.Transport{
			MaxIdleConnsPerHost: 8,
			IdleConnTimeout:     10 * time.Minute,
			DisableKeepAlives:   false,
		},
		Timeout: 30 * time.Second,
	}

	return &Client{
		Session: session,
		GuildID: guildID,
	}, nil
}

// Connect opens the Discord connection with retry logic
func (c *Client) Connect() error {
	// Add rate limit handler
	AddRateLimitHandler(c.Session)

	if err := c.ConnectWithRetry(5, 2*time.Second); err != nil {
		return err
	}

	// Pre-warm the HTTP connection to Discord's REST API so the first
	// interaction acknowledgment doesn't pay TCP+TLS handshake costs.
	go func() {
		if _, err := c.Session.Client.Get("https://discord.com/api/v10/gateway"); err != nil {
			log.Debug().Err(err).Msg("Discord HTTP pre-warm request failed (non-fatal)")
		} else {
			log.Debug().Msg("Discord HTTP connection pre-warmed")
		}
	}()

	return nil
}

// ConnectWithRetry attempts to connect with exponential backoff
func (c *Client) ConnectWithRetry(maxRetries int, initialDelay time.Duration) error {
	log.Info().
		Int("max_retries", maxRetries).
		Dur("initial_delay", initialDelay).
		Msg("Connecting to Discord with retry logic...")

	var lastErr error
	delay := initialDelay

	for attempt := 1; attempt <= maxRetries; attempt++ {
		log.Info().
			Int("attempt", attempt).
			Int("max_attempts", maxRetries).
			Msg("Attempting Discord connection")

		err := c.Session.Open()
		if err == nil {
			// Success! Log bot user info
			user := c.Session.State.User
			log.Info().
				Str("username", user.Username).
				Str("id", user.ID).
				Int("attempt", attempt).
				Msg("Discord connection established successfully")
			return nil
		}

		lastErr = err
		log.Warn().
			Err(err).
			Int("attempt", attempt).
			Int("max_attempts", maxRetries).
			Dur("next_retry_delay", delay).
			Msg("Discord connection attempt failed, retrying...")

		// Don't sleep on the last attempt
		if attempt < maxRetries {
			time.Sleep(delay)
			// Exponential backoff with jitter
			delay = time.Duration(float64(delay) * (1.5 + 0.5*float64(attempt)/float64(maxRetries)))
			// Cap at 30 seconds
			if delay > 30*time.Second {
				delay = 30 * time.Second
			}
		}
	}

	log.Error().
		Err(lastErr).
		Int("max_retries", maxRetries).
		Msg("Failed to establish Discord connection after all retries")

	return fmt.Errorf("failed to connect to Discord after %d attempts: %w", maxRetries, lastErr)
}

// Close closes the Discord connection
func (c *Client) Close() error {
	if c.Session != nil {
		log.Info().Msg("Closing Discord connection...")
		return c.Session.Close()
	}
	return nil
}

// RegisterCommand registers a slash command
func (c *Client) RegisterCommand(cmd *discordgo.ApplicationCommand) error {
	_, err := c.Session.ApplicationCommandCreate(c.Session.State.User.ID, c.GuildID, cmd)
	if err != nil {
		return fmt.Errorf("failed to register command %s: %w", cmd.Name, err)
	}
	log.Info().Str("command", cmd.Name).Msg("Command registered")
	return nil
}

// RegisterCommands registers multiple slash commands
func (c *Client) RegisterCommands(commands []*discordgo.ApplicationCommand) error {
	log.Info().Msg("Registering slash commands...")

	for _, cmd := range commands {
		if err := c.RegisterCommand(cmd); err != nil {
			return err
		}
	}

	log.Info().Int("count", len(commands)).Msg("All commands registered successfully")
	return nil
}

// AddHandler adds an event handler
func (c *Client) AddHandler(handler interface{}) {
	c.Session.AddHandler(handler)
}
