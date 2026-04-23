package jobs

import (
	"log/slog"

	"maragu.dev/glue/jobs"
	"maragu.dev/goqite"

	"app/llm"
	"app/model"
	"app/sqlite"
)

type RegisterOpts struct {
	Database *sqlite.Database
	LLM      *llm.Client
	Log      *slog.Logger
	Queue    *goqite.Queue
}

// Register all available jobs with the given dependencies.
func Register(r *jobs.Runner, opts RegisterOpts) {
	if opts.Log == nil {
		opts.Log = slog.New(slog.DiscardHandler)
	}

	r.Register(model.JobNameGenerateResults.String(), GenerateResults(opts.Log, opts.Database, opts.Queue))
	r.Register(model.JobNameGenerateResult.String(), GenerateResult(opts.Log, opts.Database, opts.LLM, opts.Queue))
	r.Register(model.JobNameGenerateWebsite.String(), GenerateWebsite(opts.Log, opts.Database, opts.LLM))
	r.Register(model.JobNameGenerateAds.String(), GenerateAds(opts.Log, opts.Database, opts.Queue))
	r.Register(model.JobNameGenerateAd.String(), GenerateAd(opts.Log, opts.Database, opts.LLM, opts.Queue))
	r.Register(model.JobNameGenerateAdWebsite.String(), GenerateAdWebsite(opts.Log, opts.Database, opts.LLM))
}
