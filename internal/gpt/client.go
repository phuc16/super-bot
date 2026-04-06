package gpt

import (
	"context"
	"fmt"
	"strings"

	"github.com/rs/zerolog/log"
	openai "github.com/sashabaranov/go-openai"
)

// Client wraps the OpenAI client
type Client struct {
	client         *openai.Client
	model          string
	maxTokens      int
	mockMode       bool
	costTracker    *CostTracker
	circuitBreaker *CircuitBreaker
	mockLanguage   string // For mock responses, default "vi"
}

// NewClient creates a new GPT client
func NewClient(apiKey string, model string, maxTokens int, mockMode bool, costTracker *CostTracker) *Client {
	circuitBreaker := NewCircuitBreaker()

	if mockMode {
		log.Info().Msg("GPT client initialized in MOCK mode (no API calls)")
		return &Client{
			model:          model,
			maxTokens:      maxTokens,
			mockMode:       true,
			costTracker:    costTracker,
			circuitBreaker: circuitBreaker,
			mockLanguage:   "vi", // Default to Vietnamese
		}
	}

	client := openai.NewClient(apiKey)
	log.Info().Str("model", model).Msg("GPT client initialized with retry and circuit breaker")

	return &Client{
		client:         client,
		model:          model,
		maxTokens:      maxTokens,
		mockMode:       false,
		costTracker:    costTracker,
		circuitBreaker: circuitBreaker,
	}
}

// CreateChatCompletionWithParams sends a request to GPT with explicit temperature and maxTokens
func (c *Client) CreateChatCompletionWithParams(ctx context.Context, messages []openai.ChatCompletionMessage, temperature float32, maxTokens int) (string, int, error) {
	if c.mockMode {
		return c.CreateChatCompletion(ctx, messages)
	}
	if c.costTracker != nil && !c.costTracker.CanProceed() {
		return "", 0, fmt.Errorf("daily GPT budget exceeded")
	}
	req := openai.ChatCompletionRequest{
		Model:            c.model,
		Messages:         messages,
		MaxTokens:        maxTokens,
		Temperature:      float32(temperature),
		TopP:             1.0,
		FrequencyPenalty: 0.1,
		PresencePenalty:  0.0,
	}
	resp, err := c.client.CreateChatCompletion(ctx, req)
	if err != nil {
		return "", 0, fmt.Errorf("GPT API error: %w", err)
	}
	if len(resp.Choices) == 0 {
		return "", 0, fmt.Errorf("no completion choices returned")
	}
	content := resp.Choices[0].Message.Content
	tokens := resp.Usage.TotalTokens
	if c.costTracker != nil {
		c.costTracker.RecordUsage(resp.Usage.PromptTokens, resp.Usage.CompletionTokens, resp.Model)
	}
	return content, tokens, nil
}

