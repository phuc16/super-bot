package reporting

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/rs/zerolog/log"
	"gorm.io/gorm"

	"github.com/phuc16/VCG-super-bot/internal/models"
)

// Poster handles posting reports to Discord channels
type Poster struct {
	db      *gorm.DB
	session *discordgo.Session
}

// NewPoster creates a new report poster
func NewPoster(db *gorm.DB, session *discordgo.Session) *Poster {
	return &Poster{
		db:      db,
		session: session,
	}
}

// PostReport posts a weekly report to the specified Discord channel
func (p *Poster) PostReport(ctx context.Context, report *models.WeeklyReport, channelID string) error {
	if channelID == "" {
		return fmt.Errorf("channel ID is required")
	}

	// Parse report data
	reportData, err := p.parseReportData(report)
	if err != nil {
		return fmt.Errorf("failed to parse report data: %w", err)
	}

	// Format report as Discord embed
	embed := p.formatReportEmbed(reportData)

	// Send to Discord
	message, err := p.session.ChannelMessageSendEmbed(channelID, embed)
	if err != nil {
		return fmt.Errorf("failed to send report to Discord: %w", err)
	}

	// Update report with posting information
	report.MarkAsPosted(channelID, message.ID)

	err = p.db.WithContext(ctx).Model(report).Updates(map[string]interface{}{
		"channel_id":        report.ChannelID,
		"posted_message_id": report.PostedMessageID,
		"posted_at":         report.PostedAt,
	}).Error

	if err != nil {
		log.Error().Err(err).Str("report_id", report.ID.String()).Msg("Failed to update report posting info")
		// Don't return error as message was posted successfully
	}

	log.Info().
		Str("report_id", report.ID.String()).
		Str("channel_id", channelID).
		Str("message_id", message.ID).
		Msg("Report posted successfully")

	return nil
}

// parseReportData extracts ReportData from the report model
func (p *Poster) parseReportData(report *models.WeeklyReport) (*ReportData, error) {
	rankings := make([]UserRanking, 0)

	// Extract rankings from JSON
	if rankingsData, ok := report.ReportContent["rankings"]; ok {
		if rankingsList, ok := rankingsData.([]interface{}); ok {
			for _, item := range rankingsList {
				if rankingMap, ok := item.(map[string]interface{}); ok {
					ranking := UserRanking{
						Rank:         int(getFloat64(rankingMap, "rank")),
						UserID:       getString(rankingMap, "user_id"),
						Username:     getString(rankingMap, "username"),
						MessageCount: int(getFloat64(rankingMap, "message_count")),
						VoiceMinutes: int(getFloat64(rankingMap, "voice_minutes")),
						AvatarURL:    getString(rankingMap, "avatar_url"),
					}
					rankings = append(rankings, ranking)
				}
			}
		}
	}

	return &ReportData{
		Year:          report.Year,
		WeekNum:       report.WeekNum,
		WeekStart:     report.WeekStart,
		WeekEnd:       report.WeekEnd,
		TotalUsers:    report.TotalUsers,
		ActiveUsers:   report.ActiveUsers,
		TotalMessages: report.TotalMessages,
		Rankings:      rankings,
	}, nil
}

// formatReportEmbed formats the report as a Discord embed
func (p *Poster) formatReportEmbed(data *ReportData) *discordgo.MessageEmbed {
	// Handle zero activity case
	if data.ActiveUsers == 0 {
		return &discordgo.MessageEmbed{
			Title:       "📊 Weekly Activity Report",
			Description: fmt.Sprintf("**Week %d, %d**\n\nNo activity recorded this week.", data.WeekNum, data.Year),
			Color:       0x808080, // Gray
			Timestamp:   time.Now().Format(time.RFC3339),
		}
	}

	// Build description
	var description strings.Builder
	description.WriteString(fmt.Sprintf("**Week %d, %d**\n", data.WeekNum, data.Year))
	description.WriteString(fmt.Sprintf("%s to %s\n\n",
		data.WeekStart.Format("Jan 2"),
		data.WeekEnd.Format("Jan 2, 2006")))
	description.WriteString(fmt.Sprintf("📊 **%d** active users\n", data.ActiveUsers))
	description.WriteString(fmt.Sprintf("💬 **%d** total messages\n", data.TotalMessages))

	// Build fields for top users
	fields := make([]*discordgo.MessageEmbedField, 0)

	// Top 10 users
	topCount := 10
	if len(data.Rankings) < topCount {
		topCount = len(data.Rankings)
	}

	if topCount > 0 {
		var topUsersText strings.Builder
		lastRank := -1

		for i := 0; i < topCount; i++ {
			ranking := data.Rankings[i]

			// Stop if we've gone beyond rank 10 (but include ties)
			if ranking.Rank > 10 && ranking.Rank != lastRank {
				break
			}

			emoji := getRankEmoji(ranking.Rank)
			topUsersText.WriteString(fmt.Sprintf("%s **#%d** - %s\n💬 %d messages\n\n",
				emoji, ranking.Rank, ranking.Username, ranking.MessageCount))

			lastRank = ranking.Rank
		}

		fields = append(fields, &discordgo.MessageEmbedField{
			Name:   "🏆 Top Active Users",
			Value:  topUsersText.String(),
			Inline: false,
		})
	}

	// Create embed
	embed := &discordgo.MessageEmbed{
		Title:       "📊 Weekly Activity Report",
		Description: description.String(),
		Color:       0x00FF00, // Green
		Fields:      fields,
		Timestamp:   time.Now().Format(time.RFC3339),
		Footer: &discordgo.MessageEmbedFooter{
			Text: "Keep chatting to climb the leaderboard! 🚀",
		},
	}

	return embed
}

// Helper functions for type assertions
func getString(m map[string]interface{}, key string) string {
	if val, ok := m[key]; ok {
		if str, ok := val.(string); ok {
			return str
		}
	}
	return ""
}

func getFloat64(m map[string]interface{}, key string) float64 {
	if val, ok := m[key]; ok {
		switch v := val.(type) {
		case float64:
			return v
		case int:
			return float64(v)
		case int64:
			return float64(v)
		}
	}
	return 0
}

func getRankEmoji(rank int) string {
	switch rank {
	case 1:
		return "🥇"
	case 2:
		return "🥈"
	case 3:
		return "🥉"
	case 4, 5:
		return "🏆"
	default:
		return "📊"
	}
}
