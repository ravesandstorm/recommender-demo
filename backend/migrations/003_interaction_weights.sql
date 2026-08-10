-- +migrate Up
CREATE TABLE interaction_weights (
    interaction_type TEXT PRIMARY KEY,
    weight DOUBLE PRECISION NOT NULL
);

INSERT INTO interaction_weights (interaction_type, weight) VALUES
    ('like', 1.0),
    ('dislike', -1.0),
    ('save', 1.5),
    ('comment', 2.0),
    ('share', 1.2);

-- +migrate Down
DROP TABLE IF EXISTS interaction_weights;
