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
	Roles       []string
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

const cols = `id, username, display_name, email, phone, status, last_login_at, created_at, updated_at`

type rowScanner interface {
	Scan(dest ...any) error
}

func scan(row rowScanner) (Record, error) {
	var u Record
	err := row.Scan(
		&u.ID, &u.Username, &u.DisplayName, &u.Email, &u.Phone, &u.Status, &u.LastLoginAt, &u.CreatedAt, &u.UpdatedAt,
	)
	if u.Roles == nil {
		u.Roles = []string{}
	}
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
	if err := rows.Err(); err != nil {
		return nil, err
	}
	byUser, err := s.rolesByUser(ctx)
	if err != nil {
		return nil, err
	}
	for i := range out {
		if r, ok := byUser[out[i].ID]; ok {
			out[i].Roles = r
		}
	}
	return out, nil
}

func (s *Store) GetByUsername(ctx context.Context, username string) (*Record, error) {
	u, err := scan(s.pool.QueryRow(ctx, `SELECT `+cols+` FROM ops_users WHERE username = $1`, username))
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get user: %w", err)
	}
	return &u, nil
}

func (s *Store) GetByID(ctx context.Context, id int64) (*Record, error) {
	u, err := scan(s.pool.QueryRow(ctx, `SELECT `+cols+` FROM ops_users WHERE id = $1`, id))
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get user: %w", err)
	}
	roles, err := s.rolesFor(ctx, id)
	if err != nil {
		return nil, err
	}
	u.Roles = roles
	return &u, nil
}

func (s *Store) Insert(ctx context.Context, username, hash string, displayName, email, phone *string, roles []string, at time.Time) (*Record, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	u, err := scan(tx.QueryRow(ctx, `
		INSERT INTO ops_users (username, password_hash, display_name, email, phone, status, created_at, updated_at)
		VALUES ($1,$2,$3,$4,$5,'active',$6,$6)
		RETURNING `+cols, username, hash, displayName, email, phone, at))
	if isUnique(err) {
		return nil, ErrConflict
	}
	if err != nil {
		return nil, fmt.Errorf("insert user: %w", err)
	}
	if err := replaceRoles(ctx, tx, u.ID, roles); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	u.Roles = append([]string{}, roles...)
	return &u, nil
}

func (s *Store) Update(ctx context.Context, u *Record, roles *[]string) (*Record, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	out, err := scan(tx.QueryRow(ctx, `
		UPDATE ops_users
		SET display_name = $2, email = $3, phone = $4, status = $5, updated_at = $6
		WHERE id = $1
		RETURNING `+cols, u.ID, u.DisplayName, u.Email, u.Phone, u.Status, u.UpdatedAt))
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("update user: %w", err)
	}
	if roles != nil {
		if err := replaceRoles(ctx, tx, u.ID, *roles); err != nil {
			return nil, err
		}
		out.Roles = append([]string{}, *roles...)
	} else {
		names, err := rolesForTx(ctx, tx, u.ID)
		if err != nil {
			return nil, err
		}
		out.Roles = names
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
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

type roleExec interface {
	Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

func replaceRoles(ctx context.Context, db roleExec, userID int64, roles []string) error {
	if _, err := db.Exec(ctx, `DELETE FROM ops_user_roles WHERE user_id = $1`, userID); err != nil {
		return fmt.Errorf("clear roles: %w", err)
	}
	if len(roles) == 0 {
		return nil
	}
	_, err := db.Exec(ctx, `
		INSERT INTO ops_user_roles (user_id, role_id)
		SELECT $1, id FROM ops_roles WHERE name = ANY($2)`, userID, roles)
	if err != nil {
		return fmt.Errorf("bind roles: %w", err)
	}
	return nil
}

func (s *Store) rolesFor(ctx context.Context, userID int64) ([]string, error) {
	return rolesForTx(ctx, s.pool, userID)
}

func rolesForTx(ctx context.Context, db roleExec, userID int64) ([]string, error) {
	rows, err := db.Query(ctx, `
		SELECT r.name FROM ops_user_roles ur
		JOIN ops_roles r ON r.id = ur.role_id
		WHERE ur.user_id = $1 ORDER BY r.name`, userID)
	if err != nil {
		return nil, fmt.Errorf("list roles: %w", err)
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		out = append(out, name)
	}
	return out, rows.Err()
}

func (s *Store) rolesByUser(ctx context.Context) (map[int64][]string, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT ur.user_id, r.name FROM ops_user_roles ur
		JOIN ops_roles r ON r.id = ur.role_id
		ORDER BY ur.user_id, r.name`)
	if err != nil {
		return nil, fmt.Errorf("list roles: %w", err)
	}
	defer rows.Close()
	out := map[int64][]string{}
	for rows.Next() {
		var id int64
		var name string
		if err := rows.Scan(&id, &name); err != nil {
			return nil, err
		}
		out[id] = append(out[id], name)
	}
	return out, rows.Err()
}

func isUnique(err error) bool {
	var pe *pgconn.PgError
	return errors.As(err, &pe) && pe.Code == "23505"
}
