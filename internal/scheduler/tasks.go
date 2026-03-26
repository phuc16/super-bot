package scheduler

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"
	openai "github.com/sashabaranov/go-openai"
	"github.com/robfig/cron/v3"
	"github.com/rs/zerolog/log"
	"gorm.io/gorm"

	"github.com/phuc16/VCG-super-bot/internal/gpt"
	"github.com/phuc16/VCG-super-bot/internal/models"
	configService "github.com/phuc16/VCG-super-bot/internal/services/config"
	"github.com/phuc16/VCG-super-bot/internal/services/aitopic"
	"github.com/phuc16/VCG-super-bot/internal/services/reporting"
	"github.com/phuc16/VCG-super-bot/internal/services/roles"
)

// Scheduler manages scheduled tasks
type Scheduler struct {
	cron          *cron.Cron
	db            *gorm.DB
	session       *discordgo.Session
	analyzer      *roles.Analyzer
	assigner      *roles.Assigner
	reportGen     *reporting.Generator
	reportPoster  *reporting.Poster
	configService *configService.Service
	aiTopicSvc    *aitopic.Service
	gptClient     *gpt.Client
	guildID       string
}

// NewScheduler creates a new task scheduler
func NewScheduler(db *gorm.DB, session *discordgo.Session, analyzer *roles.Analyzer, assigner *roles.Assigner, reportGen *reporting.Generator, reportPoster *reporting.Poster, configService *configService.Service, aiTopicSvc *aitopic.Service, gptClient *gpt.Client, guildID string) *Scheduler {
	// Create cron with seconds precision
	c := cron.New(cron.WithSeconds())

	return &Scheduler{
		cron:          c,
		db:            db,
		session:       session,
		analyzer:      analyzer,
		assigner:      assigner,
		reportGen:     reportGen,
		reportPoster:  reportPoster,
		configService: configService,
		aiTopicSvc:    aiTopicSvc,
		gptClient:     gptClient,
		guildID:       guildID,
	}
}

// Start begins the scheduler
func (s *Scheduler) Start() error {
	// Weekly role assignment task - Monday at 00:00:00
	_, err := s.cron.AddFunc("0 0 0 * * MON", s.weeklyRoleAssignmentTask)
	if err != nil {
		return fmt.Errorf("failed to add weekly role assignment task: %w", err)
	}

	// Weekly report generation task - Monday at 00:05:00 (5 minutes after role assignment)
	_, err = s.cron.AddFunc("0 5 0 * * MON", s.weeklyReportTask)
	if err != nil {
		return fmt.Errorf("failed to add weekly report task: %w", err)
	}

	// Daily username sync task - Every day at 03:00:00
	_, err = s.cron.AddFunc("0 0 3 * * *", s.dailyUsernameSyncTask)
	if err != nil {
		return fmt.Errorf("failed to add daily username sync task: %w", err)
	}

	// Daily data archival task - Every day at 02:00:00 (archives data older than 90 days)
	_, err = s.cron.AddFunc("0 0 2 * * *", s.dailyDataArchivalTask)
	if err != nil {
		return fmt.Errorf("failed to add daily data archival task: %w", err)
	}

	// Daily soft delete cleanup - Every day at 04:00:00 (hard deletes records soft-deleted 7+ days ago)
	_, err = s.cron.AddFunc("0 0 4 * * *", s.dailySoftDeleteCleanupTask)
	if err != nil {
		return fmt.Errorf("failed to add daily soft delete cleanup task: %w", err)
	}

	// AI content task - runs every minute, checks per-topic intervals
	_, err = s.cron.AddFunc("0 * * * * *", s.aiContentTask)
	if err != nil {
		return fmt.Errorf("failed to add AI content task: %w", err)
	}

	s.cron.Start()
	log.Info().Msg("Scheduler started with weekly tasks (roles, reports), daily tasks (username sync, archival, cleanup), and AI content task (every 5 minutes)")

	return nil
}

