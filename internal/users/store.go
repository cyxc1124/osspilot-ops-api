package users

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

type Record struct {
	ID          int64
	Username    string
	DisplayName *string
	Email       *string
	Phone       *string
	Status      string
	Role        string
	LastLoginAt *time.Time
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

type Store struct {
	pool *pgxpool.Pool
}

func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

const cols = `id, username, display_name, email, phone, status, role, last_login_at, created_at, updated_at`

type rowScanner interface {
	Scan(dest ...any) error
}

func scan(row rowScanner) (Record, error) {
	var u Record
	err := row.Scan(
		&u.ID, &u.Username, &u.DisplayName, &u.Email, &u.Phone, &u.Status, &u.Role, &u.LastLoginAt, &u.CreatedAt, &u.UpdatedAt,
	)
	return u, err
}

func (s *Store) List(ctx context.Context) ([]Record, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+cols+` FROM ops_users ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("list users: %w", err)
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
	u, err := scan(s.pool.QueryRow(ctx, `SELECT `+cols+` FROM ops_users WHERE id = $1`, id))
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get user: %w", err)
	}
	return &u, nil
}

func (s *Store) Insert(ctx context.Context, username, hash string, displayName, email, phone *string, role string, at time.Time) (*Record, error) {
	u, err := scan(s.pool.QueryRow(ctx, `
		INSERT INTO ops_users (username, password_hash, display_name, email, phone, status, role, created_at, updated_at)
		VALUES ($1,$2,$3,$4,$5,'active',$6,$7,$7)
		RETURNING `+cols, username, hash, displayName, email, phone, role, at))
	if isUnique(err) {
		return nil, ErrConflict
	}
	if err != nil {
		return nil, fmt.Errorf("insert user: %w", err)
	}
	return &u, nil
}

func (s *Store) Update(ctx context.Context, u *Record) (*Record, error) {
	out, err := scan(s.pool.QueryRow(ctx, `
		UPDATE ops_users
		SET display_name = $2, email = $3, phone = $4, status = $5, role = $6, updated_at = $7
		WHERE id = $1
		RETURNING `+cols, u.ID, u.DisplayName, u.Email, u.Phone, u.Status, u.Role, u.UpdatedAt))
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("update user: %w", err)
	}
	return &out, nil
}

func (s *Store) UpdatePassword(ctx context.Context, id int64, hash string, at time.Time) error {
	tag, err := s.pool.Exec(ctx, `UPDATE ops_users SET password_hash = $2, updated_at = $3 WHERE id = $1`, id, hash, at)
	if err != nil {
		return fmt.Errorf("reset password: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
	return nil
}

func (s *Store) Delete(ctx context.Context, id int64) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM ops_users WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("delete user: %w", err)
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
