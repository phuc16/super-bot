package commands

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/rs/zerolog/log"
)

// roleEmoji is the emoji prefix used in player-facing role-count text —
// role.label() itself is plain text, so this is the announcement/editor
// layer's own small mapping, kept separate from role.label() since not
// every caller of label() wants an emoji prefix.
func roleEmoji(r role) string {
	switch r {
	case roleWerewolf:
		return "🐺"
	case roleWolfCub:
		return "🐺👶"
	case roleCursedWolf:
		return "🐺✨"
	case roleTraitor:
		return "🎭"
	case roleSeer:
		return "🔮"
	case roleDoctor:
		return "💉"
	case roleWitch:
		return "🧙"
	case roleHunter:
		return "🏹"
	case roleDisruptor:
		return "🤫"
	case roleCupid:
		return "💘"
	default:
		return "👤"
	}
}

// formatRoleCountsAnnouncement builds the role-count breakdown for
// startPreflight's role-assigned announcement, counting the players map
// that was ACTUALLY dealt — correct for both the fixed table and a
// custom-roles game, unlike re-deriving from rolesForPlayerCount (which
// only ever agrees with reality on the fixed-table path, since that path
// alone is what it can see). Zero-count roles are omitted; Villager
// (whatever's left over) is always listed last if present.
func formatRoleCountsAnnouncement(players map[string]*player) string {
	counts := make(map[role]int)
	for _, p := range players {
		counts[p.Role]++
	}
	var parts []string
	for _, r := range append(append([]role{}, roleCountWolfSideRoles...), roleCountVillagerSideRoles...) {
		if r == roleVillager {
			continue // rendered last, below
		}
		if c := counts[r]; c > 0 {
			parts = append(parts, fmt.Sprintf("%s %s: %d", roleEmoji(r), r.label(), c))
		}
	}
	if c := counts[roleVillager]; c > 0 {
		parts = append(parts, fmt.Sprintf("%s %s: %d", roleEmoji(roleVillager), roleVillager.label(), c))
	}
	return "\n" + strings.Join(parts, " · ")
}

const (
	werewolfRoleCountID        = "ww:rolecount:"
	werewolfRoleCountConfirmID = "ww:rolecountconfirm:"

	// roleEditTimeout is how long the starter has to confirm the manual
	// role-count editor before the game falls back to the fixed table
	// automatically.
	roleEditTimeout = 5 * time.Minute
)

// roleCountWolfSideRoles and roleCountVillagerSideRoles are the manual-pick
// editor's two fixed groupings (spec §3b's "Phe Sói" / "Phe Dân" split).
// Cursed Wolf is grouped under the wolf side here even though its starting
// Faction is factionVillager (it doesn't flip until a bite conversion) —
// this grouping is a UI/setup-time concept distinct from Faction, matching
// the reference's own preset grouping.
var (
	roleCountWolfSideRoles     = []role{roleWerewolf, roleWolfCub, roleCursedWolf, roleTraitor}
	roleCountVillagerSideRoles = []role{roleVillager, roleSeer, roleDoctor, roleWitch, roleHunter, roleDisruptor, roleCupid}
)

// roleCountMax returns the highest count the manual editor allows for role
// r, given playerCount total seats. Seven roles are capped at 1 — see this
// plan's Global Constraints for why (each is only ever actually acted on
// via the first alive player of that role in the engine today, so a
// second one dealt would silently do nothing). Every other countable role
// is capped only by the total player count, which the confirm-time
// wolf-side/villager-side balance check (roleCountConfirmValid) further
// constrains in practice.
func roleCountMax(r role, playerCount int) int {
	switch r {
	case roleSeer, roleDoctor, roleWitch, roleHunter, roleDisruptor, roleCupid, roleTraitor:
		return 1
	default:
		return playerCount
	}
}

// balanceScore sums rolePoints across every counted role — the manual
// editor's live, informational-only readout (spec §2: no fixed pass/fail
// threshold is load-bearing here).
func balanceScore(counts map[role]int) int {
	score := 0
	for r, c := range counts {
		score += rolePoints[r] * c
	}
	return score
}

