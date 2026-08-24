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
	return wolfSide+villagerSide == playerCount && wolfSide >= 1 && villagerSide >= 1 &&
		wolfVoteParticipants >= 1 && wolfFactionBalanceOK(counts, playerCount)
}

// wolfFactionBalanceOK enforces the balance floor: checkWinCondition
// declares a wolf win the instant aliveWolves >= aliveOthers, so a setup
// that can reach that bar before the village ever gets a day phase is an
// unplayable instant loss and must be rejected at confirm time.
//
// The required margin depends on the worst single night-1 event the setup
// actually allows:
//
//   - With no Cursed Wolf, the worst case is one plain kill: aliveOthers
//     drops by 1, a 1-point swing. Surviving it needs wolfFaction*2+1 <
//     playerCount.
//   - With at least one Cursed Wolf, a bite can instead CONVERT, which
//     moves a player from aliveOthers to aliveWolves — a 2-point swing in
//     one step. Surviving that needs wolfFaction*2+2 < playerCount.
//
// Applying the +2 margin unconditionally (as an earlier revision did)
// wrongly rejected ordinary no-Cursed-Wolf setups such as 2 wolves at 6
// players, which are perfectly playable: night 1 leaves 2 vs 3 with a full
// day of discussion and a vote.
//
// wolfFaction counts wolf-FACTION membership (matching factionForRole),
// not the editor's wolfSide grouping: Cursed Wolf is displayed on the wolf
// side but deals as villager-faction until it converts, so it must not be
// counted as a wolf here — its danger is expressed through the larger
// margin instead.
func wolfFactionBalanceOK(counts map[role]int, playerCount int) bool {
	wolfFaction := counts[roleWerewolf] + counts[roleWolfCub] + counts[roleTraitor]
	margin := 1
	if counts[roleCursedWolf] > 0 {
		margin = 2
	}
	return wolfFaction*2+margin < playerCount
}

// roleCountConfirmError returns a specific, accurate explanation of which
// of roleCountConfirmValid's conditions the starter's current counts fail
// — checked in the same order they're evaluated there. Without this, the
// confirm button had one fixed message describing only the total/both-
// sides conditions, which actively misdescribed a rejection caused by the
// wolf-vote-participant rule or the wolf-faction balance floor (both added
// after that message was first written), leaving the starter no way to
// diagnose a setup that already looked correct on screen.
//
// Returns "" when counts are in fact valid, so the empty string is a safe
// validity test on its own — the leading case below makes that true rather
// than relying on every caller to pre-check roleCountConfirmValid.
func roleCountConfirmError(counts map[role]int, playerCount int) string {
	if roleCountConfirmValid(counts, playerCount) {
		return ""
	}
	wolfSide, villagerSide := 0, 0
	for _, r := range roleCountWolfSideRoles {
		wolfSide += counts[r]
	}
	for _, r := range roleCountVillagerSideRoles {
		villagerSide += counts[r]
	}
	total := wolfSide + villagerSide
	switch {
	case total != playerCount:
		return fmt.Sprintf("Tổng số vai trò hiện tại là %d, cần đúng bằng %d người chơi.", total, playerCount)
	case wolfSide < 1:
		return "Cần ít nhất 1 vai phe Sói."
	case villagerSide < 1:
		return "Cần ít nhất 1 vai phe Dân."
	case counts[roleWerewolf]+counts[roleWolfCub] < 1:
		return "Cần ít nhất 1 Ma Sói hoặc Sói Con để bầy Sói có người bỏ phiếu cắn — Sói Nguyền và Kẻ Phản Bội một mình không đủ."
	default:
		// Deliberately does NOT suggest reducing Sói Nguyền: Cursed Wolf
		// is excluded from the wolfFaction count in wolfFactionBalanceOK,
		// so cutting it can never clear this rejection — naming it here
		// would send the starter into a loop applying a remedy that does
		// nothing, the same defect class this whole function exists to fix.
		// (Removing every Cursed Wolf does shrink the required margin from
		// 2 to 1, which can help at the boundary, so it's mentioned as a
		// distinct secondary option rather than as a count to reduce.)
		msg := "Phe Sói hiện quá mạnh so với số người chơi: chỉ cần 1 đêm là phe Sói đã có thể thắng ngay, không kịp có ngày thảo luận nào. Hãy giảm bớt Ma Sói/Sói Con/Kẻ Phản Bội, hoặc tăng số người phe Dân."
		if counts[roleCursedWolf] > 0 {
			msg += " (Bỏ hết Sói Nguyền cũng nới được yêu cầu này một chút, vì khi đó không còn nguy cơ bị biến hình ngay đêm đầu.)"
		}
		return msg
	}
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
// per-role counts and that side's own subtotal.
//
// It deliberately renders NO cross-side data. The editor is two separate
// ephemeral messages, and only the one just clicked gets re-rendered
// (handleRoleCountButton uses InteractionResponseUpdateMessage), so any
// figure computed across BOTH sides — a combined total, a whole-game
// balance score — goes stale on the message that wasn't clicked and can
// sit visibly wrong for the entire roleEditTimeout. That's worse than not
// showing it: a starter reading "Tổng: 17/20" on the wolf message after
// having already filled the villager side to 20 will "fix" a total that
// was never wrong.
//
// Keeping both messages in sync would need the original interaction token
// stored on the game so the other message could be edited via
// FollowupMessageEdit — real complexity for a running total. Instead the
// authoritative combined figures are surfaced where they can only ever be
// computed fresh: roleCountConfirmError reports the exact live total when
// it doesn't match, and handleRoleCountConfirm reports it on success.
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

	// This side only — by construction it can only be changed by this same
	// message's own buttons, so it can never be stale.
	sideTotal := 0
	for _, r := range roles {
		sideTotal += counts[r]
	}

	var b strings.Builder
	fmt.Fprintf(&b, "**%s** — chọn số lượng cho mỗi vai trò:\n", title)
	for _, r := range roles {
		fmt.Fprintf(&b, "%s: %d\n", r.label(), counts[r])
	}
	fmt.Fprintf(&b, "\nPhe này: %d vai · Cần tổng cộng %d người (bấm ✅ Xác nhận để kiểm tra tổng)", sideTotal, playerCount)
	return b.String()
}

