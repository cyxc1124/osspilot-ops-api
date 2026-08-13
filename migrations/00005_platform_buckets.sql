-- +goose Up
CREATE TABLE platform_buckets (
    id                BIGSERIAL PRIMARY KEY,
    bucket_name       VARCHAR(63)  NOT NULL,
    display_name      VARCHAR(128),
    storage_region_id BIGINT REFERENCES storage_regions (id) ON DELETE SET NULL,
    quota_bytes       BIGINT,
    object_limit      BIGINT,
    status            VARCHAR(32)  NOT NULL DEFAULT 'active',
    created_at        TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at        TIMESTAMPTZ  NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX ix_platform_buckets_name ON platform_buckets (bucket_name);
CREATE INDEX ix_platform_buckets_status ON platform_buckets (status);
CREATE INDEX ix_platform_buckets_region ON platform_buckets (storage_region_id);

CREATE TABLE account_bucket_grants (
    account_id BIGINT NOT NULL REFERENCES tenant_accounts (id) ON DELETE CASCADE,
    bucket_id  BIGINT NOT NULL REFERENCES platform_buckets (id) ON DELETE CASCADE,
    granted_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (account_id, bucket_id)
);

CREATE INDEX ix_account_bucket_grants_bucket ON account_bucket_grants (bucket_id);

-- +goose Down
DROP TABLE IF EXISTS account_bucket_grants;
DROP TABLE IF EXISTS platform_buckets;
