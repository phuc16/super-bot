package logger

import (
	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

// ErrorWithID logs an error with a unique tracking ID
type ErrorWithID struct {
	ID      string
	Err     error
	Context map[string]interface{}
}

// NewError creates a new error with a unique ID for tracking
func NewError(err error) *ErrorWithID {
	return &ErrorWithID{
		ID:      uuid.New().String(),
		Err:     err,
		Context: make(map[string]interface{}),
	}
}

// WithContext adds context fields to the error
func (e *ErrorWithID) WithContext(key string, value interface{}) *ErrorWithID {
	e.Context[key] = value
	return e
}

// WithUserContext adds user-related context
func (e *ErrorWithID) WithUserContext(userID, username string) *ErrorWithID {
	e.Context[FieldUserID] = userID
	e.Context[FieldUsername] = username
	return e
}

// WithGuildContext adds guild-related context
func (e *ErrorWithID) WithGuildContext(guildID string) *ErrorWithID {
	e.Context[FieldGuildID] = guildID
	return e
}

// WithCommandContext adds command-related context
func (e *ErrorWithID) WithCommandContext(commandName string) *ErrorWithID {
	e.Context["command"] = commandName
	return e
}

// Log logs the error with all context
func (e *ErrorWithID) Log(level zerolog.Level, message string) {
	event := log.WithLevel(level).
		Err(e.Err).
		Str("error_id", e.ID)

	// Add all context fields
	for key, value := range e.Context {
		switch v := value.(type) {
		case string:
			event = event.Str(key, v)
		case int:
			event = event.Int(key, v)
		case int64:
			event = event.Int64(key, v)
		case bool:
			event = event.Bool(key, v)
		case float64:
			event = event.Float64(key, v)
		default:
			event = event.Interface(key, v)
		}
	}

	event.Msg(message)
}

// LogError is a convenience method to log at error level
func (e *ErrorWithID) LogError(message string) {
	e.Log(zerolog.ErrorLevel, message)
}

// LogWarn is a convenience method to log at warn level
func (e *ErrorWithID) LogWarn(message string) {
	e.Log(zerolog.WarnLevel, message)
}

// Error implements the error interface
func (e *ErrorWithID) Error() string {
	return e.Err.Error()
}

// GetID returns the error tracking ID
func (e *ErrorWithID) GetID() string {
	return e.ID
}

// LogErrorWithID is a helper function to quickly log an error with ID
func LogErrorWithID(err error, message string, context ...map[string]interface{}) string {
	errorWithID := NewError(err)

	// Add any provided context
	if len(context) > 0 {
		for key, value := range context[0] {
			errorWithID.WithContext(key, value)
		}
	}

	errorWithID.LogError(message)
	return errorWithID.ID
}

// LogWarnWithID is a helper function to quickly log a warning with ID
func LogWarnWithID(err error, message string, context ...map[string]interface{}) string {
	errorWithID := NewError(err)

	// Add any provided context
	if len(context) > 0 {
		for key, value := range context[0] {
			errorWithID.WithContext(key, value)
		}
	}

	errorWithID.LogWarn(message)
	return errorWithID.ID
}

// ErrorResponse represents an error response with tracking ID for users
type ErrorResponse struct {
	Message string `json:"message"`
	ErrorID string `json:"error_id"`
}

// NewErrorResponse creates a user-facing error response
func NewErrorResponse(userMessage string, errorID string) *ErrorResponse {
	return &ErrorResponse{
		Message: userMessage,
		ErrorID: errorID,
	}
}

// String returns a formatted error message with ID
func (e *ErrorResponse) String() string {
	if e.ErrorID != "" {
		return e.Message + " (Error ID: " + e.ErrorID + ")"
	}
	return e.Message
}