// roleCountButtonRows builds one editor message's component rows: the
// given roles' -/+ buttons, two roles per row, plus a trailing confirm
// row. customID encodes the sign ("inc"/"dec"), the role's int value, the
// channel ID, and the game's Seq, colon-separated after the
// werewolfRoleCountID prefix — this is this codebase's first customID
// needing more than one encoded field, so it's parsed with
// strings.SplitN rather than the simple TrimPrefix-only pattern every
// other werewolf customID uses.
//
// The Seq field is load-bearing, not decoration. These editors are
// EPHEMERAL followups that stay interactive for the interaction token's
// full ~15-minute life, and cancelling during phaseRoleEdit is explicitly
// allowed. So a starter can cancel game A and immediately start game B in
// the same channel while A's editor buttons are still clickable — at
// which point channelID, phase (phaseRoleEdit) and StarterID are all
// byte-identical between the two games, and every cheap guard passes.
// Seq is the only thing that distinguishes them, so the handlers resolve
// via gameIfCurrent(channelID, seq) rather than a bare map lookup.
// Without it, a click on A's stale confirm button deals game B with
// whatever counts were left in A.
//
// Length budget: prefix 13 + sign 3 + role ≤2 + snowflake 19 + seq + 3
// colons ≈ 45 chars, far under Discord's 100-char customID cap.
func roleCountButtonRows(channelID string, seq uint64, roles []role, counts map[role]int, playerCount int) []discordgo.MessageComponent {
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
					CustomID: fmt.Sprintf("%sdec:%d:%s:%d", werewolfRoleCountID, int(r), channelID, seq),
					Disabled: current <= 0,
				},
				discordgo.Button{
					Label:    "➕ " + r.label(),
					Style:    discordgo.SecondaryButton,
					CustomID: fmt.Sprintf("%sinc:%d:%s:%d", werewolfRoleCountID, int(r), channelID, seq),
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
				CustomID: fmt.Sprintf("%s%s:%d", werewolfRoleCountConfirmID, channelID, seq),
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
		Components: roleCountButtonRows(channelID, seq, roleCountWolfSideRoles, counts, playerCount),
		Flags:      discordgo.MessageFlagsEphemeral,
	}); err != nil {
		log.Error().Err(err).Msg("werewolf: failed to send wolf-side role-count editor")
	}
	if _, err := s.FollowupMessageCreate(interaction, true, &discordgo.WebhookParams{
		Content:    roleCountEditorContent("villager", counts, playerCount),
		Components: roleCountButtonRows(channelID, seq, roleCountVillagerSideRoles, counts, playerCount),
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
	// A customID that doesn't parse is almost always a button rendered by
	// an OLDER build (the encoding gained a Seq field), still sitting in a
	// user's client. Answer the interaction instead of returning silently:
	// a bare return leaves Discord showing "This interaction failed", which
	// looks like a bot outage rather than a stale button.
	parts := strings.SplitN(remainder, ":", 4)
	if len(parts) != 4 {
		respondComponentEphemeral(s, i, "Nút này thuộc phiên bản cũ, không dùng được nữa. Hãy mở lại ván mới.")
		return
	}
	sign, roleStr, channelID, seqStr := parts[0], parts[1], parts[2], parts[3]
	roleInt, err := strconv.Atoi(roleStr)
	if err != nil {
		respondComponentEphemeral(s, i, "Nút này không hợp lệ, hãy mở lại ván mới.")
		return
	}
	seq, err := strconv.ParseUint(seqStr, 10, 64)
	if err != nil {
		respondComponentEphemeral(s, i, "Nút này thuộc phiên bản cũ, không dùng được nữa. Hãy mở lại ván mới.")
		return
	}
	r := role(roleInt)

	cmd.mu.Lock()
	// gameIfCurrent, not a bare map lookup: an ephemeral editor from a
	// cancelled game stays clickable, and channelID/phase/StarterID are
	// all identical if the same starter immediately opens a new game in
	// the same channel — Seq is the only thing that tells them apart.
	game := cmd.gameIfCurrent(channelID, seq)
	if game == nil || game.Phase != phaseRoleEdit {
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
			Components: roleCountButtonRows(channelID, seq, roles, counts, playerCount),
		},
	}); err != nil {
		log.Error().Err(err).Msg("werewolf: failed to update role-count editor")
	}
}

