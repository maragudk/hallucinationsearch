// Package service provides business logic.
// See https://www.alexedwards.net/blog/the-fat-service-pattern
package service

import (
	"maragu.dev/goqite"

	"app/llm"
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

// LLM returns the underlying [llm.Client] for model calls that bypass
// the jobs queue (e.g. the synchronous /image handler).
func (f *Fat) LLM() *llm.Client {
	return f.llm
}

// Queue returns the jobs queue for enqueueing new work.
func (f *Fat) Queue() *goqite.Queue {
	return f.queue
}
