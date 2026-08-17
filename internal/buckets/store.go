package buckets

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrConflict = errors.New("bucket exists")

type RegionBrief struct {
	ID   int64
	Code string
	Name string
}

type Bucket struct {
	ID              int64
	BucketName      string
	DisplayName     *string
	StorageRegionID *int64
	StorageRegion   *RegionBrief
	QuotaBytes      *int64
	ObjectLimit     *int64
	Status          string
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

type Store struct {
	pool *pgxpool.Pool
}

func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

const cols = `b.id, b.bucket_name, b.display_name, b.storage_region_id, b.quota_bytes, b.object_limit,
	b.status, b.created_at, b.updated_at, r.code, r.name`

const fromJoin = `FROM platform_buckets b LEFT JOIN storage_regions r ON r.id = b.storage_region_id`

func scan(row interface{ Scan(dest ...any) error }) (Bucket, error) {
	var b Bucket
	var code, name *string
	err := row.Scan(
		&b.ID, &b.BucketName, &b.DisplayName, &b.StorageRegionID, &b.QuotaBytes, &b.ObjectLimit,
		&b.Status, &b.CreatedAt, &b.UpdatedAt, &code, &name,
	)
	if err != nil {
		return b, err
	}
	if b.StorageRegionID != nil && code != nil && name != nil {
		b.StorageRegion = &RegionBrief{ID: *b.StorageRegionID, Code: *code, Name: *name}
	}
	return b, nil
}

func (s *Store) List(ctx context.Context) ([]Bucket, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+cols+` `+fromJoin+` ORDER BY b.bucket_name`)
	if err != nil {
		return nil, fmt.Errorf("list buckets: %w", err)
	}
	defer rows.Close()
	var out []Bucket
	for rows.Next() {
		b, err := scan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

func (s *Store) GetByID(ctx context.Context, id int64) (*Bucket, error) {
	b, err := scan(s.pool.QueryRow(ctx, `SELECT `+cols+` `+fromJoin+` WHERE b.id = $1`, id))
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get bucket: %w", err)
	}
	return &b, nil
}

func (s *Store) GetByIDs(ctx context.Context, ids []int64) ([]Bucket, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	rows, err := s.pool.Query(ctx, `SELECT `+cols+` `+fromJoin+` WHERE b.id = ANY($1)`, ids)
	if err != nil {
		return nil, fmt.Errorf("get buckets: %w", err)
	}
	defer rows.Close()
	var out []Bucket
	for rows.Next() {
		b, err := scan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

func (s *Store) NameSet(ctx context.Context) (map[string]struct{}, error) {
	rows, err := s.pool.Query(ctx, `SELECT bucket_name FROM platform_buckets`)
	if err != nil {
		return nil, fmt.Errorf("bucket names: %w", err)
	}
	defer rows.Close()
	out := map[string]struct{}{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		out[name] = struct{}{}
	}
	return out, rows.Err()
}

func (s *Store) FirstAccountID(ctx context.Context, bucketID int64) (int64, error) {
	var id int64
	err := s.pool.QueryRow(ctx, `
		SELECT account_id FROM account_bucket_grants WHERE bucket_id = $1 ORDER BY account_id LIMIT 1`, bucketID).Scan(&id)
	if err == pgx.ErrNoRows {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("first account: %w", err)
	}
	return id, nil
}

func (s *Store) Insert(ctx context.Context, name string, display *string, regionID, quota, objects *int64, at time.Time) (*Bucket, error) {
	var id int64
	err := s.pool.QueryRow(ctx, `
		INSERT INTO platform_buckets (bucket_name, display_name, storage_region_id, quota_bytes, object_limit, status, created_at, updated_at)
		VALUES ($1,$2,$3,$4,$5,'active',$6,$6)
		RETURNING id`, name, display, regionID, quota, objects, at).Scan(&id)
	if isUnique(err) {
		return nil, ErrConflict
	}
	if err != nil {
		return nil, fmt.Errorf("insert bucket: %w", err)
	}
	return s.GetByID(ctx, id)
}

func isUnique(err error) bool {
	var pe *pgconn.PgError
	return errors.As(err, &pe) && pe.Code == "23505"
}
