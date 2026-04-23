package llm

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
)

// hashRe matches the canonical sha256 hex representation produced by the
// /image handler: exactly 64 lowercase hex characters. Anything else is
// rejected by the store before touching the filesystem so a malformed (or
// hostile) hash cannot escape the sharded layout.
var hashRe = regexp.MustCompile(`^[0-9a-f]{64}$`)

// ImageStore persists generated Nano Banana image bytes on disk, sharded by
// the first four hex characters of the sha256 prompt hash so no single leaf
// directory accumulates more than a few hundred entries at a million-image
// scale.
//
// The layout is {root}/{hash[0:2]}/{hash[2:4]}/{hash[4:]}.png. All images are
// stored as `.png`: Nano Banana returns PNG by default, and browsers
// sniff-correct the rare JPEG that might slip through.
type ImageStore struct {
	root string
}

// NewImageStore returns an [ImageStore] rooted at the given directory. The
// caller is expected to have created the directory (e.g. via os.MkdirAll at
// startup); Put will create the per-shard subdirectories on demand.
func NewImageStore(root string) *ImageStore {
	return &ImageStore{root: root}
}

// Path returns the on-disk path for the given hash, or an empty string if
// the hash fails validation. Intended for logging and span attributes -- the
// file itself may or may not exist.
func (s *ImageStore) Path(hash string) string {
	if !hashRe.MatchString(hash) {
		return ""
	}
	return filepath.Join(s.root, hash[0:2], hash[2:4], hash[4:]+".png")
}

// Get reads the image bytes for the given hash. It returns (data, true, nil)
// on success, (nil, false, nil) if no file exists for the hash, and
// (nil, false, err) for a malformed hash or a real I/O error.
func (s *ImageStore) Get(hash string) ([]byte, bool, error) {
	if !hashRe.MatchString(hash) {
		return nil, false, fmt.Errorf("invalid hash: %q", hash)
	}

	data, err := os.ReadFile(s.Path(hash))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("read image: %w", err)
	}
	return data, true, nil
}

// Put writes the given bytes to the image path for the hash, creating any
// missing shard directories. The write is atomic: bytes are written to a
// temp file in the same directory, then os.Rename'd onto the final path.
// Concurrent writers for the same hash each produce their own temp file and
// whichever renames last wins -- os.Rename silently overwrites, which is
// what we want for a deterministic-ish cache key.
func (s *ImageStore) Put(hash string, data []byte) error {
	if !hashRe.MatchString(hash) {
		return fmt.Errorf("invalid hash: %q", hash)
	}

	final := s.Path(hash)
	dir := filepath.Dir(final)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("mkdir shard: %w", err)
	}

	// 8 random bytes is plenty to avoid collisions between concurrent writers
	// for the same hash. crypto/rand so we don't have to seed anything.
	var suffix [8]byte
	if _, err := rand.Read(suffix[:]); err != nil {
		return fmt.Errorf("random suffix: %w", err)
	}
	tmp := final + ".tmp-" + hex.EncodeToString(suffix[:])

	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		// WriteFile may have created a partial file before failing (e.g. disk full).
		// Best-effort cleanup so shard dirs don't accumulate orphaned tmp files.
		_ = os.Remove(tmp)
		return fmt.Errorf("write temp file: %w", err)
	}

	if err := os.Rename(tmp, final); err != nil {
		// Best-effort cleanup; the rename failed, so the temp file may still be there.
		_ = os.Remove(tmp)
		return fmt.Errorf("rename into place: %w", err)
	}
	return nil
}
