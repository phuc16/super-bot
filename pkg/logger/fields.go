package logger

import (
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

// Standard field names for consistent logging across the application
const (
	FieldOperationType = "operation_type"
	FieldOperationID   = "operation_id"
	FieldDuration      = "duration_ms"
	FieldDurationSec   = "duration_sec"
	FieldStatus        = "status"
	FieldError         = "error"

	// Discord fields
	FieldGuildID   = "guild_id"
	FieldChannelID = "channel_id"
	FieldUserID    = "user_id"
	FieldUsername  = "username"
	FieldRoleID    = "role_id"
	FieldMessageID = "message_id"

	// Export fields
	FieldExportID     = "export_id"
	FieldExportType   = "export_type"
	FieldMessageCount = "message_count"
	FieldTokensUsed   = "tokens_used"

	// Activity fields
	FieldActivityType = "activity_type"
	FieldEventCount   = "event_count"
	FieldBatchSize    = "batch_size"

	// Report fields
	FieldReportID      = "report_id"
	FieldWeekNum       = "week_num"
	FieldYear          = "year"
	FieldActiveUsers   = "active_users"
	FieldTotalMessages = "total_messages"

	// GPT fields
	FieldModel            = "gpt_model"
	FieldPromptTokens     = "prompt_tokens"
	FieldCompletionTokens = "completion_tokens"
	FieldCostUSD          = "cost_usd"

	// System fields
	FieldComponent = "component"
	FieldVersion   = "version"
)

// Operation status constants
const (
	StatusSuccess = "success"
	StatusFailure = "failure"
	StatusPending = "pending"
	StatusSkipped = "skipped"
)

// OperationLogger helps with consistent structured logging for operations
type OperationLogger struct {
	event     *zerolog.Event
	startTime time.Time
	opType    string
	opID      string
}

// NewOperation starts logging an operation
func NewOperation(opType string) *OperationLogger {
	opID := uuid.New().String()

	return &OperationLogger{
		startTime: time.Now(),
		opType:    opType,
		opID:      opID,
	}
}

// WithField adds a field to the operation log
func (o *OperationLogger) WithField(key string, value interface{}) *OperationLogger {
	return o
}

// Success logs successful completion of the operation
func (o *OperationLogger) Success(message string) {
	duration := time.Since(o.startTime)

	log.Info().
		Str(FieldOperationType, o.opType).
		Str(FieldOperationID, o.opID).
		Str(FieldStatus, StatusSuccess).
		Dur(FieldDuration, duration).
		Float64(FieldDurationSec, duration.Seconds()).
		Msg(message)
}

// Failure logs failed completion of the operation
func (o *OperationLogger) Failure(err error, message string) {
	duration := time.Since(o.startTime)

	log.Error().
		Err(err).
		Str(FieldOperationType, o.opType).
		Str(FieldOperationID, o.opID).
		Str(FieldStatus, StatusFailure).
		Dur(FieldDuration, duration).
		Float64(FieldDurationSec, duration.Seconds()).
		Msg(message)
}

// LogEvent creates a structured log event with standard fields
func LogEvent(level zerolog.Level) *zerolog.Event {
	var event *zerolog.Event

	switch level {
	case zerolog.DebugLevel:
		event = log.Debug()
	case zerolog.InfoLevel:
		event = log.Info()
	case zerolog.WarnLevel:
		event = log.Warn()
	case zerolog.ErrorLevel:
		event = log.Error()
	default:
		event = log.Info()
	}

	return event
}

// LogExportOperation logs export-related operations
func LogExportOperation(level zerolog.Level, exportID uuid.UUID, exportType, status string) *zerolog.Event {
	return LogEvent(level).
		Str(FieldExportID, exportID.String()).
		Str(FieldExportType, exportType).
		Str(FieldStatus, status)
}

// LogActivityOperation logs activity-related operations
func LogActivityOperation(level zerolog.Level, guildID, userID string) *zerolog.Event {
	return LogEvent(level).
		Str(FieldGuildID, guildID).
		Str(FieldUserID, userID)
}

// LogReportOperation logs report-related operations
func LogReportOperation(level zerolog.Level, reportID uuid.UUID, weekNum, year int) *zerolog.Event {
	return LogEvent(level).
		Str(FieldReportID, reportID.String()).
		Int(FieldWeekNum, weekNum).
		Int(FieldYear, year)
}

// LogGPTOperation logs GPT-related operations
func LogGPTOperation(level zerolog.Level, model string) *zerolog.Event {
	return LogEvent(level).
		Str(FieldModel, model)
}

// LogDiscordOperation logs Discord API operations
func LogDiscordOperation(level zerolog.Level, guildID, channelID string) *zerolog.Event {
	return LogEvent(level).
		Str(FieldGuildID, guildID).
		Str(FieldChannelID, channelID)
}

// WithDuration adds duration fields to a log event
func WithDuration(event *zerolog.Event, duration time.Duration) *zerolog.Event {
	return event.
		Dur(FieldDuration, duration).
		Float64(FieldDurationSec, duration.Seconds())
}

// WithUserContext adds user context fields to a log event
func WithUserContext(event *zerolog.Event, userID, username string) *zerolog.Event {
	return event.
		Str(FieldUserID, userID).
		Str(FieldUsername, username)
}

// WithGuildContext adds guild context fields to a log event
func WithGuildContext(event *zerolog.Event, guildID string) *zerolog.Event {
	return event.Str(FieldGuildID, guildID)
}

// WithChannelContext adds channel context fields to a log event
func WithChannelContext(event *zerolog.Event, channelID string) *zerolog.Event {
	return event.Str(FieldChannelID, channelID)
}