func (cmd *WerewolfCommand) handleRoleCountConfirm(s *discordgo.Session, i *discordgo.InteractionCreate, remainder string) {
	// See handleRoleCountButton: a confirm button from an older build
	// carries no Seq, so answer the interaction rather than returning
	// silently and leaving Discord's "This interaction failed" toast.
	channelID, seqStr, ok := strings.Cut(remainder, ":")
	if !ok {
		respondComponentEphemeral(s, i, "Nút này thuộc phiên bản cũ, không dùng được nữa. Hãy mở lại ván mới.")
		return
	}
	seq, err := strconv.ParseUint(seqStr, 10, 64)
	if err != nil {
		respondComponentEphemeral(s, i, "Nút này thuộc phiên bản cũ, không dùng được nữa. Hãy mở lại ván mới.")
		return
	}

	cmd.mu.Lock()
	// gameIfCurrent, not a bare map lookup — see handleRoleCountButton for
	// why the Seq in the customID is load-bearing here. Without it, a
	// confirm click on a CANCELLED game's still-live ephemeral editor
	// would deal whichever game now occupies this channel, using counts
	// its own starter never chose.
	game := cmd.gameIfCurrent(channelID, seq)
	if game == nil || game.Phase != phaseRoleEdit {
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
		errMsg := roleCountConfirmError(game.PendingRoleCounts, playerCount)
		cmd.mu.Unlock()
		respondComponentEphemeral(s, i, errMsg)
		return
	}
	ratio := roleRatioFromCounts(game.PendingRoleCounts)
	joinOrder := append([]string(nil), game.JoinOrder...)
	labels := make(map[string]string, len(game.Players))
	for id, p := range game.Players {
		labels[id] = p.Label
	}
	// Snapshot the balance score under the lock, for the confirmation
	// message below: the per-side editor messages deliberately no longer
	// render whole-game figures (they'd go stale — see
	// roleCountEditorContent), so this is where the starter gets the
	// authoritative combined readout, computed fresh from live counts.
	score := balanceScore(game.PendingRoleCounts)
	game.Phase = phasePreflight
	cmd.mu.Unlock()

	respondComponentEphemeral(s, i, fmt.Sprintf(
		"✅ Đã xác nhận vai trò tuỳ chỉnh! Tổng %d người · Điểm cân bằng: %d", playerCount, score))
	if _, err := s.ChannelMessageSendComplex(channelID, &discordgo.MessageSend{
		Content:         "🐺 Đang chia vai trò và gửi tin nhắn riêng, chờ chút...",
		AllowedMentions: &discordgo.MessageAllowedMentions{},
	}); err != nil {
		log.Error().Err(err).Msg("werewolf: failed to post custom-roles-confirmed notice")
	}
	go cmd.startPreflight(s, channelID, joinOrder, labels, seq, &ratio)
}