// roleCountConfirmValid mirrors the reference's setupValid: the total
// count must exactly match playerCount, and both the wolf-side and
// villager-side groupings must have at least one role counted. It also
// requires at least one actual wolf-vote-participant (Werewolf or Wolf
// Cub) — Cursed Wolf and Traitor both count toward the wolf-side grouping
// above but neither one ever votes in the wolf kill, so a wolf side made
// up of only those two would leave the game with no wolf thread and no
// possible kill, instantly resolving as a villager win.
func roleCountConfirmValid(counts map[role]int, playerCount int) bool {
	wolfSide, villagerSide := 0, 0
	for _, r := range roleCountWolfSideRoles {
		wolfSide += counts[r]
	}
	for _, r := range roleCountVillagerSideRoles {
		villagerSide += counts[r]
	}
	wolfVoteParticipants := counts[roleWerewolf] + counts[roleWolfCub]
	return wolfSide+villagerSide == playerCount && wolfSide >= 1 && villagerSide >= 1 && wolfVoteParticipants >= 1
}

// roleRatioFromCounts converts the editor's counts into a roleRatio for
// dealRoles. roleVillager has no roleRatio field on purpose — dealRoles
// fills every seat dealRoles's assign() calls don't claim as roleVillager,
// and since roleCountConfirmValid already guarantees the total matches
// playerCount, that remainder is exactly what the editor showed as the
// Villager count.
func roleRatioFromCounts(counts map[role]int) roleRatio {
	return roleRatio{
		Werewolves:   counts[roleWerewolf],
		WolfCubs:     counts[roleWolfCub],
		CursedWolves: counts[roleCursedWolf],
		Traitors:     counts[roleTraitor],
		Seers:        counts[roleSeer],
		Doctors:      counts[roleDoctor],
		Witches:      counts[roleWitch],
		Hunters:      counts[roleHunter],
		Disruptors:   counts[roleDisruptor],
		Cupids:       counts[roleCupid],
	}
}

// roleCountEditorContent renders one side of the editor: that side's
// per-role counts, the combined running total against playerCount, and
// the combined balance score (computed across BOTH sides, since the score
// reflects the whole game's shape, not just this message's side).
//
// Deliberate simplification: only the message that was just clicked gets
// re-rendered (see werewolf_rolepick.go's handleRoleCountButton) — the
// OTHER side's message can show a stale total/score until it's next
// interacted with. This is a display-freshness nit, not a correctness
// issue: confirm-time validation (roleCountConfirmValid) always reads the
// true shared counts, never anything from a message's displayed content.
// Keeping the two messages in perfect sync would require tracking the
// original slash-command interaction's token to edit the other message via
// FollowupMessageEdit — real complexity this feature doesn't need.
func roleCountEditorContent(side string, counts map[role]int, playerCount int) string {
	var roles []role
	var title string
	switch side {
	case "wolf":
		roles = roleCountWolfSideRoles
		title = "🐺 Phe Sói"
	case "villager":
		roles = roleCountVillagerSideRoles
		title = "🏡 Phe Dân"
	}

	total := 0
	for _, r := range roleCountWolfSideRoles {
		total += counts[r]
	}
	for _, r := range roleCountVillagerSideRoles {
		total += counts[r]
	}

	var b strings.Builder
	fmt.Fprintf(&b, "**%s** — chọn số lượng cho mỗi vai trò:\n", title)
	for _, r := range roles {
		fmt.Fprintf(&b, "%s: %d\n", r.label(), counts[r])
	}
	fmt.Fprintf(&b, "\nTổng: %d/%d người · Điểm cân bằng: %d", total, playerCount, balanceScore(counts))
	return b.String()
}

// roleCountButtonRows builds one editor message's component rows: the
// given roles' -/+ buttons, two roles per row, plus a trailing confirm
// row. customID encodes the sign ("inc"/"dec"), the role's int value, and
// the channel ID, colon-separated after the werewolfRoleCountID prefix —
// this is this codebase's first customID needing more than one encoded
// field, so it's parsed with strings.SplitN rather than the simple
// TrimPrefix-only pattern every other werewolf customID uses.
func roleCountButtonRows(channelID string, roles []role, counts map[role]int, playerCount int) []discordgo.MessageComponent {
	var rows []discordgo.MessageComponent
	for start := 0; start < len(roles); start += 2 {
		end := min(start+2, len(roles))
		var buttons []discordgo.MessageComponent
		for _, r := range roles[start:end] {
			current := counts[r]
			buttons = append(buttons,
				discordgo.Button{
					Label:    "➖ " + r.label(),
					Style:    discordgo.SecondaryButton,
					CustomID: fmt.Sprintf("%sdec:%d:%s", werewolfRoleCountID, int(r), channelID),
					Disabled: current <= 0,
				},
				discordgo.Button{
					Label:    "➕ " + r.label(),
					Style:    discordgo.SecondaryButton,
					CustomID: fmt.Sprintf("%sinc:%d:%s", werewolfRoleCountID, int(r), channelID),
					Disabled: current >= roleCountMax(r, playerCount),
				},
			)
		}
		rows = append(rows, discordgo.ActionsRow{Components: buttons})
	}
	rows = append(rows, discordgo.ActionsRow{
		Components: []discordgo.MessageComponent{
			discordgo.Button{
				Label:    "✅ Xác nhận",
				Style:    discordgo.SuccessButton,
				CustomID: werewolfRoleCountConfirmID + channelID,
			},
		},
	})
	return rows
}

