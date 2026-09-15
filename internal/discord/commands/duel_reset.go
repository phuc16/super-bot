package commands

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/rs/zerolog/log"

	"github.com/phuc16/VCG-super-bot/internal/services/duelstats"
)

const (
	resetConfirmID = "duelreset:confirm:"
	resetCancelID  = "duelreset:cancel:"
	resetExpiry    = 30 * time.Second
)

// pendingReset tracks a /duel-reset awaiting the initiating admin's confirmation.
type pendingReset struct {
	GuildID        string
	ChannelID      string
	MessageID      string
	InitiatorID    string
	IncludeHistory bool
	Timer          *time.Timer
}

// DuelResetCommand handles the /duel-reset slash command and its confirm/cancel buttons
type DuelResetCommand struct {
	svc *duelstats.Service

	mu      sync.Mutex
	pending map[string]*pendingReset
}

// NewDuelResetCommand creates a new DuelResetCommand
func NewDuelResetCommand(svc *duelstats.Service) *DuelResetCommand {
	return &DuelResetCommand{svc: svc, pending: make(map[string]*pendingReset)}
}

// Definition returns the Discord application command definition
func (cmd *DuelResetCommand) Definition() *discordgo.ApplicationCommand {
	return &discordgo.ApplicationCommand{
		Name:        "duel-reset",
		Description: "Reset toàn bộ điểm/thắng/thua/streak của /duel trong server (Admin only)",
		Options: []*discordgo.ApplicationCommandOption{
			{
				Type:        discordgo.ApplicationCommandOptionBoolean,
				Name:        "wipe_history",
				Description: "Xoá luôn lịch sử đối đầu (/duel-history), mặc định false",
				Required:    false,
			},
		},
	}
}

// Handle processes the /duel-reset interaction: asks for confirmation before
// wiping anything, since this action is destructive and irreversible.
func (cmd *DuelResetCommand) Handle(s *discordgo.Session, i *discordgo.InteractionCreate) {
	if !hasAdminPermission(i.Member) {
		respondError(s, i, "Bạn cần quyền Administrator hoặc Manage Server để dùng lệnh này.")
		return
	}

	options := parseOptions(i.ApplicationCommandData().Options)
	wipeHistory, _ := options["wipe_history"].(bool)

	resetID := i.Interaction.ID

	warning := "⚠️ **Xác nhận reset toàn bộ điểm/thắng/thua/streak của `/duel` trong server này.**\nHành động này KHÔNG thể hoàn tác."
	if wipeHistory {
		warning += "\nLịch sử đối đầu (`/duel-history`) cũng sẽ bị xoá."
	}
	warning += "\n\nBạn có 30 giây để xác nhận."

	err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Content:    warning,
			Components: resetButtons(resetID, false),
		},
	})
	if err != nil {
		log.Error().Err(err).Msg("duel-reset: failed to send confirmation")
		return
	}

	msg, err := s.InteractionResponse(i.Interaction)
	if err != nil {
		log.Error().Err(err).Msg("duel-reset: failed to fetch confirmation message")
		return
	}

	pr := &pendingReset{
		GuildID:        i.GuildID,
		ChannelID:      i.ChannelID,
		MessageID:      msg.ID,
		InitiatorID:    i.Member.User.ID,
		IncludeHistory: wipeHistory,
	}
	pr.Timer = time.AfterFunc(resetExpiry, func() {
		cmd.expire(s, resetID)
	})

	cmd.mu.Lock()
	cmd.pending[resetID] = pr
	cmd.mu.Unlock()
}

// HandleComponent processes Confirm/Cancel button clicks for a pending reset.
// Returns true if it handled the interaction (customID matched this command).
func (cmd *DuelResetCommand) HandleComponent(s *discordgo.Session, i *discordgo.InteractionCreate) bool {
	customID := i.MessageComponentData().CustomID

	var confirmed bool
	var resetID string
	switch {
	case strings.HasPrefix(customID, resetConfirmID):
		confirmed = true
		resetID = strings.TrimPrefix(customID, resetConfirmID)
	case strings.HasPrefix(customID, resetCancelID):
		confirmed = false
		resetID = strings.TrimPrefix(customID, resetCancelID)
	default:
		return false
	}

	cmd.mu.Lock()
	pr, ok := cmd.pending[resetID]
	if ok {
		delete(cmd.pending, resetID)
	}
	cmd.mu.Unlock()

	if !ok {
		respondComponentEphemeral(s, i, "Yêu cầu reset này đã hết hạn hoặc đã được xử lý.")
		return true
	}
	pr.Timer.Stop()

	if i.Member.User.ID != pr.InitiatorID {
		respondComponentEphemeral(s, i, "Chỉ người đã chạy `/duel-reset` mới được xác nhận.")
		cmd.mu.Lock()
		cmd.pending[resetID] = pr
		cmd.mu.Unlock()
		pr.Timer.Reset(resetExpiry)
		return true
	}

	if !confirmed {
		_ = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseUpdateMessage,
			Data: &discordgo.InteractionResponseData{
				Content:    "🚫 Đã huỷ reset.",
				Components: []discordgo.MessageComponent{},
			},
		})
		return true
	}

	if err := cmd.svc.ResetAll(context.Background(), pr.GuildID, pr.IncludeHistory); err != nil {
		log.Error().Err(err).Msg("duel-reset: failed to reset")
		_ = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseUpdateMessage,
			Data: &discordgo.InteractionResponseData{
				Content:    "❌ Có lỗi xảy ra khi reset.",
				Components: []discordgo.MessageComponent{},
			},
		})
		return true
	}

	msg := fmt.Sprintf("✅ Đã reset toàn bộ điểm, thắng/thua, và streak của `/duel`. Mọi người sẽ bắt đầu lại từ %d điểm.", duelstats.StartingPoints)
	if pr.IncludeHistory {
		msg += " Lịch sử đối đầu cũng đã bị xoá."
	}
	_ = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseUpdateMessage,
		Data: &discordgo.InteractionResponseData{
			Content:    msg,
			Components: []discordgo.MessageComponent{},
		},
	})
	return true
}

// expire edits the confirmation message to show it timed out, if still pending.
func (cmd *DuelResetCommand) expire(s *discordgo.Session, resetID string) {
	cmd.mu.Lock()
	pr, ok := cmd.pending[resetID]
	if ok {
		delete(cmd.pending, resetID)
	}
	cmd.mu.Unlock()
	if !ok {
		return
	}

	content := "⌛ Yêu cầu reset đã hết hạn (không xác nhận kịp)."
	components := []discordgo.MessageComponent{}
	_, err := s.ChannelMessageEditComplex(&discordgo.MessageEdit{
		Channel:    pr.ChannelID,
		ID:         pr.MessageID,
		Content:    &content,
		Components: &components,
	})
	if err != nil {
		log.Error().Err(err).Msg("duel-reset: failed to edit expired confirmation message")
	}
}

func resetButtons(resetID string, disabled bool) []discordgo.MessageComponent {
	return []discordgo.MessageComponent{
		discordgo.ActionsRow{
			Components: []discordgo.MessageComponent{
				discordgo.Button{
					Label:    "Xác nhận reset",
					Style:    discordgo.DangerButton,
					CustomID: resetConfirmID + resetID,
					Disabled: disabled,
				},
				discordgo.Button{
					Label:    "Huỷ",
					Style:    discordgo.SecondaryButton,
					CustomID: resetCancelID + resetID,
					Disabled: disabled,
				},
			},
		},
	}
}
