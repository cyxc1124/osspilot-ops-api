package stats

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type Store struct {
	pool *pgxpool.Pool
}

func NewStore(pool *pgxpool.Pool) *Store { return &Store{pool: pool} }

type Overview struct {
	TenantCount int
	BucketCount int
	QuotaBytes  *int64
}

type TenantRow struct {
	ID          int64
	Name        string
	DisplayName *string
	Status      string
	QuotaBytes  *int64
}

func (s *Store) Overview(ctx context.Context) (Overview, error) {
	var o Overview
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM tenant_accounts WHERE status = 'active'`).Scan(&o.TenantCount); err != nil {
		return o, fmt.Errorf("count tenants: %w", err)
	}
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM platform_buckets WHERE status = 'active'`).Scan(&o.BucketCount); err != nil {
		return o, fmt.Errorf("count buckets: %w", err)
	}
	var q *int64
	if err := s.pool.QueryRow(ctx, `SELECT coalesce(sum(quota_bytes), 0) FROM tenant_accounts WHERE status = 'active'`).Scan(&q); err != nil {
		return o, fmt.Errorf("sum quota: %w", err)
	}
	o.QuotaBytes = q
	return o, nil
}

func (s *Store) Tenants(ctx context.Context) ([]TenantRow, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, username, display_name, status, quota_bytes
		FROM tenant_accounts ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("list tenants: %w", err)
	}
	defer rows.Close()
	var out []TenantRow
	for rows.Next() {
		var t TenantRow
		if err := rows.Scan(&t.ID, &t.Name, &t.DisplayName, &t.Status, &t.QuotaBytes); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (s *Store) Username(ctx context.Context, id int64) (string, error) {
	if s == nil || s.pool == nil {
		return "", nil
	}
	var name string
	err := s.pool.QueryRow(ctx, `SELECT username FROM tenant_accounts WHERE id = $1`, id).Scan(&name)
	if err != nil {
		return "", nil
	}
	return name, nil
}

func collected() string { return time.Now().UTC().Format(time.RFC3339) }
