package gpt

import (
	"context"
	"fmt"

	openai "github.com/sashabaranov/go-openai"
)

const (
	// SystemPromptEN defines the GPT system role for summarization in English
	SystemPromptEN = `You are summarizing a Discord chat conversation. Write like a friend catching someone up — casual, specific, and actually useful. Not a corporate report.

Rules:
- Be SPECIFIC: mention actual usernames, actual things said, actual jokes — not vague descriptions like "they talked about drinks"
- Paraphrase or lightly quote the funniest/most interesting moments directly
- Only include sections that have real content — skip empty ones
- Keep it concise but packed with detail
- Don't pad or repeat yourself

Format (only include sections that apply):
**TL;DR** — 1-2 sentences on the overall vibe
**What went down** — concrete bullet points: who said what, what actually happened, specific topics with actual content
**Best moments** — the funniest, most interesting, or most notable specific exchanges (with usernames)
**Heads up** — actual plans, decisions, or announcements (skip if none)`

	// SystemPromptVI defines the GPT system role for summarization in Vietnamese
	SystemPromptVI = `Bạn đang tóm tắt đoạn chat Discord. Viết như kể lại cho bạn bè — tự nhiên, cụ thể, và thực sự có ích. Không phải báo cáo công ty.

Nguyên tắc:
- Phải CỤ THỂ: nhắc tên người dùng thật, nội dung thật, trò đùa thật — không được viết chung chung kiểu "họ nói về đồ uống"
- Paraphrase hoặc trích dẫn nhẹ những khoảnh khắc buồn cười hoặc đáng chú ý nhất
- Chỉ đưa vào phần nào thực sự có nội dung — bỏ qua phần trống
- Ngắn gọn nhưng đầy chi tiết — không dài dòng, không lặp lại
- Dùng ngôn ngữ tự nhiên, vui khi chat vui

Format (chỉ dùng phần nào có nội dung):
**Tóm lại** — 1-2 câu về không khí chung
**Chuyện gì xảy ra** — gạch đầu dòng cụ thể: ai nói gì, chủ đề gì với nội dung thật, không nói chung chung
**Khoảnh khắc đáng nhớ** — những đoạn buồn cười, thú vị hoặc nổi bật nhất, có tên người (bỏ qua nếu không có gì đặc biệt)
**Cần biết** — kế hoạch, quyết định, thông báo thực sự (bỏ qua nếu không có)`
)

// Summarizer handles AI summarization of messages
type Summarizer struct {
	client   *Client
	language string // "vi" for Vietnamese, "en" for English
}

// NewSummarizer creates a new summarizer (default Vietnamese)
func NewSummarizer(client *Client) *Summarizer {
	return &Summarizer{
		client:   client,
		language: "vi",
	}
}

// SetLanguage changes the summarization language
func (s *Summarizer) SetLanguage(language string) {
	s.language = language
}

// Summarize generates an AI summary of formatted messages
func (s *Summarizer) Summarize(ctx context.Context, formattedMessages string, channelName string) (string, int, error) {
	// Select system prompt based on language
	var systemPrompt, userPrompt string
	if s.language == "vi" {
		systemPrompt = SystemPromptVI
		userPrompt = fmt.Sprintf(
			"Hãy tóm tắt cuộc trò chuyện Discord sau từ kênh #%s:\n\n%s\n\nHãy cung cấp một bản tóm tắt toàn diện theo định dạng được quy định trong hướng dẫn của bạn.",
			channelName,
			formattedMessages,
		)
	} else {
		systemPrompt = SystemPromptEN
		userPrompt = fmt.Sprintf(
			"Please summarize the following Discord conversation from #%s:\n\n%s\n\nProvide a comprehensive summary following the format specified in your instructions.",
			channelName,
			formattedMessages,
		)
	}

	// Create message array
	messages := []openai.ChatCompletionMessage{
		{
			Role:    openai.ChatMessageRoleSystem,
			Content: systemPrompt,
		},
		{
			Role:    openai.ChatMessageRoleUser,
			Content: userPrompt,
		},
	}

	// Call GPT API with retry and circuit breaker protection
	summary, tokens, err := s.client.CreateChatCompletionWithRetry(ctx, messages)
	if err != nil {
		return "", 0, fmt.Errorf("failed to generate summary: %w", err)
	}

	return summary, tokens, nil
}

// SummarizeWithFallback attempts to summarize, with fallback for errors
func (s *Summarizer) SummarizeWithFallback(ctx context.Context, formattedMessages string, channelName string) (summary string, tokens int, status string, err error) {
	summary, tokens, err = s.Summarize(ctx, formattedMessages, channelName)

	if err != nil {
		// Return error status but no actual error (graceful degradation)
		return "", 0, "failed", nil
	}

	return summary, tokens, "completed", nil
}
