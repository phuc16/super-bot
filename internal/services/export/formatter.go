package export

import (
	"fmt"
	"strings"

	"github.com/phuc16/VCG-super-bot/internal/models"
)

// Formatter handles formatting messages for GPT
type Formatter struct{}

// NewFormatter creates a new message formatter
func NewFormatter() *Formatter {
	return &Formatter{}
}

// FormatForGPT formats Discord messages into a text format suitable for GPT
func (f *Formatter) FormatForGPT(messages []models.DiscordMessage, channelName string) string {
	if len(messages) == 0 {
		return "No messages found."
	}

	var sb strings.Builder

	// Header information
	sb.WriteString(fmt.Sprintf("Channel: #%s\n", channelName))

	if len(messages) > 0 {
		startTime := messages[len(messages)-1].Timestamp // Oldest
		endTime := messages[0].Timestamp                 // Newest
		sb.WriteString(fmt.Sprintf("Date Range: %s to %s\n",
			startTime.Format("2006-01-02 15:04:05"),
			endTime.Format("2006-01-02 15:04:05")))
	}

	sb.WriteString(fmt.Sprintf("Total Messages: %d\n", len(messages)))

	// Count unique participants
	participants := make(map[string]bool)
	for _, msg := range messages {
		participants[msg.Author] = true
	}
	sb.WriteString(fmt.Sprintf("Participants: %d users\n\n", len(participants)))
	sb.WriteString("---\n\n")

	// Format messages (newest first to oldest)
	for i := len(messages) - 1; i >= 0; i-- {
		msg := messages[i]

		// Format timestamp
		timestamp := msg.Timestamp.Format("2006-01-02 15:04:05")

		// Format content
		content := msg.Content
		if content == "" {
			content = "[empty message]"
		}

		// Truncate very long messages
		if len(content) > 500 {
			content = content[:497] + "..."
		}

		// Redact sensitive content
		content = f.redactSensitiveContent(content)

		// Add attachment indicator
		if msg.HasAttach {
			content += " [has attachments]"
		}

		sb.WriteString(fmt.Sprintf("[%s] @%s: %s\n\n", timestamp, msg.Author, content))
	}

	return sb.String()
}

// redactSensitiveContent removes or masks sensitive information
func (f *Formatter) redactSensitiveContent(content string) string {
	// Simple redaction patterns (can be enhanced)

	// Redact tokens/API keys (basic pattern)
	if strings.Contains(strings.ToLower(content), "token") ||
		strings.Contains(strings.ToLower(content), "api") ||
		strings.Contains(strings.ToLower(content), "key") ||
		strings.Contains(strings.ToLower(content), "secret") ||
		strings.Contains(strings.ToLower(content), "password") {

		// Look for patterns like "token=xyz", "API_KEY: xyz", etc.
		words := strings.Fields(content)
		for i, word := range words {
			lower := strings.ToLower(word)
			if strings.Contains(lower, "token") ||
				strings.Contains(lower, "key") ||
				strings.Contains(lower, "secret") ||
				strings.Contains(lower, "password") {
				// Redact next word if it looks like a value
				if i+1 < len(words) && len(words[i+1]) > 10 {
					words[i+1] = "[REDACTED]"
				}
			}
		}
		content = strings.Join(words, " ")
	}

	return content
}

// EstimateTokens provides a rough estimate of GPT tokens
// Rule of thumb: ~4 characters per token for English text
func (f *Formatter) EstimateTokens(text string) int {
	return len(text) / 4
}