// Stop stops the scheduler
func (s *Scheduler) Stop() {
	ctx := s.cron.Stop()
	<-ctx.Done()
	log.Info().Msg("Scheduler stopped")
}

// weeklyRoleAssignmentTask handles the weekly role assignment
func (s *Scheduler) weeklyRoleAssignmentTask() {
	start := time.Now()
	ctx := context.Background()

	log.Info().
		Str("guild_id", s.guildID).
		Msg("Starting weekly role assignment task")

	// Use distributed lock to prevent concurrent execution
	lockName := fmt.Sprintf("weekly_role_assignment_%s", s.guildID)
	if !s.acquireLock(ctx, lockName) {
		log.Warn().Str("lock", lockName).Msg("Failed to acquire lock, task may be running elsewhere")
		return
	}
	defer s.releaseLock(ctx, lockName)

	// Step 1: Rollover week for all users
	if err := s.rolloverWeek(ctx); err != nil {
		log.Error().Err(err).Msg("Failed to rollover week")
		return
	}

	// Step 2: Archive last week's data
	if err := s.archiveWeekData(ctx); err != nil {
		log.Error().Err(err).Msg("Failed to archive week data")
		// Continue despite error
	}

	// Step 3: Get configuration
	config, err := s.configService.Get(ctx, s.guildID)
	if err != nil {
		log.Error().Err(err).Msg("Failed to get server config")
		return
	}

	// Step 4: Identify top 5 users
	topUsers, err := s.analyzer.GetTopActiveUsers(ctx, s.guildID, 5)
	if err != nil {
		log.Error().Err(err).Msg("Failed to get top active users")
		return
	}

	// Step 5: Assign roles
	if err := s.assigner.AssignActiveRole(ctx, s.guildID, config.ActiveRoleName, topUsers); err != nil {
		log.Error().Err(err).Msg("Failed to assign active roles")
		return
	}

	duration := time.Since(start)
	log.Info().
		Str("guild_id", s.guildID).
		Int("top_users", len(topUsers)).
		Dur("duration", duration).
		Msg("Weekly role assignment task completed successfully")
}

// rolloverWeek moves current week stats to previous week for all users
func (s *Scheduler) rolloverWeek(ctx context.Context) error {
	year, week := time.Now().ISOWeek()

	// Update all user activities
	result := s.db.WithContext(ctx).
		Model(&models.UserActivity{}).
		Where("guild_id = ?", s.guildID).
		Updates(map[string]interface{}{
			"previous_week_messages":     gorm.Expr("current_week_messages"),
			"current_week_messages":      0,
			"current_week_voice_minutes": 0,
			"current_week_num":           week,
		})

	if result.Error != nil {
		return fmt.Errorf("failed to rollover week: %w", result.Error)
	}

	log.Info().
		Str("guild_id", s.guildID).
		Int("year", year).
		Int("week", week).
		Int64("users_updated", result.RowsAffected).
		Msg("Week rollover completed")

	return nil
}