// sendRoleCountEditor sends the two ephemeral editor messages (visible
// only to the starter) as followups to the "Bắt đầu" interaction, then
// starts the abandon-timeout watchdog.
func (cmd *WerewolfCommand) sendRoleCountEditor(s *discordgo.Session, interaction *discordgo.Interaction, channelID string, playerCount int, seq uint64) {
	counts := map[role]int{}
	if _, err := s.FollowupMessageCreate(interaction, true, &discordgo.WebhookParams{
		Content:    roleCountEditorContent("wolf", counts, playerCount),
		Components: roleCountButtonRows(channelID, roleCountWolfSideRoles, counts, playerCount),
		Flags:      discordgo.MessageFlagsEphemeral,
	}); err != nil {
		log.Error().Err(err).Msg("werewolf: failed to send wolf-side role-count editor")
	}
	if _, err := s.FollowupMessageCreate(interaction, true, &discordgo.WebhookParams{
		Content:    roleCountEditorContent("villager", counts, playerCount),
		Components: roleCountButtonRows(channelID, roleCountVillagerSideRoles, counts, playerCount),
		Flags:      discordgo.MessageFlagsEphemeral,
	}); err != nil {
		log.Error().Err(err).Msg("werewolf: failed to send villager-side role-count editor")
	}

	go cmd.waitForRoleCountConfirm(s, channelID, seq)
}

// waitForRoleCountConfirm polls until the starter confirms (game.Phase
// leaves phaseRoleEdit some other way) or roleEditTimeout elapses. On
// timeout, it performs the same phaseRoleEdit -> phasePreflight handoff
// handleRoleCountConfirm performs, atomically, so only one of the two
// ever actually starts startPreflight.
func (cmd *WerewolfCommand) waitForRoleCountConfirm(s *discordgo.Session, channelID string, seq uint64) {
	// This runs as its own goroutine (started with a bare `go`, not through
	// startPreflight, which is the only other place in the game's phase
	// chain that has its own recover). A panic anywhere in this polling
	// loop — before it ever reaches startPreflight's guarded tail-call —
	// would otherwise be unrecovered and crash the whole bot process.
	defer func() {
		if r := recover(); r != nil {
			log.Error().
				Interface("panic", r).
				Str("channel_id", channelID).
				Msg("werewolf: recovered from panic in role-count-confirm goroutine")
		}
	}()

	deadline := time.Now().Add(roleEditTimeout)
	for {
		var stillEditing bool
		func() {
			cmd.mu.Lock()
			defer cmd.mu.Unlock()
			game := cmd.gameIfCurrent(channelID, seq)
			stillEditing = game != nil && game.Phase == phaseRoleEdit
		}()
		if !stillEditing || time.Now().After(deadline) {
			break
		}
		time.Sleep(5 * time.Second)
	}

	var joinOrder []string
	var labels map[string]string
	var timedOut bool
	func() {
		cmd.mu.Lock()
		defer cmd.mu.Unlock()
		game := cmd.gameIfCurrent(channelID, seq)
		if game == nil || game.Phase != phaseRoleEdit {
			return
		}
		timedOut = true
		joinOrder = append([]string(nil), game.JoinOrder...)
		labels = make(map[string]string, len(game.Players))
		for id, p := range game.Players {
			labels[id] = p.Label
		}
		game.Phase = phasePreflight
	}()
	if !timedOut {
		return
	}

	if _, err := s.ChannelMessageSendComplex(channelID, &discordgo.MessageSend{
		Content:         "⏱️ Hết thời gian chọn vai trò tuỳ chỉnh, dùng bảng vai trò mặc định.",
		AllowedMentions: &discordgo.MessageAllowedMentions{},
	}); err != nil {
		log.Error().Err(err).Msg("werewolf: failed to post role-edit-timeout notice")
	}
	go cmd.startPreflight(s, channelID, joinOrder, labels, seq, nil)
}

