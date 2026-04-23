package html_test

import (
	"crypto/tls"
	"net/http/httptest"
	"strings"
	"testing"

	"maragu.dev/is"

	"app/html"
)

func TestAbsoluteURL(t *testing.T) {
	t.Run("uses http for plain requests", func(t *testing.T) {
		r := httptest.NewRequest("GET", "http://example.com/", nil)
		is.Equal(t, "http://example.com/images/og.jpg", html.AbsoluteURL(r, "/images/og.jpg"))
	})

	t.Run("uses https when TLS is present", func(t *testing.T) {
		r := httptest.NewRequest("GET", "https://example.com/", nil)
		r.TLS = &tls.ConnectionState{}
		is.Equal(t, "https://example.com/images/og.jpg", html.AbsoluteURL(r, "/images/og.jpg"))
	})

	t.Run("honours X-Forwarded-Proto https", func(t *testing.T) {
		r := httptest.NewRequest("GET", "http://example.com/", nil)
		r.Header.Set("X-Forwarded-Proto", "https")
		is.Equal(t, "https://example.com/images/og.jpg", html.AbsoluteURL(r, "/images/og.jpg"))
	})

	t.Run("preserves host with port", func(t *testing.T) {
		r := httptest.NewRequest("GET", "http://localhost:8091/", nil)
		is.Equal(t, "http://localhost:8091/", html.AbsoluteURL(r, "/"))
	})

	t.Run("preserves query string in path", func(t *testing.T) {
		r := httptest.NewRequest("GET", "http://localhost:8091/", nil)
		is.Equal(t, "http://localhost:8091/?q=cats", html.AbsoluteURL(r, "/?q=cats"))
	})
}

func TestHomePageOGTags(t *testing.T) {
	r := httptest.NewRequest("GET", "http://localhost:8091/", nil)
	props := html.HomePageProps{PageProps: html.PageProps{R: r}}

	var sb strings.Builder
	is.NotError(t, html.HomePage(props).Render(&sb))
	out := sb.String()

	// Title and description from PageProps.
	is.True(t, strings.Contains(out, `<title>Hallucination Search</title>`))
	is.True(t, strings.Contains(out, `<meta name="description" content="Nothing you see here is real.">`))

	// OG basics.
	is.True(t, strings.Contains(out, `<meta property="og:type" content="website">`))
	is.True(t, strings.Contains(out, `<meta property="og:site_name" content="Hallucination Search">`))
	is.True(t, strings.Contains(out, `<meta property="og:title" content="Hallucination Search">`))
	is.True(t, strings.Contains(out, `<meta property="og:description" content="Nothing you see here is real.">`))

	// URLs are absolute and query-aware. Home has no q so url is just /.
	is.True(t, strings.Contains(out, `<meta property="og:url" content="http://localhost:8091/">`))
	is.True(t, strings.Contains(out, `<meta property="og:image" content="http://localhost:8091/images/og.jpg">`))
	is.True(t, strings.Contains(out, `<meta property="og:image:width" content="1200">`))
	is.True(t, strings.Contains(out, `<meta property="og:image:height" content="630">`))

	// Twitter card.
	is.True(t, strings.Contains(out, `<meta name="twitter:card" content="summary_large_image">`))
	is.True(t, strings.Contains(out, `<meta name="twitter:title" content="Hallucination Search">`))
	is.True(t, strings.Contains(out, `<meta name="twitter:description" content="Nothing you see here is real.">`))
	is.True(t, strings.Contains(out, `<meta name="twitter:image" content="http://localhost:8091/images/og.jpg">`))
}

func TestResultsPageOGTags(t *testing.T) {
	r := httptest.NewRequest("GET", "http://localhost:8091/?q=testquery", nil)
	props := html.ResultsPageProps{
		PageProps: html.PageProps{R: r},
		QueryRaw:  "testquery",
	}

	var sb strings.Builder
	is.NotError(t, html.ResultsPage(props).Render(&sb))
	out := sb.String()

	// Title and description are query-aware.
	is.True(t, strings.Contains(out, `<title>Hallucination Search: testquery</title>`))
	is.True(t, strings.Contains(out, `<meta name="description" content="Fabricated search results for testquery">`))

	// OG title/description follow the same, site_name stays the brand.
	is.True(t, strings.Contains(out, `<meta property="og:site_name" content="Hallucination Search">`))
	is.True(t, strings.Contains(out, `<meta property="og:title" content="Hallucination Search: testquery">`))
	is.True(t, strings.Contains(out, `<meta property="og:description" content="Fabricated search results for testquery">`))

	// URL includes the original request path + query.
	is.True(t, strings.Contains(out, `<meta property="og:url" content="http://localhost:8091/?q=testquery">`))
	is.True(t, strings.Contains(out, `<meta property="og:image" content="http://localhost:8091/images/og.jpg">`))

	// Twitter title/description mirror.
	is.True(t, strings.Contains(out, `<meta name="twitter:title" content="Hallucination Search: testquery">`))
	is.True(t, strings.Contains(out, `<meta name="twitter:description" content="Fabricated search results for testquery">`))
	is.True(t, strings.Contains(out, `<meta name="twitter:image" content="http://localhost:8091/images/og.jpg">`))
}

func TestErrorPageSkipsOGWhenRequestIsNil(t *testing.T) {
	// ErrorPage is rendered from places where we may not have a request (background error flush).
	// It must not panic and must not emit broken relative URLs.
	var sb strings.Builder
	is.NotError(t, html.ErrorPage().Render(&sb))
	out := sb.String()

	is.True(t, !strings.Contains(out, `property="og:`))
	is.True(t, !strings.Contains(out, `name="twitter:`))
}
