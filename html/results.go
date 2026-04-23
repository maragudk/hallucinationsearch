package html

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"

	. "maragu.dev/gomponents"
	data "maragu.dev/gomponents-datastar"
	. "maragu.dev/gomponents/html"

	"app/model"
)

const (
	resultsPerQuery = 10
	adsPerQuery     = 3
)

type ResultsPageProps struct {
	PageProps
	QueryRaw string
	QueryID  model.QueryID
	Results  []model.Result
	Ads      []model.Ad
}

// ResultsPage is the /?q=... view: a fixed search bar at the top, then three
// sponsored-result slots (visually almost identical to the regular results, but
// with a camouflaged "Ad" label plus sponsor and call-to-action), then ten
// regular result slots. Each slot binds to a Datastar signal; the server streams
// signal patches via `/events?q=...` to fill empty slots as the LLM produces them.
func ResultsPage(props ResultsPageProps) Node {
	props.Title = "Hallucination Search: " + props.QueryRaw
	props.Description = "Fabricated search results for " + props.QueryRaw

	byPosition := make(map[int]model.Result, len(props.Results))
	for _, r := range props.Results {
		byPosition[r.Position] = r
	}

	adsByPosition := make(map[int]model.Ad, len(props.Ads))
	for _, a := range props.Ads {
		adsByPosition[a.Position] = a
	}

	initialSignals := map[string]any{
		"results": initialResultsSignals(props.Results),
		"ads":     initialAdsSignals(props.Ads),
	}

	eventsURL := "/events?q=" + url.QueryEscape(props.QueryRaw)

	return Page(props.PageProps,
		Div(
			data.Signals(initialSignals),
			data.Init(fmt.Sprintf("@get(%q)", eventsURL)),

			Div(Class("mb-6"),
				A(Href("/"), Class("inline-block text-2xl md:text-3xl font-bold tracking-tight bg-gradient-to-r from-fuchsia-600 via-pink-500 to-purple-600 bg-clip-text text-transparent bg-[length:200%_auto] animate-gradient-shift"),
					Text("Hallucination Search"),
				),
			),

			Div(Class("mb-8"),
				searchForm(props.QueryRaw, "w-full"),
			),

			Div(Class("flex flex-col gap-6 mb-6"),
				Group(mapAdPositions(adsByPosition)),
			),

			Div(Class("flex flex-col gap-6"),
				Group(mapPositions(byPosition)),
			),
		),
	)
}

func mapPositions(byPosition map[int]model.Result) []Node {
	out := make([]Node, 0, resultsPerQuery)
	for i := 0; i < resultsPerQuery; i++ {
		out = append(out, resultCard(i, byPosition[i]))
	}
	return out
}

// resultCard renders slot `position` as both the skeleton and the filled card,
// letting Datastar toggle between them based on the corresponding signal's `f`
// (filled) boolean. Using a dedicated boolean rather than `!= null` avoids a
// Datastar quirk where nested-null comparisons can resolve truthy on first paint.
func resultCard(position int, r model.Result) Node {
	signal := fmt.Sprintf("$results.p%d", position)

	// If we already have this result server-side, start with the card visible and no
	// skeleton shown; otherwise start with skeleton visible and card hidden.
	haveResult := r.Title != ""
	title := r.Title
	displayURL := r.DisplayURL
	description := r.Description
	slug := ""
	if haveResult {
		slug = siteSlug(title, string(r.ID))
	}

	return Div(Class("result-card"),
		// Skeleton card -- three pulsing grey bars. Shown only until the signal is
		// flagged as filled. When we already have the result server-side we hide
		// it immediately so there's no skeleton flash before Datastar evaluates.
		Div(
			data.Show("!"+signal+".f"),
			If(haveResult, Style("display: none")),
			Class("animate-pulse flex flex-col gap-3"),
			Div(Class("h-3 bg-gray-200 dark:bg-gray-700 rounded w-1/3")),
			Div(Class("h-6 bg-gray-300 dark:bg-gray-600 rounded w-2/3")),
			Div(Class("h-4 bg-gray-200 dark:bg-gray-700 rounded w-full")),
		),

		// Filled card. Server-rendered text acts as a no-JS fallback, and Datastar
		// rebinds the same text once `.f` flips true. Hidden initially when we have
		// no server-side data to avoid a flash of empty text.
		Div(
			data.Show(signal+".f"),
			If(!haveResult, Style("display: none")),
			Class("flex flex-col gap-1"),

			// Display URL (grey breadcrumb).
			P(Class("text-sm text-gray-500 dark:text-gray-400 truncate"),
				If(haveResult, Text(displayURL)),
				data.Text(signal+".u"),
			),

			// Title link (fuchsia, purple once visited).
			H2(Class("text-xl font-semibold"),
				A(
					Class("text-primary-600 visited:text-primary-900 dark:visited:text-primary-400 hover:text-primary-500 hover:underline"),
					If(haveResult, Href("/site/"+slug)),
					data.Attr("href", "'/site/' + "+signal+".s"),
					If(haveResult, Text(title)),
					data.Text(signal+".t"),
				),
			),

			// Description.
			P(Class("text-gray-700 dark:text-gray-200"),
				If(haveResult, Text(description)),
				data.Text(signal+".d"),
			),
		),
	)
}

