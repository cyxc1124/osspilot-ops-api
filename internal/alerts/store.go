package alerts

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Rule struct {
	ID           int64
	Name         string
	RuleType     string
	Enabled      bool
	Severity     string
	Config       json.RawMessage
	ChannelIDs   json.RawMessage
	NotifyTenant bool
	Description  *string
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

type Channel struct {
	ID          int64
	Name        string
	ChannelType string
	Enabled     bool
	Config      json.RawMessage
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

type Event struct {
	ID             int64
	RuleID         *int64
	RuleType       string
	Severity       string
	Status         string
	Title          string
	Message        string
	TenantID       *int64
	BucketID       *int64
	BucketName     *string
	Details        json.RawMessage
	NotifyTenant   bool
	FiredAt        time.Time
	AcknowledgedAt *time.Time
	AcknowledgedBy *int64
	ResolvedAt     *time.Time
	CreatedAt      time.Time
}

type Store struct {
	pool *pgxpool.Pool
}

func NewStore(pool *pgxpool.Pool) *Store { return &Store{pool: pool} }

func (s *Store) ListRules(ctx context.Context) ([]Rule, error) {
	rows, err := s.pool.Query(ctx, `SELECT id, name, rule_type, enabled, severity, config, channel_ids, notify_tenant, description, created_at, updated_at FROM alert_rules ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Rule
	for rows.Next() {
		var r Rule
		if err := rows.Scan(&r.ID, &r.Name, &r.RuleType, &r.Enabled, &r.Severity, &r.Config, &r.ChannelIDs, &r.NotifyTenant, &r.Description, &r.CreatedAt, &r.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *Store) GetRule(ctx context.Context, id int64) (*Rule, error) {
	var r Rule
	err := s.pool.QueryRow(ctx, `SELECT id, name, rule_type, enabled, severity, config, channel_ids, notify_tenant, description, created_at, updated_at FROM alert_rules WHERE id = $1`, id).
		Scan(&r.ID, &r.Name, &r.RuleType, &r.Enabled, &r.Severity, &r.Config, &r.ChannelIDs, &r.NotifyTenant, &r.Description, &r.CreatedAt, &r.UpdatedAt)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &r, nil
}

func (s *Store) InsertRule(ctx context.Context, r Rule) (*Rule, error) {
	var id int64
	err := s.pool.QueryRow(ctx, `
		INSERT INTO alert_rules (name, rule_type, enabled, severity, config, channel_ids, notify_tenant, description)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8) RETURNING id`,
		r.Name, r.RuleType, r.Enabled, r.Severity, nzJSON(r.Config, "{}"), nzArray(r.ChannelIDs), r.NotifyTenant, r.Description).Scan(&id)
	if err != nil {
		return nil, err
	}
	return s.GetRule(ctx, id)
}

func (s *Store) UpdateRule(ctx context.Context, r *Rule) (*Rule, error) {
	_, err := s.pool.Exec(ctx, `
		UPDATE alert_rules SET name=$2, enabled=$3, severity=$4, config=$5, channel_ids=$6, notify_tenant=$7, description=$8, updated_at=now()
		WHERE id=$1`, r.ID, r.Name, r.Enabled, r.Severity, nzJSON(r.Config, "{}"), nzArray(r.ChannelIDs), r.NotifyTenant, r.Description)
	if err != nil {
		return nil, err
	}
	return s.GetRule(ctx, r.ID)
}

func (s *Store) DeleteRule(ctx context.Context, id int64) (bool, error) {
	tag, err := s.pool.Exec(ctx, `DELETE FROM alert_rules WHERE id = $1`, id)
	return tag.RowsAffected() > 0, err
}

func (s *Store) CountEnabled(ctx context.Context) (int, error) {
	var n int
	err := s.pool.QueryRow(ctx, `SELECT count(*) FROM alert_rules WHERE enabled`).Scan(&n)
	return n, err
}

func (s *Store) ListChannels(ctx context.Context) ([]Channel, error) {
	rows, err := s.pool.Query(ctx, `SELECT id, name, channel_type, enabled, config, created_at, updated_at FROM notification_channels ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Channel
	for rows.Next() {
		var c Channel
		if err := rows.Scan(&c.ID, &c.Name, &c.ChannelType, &c.Enabled, &c.Config, &c.CreatedAt, &c.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s *Store) GetChannel(ctx context.Context, id int64) (*Channel, error) {
	var c Channel
	err := s.pool.QueryRow(ctx, `SELECT id, name, channel_type, enabled, config, created_at, updated_at FROM notification_channels WHERE id = $1`, id).
		Scan(&c.ID, &c.Name, &c.ChannelType, &c.Enabled, &c.Config, &c.CreatedAt, &c.UpdatedAt)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &c, nil
}

func (s *Store) InsertChannel(ctx context.Context, c Channel) (*Channel, error) {
	var id int64
	err := s.pool.QueryRow(ctx, `
		INSERT INTO notification_channels (name, channel_type, enabled, config) VALUES ($1,$2,$3,$4) RETURNING id`,
		c.Name, c.ChannelType, c.Enabled, nzJSON(c.Config, "{}")).Scan(&id)
	if err != nil {
		return nil, err
	}
	return s.GetChannel(ctx, id)
}

func (s *Store) UpdateChannel(ctx context.Context, c *Channel) (*Channel, error) {
	_, err := s.pool.Exec(ctx, `UPDATE notification_channels SET name=$2, enabled=$3, config=$4, updated_at=now() WHERE id=$1`,
		c.ID, c.Name, c.Enabled, nzJSON(c.Config, "{}"))
	if err != nil {
		return nil, err
	}
	return s.GetChannel(ctx, c.ID)
}

func (s *Store) DeleteChannel(ctx context.Context, id int64) (bool, error) {
	tag, err := s.pool.Exec(ctx, `DELETE FROM notification_channels WHERE id = $1`, id)
	return tag.RowsAffected() > 0, err
}

func (s *Store) ListEvents(ctx context.Context, status string, tenantID *int64, page, pageSize int) ([]Event, int, error) {
	where, args := eventWhere(status, tenantID)
	var total int
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM alert_events`+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	args = append(args, pageSize, (page-1)*pageSize)
	rows, err := s.pool.Query(ctx, eventSelect+where+` ORDER BY fired_at DESC, id DESC LIMIT $`+fmt.Sprint(len(args)-1)+` OFFSET $`+fmt.Sprint(len(args)), args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out, err := scanEvents(rows)
	return out, total, err
}

func (s *Store) RecentEvents(ctx context.Context, limit int) ([]Event, error) {
	rows, err := s.pool.Query(ctx, eventSelect+` ORDER BY fired_at DESC, id DESC LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanEvents(rows)
}

func (s *Store) GetEvent(ctx context.Context, id int64) (*Event, error) {
	e, err := scanEvent(s.pool.QueryRow(ctx, eventSelect+` WHERE id = $1`, id))
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &e, nil
}

func (s *Store) Acknowledge(ctx context.Context, id, userID int64) (*Event, error) {
	tag, err := s.pool.Exec(ctx, `
		UPDATE alert_events SET status='acknowledged', acknowledged_at=now(), acknowledged_by=$2
		WHERE id=$1 AND status <> 'resolved'`, id, userID)
	if err != nil {
		return nil, err
	}
	if tag.RowsAffected() == 0 {
		return s.GetEvent(ctx, id)
	}
	return s.GetEvent(ctx, id)
}

func (s *Store) Resolve(ctx context.Context, id int64) (*Event, error) {
	_, err := s.pool.Exec(ctx, `UPDATE alert_events SET status='resolved', resolved_at=now() WHERE id=$1 AND status <> 'resolved'`, id)
	if err != nil {
		return nil, err
	}
	return s.GetEvent(ctx, id)
}

const eventSelect = `SELECT id, rule_id, rule_type, severity, status, title, message, tenant_id, bucket_id, bucket_name, details, notify_tenant, fired_at, acknowledged_at, acknowledged_by, resolved_at, created_at FROM alert_events`

func eventWhere(status string, tenantID *int64) (string, []any) {
	var parts []string
	var args []any
	if status != "" {
		args = append(args, status)
		parts = append(parts, fmt.Sprintf("status = $%d", len(args)))
	}
	if tenantID != nil {
		args = append(args, *tenantID)
		parts = append(parts, fmt.Sprintf("tenant_id = $%d", len(args)))
	}
	if len(parts) == 0 {
		return "", args
	}
	return " WHERE " + strings.Join(parts, " AND "), args
}

type rowScanner interface{ Scan(dest ...any) error }

func scanEvent(row rowScanner) (Event, error) {
	var e Event
	err := row.Scan(&e.ID, &e.RuleID, &e.RuleType, &e.Severity, &e.Status, &e.Title, &e.Message, &e.TenantID, &e.BucketID, &e.BucketName, &e.Details, &e.NotifyTenant, &e.FiredAt, &e.AcknowledgedAt, &e.AcknowledgedBy, &e.ResolvedAt, &e.CreatedAt)
	return e, err
}

func scanEvents(rows pgx.Rows) ([]Event, error) {
	var out []Event
	for rows.Next() {
		e, err := scanEvent(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func nzJSON(raw json.RawMessage, empty string) []byte {
	if len(raw) == 0 {
		return []byte(empty)
	}
	return raw
}

func nzArray(raw json.RawMessage) []byte { return nzJSON(raw, "[]") }