// archiveWeekData archives the previous week's activity to weekly_activities table
func (s *Scheduler) archiveWeekData(ctx context.Context) error {
	// Get previous week
	now := time.Now()
	year, week := now.AddDate(0, 0, -7).ISOWeek()
	weekStart, weekEnd := models.GetWeekBounds(year, week)

	// Get top users from previous week
	var activities []models.UserActivity
	err := s.db.WithContext(ctx).
		Where("guild_id = ? AND previous_week_messages > 0", s.guildID).
		Order("previous_week_messages DESC, username ASC").
		Find(&activities).Error

	if err != nil {
		return fmt.Errorf("failed to query previous week activities: %w", err)
	}

	if len(activities) == 0 {
		log.Info().
			Str("guild_id", s.guildID).
			Int("year", year).
			Int("week", week).
			Msg("No activities to archive for previous week")
		return nil
	}

	// Create weekly activity records with ranks
	weeklyActivities := make([]models.WeeklyActivity, 0, len(activities))
	currentRank := 1
	lastMessageCount := -1
	usersAtRank := 0

	for _, activity := range activities {
		// Calculate rank with ties
		if activity.PreviousWeekMessages != lastMessageCount {
			currentRank += usersAtRank
			usersAtRank = 0
			lastMessageCount = activity.PreviousWeekMessages
		}
		usersAtRank++

		weeklyActivity := models.WeeklyActivity{
			GuildID:      s.guildID,
			Year:         year,
			WeekNum:      week,
			WeekStart:    weekStart,
			WeekEnd:      weekEnd,
			UserID:       activity.UserID,
			Username:     activity.Username,
			MessageCount: activity.PreviousWeekMessages,
			VoiceMinutes: 0, // TODO: Track voice minutes
			Rank:         currentRank,
			ReceivedRole: activity.HasActiveRole,
			AvatarURL:    activity.AvatarURL,
		}

		weeklyActivities = append(weeklyActivities, weeklyActivity)
	}

	// Batch insert
	if err := s.db.WithContext(ctx).Create(&weeklyActivities).Error; err != nil {
		return fmt.Errorf("failed to create weekly activity records: %w", err)
	}

	log.Info().
		Str("guild_id", s.guildID).
		Int("year", year).
		Int("week", week).
		Int("records", len(weeklyActivities)).
		Msg("Week data archived successfully")

	return nil
}

// acquireLock attempts to acquire a distributed lock using MySQL
func (s *Scheduler) acquireLock(ctx context.Context, lockName string) bool {
	var result int
	err := s.db.WithContext(ctx).Raw("SELECT GET_LOCK(?, 0)", lockName).Scan(&result).Error
	if err != nil {
		log.Error().Err(err).Str("lock", lockName).Msg("Failed to acquire lock")
		return false
	}

	return result == 1
}

// releaseLock releases a distributed lock
func (s *Scheduler) releaseLock(ctx context.Context, lockName string) {
	var result int
	err := s.db.WithContext(ctx).Raw("SELECT RELEASE_LOCK(?)", lockName).Scan(&result).Error
	if err != nil {
		log.Error().Err(err).Str("lock", lockName).Msg("Failed to release lock")
	}
}

// weeklyReportTask handles the weekly report generation
func (s *Scheduler) weeklyReportTask() {
	start := time.Now()
	ctx := context.Background()

	log.Info().
		Str("guild_id", s.guildID).
		Msg("Starting weekly report generation task")

	// Use distributed lock to prevent concurrent execution
	lockName := fmt.Sprintf("weekly_report_%s", s.guildID)
	if !s.acquireLock(ctx, lockName) {
		log.Warn().Str("lock", lockName).Msg("Failed to acquire lock, task may be running elsewhere")
		return
	}
	defer s.releaseLock(ctx, lockName)

	// Get configuration
	config, err := s.configService.Get(ctx, s.guildID)
	if err != nil {
		log.Error().Err(err).Msg("Failed to get server config")
		return
	}

	// Get last week's data (reports are generated for the completed week)
	lastWeek := time.Now().AddDate(0, 0, -7)
	year, week := lastWeek.ISOWeek()

	// Generate report from archived data
	report, err := s.reportGen.GenerateWeeklyReport(ctx, s.guildID, year, week, "")
	if err != nil {
		log.Error().Err(err).Msg("Failed to generate weekly report")
		return
	}

	// Post to configured channel if available
	if config.ReportsChannelID != nil && *config.ReportsChannelID != "" {
		err = s.reportPoster.PostReport(ctx, report, *config.ReportsChannelID)
		if err != nil {
			log.Error().Err(err).
				Str("report_id", report.ID.String()).
				Str("channel_id", *config.ReportsChannelID).
				Msg("Failed to post weekly report")
			return
		}
	} else {
		log.Warn().Msg("Reports channel not configured, report generated but not posted")
	}

	duration := time.Since(start)
	log.Info().
		Str("guild_id", s.guildID).
		Str("report_id", report.ID.String()).
		Int("active_users", report.ActiveUsers).
		Int("total_messages", report.TotalMessages).
		Dur("duration", duration).
		Msg("Weekly report task completed successfully")
}