func (cmd *WerewolfCommand) handleRoleCountButton(s *discordgo.Session, i *discordgo.InteractionCreate, remainder string) {
	parts := strings.SplitN(remainder, ":", 3)
	if len(parts) != 3 {
		return
	}
	sign, roleStr, channelID := parts[0], parts[1], parts[2]
	roleInt, err := strconv.Atoi(roleStr)
	if err != nil {
		return
	}
	r := role(roleInt)

	cmd.mu.Lock()
	game, ok := cmd.games[channelID]
	if !ok || game.Phase != phaseRoleEdit {
		cmd.mu.Unlock()
		respondComponentEphemeral(s, i, "Đã hết thời gian hoặc ván đã bắt đầu.")
		return
	}
	if interactionUserID(i) != game.StarterID {
		cmd.mu.Unlock()
		respondComponentEphemeral(s, i, "Chỉ người mở bàn mới được chọn.")
		return
	}
	playerCount := len(game.JoinOrder)
	current := game.PendingRoleCounts[r]
	switch sign {
	case "inc":
		if current < roleCountMax(r, playerCount) {
			game.PendingRoleCounts[r] = current + 1
		}
	case "dec":
		if current > 0 {
			game.PendingRoleCounts[r] = current - 1
		}
	}
	counts := make(map[role]int, len(game.PendingRoleCounts))
	for k, v := range game.PendingRoleCounts {
		counts[k] = v
	}
	cmd.mu.Unlock()

	side := "wolf"
	roles := roleCountWolfSideRoles
	for _, vr := range roleCountVillagerSideRoles {
		if vr == r {
			side, roles = "villager", roleCountVillagerSideRoles
			break
		}
	}
	if err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseUpdateMessage,
		Data: &discordgo.InteractionResponseData{
			Content:    roleCountEditorContent(side, counts, playerCount),
			Components: roleCountButtonRows(channelID, roles, counts, playerCount),
		},
	}); err != nil {
		log.Error().Err(err).Msg("werewolf: failed to update role-count editor")
	}
}

func (cmd *WerewolfCommand) handleRoleCountConfirm(s *discordgo.Session, i *discordgo.InteractionCreate, channelID string) {
	cmd.mu.Lock()
	game, ok := cmd.games[channelID]
	if !ok || game.Phase != phaseRoleEdit {
		cmd.mu.Unlock()
		respondComponentEphemeral(s, i, "Đã hết thời gian hoặc ván đã bắt đầu.")
		return
	}
	if interactionUserID(i) != game.StarterID {
		cmd.mu.Unlock()
		respondComponentEphemeral(s, i, "Chỉ người mở bàn mới được xác nhận.")
		return
	}
	playerCount := len(game.JoinOrder)
	if !roleCountConfirmValid(game.PendingRoleCounts, playerCount) {
		cmd.mu.Unlock()
		respondComponentEphemeral(s, i, "Tổng số vai trò phải bằng số người chơi, và cần ít nhất 1 vai phe Sói và 1 vai phe Dân.")
		return
	}
	ratio := roleRatioFromCounts(game.PendingRoleCounts)
	joinOrder := append([]string(nil), game.JoinOrder...)
	labels := make(map[string]string, len(game.Players))
	for id, p := range game.Players {
		labels[id] = p.Label
	}
	seq := game.Seq
	game.Phase = phasePreflight
	cmd.mu.Unlock()

	respondComponentEphemeral(s, i, "✅ Đã xác nhận vai trò tuỳ chỉnh!")
	if _, err := s.ChannelMessageSendComplex(channelID, &discordgo.MessageSend{
		Content:         "🐺 Đang chia vai trò và gửi tin nhắn riêng, chờ chút...",
		AllowedMentions: &discordgo.MessageAllowedMentions{},
	}); err != nil {
		log.Error().Err(err).Msg("werewolf: failed to post custom-roles-confirmed notice")
	}
	go cmd.startPreflight(s, channelID, joinOrder, labels, seq, &ratio)
}
