package commands

import (
	"testing"

	"github.com/bwmarrin/discordgo"
)

// Discord's documented embed limits. Discord counts CODE POINTS, not bytes,
// which matters a great deal here: the Vietnamese help text averages well
// over one byte per character, so a byte-based check would fire spuriously
// long before a real limit was reached.
const (
	maxEmbedFieldNameRunes  = 256
	maxEmbedFieldValueRunes = 1024
	maxEmbedFields          = 25
	maxEmbedTotalRunes      = 6000
)

// TestHelpEmbedsWithinDiscordLimits guards every /help embed against
// Discord's size limits, in both languages.
//
// This is a permanent test because the project has repeatedly shipped, or
// nearly shipped, embeds that blew a limit: one help revision exceeded the
// 6000-rune total and was rejected outright in production
// (MAX_EMBED_SIZE_EXCEEDED), and several later text edits landed within
// ~50 runes of the 1024 per-field cap. A failure here is not cosmetic —
// Discord rejects the entire message, and the werewolf help's followup send
// only logs the error, so /help would silently deliver a partial response
// with nothing surfaced to the user.
func TestHelpEmbedsWithinDiscordLimits(t *testing.T) {
	cmd := &HelpCommand{}

	// Every command name getCommandHelp knows how to render, plus
	// "werewolf", which routes to its own multi-embed builder.
	commandNames := []string{
		"werewolf", "export", "topic", "custom", "roast",
		"duel", "duel-leaderboard", "duel-history", "duel-reset", "duel-challenge-all",
	}

	for _, lang := range []string{"vi", "en"} {
		t.Run(lang, func(t *testing.T) {
			checkHelpEmbeds(t, lang, "general", []*discordgo.MessageEmbed{cmd.getGeneralHelp(lang)})
			for _, name := range commandNames {
				embeds := cmd.getCommandHelp(name, lang)
				if len(embeds) == 0 {
					t.Errorf("%s/%s: getCommandHelp returned no embeds", lang, name)
					continue
				}
				checkHelpEmbeds(t, lang, name, embeds)
			}
		})
	}
}

func checkHelpEmbeds(t *testing.T, lang, name string, embeds []*discordgo.MessageEmbed) {
	t.Helper()
	for idx, e := range embeds {
		if e == nil {
			t.Errorf("%s/%s embed %d is nil", lang, name, idx)
			continue
		}
		total := len([]rune(e.Title)) + len([]rune(e.Description))
		if n := len(e.Fields); n > maxEmbedFields {
			t.Errorf("%s/%s embed %d: %d fields exceeds Discord's max of %d", lang, name, idx, n, maxEmbedFields)
		}
		for _, f := range e.Fields {
			if n := len([]rune(f.Name)); n > maxEmbedFieldNameRunes {
				t.Errorf("%s/%s embed %d: field name is %d runes, exceeds %d — %q",
					lang, name, idx, n, maxEmbedFieldNameRunes, f.Name)
			}
			if n := len([]rune(f.Value)); n > maxEmbedFieldValueRunes {
				t.Errorf("%s/%s embed %d: field %q value is %d runes, exceeds %d — trim it",
					lang, name, idx, f.Name, n, maxEmbedFieldValueRunes)
			}
			total += len([]rune(f.Name)) + len([]rune(f.Value))
		}
		// Each embed is checked on its own budget because the werewolf help
		// deliberately ships its two embeds as SEPARATE messages (an
		// interaction response plus a followup), so each gets its own 6000.
		// If that send strategy ever reverts to one message, this check must
		// change with it — Discord applies the 6000 cap to the SUM of all
		// embeds in a single message, which is exactly the production
		// failure that motivated splitting them in the first place.
		if total > maxEmbedTotalRunes {
			t.Errorf("%s/%s embed %d: total is %d runes, exceeds %d", lang, name, idx, total, maxEmbedTotalRunes)
		}
	}
}
