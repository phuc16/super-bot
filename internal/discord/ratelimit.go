package discord

import (
	"fmt"
	"math"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/rs/zerolog/log"
)

// RateLimitHandler handles Discord API rate limits with exponential backoff
type RateLimitHandler struct {
	maxRetries   int
	initialDelay time.Duration
	maxDelay     time.Duration
}

// NewRateLimitHandler creates a new rate limit handler
func NewRateLimitHandler() *RateLimitHandler {
	return &RateLimitHandler{
		maxRetries:   5,
		initialDelay: 1 * time.Second,
		maxDelay:     60 * time.Second,
	}
}

// Execute executes a Discord API call with automatic retry on rate limits
func (h *RateLimitHandler) Execute(operation string, fn func() error) error {
	var lastErr error
	delay := h.initialDelay

	for attempt := 1; attempt <= h.maxRetries; attempt++ {
		err := fn()

		if err == nil {
			return nil // Success
		}

		// Check if it's a rate limit error
		if restErr, ok := err.(*discordgo.RESTError); ok {
			if restErr.Response != nil && restErr.Response.StatusCode == 429 {
				// Rate limited - extract retry after from response
				retryAfter := h.extractRetryAfter(restErr)

				if retryAfter > 0 {
					delay = time.Duration(retryAfter) * time.Millisecond
				}

				log.Warn().
					Str("operation", operation).
					Int("attempt", attempt).
					Int("max_attempts", h.maxRetries).
					Dur("retry_after", delay).
					Msg("Discord API rate limit hit, waiting before retry")

				if attempt < h.maxRetries {
					time.Sleep(delay)

					// Exponential backoff for next attempt if no retry-after header
					if retryAfter == 0 {
						delay = h.calculateBackoff(attempt, delay)
					}
					continue
				}
			}
		}

		// Not a rate limit error, or last attempt
		lastErr = err
		break
	}

	if lastErr != nil {
		return fmt.Errorf("operation %s failed after %d attempts: %w", operation, h.maxRetries, lastErr)
	}

	return lastErr
}

// extractRetryAfter extracts retry-after from Discord rate limit response
func (h *RateLimitHandler) extractRetryAfter(err *discordgo.RESTError) int {
	// Discord includes retry_after in the response body
	// Note: The exact field structure depends on Discord API version
	// For now, return 0 and rely on exponential backoff
	return 0
}

// calculateBackoff calculates exponential backoff delay
func (h *RateLimitHandler) calculateBackoff(attempt int, currentDelay time.Duration) time.Duration {
	// Exponential backoff: delay * 2^attempt with jitter
	multiplier := math.Pow(2, float64(attempt))
	newDelay := time.Duration(float64(currentDelay) * multiplier)

	// Add jitter (±20%)
	jitter := float64(newDelay) * 0.2 * (float64(time.Now().UnixNano()%100)/100.0 - 0.5)
	newDelay = time.Duration(float64(newDelay) + jitter)

	// Cap at max delay
	if newDelay > h.maxDelay {
		newDelay = h.maxDelay
	}

	return newDelay
}

// WithRateLimitRetry wraps a Discord API call with rate limit handling
func WithRateLimitRetry(operation string, fn func() error) error {
	handler := NewRateLimitHandler()
	return handler.Execute(operation, fn)
}

// AddRateLimitHandler adds rate limit event logging to a Discord session
func AddRateLimitHandler(session *discordgo.Session) {
	session.AddHandler(func(s *discordgo.Session, r *discordgo.RateLimit) {
		log.Warn().
			Str("url", r.URL).
			Msg("Discord rate limit encountered")
	})
}
