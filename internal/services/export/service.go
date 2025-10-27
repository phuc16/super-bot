package export

import (
	"context"
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
	ChannelID    string
	ChannelName  string
	GuildID      string
	UserID       string
	ExportType   models.ExportType
	StartTime    *time.Time
	EndTime      *time.Time
	Language     string // "vi" for Vietnamese, "en" for English
	MessageCount int    // Number of recent messages to fetch (for recent type)
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
		messages, err = s.fetcher.FetchDateRange(req.ChannelID, *req.StartTime, *req.EndTime)
	case models.ExportTypeRecent:
		messages, err = s.fetcher.FetchRecentMessages(req.ChannelID, req.MessageCount)
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
	requestID, responseChan := s.gptQueue.SubmitWithLanguage(formatted, req.ChannelName, language)
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
