package scheduler

import (
	"context"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"
	openai "github.com/sashabaranov/go-openai"
	"github.com/robfig/cron/v3"
	"github.com/rs/zerolog/log"
	"gorm.io/gorm"

	"github.com/phuc16/VCG-super-bot/internal/gpt"
	"github.com/phuc16/VCG-super-bot/internal/services/aitopic"
)

// Scheduler manages scheduled tasks
type Scheduler struct {
	cron       *cron.Cron
	db         *gorm.DB
	session    *discordgo.Session
	aiTopicSvc *aitopic.Service
	gptClient  *gpt.Client
	guildID    string
}

// NewScheduler creates a new task scheduler
func NewScheduler(db *gorm.DB, session *discordgo.Session, aiTopicSvc *aitopic.Service, gptClient *gpt.Client, guildID string) *Scheduler {
	return &Scheduler{
		cron:       cron.New(cron.WithSeconds()),
		db:         db,
		session:    session,
		aiTopicSvc: aiTopicSvc,
		gptClient:  gptClient,
		guildID:    guildID,
	}
}

// Start begins the scheduler
func (s *Scheduler) Start() error {
	// AI content task - runs every minute, checks per-topic intervals
	if _, err := s.cron.AddFunc("0 * * * * *", s.aiContentTask); err != nil {
		return err
	}
	s.cron.Start()
	log.Info().Msg("Scheduler started (AI content task every minute)")
	return nil
}

// Stop stops the scheduler
func (s *Scheduler) Stop() {
	ctx := s.cron.Stop()
	<-ctx.Done()
	log.Info().Msg("Scheduler stopped")
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
		// Calculate last scheduled slot to align future posts to the original schedule
		var scheduledTime time.Time
		if topic.LastPostedAt == nil {
			scheduledTime = now
		} else {
			interval := time.Duration(topic.IntervalMinutes) * time.Minute
			slots := now.Sub(*topic.LastPostedAt) / interval
			scheduledTime = topic.LastPostedAt.Add(slots * interval)
		}

		// Replace {mention} placeholder with Discord mention before sending to GPT
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
			log.Error().Err(err).Str("topic_id", topic.ID.String()).Msg("AI content task: GPT call failed")
			continue
		}

		if _, err := s.session.ChannelMessageSend(topic.ChannelID, content); err != nil {
			log.Error().Err(err).Str("channel_id", topic.ChannelID).Msg("AI content task: failed to send message")
			continue
		}

		if err := s.aiTopicSvc.MarkPosted(ctx, topic.ID.String(), scheduledTime); err != nil {
			log.Error().Err(err).Str("topic_id", topic.ID.String()).Msg("AI content task: failed to mark posted")
		}
	}
}