// dailyUsernameSyncTask syncs usernames from Discord daily
func (s *Scheduler) dailyUsernameSyncTask() {
	start := time.Now()
	ctx := context.Background()

	log.Info().
		Str("guild_id", s.guildID).
		Msg("Starting daily username sync task")

	// Use distributed lock to prevent concurrent execution
	lockName := fmt.Sprintf("username_sync_%s", s.guildID)
	if !s.acquireLock(ctx, lockName) {
		log.Warn().Str("lock", lockName).Msg("Failed to acquire lock, task may be running elsewhere")
		return
	}
	defer s.releaseLock(ctx, lockName)

	// Get all user activities for this guild
	var activities []models.UserActivity
	err := s.db.WithContext(ctx).
		Where("guild_id = ?", s.guildID).
		Find(&activities).Error

	if err != nil {
		log.Error().Err(err).Msg("Failed to fetch user activities for sync")
		return
	}

	if len(activities) == 0 {
		log.Info().Msg("No user activities to sync")
		return
	}

	// Fetch guild members from Discord
	members, err := s.session.GuildMembers(s.guildID, "", 1000)
	if err != nil {
		log.Error().Err(err).Msg("Failed to fetch guild members from Discord")
		return
	}

	// Create a map for quick lookup
	memberMap := make(map[string]*discordgo.Member)
	for _, member := range members {
		memberMap[member.User.ID] = member
	}

	// Update usernames
	updatedCount := 0
	for _, activity := range activities {
		member, exists := memberMap[activity.UserID]
		if !exists {
			continue
		}

		// Check if username or avatar has changed
		newUsername := member.User.Username
		newAvatarURL := member.User.AvatarURL("")

		if activity.Username != newUsername || activity.AvatarURL != newAvatarURL {
			activity.Username = newUsername
			activity.AvatarURL = newAvatarURL

			err := s.db.WithContext(ctx).Model(&activity).Updates(map[string]interface{}{
				"username":   newUsername,
				"avatar_url": newAvatarURL,
			}).Error

			if err != nil {
				log.Error().
					Err(err).
					Str("user_id", activity.UserID).
					Msg("Failed to update user activity")
				continue
			}

			updatedCount++
		}
	}

	duration := time.Since(start)
	log.Info().
		Str("guild_id", s.guildID).
		Int("total_users", len(activities)).
		Int("updated_users", updatedCount).
		Dur("duration", duration).
		Msg("Daily username sync task completed successfully")
}

// TriggerWeeklyTask manually triggers the weekly role assignment task (for testing)
func (s *Scheduler) TriggerWeeklyTask() {
	go s.weeklyRoleAssignmentTask()
}

// TriggerWeeklyReport manually triggers the weekly report task (for testing)
func (s *Scheduler) TriggerWeeklyReport() {
	go s.weeklyReportTask()
}

// TriggerUsernameSync manually triggers the username sync task (for testing)
func (s *Scheduler) TriggerUsernameSync() {
	go s.dailyUsernameSyncTask()
}

