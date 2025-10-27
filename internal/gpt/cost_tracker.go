package gpt

import (
	"sync"
	"time"

	"github.com/rs/zerolog/log"
)

// CostTracker tracks GPT API costs and enforces budget limits
type CostTracker struct {
	mu               sync.Mutex
	dailyBudgetUSD   float64
	currentDaySpend  float64
	currentDay       time.Time
	totalTokensToday int
	requestsToday    int
}

// NewCostTracker creates a new cost tracker
func NewCostTracker(dailyBudgetUSD float64) *CostTracker {
	return &CostTracker{
		dailyBudgetUSD:  dailyBudgetUSD,
		currentDay:      time.Now().Truncate(24 * time.Hour),
		currentDaySpend: 0,
	}
}

// RecordUsage records token usage and calculates cost
func (ct *CostTracker) RecordUsage(inputTokens, outputTokens int, model string) float64 {
	ct.mu.Lock()
	defer ct.mu.Unlock()

	// Check if it's a new day
	today := time.Now().Truncate(24 * time.Hour)
	if today.After(ct.currentDay) {
		log.Info().
			Float64("yesterday_spend", ct.currentDaySpend).
			Int("yesterday_tokens", ct.totalTokensToday).
			Int("yesterday_requests", ct.requestsToday).
			Msg("Daily GPT usage reset")

		ct.currentDay = today
		ct.currentDaySpend = 0
		ct.totalTokensToday = 0
		ct.requestsToday = 0
	}

	// Calculate cost based on model
	cost := ct.calculateCost(inputTokens, outputTokens, model)

	// Update tracking
	ct.currentDaySpend += cost
	ct.totalTokensToday += inputTokens + outputTokens
	ct.requestsToday++

	// Check budget thresholds
	percentUsed := (ct.currentDaySpend / ct.dailyBudgetUSD) * 100

	if percentUsed >= 100 {
		log.Error().
			Float64("spend", ct.currentDaySpend).
			Float64("budget", ct.dailyBudgetUSD).
			Msg("🚨 CRITICAL: Daily GPT budget exceeded!")
	} else if percentUsed >= 80 {
		log.Warn().
			Float64("spend", ct.currentDaySpend).
			Float64("budget", ct.dailyBudgetUSD).
			Float64("percent", percentUsed).
			Msg("⚠️ WARNING: GPT budget at 80%")
	} else if percentUsed >= 50 {
		log.Warn().
			Float64("spend", ct.currentDaySpend).
			Float64("budget", ct.dailyBudgetUSD).
			Float64("percent", percentUsed).
			Msg("GPT budget at 50%")
	}

	log.Debug().
		Int("input_tokens", inputTokens).
		Int("output_tokens", outputTokens).
		Float64("cost", cost).
		Float64("daily_total", ct.currentDaySpend).
		Str("model", model).
		Msg("GPT usage recorded")

	return cost
}

// calculateCost computes cost based on token counts and model
func (ct *CostTracker) calculateCost(inputTokens, outputTokens int, model string) float64 {
	// Pricing per 1K tokens (as of 2025)
	var inputCostPer1k, outputCostPer1k float64

	switch model {
	case "gpt-4-turbo-preview", "gpt-4-turbo":
		inputCostPer1k = 0.01
		outputCostPer1k = 0.03
	case "gpt-3.5-turbo":
		inputCostPer1k = 0.0015
		outputCostPer1k = 0.002
	default:
		// Default to GPT-4-turbo pricing
		inputCostPer1k = 0.01
		outputCostPer1k = 0.03
	}

	inputCost := (float64(inputTokens) / 1000.0) * inputCostPer1k
	outputCost := (float64(outputTokens) / 1000.0) * outputCostPer1k

	return inputCost + outputCost
}

// CanProceed checks if we're within budget
func (ct *CostTracker) CanProceed() bool {
	ct.mu.Lock()
	defer ct.mu.Unlock()

	// Check if it's a new day
	today := time.Now().Truncate(24 * time.Hour)
	if today.After(ct.currentDay) {
		return true // New day, budget reset
	}

	return ct.currentDaySpend < ct.dailyBudgetUSD
}

// GetCurrentSpend returns today's spending
func (ct *CostTracker) GetCurrentSpend() (float64, float64) {
	ct.mu.Lock()
	defer ct.mu.Unlock()

	// Check if it's a new day
	today := time.Now().Truncate(24 * time.Hour)
	if today.After(ct.currentDay) {
		return 0, ct.dailyBudgetUSD
	}

	return ct.currentDaySpend, ct.dailyBudgetUSD
}

// GetStats returns usage statistics
func (ct *CostTracker) GetStats() map[string]interface{} {
	ct.mu.Lock()
	defer ct.mu.Unlock()

	// Check if it's a new day
	today := time.Now().Truncate(24 * time.Hour)
	if today.After(ct.currentDay) {
		return map[string]interface{}{
			"daily_spend":    0.0,
			"daily_budget":   ct.dailyBudgetUSD,
			"total_tokens":   0,
			"total_requests": 0,
			"percent_used":   0.0,
		}
	}

	percentUsed := (ct.currentDaySpend / ct.dailyBudgetUSD) * 100

	return map[string]interface{}{
		"daily_spend":    ct.currentDaySpend,
		"daily_budget":   ct.dailyBudgetUSD,
		"total_tokens":   ct.totalTokensToday,
		"total_requests": ct.requestsToday,
		"percent_used":   percentUsed,
	}
}
