-- +migrate Up
CREATE TABLE saves (
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    post_id UUID NOT NULL REFERENCES posts(id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (user_id, post_id)
);

CREATE INDEX idx_saves_post_id ON saves(post_id);

-- +migrate Down
DROP TABLE IF EXISTS saves;
