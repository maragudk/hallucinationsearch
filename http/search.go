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

	"maragu.dev/goqite"

	. "maragu.dev/gomponents"

	gluejobs "maragu.dev/glue/jobs"

	"app/html"
	"app/model"
)

const (
	resultsPerQuery    = 10
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

		q, err := svc.UpsertQuery(ctx, normalised)
		if err != nil {
			log.Error("Error upserting query", "error", err, "query", normalised)
			return nil, err
		}

		if err := enqueueGenerateResults(ctx, svc.Queue(), q.ID); err != nil {
			log.Error("Error enqueueing generate-results", "error", err, "query_id", q.ID)
			// Do not fail the whole page over this; render what we have.
		}

		results, err := svc.GetResults(ctx, q.ID)
		if err != nil {
			return nil, err
		}

		return html.ResultsPage(html.ResultsPageProps{
			PageProps: props,
			QueryRaw:  raw,
			QueryID:   q.ID,
			Results:   results,
		}), nil
	})

	r.Mux.Get("/events", handleEvents(log, svc))
	r.Mux.Get("/site/{slug}", handleSite(log, svc))
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

// handleEvents streams Datastar signal patches. Signals carry per-position result
// payloads, which the page reactively binds to its skeleton cards.
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

		q, err := svc.GetQueryByText(ctx, normalised)
		if err != nil {
			// Nothing to stream -- the main page handler will have created the row,
			// but if we race it we just bail quietly.
			return
		}

		sent := make(map[int]bool, resultsPerQuery)

		emit := func(results []model.Result) error {
			first := true
			for _, res := range results {
				if sent[res.Position] {
					continue
				}
				if first {
					// Signals are merged client-side, so we can emit all fresh
					// positions in one event.
					first = false
				}
				sent[res.Position] = true
			}
			return writeSignalsPatch(w, flusher, results, sent)
		}

		// Initial push.
		initial, err := svc.GetResults(ctx, q.ID)
		if err != nil {
			log.Error("Error reading initial results", "error", err)
			return
		}
		if err := emit(initial); err != nil {
			return
		}
		if len(sent) >= resultsPerQuery {
			return
		}

		ticker := time.NewTicker(eventsPollInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				rs, err := svc.GetResults(ctx, q.ID)
				if err != nil {
					log.Error("Error polling results", "error", err)
					return
				}
				if err := emit(rs); err != nil {
					return
				}
				if len(sent) >= resultsPerQuery {
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

// writeSignalsPatch writes a datastar-patch-signals event containing every position
// in `sent` that we know about, as a nested object under `results`.
func writeSignalsPatch(w http.ResponseWriter, flusher http.Flusher, all []model.Result, sent map[int]bool) error {
	if len(sent) == 0 {
		return nil
	}

	payload := make(map[string]any, len(sent))
	for _, r := range all {
		if !sent[r.Position] {
			continue
		}
		payload[fmt.Sprintf("p%d", r.Position)] = signalPayload{
			F: true,
			T: r.Title,
			U: r.DisplayURL,
			D: r.Description,
			S: siteSlug(r.Title, r.ID),
		}
	}

	signals := map[string]any{"results": payload}
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

		site, err := svc.GetWebsite(ctx, res.ID)
		if err == nil {
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

func writeSiteHTML(w http.ResponseWriter, html string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=86400")
	w.Header().Set("X-Frame-Options", "SAMEORIGIN")
	_, _ = w.Write([]byte(html))
}

// resultIDRe matches the `r_`+32-hex suffix we put at the end of the site URL.
var resultIDRe = regexp.MustCompile(`r_[0-9a-f]{32}$`)

func extractResultID(slug string) (model.ResultID, bool) {
	m := resultIDRe.FindString(slug)
	if m == "" {
		return "", false
	}
	return model.ResultID(m), true
}

// siteSlug produces the URL path segment for a result link:
// a cosmetic kebab-case title, a dash, then the result ID.
func siteSlug(title string, id model.ResultID) string {
	slug := titleToSlug(title)
	if slug == "" {
		return string(id)
	}
	return slug + "-" + string(id)
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
