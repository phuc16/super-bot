package reporting

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
	"gorm.io/gorm"

	"github.com/phuc16/VCG-super-bot/internal/models"
)

// UserRanking represents a user's ranking in a report
type UserRanking struct {
	Rank         int    `json:"rank"`
	UserID       string `json:"user_id"`
	Username     string `json:"username"`
	MessageCount int    `json:"message_count"`
	VoiceMinutes int    `json:"voice_minutes"`
	AvatarURL    string `json:"avatar_url,omitempty"`
}

// ReportData represents the complete report data
type ReportData struct {
	Year          int           `json:"year"`
	WeekNum       int           `json:"week_num"`
	WeekStart     time.Time     `json:"week_start"`
	WeekEnd       time.Time     `json:"week_end"`
	TotalUsers    int           `json:"total_users"`
	ActiveUsers   int           `json:"active_users"`
	TotalMessages int           `json:"total_messages"`
	Rankings      []UserRanking `json:"rankings"`
}

// Generator handles weekly report generation
type Generator struct {
	db *gorm.DB
}

// NewGenerator creates a new report generator
func NewGenerator(db *gorm.DB) *Generator {
	return &Generator{db: db}
}

// GenerateWeeklyReport generates a weekly activity report for the specified week
func (g *Generator) GenerateWeeklyReport(ctx context.Context, guildID string, year int, weekNum int, generatedByUser string) (*models.WeeklyReport, error) {
	start := time.Now()

	// Get week bounds
	weekStart, weekEnd := models.GetWeekBounds(year, weekNum)

	log.Info().
		Str("guild_id", guildID).
		Int("year", year).
		Int("week", weekNum).
		Time("week_start", weekStart).
		Time("week_end", weekEnd).
		Msg("Generating weekly report")

	// Query weekly activity data
	var activities []models.WeeklyActivity
	err := g.db.WithContext(ctx).
		Where("guild_id = ? AND year = ? AND week_num = ?", guildID, year, weekNum).
		Order("rank ASC, username ASC").
		Find(&activities).Error

	if err != nil {
		return nil, fmt.Errorf("failed to query weekly activities: %w", err)
	}

	// Build report data
	reportData := g.buildReportData(year, weekNum, weekStart, weekEnd, activities)

	// Create report record
	report := &models.WeeklyReport{
		ID:              uuid.New(),
		GuildID:         guildID,
		Year:            year,
		WeekNum:         weekNum,
		WeekStart:       weekStart,
		WeekEnd:         weekEnd,
		TotalUsers:      reportData.TotalUsers,
		ActiveUsers:     reportData.ActiveUsers,
		TotalMessages:   reportData.TotalMessages,
		GeneratedAt:     time.Now(),
		GeneratedByUser: generatedByUser,
		ReportContent:   g.reportDataToJSON(reportData),
		Top5Users:       g.extractTop5UserIDs(reportData.Rankings),
	}

	// Save to database
	if err := g.db.WithContext(ctx).Create(report).Error; err != nil {
		return nil, fmt.Errorf("failed to save report: %w", err)
	}

	duration := time.Since(start)
	log.Info().
		Str("report_id", report.ID.String()).
		Str("guild_id", guildID).
		Int("active_users", reportData.ActiveUsers).
		Int("total_messages", reportData.TotalMessages).
		Dur("duration", duration).
		Msg("Weekly report generated successfully")

	return report, nil
}

// GenerateCurrentWeekReport generates a report for the current week
func (g *Generator) GenerateCurrentWeekReport(ctx context.Context, guildID string, generatedByUser string) (*models.WeeklyReport, error) {
	year, week := time.Now().ISOWeek()

	// For current week, use data from user_activity_records
	return g.GenerateCurrentWeekReportFromActivity(ctx, guildID, year, week, generatedByUser)
}

