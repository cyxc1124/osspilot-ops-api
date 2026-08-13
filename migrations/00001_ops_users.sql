-- +goose Up
CREATE TABLE ops_users (
    id              BIGSERIAL PRIMARY KEY,
    username        VARCHAR(64)  NOT NULL,
    password_hash   VARCHAR(255) NOT NULL,
    display_name    VARCHAR(128),
    email           VARCHAR(255),
    phone           VARCHAR(32),
    status          VARCHAR(32)  NOT NULL DEFAULT 'active',
    role            VARCHAR(32)  NOT NULL DEFAULT 'ops_operator',
    must_change_password BOOLEAN NOT NULL DEFAULT false,
    last_login_at   TIMESTAMPTZ,
    created_at      TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ  NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX ix_ops_users_username ON ops_users (username);
CREATE INDEX ix_ops_users_status ON ops_users (status);

-- seed username/password: admin / admin
INSERT INTO ops_users (username, password_hash, display_name, role, must_change_password)
VALUES (
    'admin',
    '$2a$10$6apLvtRj9fP/MibiTA.VOexIIIPUtW5oeTiZA1BAD3tbSWZUEqPm2',
    'Administrator',
    'platform_admin',
    true
);

-- +goose Down
DROP TABLE IF EXISTS ops_users;
