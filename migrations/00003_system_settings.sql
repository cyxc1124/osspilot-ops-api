-- +goose Up
CREATE TABLE system_settings (
    key        VARCHAR(64) PRIMARY KEY,
    value      TEXT        NOT NULL,
    is_secret  BOOLEAN     NOT NULL DEFAULT false,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE IF EXISTS system_settings;
