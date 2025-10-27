package gpt

import (
	"context"
	"fmt"
	"math"
	"math/rand"
	"time"

	"github.com/rs/zerolog/log"
	openai "github.com/sashabaranov/go-openai"
)

// RetryConfig defines the retry behavior for GPT API calls
type RetryConfig struct {
	MaxRetries int
	BaseDelay  time.Duration
}

// DefaultRetryConfig returns the default retry configuration
func DefaultRetryConfig() *RetryConfig {
	return &RetryConfig{
		MaxRetries: 3,
		BaseDelay:  1 * time.Second,
	}
}

// CallWithRetry executes a GPT API call with exponential backoff retry logic
func (c *Client) CallWithRetry(ctx context.Context, messages []openai.ChatCompletionMessage) (string, int, error) {
	config := DefaultRetryConfig()

	for attempt := 0; attempt < config.MaxRetries; attempt++ {
		content, tokens, err := c.CreateChatCompletion(ctx, messages)

		if err == nil {
			return content, tokens, nil
		}

		// Check if error is retryable
		if !isRetryable(err) {
			log.Error().
				Err(err).
				Int("attempt", attempt+1).
				Msg("Non-retryable error from GPT API")
			return "", 0, err
		}

		// Last attempt failed
		if attempt == config.MaxRetries-1 {
			log.Error().
				Err(err).
				Int("attempts", config.MaxRetries).
				Msg("Max retries exceeded for GPT API call")
			return "", 0, fmt.Errorf("max retries exceeded: %w", err)
		}

		// Calculate exponential backoff with jitter
		delay := config.BaseDelay * time.Duration(math.Pow(2, float64(attempt)))
		jitter := time.Duration(rand.Int63n(int64(delay / 2)))
		totalDelay := delay + jitter

		log.Warn().
			Err(err).
			Int("attempt", attempt+1).
			Int("max_retries", config.MaxRetries).
			Dur("delay", totalDelay).
			Msg("Retrying GPT API call after error")

		// Wait before retry
		select {
		case <-ctx.Done():
			return "", 0, ctx.Err()
		case <-time.After(totalDelay):
			// Continue to next retry
		}
	}

	return "", 0, fmt.Errorf("unexpected retry loop exit")
}

// isRetryable determines if an error should trigger a retry
func isRetryable(err error) bool {
	if err == nil {
		return false
	}

	errMsg := err.Error()

	// Rate limit errors (429)
	if contains(errMsg, "rate limit") || contains(errMsg, "429") {
		return true
	}

	// Server errors (500, 502, 503)
	if contains(errMsg, "500") || contains(errMsg, "502") || contains(errMsg, "503") {
		return true
	}

	// Timeout errors
	if contains(errMsg, "timeout") || contains(errMsg, "deadline exceeded") {
		return true
	}

	// Temporary network errors
	if contains(errMsg, "connection reset") || contains(errMsg, "connection refused") {
		return true
	}

	// Non-retryable errors:
	// - 400 (invalid request)
	// - 401 (authentication)
	// - 404 (not found)
	// - Content filter violations
	return false
}

// contains checks if a string contains a substring (case-insensitive)
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 ||
		(len(s) > 0 && len(substr) > 0 && stringContains(s, substr)))
}

func stringContains(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
