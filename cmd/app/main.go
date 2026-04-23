package main

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"maragu.dev/env"
	"maragu.dev/errors"
	"maragu.dev/glue/app"
	gluehttp "maragu.dev/glue/http"
	gluejobs "maragu.dev/glue/jobs"
	"maragu.dev/glue/sql"

	"app/html"
	"app/http"
	"app/jobs"
	"app/llm"
	"app/service"
	"app/sqlite"
)

func main() {
	app.Start(start)
}

func start(ctx context.Context, log *slog.Logger, eg app.Goer) error {
	databaseLog := log.With("component", "sql.Database")

	jobTimeout := env.GetDurationOrDefault("JOB_QUEUE_TIMEOUT", 3*time.Minute)

	db := sqlite.NewDatabase(sqlite.NewDatabaseOptions{
		H: sql.NewHelper(sql.NewHelperOptions{
			JobQueue: sql.JobQueueOptions{
				Timeout: jobTimeout,
			},
			Log: databaseLog,
			SQLite: sql.SQLiteOptions{
				Path: env.GetStringOrDefault("DATABASE_PATH", "app.db"),
			},
		}),
		Log: databaseLog,
	})
	if err := db.H.Connect(ctx); err != nil {
		return errors.Wrap(err, "error connecting to database")
	}

	if err := db.H.MigrateUp(ctx); err != nil {
		return errors.Wrap(err, "error migrating database")
	}

	llmClient := llm.NewClient(llm.NewClientOptions{
		Key:       env.GetStringOrDefault("ANTHROPIC_API_KEY", ""),
		GoogleKey: env.GetStringOrDefault("GOOGLE_API_KEY", ""),
		Log:       log.With("component", "llm.Client"),
	})

	imagesRoot := env.GetStringOrDefault("IMAGES_PATH", "images")
	if err := os.MkdirAll(imagesRoot, 0o755); err != nil {
		return errors.Wrap(err, "error creating images directory")
	}
	absImagesRoot, err := filepath.Abs(imagesRoot)
	if err != nil {
		return errors.Wrap(err, "error resolving images path")
	}
	log.Info("Initialised image store", "path", absImagesRoot)
	imageStore := llm.NewImageStore(absImagesRoot)

	runner := gluejobs.NewRunner(gluejobs.NewRunnerOpts{
		Limit: 8,
		Log:   log.With("component", "jobs.Runner"),
		Queue: db.H.JobsQ,
	})

	baseURL := env.GetStringOrDefault("BASE_URL", "http://localhost:8080")

	jobs.Register(runner, jobs.RegisterOpts{
		Database: db,
		LLM:      llmClient,
		Log:      log.With("component", "jobs"),
		Queue:    db.H.JobsQ,
	})

	svc := service.NewFat(service.NewFatOptions{
		Database:   db,
		LLM:        llmClient,
		Queue:      db.H.JobsQ,
		ImageStore: imageStore,
	})

	// Website fabrication can block the `/site/...` handler for up to ~2 minutes, so
	// give the HTTP server a generous write timeout. The read timeout stays default.
	server := gluehttp.NewServer(gluehttp.NewServerOptions{
		Address:            env.GetStringOrDefault("SERVER_ADDRESS", ":8080"),
		BaseURL:            baseURL,
		CSP:                http.CSP(env.GetBoolOrDefault("CSP_ALLOW_UNSAFE_INLINE", false), env.GetBoolOrDefault("CSP_ALLOW_UNSAFE_EVAL", false)),
		HTMLPage:           html.Page,
		HTTPRouterInjector: http.InjectHTTPRouter(log, svc),
		Log:                log.With("component", "http.Server"),
		SecureCookie:       env.GetBoolOrDefault("SECURE_COOKIE", true),
		WriteTimeout:       3 * time.Minute,
	})

	eg.Go(func() error {
		return server.Start(ctx)
	})

	eg.Go(func() error {
		runner.Start(ctx)
		return nil
	})

	return nil
}
