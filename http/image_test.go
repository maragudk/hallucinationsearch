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
