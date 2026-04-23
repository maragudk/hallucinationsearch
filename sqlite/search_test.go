package sqlite_test

import (
	"strings"
	"testing"

	"maragu.dev/is"

	"app/model"
	"app/sqlitetest"
)

func TestUpsertQuery(t *testing.T) {
	t.Run("inserts a new query and returns the row", func(t *testing.T) {
		db := sqlitetest.NewDatabase(t)

		q, err := db.UpsertQuery(t.Context(), "cats")
		is.NotError(t, err)
		is.True(t, strings.HasPrefix(string(q.ID), "q_"), "id should have q_ prefix, got: "+string(q.ID))
		is.Equal(t, "cats", q.Text)
	})

	t.Run("is idempotent -- same text returns the same row", func(t *testing.T) {
		db := sqlitetest.NewDatabase(t)

		first, err := db.UpsertQuery(t.Context(), "dogs")
		is.NotError(t, err)

		second, err := db.UpsertQuery(t.Context(), "dogs")
		is.NotError(t, err)

		is.Equal(t, first.ID, second.ID)
	})
}

func TestInsertResultAndGetResults(t *testing.T) {
	t.Run("round-trips a result", func(t *testing.T) {
		db := sqlitetest.NewDatabase(t)

		q, err := db.UpsertQuery(t.Context(), "cats")
		is.NotError(t, err)

		err = db.InsertResult(t.Context(), model.Result{
			QueryID:     q.ID,
			Position:    0,
			Title:       "Ten Secrets Cats Do Not Want You to Know",
			DisplayURL:  "gatopedia.org > wiki > Secrets",
			Description: "A confidential primer.",
		})
		is.NotError(t, err)

		rs, err := db.GetResults(t.Context(), q.ID)
		is.NotError(t, err)
		is.Equal(t, 1, len(rs))
		is.Equal(t, "Ten Secrets Cats Do Not Want You to Know", rs[0].Title)
		is.Equal(t, 0, rs[0].Position)
	})

	t.Run("second insert at the same position is a no-op", func(t *testing.T) {
		db := sqlitetest.NewDatabase(t)

		q, err := db.UpsertQuery(t.Context(), "cats")
		is.NotError(t, err)

		err = db.InsertResult(t.Context(), model.Result{QueryID: q.ID, Position: 0, Title: "First", DisplayURL: "a", Description: "b"})
		is.NotError(t, err)

		err = db.InsertResult(t.Context(), model.Result{QueryID: q.ID, Position: 0, Title: "Second", DisplayURL: "a", Description: "b"})
		is.NotError(t, err)

		rs, err := db.GetResults(t.Context(), q.ID)
		is.NotError(t, err)
		is.Equal(t, 1, len(rs))
		is.Equal(t, "First", rs[0].Title, "first write should win")
	})

	t.Run("GetResultPositions returns the filled set", func(t *testing.T) {
		db := sqlitetest.NewDatabase(t)

		q, err := db.UpsertQuery(t.Context(), "cats")
		is.NotError(t, err)

		for _, p := range []int{0, 2, 5} {
			err = db.InsertResult(t.Context(), model.Result{QueryID: q.ID, Position: p, Title: "t", DisplayURL: "u", Description: "d"})
			is.NotError(t, err)
		}

		ps, err := db.GetResultPositions(t.Context(), q.ID)
		is.NotError(t, err)
		is.EqualSlice(t, []int{0, 2, 5}, ps)
	})
}

func TestInsertAndGetWebsite(t *testing.T) {
	t.Run("round-trips a fabricated website", func(t *testing.T) {
		db := sqlitetest.NewDatabase(t)

		q, err := db.UpsertQuery(t.Context(), "cats")
		is.NotError(t, err)

		err = db.InsertResult(t.Context(), model.Result{QueryID: q.ID, Position: 0, Title: "T", DisplayURL: "U", Description: "D"})
		is.NotError(t, err)

		rs, err := db.GetResults(t.Context(), q.ID)
		is.NotError(t, err)
		is.Equal(t, 1, len(rs))

		_, err = db.GetWebsite(t.Context(), rs[0].ID)
		is.Error(t, model.ErrorWebsiteNotFound, err)

		err = db.InsertWebsite(t.Context(), rs[0].ID, "<!doctype html><html><body>hi</body></html>")
		is.NotError(t, err)

		w, err := db.GetWebsite(t.Context(), rs[0].ID)
		is.NotError(t, err)
		is.Equal(t, rs[0].ID, w.ResultID)
		is.True(t, strings.Contains(w.HTML, "<body>"), w.HTML)
	})
}

