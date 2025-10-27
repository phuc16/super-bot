package commands

import (
	"context"
	"fmt"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/rs/zerolog/log"

	"github.com/phuc16/VCG-super-bot/internal/models"
	"github.com/phuc16/VCG-super-bot/internal/services/export"
)

// ExportCommand handles the /export slash command
type ExportCommand struct {
	service     *export.Service
	rateLimiter *export.RateLimiter
	timezone    *time.Location
}

// NewExportCommand creates a new export command handler
func NewExportCommand(service *export.Service, rateLimiter *export.RateLimiter, timezone string) *ExportCommand {
	// Parse timezone, default to UTC if invalid
	loc, err := time.LoadLocation(timezone)
	if err != nil {
		loc = time.UTC
		log.Warn().Str("timezone", timezone).Msg("Invalid timezone, using UTC")
	}

	return &ExportCommand{
		service:     service,
		rateLimiter: rateLimiter,
		timezone:    loc,
	}
}

// Definition returns the command definition for Discord
func (cmd *ExportCommand) Definition() *discordgo.ApplicationCommand {
	return &discordgo.ApplicationCommand{
		Name:        "export",
		Description: "Export and summarize messages from this channel",
		Options: []*discordgo.ApplicationCommandOption{
			{
				Type:        discordgo.ApplicationCommandOptionString,
				Name:        "type",
				Description: "Export type: date-range or recent",
				Required:    true,
				Choices: []*discordgo.ApplicationCommandOptionChoice{
					{Name: "Date Range", Value: "date_range"},
					{Name: "Recent Messages", Value: "recent"},
				},
			},
			{
				Type:        discordgo.ApplicationCommandOptionInteger,
				Name:        "count",
				Description: "Last N messages (only for Recent type, default: 100, max: 500)",
				Required:    false,
			},
			{
				Type:        discordgo.ApplicationCommandOptionString,
				Name:        "from",
				Description: "Start date/time (only for Date Range)",
				Required:    false,
			},
			{
				Type:        discordgo.ApplicationCommandOptionString,
				Name:        "end",
				Description: "End date/time (only for Date Range)",
				Required:    false,
			},
			{
				Type:        discordgo.ApplicationCommandOptionString,
				Name:        "language",
				Description: "Summary language (english or vietnamese)",
				Required:    false,
				Choices: []*discordgo.ApplicationCommandOptionChoice{
					{Name: "English", Value: "en"},
					{Name: "Tiếng Việt", Value: "vi"},
				},
			},
		},
	}
}

// Handle processes the command interaction
func (cmd *ExportCommand) Handle(s *discordgo.Session, i *discordgo.InteractionCreate) {
	// Parse options
	options := parseOptions(i.ApplicationCommandData().Options)
	exportType := options["type"].(string)

	// Get language option (default to Vietnamese)
	language := "vi"
	if lang, ok := options["language"].(string); ok {
		language = lang
	}

	// Get count option (default to 100 for recent type)
	messageCount := 100
	if count, ok := options["count"].(float64); ok {
		messageCount = int(count)
		if messageCount < 1 || messageCount > 500 {
			respondError(s, i, "Message count must be between 1 and 500")
			return
		}
	}

	userID := i.Member.User.ID
	channelID := i.ChannelID
	guildID := i.GuildID

	// Get channel info
	channel, err := s.Channel(channelID)
	if err != nil {
		respondError(s, i, "Failed to get channel information")
		return
	}

	// Check rate limit
	allowed, minutesLeft, err := cmd.rateLimiter.Allow(userID)
	if !allowed {
		respondRateLimited(s, i, minutesLeft)
		return
	}

	// Defer response (processing takes time)
	err = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseDeferredChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Content: "⏳ Exporting messages... This may take up to 60 seconds.",
			Flags:   discordgo.MessageFlagsEphemeral,
		},
	})
	if err != nil {
		log.Error().Err(err).Msg("Failed to defer response")
		return
	}

	// Parse export parameters
	req := export.ExportRequest{
		ChannelID:    channelID,
		ChannelName:  channel.Name,
		GuildID:      guildID,
		UserID:       userID,
		ExportType:   models.ExportType(exportType),
		Language:     language,
		MessageCount: messageCount,
	}

	// Handle date-range type
	if exportType == string(models.ExportTypeDateRange) {
		// Parse from and end datetimes
		fromStr, hasFrom := options["from"].(string)
		endStr, hasEnd := options["end"].(string)

		if !hasFrom || !hasEnd {
			followUpError(s, i, "For date-range export, both 'from' and 'end' datetime are required.\nFormat: YYYY-MM-DD HH:MM or YYYY-MM-DD")
			return
		}

		// Validate date-time range using validation utility with timezone
		fromTime, endTime, err := ValidateDateTimeRangeWithTimezone(fromStr, endStr, cmd.timezone)
		if err != nil {
			followUpError(s, i, err.Error())
			return
		}

		req.StartTime = &fromTime
		req.EndTime = &endTime
	}

	// Execute export asynchronously
	go cmd.processExport(s, i, req)
}

