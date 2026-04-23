package http

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"unicode"
	"unicode/utf8"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

const (
	// imagePathMaxBytes caps the raw URL-path length for /image/... so a truncated
	// or hostile caller cannot blow the prompt token budget.
	imagePathMaxBytes = 1024
	// imageCacheControl tells browsers the bytes at /image/<path> are immutable
	// (the prompt is the cache key -- regenerating with the same prompt yields
	// a new image, but the browser is free to cache the current bytes forever).
	imageCacheControl = "public, max-age=31536000, immutable"
)

// imageStore is the narrow filesystem-backed cache interface the image handler needs.
type imageStore interface {
	Get(hash string) ([]byte, bool, error)
	Put(hash string, data []byte) error
}

// imageGenerator is the narrow LLM interface the image handler needs.
type imageGenerator interface {
	Image(ctx context.Context, prompt string) ([]byte, error)
}

// handleImage serves on-demand generated images for the fabricated destination
// pages. The URL path after `/image/` is URL-decoded, normalised to a prompt,
// and hashed to produce the cache key. On cache miss, the handler calls
// Nano Banana inline, stores the bytes on disk, and returns them.
//
// Concurrent duplicate requests are harmless: both callers generate, both
// rename their temp file onto the final path (os.Rename silently overwrites),
// and both serve their own bytes to their caller.
func handleImage(log *slog.Logger, store imageStore, gen imageGenerator) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		span := trace.SpanFromContext(ctx)

		rawPath := strings.TrimPrefix(r.URL.Path, "/image/")

		span.SetAttributes(attribute.Int("image.path_len", len(rawPath)))

		if len(rawPath) > imagePathMaxBytes {
			http.Error(w, "image path too long", http.StatusRequestURITooLong)
			return
		}

		prompt, ok := imagePathToPrompt(rawPath)
		if !ok {
			http.Error(w, "invalid image path", http.StatusBadRequest)
			return
		}

		sum := sha256.Sum256([]byte(prompt))
		pathHash := hex.EncodeToString(sum[:])
		span.SetAttributes(attribute.String("image.path_hash", pathHash))

		data, found, err := store.Get(pathHash)
		if err != nil {
			log.Error("Error getting image", "error", err, "path_hash", pathHash)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		if found {
			span.SetAttributes(
				attribute.Bool("image.cached", true),
				attribute.Int("image.bytes", len(data)),
				attribute.String("image.mime", sniffImageMime(data)),
			)
			writeImage(w, data)
			return
		}

		// llm.Image owns its own 60s budget; we pass the request context as-is
		// so the generation aborts promptly when the client disconnects.
		data, err = gen.Image(ctx, prompt)
		if err != nil {
			log.Error("Error generating image", "error", err, "prompt", prompt)
			http.Error(w, "image generation failed", http.StatusBadGateway)
			return
		}

		if err := store.Put(pathHash, data); err != nil {
			// Cache write failed, but we have valid bytes; serve them and move on.
			log.Error("Error storing image", "error", err, "path_hash", pathHash)
		}

		span.SetAttributes(
			attribute.Bool("image.cached", false),
			attribute.Int("image.bytes", len(data)),
			attribute.String("image.mime", sniffImageMime(data)),
		)
		writeImage(w, data)
	}
}

func writeImage(w http.ResponseWriter, data []byte) {
	w.Header().Set("Content-Type", sniffImageMime(data))
	w.Header().Set("Cache-Control", imageCacheControl)
	_, _ = w.Write(data)
}

// sniffImageMime returns the Content-Type for the given image bytes by looking
// at the leading magic bytes. Nano Banana v1 returns PNG, v2 returns JPEG; we
// label each correctly so downloads and strict user-agents see a sensible
// Content-Type. Unknown shapes fall back to image/png and let the browser
// sniff-correct.
func sniffImageMime(data []byte) string {
	if len(data) >= 8 && data[0] == 0x89 && data[1] == 0x50 && data[2] == 0x4E && data[3] == 0x47 &&
		data[4] == 0x0D && data[5] == 0x0A && data[6] == 0x1A && data[7] == 0x0A {
		return "image/png"
	}
	if len(data) >= 3 && data[0] == 0xFF && data[1] == 0xD8 && data[2] == 0xFF {
		return "image/jpeg"
	}
	if len(data) >= 12 && data[0] == 'R' && data[1] == 'I' && data[2] == 'F' && data[3] == 'F' &&
		data[8] == 'W' && data[9] == 'E' && data[10] == 'B' && data[11] == 'P' {
		return "image/webp"
	}
	return "image/png"
}

// imagePathToPrompt URL-decodes the raw /image/ path, normalises it into a
// prompt, and returns the result. The normalised prompt doubles as the
// Nano Banana prompt and the cache key input: `-` and `/` become spaces,
// internal whitespace collapses, and the whole string is lowercased.
//
// Returns ("", false) if the path cannot be decoded, contains non-UTF-8
// runes, or normalises to empty. This is the exported-for-tests boundary
// between "a path made it to the handler" and "there's a prompt worth
// hashing".
func imagePathToPrompt(raw string) (string, bool) {
	decoded, err := url.PathUnescape(raw)
	if err != nil {
		return "", false
	}
	if !utf8.ValidString(decoded) {
		return "", false
	}
	// Separator runes become spaces so hyphens, slashes, and other punctuation
	// collapse into whitespace for downstream handling.
	var b strings.Builder
	b.Grow(len(decoded))
	for _, r := range decoded {
		switch r {
		case '-', '/':
			b.WriteByte(' ')
		default:
			b.WriteRune(r)
		}
	}
	// Lowercase + collapse internal whitespace + trim ends.
	s := strings.ToLower(b.String())
	var out strings.Builder
	out.Grow(len(s))
	var prevSpace bool
	for _, r := range s {
		if unicode.IsSpace(r) {
			if !prevSpace && out.Len() > 0 {
				out.WriteByte(' ')
			}
			prevSpace = true
			continue
		}
		out.WriteRune(r)
		prevSpace = false
	}
	result := strings.TrimRight(out.String(), " ")
	if result == "" {
		return "", false
	}
	return result, true
}
