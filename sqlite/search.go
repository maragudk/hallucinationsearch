package sqlite

import (
	"context"
	"errors"

	"maragu.dev/glue/sql"

	"app/model"
)

// UpsertQuery inserts the given normalised query text, or returns the existing row if it already exists.
// Returns the query row either way.
func (d *Database) UpsertQuery(ctx context.Context, text string) (model.Query, error) {
	var q model.Query
	err := d.H.InTx(ctx, func(ctx context.Context, tx *Tx) error {
		if err := tx.Exec(ctx, `insert into queries (text) values (?) on conflict (text) do nothing`, text); err != nil {
			return err
		}
		return tx.Get(ctx, &q, `select * from queries where text = ?`, text)
	})
	return q, err
}

// GetQueryByText returns the query row with the given normalised text, or [sql.ErrNoRows] if not found.
func (d *Database) GetQueryByText(ctx context.Context, text string) (model.Query, error) {
	var q model.Query
	err := d.H.Get(ctx, &q, `select * from queries where text = ?`, text)
	return q, err
}

// GetResults returns all result rows for the given query, ordered by position ascending.
func (d *Database) GetResults(ctx context.Context, queryID model.QueryID) ([]model.Result, error) {
	var rs []model.Result
	if err := d.H.Select(ctx, &rs, `select * from results where query_id = ? order by position`, queryID); err != nil {
		return nil, err
	}
	return rs, nil
}

// GetResultPositions returns the set of positions already filled for the given query.
func (d *Database) GetResultPositions(ctx context.Context, queryID model.QueryID) ([]int, error) {
	var ps []int
	if err := d.H.Select(ctx, &ps, `select position from results where query_id = ? order by position`, queryID); err != nil {
		return nil, err
	}
	return ps, nil
}

// InsertResult inserts a fabricated search result for the given query and position.
// If a row already exists at that (query_id, position) it is left untouched (first write wins).
func (d *Database) InsertResult(ctx context.Context, r model.Result) error {
	return d.H.Exec(ctx,
		`insert into results (query_id, position, title, display_url, description)
		 values (?, ?, ?, ?, ?)
		 on conflict (query_id, position) do nothing`,
		r.QueryID, r.Position, r.Title, r.DisplayURL, r.Description)
}

// GetResult returns a single result by ID. Returns [model.ErrorResultNotFound] if not found.
func (d *Database) GetResult(ctx context.Context, id model.ResultID) (model.Result, error) {
	var r model.Result
	if err := d.H.Get(ctx, &r, `select * from results where id = ?`, id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return r, model.ErrorResultNotFound
		}
		return r, err
	}
	return r, nil
}

// GetQuery returns the query row with the given ID.
func (d *Database) GetQuery(ctx context.Context, id model.QueryID) (model.Query, error) {
	var q model.Query
	if err := d.H.Get(ctx, &q, `select * from queries where id = ?`, id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return q, model.ErrorQueryNotFound
		}
		return q, err
	}
	return q, nil
}

// GetWebsite returns the fabricated HTML document for the given result.
// Returns [model.ErrorWebsiteNotFound] if not generated yet.
func (d *Database) GetWebsite(ctx context.Context, id model.ResultID) (model.Website, error) {
	var w model.Website
	if err := d.H.Get(ctx, &w, `select * from websites where result_id = ?`, id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return w, model.ErrorWebsiteNotFound
		}
		return w, err
	}
	return w, nil
}

// InsertWebsite inserts a fabricated HTML document for the given result.
// If a row already exists for that result_id it is left untouched (first write wins).
func (d *Database) InsertWebsite(ctx context.Context, id model.ResultID, html string) error {
	return d.H.Exec(ctx,
		`insert into websites (result_id, html) values (?, ?)
		 on conflict (result_id) do nothing`,
		id, html)
}

// GetAds returns all ad rows for the given query, ordered by position ascending.
func (d *Database) GetAds(ctx context.Context, queryID model.QueryID) ([]model.Ad, error) {
	var ads []model.Ad
	if err := d.H.Select(ctx, &ads, `select * from ads where query_id = ? order by position`, queryID); err != nil {
		return nil, err
	}
	return ads, nil
}

// GetAdPositions returns the set of ad positions already filled for the given query.
func (d *Database) GetAdPositions(ctx context.Context, queryID model.QueryID) ([]int, error) {
	var ps []int
	if err := d.H.Select(ctx, &ps, `select position from ads where query_id = ? order by position`, queryID); err != nil {
		return nil, err
	}
	return ps, nil
}

// InsertAd inserts a fabricated sponsored result for the given query and position.
// If a row already exists at that (query_id, position) it is left untouched (first write wins).
func (d *Database) InsertAd(ctx context.Context, a model.Ad) error {
	return d.H.Exec(ctx,
		`insert into ads (query_id, position, title, display_url, description, sponsor, cta)
		 values (?, ?, ?, ?, ?, ?, ?)
		 on conflict (query_id, position) do nothing`,
		a.QueryID, a.Position, a.Title, a.DisplayURL, a.Description, a.Sponsor, a.CTA)
}

// GetAd returns a single ad by ID. Returns [model.ErrorAdNotFound] if not found.
func (d *Database) GetAd(ctx context.Context, id model.AdID) (model.Ad, error) {
	var a model.Ad
	if err := d.H.Get(ctx, &a, `select * from ads where id = ?`, id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return a, model.ErrorAdNotFound
		}
		return a, err
	}
	return a, nil
}

// GetAdWebsite returns the fabricated HTML document for the given ad.
// Returns [model.ErrorAdWebsiteNotFound] if not generated yet.
func (d *Database) GetAdWebsite(ctx context.Context, id model.AdID) (model.AdWebsite, error) {
	var w model.AdWebsite
	if err := d.H.Get(ctx, &w, `select * from ad_websites where ad_id = ?`, id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return w, model.ErrorAdWebsiteNotFound
		}
		return w, err
	}
	return w, nil
}

// InsertAdWebsite inserts a fabricated HTML document for the given ad.
// If a row already exists for that ad_id it is left untouched (first write wins).
func (d *Database) InsertAdWebsite(ctx context.Context, id model.AdID, html string) error {
	return d.H.Exec(ctx,
		`insert into ad_websites (ad_id, html) values (?, ?)
		 on conflict (ad_id) do nothing`,
		id, html)
}
