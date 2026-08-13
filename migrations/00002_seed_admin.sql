-- +goose Up
ALTER TABLE ops_users
    ADD COLUMN must_change_password BOOLEAN NOT NULL DEFAULT false;

-- seed username/password: admin / admin (must_change_password=true)
INSERT INTO ops_users (username, password_hash, display_name, role, must_change_password)
VALUES (
    'admin',
    '$2a$10$6apLvtRj9fP/MibiTA.VOexIIIPUtW5oeTiZA1BAD3tbSWZUEqPm2',
    'Administrator',
    'platform_admin',
    true
)
ON CONFLICT (username) DO NOTHING;

-- +goose Down
DELETE FROM ops_users WHERE username = 'admin';
ALTER TABLE ops_users DROP COLUMN IF EXISTS must_change_password;
