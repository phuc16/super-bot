package export

import (
	"fmt"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/phuc16/VCG-super-bot/internal/models"
	"github.com/rs/zerolog/log"
)

// Fetcher handles fetching messages from Discord
type Fetcher struct {
	session *discordgo.Session
}

// NewFetcher creates a new message fetcher
func NewFetcher(session *discordgo.Session) *Fetcher {
	return &Fetcher{session: session}
}

// FetchDateRange fetches messages within a date range.
// guildID is used to resolve server nicknames; pass empty string to skip.
func (f *Fetcher) FetchDateRange(channelID string, guildID string, startTime, endTime time.Time) ([]models.DiscordMessage, error) {
	log.Debug().
		Str("channel_id", channelID).
		Time("start", startTime).
		Time("end", endTime).
		Msg("Fetching messages by date range")

	// Pre-fetch server nicknames for better display names
	nicknames := f.fetchNicknames(guildID)

	var allMessages []models.DiscordMessage
	var beforeID string
	limit := 100         // Discord API limit per request
	maxMessages := 10000 // Our maximum limit

	for {
		// Fetch batch of messages
		var messages []*discordgo.Message
		var err error

		func() {
			defer func() {
				if r := recover(); r != nil {
					log.Warn().Interface("panic", r).Msg("Skipping batch due to unsupported component type")
					messages = []*discordgo.Message{}
				}
			}()

			messages, err = f.session.ChannelMessages(channelID, limit, beforeID, "", "")
			if err != nil {
				log.Error().Err(err).Msg("Failed to fetch messages")
			}
		}()

		if err != nil {
			return nil, fmt.Errorf("failed to fetch messages: %w", err)
		}

		if len(messages) == 0 {
			break
		}

		// Filter messages by date range
		for _, msg := range messages {
			timestamp := msg.Timestamp

			// Stop if we've gone past the start time
			if timestamp.Before(startTime) {
				return allMessages, nil
			}

			// Skip bot messages (e.g. previous export summaries posted by this bot)
			if msg.Author.Bot {
				continue
			}

			// Include message if within range
			if timestamp.After(startTime) && timestamp.Before(endTime) {
				replyAuthor, replyContent := resolveReplyTarget(msg, nicknames)
				allMessages = append(allMessages, models.DiscordMessage{
					ID:             msg.ID,
					Author:         resolveDisplayName(msg.Author, nicknames),
					Content:        msg.Content,
					Timestamp:      timestamp,
					HasAttach:      len(msg.Attachments) > 0 || len(msg.Embeds) > 0,
					ReplyToAuthor:  replyAuthor,
					ReplyToContent: replyContent,
				})

				// Check if we've hit the max limit
				if len(allMessages) >= maxMessages {
					log.Warn().Int("count", maxMessages).Msg("Reached maximum message limit")
					return allMessages, nil
				}
			}
		}

		// Set beforeID for next batch
		beforeID = messages[len(messages)-1].ID

		// Small delay to respect rate limits
		time.Sleep(100 * time.Millisecond)
	}

	log.Info().Int("count", len(allMessages)).Msg("Messages fetched successfully")
	return allMessages, nil
}

// fetchNicknames returns a map of userID → server nickname for the guild.
// Returns an empty map on error (non-fatal).
func (f *Fetcher) fetchNicknames(guildID string) map[string]string {
	m := make(map[string]string)
	if guildID == "" {
		return m
	}
	after := ""
	for {
		members, err := f.session.GuildMembers(guildID, after, 1000)
		if err != nil || len(members) == 0 {
			break
		}
		for _, mem := range members {
			if mem.User == nil {
				continue
			}
			if mem.Nick != "" {
				m[mem.User.ID] = mem.Nick
			}
		}
		if len(members) < 1000 {
			break
		}
		after = members[len(members)-1].User.ID
	}
	return m
}

// resolveReplyTarget extracts the author and (truncated) content of the message
// being replied to, using Discord's own reply/reference data rather than
// inferring it from surrounding text. Returns ("", "") if not a reply.
func resolveReplyTarget(msg *discordgo.Message, nicknames map[string]string) (author string, content string) {
	ref := msg.ReferencedMessage
	if ref == nil || ref.Author == nil {
		return "", ""
	}
	replyContent := ref.Content
	if len(replyContent) > 200 {
		replyContent = replyContent[:200] + "..."
	}
	return resolveDisplayName(ref.Author, nicknames), replyContent
}

// resolveDisplayName picks the best human-readable name for a user:
// server nickname > global display name > username (no # suffix, no @ prefix).
func resolveDisplayName(u *discordgo.User, nicknames map[string]string) string {
	if nicknames != nil {
		if nick, ok := nicknames[u.ID]; ok && nick != "" {
			return nick
		}
	}
	if u.GlobalName != "" {
		return u.GlobalName
	}
	return u.Username
}

// FetchRecentMessages fetches the last N messages (default 100)
func (f *Fetcher) FetchRecentMessages(channelID string, count int) ([]models.DiscordMessage, error) {
	if count <= 0 {
		count = 100
	}

	return f.FetchRecent(channelID, count)
}

// FetchRecent fetches the most recent N messages
func (f *Fetcher) FetchRecent(channelID string, limit int) ([]models.DiscordMessage, error) {
	log.Debug().
		Str("channel_id", channelID).
		Int("limit", limit).
		Msg("Fetching recent messages")

	if limit > 10000 {
		limit = 10000
	}

	var messages []*discordgo.Message
	var err error

	func() {
		defer func() {
			if r := recover(); r != nil {
				log.Warn().Interface("panic", r).Msg("Recovered from panic while fetching recent messages")
			}
		}()

		messages, err = f.session.ChannelMessages(channelID, limit, "", "", "")
		if err != nil {
			log.Error().Err(err).Msg("Failed to fetch messages")
		}
	}()

	if err != nil {
		return nil, fmt.Errorf("failed to fetch messages: %w", err)
	}

	var result []models.DiscordMessage
	for _, msg := range messages {
		replyAuthor, replyContent := resolveReplyTarget(msg, nil)
		result = append(result, models.DiscordMessage{
			ID:             msg.ID,
			Author:         resolveDisplayName(msg.Author, nil),
			Content:        msg.Content,
			Timestamp:      msg.Timestamp,
			HasAttach:      len(msg.Attachments) > 0 || len(msg.Embeds) > 0,
			ReplyToAuthor:  replyAuthor,
			ReplyToContent: replyContent,
		})
	}

	return result, nil
}
