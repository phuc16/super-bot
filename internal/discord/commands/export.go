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
				Description: "Export type: date-range, recent, or since last export",
				Required:    true,
				Choices: []*discordgo.ApplicationCommandOptionChoice{
					{Name: "Date Range", Value: "date_range"},
					{Name: "Recent Messages", Value: "recent"},
					{Name: "Since Last Export", Value: "since_last"},
				},
			},
			{
				Type:        discordgo.ApplicationCommandOptionInteger,
				Name:        "hours",
				Description: "Last N hours (optional for Recent type, default: 1, max: 168)",
				Required:    false,
			},
			{
				Type:        discordgo.ApplicationCommandOptionString,
				Name:        "from",
				Description: "Start date/time (required for Date Range) - Format: YYYY-MM-DD HH:MM or YYYY-MM-DD",
				Required:    false,
			},
			{
				Type:        discordgo.ApplicationCommandOptionString,
				Name:        "end",
				Description: "End date/time (optional for Date Range, defaults to now) - Format: YYYY-MM-DD HH:MM or YYYY-MM-DD",
				Required:    false,
			},
			{
				Type:        discordgo.ApplicationCommandOptionString,
				Name:        "language",
				Description: "Summary language (optional: english or vietnamese, default vietnamese)",
				Required:    false,
				Choices: []*discordgo.ApplicationCommandOptionChoice{
					{Name: "English", Value: "en"},
					{Name: "Tiếng Việt", Value: "vi"},
				},
			},
			{
				Type:        discordgo.ApplicationCommandOptionBoolean,
				Name:        "public",
				Description: "Show result to everyone in the channel (default: false — only you can see)",
				Required:    false,
			},
		},
	}
}

// Handle processes the command interaction
func (cmd *ExportCommand) Handle(s *discordgo.Session, i *discordgo.InteractionCreate) {
	// Only admins (Manage Server / Administrator) can use this command
	if !hasAdminPermission(i.Member) {
		respondError(s, i, "Bạn cần quyền Administrator hoặc Manage Server để dùng lệnh này.")
		return
	}

	// Parse options
	options := parseOptions(i.ApplicationCommandData().Options)
	exportType := options["type"].(string)

	// Get language option (default to Vietnamese)
	language := "vi"
	if lang, ok := options["language"].(string); ok {
		language = lang
	}

	// Get public option (default to false — ephemeral)
	isPublic := false
	if pub, ok := options["public"].(bool); ok {
		isPublic = pub
	}
	var msgFlags discordgo.MessageFlags
	if !isPublic {
		msgFlags = discordgo.MessageFlagsEphemeral
	}

	// Get hours option (default to 1 for recent type)
	hours := 1
	if hoursVal, ok := options["hours"].(float64); ok {
		hours = int(hoursVal)
		if hours < 1 || hours > 168 {
			respondError(s, i, "Hours must be between 1 and 168 (7 days)")
			return
		}
	}

	userID := i.Member.User.ID
	channelID := i.ChannelID
	guildID := i.GuildID

	// Check rate limit
	allowed, minutesLeft, err := cmd.rateLimiter.Allow(userID)
	if !allowed {
		respondRateLimited(s, i, minutesLeft)
		return
	}

	// Defer response immediately — must happen within 3 seconds
	err = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseDeferredChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Content: "⏳ Exporting messages... This may take up to 60 seconds.",
			Flags:   msgFlags,
		},
	})
	if err != nil {
		log.Error().Err(err).Msg("Failed to defer response")
		return
	}

	// Resolve channel name from state cache (no API call); fall back to API if missing
	channelName := channelID
	if ch, err := s.State.Channel(channelID); err == nil {
		channelName = ch.Name
	} else if ch, err := s.Channel(channelID); err == nil {
		channelName = ch.Name
	}

	// Parse export parameters
	req := export.ExportRequest{
		ChannelID:   channelID,
		ChannelName: channelName,
		GuildID:     guildID,
		UserID:      userID,
		ExportType:  models.ExportType(exportType),
		Language:    language,
		Hours:       hours,
	}

	// Handle date-range type
	if exportType == string(models.ExportTypeDateRange) {
		// Parse from and end datetimes
		fromStr, hasFrom := options["from"].(string)
		endStr, hasEnd := options["end"].(string)

		if !hasFrom {
			followUpError(s, i, "For date-range export, 'from' datetime is required.\nFormat: YYYY-MM-DD HH:MM or YYYY-MM-DD")
			return
		}

		// If end is not provided, default to now in the configured timezone
		if !hasEnd {
			endStr = time.Now().In(cmd.timezone).Format("2006-01-02 15:04")
		}

		// Validate date-time range using validation utility with timezone
		fromTime, endTime, err := ValidateDateTimeRangeWithTimezone(fromStr, endStr, cmd.timezone)
		if err != nil {
			followUpError(s, i, err.Error())
			return
		}

		req.StartTime = &fromTime
		req.EndTime = &endTime
	} else if exportType == string(models.ExportTypeRecent) {
		now := time.Now().In(cmd.timezone)
		startTime := now.Add(-time.Duration(hours) * time.Hour)

		req.StartTime = &startTime
		req.EndTime = &now
	} else if exportType == "since_last" {
		lastEnd, err := cmd.service.GetLastExportEndTime(context.Background(), guildID, channelID)
		if err != nil {
			followUpError(s, i, "Không thể lấy thông tin export trước: "+err.Error())
			return
		}
		if lastEnd == nil {
			followUpError(s, i, "Chưa có export nào trước đó cho channel này. Hãy dùng **Date Range** hoặc **Recent Messages**.")
			return
		}
		now := time.Now().In(cmd.timezone)
		// Use date_range under the hood
		req.ExportType = models.ExportTypeDateRange
		req.StartTime = lastEnd
		req.EndTime = &now
	}

	// Execute export asynchronously
	go cmd.processExport(s, i, req, msgFlags)
}

