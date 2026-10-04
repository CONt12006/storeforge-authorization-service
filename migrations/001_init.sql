CREATE TABLE IF NOT EXISTS roles (
    id BIGSERIAL PRIMARY KEY,
    name VARCHAR(100) NOT NULL UNIQUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS permissions (
    id BIGSERIAL PRIMARY KEY,
    resource VARCHAR(150) NOT NULL,
    action VARCHAR(150) NOT NULL,
    UNIQUE (resource, action)
);

CREATE TABLE IF NOT EXISTS user_roles (
    user_id BIGINT NOT NULL,
    role_id BIGINT NOT NULL REFERENCES roles(id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (user_id, role_id)
);

CREATE TABLE IF NOT EXISTS role_permissions (
    role_id BIGINT NOT NULL REFERENCES roles(id) ON DELETE CASCADE,
    permission_id BIGINT NOT NULL REFERENCES permissions(id) ON DELETE CASCADE,
    PRIMARY KEY (role_id, permission_id)
);

CREATE TABLE IF NOT EXISTS policies (
    id BIGSERIAL PRIMARY KEY,
    role_id BIGINT NOT NULL REFERENCES roles(id) ON DELETE CASCADE,
    resource VARCHAR(150) NOT NULL,
    action VARCHAR(150) NOT NULL,
    effect VARCHAR(20) NOT NULL CHECK (effect IN ('allow', 'deny')),
    condition_type VARCHAR(50) NOT NULL CHECK (condition_type IN ('always', 'owner')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (role_id, resource, action, effect, condition_type)
);

CREATE INDEX IF NOT EXISTS idx_user_roles_user_id ON user_roles(user_id);
CREATE INDEX IF NOT EXISTS idx_role_permissions_role_id ON role_permissions(role_id);
CREATE INDEX IF NOT EXISTS idx_policies_role_id ON policies(role_id);

INSERT INTO roles (name) VALUES ('user'), ('moderator'), ('admin') ON CONFLICT DO NOTHING;

INSERT INTO permissions (resource, action) VALUES
    ('profile', 'read'),
    ('profile', 'update'),
    ('order', 'read'),
    ('order', 'create'),
    ('order', 'update'),
    ('order', 'delete'),
    ('*', '*')
ON CONFLICT DO NOTHING;

INSERT INTO role_permissions (role_id, permission_id)
SELECT r.id, p.id
FROM roles r
JOIN permissions p ON
    (r.name = 'user' AND (p.resource, p.action) IN (('profile', 'read'), ('order', 'create'))) OR
    (r.name = 'moderator' AND (p.resource, p.action) IN (('profile', 'read'), ('order', 'read'), ('order', 'update'))) OR
    (r.name = 'admin' AND p.resource = '*' AND p.action = '*')
ON CONFLICT DO NOTHING;

INSERT INTO policies (role_id, resource, action, effect, condition_type)
SELECT id, 'profile', 'update', 'allow', 'owner' FROM roles WHERE name = 'user'
ON CONFLICT DO NOTHING;

INSERT INTO policies (role_id, resource, action, effect, condition_type)
SELECT id, 'order', 'read', 'allow', 'owner' FROM roles WHERE name = 'user'
ON CONFLICT DO NOTHING;

INSERT INTO policies (role_id, resource, action, effect, condition_type)
SELECT id, 'order', 'update', 'allow', 'owner' FROM roles WHERE name = 'user'
ON CONFLICT DO NOTHING;

INSERT INTO policies (role_id, resource, action, effect, condition_type)
SELECT id, 'order', 'delete', 'allow', 'owner' FROM roles WHERE name = 'user'
ON CONFLICT DO NOTHING;
