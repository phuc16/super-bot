package gpt

import (
	"context"
	"fmt"

	openai "github.com/sashabaranov/go-openai"
)

const (
	// SystemPromptEN defines the GPT system role for summarization in English
	SystemPromptEN = `You are an AI assistant specialized in summarizing Discord conversations. Your goal is to provide clear, actionable summaries that help users quickly understand what happened in a conversation without reading every message.

Guidelines:
1. Identify and list the main discussion topics
2. Highlight any decisions that were made
3. Extract action items or tasks mentioned
4. Note any important announcements or events
5. Summarize the overall tone and sentiment
6. Be concise but comprehensive
7. Use bullet points for clarity
8. Avoid speculation - only summarize what was actually discussed

Format your response with these sections:
- Executive Summary (2-3 sentences)
- Key Topics (bullet points)
- Decisions Made (if any)
- Action Items (if any)
- Notable Events/Announcements (if any)`

	// SystemPromptVI defines the GPT system role for summarization in Vietnamese
	SystemPromptVI = `Bạn là trợ lý AI chuyên tóm tắt các cuộc trò chuyện Discord. Mục tiêu của bạn là cung cấp các bản tóm tắt rõ ràng, có thể thực thi giúp người dùng nhanh chóng hiểu điều gì đã xảy ra trong cuộc trò chuyện mà không cần đọc từng tin nhắn.

Nguyên tắc:
1. Xác định và liệt kê các chủ đề thảo luận chính
2. Làm nổi bật các quyết định đã được đưa ra
3. Trích xuất các hành động hoặc nhiệm vụ được đề cập
4. Ghi chú các thông báo hoặc sự kiện quan trọng
5. Tóm tắt giọng điệu và cảm xúc tổng thể
6. Ngắn gọn nhưng đầy đủ
7. Sử dụng dấu đầu dòng cho rõ ràng
8. Tránh suy đoán - chỉ tóm tắt những gì thực sự được thảo luận

Định dạng phản hồi với các phần:
- Tóm Tắt Điều Hành (2-3 câu)
- Chủ Đề Chính (dấu đầu dòng)
- Quyết Định Đã Đưa Ra (nếu có)
- Hành Động Cần Làm (nếu có)
- Sự Kiện/Thông Báo Đáng Chú Ý (nếu có)`
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
