-- +goose Up
ALTER TABLE challenges ADD COLUMN topic_report_sending_at TEXT;
ALTER TABLE challenges ADD COLUMN topic_report_sent_at TEXT;

UPDATE challenges
SET topic_report_sent_at = updated_at
WHERE state = 'finished';

CREATE TABLE topic_suggestions (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    challenge_id INTEGER NOT NULL,
    author_user_id INTEGER NOT NULL,
    source_chat_id INTEGER NOT NULL,
    source_message_id INTEGER NOT NULL,
    text TEXT NOT NULL,
    suggested_at TEXT NOT NULL,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    FOREIGN KEY (challenge_id) REFERENCES challenges(id) ON DELETE CASCADE,
    FOREIGN KEY (author_user_id) REFERENCES users(id),
    UNIQUE (challenge_id, source_chat_id, source_message_id)
);

CREATE INDEX topic_suggestions_challenge_idx
    ON topic_suggestions(challenge_id);

-- +goose Down
DROP INDEX topic_suggestions_challenge_idx;
DROP TABLE topic_suggestions;
ALTER TABLE challenges DROP COLUMN topic_report_sent_at;
ALTER TABLE challenges DROP COLUMN topic_report_sending_at;
