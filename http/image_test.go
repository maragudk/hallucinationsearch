package http

import (
	"testing"

	"maragu.dev/is"
)

func TestImagePathToPrompt(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
		ok   bool
	}{
		{
			name: "basic kebab case",
			in:   "tabby-cat-on-books",
			want: "tabby cat on books",
			ok:   true,
		},
		{
			name: "uppercase is lowercased",
			in:   "Tabby-CAT-On-Books",
			want: "tabby cat on books",
			ok:   true,
		},
		{
			name: "percent-encoded spaces round-trip",
			in:   "tabby%20cat%20on%20books",
			want: "tabby cat on books",
			ok:   true,
		},
		{
			name: "percent-encoded uppercase",
			in:   "Tabby%20CAT%20On%20Books",
			want: "tabby cat on books",
			ok:   true,
		},
		{
			name: "multiple dashes collapse to one space",
			in:   "tabby---cat---on---books",
			want: "tabby cat on books",
			ok:   true,
		},
		{
			name: "trailing dashes are trimmed",
			in:   "---tabby-cat---",
			want: "tabby cat",
			ok:   true,
		},
		{
			name: "slashes become spaces",
			in:   "tabby/cat/on/books",
			want: "tabby cat on books",
			ok:   true,
		},
		{
			name: "mixed slash and dash",
			in:   "tabby-cat/on-books",
			want: "tabby cat on books",
			ok:   true,
		},
		{
			name: "empty is rejected",
			in:   "",
			want: "",
			ok:   false,
		},
		{
			name: "only separators is rejected",
			in:   "---///",
			want: "",
			ok:   false,
		},
		{
			name: "internal whitespace collapses",
			in:   "tabby  cat   on\tbooks",
			want: "tabby cat on books",
			ok:   true,
		},
		{
			name: "invalid percent-encoding is rejected",
			in:   "tabby-%ZZ-cat",
			want: "",
			ok:   false,
		},
		{
			name: "non-utf8 bytes rejected via percent-encoding",
			in:   "tabby-%FF%FE-cat",
			want: "",
			ok:   false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := imagePathToPrompt(tc.in)
			is.Equal(t, tc.ok, ok)
			is.Equal(t, tc.want, got)
		})
	}
}

func TestSniffImageMime(t *testing.T) {
	cases := []struct {
		name string
		in   []byte
		want string
	}{
		{
			name: "PNG magic",
			in:   []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A, 0x00, 0x00},
			want: "image/png",
		},
		{
			name: "JPEG magic (Nano Banana v2)",
			in:   []byte{0xFF, 0xD8, 0xFF, 0xE0, 0x00, 0x10, 'J', 'F', 'I', 'F'},
			want: "image/jpeg",
		},
		{
			name: "WebP magic",
			in:   []byte{'R', 'I', 'F', 'F', 0x00, 0x00, 0x00, 0x00, 'W', 'E', 'B', 'P'},
			want: "image/webp",
		},
		{
			name: "unknown bytes fall back to image/png",
			in:   []byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0a, 0x0b, 0x0c},
			want: "image/png",
		},
		{
			name: "empty falls back to image/png",
			in:   nil,
			want: "image/png",
		},
		{
			name: "too short for PNG magic falls back",
			in:   []byte{0x89, 0x50, 0x4E, 0x47},
			want: "image/png",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			is.Equal(t, tc.want, sniffImageMime(tc.in))
		})
	}
}
