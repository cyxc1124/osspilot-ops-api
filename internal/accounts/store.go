package accounts

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrConflict = errors.New("username exists")

type RegionBrief struct {
	ID   int64
	Code string
	Name string
}

type Record struct {
	ID               int64
	Username         string
	DisplayName      *string
	Email            *string
	Phone            *string
	Status           string
	QuotaBytes       *int64
	ObjectLimit      *int64
	DailyUploadBytes *int64
	BucketLimit      *int64
	StorageRegionID  *int64
	StorageRegion    *RegionBrief
	LastLoginAt      *time.Time
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

type Store struct {
	pool *pgxpool.Pool
}

func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

const cols = `a.id, a.username, a.display_name, a.email, a.phone, a.status,
	a.quota_bytes, a.object_limit, a.daily_upload_bytes, a.bucket_limit,
	a.storage_region_id, a.last_login_at, a.created_at, a.updated_at,
	r.code, r.name`

func scan(row interface{ Scan(dest ...any) error }) (Record, error) {
	var u Record
	var code, name *string
	err := row.Scan(
		&u.ID, &u.Username, &u.DisplayName, &u.Email, &u.Phone, &u.Status,
		&u.QuotaBytes, &u.ObjectLimit, &u.DailyUploadBytes, &u.BucketLimit,
		&u.StorageRegionID, &u.LastLoginAt, &u.CreatedAt, &u.UpdatedAt,
		&code, &name,
	)
	if err != nil {
		return u, err
	}
	if u.StorageRegionID != nil && code != nil && name != nil {
		u.StorageRegion = &RegionBrief{ID: *u.StorageRegionID, Code: *code, Name: *name}
	}
	return u, nil
}

const fromJoin = `FROM tenant_accounts a LEFT JOIN storage_regions r ON r.id = a.storage_region_id`

func (s *Store) List(ctx context.Context) ([]Record, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+cols+` `+fromJoin+` ORDER BY a.id`)
	if err != nil {
		return nil, fmt.Errorf("list accounts: %w", err)
	}
	defer rows.Close()
	var out []Record
	for rows.Next() {
		u, err := scan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

func (s *Store) GetByID(ctx context.Context, id int64) (*Record, error) {
	u, err := scan(s.pool.QueryRow(ctx, `SELECT `+cols+` `+fromJoin+` WHERE a.id = $1`, id))
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get account: %w", err)
	}
	return &u, nil
}

func (s *Store) Insert(ctx context.Context, username, hash string, displayName, email, phone *string, quota, objects, daily, buckets, regionID *int64, at time.Time) (*Record, error) {
	var id int64
	err := s.pool.QueryRow(ctx, `
		INSERT INTO tenant_accounts (
			username, password_hash, display_name, email, phone, status,
			quota_bytes, object_limit, daily_upload_bytes, bucket_limit, storage_region_id,
			must_change_password, created_at, updated_at
		) VALUES ($1,$2,$3,$4,$5,'active',$6,$7,$8,$9,$10,true,$11,$11)
		RETURNING id`, username, hash, displayName, email, phone, quota, objects, daily, buckets, regionID, at).Scan(&id)
	if isUnique(err) {
		return nil, ErrConflict
	}
	if err != nil {
		return nil, fmt.Errorf("insert account: %w", err)
	}
	return s.GetByID(ctx, id)
}

func (s *Store) Update(ctx context.Context, u *Record) (*Record, error) {
	tag, err := s.pool.Exec(ctx, `
		UPDATE tenant_accounts SET
			display_name = $2, email = $3, phone = $4, status = $5,
			quota_bytes = $6, object_limit = $7, daily_upload_bytes = $8, bucket_limit = $9,
			storage_region_id = $10, updated_at = $11
		WHERE id = $1`,
		u.ID, u.DisplayName, u.Email, u.Phone, u.Status,
		u.QuotaBytes, u.ObjectLimit, u.DailyUploadBytes, u.BucketLimit,
		u.StorageRegionID, u.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("update account: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return nil, nil
	}
	return s.GetByID(ctx, u.ID)
}

func (s *Store) Secret(ctx context.Context, id int64) (hash string, mustChange bool, err error) {
	err = s.pool.QueryRow(ctx, `SELECT password_hash, must_change_password FROM tenant_accounts WHERE id = $1`, id).Scan(&hash, &mustChange)
	if err == pgx.ErrNoRows {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("account secret: %w", err)
	}
	return hash, mustChange, nil
}

func (s *Store) UpdatePassword(ctx context.Context, id int64, hash string, at time.Time) error {
	tag, err := s.pool.Exec(ctx, `UPDATE tenant_accounts SET password_hash = $2, updated_at = $3 WHERE id = $1`, id, hash, at)
	if err != nil {
		return fmt.Errorf("reset password: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
	return nil
}

func (s *Store) Delete(ctx context.Context, id int64) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM tenant_accounts WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("delete account: %w", err)
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