// processExport handles the actual export processing
func (cmd *ExportCommand) processExport(s *discordgo.Session, i *discordgo.InteractionCreate, req export.ExportRequest) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	// Perform export
	result, err := cmd.service.Export(ctx, req)
	if err != nil {
		log.Error().Err(err).Msg("Export failed")
		followUpError(s, i, fmt.Sprintf("Export failed: %s", err.Error()))
		return
	}

	// Build response message
	var content string
	if result.SummaryStatus == models.SummaryStatusCompleted {
		content = fmt.Sprintf("📊 **Message Export Complete**\n\n"+
			"**Channel**: <#%s>\n"+
			"**Messages Exported**: %d\n"+
			"**Processing Time**: %.1fs\n\n"+
			"**AI Summary**:\n%s\n\n"+
			"_Export ID: %s | Tokens used: %d_",
			req.ChannelID,
			result.MessageCount,
			float64(result.ProcessingDurationMs)/1000.0,
			result.Summary,
			result.ExportID.String()[:8],
			result.TokensUsed,
		)
	} else if result.SummaryStatus == models.SummaryStatusSkipped {
		content = fmt.Sprintf("📊 **Message Export Complete**\n\n"+
			"**Channel**: <#%s>\n"+
			"**Messages Exported**: %d\n\n"+
			"%s\n\n"+
			"_Export ID: %s_",
			req.ChannelID,
			result.MessageCount,
			result.Summary,
			result.ExportID.String()[:8],
		)
	} else {
		// Failed
		content = fmt.Sprintf("⚠️ **Message Export Completed with Issues**\n\n"+
			"**Channel**: <#%s>\n"+
			"**Messages Exported**: %d\n\n"+
			"%s\n\n"+
			"_Export ID: %s_",
			req.ChannelID,
			result.MessageCount,
			result.Summary,
			result.ExportID.String()[:8],
		)
	}

	// Send follow-up response
	_, err = s.FollowupMessageCreate(i.Interaction, true, &discordgo.WebhookParams{
		Content: content,
		Flags:   discordgo.MessageFlagsEphemeral,
	})
	if err != nil {
		log.Error().Err(err).Msg("Failed to send follow-up message")
	}
}

// parseOptions converts command options to a map for easier access
func parseOptions(options []*discordgo.ApplicationCommandInteractionDataOption) map[string]interface{} {
	optionMap := make(map[string]interface{})
	for _, opt := range options {
		optionMap[opt.Name] = opt.Value
	}
	return optionMap
}

// respondRateLimited sends a rate limit message
func respondRateLimited(s *discordgo.Session, i *discordgo.InteractionCreate, minutesLeft int) {
	message := fmt.Sprintf("⏱️ **Rate Limit Reached**\n\nYou've made 3 export requests in the past hour (limit: 3/hour).\nTry again in %d minutes.", minutesLeft)
	s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Content: message,
			Flags:   discordgo.MessageFlagsEphemeral,
		},
	})
}

func ptrFloat64(f float64) *float64 {
	return &f
}

func getFloat64(m map[string]interface{}, key string) float64 {
	if val, ok := m[key]; ok {
		switch v := val.(type) {
		case float64:
			return v
		case float32:
			return float64(v)
		case int:
			return float64(v)
		case int64:
			return float64(v)
		}
	}
	return 0.0
}

func getInt64(m map[string]interface{}, key string) int64 {
	if val, ok := m[key]; ok {
		switch v := val.(type) {
		case int64:
			return v
		case int:
			return int64(v)
		case float64:
			return int64(v)
		}
	}
	return 0
}

func parseDateTime(s string) (time.Time, error) {
	// Try parsing with time first (YYYY-MM-DD HH:MM)
	t, err := time.Parse("2006-01-02 15:04", s)
	if err == nil {
		return t, nil
	}

	// Try parsing date only (YYYY-MM-DD)
	t, err = time.Parse("2006-01-02", s)
	if err == nil {
		// If only date provided, set to start of day (00:00)
		return t, nil
	}

	return time.Time{}, fmt.Errorf("invalid datetime format: %s", s)
}
