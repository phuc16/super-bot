package commands

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestDownloadAttachment covers the fetch that makes custom-command images
// survive at all. It runs once, at /addcom time, against a signed Discord
// URL that expires within hours and cannot be re-issued — so anything it
// gets wrong is unrecoverable, not retryable.
func TestDownloadAttachment(t *testing.T) {
	serve := func(status int, body []byte) *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(status)
			_, _ = w.Write(body)
		}))
	}

	t.Run("returns the bytes unchanged", func(t *testing.T) {
		want := []byte("\x89PNG\r\n\x1a\n some image bytes")
		srv := serve(http.StatusOK, want)
		defer srv.Close()

		got, err := downloadAttachment(srv.URL, maxCustomCommandImageBytes)
		if err != nil {
			t.Fatalf("downloadAttachment: %v", err)
		}
		if !bytes.Equal(got, want) {
			t.Errorf("got %q, want %q", got, want)
		}
	})

	t.Run("a file at exactly the limit is accepted", func(t *testing.T) {
		// Off-by-one here would reject valid uploads, so pin the boundary.
		const limit = 1024
		srv := serve(http.StatusOK, bytes.Repeat([]byte("a"), limit))
		defer srv.Close()

		got, err := downloadAttachment(srv.URL, limit)
		if err != nil {
			t.Fatalf("a file of exactly %d bytes was rejected: %v", limit, err)
		}
		if len(got) != limit {
			t.Errorf("got %d bytes, want %d", len(got), limit)
		}
	})

	t.Run("an oversized file is rejected, never truncated", func(t *testing.T) {
		// The dangerous failure: storing a half-read image that renders as
		// a broken thumbnail forever, with nothing to indicate why.
		const limit = 1024
		srv := serve(http.StatusOK, bytes.Repeat([]byte("a"), limit*4))
		defer srv.Close()

		got, err := downloadAttachment(srv.URL, limit)
		if err == nil {
			t.Fatalf("oversized file accepted, returned %d bytes", len(got))
		}
		if got != nil {
			t.Errorf("returned %d bytes alongside the error — a truncated image must never reach the caller", len(got))
		}
		if !strings.Contains(err.Error(), "MB") {
			t.Errorf("error %q does not tell the user it was a size problem", err)
		}
	})

	t.Run("a non-200 response is an error", func(t *testing.T) {
		// The expected shape once a signed URL lapses: Discord serves a
		// non-200 with a short body, which must not be stored as an image.
		srv := serve(http.StatusNotFound, []byte("This content is no longer available."))
		defer srv.Close()

		if _, err := downloadAttachment(srv.URL, maxCustomCommandImageBytes); err == nil {
			t.Error("a 404 body was accepted as image data")
		}
	})

	t.Run("an empty body is an error", func(t *testing.T) {
		srv := serve(http.StatusOK, nil)
		defer srv.Close()

		if _, err := downloadAttachment(srv.URL, maxCustomCommandImageBytes); err == nil {
			t.Error("an empty response was accepted — the command would store a zero-byte image")
		}
	})

	t.Run("an unreachable host is an error, not a panic", func(t *testing.T) {
		srv := serve(http.StatusOK, []byte("x"))
		url := srv.URL
		srv.Close()

		if _, err := downloadAttachment(url, maxCustomCommandImageBytes); err == nil {
			t.Error("a dead host was accepted")
		}
	})

	t.Run("the cap leaves room inside Discord's own limit", func(t *testing.T) {
		// Discord accepts 20 MiB by default. Staying under it means an
		// upload the user could make is never rejected by us for a reason
		// they cannot see, and the re-upload on each use always fits.
		const discordDefaultLimit = 20 << 20
		if maxCustomCommandImageBytes > discordDefaultLimit {
			t.Errorf("maxCustomCommandImageBytes (%d) exceeds Discord's default %d — stored images could be unpostable",
				maxCustomCommandImageBytes, discordDefaultLimit)
		}
		if maxCustomCommandImageBytes <= 0 {
			t.Error("maxCustomCommandImageBytes must be positive")
		}
	})
}
