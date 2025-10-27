package gpt

import (
	"fmt"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
)

// CircuitState represents the state of the circuit breaker
type CircuitState string

const (
	// StateClosed means the circuit is closed and requests are allowed
	StateClosed CircuitState = "closed"
	// StateOpen means the circuit is open and requests are blocked
	StateOpen CircuitState = "open"
	// StateHalfOpen means the circuit is testing if the service has recovered
	StateHalfOpen CircuitState = "half-open"
)

// CircuitBreaker implements the circuit breaker pattern to prevent cascading failures
type CircuitBreaker struct {
	mu sync.RWMutex

	failureThreshold int           // Number of consecutive failures before opening
	resetTimeout     time.Duration // Time to wait before moving from open to half-open
	halfOpenMaxCalls int           // Number of test calls allowed in half-open state

	state                CircuitState
	failures             int
	consecutiveSuccesses int
	lastFailureTime      time.Time
	lastStateChange      time.Time
}

// NewCircuitBreaker creates a new circuit breaker with default settings
func NewCircuitBreaker() *CircuitBreaker {
	return &CircuitBreaker{
		failureThreshold: 5,               // Open after 5 consecutive failures
		resetTimeout:     5 * time.Minute, // Wait 5 minutes before testing recovery
		halfOpenMaxCalls: 3,               // Allow 3 test calls in half-open
		state:            StateClosed,
		lastStateChange:  time.Now(),
	}
}

// Execute runs the given function with circuit breaker protection
func (cb *CircuitBreaker) Execute(fn func() error) error {
	// Check if request is allowed
	if err := cb.beforeRequest(); err != nil {
		return err
	}

	// Execute the function
	err := fn()

	// Record the result
	cb.afterRequest(err)

	return err
}

// beforeRequest checks if a request should be allowed
func (cb *CircuitBreaker) beforeRequest() error {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	switch cb.state {
	case StateClosed:
		// Allow request
		return nil

	case StateOpen:
		// Check if we should transition to half-open
		if time.Since(cb.lastFailureTime) >= cb.resetTimeout {
			log.Info().
				Dur("elapsed", time.Since(cb.lastFailureTime)).
				Msg("Circuit breaker transitioning to half-open state")
			cb.state = StateHalfOpen
			cb.consecutiveSuccesses = 0
			cb.lastStateChange = time.Now()
			return nil
		}

		// Circuit is still open
		return fmt.Errorf("circuit breaker is open: GPT API temporarily unavailable (retry after %v)",
			cb.resetTimeout-time.Since(cb.lastFailureTime))

	case StateHalfOpen:
		// Allow limited requests to test recovery
		return nil

	default:
		return fmt.Errorf("unknown circuit breaker state: %s", cb.state)
	}
}

// afterRequest records the result of a request
func (cb *CircuitBreaker) afterRequest(err error) {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	if err == nil {
		cb.onSuccess()
	} else {
		cb.onFailure()
	}
}

// onSuccess handles a successful request
func (cb *CircuitBreaker) onSuccess() {
	switch cb.state {
	case StateClosed:
		// Reset failure count
		cb.failures = 0

	case StateHalfOpen:
		cb.consecutiveSuccesses++

		// After enough successes, close the circuit
		if cb.consecutiveSuccesses >= cb.halfOpenMaxCalls {
			log.Info().
				Int("successes", cb.consecutiveSuccesses).
				Msg("Circuit breaker transitioning to closed state")
			cb.state = StateClosed
			cb.failures = 0
			cb.consecutiveSuccesses = 0
			cb.lastStateChange = time.Now()
		}

	case StateOpen:
		// This shouldn't happen, but reset failures anyway
		cb.failures = 0
	}
}

// onFailure handles a failed request
func (cb *CircuitBreaker) onFailure() {
	cb.failures++
	cb.lastFailureTime = time.Now()

	switch cb.state {
	case StateClosed:
		// Check if we should open the circuit
		if cb.failures >= cb.failureThreshold {
			log.Warn().
				Int("failures", cb.failures).
				Int("threshold", cb.failureThreshold).
				Dur("reset_timeout", cb.resetTimeout).
				Msg("Circuit breaker opening due to consecutive failures")
			cb.state = StateOpen
			cb.lastStateChange = time.Now()
		}

	case StateHalfOpen:
		// Failure during half-open immediately reopens the circuit
		log.Warn().
			Msg("Circuit breaker reopening after failure in half-open state")
		cb.state = StateOpen
		cb.consecutiveSuccesses = 0
		cb.lastStateChange = time.Now()
	}
}

// GetState returns the current circuit breaker state
func (cb *CircuitBreaker) GetState() CircuitState {
	cb.mu.RLock()
	defer cb.mu.RUnlock()
	return cb.state
}

// GetStats returns current circuit breaker statistics
func (cb *CircuitBreaker) GetStats() map[string]interface{} {
	cb.mu.RLock()
	defer cb.mu.RUnlock()

	return map[string]interface{}{
		"state":                 string(cb.state),
		"failures":              cb.failures,
		"consecutive_successes": cb.consecutiveSuccesses,
		"last_failure_time":     cb.lastFailureTime,
		"last_state_change":     cb.lastStateChange,
		"time_in_state":         time.Since(cb.lastStateChange),
	}
}

// Reset manually resets the circuit breaker to closed state
func (cb *CircuitBreaker) Reset() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	log.Info().Msg("Circuit breaker manually reset to closed state")
	cb.state = StateClosed
	cb.failures = 0
	cb.consecutiveSuccesses = 0
	cb.lastStateChange = time.Now()
}
