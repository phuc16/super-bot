package commands

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/rs/zerolog/log"

	"github.com/phuc16/VCG-super-bot/internal/services/customcmd"
)

// maxCustomCommandImageBytes caps what /addcom will store. Discord allows
// 20 MiB by default (more on boosted servers), but these images are
// re-uploaded on every keyword hit, and they live in a MySQL LONGBLOB, so
// the practical limit is lower than what Discord would accept.
const maxCustomCommandImageBytes = 8 << 20 // 8 MiB

// imageDownloadTimeout bounds the fetch. It runs inside the 3-second
// interaction-response window, so a slow CDN must fail fast enough to still
// answer the user rather than leaving "This interaction failed" on screen.
const imageDownloadTimeout = 10 * time.Second

// downloadAttachment fetches an attachment's bytes while its signed URL is
// still valid.
//
// This is the whole point of the feature: slash-command attachments are
// served from /ephemeral-attachments/ with a URL carrying an expiry
// signature, and Discord does not retain that content — it cannot be
// re-signed later. The bytes must be taken now or they are gone.
//
// Reads one byte past the cap so an oversized file is rejected outright
// rather than silently stored truncated.
func downloadAttachment(url string, limit int64) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), imageDownloadTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("attachment fetch returned %s", resp.Status)
	}

	data, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("ảnh lớn hơn %d MB", limit>>20)
	}
	if len(data) == 0 {
		return nil, errors.New("attachment was empty")
	}
	return data, nil
}

// AddcomCommand handles the /addcom slash command
type AddcomCommand struct {
	svc *customcmd.Service
}

// NewAddcomCommand creates a new AddcomCommand
func NewAddcomCommand(svc *customcmd.Service) *AddcomCommand {
	return &AddcomCommand{svc: svc}
}

// Definition returns the Discord application command definition
func (cmd *AddcomCommand) Definition() *discordgo.ApplicationCommand {
	return &discordgo.ApplicationCommand{
		Name:        "addcom",
		Description: "Thêm keyword command (Admin only)",
		Options: []*discordgo.ApplicationCommandOption{
			{
				Type:        discordgo.ApplicationCommandOptionString,
				Name:        "keyword",
				Description: "Keyword (không cần dấu /), ví dụ: Kami",
				Required:    true,
			},
			{
				Type:        discordgo.ApplicationCommandOptionString,
				Name:        "response",
				Description: "Nội dung bot sẽ reply (để trống nếu chỉ reply ảnh)",
				Required:    false,
			},
			{
				Type:        discordgo.ApplicationCommandOptionAttachment,
				Name:        "image",
				Description: "Ảnh bot sẽ reply kèm theo (tùy chọn)",
				Required:    false,
			},
		},
	}
}

// Handle processes the /addcom interaction
func (cmd *AddcomCommand) Handle(s *discordgo.Session, i *discordgo.InteractionCreate) {
	if !hasAdminPermission(i.Member) {
		respondError(s, i, "Bạn cần quyền Administrator hoặc Manage Server để dùng lệnh này.")
		return
	}

	data := i.ApplicationCommandData()
	options := parseOptions(data.Options)

	keyword, _ := options["keyword"].(string)
	keyword = strings.TrimPrefix(strings.TrimSpace(keyword), "/")
	if keyword == "" {
		respondError(s, i, "Keyword không được để trống.")
		return
	}

	response, _ := options["response"].(string)

	// Download the image NOW, not later: this URL is signed and expires
	// within hours, and Discord will not re-issue it.
	var imageData []byte
	var imageName string
	if attachID, ok := options["image"].(string); ok && attachID != "" {
		if attach, ok := data.Resolved.Attachments[attachID]; ok {
			var err error
			imageData, err = downloadAttachment(attach.URL, maxCustomCommandImageBytes)
			if err != nil {
				log.Error().Err(err).Str("guild_id", i.GuildID).Str("trigger", keyword).Msg("Failed to download custom command image")
				respondError(s, i, "Không tải được ảnh: "+err.Error()+". Thử lại hoặc dùng ảnh nhỏ hơn.")
				return
			}
			imageName = attach.Filename
		}
	}

	if response == "" && len(imageData) == 0 {
		respondError(s, i, "Phải có ít nhất `response` hoặc `image`.")
		return
	}

	err := cmd.svc.Add(context.Background(), i.GuildID, keyword, response, i.Member.User.ID, imageData, imageName)
	if errors.Is(err, customcmd.ErrAlreadyExists) {
		respondError(s, i, "Keyword `"+keyword+"` đã tồn tại. Dùng `/delcom` để xóa trước.")
		return
	}
	if err != nil {
		log.Error().Err(err).Str("guild_id", i.GuildID).Str("trigger", keyword).Msg("Failed to add custom command")
		respondError(s, i, "Có lỗi xảy ra khi thêm keyword.")
		return
	}

	desc := "`" + keyword + "`"
	if response != "" {
		desc += " → **" + response + "**"
	}
	if len(imageData) > 0 {
		desc += " + ảnh"
	}
	respondSuccess(s, i, "✅ Đã thêm keyword "+desc+"\nNhắn `/"+keyword+"` để dùng.")
}
