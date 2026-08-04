ALTER TABLE users
    ADD COLUMN role TEXT NOT NULL DEFAULT 'user' CHECK (role IN ('user', 'admin')),
    ADD COLUMN must_change_password BOOLEAN NOT NULL DEFAULT false;

CREATE INDEX users_role_idx ON users(role);
