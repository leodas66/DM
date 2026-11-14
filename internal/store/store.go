// Package store persists downloads and segment progress in SQLite
// (modernc.org/sqlite, pure Go). The Manager is its only writer; the store
// itself holds no cache and no mutable state beyond the connection pool.
package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	_ "modernc.org/sqlite"

	"github.com/vishwa/dm/internal/model"
)

// ErrNotFound means no download has that ID.
var ErrNotFound = errors.New("store: download not found")

// migrations are applied in order; the schema_version table records how many
// have run. Never edit a shipped migration, append a new one.
var migrations = []string{
	`CREATE TABLE downloads (
		id            TEXT PRIMARY KEY,
		url           TEXT NOT NULL,
		final_url     TEXT NOT NULL DEFAULT '',
		filename      TEXT NOT NULL,
		dir           TEXT NOT NULL,
		size          INTEGER NOT NULL DEFAULT 0,
		accepts_ranges INTEGER NOT NULL DEFAULT 0,
		etag          TEXT NOT NULL DEFAULT '',
		last_modified TEXT NOT NULL DEFAULT '',
		status        TEXT NOT NULL,
		error         TEXT NOT NULL DEFAULT '',
		created_at    INTEGER NOT NULL
	);
	CREATE INDEX downloads_created_at ON downloads(created_at);

	CREATE TABLE segments (
		download_id TEXT NOT NULL REFERENCES downloads(id) ON DELETE CASCADE,
		idx         INTEGER NOT NULL,
		start       INTEGER NOT NULL,
		end         INTEGER NOT NULL,
		downloaded  INTEGER NOT NULL DEFAULT 0,
		status      TEXT NOT NULL,
		PRIMARY KEY (download_id, idx)
	);`,
	`ALTER TABLE downloads ADD COLUMN category_id TEXT NOT NULL DEFAULT '';
	ALTER TABLE downloads ADD COLUMN start_at INTEGER NOT NULL DEFAULT 0;

	CREATE TABLE categories (
		id          TEXT PRIMARY KEY,
		name        TEXT NOT NULL,
		default_dir TEXT NOT NULL,
		created_at  INTEGER NOT NULL
	);
	CREATE INDEX categories_name ON categories(name);`,
	`CREATE TABLE settings (
		key   TEXT PRIMARY KEY,
		value TEXT NOT NULL
	);`,
}

// Store is a handle to the SQLite database.
type Store struct {
	db *sql.DB
}

// Open opens (creating if needed) the database at path and applies migrations.
func Open(path string) (*Store, error) {
	dsn := "file:" + path + "?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("store: open: %w", err)
	}
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("store: ping: %w", err)
	}
	if err := migrate(context.Background(), db); err != nil {
		db.Close()
		return nil, err
	}
	return &Store{db: db}, nil
}

