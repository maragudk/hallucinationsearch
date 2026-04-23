package html

import (
	"crypto/sha256"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"maragu.dev/glue/html"
	. "maragu.dev/gomponents"
	. "maragu.dev/gomponents/components"
	. "maragu.dev/gomponents/html"
)

const (
	siteName  = "Hallucination Search"
	ogImage   = "/images/og.jpg"
	ogWidth   = "1200"
	ogHeight  = "630"
	ogType    = "website"
	twCardTyp = "summary_large_image"
)

var hashOnce sync.Once
var appCSSPath, appJSPath string
var datastarJSPath string

func Page(props PageProps, body ...Node) Node {
	hashOnce.Do(func() {
		appCSSPath = getHashedPath("public/styles/app.css")
		appJSPath = getHashedPath("public/scripts/app.js")

		datastarJSPath = getHashedPath("public/scripts/datastar.js")
	})

	return HTML5(HTML5Props{
		Title:       props.Title,
		Description: props.Description,
		Language:    "en",
		Head: []Node{
			Link(Rel("stylesheet"), Href(appCSSPath)),
			Script(Type("module"), Src(datastarJSPath), Defer()),
			Script(Src(appJSPath), Defer()),
			Script(Src("https://cdn.usefathom.com/script.js"), Data("site", "123"), Defer()),
			html.FavIcons("Hallucination Search"),
			ogTags(props),
		},
		Body: []Node{Class("min-h-dvh bg-white dark:bg-gray-800 text-gray-900 dark:text-white"),
			Div(Class("min-h-dvh flex flex-col"),
				header(props),
				Div(Class("grow bg-white dark:bg-gray-800 h-auto"),
					container(true,
						Group(body),
					),
				),
			),
		},
	})
}

func header(_ PageProps) Node {
	return Div(
		container(false),
	)
}

func container(padY bool, children ...Node) Node {
	return Div(
		Classes{
			"max-w-7xl mx-auto h-full": true,
			"px-4 sm:px-6 lg:px-8":     true,
			"py-4 md:py-8":             padY,
		},
		Group(children),
	)
}

// ogTags emits the OpenGraph + Twitter card meta tags. When props.R is nil (for
// example on error / 404 pages rendered outside a request context), we skip the
// tags entirely rather than emit broken relative URLs that crawlers will cache.
// og:image deliberately links to the static /images/og.jpg path without going
// through getHashedPath: crawlers store the URL verbatim, so hashing adds churn
// without benefit.
func ogTags(props PageProps) Node {
	if props.R == nil {
		return nil
	}

	pageURL := AbsoluteURL(props.R, props.R.URL.RequestURI())
	imageURL := AbsoluteURL(props.R, ogImage)

	return Group{
		// OpenGraph.
		Meta(Attr("property", "og:type"), Content(ogType)),
		Meta(Attr("property", "og:site_name"), Content(siteName)),
		Meta(Attr("property", "og:title"), Content(props.Title)),
		Meta(Attr("property", "og:description"), Content(props.Description)),
		Meta(Attr("property", "og:url"), Content(pageURL)),
		Meta(Attr("property", "og:image"), Content(imageURL)),
		Meta(Attr("property", "og:image:width"), Content(ogWidth)),
		Meta(Attr("property", "og:image:height"), Content(ogHeight)),

		// Twitter card.
		Meta(Name("twitter:card"), Content(twCardTyp)),
		Meta(Name("twitter:title"), Content(props.Title)),
		Meta(Name("twitter:description"), Content(props.Description)),
		Meta(Name("twitter:image"), Content(imageURL)),
	}
}

// AbsoluteURL resolves path against r's scheme + host. The scheme is https if
// the request arrived over TLS or a reverse proxy set X-Forwarded-Proto=https;
// otherwise http. path is used verbatim, so it can include a query string.
func AbsoluteURL(r *http.Request, path string) string {
	scheme := "http"
	if r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https" {
		scheme = "https"
	}
	return scheme + "://" + r.Host + path
}

func getHashedPath(path string) string {
	externalPath := strings.TrimPrefix(path, "public")
	ext := filepath.Ext(path)
	if ext == "" {
		panic("no extension found")
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Sprintf("%v.x%v", strings.TrimSuffix(externalPath, ext), ext)
	}

	return fmt.Sprintf("%v.%x%v", strings.TrimSuffix(externalPath, ext), sha256.Sum256(data), ext)
}