// processExport handles the actual export processing
func (cmd *ExportCommand) processExport(s *discordgo.Session, i *discordgo.InteractionCreate, req export.ExportRequest, msgFlags discordgo.MessageFlags) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	// Perform export
	result, err := cmd.service.Export(ctx, req)
	if err != nil {
		log.Error().Err(err).Msg("Export failed")
		followUpError(s, i, fmt.Sprintf("Export failed: %s", err.Error()))
		return
	}

	// Format time range
	timeFormat := "02/01/2006 15:04"
	timeRange := ""
	if req.StartTime != nil && req.EndTime != nil {
		timeRange = fmt.Sprintf("**Time**: %s → %s\n",
			req.StartTime.In(cmd.timezone).Format(timeFormat),
			req.EndTime.In(cmd.timezone).Format(timeFormat),
		)
	}

	params := &discordgo.WebhookParams{
		Flags: msgFlags,
	}

	const embedDescLimit = 4096

	switch result.SummaryStatus {
	case models.SummaryStatusCompleted:
		// Build header embed with stats
		headerEmbed := &discordgo.MessageEmbed{
			Color: 0x5865F2,
			Description: fmt.Sprintf("**Channel**: <#%s>\n%s**Messages**: %d | **Time**: %.1fs | **Tokens**: %d",
				req.ChannelID,
				timeRange,
				result.MessageCount,
				float64(result.ProcessingDurationMs)/1000.0,
				result.TokensUsed,
			),
		}
		params.Embeds = append(params.Embeds, headerEmbed)

		// Split summary across embeds (4096 chars each), all in one message
		for _, chunk := range splitByLines(result.Summary, embedDescLimit) {
			params.Embeds = append(params.Embeds, &discordgo.MessageEmbed{
				Color:       0x5865F2,
				Description: chunk,
			})
		}

	case models.SummaryStatusSkipped:
		params.Embeds = []*discordgo.MessageEmbed{{
			Color: 0xFEE75C,
			Description: fmt.Sprintf("**Channel**: <#%s>\n%s**Messages**: %d\n\n%s",
				req.ChannelID, timeRange, result.MessageCount, result.Summary),
		}}

	default:
		params.Embeds = []*discordgo.MessageEmbed{{
			Color: 0xED4245,
			Description: fmt.Sprintf("**Channel**: <#%s>\n%s**Messages**: %d\n\n%s",
				req.ChannelID, timeRange, result.MessageCount, result.Summary),
		}}
	}

	if _, err = s.FollowupMessageCreate(i.Interaction, true, params); err != nil {
		log.Error().Err(err).Msg("Failed to send follow-up message")
	}
}

// splitByLines splits text into non-empty chunks no larger than maxLen, breaking on newlines.
func splitByLines(text string, maxLen int) []string {
	var chunks []string
	for len(text) > 0 {
		if len(text) <= maxLen {
			if trimmed := trimRight(text); trimmed != "" {
				chunks = append(chunks, trimmed)
			}
			break
		}
		cut := maxLen
		for j := maxLen; j > 0; j-- {
			if text[j-1] == '\n' {
				cut = j
				break
			}
		}
		chunk := trimRight(text[:cut])
		if chunk != "" {
			chunks = append(chunks, chunk)
		}
		text = text[cut:]
	}
	return chunks
}

func trimRight(s string) string {
	end := len(s)
	for end > 0 && (s[end-1] == '\n' || s[end-1] == '\r' || s[end-1] == ' ') {
		end--
	}
	return s[:end]
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
			Flags:   0,
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
