package http

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"regexp"
	"strings"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	"maragu.dev/goqite"

	. "maragu.dev/gomponents"

	gluejobs "maragu.dev/glue/jobs"

	"app/html"
	"app/model"
)

const (
	resultsPerQuery    = 10
	adsPerQuery        = 3
	sitePollInterval   = 500 * time.Millisecond
	siteMaxWait        = 2 * time.Minute
	eventsPollInterval = 500 * time.Millisecond
	eventsMaxWait      = 60 * time.Second
)

type searchDB interface {
	UpsertQuery(ctx context.Context, text string) (model.Query, error)
	GetQueryByText(ctx context.Context, text string) (model.Query, error)
	GetResults(ctx context.Context, id model.QueryID) ([]model.Result, error)
	GetResult(ctx context.Context, id model.ResultID) (model.Result, error)
	GetWebsite(ctx context.Context, id model.ResultID) (model.Website, error)
	GetAds(ctx context.Context, id model.QueryID) ([]model.Ad, error)
	GetAd(ctx context.Context, id model.AdID) (model.Ad, error)
	GetAdWebsite(ctx context.Context, id model.AdID) (model.AdWebsite, error)
}

type queueAccessor interface {
	Queue() *goqite.Queue
}

type searchService interface {
	searchDB
	queueAccessor
}

type searchServiceAdapter struct {
	searchDB
	queue *goqite.Queue
}

func (s searchServiceAdapter) Queue() *goqite.Queue {
	return s.queue
}

// Search wires up the three search-related routes: the homepage / results page,
// the Datastar SSE signal stream, and the blocking fabricated-site renderer.
func Search(r *Router, log *slog.Logger, svc searchService) {
	r.Get("/", func(props html.PageProps) (Node, error) {
		ctx := props.Ctx
		raw := props.R.URL.Query().Get("q")
		if strings.TrimSpace(raw) == "" {
			return html.HomePage(html.HomePageProps{PageProps: props}), nil
		}

		normalised := model.NormalizeQuery(raw)
		if normalised == "" {
			return html.HomePage(html.HomePageProps{PageProps: props}), nil
		}

		trace.SpanFromContext(ctx).SetAttributes(
			attribute.String("query.raw", truncate(raw, 256)),
			attribute.String("query.text", normalised),
		)

		q, err := svc.UpsertQuery(ctx, normalised)
		if err != nil {
			log.Error("Error upserting query", "error", err, "query", normalised)
			return nil, err
		}

		trace.SpanFromContext(ctx).SetAttributes(attribute.String("query.id", string(q.ID)))

		if err := enqueueGenerateResults(ctx, svc.Queue(), q.ID); err != nil {
			log.Error("Error enqueueing generate-results", "error", err, "query_id", q.ID)
			// Do not fail the whole page over this; render what we have.
		}
		if err := enqueueGenerateAds(ctx, svc.Queue(), q.ID); err != nil {
			log.Error("Error enqueueing generate-ads", "error", err, "query_id", q.ID)
		}

		results, err := svc.GetResults(ctx, q.ID)
		if err != nil {
			return nil, err
		}

		ads, err := svc.GetAds(ctx, q.ID)
		if err != nil {
			return nil, err
		}

		trace.SpanFromContext(ctx).SetAttributes(
			attribute.Int("results.count", len(results)),
			attribute.Int("ads.count", len(ads)),
		)

		return html.ResultsPage(html.ResultsPageProps{
			PageProps: props,
			QueryRaw:  raw,
			QueryID:   q.ID,
			Results:   results,
			Ads:       ads,
		}), nil
	})

	r.Mux.Get("/events", handleEvents(log, svc))
	r.Mux.Get("/site/{slug}", handleSite(log, svc))
	r.Mux.Get("/ad/{slug}", handleAd(log, svc))
}

// enqueueGenerateResults always enqueues a results job as a safety net -- the handler
// renders whatever's already in the DB, and new results arrive via SSE signals.
func enqueueGenerateResults(ctx context.Context, queue *goqite.Queue, id model.QueryID) error {
	body, err := json.Marshal(model.GenerateResultsJobData{QueryID: id})
	if err != nil {
		return err
	}
	return gluejobs.Create(ctx, queue, model.JobNameGenerateResults.String(), goqite.Message{
		Body:     body,
		Priority: 2,
	})
}

