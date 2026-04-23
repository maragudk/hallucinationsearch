// Package service provides business logic.
// See https://www.alexedwards.net/blog/the-fat-service-pattern
package service

import (
	"context"

	"maragu.dev/goqite"

	"app/llm"
	"app/model"
	"app/sqlite"
)

type Fat struct {
	db    *sqlite.Database
	llm   *llm.Client
	queue *goqite.Queue
}

type NewFatOptions struct {
	Database *sqlite.Database
	LLM      *llm.Client
	Queue    *goqite.Queue
}

func NewFat(opts NewFatOptions) *Fat {
	return &Fat{
		db:    opts.Database,
		llm:   opts.LLM,
		queue: opts.Queue,
	}
}

// DB returns the underlying [sqlite.Database] for read/write access.
func (f *Fat) DB() *sqlite.Database {
	return f.db
}

// Queue returns the jobs queue for enqueueing new work.
func (f *Fat) Queue() *goqite.Queue {
	return f.queue
}

// GetUser satisfies the [userGetter] interface so the auth middleware keeps working
// even though login flows are served by the reverse proxy and not this app.
func (f *Fat) GetUser(ctx context.Context, id model.UserID) (model.User, error) {
	return f.db.GetUser(ctx, id)
}
