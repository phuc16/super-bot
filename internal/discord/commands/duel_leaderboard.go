package commands

import (
	"context"
	"fmt"

	"github.com/bwmarrin/discordgo"
	"github.com/rs/zerolog/log"

	"github.com/phuc16/VCG-super-bot/internal/models"
	"github.com/phuc16/VCG-super-bot/internal/services/duelstats"
)

// DuelLeaderboardCommand handles the /duel-leaderboard slash command
type DuelLeaderboardCommand struct {
	svc *duelstats.Service
}

// NewDuelLeaderboardCommand creates a new DuelLeaderboardCommand
func NewDuelLeaderboardCommand(svc *duelstats.Service) *DuelLeaderboardCommand {
	return &DuelLeaderboardCommand{svc: svc}
}

// Definition returns the Discord application command definition
func (cmd *DuelLeaderboardCommand) Definition() *discordgo.ApplicationCommand {
	return &discordgo.ApplicationCommand{
		Name:        "duel-leaderboard",
		Description: "Xem bảng xếp hạng /duel trong server",
	}
}

var duelMedals = []string{"🥇", "🥈", "🥉"}

// Handle processes the /duel-leaderboard interaction
func (cmd *DuelLeaderboardCommand) Handle(s *discordgo.Session, i *discordgo.InteractionCreate) {
	ctx := context.Background()

	stats, err := cmd.svc.Leaderboard(ctx, i.GuildID, 10)
	if err != nil {
		log.Error().Err(err).Msg("duel-leaderboard: failed to get leaderboard")
		respondError(s, i, "Có lỗi xảy ra.")
		return
	}
	if len(stats) == 0 {
		respondSuccess(s, i, "Chưa có ai dùng `/duel` cả.")
		return
	}

	content := "⚔️ **Bảng xếp hạng Duel**\n\n"
	for idx, stat := range stats {
		rank := fmt.Sprintf("%d.", idx+1)
		if idx < len(duelMedals) {
			rank = duelMedals[idx]
		}

		streakNote := ""
		switch {
		case stat.CurrentStreak >= 2:
			streakNote = fmt.Sprintf(" 🔥x%d", stat.CurrentStreak)
		case stat.CurrentStreak <= -2:
			streakNote = fmt.Sprintf(" 🥶x%d", -stat.CurrentStreak)
		}

		content += fmt.Sprintf(
			"%s <@%s> — **%d điểm** (%dT/%dB/%dH)%s\n",
			rank, stat.UserID, stat.Points, stat.Wins, stat.Losses, stat.Draws, streakNote,
		)
	}

	userStat, err := cmd.svc.Get(ctx, i.GuildID, i.Member.User.ID)
	if err == nil && userStat != nil && !inTop(stats, i.Member.User.ID) {
		content += fmt.Sprintf(
			"\n...\n**%s** — **%d điểm** (%dT/%dB/%dH)\n",
			nameTag(i.Member.User.ID), userStat.Points, userStat.Wins, userStat.Losses, userStat.Draws,
		)
	}

	err = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Content:         content,
			AllowedMentions: &discordgo.MessageAllowedMentions{}, // render @mentions without pinging
		},
	})
	if err != nil {
		log.Error().Err(err).Msg("duel-leaderboard: failed to send response")
	}
}

func inTop(stats []models.DuelStat, userID string) bool {
	for _, stat := range stats {
		if stat.UserID == userID {
			return true
		}
	}
	return false
}
