-- +goose Up
ALTER TABLE challenges ADD COLUMN results_chat_id INTEGER;

CREATE TABLE challenge_winners (
    challenge_id INTEGER NOT NULL,
    username TEXT NOT NULL,
    user_id INTEGER,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    PRIMARY KEY (challenge_id, username),
    FOREIGN KEY (challenge_id) REFERENCES challenges(id) ON DELETE CASCADE,
    FOREIGN KEY (user_id) REFERENCES users(id)
);

-- +goose Down
DROP TABLE challenge_winners;
ALTER TABLE challenges DROP COLUMN results_chat_id;