// dailyDataArchivalTask archives old data to keep the database performant
func (s *Scheduler) dailyDataArchivalTask() {
	start := time.Now()
	ctx := context.Background()

	log.Info().
		Str("guild_id", s.guildID).
		Msg("Starting daily data archival task")

	// Use distributed lock
	lockName := fmt.Sprintf("data_archival_%s", s.guildID)
	if !s.acquireLock(ctx, lockName) {
		log.Warn().Str("lock", lockName).Msg("Failed to acquire lock, task may be running elsewhere")
		return
	}
	defer s.releaseLock(ctx, lockName)

	// Archive threshold: 90 days ago
	archiveThreshold := time.Now().UTC().AddDate(0, 0, -90)

	// Archive old message exports
	var archivedExports int64
	result := s.db.WithContext(ctx).
		Model(&models.MessageExport{}).
		Where("created_at < ?", archiveThreshold).
		Where("archived_at IS NULL").
		Update("archived_at", time.Now().UTC())

	if result.Error != nil {
		log.Error().Err(result.Error).Msg("Failed to archive old message exports")
	} else {
		archivedExports = result.RowsAffected
	}

	// Archive old weekly activity records
	var archivedWeeklyActivity int64
	result = s.db.WithContext(ctx).
		Model(&models.WeeklyActivity{}).
		Where("week_start < ?", archiveThreshold).
		Where("archived_at IS NULL").
		Update("archived_at", time.Now().UTC())

	if result.Error != nil {
		log.Error().Err(result.Error).Msg("Failed to archive old weekly activity")
	} else {
		archivedWeeklyActivity = result.RowsAffected
	}

	// Archive old activity reports
	var archivedReports int64
	result = s.db.WithContext(ctx).
		Model(&models.WeeklyReport{}).
		Where("created_at < ?", archiveThreshold).
		Where("archived_at IS NULL").
		Update("archived_at", time.Now().UTC())

	if result.Error != nil {
		log.Error().Err(result.Error).Msg("Failed to archive old activity reports")
	} else {
		archivedReports = result.RowsAffected
	}

	duration := time.Since(start)
	log.Info().
		Str("guild_id", s.guildID).
		Int64("archived_exports", archivedExports).
		Int64("archived_weekly_activity", archivedWeeklyActivity).
		Int64("archived_reports", archivedReports).
		Dur("duration", duration).
		Msg("Daily data archival task completed successfully")
}

// dailySoftDeleteCleanupTask permanently deletes records that were soft-deleted 7+ days ago
func (s *Scheduler) dailySoftDeleteCleanupTask() {
	start := time.Now()
	ctx := context.Background()

	log.Info().
		Str("guild_id", s.guildID).
		Msg("Starting daily soft delete cleanup task")

	// Use distributed lock
	lockName := fmt.Sprintf("soft_delete_cleanup_%s", s.guildID)
	if !s.acquireLock(ctx, lockName) {
		log.Warn().Str("lock", lockName).Msg("Failed to acquire lock, task may be running elsewhere")
		return
	}
	defer s.releaseLock(ctx, lockName)

	// Cleanup threshold: 7 days ago
	cleanupThreshold := time.Now().UTC().AddDate(0, 0, -7)

	// Hard delete old soft-deleted message exports
	var deletedExports int64
	result := s.db.WithContext(ctx).
		Unscoped(). // Allows permanent deletion
		Where("deleted_at IS NOT NULL").
		Where("deleted_at < ?", cleanupThreshold).
		Delete(&models.MessageExport{})

	if result.Error != nil {
		log.Error().Err(result.Error).Msg("Failed to cleanup old message exports")
	} else {
		deletedExports = result.RowsAffected
	}

	// Hard delete old soft-deleted weekly activity records
	var deletedWeeklyActivity int64
	result = s.db.WithContext(ctx).
		Unscoped().
		Where("deleted_at IS NOT NULL").
		Where("deleted_at < ?", cleanupThreshold).
		Delete(&models.WeeklyActivity{})

	if result.Error != nil {
		log.Error().Err(result.Error).Msg("Failed to cleanup old weekly activity")
	} else {
		deletedWeeklyActivity = result.RowsAffected
	}

	// Hard delete old soft-deleted activity reports
	var deletedReports int64
	result = s.db.WithContext(ctx).
		Unscoped().
		Where("deleted_at IS NOT NULL").
		Where("deleted_at < ?", cleanupThreshold).
		Delete(&models.WeeklyReport{})

	if result.Error != nil {
		log.Error().Err(result.Error).Msg("Failed to cleanup old activity reports")
	} else {
		deletedReports = result.RowsAffected
	}

	duration := time.Since(start)
	log.Info().
		Str("guild_id", s.guildID).
		Int64("deleted_exports", deletedExports).
		Int64("deleted_weekly_activity", deletedWeeklyActivity).
		Int64("deleted_reports", deletedReports).
		Dur("duration", duration).
		Msg("Daily soft delete cleanup task completed successfully")
}

