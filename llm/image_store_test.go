package llm_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"maragu.dev/is"

	"app/llm"
)

// validHash is a 64-character lowercase hex sha256-shaped hash used across the table tests.
const validHash = "abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789"

func TestImageStore(t *testing.T) {
	t.Run("Get on missing file returns (nil, false, nil)", func(t *testing.T) {
		root := t.TempDir()
		s := llm.NewImageStore(root)

		data, found, err := s.Get(validHash)
		is.NotError(t, err)
		is.True(t, !found)
		is.True(t, data == nil)
	})

	t.Run("Put then Get round-trips identical bytes", func(t *testing.T) {
		root := t.TempDir()
		s := llm.NewImageStore(root)

		want := []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A, 0x00, 0x01, 0x02, 0x03}
		err := s.Put(validHash, want)
		is.NotError(t, err)

		got, found, err := s.Get(validHash)
		is.NotError(t, err)
		is.True(t, found)
		is.Equal(t, len(want), len(got))
		for i := range want {
			is.Equal(t, want[i], got[i])
		}
	})

	t.Run("Put over an existing file overwrites cleanly", func(t *testing.T) {
		root := t.TempDir()
		s := llm.NewImageStore(root)

		is.NotError(t, s.Put(validHash, []byte{0x01, 0x02}))
		is.NotError(t, s.Put(validHash, []byte{0x03, 0x04, 0x05}))

		got, found, err := s.Get(validHash)
		is.NotError(t, err)
		is.True(t, found)
		is.Equal(t, 3, len(got))
		is.Equal(t, byte(0x03), got[0])
		is.Equal(t, byte(0x04), got[1])
		is.Equal(t, byte(0x05), got[2])
	})

	t.Run("on-disk path matches the expected shard layout", func(t *testing.T) {
		root := t.TempDir()
		s := llm.NewImageStore(root)

		is.NotError(t, s.Put(validHash, []byte{0xFF}))

		want := filepath.Join(root, "ab", "cd", "ef0123456789abcdef0123456789abcdef0123456789abcdef0123456789.png")
		is.Equal(t, want, s.Path(validHash))

		info, err := os.Stat(want)
		is.NotError(t, err)
		is.True(t, !info.IsDir())

		// No temp files left behind.
		entries, err := os.ReadDir(filepath.Join(root, "ab", "cd"))
		is.NotError(t, err)
		is.Equal(t, 1, len(entries))
		is.Equal(t, false, strings.Contains(entries[0].Name(), ".tmp-"))
	})

	t.Run("bad hash is rejected by Get and Put", func(t *testing.T) {
		root := t.TempDir()
		s := llm.NewImageStore(root)

		bad := []string{
			"", // empty
			"ABCDEF0123456789abcdef0123456789abcdef0123456789abcdef0123456789ab", // uppercase + too long
			"abcdef0123456789abcdef0123456789abcdef0123456789abcdef012345678",    // 63 chars
			"abcdef0123456789abcdef0123456789abcdef0123456789abcdef01234567890",  // 65 chars
			"gggggg0123456789abcdef0123456789abcdef0123456789abcdef0123456789",   // non-hex
			"../../etc/passwd", // path traversal
			"ABCDEF0123456789ABCDEF0123456789ABCDEF0123456789ABCDEF0123456789", // all uppercase
		}
		for _, h := range bad {
			_, _, err := s.Get(h)
			is.True(t, err != nil)

			err = s.Put(h, []byte{0x00})
			is.True(t, err != nil)
		}

		// Nothing should have been written to the root.
		entries, err := os.ReadDir(root)
		is.NotError(t, err)
		is.Equal(t, 0, len(entries))
	})
}
