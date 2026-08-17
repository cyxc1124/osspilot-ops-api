package audit

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type Entry struct {
	ID           int64
	UserID       *int64
	Username     *string
	TenantID     *int64
	TenantName   *string
	BucketName   *string
	ObjectKey    *string
	Action       string
	SourceIP     *string
	UserAgent    *string
	Status       string
	ErrorMessage *string
	CreatedAt    time.Time
}

type Filter struct {
	TenantID   *int64
	TenantName string
	UserID     *int64
	Username   string
	BucketName string
	ObjectKey  string
	Action     string
	Status     string
	SourceIP   string
	Keyword    string
	AdminOnly  bool
	From, To   *time.Time
}

type Store struct {
	pool *pgxpool.Pool
}

func NewStore(pool *pgxpool.Pool) *Store { return &Store{pool: pool} }

const cols = `id, user_id, username, tenant_id, tenant_name, bucket_name, object_key,
	action, source_ip, user_agent, status, error_message, created_at`

func scan(row interface{ Scan(dest ...any) error }) (Entry, error) {
	var e Entry
	err := row.Scan(&e.ID, &e.UserID, &e.Username, &e.TenantID, &e.TenantName, &e.BucketName, &e.ObjectKey,
		&e.Action, &e.SourceIP, &e.UserAgent, &e.Status, &e.ErrorMessage, &e.CreatedAt)
	e.ErrorMessage = sanitizeError(e.Action, e.ErrorMessage)
	return e, err
}

func (s *Store) Insert(ctx context.Context, e Entry) error {
	status := e.Status
	if status == "" {
		status = "success"
	}
	_, err := s.pool.Exec(ctx, `
		INSERT INTO audit_logs (user_id, username, tenant_id, tenant_name, bucket_name, object_key,
			action, source_ip, user_agent, status, error_message)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`,
		e.UserID, e.Username, e.TenantID, e.TenantName, e.BucketName, e.ObjectKey,
		e.Action, e.SourceIP, e.UserAgent, status, e.ErrorMessage)
	return err
}

func (s *Store) List(ctx context.Context, f Filter, page, pageSize int) ([]Entry, int, error) {
	where, args := f.where()
	var total int
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM audit_logs`+where, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count audit: %w", err)
	}
	args = append(args, pageSize, (page-1)*pageSize)
	rows, err := s.pool.Query(ctx, `SELECT `+cols+` FROM audit_logs`+where+` ORDER BY created_at DESC, id DESC LIMIT $`+fmt.Sprint(len(args)-1)+` OFFSET $`+fmt.Sprint(len(args)), args...)
	if err != nil {
		return nil, 0, fmt.Errorf("list audit: %w", err)
	}
	defer rows.Close()
	var out []Entry
	for rows.Next() {
		e, err := scan(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, e)
	}
	return out, total, rows.Err()
}

func (s *Store) Export(ctx context.Context, f Filter) ([]Entry, error) {
	where, args := f.where()
	rows, err := s.pool.Query(ctx, `SELECT `+cols+` FROM audit_logs`+where+` ORDER BY created_at DESC, id DESC LIMIT 10000`, args...)
	if err != nil {
		return nil, fmt.Errorf("export audit: %w", err)
	}
	defer rows.Close()
	var out []Entry
	for rows.Next() {
		e, err := scan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func (f Filter) where() (string, []any) {
	var parts []string
	var args []any
	add := func(sql string, v any) {
		args = append(args, v)
		parts = append(parts, fmt.Sprintf(sql, len(args)))
	}
	if f.TenantID != nil {
		add("tenant_id = $%d", *f.TenantID)
	}
	if f.TenantName != "" {
		add("tenant_name ILIKE $%d", "%"+f.TenantName+"%")
	}
	if f.UserID != nil {
		add("user_id = $%d", *f.UserID)
	}
	if f.Username != "" {
		add("username ILIKE $%d", "%"+f.Username+"%")
	}
	if f.BucketName != "" {
		add("bucket_name ILIKE $%d", "%"+f.BucketName+"%")
	}
	if f.ObjectKey != "" {
		add("object_key ILIKE $%d", "%"+f.ObjectKey+"%")
	}
	if f.Action != "" {
		add("action = $%d", f.Action)
	}
	if f.Status != "" {
		add("status = $%d", f.Status)
	}
	if f.SourceIP != "" {
		add("source_ip ILIKE $%d", "%"+f.SourceIP+"%")
	}
	if f.Keyword != "" {
		args = append(args, "%"+f.Keyword+"%")
		n := fmt.Sprint(len(args))
		parts = append(parts, `(coalesce(username,'') ILIKE $`+n+` OR coalesce(tenant_name,'') ILIKE $`+n+` OR coalesce(bucket_name,'') ILIKE $`+n+` OR coalesce(object_key,'') ILIKE $`+n+` OR action ILIKE $`+n+` OR coalesce(error_message,'') ILIKE $`+n+`)`)
	}
	if f.From != nil {
		add("created_at >= $%d", *f.From)
	}
	if f.To != nil {
		add("created_at <= $%d", *f.To)
	}
	if f.AdminOnly {
		parts = append(parts, adminSQL())
	}
	if len(parts) == 0 {
		return "", args
	}
	return " WHERE " + strings.Join(parts, " AND "), args
}

func adminSQL() string {
	return `action IN ('user_create','user_delete','user_update','modify_user_role','create_tenant','delete_tenant','disable_tenant','enable_tenant','update_tenant','modify_quota','bucket_create','bucket_import','bucket_delete','modify_bucket','modify_bucket_quota','modify_access_logging','modify_bucket_policy','modify_bucket_cors','modify_permission','modify_lifecycle','force_unlock_file','restart_rgw','rolling_restart_rgw','update_system_settings','request_tenant_api_access','approve_tenant_api_access','reject_tenant_api_access','disable_tenant_api_access','create_application','update_application','delete_application','create_access_key','disable_access_key','issue_sts_credentials','create_lifecycle_rule','update_lifecycle_rule','delete_lifecycle_rule')`
}
