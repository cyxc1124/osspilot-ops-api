-- +goose Up
CREATE TABLE ops_roles (
    id          BIGSERIAL PRIMARY KEY,
    name        VARCHAR(64) NOT NULL,
    description VARCHAR(255),
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX ix_ops_roles_name ON ops_roles (name);

INSERT INTO ops_roles (name, description) VALUES
    ('platform_admin', 'Platform administrator'),
    ('ops_operator', 'Operations operator');

CREATE TABLE ops_user_roles (
    id         BIGSERIAL PRIMARY KEY,
    user_id    BIGINT NOT NULL REFERENCES ops_users (id) ON DELETE CASCADE,
    role_id    BIGINT NOT NULL REFERENCES ops_roles (id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (user_id, role_id)
);

CREATE INDEX ix_ops_user_roles_user_id ON ops_user_roles (user_id);

INSERT INTO ops_user_roles (user_id, role_id)
SELECT u.id, r.id
FROM ops_users u
JOIN ops_roles r ON r.name = u.role
WHERE u.role <> '';

ALTER TABLE ops_users DROP COLUMN role;

-- +goose Down
ALTER TABLE ops_users ADD COLUMN role VARCHAR(32) NOT NULL DEFAULT 'ops_operator';

UPDATE ops_users u
SET role = COALESCE(
    (SELECT r.name
     FROM ops_user_roles ur
     JOIN ops_roles r ON r.id = ur.role_id
     WHERE ur.user_id = u.id
     ORDER BY CASE r.name WHEN 'platform_admin' THEN 0 ELSE 1 END
     LIMIT 1),
    'ops_operator'
);

DROP TABLE IF EXISTS ops_user_roles;
DROP TABLE IF EXISTS ops_roles;