// GenerateCurrentWeekReportFromActivity generates a report from current user activity (not archived)
func (g *Generator) GenerateCurrentWeekReportFromActivity(ctx context.Context, guildID string, year int, weekNum int, generatedByUser string) (*models.WeeklyReport, error) {
	weekStart, weekEnd := models.GetWeekBounds(year, weekNum)

	// Query current week activities from user_activity_records
	var activities []models.UserActivity
	err := g.db.WithContext(ctx).
		Where("guild_id = ? AND current_week_messages > 0", guildID).
		Order("current_week_messages DESC, username ASC").
		Find(&activities).Error

	if err != nil {
		return nil, fmt.Errorf("failed to query user activities: %w", err)
	}

	// Convert to rankings
	rankings := make([]UserRanking, 0, len(activities))
	currentRank := 1
	lastMessageCount := -1
	usersAtRank := 0

	totalMessages := 0
	for _, activity := range activities {
		// Calculate rank with ties
		if activity.CurrentWeekMessages != lastMessageCount {
			currentRank += usersAtRank
			usersAtRank = 0
			lastMessageCount = activity.CurrentWeekMessages
		}
		usersAtRank++

		totalMessages += activity.CurrentWeekMessages

		rankings = append(rankings, UserRanking{
			Rank:         currentRank,
			UserID:       activity.UserID,
			Username:     activity.Username,
			MessageCount: activity.CurrentWeekMessages,
			VoiceMinutes: activity.CurrentWeekVoiceMinutes,
			AvatarURL:    activity.AvatarURL,
		})
	}

	reportData := ReportData{
		Year:          year,
		WeekNum:       weekNum,
		WeekStart:     weekStart,
		WeekEnd:       weekEnd,
		TotalUsers:    len(activities),
		ActiveUsers:   len(activities),
		TotalMessages: totalMessages,
		Rankings:      rankings,
	}

	// Create report record
	report := &models.WeeklyReport{
		ID:              uuid.New(),
		GuildID:         guildID,
		Year:            year,
		WeekNum:         weekNum,
		WeekStart:       weekStart,
		WeekEnd:         weekEnd,
		TotalUsers:      reportData.TotalUsers,
		ActiveUsers:     reportData.ActiveUsers,
		TotalMessages:   reportData.TotalMessages,
		GeneratedAt:     time.Now(),
		GeneratedByUser: generatedByUser,
		ReportContent:   g.reportDataToJSON(reportData),
		Top5Users:       g.extractTop5UserIDs(reportData.Rankings),
	}

	// Save to database
	if err := g.db.WithContext(ctx).Create(report).Error; err != nil {
		return nil, fmt.Errorf("failed to save report: %w", err)
	}

	log.Info().
		Str("report_id", report.ID.String()).
		Str("guild_id", guildID).
		Int("active_users", reportData.ActiveUsers).
		Int("total_messages", reportData.TotalMessages).
		Msg("Current week report generated")

	return report, nil
}

// buildReportData constructs report data from weekly activities
func (g *Generator) buildReportData(year int, weekNum int, weekStart time.Time, weekEnd time.Time, activities []models.WeeklyActivity) ReportData {
	rankings := make([]UserRanking, 0, len(activities))
	totalMessages := 0

	for _, activity := range activities {
		totalMessages += activity.MessageCount

		rankings = append(rankings, UserRanking{
			Rank:         activity.Rank,
			UserID:       activity.UserID,
			Username:     activity.Username,
			MessageCount: activity.MessageCount,
			VoiceMinutes: activity.VoiceMinutes,
			AvatarURL:    activity.AvatarURL,
		})
	}

	return ReportData{
		Year:          year,
		WeekNum:       weekNum,
		WeekStart:     weekStart,
		WeekEnd:       weekEnd,
		TotalUsers:    len(activities),
		ActiveUsers:   len(activities),
		TotalMessages: totalMessages,
		Rankings:      rankings,
	}
}

// reportDataToJSON converts report data to JSON map
func (g *Generator) reportDataToJSON(data ReportData) models.JSONMap {
	return models.JSONMap{
		"year":           data.Year,
		"week_num":       data.WeekNum,
		"week_start":     data.WeekStart,
		"week_end":       data.WeekEnd,
		"total_users":    data.TotalUsers,
		"active_users":   data.ActiveUsers,
		"total_messages": data.TotalMessages,
		"rankings":       data.Rankings,
	}
}

// extractTop5UserIDs extracts the top 5 user IDs from rankings
func (g *Generator) extractTop5UserIDs(rankings []UserRanking) models.JSONArray {
	top5 := make([]interface{}, 0, 5)

	for i, ranking := range rankings {
		if i >= 5 && ranking.Rank > 5 {
			break // Stop if we've passed rank 5 (accounting for ties)
		}
		top5 = append(top5, ranking.UserID)
	}

	return top5
}
