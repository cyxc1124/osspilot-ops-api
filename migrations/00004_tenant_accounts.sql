-- +goose Up
CREATE TABLE tenant_accounts (
    id                   BIGSERIAL PRIMARY KEY,
    username             VARCHAR(64)  NOT NULL,
    password_hash        VARCHAR(255) NOT NULL,
    display_name         VARCHAR(128),
    email                VARCHAR(255),
    phone                VARCHAR(32),
    status               VARCHAR(32)  NOT NULL DEFAULT 'active',
    quota_bytes          BIGINT,
    object_limit         BIGINT,
    daily_upload_bytes   BIGINT,
    bucket_limit         BIGINT,
    storage_region_id    BIGINT REFERENCES storage_regions (id) ON DELETE SET NULL,
    must_change_password BOOLEAN      NOT NULL DEFAULT true,
    last_login_at        TIMESTAMPTZ,
    created_at           TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at           TIMESTAMPTZ  NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX ix_tenant_accounts_username ON tenant_accounts (username);
CREATE INDEX ix_tenant_accounts_status ON tenant_accounts (status);
CREATE INDEX ix_tenant_accounts_region ON tenant_accounts (storage_region_id);

-- +goose Down
DROP TABLE IF EXISTS tenant_accounts;