// TriggerDataArchival manually triggers the data archival task (for testing)
func (s *Scheduler) TriggerDataArchival() {
	go s.dailyDataArchivalTask()
}

// TriggerSoftDeleteCleanup manually triggers the soft delete cleanup task (for testing)
func (s *Scheduler) TriggerSoftDeleteCleanup() {
	go s.dailySoftDeleteCleanupTask()
}


// aiContentTask checks all topics every minute and posts AI-generated content when due
func (s *Scheduler) aiContentTask() {
	ctx := context.Background()
	now := time.Now().UTC()

	dueTopics, err := s.aiTopicSvc.GetDueTopics(ctx, s.guildID)
	if err != nil {
		log.Error().Err(err).Str("guild_id", s.guildID).Msg("AI content task: failed to get due topics")
		return
	}
	if len(dueTopics) == 0 {
		return
	}

	for _, topic := range dueTopics {
		// Calculate the last scheduled slot (not necessarily now).
		// If the bot was down and multiple intervals were skipped,
		// we still only post once and align last_posted_at to the schedule.
		var scheduledTime time.Time
		if topic.LastPostedAt == nil {
			scheduledTime = now
		} else {
			interval := time.Duration(topic.IntervalMinutes) * time.Minute
			elapsed := now.Sub(*topic.LastPostedAt)
			slots := elapsed / interval // integer division — how many full intervals passed
			scheduledTime = topic.LastPostedAt.Add(slots * interval)
		}
		// Replace {mention} placeholder in topic with Discord mention before sending to GPT
		topicText := topic.Topic
		if topic.MentionUserID != nil && *topic.MentionUserID != "" {
			topicText = strings.ReplaceAll(topicText, "{mention}", "<@"+*topic.MentionUserID+">")
		}

		messages := []openai.ChatCompletionMessage{
			{
				Role: openai.ChatMessageRoleSystem,
				Content: "Bạn là một AI sáng tạo, viết bằng tiếng Việt. " +
					"Hãy viết một đoạn văn tự nhiên, hấp dẫn, dài khoảng 100-200 từ về chủ đề được yêu cầu. " +
					"Không cần tiêu đề, chỉ viết đoạn văn. " +
					"Nếu chủ đề có chuỗi <@ID>, hãy giữ nguyên chuỗi đó trong đoạn văn.",
			},
			{Role: openai.ChatMessageRoleUser, Content: "Viết một đoạn văn về: " + topicText},
		}

		content, _, err := s.gptClient.CreateChatCompletionWithRetry(ctx, messages)
		if err != nil {
			log.Error().Err(err).Str("guild_id", s.guildID).Str("topic_id", topic.ID.String()).Msg("AI content task: GPT call failed")
			continue
		}

		message := content
		if _, err := s.session.ChannelMessageSend(topic.ChannelID, message); err != nil {
			log.Error().Err(err).Str("channel_id", topic.ChannelID).Msg("AI content task: failed to send message")
			continue
		}

		if err := s.aiTopicSvc.MarkPosted(ctx, topic.ID.String(), scheduledTime); err != nil {
			log.Error().Err(err).Str("topic_id", topic.ID.String()).Msg("AI content task: failed to mark posted")
		}
	}
}