func migrate(ctx context.Context, db *sql.DB) error {
	if _, err := db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_version (version INTEGER NOT NULL)`); err != nil {
		return fmt.Errorf("store: schema_version: %w", err)
	}
	var version int
	err := db.QueryRowContext(ctx, `SELECT version FROM schema_version LIMIT 1`).Scan(&version)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		if _, err := db.ExecContext(ctx, `INSERT INTO schema_version (version) VALUES (0)`); err != nil {
			return fmt.Errorf("store: init schema_version: %w", err)
		}
		version = 0
	case err != nil:
		return fmt.Errorf("store: read schema_version: %w", err)
	}

	for i := version; i < len(migrations); i++ {
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, migrations[i]); err != nil {
			tx.Rollback()
			return fmt.Errorf("store: migration %d: %w", i+1, err)
		}
		if _, err := tx.ExecContext(ctx, `UPDATE schema_version SET version = ?`, i+1); err != nil {
			tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}

// Close closes the database.
func (s *Store) Close() error { return s.db.Close() }

// Save upserts a download and replaces its segment rows in one transaction.
func (s *Store) Save(ctx context.Context, d model.Download) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	_, err = tx.ExecContext(ctx, `
		INSERT INTO downloads (id, url, final_url, filename, dir, category_id, start_at, size, accepts_ranges,
		                       etag, last_modified, status, error, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			url = excluded.url,
			final_url = excluded.final_url,
			filename = excluded.filename,
			dir = excluded.dir,
			category_id = excluded.category_id,
			start_at = excluded.start_at,
			size = excluded.size,
			accepts_ranges = excluded.accepts_ranges,
			etag = excluded.etag,
			last_modified = excluded.last_modified,
			status = excluded.status,
			error = excluded.error`,
		d.ID, d.URL, d.FinalURL, d.Filename, d.Dir, d.CategoryID, d.StartAt.UnixNano(), d.Size, boolInt(d.AcceptsRanges),
		d.ETag, d.LastModified, string(d.Status), d.Error, d.CreatedAt.UnixNano(),
	)
	if err != nil {
		return fmt.Errorf("store: upsert download: %w", err)
	}

	if _, err := tx.ExecContext(ctx, `DELETE FROM segments WHERE download_id = ?`, d.ID); err != nil {
		return fmt.Errorf("store: clear segments: %w", err)
	}
	for _, seg := range d.Segments {
		_, err := tx.ExecContext(ctx, `
			INSERT INTO segments (download_id, idx, start, end, downloaded, status)
			VALUES (?, ?, ?, ?, ?, ?)`,
			d.ID, seg.Index, seg.Start, seg.End, seg.Downloaded, string(seg.Status))
		if err != nil {
			return fmt.Errorf("store: insert segment %d: %w", seg.Index, err)
		}
	}
	return tx.Commit()
}

// Get returns one download with its segments.
func (s *Store) Get(ctx context.Context, id string) (model.Download, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, url, final_url, filename, dir, category_id, start_at, size, accepts_ranges, etag,
		       last_modified, status, error, created_at
		FROM downloads WHERE id = ?`, id)
	d, err := scanDownload(row)
	if errors.Is(err, sql.ErrNoRows) {
		return model.Download{}, ErrNotFound
	}
	if err != nil {
		return model.Download{}, err
	}
	segs, err := s.segmentsFor(ctx, id)
	if err != nil {
		return model.Download{}, err
	}
	d.Segments = segs
	return d, nil
}

// List returns every download, oldest first, with segments.
func (s *Store) List(ctx context.Context) ([]model.Download, error) {
	return s.query(ctx, `
		SELECT id, url, final_url, filename, dir, category_id, start_at, size, accepts_ranges, etag,
		       last_modified, status, error, created_at
		FROM downloads ORDER BY created_at, id`)
}

// Unfinished returns downloads that have not reached a terminal state.
func (s *Store) Unfinished(ctx context.Context) ([]model.Download, error) {
	return s.query(ctx, `
		SELECT id, url, final_url, filename, dir, category_id, start_at, size, accepts_ranges, etag,
		       last_modified, status, error, created_at
		FROM downloads
		WHERE status NOT IN ('completed', 'failed', 'canceled')
		ORDER BY created_at, id`)
}

// Delete removes a download; its segments go with it via ON DELETE CASCADE.
func (s *Store) Delete(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM downloads WHERE id = ?`, id)
	return err
}

// SaveCategory upserts a category.
func (s *Store) SaveCategory(ctx context.Context, c model.Category) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO categories (id, name, default_dir, created_at)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			name = excluded.name,
			default_dir = excluded.default_dir`,
		c.ID, c.Name, c.DefaultDir, c.CreatedAt.UnixNano(),
	)
	if err != nil {
		return fmt.Errorf("store: upsert category: %w", err)
	}
	return nil
}

