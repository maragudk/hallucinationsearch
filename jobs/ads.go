package jobs

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	"maragu.dev/glue/jobs"
	"maragu.dev/goqite"

	"app/llm"
	"app/model"
)

const adsPerQuery = 3

type generateAdsDB interface {
	GetQuery(ctx context.Context, id model.QueryID) (model.Query, error)
	GetAdPositions(ctx context.Context, id model.QueryID) ([]int, error)
}

// GenerateAds fans out a [model.JobNameGenerateAd] job for every ad position
// that doesn't yet have a row. Cheap and idempotent: safe to re-enqueue on every page load.
func GenerateAds(log *slog.Logger, db generateAdsDB, queue *goqite.Queue) jobs.Func {
	return jobs.WithTracing("jobs.GenerateAds", func(ctx context.Context, m []byte) error {
		var jd model.GenerateAdsJobData
		if err := json.Unmarshal(m, &jd); err != nil {
			return err
		}

		trace.SpanFromContext(ctx).SetAttributes(attribute.String("query.id", string(jd.QueryID)))

		q, err := db.GetQuery(ctx, jd.QueryID)
		if err != nil {
			log.Error("Error getting query for GenerateAds", "error", err, "query_id", jd.QueryID)
			return err
		}

		positions, err := db.GetAdPositions(ctx, q.ID)
		if err != nil {
			return err
		}
		filled := make(map[int]bool, len(positions))
		for _, p := range positions {
			filled[p] = true
		}

		log.Info("Fanning out ad jobs", "query", q.Text, "already_filled", len(filled))

		for i := 0; i < adsPerQuery; i++ {
			if filled[i] {
				continue
			}
			body, err := json.Marshal(model.GenerateAdJobData{QueryID: q.ID, Position: i})
			if err != nil {
				return err
			}
			if err := jobs.Create(ctx, queue, model.JobNameGenerateAd.String(), goqite.Message{
				Body:     body,
				Priority: 1,
			}); err != nil {
				log.Error("Error enqueueing generate-ad job", "error", err, "position", i)
				return err
			}
		}
		return nil
	})
}

type generateAdDB interface {
	GetQuery(ctx context.Context, id model.QueryID) (model.Query, error)
	GetAds(ctx context.Context, id model.QueryID) ([]model.Ad, error)
	InsertAd(ctx context.Context, a model.Ad) error
}

type adGenerator interface {
	GenerateAd(ctx context.Context, query string, position int, existingSponsors []string) (llm.Ad, error)
}

// GenerateAd fabricates a single ad for a query and position and inserts it.
// The insert uses on-conflict-do-nothing so concurrent runs of the same position are harmless.
func GenerateAd(log *slog.Logger, db generateAdDB, gen adGenerator, _ *goqite.Queue) jobs.Func {
	return jobs.WithTracing("jobs.GenerateAd", func(ctx context.Context, m []byte) error {
		var jd model.GenerateAdJobData
		if err := json.Unmarshal(m, &jd); err != nil {
			return err
		}

		trace.SpanFromContext(ctx).SetAttributes(
			attribute.String("query.id", string(jd.QueryID)),
			attribute.Int("ad.position", jd.Position),
		)

		q, err := db.GetQuery(ctx, jd.QueryID)
		if err != nil {
			return err
		}

		// Collect sponsors already produced so the model picks a distinct brand.
		existing, err := db.GetAds(ctx, q.ID)
		if err != nil {
			return err
		}
		sponsors := make([]string, 0, len(existing))
		for _, a := range existing {
			sponsors = append(sponsors, a.Sponsor)
		}

		log.Info("Fabricating ad", "query", q.Text, "position", jd.Position)

		llmCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
		defer cancel()

		a, err := gen.GenerateAd(llmCtx, q.Text, jd.Position, sponsors)
		if err != nil {
			log.Error("Error fabricating ad", "error", err, "query", q.Text, "position", jd.Position)
			return err
		}

		return db.InsertAd(ctx, model.Ad{
			QueryID:     q.ID,
			Position:    jd.Position,
			Title:       a.Title,
			DisplayURL:  a.DisplayURL,
			Description: a.Description,
			Sponsor:     a.Sponsor,
			CTA:         a.CTA,
		})
	})
}

type generateAdWebsiteDB interface {
	GetAd(ctx context.Context, id model.AdID) (model.Ad, error)
	GetQuery(ctx context.Context, id model.QueryID) (model.Query, error)
	InsertAdWebsite(ctx context.Context, id model.AdID, html string) error
}

type adWebsiteGenerator interface {
	GenerateAdWebsite(ctx context.Context, query, title, displayURL, description, sponsor, cta string) (string, error)
}

// GenerateAdWebsite fabricates a full standalone HTML landing page for a single ad and stores it.
func GenerateAdWebsite(log *slog.Logger, db generateAdWebsiteDB, gen adWebsiteGenerator) jobs.Func {
	return jobs.WithTracing("jobs.GenerateAdWebsite", func(ctx context.Context, m []byte) error {
		var jd model.GenerateAdWebsiteJobData
		if err := json.Unmarshal(m, &jd); err != nil {
			return err
		}

		trace.SpanFromContext(ctx).SetAttributes(attribute.String("ad.id", string(jd.AdID)))

		a, err := db.GetAd(ctx, jd.AdID)
		if err != nil {
			return err
		}
		q, err := db.GetQuery(ctx, a.QueryID)
		if err != nil {
			return err
		}

		log.Info("Fabricating ad website", "query", q.Text, "title", a.Title, "sponsor", a.Sponsor)

		llmCtx, cancel := context.WithTimeout(ctx, 3*time.Minute)
		defer cancel()

		html, err := gen.GenerateAdWebsite(llmCtx, q.Text, a.Title, a.DisplayURL, a.Description, a.Sponsor, a.CTA)
		if err != nil {
			log.Error("Error fabricating ad website", "error", err, "ad_id", a.ID)
			return err
		}

		return db.InsertAdWebsite(ctx, a.ID, html)
	})
}
