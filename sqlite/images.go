package sqlite

import (
	"context"
	"errors"

	"maragu.dev/glue/sql"

	"app/model"
)

// GetImage returns the cached image row for the given path hash.
// Returns [model.ErrorImageNotFound] if no such row exists.
func (d *Database) GetImage(ctx context.Context, pathHash string) (model.Image, error) {
	var img model.Image
	if err := d.H.Get(ctx, &img, `select * from images where path_hash = ?`, pathHash); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return img, model.ErrorImageNotFound
		}
		return img, err
	}
	return img, nil
}

// InsertImage inserts a cached image row.
// If a row already exists for the same path_hash it is left untouched (first write wins).
func (d *Database) InsertImage(ctx context.Context, img model.Image) error {
	return d.H.Exec(ctx,
		`insert into images (path_hash, path, mime_type, data)
		 values (?, ?, ?, ?)
		 on conflict (path_hash) do nothing`,
		img.PathHash, img.Path, img.MimeType, img.Data)
}
