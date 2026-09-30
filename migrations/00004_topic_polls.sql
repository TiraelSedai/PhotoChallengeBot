-- +goose Up
ALTER TABLE challenges ADD COLUMN topic_poll_sending_at TEXT;
ALTER TABLE challenges ADD COLUMN topic_poll_sent_at TEXT;

UPDATE challenges
SET topic_poll_sent_at = updated_at
WHERE state = 'finished';

-- +goose Down
ALTER TABLE challenges DROP COLUMN topic_poll_sent_at;
ALTER TABLE challenges DROP COLUMN topic_poll_sending_at;
