package sqlite_test

import (
	"testing"

	"maragu.dev/is"

	"app/model"
	"app/sqlitetest"
)

func TestInsertAndGetImage(t *testing.T) {
	t.Run("round-trips an image", func(t *testing.T) {
		db := sqlitetest.NewDatabase(t)

		img := model.Image{
			PathHash: "abc123",
			Path:     "tabby cat sleeping on books",
			MimeType: "image/png",
			Data:     []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A, 0x00},
		}

		err := db.InsertImage(t.Context(), img)
		is.NotError(t, err)

		got, err := db.GetImage(t.Context(), "abc123")
		is.NotError(t, err)
		is.Equal(t, "abc123", got.PathHash)
		is.Equal(t, "tabby cat sleeping on books", got.Path)
		is.Equal(t, "image/png", got.MimeType)
		is.Equal(t, len(img.Data), len(got.Data))
		for i := range img.Data {
			is.Equal(t, img.Data[i], got.Data[i])
		}
	})

	t.Run("second insert with the same path_hash is a no-op", func(t *testing.T) {
		db := sqlitetest.NewDatabase(t)

		err := db.InsertImage(t.Context(), model.Image{
			PathHash: "hash1",
			Path:     "first",
			MimeType: "image/png",
			Data:     []byte{0x01, 0x02},
		})
		is.NotError(t, err)

		err = db.InsertImage(t.Context(), model.Image{
			PathHash: "hash1",
			Path:     "second",
			MimeType: "image/jpeg",
			Data:     []byte{0xFF, 0xD8, 0xFF},
		})
		is.NotError(t, err)

		got, err := db.GetImage(t.Context(), "hash1")
		is.NotError(t, err)
		is.Equal(t, "first", got.Path, "first write should win")
		is.Equal(t, "image/png", got.MimeType)
		is.Equal(t, 2, len(got.Data))
	})

	t.Run("returns ErrorImageNotFound when missing", func(t *testing.T) {
		db := sqlitetest.NewDatabase(t)

		_, err := db.GetImage(t.Context(), "nonexistent")
		is.Error(t, model.ErrorImageNotFound, err)
	})
}
