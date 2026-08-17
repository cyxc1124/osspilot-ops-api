package grants

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type Grant struct {
	BucketID    int64
	BucketName  string
	DisplayName *string
	GrantedAt   time.Time
}

type Store struct {
	pool *pgxpool.Pool
}

func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

func (s *Store) List(ctx context.Context, accountID int64) ([]Grant, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT g.bucket_id, b.bucket_name, b.display_name, g.granted_at
		FROM account_bucket_grants g
		JOIN platform_buckets b ON b.id = g.bucket_id
		WHERE g.account_id = $1
		ORDER BY b.bucket_name`, accountID)
	if err != nil {
		return nil, fmt.Errorf("list grants: %w", err)
	}
	defer rows.Close()
	var out []Grant
	for rows.Next() {
		var g Grant
		if err := rows.Scan(&g.BucketID, &g.BucketName, &g.DisplayName, &g.GrantedAt); err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

func (s *Store) Replace(ctx context.Context, accountID int64, ids []int64) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `DELETE FROM account_bucket_grants WHERE account_id = $1`, accountID); err != nil {
		return fmt.Errorf("clear grants: %w", err)
	}
	for _, id := range ids {
		if _, err := tx.Exec(ctx, `
			INSERT INTO account_bucket_grants (account_id, bucket_id) VALUES ($1,$2)`, accountID, id); err != nil {
			return fmt.Errorf("insert grant: %w", err)
		}
	}
	return tx.Commit(ctx)
}

func (s *Store) Revoke(ctx context.Context, accountID, bucketID int64) (bool, error) {
	tag, err := s.pool.Exec(ctx, `
		DELETE FROM account_bucket_grants WHERE account_id = $1 AND bucket_id = $2`, accountID, bucketID)
	if err != nil {
		return false, fmt.Errorf("revoke grant: %w", err)
	}
	return tag.RowsAffected() > 0, nil
}
