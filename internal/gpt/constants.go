package gpt

import "time"

const (
	// GPTRequestTimeout is the maximum time for a single GPT request
	GPTRequestTimeout = 90 * time.Second

	// GPT4TurboContextLimit is the maximum context window for GPT-4-turbo
	GPT4TurboContextLimit = 128000

	// GPT35TurboContextLimit is the maximum context window for GPT-3.5-turbo
	GPT35TurboContextLimit = 16000

	// MaxResponseTokens is the maximum tokens reserved for GPT response
	MaxResponseTokens = 4096
)
