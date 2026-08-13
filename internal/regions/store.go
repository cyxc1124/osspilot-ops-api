package regions

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	ErrConflict = errors.New("region code exists")
	ErrDefault  = errors.New("cannot delete default region")
)

type Region struct {
	ID           int64
	Code         string
	Name         string
	S3Endpoint   string
	S3RegionName string
	IsDefault    bool
	Status       string
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

type Store struct {
	pool *pgxpool.Pool
}

func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

const cols = `id, code, name, s3_endpoint, s3_region_name, is_default, status, created_at, updated_at`

type rowScanner interface {
	Scan(dest ...any) error
}

func scan(row rowScanner) (Region, error) {
	var r Region
	err := row.Scan(&r.ID, &r.Code, &r.Name, &r.S3Endpoint, &r.S3RegionName, &r.IsDefault, &r.Status, &r.CreatedAt, &r.UpdatedAt)
	return r, err
}

func (s *Store) List(ctx context.Context) ([]Region, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+cols+` FROM storage_regions ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("list regions: %w", err)
	}
	defer rows.Close()
	var out []Region
	for rows.Next() {
		r, err := scan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *Store) GetByID(ctx context.Context, id int64) (*Region, error) {
	r, err := scan(s.pool.QueryRow(ctx, `SELECT `+cols+` FROM storage_regions WHERE id = $1`, id))
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get region: %w", err)
	}
	return &r, nil
}

func (s *Store) Insert(ctx context.Context, r *Region) (*Region, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	if r.IsDefault {
		if _, err := tx.Exec(ctx, `UPDATE storage_regions SET is_default = false WHERE is_default`); err != nil {
			return nil, err
		}
	}
	out, err := scan(tx.QueryRow(ctx, `
		INSERT INTO storage_regions (code, name, s3_endpoint, s3_region_name, is_default, status, created_at, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$7)
		RETURNING `+cols, r.Code, r.Name, r.S3Endpoint, r.S3RegionName, r.IsDefault, r.Status, r.CreatedAt))
	if isUnique(err) {
		return nil, ErrConflict
	}
	if err != nil {
		return nil, fmt.Errorf("insert region: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return &out, nil
}

func (s *Store) Update(ctx context.Context, r *Region, setDefault *bool) (*Region, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	if setDefault != nil && *setDefault {
		if _, err := tx.Exec(ctx, `UPDATE storage_regions SET is_default = false WHERE is_default AND id <> $1`, r.ID); err != nil {
			return nil, err
		}
	}
	out, err := scan(tx.QueryRow(ctx, `
		UPDATE storage_regions
		SET name = $2, s3_endpoint = $3, s3_region_name = $4, is_default = $5, status = $6, updated_at = $7
		WHERE id = $1
		RETURNING `+cols, r.ID, r.Name, r.S3Endpoint, r.S3RegionName, r.IsDefault, r.Status, r.UpdatedAt))
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("update region: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return &out, nil
}

func (s *Store) Delete(ctx context.Context, id int64) error {
	r, err := s.GetByID(ctx, id)
	if err != nil {
		return err
	}
	if r == nil {
		return pgx.ErrNoRows
	}
	if r.IsDefault {
		return ErrDefault
	}
	// ponytail: tenant_count stays 0 until O5 accounts exist
	tag, err := s.pool.Exec(ctx, `DELETE FROM storage_regions WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("delete region: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
	return nil
}

func isUnique(err error) bool {
	var pe *pgconn.PgError
	return errors.As(err, &pe) && pe.Code == "23505"
}