// enqueueGenerateAds mirrors [enqueueGenerateResults] for the three fabricated ads.
func enqueueGenerateAds(ctx context.Context, queue *goqite.Queue, id model.QueryID) error {
	body, err := json.Marshal(model.GenerateAdsJobData{QueryID: id})
	if err != nil {
		return err
	}
	return gluejobs.Create(ctx, queue, model.JobNameGenerateAds.String(), goqite.Message{
		Body:     body,
		Priority: 2,
	})
}

// handleEvents streams Datastar signal patches. Signals carry per-position result
// and ad payloads, which the page reactively binds to its skeleton cards.
func handleEvents(log *slog.Logger, svc searchDB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		raw := r.URL.Query().Get("q")
		normalised := model.NormalizeQuery(raw)
		if normalised == "" {
			http.Error(w, "missing q", http.StatusBadRequest)
			return
		}

		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "SSE not supported", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.Header().Set("X-Accel-Buffering", "no") // nginx / proxy friendliness
		w.WriteHeader(http.StatusOK)
		flusher.Flush()

		ctx, cancel := context.WithTimeout(r.Context(), eventsMaxWait)
		defer cancel()

		trace.SpanFromContext(ctx).SetAttributes(attribute.String("query.text", normalised))

		q, err := svc.GetQueryByText(ctx, normalised)
		if err != nil {
			// Nothing to stream -- the main page handler will have created the row,
			// but if we race it we just bail quietly.
			return
		}

		trace.SpanFromContext(ctx).SetAttributes(attribute.String("query.id", string(q.ID)))

		sentResults := make(map[int]bool, resultsPerQuery)
		sentAds := make(map[int]bool, adsPerQuery)

		done := func() bool {
			return len(sentResults) >= resultsPerQuery && len(sentAds) >= adsPerQuery
		}

		defer func() {
			trace.SpanFromContext(ctx).SetAttributes(
				attribute.Int("events.results_sent", len(sentResults)),
				attribute.Int("events.ads_sent", len(sentAds)),
				attribute.Bool("events.done", done()),
			)
		}()

		pushState := func() error {
			rs, err := svc.GetResults(ctx, q.ID)
			if err != nil {
				return err
			}
			ads, err := svc.GetAds(ctx, q.ID)
			if err != nil {
				return err
			}

			freshResults := false
			for _, res := range rs {
				if !sentResults[res.Position] {
					sentResults[res.Position] = true
					freshResults = true
				}
			}
			freshAds := false
			for _, a := range ads {
				if !sentAds[a.Position] {
					sentAds[a.Position] = true
					freshAds = true
				}
			}
			if !freshResults && !freshAds {
				return nil
			}
			return writeSignalsPatch(w, flusher, rs, sentResults, ads, sentAds)
		}

		// Initial push.
		if err := pushState(); err != nil {
			log.Error("Error pushing initial state", "error", err)
			return
		}
		if done() {
			return
		}

		ticker := time.NewTicker(eventsPollInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if err := pushState(); err != nil {
					log.Error("Error polling state", "error", err)
					return
				}
				if done() {
					return
				}
			}
		}
	}
}

// signalPayload is what each position in the `results` signal map contains.
// Keys are short to keep the SSE wire payload small. `F` (filled) drives the
// skeleton-vs-filled `data-show` toggle on the client; anything streamed is
// always filled, so it's hardcoded to true when constructed.
type signalPayload struct {
	F bool   `json:"f"`
	T string `json:"t"`
	U string `json:"u"`
	D string `json:"d"`
	S string `json:"s"`
}

// adSignalPayload is the ad-branch equivalent of [signalPayload]. It adds a
// sponsor name and a call-to-action label. The `s` slug points at /ad/{slug}
// rather than /site/{slug}.
type adSignalPayload struct {
	F bool   `json:"f"`
	T string `json:"t"`
	U string `json:"u"`
	D string `json:"d"`
	S string `json:"s"`
	N string `json:"n"` // sponsor name
	C string `json:"c"` // call to action
}