// GetCategory returns one category by id.
func (s *Store) GetCategory(ctx context.Context, id string) (model.Category, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, name, default_dir, created_at
		FROM categories WHERE id = ?`, id)
	var c model.Category
	var createdNS int64
	err := row.Scan(&c.ID, &c.Name, &c.DefaultDir, &createdNS)
	if errors.Is(err, sql.ErrNoRows) {
		return model.Category{}, ErrNotFound
	}
	if err != nil {
		return model.Category{}, err
	}
	c.CreatedAt = time.Unix(0, createdNS).UTC()
	return c, nil
}

// ListCategories returns every category, oldest first.
func (s *Store) ListCategories(ctx context.Context) ([]model.Category, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, name, default_dir, created_at
		FROM categories ORDER BY created_at, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []model.Category
	for rows.Next() {
		var c model.Category
		var createdNS int64
		if err := rows.Scan(&c.ID, &c.Name, &c.DefaultDir, &createdNS); err != nil {
			return nil, err
		}
		c.CreatedAt = time.Unix(0, createdNS).UTC()
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(out) == 0 {
		return out, nil
	}
	return out, nil
}

// DeleteCategory removes a category. Downloads referencing it keep their
// category_id but the category is gone.
func (s *Store) DeleteCategory(ctx context.Context, id string) error {
	if _, err := s.GetCategory(ctx, id); err != nil {
		return err
	}
	_, err := s.db.ExecContext(ctx, `DELETE FROM categories WHERE id = ?`, id)
	return err
}

// GetSetting returns a stored setting value, or ErrNotFound when it was never
// written. Values are opaque strings: the store does not interpret them.
func (s *Store) GetSetting(ctx context.Context, key string) (string, error) {
	var value string
	err := s.db.QueryRowContext(ctx, `SELECT value FROM settings WHERE key = ?`, key).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", err
	}
	return value, nil
}

// PutSetting writes one setting value, replacing any previous value.
func (s *Store) PutSetting(ctx context.Context, key, value string) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO settings (key, value) VALUES (?, ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value`, key, value)
	return err
}

func (s *Store) query(ctx context.Context, q string) ([]model.Download, error) {
	rows, err := s.db.QueryContext(ctx, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []model.Download
	for rows.Next() {
		d, err := scanDownload(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(out) == 0 {
		return out, nil
	}

	segRows, err := s.db.QueryContext(ctx, `
		SELECT download_id, idx, start, end, downloaded, status
		FROM segments ORDER BY download_id, idx`)
	if err != nil {
		return nil, err
	}
	defer segRows.Close()

	byID := make(map[string][]model.Segment, len(out))
	index := make(map[string]int, len(out))
	for i, d := range out {
		index[d.ID] = i
	}
	for segRows.Next() {
		var id string
		var seg model.Segment
		var status string
		if err := segRows.Scan(&id, &seg.Index, &seg.Start, &seg.End, &seg.Downloaded, &status); err != nil {
			return nil, err
		}
		seg.Status = model.SegmentStatus(status)
		byID[id] = append(byID[id], seg)
	}
	if err := segRows.Err(); err != nil {
		return nil, err
	}
	for id, segs := range byID {
		if i, ok := index[id]; ok {
			out[i].Segments = segs
		}
	}
	return out, nil
}

func (s *Store) segmentsFor(ctx context.Context, id string) ([]model.Segment, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT idx, start, end, downloaded, status
		FROM segments WHERE download_id = ? ORDER BY idx`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var segs []model.Segment
	for rows.Next() {
		var seg model.Segment
		var status string
		if err := rows.Scan(&seg.Index, &seg.Start, &seg.End, &seg.Downloaded, &status); err != nil {
			return nil, err
		}
		seg.Status = model.SegmentStatus(status)
		segs = append(segs, seg)
	}
	return segs, rows.Err()
}

type scanner interface {
	Scan(dest ...any) error
}

func scanDownload(sc scanner) (model.Download, error) {
	var (
		d         model.Download
		ranges    int
		status    string
		startNS   int64
		createdNS int64
	)
	err := sc.Scan(&d.ID, &d.URL, &d.FinalURL, &d.Filename, &d.Dir, &d.CategoryID, &startNS, &d.Size, &ranges,
		&d.ETag, &d.LastModified, &status, &d.Error, &createdNS)
	if err != nil {
		return model.Download{}, err
	}
	d.AcceptsRanges = ranges != 0
	d.Status = model.Status(status)
	d.StartAt = time.Unix(0, startNS).UTC()
	d.CreatedAt = time.Unix(0, createdNS).UTC()
	return d, nil
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
