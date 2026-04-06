package export

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
	"gorm.io/gorm"

	"github.com/phuc16/VCG-super-bot/internal/gpt"
	"github.com/phuc16/VCG-super-bot/internal/models"
)

// Service handles message export operations
type Service struct {
	db        *gorm.DB
	fetcher   *Fetcher
	formatter *Formatter
	gptQueue  *gpt.Queue
}

// NewService creates a new export service
func NewService(db *gorm.DB, session *discordgo.Session, gptQueue *gpt.Queue) *Service {
	return &Service{
		db:        db,
		fetcher:   NewFetcher(session),
		formatter: NewFormatter(),
		gptQueue:  gptQueue,
	}
}

// ExportRequest represents an export request
type ExportRequest struct {
	ChannelID   string
	ChannelName string
	GuildID     string
	UserID      string
	ExportType  models.ExportType
	StartTime   *time.Time
	EndTime     *time.Time
	Language    string // "vi" for Vietnamese, "en" for English
	Hours       int    // Number of hours to go back (for recent type)
}

// ExportResult represents the result of an export
type ExportResult struct {
	ExportID             uuid.UUID
	MessageCount         int
	Summary              string
	SummaryStatus        models.SummaryStatus
	TokensUsed           int
	ProcessingDurationMs int64
}

// Export performs a complete export with summarization
func (s *Service) Export(ctx context.Context, req ExportRequest) (*ExportResult, error) {
	startTime := time.Now()

	log.Info().
		Str("channel_id", req.ChannelID).
		Str("user_id", req.UserID).
		Str("type", string(req.ExportType)).
		Msg("Starting message export")

	// Create export record
	export := &models.MessageExport{
		GuildID:         req.GuildID,
		ChannelID:       req.ChannelID,
		RequesterUserID: req.UserID,
		ExportType:      req.ExportType,
		StartTime:       req.StartTime,
		EndTime:         req.EndTime,
		MessageCount:    0,
		SummaryStatus:   models.SummaryStatusPending,
	}

	if err := s.db.Create(export).Error; err != nil {
		return nil, fmt.Errorf("failed to create export record: %w", err)
	}

	// Fetch messages
	var messages []models.DiscordMessage
	var err error

	switch req.ExportType {
	case models.ExportTypeDateRange:
		if req.StartTime == nil || req.EndTime == nil {
			return nil, fmt.Errorf("start_time and end_time required for date_range export")
		}
		messages, err = s.fetcher.FetchDateRange(req.ChannelID, req.GuildID, *req.StartTime, *req.EndTime)
	case models.ExportTypeRecent:
		if req.StartTime == nil || req.EndTime == nil {
			return nil, fmt.Errorf("start_time and end_time required for recent export")
		}
		messages, err = s.fetcher.FetchDateRange(req.ChannelID, req.GuildID, *req.StartTime, *req.EndTime)
	default:
		return nil, fmt.Errorf("invalid export type: %s", req.ExportType)
	}

	if err != nil {
		s.updateExportError(export.ID, err)
		return nil, fmt.Errorf("failed to fetch messages: %w", err)
	}

	// Update message count
	export.MessageCount = len(messages)
	if err := s.db.Model(export).Update("message_count", len(messages)).Error; err != nil {
		log.Warn().Err(err).Msg("Failed to update message count")
	}

	// Check if we have messages to summarize
	if len(messages) == 0 {
		export.SummaryStatus = models.SummaryStatusSkipped
		export.CompletedAt = &startTime
		s.db.Save(export)

		return &ExportResult{
			ExportID:             export.ID,
			MessageCount:         0,
			Summary:              "No messages found in the specified range.",
			SummaryStatus:        models.SummaryStatusSkipped,
			ProcessingDurationMs: time.Since(startTime).Milliseconds(),
		}, nil
	}

	// Format messages for GPT
	formatted := s.formatter.FormatForGPT(messages, req.ChannelName)

	// Estimate tokens
	estimatedTokens := s.formatter.EstimateTokens(formatted)
	log.Debug().Int("estimated_tokens", estimatedTokens).Msg("Token estimation")

	// Validate token limit
	if err := gpt.ValidateTokenLimit(estimatedTokens, gpt.GPT4TurboContextLimit); err != nil {
		export.SummaryStatus = models.SummaryStatusSkipped
		errMsg := "Message export too large for summarization"
		export.SummaryError = &errMsg
		export.CompletedAt = &startTime
		s.db.Save(export)

		return &ExportResult{
			ExportID:             export.ID,
			MessageCount:         len(messages),
			Summary:              "Export completed but messages exceed GPT token limit. Summary not available.",
			SummaryStatus:        models.SummaryStatusSkipped,
			ProcessingDurationMs: time.Since(startTime).Milliseconds(),
		}, nil
	}

	// Submit to GPT queue with language
	language := req.Language
	if language == "" {
		language = "vi" // Default to Vietnamese
	}

	// Fetch recent summaries as historical context
	historicalContext := s.buildHistoricalContext(ctx, req.GuildID, req.ChannelID, export.ID.String())

	requestID, responseChan := s.gptQueue.SubmitWithLanguage(formatted, req.ChannelName, language, historicalContext)
	log.Debug().
		Str("gpt_request_id", requestID.String()).
		Str("language", language).
		Msg("Submitted to GPT queue")

	// Wait for response (with timeout)
	select {
	case response := <-responseChan:
		now := time.Now()
		duration := time.Since(startTime).Milliseconds()

		if response.Error != nil {
			// Summarization failed
			export.SummaryStatus = models.SummaryStatusFailed
			errMsg := response.Error.Error()
			export.SummaryError = &errMsg
			export.CompletedAt = &now
			durationInt := int(duration)
			export.ProcessingDurationMs = &durationInt
			s.db.Save(export)

			return &ExportResult{
				ExportID:             export.ID,
				MessageCount:         len(messages),
				Summary:              "Export completed but AI summarization failed. Raw messages available on request.",
				SummaryStatus:        models.SummaryStatusFailed,
				ProcessingDurationMs: duration,
			}, nil
		}

		// Success
		export.SummaryText = &response.Summary
		export.SummaryStatus = models.SummaryStatusCompleted
		export.GPTTokensUsed = &response.Tokens
		export.CompletedAt = &now
		durationInt := int(duration)
		export.ProcessingDurationMs = &durationInt
		s.db.Save(export)

		return &ExportResult{
			ExportID:             export.ID,
			MessageCount:         len(messages),
			Summary:              response.Summary,
			SummaryStatus:        models.SummaryStatusCompleted,
			TokensUsed:           response.Tokens,
			ProcessingDurationMs: duration,
		}, nil

	case <-ctx.Done():
		// Timeout
		export.SummaryStatus = models.SummaryStatusFailed
		errMsg := "Request timeout"
		export.SummaryError = &errMsg
		s.db.Save(export)

		return nil, fmt.Errorf("export request timeout")
	}
}

