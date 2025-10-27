package export

import (
	"fmt"
	"sync"
	"time"
)

// RateLimiter manages per-user rate limiting for exports
type RateLimiter struct {
	mu            sync.Mutex
	userLimits    map[string]*userLimit
	maxPerHour    int
	cleanupTicker *time.Ticker
	stopChan      chan struct{}
}

// userLimit tracks export requests for a single user
type userLimit struct {
	timestamps []time.Time
}

// NewRateLimiter creates a new rate limiter
func NewRateLimiter(maxPerHour int) *RateLimiter {
	rl := &RateLimiter{
		userLimits: make(map[string]*userLimit),
		maxPerHour: maxPerHour,
		stopChan:   make(chan struct{}),
	}

	// Start cleanup goroutine
	rl.cleanupTicker = time.NewTicker(5 * time.Minute)
	go rl.cleanupExpired()

	return rl
}

// Allow checks if a user is allowed to make a request
func (rl *RateLimiter) Allow(userID string) (bool, int, error) {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	oneHourAgo := now.Add(-1 * time.Hour)

	// Get or create user limit
	limit, exists := rl.userLimits[userID]
	if !exists {
		limit = &userLimit{timestamps: []time.Time{}}
		rl.userLimits[userID] = limit
	}

	// Remove timestamps older than 1 hour
	validTimestamps := []time.Time{}
	for _, ts := range limit.timestamps {
		if ts.After(oneHourAgo) {
			validTimestamps = append(validTimestamps, ts)
		}
	}
	limit.timestamps = validTimestamps

	// Check if under limit
	if len(limit.timestamps) >= rl.maxPerHour {
		// Calculate minutes until oldest request expires
		oldestRequest := limit.timestamps[0]
		minutesUntilExpiry := int(time.Until(oldestRequest.Add(1 * time.Hour)).Minutes())
		if minutesUntilExpiry < 0 {
			minutesUntilExpiry = 0
		}

		return false, minutesUntilExpiry, fmt.Errorf("rate limit exceeded: %d/%d requests used", len(limit.timestamps), rl.maxPerHour)
	}

	// Add current timestamp
	limit.timestamps = append(limit.timestamps, now)

	return true, 0, nil
}

// GetUsage returns the current usage for a user
func (rl *RateLimiter) GetUsage(userID string) (int, int) {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	limit, exists := rl.userLimits[userID]
	if !exists {
		return 0, rl.maxPerHour
	}

	now := time.Now()
	oneHourAgo := now.Add(-1 * time.Hour)

	// Count valid timestamps
	count := 0
	for _, ts := range limit.timestamps {
		if ts.After(oneHourAgo) {
			count++
		}
	}

	return count, rl.maxPerHour
}

// cleanupExpired removes expired rate limit data
func (rl *RateLimiter) cleanupExpired() {
	for {
		select {
		case <-rl.cleanupTicker.C:
			rl.mu.Lock()
			now := time.Now()
			oneHourAgo := now.Add(-1 * time.Hour)

			// Clean up users with no recent requests
			for userID, limit := range rl.userLimits {
				validTimestamps := []time.Time{}
				for _, ts := range limit.timestamps {
					if ts.After(oneHourAgo) {
						validTimestamps = append(validTimestamps, ts)
					}
				}

				if len(validTimestamps) == 0 {
					delete(rl.userLimits, userID)
				} else {
					limit.timestamps = validTimestamps
				}
			}
			rl.mu.Unlock()

		case <-rl.stopChan:
			rl.cleanupTicker.Stop()
			return
		}
	}
}

// Stop stops the rate limiter cleanup goroutine
func (rl *RateLimiter) Stop() {
	close(rl.stopChan)
}