// writeSignalsPatch writes a datastar-patch-signals event containing every freshly
// filled position under `results` and `ads`. `sentResults` / `sentAds` drive which
// positions make it into the payload.
func writeSignalsPatch(
	w http.ResponseWriter, flusher http.Flusher,
	allResults []model.Result, sentResults map[int]bool,
	allAds []model.Ad, sentAds map[int]bool,
) error {
	if len(sentResults) == 0 && len(sentAds) == 0 {
		return nil
	}

	signals := map[string]any{}

	if len(sentResults) > 0 {
		payload := make(map[string]any, len(sentResults))
		for _, r := range allResults {
			if !sentResults[r.Position] {
				continue
			}
			payload[fmt.Sprintf("p%d", r.Position)] = signalPayload{
				F: true,
				T: r.Title,
				U: r.DisplayURL,
				D: r.Description,
				S: siteSlug(r.Title, string(r.ID)),
			}
		}
		signals["results"] = payload
	}

	if len(sentAds) > 0 {
		payload := make(map[string]any, len(sentAds))
		for _, a := range allAds {
			if !sentAds[a.Position] {
				continue
			}
			payload[fmt.Sprintf("p%d", a.Position)] = adSignalPayload{
				F: true,
				T: a.Title,
				U: a.DisplayURL,
				D: a.Description,
				S: siteSlug(a.Title, string(a.ID)),
				N: a.Sponsor,
				C: a.CTA,
			}
		}
		signals["ads"] = payload
	}

	body, err := json.Marshal(signals)
	if err != nil {
		return err
	}

	if _, err := fmt.Fprintf(w, "event: datastar-patch-signals\ndata: signals %s\n\n", body); err != nil {
		return err
	}
	flusher.Flush()
	return nil
}

// handleSite renders or blocks-and-renders the fabricated destination page.
func handleSite(log *slog.Logger, svc searchService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		slug := strings.TrimPrefix(r.URL.Path, "/site/")
		id, ok := extractResultID(slug)
		if !ok {
			http.NotFound(w, r)
			return
		}

		ctx := r.Context()

		trace.SpanFromContext(ctx).SetAttributes(attribute.String("result.id", string(id)))

		res, err := svc.GetResult(ctx, id)
		if err != nil {
			if errors.Is(err, model.ErrorResultNotFound) {
				http.NotFound(w, r)
				return
			}
			log.Error("Error getting result", "error", err, "id", id)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}

		trace.SpanFromContext(ctx).SetAttributes(attribute.String("query.id", string(res.QueryID)))

		site, err := svc.GetWebsite(ctx, res.ID)
		if err == nil {
			trace.SpanFromContext(ctx).SetAttributes(attribute.Bool("website.cached", true))
			writeSiteHTML(w, site.HTML)
			return
		}
		if !errors.Is(err, model.ErrorWebsiteNotFound) {
			log.Error("Error getting website", "error", err, "id", res.ID)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}

		// Enqueue a website job, then poll the DB until it shows up.
		if err := enqueueGenerateWebsite(ctx, svc.Queue(), res.ID); err != nil {
			log.Error("Error enqueueing generate-website", "error", err)
		}

		pollCtx, cancel := context.WithTimeout(ctx, siteMaxWait)
		defer cancel()
		ticker := time.NewTicker(sitePollInterval)
		defer ticker.Stop()

		for {
			select {
			case <-pollCtx.Done():
				http.Error(w, "website not ready", http.StatusBadGateway)
				return
			case <-ticker.C:
				site, err := svc.GetWebsite(pollCtx, res.ID)
				if err == nil {
					trace.SpanFromContext(ctx).SetAttributes(attribute.Bool("website.cached", false))
					writeSiteHTML(w, site.HTML)
					return
				}
				// If the poll context expired between `select` picking this case and
				// the DB call, the lookup returns a context error rather than
				// [model.ErrorWebsiteNotFound]. Translate that back to the timeout
				// branch so the client gets a 502 rather than a misleading 500.
				if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
					http.Error(w, "website not ready", http.StatusBadGateway)
					return
				}
				if !errors.Is(err, model.ErrorWebsiteNotFound) {
					log.Error("Error getting website", "error", err, "id", res.ID)
					http.Error(w, "internal error", http.StatusInternalServerError)
					return
				}
			}
		}
	}
}

func enqueueGenerateWebsite(ctx context.Context, queue *goqite.Queue, id model.ResultID) error {
	body, err := json.Marshal(model.GenerateWebsiteJobData{ResultID: id})
	if err != nil {
		return err
	}
	return gluejobs.Create(ctx, queue, model.JobNameGenerateWebsite.String(), goqite.Message{
		Body:     body,
		Priority: 0,
	})
}

