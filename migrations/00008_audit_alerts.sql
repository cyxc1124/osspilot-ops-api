-- +goose Up
CREATE TABLE audit_logs (
    id            BIGSERIAL PRIMARY KEY,
    user_id       BIGINT REFERENCES ops_users (id) ON DELETE SET NULL,
    username      VARCHAR(64),
    tenant_id     BIGINT REFERENCES tenant_accounts (id) ON DELETE SET NULL,
    tenant_name   VARCHAR(64),
    bucket_name   VARCHAR(255),
    object_key    VARCHAR(2048),
    action        VARCHAR(64) NOT NULL,
    source_ip     VARCHAR(64),
    user_agent    VARCHAR(512),
    status        VARCHAR(32) NOT NULL DEFAULT 'success',
    error_message TEXT,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX ix_audit_logs_user_id ON audit_logs (user_id);
CREATE INDEX ix_audit_logs_tenant_id ON audit_logs (tenant_id);
CREATE INDEX ix_audit_logs_action ON audit_logs (action);
CREATE INDEX ix_audit_logs_created_at ON audit_logs (created_at);

CREATE TABLE alert_rules (
    id            BIGSERIAL PRIMARY KEY,
    name          VARCHAR(128) NOT NULL,
    rule_type     VARCHAR(64) NOT NULL,
    enabled       BOOLEAN NOT NULL DEFAULT true,
    severity      VARCHAR(32) NOT NULL DEFAULT 'warning',
    config        JSONB NOT NULL DEFAULT '{}'::jsonb,
    channel_ids   JSONB NOT NULL DEFAULT '[]'::jsonb,
    notify_tenant BOOLEAN NOT NULL DEFAULT false,
    description   TEXT,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX ix_alert_rules_rule_type ON alert_rules (rule_type);
CREATE INDEX ix_alert_rules_enabled ON alert_rules (enabled);

CREATE TABLE notification_channels (
    id           BIGSERIAL PRIMARY KEY,
    name         VARCHAR(128) NOT NULL,
    channel_type VARCHAR(32) NOT NULL,
    enabled      BOOLEAN NOT NULL DEFAULT true,
    config       JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX ix_notification_channels_type ON notification_channels (channel_type);

CREATE TABLE alert_events (
    id               BIGSERIAL PRIMARY KEY,
    rule_id          BIGINT REFERENCES alert_rules (id) ON DELETE SET NULL,
    rule_type        VARCHAR(64) NOT NULL,
    severity         VARCHAR(32) NOT NULL,
    status           VARCHAR(32) NOT NULL DEFAULT 'firing',
    title            VARCHAR(255) NOT NULL,
    message          TEXT NOT NULL,
    tenant_id        BIGINT REFERENCES tenant_accounts (id) ON DELETE SET NULL,
    bucket_id        BIGINT REFERENCES platform_buckets (id) ON DELETE SET NULL,
    bucket_name      VARCHAR(255),
    details          JSONB NOT NULL DEFAULT '{}'::jsonb,
    notify_tenant    BOOLEAN NOT NULL DEFAULT false,
    fired_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    acknowledged_at  TIMESTAMPTZ,
    acknowledged_by  BIGINT REFERENCES ops_users (id) ON DELETE SET NULL,
    resolved_at      TIMESTAMPTZ,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX ix_alert_events_status ON alert_events (status);
CREATE INDEX ix_alert_events_tenant_id ON alert_events (tenant_id);
CREATE INDEX ix_alert_events_fired_at ON alert_events (fired_at);

-- +goose Down
DROP TABLE IF EXISTS alert_events;
DROP TABLE IF EXISTS notification_channels;
DROP TABLE IF EXISTS alert_rules;
DROP TABLE IF EXISTS audit_logs;
