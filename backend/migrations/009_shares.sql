-- +migrate Up
-- Per-user share dedupe so preference vector is applied once per (user, post).
CREATE TABLE shares (
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    post_id UUID NOT NULL REFERENCES posts(id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (user_id, post_id)
);

CREATE INDEX idx_shares_post_id ON shares(post_id);

-- +migrate Down
DROP TABLE IF EXISTS shares;
