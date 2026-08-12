-- +migrate Up
-- Retune relative strengths for L2-normalized preference updates.
-- like stays 1.0 (anchor). dislike softer than like. comment/share >1.3 but below old 2.0.
UPDATE interaction_weights SET weight = 1.0  WHERE interaction_type = 'like';
UPDATE interaction_weights SET weight = -0.8 WHERE interaction_type = 'dislike';
UPDATE interaction_weights SET weight = 1.5  WHERE interaction_type = 'save';
UPDATE interaction_weights SET weight = 1.4  WHERE interaction_type = 'share';
UPDATE interaction_weights SET weight = 1.7  WHERE interaction_type = 'comment';

-- +migrate Down
UPDATE interaction_weights SET weight = 1.0  WHERE interaction_type = 'like';
UPDATE interaction_weights SET weight = -1.0 WHERE interaction_type = 'dislike';
UPDATE interaction_weights SET weight = 1.5  WHERE interaction_type = 'save';
UPDATE interaction_weights SET weight = 1.2  WHERE interaction_type = 'share';
UPDATE interaction_weights SET weight = 2.0  WHERE interaction_type = 'comment';
