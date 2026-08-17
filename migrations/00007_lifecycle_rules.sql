-- +goose Up
CREATE TABLE lifecycle_rules (
    id                            BIGSERIAL PRIMARY KEY,
    bucket_id                     BIGINT NOT NULL REFERENCES platform_buckets (id) ON DELETE CASCADE,
    prefix                        VARCHAR(1024) NOT NULL DEFAULT '',
    enabled                       BOOLEAN NOT NULL DEFAULT true,
    delete_after_days             INTEGER,
    cleanup_trash_after_days      INTEGER,
    cleanup_versions_after_days   INTEGER,
    cleanup_multipart_after_days  INTEGER,
    created_at                    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at                    TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX ix_lifecycle_rules_bucket_id ON lifecycle_rules (bucket_id);

-- +goose Down
DROP TABLE IF EXISTS lifecycle_rules;
