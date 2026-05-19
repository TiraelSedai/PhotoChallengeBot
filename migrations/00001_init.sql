-- +goose Up
CREATE TABLE users (
    id INTEGER PRIMARY KEY,
    username TEXT,
    first_name TEXT,
    last_name TEXT,
    display_name TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE TABLE challenges (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    main_chat_id INTEGER NOT NULL,
    num INTEGER NOT NULL,
    theme TEXT NOT NULL,
    hashtag TEXT NOT NULL,
    state TEXT NOT NULL CHECK (state IN ('active', 'voting', 'finished')),
    accept_start_at TEXT NOT NULL,
    accept_until_at TEXT NOT NULL,
    reminder_at TEXT NOT NULL,
    reminder_sending_at TEXT,
    reminder_sent_at TEXT,
    reminder_message_id INTEGER,
    vote_started_at TEXT,
    vote_until_at TEXT,
    vote_sending_at TEXT,
    finished_at TEXT,
    announcement_message_id INTEGER,
    vote_message_id INTEGER,
    vote_pinned_at TEXT,
    results_sending_at TEXT,
    results_message_id INTEGER,
    results_pinned_at TEXT,
    achievements_sending_at TEXT,
    achievements_message_id INTEGER,
    achievements_sent_at TEXT,
    created_by_user_id INTEGER NOT NULL,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    FOREIGN KEY (created_by_user_id) REFERENCES users(id)
);

CREATE UNIQUE INDEX challenges_main_chat_num_idx
    ON challenges(main_chat_id, num);

CREATE UNIQUE INDEX challenges_one_open_idx
    ON challenges(main_chat_id)
    WHERE state IN ('active', 'voting');

CREATE INDEX challenges_main_chat_state_idx
    ON challenges(main_chat_id, state);

CREATE TABLE photos (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    challenge_id INTEGER NOT NULL,
    author_user_id INTEGER NOT NULL,
    file_id TEXT NOT NULL,
    file_unique_id TEXT,
    source_chat_id INTEGER NOT NULL,
    source_message_id INTEGER NOT NULL,
    caption TEXT,
    submitted_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    FOREIGN KEY (challenge_id) REFERENCES challenges(id) ON DELETE CASCADE,
    FOREIGN KEY (author_user_id) REFERENCES users(id),
    UNIQUE (challenge_id, author_user_id)
);

CREATE INDEX photos_challenge_idx
    ON photos(challenge_id);

CREATE UNIQUE INDEX photos_id_challenge_idx
    ON photos(id, challenge_id);

CREATE TABLE vote_orders (
    challenge_id INTEGER NOT NULL,
    voter_user_id INTEGER NOT NULL,
    position INTEGER NOT NULL,
    photo_id INTEGER NOT NULL,
    PRIMARY KEY (challenge_id, voter_user_id, position),
    FOREIGN KEY (challenge_id) REFERENCES challenges(id) ON DELETE CASCADE,
    FOREIGN KEY (voter_user_id) REFERENCES users(id),
    FOREIGN KEY (photo_id, challenge_id) REFERENCES photos(id, challenge_id) ON DELETE CASCADE,
    UNIQUE (challenge_id, voter_user_id, photo_id)
);

CREATE TABLE vote_progress (
    challenge_id INTEGER NOT NULL,
    voter_user_id INTEGER NOT NULL,
    current_position INTEGER NOT NULL,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    PRIMARY KEY (challenge_id, voter_user_id),
    FOREIGN KEY (challenge_id) REFERENCES challenges(id) ON DELETE CASCADE,
    FOREIGN KEY (voter_user_id) REFERENCES users(id)
);

CREATE TABLE votes (
    challenge_id INTEGER NOT NULL,
    voter_user_id INTEGER NOT NULL,
    photo_id INTEGER NOT NULL,
    kind TEXT NOT NULL CHECK (kind IN ('manual', 'self')),
    created_at TEXT NOT NULL,
    PRIMARY KEY (challenge_id, voter_user_id, photo_id),
    FOREIGN KEY (challenge_id) REFERENCES challenges(id) ON DELETE CASCADE,
    FOREIGN KEY (voter_user_id) REFERENCES users(id),
    FOREIGN KEY (photo_id, challenge_id) REFERENCES photos(id, challenge_id) ON DELETE CASCADE
);

CREATE INDEX votes_challenge_photo_idx
    ON votes(challenge_id, photo_id);

CREATE TABLE admin_sessions (
    admin_chat_id INTEGER NOT NULL,
    admin_user_id INTEGER NOT NULL,
    flow TEXT NOT NULL,
    step TEXT NOT NULL,
    payload_json TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    PRIMARY KEY (admin_chat_id, admin_user_id)
);

-- +goose Down
DROP TABLE admin_sessions;
DROP TABLE votes;
DROP TABLE vote_progress;
DROP TABLE vote_orders;
DROP TABLE photos;
DROP TABLE challenges;
DROP TABLE users;
