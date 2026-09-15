package commands

import (
	"fmt"
	"regexp"
	"time"
)

// DateTimeFormat is the expected format for datetime inputs
const DateTimeFormat = "2006-01-02 15:04"

// DateFormat is the expected format for date-only inputs
const DateFormat = "2006-01-02"

// ValidationError represents a validation error
type ValidationError struct {
	Field   string
	Message string
}

// Error implements the error interface
func (e ValidationError) Error() string {
	return fmt.Sprintf("validation error for %s: %s", e.Field, e.Message)
}

// ValidateDateTimeRange validates a date-time range input
func ValidateDateTimeRange(fromStr, endStr string) (time.Time, time.Time, error) {
	return ValidateDateTimeRangeWithTimezone(fromStr, endStr, time.UTC)
}

// ValidateDateTimeRangeWithTimezone validates a date-time range input with timezone
func ValidateDateTimeRangeWithTimezone(fromStr, endStr string, loc *time.Location) (time.Time, time.Time, error) {
	// Try datetime format first
	from, err := time.ParseInLocation(DateTimeFormat, fromStr, loc)
	if err != nil {
		// Try date-only format
		from, err = time.ParseInLocation(DateFormat, fromStr, loc)
		if err != nil {
			return time.Time{}, time.Time{}, ValidationError{
				Field:   "from",
				Message: fmt.Sprintf("Invalid date format. Use 'YYYY-MM-DD HH:MM' or 'YYYY-MM-DD'. Got: %s", fromStr),
			}
		}
	}

	// Try datetime format first
	end, err := time.ParseInLocation(DateTimeFormat, endStr, loc)
	if err != nil {
		// Try date-only format
		end, err = time.ParseInLocation(DateFormat, endStr, loc)
		if err != nil {
			return time.Time{}, time.Time{}, ValidationError{
				Field:   "end",
				Message: fmt.Sprintf("Invalid date format. Use 'YYYY-MM-DD HH:MM' or 'YYYY-MM-DD'. Got: %s", endStr),
			}
		}
	}

	// Validate range
	if end.Before(from) {
		return time.Time{}, time.Time{}, ValidationError{
			Field:   "date_range",
			Message: "End date must be after start date",
		}
	}

	// Validate not too far in the future
	now := time.Now()
	if from.After(now) {
		return time.Time{}, time.Time{}, ValidationError{
			Field:   "from",
			Message: "Start date cannot be in the future",
		}
	}

	if end.After(now) {
		return time.Time{}, time.Time{}, ValidationError{
			Field:   "end",
			Message: "End date cannot be in the future",
		}
	}

	// Validate range not too large (e.g., max 90 days)
	maxRange := 90 * 24 * time.Hour
	if end.Sub(from) > maxRange {
		return time.Time{}, time.Time{}, ValidationError{
			Field:   "date_range",
			Message: "Date range cannot exceed 90 days",
		}
	}

	return from, end, nil
}

// ValidateRoleID validates a Discord role ID
func ValidateRoleID(roleID string) error {
	if roleID == "" {
		return ValidationError{
			Field:   "role_id",
			Message: "Role ID cannot be empty",
		}
	}

	// Discord snowflake IDs are numeric strings, typically 17-19 digits
	matched, err := regexp.MatchString(`^\d{17,19}$`, roleID)
	if err != nil || !matched {
		return ValidationError{
			Field:   "role_id",
			Message: "Invalid role ID format",
		}
	}

	return nil
}

// ValidateUserID validates a Discord user ID
func ValidateUserID(userID string) error {
	if userID == "" {
		return ValidationError{
			Field:   "user_id",
			Message: "User ID cannot be empty",
		}
	}

	// Discord snowflake IDs are numeric strings, typically 17-19 digits
	matched, err := regexp.MatchString(`^\d{17,19}$`, userID)
	if err != nil || !matched {
		return ValidationError{
			Field:   "user_id",
			Message: "Invalid user ID format",
		}
	}

	return nil
}

// ValidateGuildID validates a Discord guild ID
func ValidateGuildID(guildID string) error {
	if guildID == "" {
		return ValidationError{
			Field:   "guild_id",
			Message: "Guild ID cannot be empty",
		}
	}

	// Discord snowflake IDs are numeric strings, typically 17-19 digits
	matched, err := regexp.MatchString(`^\d{17,19}$`, guildID)
	if err != nil || !matched {
		return ValidationError{
			Field:   "guild_id",
			Message: "Invalid guild ID format",
		}
	}

	return nil
}

// ValidateRoleName validates a role name
func ValidateRoleName(name string) error {
	if name == "" {
		return ValidationError{
			Field:   "role_name",
			Message: "Role name cannot be empty",
		}
	}

	if len(name) > 100 {
		return ValidationError{
			Field:   "role_name",
			Message: "Role name must be 100 characters or less",
		}
	}

	return nil
}

// ValidatePositiveInteger validates that a number is positive
func ValidatePositiveInteger(field string, value int) error {
	if value <= 0 {
		return ValidationError{
			Field:   field,
			Message: fmt.Sprintf("%s must be a positive integer", field),
		}
	}
	return nil
}

// ValidateRangeInteger validates that a number is within a range
func ValidateRangeInteger(field string, value, min, max int) error {
	if value < min || value > max {
		return ValidationError{
			Field:   field,
			Message: fmt.Sprintf("%s must be between %d and %d", field, min, max),
		}
	}
	return nil
}

// ValidateString validates string length
func ValidateString(field string, value string, minLen, maxLen int) error {
	if len(value) < minLen {
		return ValidationError{
			Field:   field,
			Message: fmt.Sprintf("%s must be at least %d characters", field, minLen),
		}
	}

	if maxLen > 0 && len(value) > maxLen {
		return ValidationError{
			Field:   field,
			Message: fmt.Sprintf("%s must be at most %d characters", field, maxLen),
		}
	}

	return nil
}
