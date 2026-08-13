-- +goose Up
CREATE TABLE storage_regions (
    id             BIGSERIAL PRIMARY KEY,
    code           VARCHAR(64)  NOT NULL,
    name           VARCHAR(128) NOT NULL,
    s3_endpoint    VARCHAR(512) NOT NULL,
    s3_region_name VARCHAR(64)  NOT NULL DEFAULT 'us-east-1',
    is_default     BOOLEAN      NOT NULL DEFAULT false,
    status         VARCHAR(32)  NOT NULL DEFAULT 'active',
    created_at     TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at     TIMESTAMPTZ  NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX ix_storage_regions_code ON storage_regions (code);
CREATE INDEX ix_storage_regions_status ON storage_regions (status);
CREATE UNIQUE INDEX ix_storage_regions_default ON storage_regions (is_default) WHERE is_default;

-- +goose Down
DROP TABLE IF EXISTS storage_regions;