// handleAd mirrors [handleSite] for fabricated ad landing pages. It renders the
// cached HTML or blocks-and-renders while the `generate-ad-website` job runs.
func handleAd(log *slog.Logger, svc searchService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		slug := strings.TrimPrefix(r.URL.Path, "/ad/")
		id, ok := extractAdID(slug)
		if !ok {
			http.NotFound(w, r)
			return
		}

		ctx := r.Context()

		trace.SpanFromContext(ctx).SetAttributes(attribute.String("ad.id", string(id)))

		a, err := svc.GetAd(ctx, id)
		if err != nil {
			if errors.Is(err, model.ErrorAdNotFound) {
				http.NotFound(w, r)
				return
			}
			log.Error("Error getting ad", "error", err, "id", id)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}

		trace.SpanFromContext(ctx).SetAttributes(attribute.String("query.id", string(a.QueryID)))

		site, err := svc.GetAdWebsite(ctx, a.ID)
		if err == nil {
			trace.SpanFromContext(ctx).SetAttributes(attribute.Bool("ad_website.cached", true))
			writeSiteHTML(w, site.HTML)
			return
		}
		if !errors.Is(err, model.ErrorAdWebsiteNotFound) {
			log.Error("Error getting ad website", "error", err, "id", a.ID)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}

		if err := enqueueGenerateAdWebsite(ctx, svc.Queue(), a.ID); err != nil {
			log.Error("Error enqueueing generate-ad-website", "error", err)
		}

		pollCtx, cancel := context.WithTimeout(ctx, siteMaxWait)
		defer cancel()
		ticker := time.NewTicker(sitePollInterval)
		defer ticker.Stop()

		for {
			select {
			case <-pollCtx.Done():
				http.Error(w, "ad website not ready", http.StatusBadGateway)
				return
			case <-ticker.C:
				site, err := svc.GetAdWebsite(pollCtx, a.ID)
				if err == nil {
					trace.SpanFromContext(ctx).SetAttributes(attribute.Bool("ad_website.cached", false))
					writeSiteHTML(w, site.HTML)
					return
				}
				if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
					http.Error(w, "ad website not ready", http.StatusBadGateway)
					return
				}
				if !errors.Is(err, model.ErrorAdWebsiteNotFound) {
					log.Error("Error getting ad website", "error", err, "id", a.ID)
					http.Error(w, "internal error", http.StatusInternalServerError)
					return
				}
			}
		}
	}
}

func enqueueGenerateAdWebsite(ctx context.Context, queue *goqite.Queue, id model.AdID) error {
	body, err := json.Marshal(model.GenerateAdWebsiteJobData{AdID: id})
	if err != nil {
		return err
	}
	return gluejobs.Create(ctx, queue, model.JobNameGenerateAdWebsite.String(), goqite.Message{
		Body:     body,
		Priority: 0,
	})
}

func writeSiteHTML(w http.ResponseWriter, html string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=86400")
	w.Header().Set("X-Frame-Options", "SAMEORIGIN")
	_, _ = w.Write([]byte(html))
}

// resultIDRe matches the `r_`+32-hex suffix we put at the end of the /site URL.
var resultIDRe = regexp.MustCompile(`r_[0-9a-f]{32}$`)

// adIDRe matches the `a_`+32-hex suffix we put at the end of the /ad URL.
var adIDRe = regexp.MustCompile(`a_[0-9a-f]{32}$`)

func extractResultID(slug string) (model.ResultID, bool) {
	m := resultIDRe.FindString(slug)
	if m == "" {
		return "", false
	}
	return model.ResultID(m), true
}

func extractAdID(slug string) (model.AdID, bool) {
	m := adIDRe.FindString(slug)
	if m == "" {
		return "", false
	}
	return model.AdID(m), true
}

// siteSlug produces the URL path segment for a result or ad link:
// a cosmetic kebab-case title, a dash, then the id (with its type prefix).
func siteSlug(title, id string) string {
	slug := titleToSlug(title)
	if slug == "" {
		return id
	}
	return slug + "-" + id
}

var nonSlugRe = regexp.MustCompile(`[^a-z0-9]+`)

func titleToSlug(s string) string {
	s = strings.ToLower(s)
	s = nonSlugRe.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	if len(s) > 60 {
		s = s[:60]
		s = strings.TrimRight(s, "-")
	}
	return s
}

// truncate clips s to at most n bytes, for attribute values (OTel exporters
// drop attributes that grow past ~4K, so keep span payloads small).
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
