package model

import (
	"strings"
	"unicode"
)

// QueryID is the primary key type for a search query row.
type QueryID string

// ResultID is the primary key type for a search result row.
type ResultID string

// AdID is the primary key type for a fabricated ad row.
type AdID string

// Query is a normalised search query, representing one row in the queries table.
type Query struct {
	ID      QueryID
	Created Time
	Updated Time
	Text    string
}

// Result is a single fabricated search result for a query.
// Position is 0-9 and unique per query.
type Result struct {
	ID          ResultID
	Created     Time
	Updated     Time
	QueryID     QueryID `db:"query_id"`
	Position    int
	Title       string
	DisplayURL  string `db:"display_url"`
	Description string
}

// Website is a fabricated destination page for a result.
// The result_id is both the primary key and the foreign key to results.id.
type Website struct {
	ResultID ResultID `db:"result_id"`
	Created  Time
	Updated  Time
	HTML     string
}

// Ad is a single fabricated sponsored result for a query.
// Position is 0-2 and unique per query. Parallel to [Result], but with a sponsor
// name and call-to-action, and camouflaged "Ad" label in the UI.
type Ad struct {
	ID          AdID
	Created     Time
	Updated     Time
	QueryID     QueryID `db:"query_id"`
	Position    int
	Title       string
	DisplayURL  string `db:"display_url"`
	Description string
	Sponsor     string
	CTA         string `db:"cta"`
}

// AdWebsite is a fabricated destination page for an ad.
// The ad_id is both the primary key and the foreign key to ads.id.
type AdWebsite struct {
	AdID    AdID `db:"ad_id"`
	Created Time
	Updated Time
	HTML    string
}

// NormalizeQuery trims, lowercases, and collapses internal whitespace in q to single spaces.
// The normalised form is what we store in the queries table and match on.
func NormalizeQuery(q string) string {
	q = strings.ToLower(strings.TrimSpace(q))

	var b strings.Builder
	b.Grow(len(q))
	var prevSpace bool
	for _, r := range q {
		if unicode.IsSpace(r) {
			if !prevSpace && b.Len() > 0 {
				b.WriteByte(' ')
			}
			prevSpace = true
			continue
		}
		b.WriteRune(r)
		prevSpace = false
	}

	out := b.String()
	return strings.TrimRight(out, " ")
}
