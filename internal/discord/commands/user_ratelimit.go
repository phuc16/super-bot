package commands

import (
	"fmt"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/rs/zerolog/log"
)

// UserRateLimiter implements per-user rate limiting for Discord commands
type UserRateLimiter struct {
	mu          sync.RWMutex
	limits      map[string]*UserLimit // userID -> limit tracker
	maxRequests int
	window      time.Duration
	cleanupTick *time.Ticker
	done        chan struct{}
}

// UserLimit tracks rate limit state for a single user
type UserLimit struct {
	requests  []time.Time
	mu        sync.Mutex
	lastUsage time.Time
}

// NewUserRateLimiter creates a new user-level rate limiter
// maxRequests: maximum number of requests allowed
// window: time window for rate limit (e.g., 1 minute)
func NewUserRateLimiter(maxRequests int, window time.Duration) *UserRateLimiter {
	rl := &UserRateLimiter{
		limits:      make(map[string]*UserLimit),
		maxRequests: maxRequests,
		window:      window,
		done:        make(chan struct{}),
	}

	// Start cleanup goroutine to remove stale entries
	rl.cleanupTick = time.NewTicker(5 * time.Minute)
	go rl.cleanup()

	return rl
}

// Allow checks if a user is allowed to make a request
// Returns (allowed bool, retryAfter duration)
func (rl *UserRateLimiter) Allow(userID string) (bool, time.Duration) {
	now := time.Now()

	rl.mu.RLock()
	limit, exists := rl.limits[userID]
	rl.mu.RUnlock()

	if !exists {
		// Create new limit tracker for this user
		limit = &UserLimit{
			requests:  []time.Time{now},
			lastUsage: now,
		}
		rl.mu.Lock()
		rl.limits[userID] = limit
		rl.mu.Unlock()
		return true, 0
	}

	limit.mu.Lock()
	defer limit.mu.Unlock()

	// Remove requests outside the current window
	cutoff := now.Add(-rl.window)
	validRequests := []time.Time{}
	for _, reqTime := range limit.requests {
		if reqTime.After(cutoff) {
			validRequests = append(validRequests, reqTime)
		}
	}

	// Check if user has exceeded the limit
	if len(validRequests) >= rl.maxRequests {
		// Calculate retry after time
		oldestRequest := validRequests[0]
		retryAfter := rl.window - now.Sub(oldestRequest)

		limit.lastUsage = now
		return false, retryAfter
	}

	// Allow the request
	validRequests = append(validRequests, now)
	limit.requests = validRequests
	limit.lastUsage = now

	return true, 0
}

// cleanup removes stale user entries periodically
func (rl *UserRateLimiter) cleanup() {
	for {
		select {
		case <-rl.cleanupTick.C:
			rl.performCleanup()
		case <-rl.done:
			rl.cleanupTick.Stop()
			return
		}
	}
}

// performCleanup removes users who haven't made requests recently
func (rl *UserRateLimiter) performCleanup() {
	now := time.Now()
	staleThreshold := 30 * time.Minute

	rl.mu.Lock()
	defer rl.mu.Unlock()

	for userID, limit := range rl.limits {
		limit.mu.Lock()
		if now.Sub(limit.lastUsage) > staleThreshold {
			delete(rl.limits, userID)
			log.Debug().Str("user_id", userID).Msg("Cleaned up stale rate limit entry")
		}
		limit.mu.Unlock()
	}
}

// Close stops the cleanup goroutine
func (rl *UserRateLimiter) Close() {
	close(rl.done)
}

// GetStats returns current rate limiter statistics
func (rl *UserRateLimiter) GetStats() map[string]interface{} {
	rl.mu.RLock()
	defer rl.mu.RUnlock()

	return map[string]interface{}{
		"tracked_users": len(rl.limits),
		"max_requests":  rl.maxRequests,
		"window":        rl.window.String(),
	}
}

// WithUserRateLimit wraps a command handler with per-user rate limiting
func WithUserRateLimit(commandName string, rateLimiter *UserRateLimiter, handler CommandHandler) CommandHandler {
	return func(s *discordgo.Session, i *discordgo.InteractionCreate) {
		userID := i.Member.User.ID
		username := i.Member.User.Username

		allowed, retryAfter := rateLimiter.Allow(userID)

		if !allowed {
			// User has exceeded rate limit
			log.Warn().
				Str("command", commandName).
				Str("user_id", userID).
				Str("username", username).
				Dur("retry_after", retryAfter).
				Msg("User rate limit exceeded")

			message := fmt.Sprintf(
				"You are using commands too quickly. Please wait %s before trying again.",
				formatDuration(retryAfter),
			)
			respondError(s, i, message)
			return
		}

		// User is within rate limit, execute command
		handler(s, i)
	}
}

// formatDuration formats a duration in a user-friendly way
func formatDuration(d time.Duration) string {
	if d < time.Second {
		return "a moment"
	}
	if d < time.Minute {
		seconds := int(d.Seconds())
		if seconds == 1 {
			return "1 second"
		}
		return fmt.Sprintf("%d seconds", seconds)
	}
	minutes := int(d.Minutes())
	if minutes == 1 {
		return "1 minute"
	}
	return fmt.Sprintf("%d minutes", minutes)
}

// GlobalRateLimiters holds rate limiters for different command types
var GlobalRateLimiters = struct {
	Export   *UserRateLimiter
	Help     *UserRateLimiter
	Roast    *UserRateLimiter
	Duel     *UserRateLimiter
	Werewolf *UserRateLimiter
}{
	Export:   NewUserRateLimiter(3, 1*time.Hour),    // 3 per hour per bot-commands.md
	Help:     NewUserRateLimiter(20, 1*time.Minute), // 20 per minute (generous for help)
	Roast:    NewUserRateLimiter(5, 1*time.Minute),  // 5 per minute (costs GPT tokens)
	Duel:     NewUserRateLimiter(10, 1*time.Minute), // 10 per minute (no GPT cost)
	Werewolf: NewUserRateLimiter(10, 1*time.Minute), // 10 per minute (start/status/leave)
}

// CloseAllRateLimiters closes all global rate limiters
func CloseAllRateLimiters() {
	GlobalRateLimiters.Export.Close()
	GlobalRateLimiters.Help.Close()
	GlobalRateLimiters.Roast.Close()
	GlobalRateLimiters.Duel.Close()
	GlobalRateLimiters.Werewolf.Close()
}
