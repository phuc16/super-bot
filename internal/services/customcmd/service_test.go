package customcmd

import (
	"strings"
	"testing"

	"github.com/phuc16/VCG-super-bot/internal/models"
)

// TestListColumnsExcludeImageData guards the one performance trap in
// storing images as blobs: /listcom never displays an image, so selecting
// image_data in a listing would pull every stored image in the guild across
// the wire and into memory to render a list of trigger words.
//
// This is a column-name list, so nothing but a test catches it drifting —
// a bare Find() would silently start loading blobs again.
func TestListColumnsExcludeImageData(t *testing.T) {
	for _, col := range listColumns {
		if strings.Contains(col, "image_data") {
			t.Errorf("listColumns includes %q — listings would load every image in the guild", col)
		}
	}
	if len(listColumns) == 0 {
		t.Fatal("listColumns is empty — Select() with no columns falls back to selecting everything")
	}

	// Every column the listing path actually reads must be present, or rows
	// come back with zero-valued fields instead of their contents.
	for _, required := range []string{"id", "guild_id", "`trigger`", "response", "created_by"} {
		found := false
		for _, col := range listColumns {
			if col == required {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("listColumns is missing %q, which the listing renders", required)
		}
	}

	// Soft-delete columns matter too: List filters on deleted_at, and GORM
	// needs the model's fields populated consistently.
	for _, required := range []string{"created_at", "deleted_at"} {
		found := false
		for _, col := range listColumns {
			if col == required {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("listColumns is missing %q", required)
		}
	}
}

// TestHasImageIgnoresLegacyURLRows pins the deliberate decision to treat
// pre-migration rows as imageless. Those rows hold a Discord
// /ephemeral-attachments/ link that expired within hours of being written
// and cannot be refreshed, so honouring it would post an embed that renders
// as an empty box — indistinguishable, to a player, from the bot being
// broken.
func TestHasImageIgnoresLegacyURLRows(t *testing.T) {
	dead := "https://cdn.discordapp.com/ephemeral-attachments/1/2/x.png?ex=6a9f814a&is=6a9&hm=deadbeef"

	legacy := &models.CustomCommand{ImageURL: &dead}
	if legacy.HasImage() {
		t.Error("a row with only an expired ImageURL reports HasImage — the reply would embed a dead link")
	}

	migrated := &models.CustomCommand{ImageURL: &dead, ImageData: []byte{0x89, 'P', 'N', 'G'}}
	if !migrated.HasImage() {
		t.Error("a row with real bytes reports no image")
	}

	empty := &models.CustomCommand{ImageData: []byte{}}
	if empty.HasImage() {
		t.Error("zero-length ImageData reports as an image")
	}
}

// TestAttachmentNameAlwaysUsable covers the filename used for both the
// upload and the attachment:// reference in the embed. An empty name breaks
// the reference and the image silently fails to render.
func TestAttachmentNameAlwaysUsable(t *testing.T) {
	original := "Screenshot_2026-07-24-20-18-03.png"
	named := &models.CustomCommand{ImageName: &original}
	if got := named.AttachmentName(); got != original {
		t.Errorf("AttachmentName() = %q, want the stored filename %q", got, original)
	}

	blank := ""
	for _, c := range []*models.CustomCommand{
		{},
		{ImageName: &blank},
	} {
		got := c.AttachmentName()
		if got == "" {
			t.Error("AttachmentName() is empty — attachment:// would reference nothing")
		}
		if !strings.Contains(got, ".") {
			t.Errorf("AttachmentName() = %q has no extension; Discord infers content type from it", got)
		}
	}
}
