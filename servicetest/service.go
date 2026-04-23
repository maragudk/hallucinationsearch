// Package servicetest provides testing helpers for the service package.
package servicetest

import (
	"testing"

	"app/service"
	"app/sqlitetest"
)

type NewFatOption func(*newFatOptions)

type newFatOptions struct {
	dbOpts []sqlitetest.NewDatabaseOption
}

// WithSQLiteTestOptions passes options to the underlying sqlitetest.NewDatabase call.
func WithSQLiteTestOptions(opts ...sqlitetest.NewDatabaseOption) NewFatOption {
	return func(o *newFatOptions) {
		o.dbOpts = append(o.dbOpts, opts...)
	}
}

// NewFat for testing, with optional options.
// The LLM client and queue are nil; tests that need them should substitute directly.
func NewFat(t *testing.T, opts ...NewFatOption) *service.Fat {
	t.Helper()

	o := &newFatOptions{}
	for _, opt := range opts {
		opt(o)
	}

	db := sqlitetest.NewDatabase(t, o.dbOpts...)
	return service.NewFat(service.NewFatOptions{
		Database: db,
		Queue:    db.H.JobsQ,
	})
}
