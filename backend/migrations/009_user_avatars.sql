ALTER TABLE users
    ADD COLUMN avatar_data BYTEA,
    ADD COLUMN avatar_media_type TEXT,
    ADD COLUMN avatar_updated_at TIMESTAMPTZ;

ALTER TABLE users
    ADD CONSTRAINT users_avatar_consistency CHECK (
        (avatar_data IS NULL AND avatar_media_type IS NULL AND avatar_updated_at IS NULL)
        OR
        (
            avatar_data IS NOT NULL
            AND octet_length(avatar_data) BETWEEN 1 AND 2097152
            AND avatar_media_type IN ('image/jpeg', 'image/png', 'image/webp')
            AND avatar_updated_at IS NOT NULL
        )
    );
