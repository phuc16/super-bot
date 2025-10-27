package gpt

// EstimateTokens provides a rough estimate of GPT tokens
// Rule of thumb: ~4 characters per token for English text
func EstimateTokens(text string) int {
	return len(text) / 4
}

// EstimateTokensFromMessages estimates tokens for a message array
func EstimateTokensFromMessages(systemPrompt, userPrompt string) int {
	// System prompt tokens
	systemTokens := EstimateTokens(systemPrompt)

	// User prompt tokens
	userTokens := EstimateTokens(userPrompt)

	// Add overhead for message formatting (~50 tokens)
	overhead := 50

	return systemTokens + userTokens + overhead
}

// ValidateTokenLimit checks if estimated tokens are within limits
func ValidateTokenLimit(estimatedTokens int, maxContextTokens int) error {
	// Leave buffer for response (default 4096 tokens)
	responseBuffer := 4096
	availableTokens := maxContextTokens - responseBuffer

	if estimatedTokens > availableTokens {
		return ErrTokenLimitExceeded{
			Estimated: estimatedTokens,
			Limit:     availableTokens,
		}
	}

	return nil
}

// ErrTokenLimitExceeded is returned when token estimate exceeds limit
type ErrTokenLimitExceeded struct {
	Estimated int
	Limit     int
}

func (e ErrTokenLimitExceeded) Error() string {
	return "estimated tokens exceed limit"
}
