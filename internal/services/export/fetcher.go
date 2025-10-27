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

// FetchDateRange fetches messages within a date range
func (f *Fetcher) FetchDateRange(channelID string, startTime, endTime time.Time) ([]models.DiscordMessage, error) {
	log.Debug().
		Str("channel_id", channelID).
		Time("start", startTime).
		Time("end", endTime).
		Msg("Fetching messages by date range")

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

			// Include message if within range
			if timestamp.After(startTime) && timestamp.Before(endTime) {
				allMessages = append(allMessages, models.DiscordMessage{
					ID:        msg.ID,
					Author:    msg.Author.Username + "#" + msg.Author.Discriminator,
					Content:   msg.Content,
					Timestamp: timestamp,
					HasAttach: len(msg.Attachments) > 0 || len(msg.Embeds) > 0,
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
		result = append(result, models.DiscordMessage{
			ID:        msg.ID,
			Author:    msg.Author.Username + "#" + msg.Author.Discriminator,
			Content:   msg.Content,
			Timestamp: msg.Timestamp,
			HasAttach: len(msg.Attachments) > 0 || len(msg.Embeds) > 0,
		})
	}

	return result, nil
}
