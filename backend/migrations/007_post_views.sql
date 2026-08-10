-- +migrate Up
CREATE TABLE post_views (
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    post_id UUID NOT NULL REFERENCES posts(id) ON DELETE CASCADE,
    viewed_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (user_id, post_id)
);

CREATE INDEX idx_post_views_user_id ON post_views(user_id);

-- +migrate Down
DROP TABLE IF EXISTS post_views;
