-- +migrate Up
ALTER TABLE posts ADD COLUMN IF NOT EXISTS view_count INTEGER NOT NULL DEFAULT 0;

CREATE INDEX IF NOT EXISTS idx_post_views_user_viewed_at
    ON post_views (user_id, viewed_at DESC);

-- +migrate Down
DROP INDEX IF EXISTS idx_post_views_user_viewed_at;
ALTER TABLE posts DROP COLUMN IF EXISTS view_count;
