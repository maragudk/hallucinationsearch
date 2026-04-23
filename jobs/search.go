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

const resultsPerQuery = 10

type generateResultsDB interface {
	GetQuery(ctx context.Context, id model.QueryID) (model.Query, error)
	GetResultPositions(ctx context.Context, id model.QueryID) ([]int, error)
}

// GenerateResults fans out a [model.JobNameGenerateResult] job for every position
// that doesn't yet have a row. Cheap and idempotent: safe to re-enqueue on every page load.
func GenerateResults(log *slog.Logger, db generateResultsDB, queue *goqite.Queue) jobs.Func {
	return jobs.WithTracing("jobs.GenerateResults", func(ctx context.Context, m []byte) error {
		var jd model.GenerateResultsJobData
		if err := json.Unmarshal(m, &jd); err != nil {
			return err
		}

		trace.SpanFromContext(ctx).SetAttributes(attribute.String("query.id", string(jd.QueryID)))

		q, err := db.GetQuery(ctx, jd.QueryID)
		if err != nil {
			log.Error("Error getting query for GenerateResults", "error", err, "query_id", jd.QueryID)
			return err
		}

		positions, err := db.GetResultPositions(ctx, q.ID)
		if err != nil {
			return err
		}
		filled := make(map[int]bool, len(positions))
		for _, p := range positions {
			filled[p] = true
		}

		log.Info("Fanning out result jobs", "query", q.Text, "already_filled", len(filled))

		for i := 0; i < resultsPerQuery; i++ {
			if filled[i] {
				continue
			}
			body, err := json.Marshal(model.GenerateResultJobData{QueryID: q.ID, Position: i})
			if err != nil {
				return err
			}
			if err := jobs.Create(ctx, queue, model.JobNameGenerateResult.String(), goqite.Message{
				Body:     body,
				Priority: 1,
			}); err != nil {
				log.Error("Error enqueueing generate-result job", "error", err, "position", i)
				return err
			}
		}
		return nil
	})
}

type generateResultDB interface {
	GetQuery(ctx context.Context, id model.QueryID) (model.Query, error)
	GetResults(ctx context.Context, id model.QueryID) ([]model.Result, error)
	InsertResult(ctx context.Context, r model.Result) error
}

type resultGenerator interface {
	GenerateResult(ctx context.Context, query string, position int, existingTitles []string) (llm.Result, error)
}

// GenerateResult fabricates a single search result for a query and position and inserts it.
// The insert uses on-conflict-do-nothing so concurrent runs of the same position are harmless.
func GenerateResult(log *slog.Logger, db generateResultDB, gen resultGenerator, _ *goqite.Queue) jobs.Func {
	return jobs.WithTracing("jobs.GenerateResult", func(ctx context.Context, m []byte) error {
		var jd model.GenerateResultJobData
		if err := json.Unmarshal(m, &jd); err != nil {
			return err
		}

		trace.SpanFromContext(ctx).SetAttributes(
			attribute.String("query.id", string(jd.QueryID)),
			attribute.Int("result.position", jd.Position),
		)

		q, err := db.GetQuery(ctx, jd.QueryID)
		if err != nil {
			return err
		}

		// Collect titles already produced so the model picks something distinct.
		existing, err := db.GetResults(ctx, q.ID)
		if err != nil {
			return err
		}
		titles := make([]string, 0, len(existing))
		for _, r := range existing {
			titles = append(titles, r.Title)
		}

		log.Info("Fabricating result", "query", q.Text, "position", jd.Position)

		llmCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
		defer cancel()

		r, err := gen.GenerateResult(llmCtx, q.Text, jd.Position, titles)
		if err != nil {
			log.Error("Error fabricating result", "error", err, "query", q.Text, "position", jd.Position)
			return err
		}

		return db.InsertResult(ctx, model.Result{
			QueryID:     q.ID,
			Position:    jd.Position,
			Title:       r.Title,
			DisplayURL:  r.DisplayURL,
			Description: r.Description,
		})
	})
}

type generateWebsiteDB interface {
	GetResult(ctx context.Context, id model.ResultID) (model.Result, error)
	GetQuery(ctx context.Context, id model.QueryID) (model.Query, error)
	InsertWebsite(ctx context.Context, id model.ResultID, html string) error
}

type websiteGenerator interface {
	GenerateWebsite(ctx context.Context, query, title, displayURL, description string) (string, error)
}

// GenerateWebsite fabricates a full standalone HTML document for a single result and stores it.
func GenerateWebsite(log *slog.Logger, db generateWebsiteDB, gen websiteGenerator) jobs.Func {
	return jobs.WithTracing("jobs.GenerateWebsite", func(ctx context.Context, m []byte) error {
		var jd model.GenerateWebsiteJobData
		if err := json.Unmarshal(m, &jd); err != nil {
			return err
		}

		trace.SpanFromContext(ctx).SetAttributes(attribute.String("result.id", string(jd.ResultID)))

		r, err := db.GetResult(ctx, jd.ResultID)
		if err != nil {
			return err
		}
		q, err := db.GetQuery(ctx, r.QueryID)
		if err != nil {
			return err
		}

		log.Info("Fabricating website", "query", q.Text, "title", r.Title)

		llmCtx, cancel := context.WithTimeout(ctx, 3*time.Minute)
		defer cancel()

		html, err := gen.GenerateWebsite(llmCtx, q.Text, r.Title, r.DisplayURL, r.Description)
		if err != nil {
			log.Error("Error fabricating website", "error", err, "result_id", r.ID)
			return err
		}

		return db.InsertWebsite(ctx, r.ID, html)
	})
}