// CreateChatCompletion sends a request to GPT and returns the response
func (c *Client) CreateChatCompletion(ctx context.Context, messages []openai.ChatCompletionMessage) (string, int, error) {
	// Mock mode - return fake response
	if c.mockMode {
		// Detect language from system prompt
		mockResponse := ""
		if len(messages) > 0 && messages[0].Role == openai.ChatMessageRoleSystem {
			systemPrompt := messages[0].Content
			if containsVietnameseChars(systemPrompt) {
				mockResponse = `Tóm Tắt Điều Hành:
Đây là bản tóm tắt mẫu được tạo ra mà không cần gọi API OpenAI.

Chủ Đề Chính:
• Chủ đề mẫu 1
• Chủ đề mẫu 2
• Chủ đề mẫu 3

Quyết Định Đã Đưa Ra:
• Quyết định mẫu

Hành Động Cần Làm:
• Hành động mẫu`
			} else {
				mockResponse = `Executive Summary:
This is a mock summary generated without calling the OpenAI API.

Key Topics:
• Mock topic 1
• Mock topic 2
• Mock topic 3

Decisions Made:
• Mock decision

Action Items:
• Mock action item`
			}
		} else {
			mockResponse = `Tóm Tắt Điều Hành:
Đây là bản tóm tắt mẫu được tạo ra mà không cần gọi API OpenAI.

Chủ Đề Chính:
• Chủ đề mẫu 1
• Chủ đề mẫu 2
• Chủ đề mẫu 3

Quyết Định Đã Đưa Ra:
• Quyết định mẫu

Hành Động Cần Làm:
• Hành động mẫu`
		}

		log.Debug().Msg("Returning mock GPT response")
		return mockResponse, 100, nil // Mock token count
	}

	// Check budget before API call
	if c.costTracker != nil && !c.costTracker.CanProceed() {
		return "", 0, fmt.Errorf("daily GPT budget exceeded")
	}

	// Real API call
	req := openai.ChatCompletionRequest{
		Model:            c.model,
		Messages:         messages,
		MaxTokens:        c.maxTokens,
		Temperature:      0.7,
		TopP:             1.0,
		FrequencyPenalty: 0.3,
		PresencePenalty:  0.1,
	}

	resp, err := c.client.CreateChatCompletion(ctx, req)
	if err != nil {
		return "", 0, fmt.Errorf("GPT API error: %w", err)
	}

	if len(resp.Choices) == 0 {
		return "", 0, fmt.Errorf("no completion choices returned")
	}

	content := resp.Choices[0].Message.Content
	tokens := resp.Usage.TotalTokens

	// Record usage and cost
	if c.costTracker != nil {
		cost := c.costTracker.RecordUsage(
			resp.Usage.PromptTokens,
			resp.Usage.CompletionTokens,
			resp.Model,
		)

		log.Debug().
			Int("input_tokens", resp.Usage.PromptTokens).
			Int("output_tokens", resp.Usage.CompletionTokens).
			Int("total_tokens", tokens).
			Float64("cost", cost).
			Str("model", resp.Model).
			Msg("GPT completion received")
	} else {
		log.Debug().
			Int("tokens", tokens).
			Str("model", resp.Model).
			Msg("GPT completion received")
	}

	return content, tokens, nil
}

// CreateChatCompletionWithRetry sends a request to GPT with retry logic and circuit breaker protection
func (c *Client) CreateChatCompletionWithRetry(ctx context.Context, messages []openai.ChatCompletionMessage) (string, int, error) {
	var result string
	var tokens int
	var callErr error

	// Execute with circuit breaker protection
	err := c.circuitBreaker.Execute(func() error {
		// Use retry logic
		result, tokens, callErr = c.CallWithRetry(ctx, messages)
		return callErr
	})

	if err != nil {
		// Check if it's a circuit breaker error
		if contains(err.Error(), "circuit breaker is open") {
			log.Warn().
				Str("state", string(c.circuitBreaker.GetState())).
				Msg("GPT request blocked by circuit breaker")
			return "", 0, fmt.Errorf("GPT service temporarily unavailable: %w", err)
		}
		return "", 0, err
	}

	return result, tokens, nil
}

// GetCircuitBreakerState returns the current circuit breaker state
func (c *Client) GetCircuitBreakerState() CircuitState {
	return c.circuitBreaker.GetState()
}

// GetCircuitBreakerStats returns circuit breaker statistics
func (c *Client) GetCircuitBreakerStats() map[string]interface{} {
	return c.circuitBreaker.GetStats()
}

// IsMockMode returns whether the client is in mock mode
func (c *Client) IsMockMode() bool {
	return c.mockMode
}

// containsVietnameseChars checks if a string contains Vietnamese characters
func containsVietnameseChars(text string) bool {
	return strings.Contains(text, "Tiếng Việt") ||
		strings.Contains(text, "tóm tắt") ||
		strings.Contains(text, "Chủ Đề") ||
		strings.Contains(text, "Quyết Định")
}