func TestGetResult(t *testing.T) {
	t.Run("returns ErrorResultNotFound when missing", func(t *testing.T) {
		db := sqlitetest.NewDatabase(t)

		_, err := db.GetResult(t.Context(), "r_00000000000000000000000000000000")
		is.Error(t, model.ErrorResultNotFound, err)
	})
}

func TestInsertAdAndGetAds(t *testing.T) {
	t.Run("round-trips an ad", func(t *testing.T) {
		db := sqlitetest.NewDatabase(t)

		q, err := db.UpsertQuery(t.Context(), "cats")
		is.NotError(t, err)

		err = db.InsertAd(t.Context(), model.Ad{
			QueryID:     q.ID,
			Position:    0,
			Title:       "Premium Cat Food",
			DisplayURL:  "whiskerfeast.example > shop",
			Description: "Finally, a meal your cat won't pretend to hate.",
			Sponsor:     "WhiskerFeast",
			CTA:         "Shop now",
		})
		is.NotError(t, err)

		ads, err := db.GetAds(t.Context(), q.ID)
		is.NotError(t, err)
		is.Equal(t, 1, len(ads))
		is.Equal(t, "Premium Cat Food", ads[0].Title)
		is.Equal(t, "WhiskerFeast", ads[0].Sponsor)
		is.Equal(t, "Shop now", ads[0].CTA)
		is.Equal(t, 0, ads[0].Position)
		is.True(t, strings.HasPrefix(string(ads[0].ID), "a_"), "id should have a_ prefix, got: "+string(ads[0].ID))
	})

	t.Run("second insert at the same position is a no-op", func(t *testing.T) {
		db := sqlitetest.NewDatabase(t)

		q, err := db.UpsertQuery(t.Context(), "cats")
		is.NotError(t, err)

		err = db.InsertAd(t.Context(), model.Ad{QueryID: q.ID, Position: 0, Title: "First", DisplayURL: "a", Description: "b", Sponsor: "s1", CTA: "go"})
		is.NotError(t, err)

		err = db.InsertAd(t.Context(), model.Ad{QueryID: q.ID, Position: 0, Title: "Second", DisplayURL: "a", Description: "b", Sponsor: "s2", CTA: "go"})
		is.NotError(t, err)

		ads, err := db.GetAds(t.Context(), q.ID)
		is.NotError(t, err)
		is.Equal(t, 1, len(ads))
		is.Equal(t, "First", ads[0].Title, "first write should win")
	})

	t.Run("GetAdPositions returns the filled set", func(t *testing.T) {
		db := sqlitetest.NewDatabase(t)

		q, err := db.UpsertQuery(t.Context(), "cats")
		is.NotError(t, err)

		for _, p := range []int{0, 2} {
			err = db.InsertAd(t.Context(), model.Ad{QueryID: q.ID, Position: p, Title: "t", DisplayURL: "u", Description: "d", Sponsor: "s", CTA: "c"})
			is.NotError(t, err)
		}

		ps, err := db.GetAdPositions(t.Context(), q.ID)
		is.NotError(t, err)
		is.EqualSlice(t, []int{0, 2}, ps)
	})
}

func TestInsertAndGetAdWebsite(t *testing.T) {
	t.Run("round-trips a fabricated ad website", func(t *testing.T) {
		db := sqlitetest.NewDatabase(t)

		q, err := db.UpsertQuery(t.Context(), "cats")
		is.NotError(t, err)

		err = db.InsertAd(t.Context(), model.Ad{QueryID: q.ID, Position: 0, Title: "T", DisplayURL: "U", Description: "D", Sponsor: "S", CTA: "C"})
		is.NotError(t, err)

		ads, err := db.GetAds(t.Context(), q.ID)
		is.NotError(t, err)
		is.Equal(t, 1, len(ads))

		_, err = db.GetAdWebsite(t.Context(), ads[0].ID)
		is.Error(t, model.ErrorAdWebsiteNotFound, err)

		err = db.InsertAdWebsite(t.Context(), ads[0].ID, "<!doctype html><html><body>ad</body></html>")
		is.NotError(t, err)

		w, err := db.GetAdWebsite(t.Context(), ads[0].ID)
		is.NotError(t, err)
		is.Equal(t, ads[0].ID, w.AdID)
		is.True(t, strings.Contains(w.HTML, "<body>"), w.HTML)
	})
}

func TestGetAd(t *testing.T) {
	t.Run("returns ErrorAdNotFound when missing", func(t *testing.T) {
		db := sqlitetest.NewDatabase(t)

		_, err := db.GetAd(t.Context(), "a_00000000000000000000000000000000")
		is.Error(t, model.ErrorAdNotFound, err)
	})
}