// updateExportError updates an export record with an error
func (s *Service) updateExportError(exportID uuid.UUID, err error) {
	errMsg := err.Error()
	now := time.Now()
	s.db.Model(&models.MessageExport{}).
		Where("id = ?", exportID).
		Updates(map[string]interface{}{
			"summary_status": models.SummaryStatusFailed,
			"summary_error":  errMsg,
			"completed_at":   now,
		})
}

// GetExport retrieves an export by ID
func (s *Service) GetExport(exportID uuid.UUID) (*models.MessageExport, error) {
	var export models.MessageExport
	if err := s.db.First(&export, "id = ?", exportID).Error; err != nil {
		return nil, fmt.Errorf("failed to find export: %w", err)
	}
	return &export, nil
}

// buildHistoricalContext fetches the last 3 completed summaries for the channel
// and concatenates them as context for the next GPT call.
// currentExportID is excluded so we don't include the record just created.
func (s *Service) buildHistoricalContext(ctx context.Context, guildID, channelID, currentExportID string) string {
	var exports []models.MessageExport
	err := s.db.WithContext(ctx).
		Where("guild_id = ? AND channel_id = ? AND summary_status = ? AND id != ? AND summary_text IS NOT NULL",
			guildID, channelID, models.SummaryStatusCompleted, currentExportID).
		Order("end_time DESC").
		Limit(3).
		Find(&exports).Error
	if err != nil || len(exports) == 0 {
		return ""
	}

	const maxCharsPerSummary = 3000
	var parts []string
	for idx, e := range exports {
		if e.SummaryText == nil || *e.SummaryText == "" {
			continue
		}
		text := *e.SummaryText
		if len(text) > maxCharsPerSummary {
			text = text[:maxCharsPerSummary] + "..."
		}
		timeLabel := ""
		if e.EndTime != nil {
			timeLabel = e.EndTime.Format("02/01/2006 15:04")
		}
		parts = append(parts, fmt.Sprintf("### Tóm tắt %d (kết thúc %s)\n%s", idx+1, timeLabel, text))
	}
	if len(parts) == 0 {
		return ""
	}
	result := ""
	for i := len(parts) - 1; i >= 0; i-- {
		result += parts[i] + "\n\n"
	}
	return result
}

// GetLastExportEndTime returns the end_time of the most recent completed export
// for the given channel. Returns nil if no prior export exists.
func (s *Service) GetLastExportEndTime(ctx context.Context, guildID, channelID string) (*time.Time, error) {
	var export models.MessageExport
	err := s.db.WithContext(ctx).
		Where("guild_id = ? AND channel_id = ? AND summary_status != ? AND end_time IS NOT NULL",
			guildID, channelID, models.SummaryStatusPending).
		Order("end_time DESC").
		First(&export).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to query last export: %w", err)
	}
	return export.EndTime, nil
}