// initialResultsSignals returns a map of `p0..p9` entries seeded with the results
// we already have at render time, plus empty placeholders (`f: false`) for the
// rest -- so the client-side signal shape matches what we stream later, and so
// every slot has a stable boolean `.f` flag to key `data-show` off.
func initialResultsSignals(rs []model.Result) map[string]any {
	out := make(map[string]any, resultsPerQuery)
	for i := 0; i < resultsPerQuery; i++ {
		out[fmt.Sprintf("p%d", i)] = map[string]any{
			"f": false,
			"t": "",
			"u": "",
			"d": "",
			"s": "",
		}
	}
	for _, r := range rs {
		out[fmt.Sprintf("p%d", r.Position)] = map[string]any{
			"f": true,
			"t": r.Title,
			"u": r.DisplayURL,
			"d": r.Description,
			"s": siteSlug(r.Title, string(r.ID)),
		}
	}
	return out
}

// initialAdsSignals mirrors [initialResultsSignals] for the `ads` branch.
// The payload shape adds `n` (sponsor name) and `c` (CTA label).
func initialAdsSignals(as []model.Ad) map[string]any {
	out := make(map[string]any, adsPerQuery)
	for i := 0; i < adsPerQuery; i++ {
		out[fmt.Sprintf("p%d", i)] = map[string]any{
			"f": false,
			"t": "",
			"u": "",
			"d": "",
			"s": "",
			"n": "",
			"c": "",
		}
	}
	for _, a := range as {
		out[fmt.Sprintf("p%d", a.Position)] = map[string]any{
			"f": true,
			"t": a.Title,
			"u": a.DisplayURL,
			"d": a.Description,
			"s": siteSlug(a.Title, string(a.ID)),
			"n": a.Sponsor,
			"c": a.CTA,
		}
	}
	return out
}

func mapAdPositions(byPosition map[int]model.Ad) []Node {
	out := make([]Node, 0, adsPerQuery)
	for i := 0; i < adsPerQuery; i++ {
		out = append(out, adCard(i, byPosition[i]))
	}
	return out
}

// adCard renders an ad slot with the same skeleton/filled toggle as [resultCard],
// plus a small "Ad" label camouflaged near-white on white (dark-pattern style),
// a sponsor name, and a call-to-action button linking to /ad/{slug}. The visual
// shape is otherwise near-identical to a regular result so the ad blends in.
func adCard(position int, a model.Ad) Node {
	signal := fmt.Sprintf("$ads.p%d", position)

	haveAd := a.Title != ""
	title := a.Title
	displayURL := a.DisplayURL
	description := a.Description
	sponsor := a.Sponsor
	cta := a.CTA
	slug := ""
	if haveAd {
		slug = siteSlug(title, string(a.ID))
	}

	return Div(Class("result-card"),
		// Skeleton -- same three-bar pulse as resultCard, shown until the signal fills.
		Div(
			data.Show("!"+signal+".f"),
			If(haveAd, Style("display: none")),
			Class("animate-pulse flex flex-col gap-3"),
			Div(Class("h-3 bg-gray-200 dark:bg-gray-700 rounded w-1/3")),
			Div(Class("h-6 bg-gray-300 dark:bg-gray-600 rounded w-2/3")),
			Div(Class("h-4 bg-gray-200 dark:bg-gray-700 rounded w-full")),
		),

		// Filled ad card.
		Div(
			data.Show(signal+".f"),
			If(!haveAd, Style("display: none")),
			Class("flex flex-col gap-1"),

			// Tiny "Ad" badge plus sponsor + display URL. The "Ad" label is near-white
			// on white so it's technically there but effectively invisible. Sponsor is
			// the normal grey breadcrumb colour so users read past the "Ad" without
			// noticing.
			Div(Class("flex items-center gap-2 text-sm"),
				Span(Class("text-gray-100 dark:text-gray-800 font-semibold select-none"),
					Text("Ad"),
				),
				Span(Class("text-gray-500 dark:text-gray-400"),
					If(haveAd, Text(sponsor)),
					data.Text(signal+".n"),
				),
				Span(Class("text-gray-400 dark:text-gray-500"),
					Text("·"),
				),
				P(Class("text-gray-500 dark:text-gray-400 truncate min-w-0"),
					If(haveAd, Text(displayURL)),
					data.Text(signal+".u"),
				),
			),

			// Title link -- styled identically to result titles.
			H2(Class("text-xl font-semibold"),
				A(
					Class("text-primary-600 visited:text-primary-900 dark:visited:text-primary-400 hover:text-primary-500 hover:underline"),
					If(haveAd, Href("/ad/"+slug)),
					data.Attr("href", "'/ad/' + "+signal+".s"),
					If(haveAd, Text(title)),
					data.Text(signal+".t"),
				),
			),

			// Description with an inline call-to-action tucked at the end, Google-sitelink
			// style. The description text lives in its own <span> so Datastar's data-text
			// targets the span rather than clobbering the nested <a> link.
			P(Class("text-gray-700 dark:text-gray-200"),
				Span(
					If(haveAd, Text(description)),
					data.Text(signal+".d"),
				),
				Text(" "),
				A(
					Class("text-primary-600 hover:text-primary-500 hover:underline whitespace-nowrap"),
					If(haveAd, Href("/ad/"+slug)),
					data.Attr("href", "'/ad/' + "+signal+".s"),
					If(haveAd, Text(cta+" →")),
					data.Text(signal+".c + ' →'"),
				),
			),
		),
	)
}

var nonSlugRe = regexp.MustCompile(`[^a-z0-9]+`)

// siteSlug mirrors the http package's slug helper; duplicated here so the html
// package doesn't import http. Kept in sync by hand. The id includes its own
// `r_`/`a_` type prefix so the same helper works for result and ad links.
func siteSlug(title, id string) string {
	slug := strings.ToLower(title)
	slug = nonSlugRe.ReplaceAllString(slug, "-")
	slug = strings.Trim(slug, "-")
	if len(slug) > 60 {
		slug = slug[:60]
		slug = strings.TrimRight(slug, "-")
	}
	if slug == "" {
		return id
	}
	return slug + "-" + id
}
