package http

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"app/model"
)

const (
	// imagePathMaxBytes caps the raw URL-path length for /image/... so a truncated
	// or hostile caller cannot blow the prompt token budget or the SQLite column.
	imagePathMaxBytes = 1024
	// imageGenTimeout is the total budget for a single cache-miss generation,
	// including the model call. The llm.Client enforces its own 60s internal
	// timeout; we mirror it at the handler level so slow generations cannot
	// pin the handler open.
	imageGenTimeout = 60 * time.Second
	// imageCacheControl tells browsers the bytes at /image/<path> are immutable
	// (the prompt is the cache key -- regenerating with the same prompt yields
	// a new image, but the browser is free to cache the current bytes forever).
	imageCacheControl = "public, max-age=31536000, immutable"
)

// imageService is the narrow persistence interface the image handler needs.
type imageService interface {
	GetImage(ctx context.Context, pathHash string) (model.Image, error)
	InsertImage(ctx context.Context, img model.Image) error
}

// imageGenerator is the narrow LLM interface the image handler needs.
type imageGenerator interface {
	Image(ctx context.Context, prompt string) ([]byte, string, error)
}

// handleImage serves on-demand generated images for the fabricated destination
// pages. The URL path after `/image/` is URL-decoded, normalised to a prompt,
// and hashed to produce the cache key. On cache miss, the handler calls
// Nano Banana inline, stores the bytes in SQLite, and returns them.
//
// Concurrent duplicate requests are harmless: both callers generate, one wins
// the `on conflict (path_hash) do nothing` insert, the other's bytes are
// discarded (but still served to that caller).
func handleImage(log *slog.Logger, svc imageService, gen imageGenerator) http.HandlerFunc {
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

		if img, err := svc.GetImage(ctx, pathHash); err == nil {
			span.SetAttributes(
				attribute.Bool("image.cached", true),
				attribute.Int("image.bytes", len(img.Data)),
				attribute.String("image.mime", img.MimeType),
			)
			writeImage(w, img.MimeType, img.Data)
			return
		} else if !errors.Is(err, model.ErrorImageNotFound) {
			log.Error("Error getting image", "error", err, "path_hash", pathHash)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}

		genCtx, cancel := context.WithTimeout(ctx, imageGenTimeout)
		defer cancel()

		data, mime, err := gen.Image(genCtx, prompt)
		if err != nil {
			log.Error("Error generating image", "error", err, "prompt", prompt)
			http.Error(w, "image generation failed", http.StatusBadGateway)
			return
		}

		if err := svc.InsertImage(ctx, model.Image{
			PathHash: pathHash,
			Path:     prompt,
			MimeType: mime,
			Data:     data,
		}); err != nil {
			// Cache write failed, but we have valid bytes; serve them and move on.
			log.Error("Error inserting image", "error", err, "path_hash", pathHash)
		}

		span.SetAttributes(
			attribute.Bool("image.cached", false),
			attribute.Int("image.bytes", len(data)),
			attribute.String("image.mime", mime),
		)
		writeImage(w, mime, data)
	}
}

func writeImage(w http.ResponseWriter, mime string, data []byte) {
	w.Header().Set("Content-Type", mime)
	w.Header().Set("Cache-Control", imageCacheControl)
	_, _ = w.Write(data)
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
