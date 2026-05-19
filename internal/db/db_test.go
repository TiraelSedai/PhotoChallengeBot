package db

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/jmoiron/sqlx"
)

func TestOpenConfiguresSQLiteAndAppliesMigrations(t *testing.T) {
	t.Parallel()

	database := openTestDB(t)
	defer database.Close()

	if got := database.Stats().MaxOpenConnections; got != 1 {
		t.Fatalf("MaxOpenConnections = %d, want 1", got)
	}

	var foreignKeys int
	if err := database.Get(&foreignKeys, "PRAGMA foreign_keys"); err != nil {
		t.Fatalf("query foreign_keys pragma: %v", err)
	}
	if foreignKeys != 1 {
		t.Fatalf("foreign_keys = %d, want 1", foreignKeys)
	}

	var busyTimeout int
	if err := database.Get(&busyTimeout, "PRAGMA busy_timeout"); err != nil {
		t.Fatalf("query busy_timeout pragma: %v", err)
	}
	if busyTimeout != 5000 {
		t.Fatalf("busy_timeout = %d, want 5000", busyTimeout)
	}

	var userTable string
	if err := database.Get(&userTable, "SELECT name FROM sqlite_master WHERE type = 'table' AND name = 'users'"); err != nil {
		t.Fatalf("query users table: %v", err)
	}
	if userTable != "users" {
		t.Fatalf("users table = %q", userTable)
	}

	for _, column := range []string{
		"reminder_sent_at",
		"reminder_sending_at",
		"reminder_message_id",
		"vote_sending_at",
		"vote_pinned_at",
		"results_sending_at",
		"results_pinned_at",
		"achievements_sending_at",
		"achievements_message_id",
		"achievements_sent_at",
	} {
		var columnName string
		if err := database.Get(&columnName, `
			SELECT name
			FROM pragma_table_info('challenges')
			WHERE name = ?
		`, column); err != nil {
			t.Fatalf("query migrated %s column: %v", column, err)
		}
		if columnName != column {
			t.Fatalf("%s column = %q", column, columnName)
		}
	}
}

func TestSchemaEnforcesOneOpenChallengePerChat(t *testing.T) {
	t.Parallel()

	database := openTestDB(t)
	defer database.Close()

	insertUser(t, database, 10)

	insertChallenge(t, database, 1, "active")
	if _, err := database.Exec(`
		INSERT INTO challenges (
			main_chat_id, num, theme, hashtag, state, accept_start_at, accept_until_at,
			reminder_at, created_by_user_id, created_at, updated_at
		)
		VALUES (-1001, 2, 'Theme 2', '#theme2', 'voting', '2026-05-18T00:00:00Z',
			'2026-06-04T15:00:00Z', '2026-06-03T09:00:00Z', 10,
			'2026-05-18T00:00:00Z', '2026-05-18T00:00:00Z')
	`); err == nil {
		t.Fatal("insert second open challenge succeeded, want unique constraint error")
	}
}

func TestSchemaEnforcesOnePhotoPerAuthorInChallenge(t *testing.T) {
	t.Parallel()

	database := openTestDB(t)
	defer database.Close()

	insertUser(t, database, 10)
	insertUser(t, database, 11)
	challengeID := insertChallenge(t, database, 1, "active")

	insertPhoto(t, database, challengeID, 11)
	if _, err := database.Exec(`
		INSERT INTO photos (
			challenge_id, author_user_id, file_id, file_unique_id, source_chat_id,
			source_message_id, submitted_at, updated_at
		)
		VALUES (?, 11, 'file-2', 'unique-2', -1001, 102,
			'2026-05-18T00:00:00Z', '2026-05-18T00:00:00Z')
	`, challengeID); err == nil {
		t.Fatal("insert second author photo succeeded, want unique constraint error")
	}
}

func TestSchemaEnforcesVoteUniqueness(t *testing.T) {
	t.Parallel()

	database := openTestDB(t)
	defer database.Close()

	insertUser(t, database, 10)
	insertUser(t, database, 11)
	insertUser(t, database, 12)
	challengeID := insertChallenge(t, database, 1, "active")
	photoID := insertPhoto(t, database, challengeID, 11)

	insertVote(t, database, challengeID, 12, photoID)
	if _, err := database.Exec(`
		INSERT INTO votes (challenge_id, voter_user_id, photo_id, kind, created_at)
		VALUES (?, 12, ?, 'manual', '2026-05-18T00:00:00Z')
	`, challengeID, photoID); err == nil {
		t.Fatal("insert duplicate vote succeeded, want primary key error")
	}
}

func TestSchemaRejectsVoteForPhotoFromDifferentChallenge(t *testing.T) {
	t.Parallel()

	database := openTestDB(t)
	defer database.Close()

	insertUser(t, database, 10)
	insertUser(t, database, 11)
	insertUser(t, database, 12)
	firstChallengeID := insertChallenge(t, database, 1, "finished")
	secondChallengeID := insertChallenge(t, database, 2, "active")
	photoID := insertPhoto(t, database, firstChallengeID, 11)

	if _, err := database.Exec(`
		INSERT INTO votes (challenge_id, voter_user_id, photo_id, kind, created_at)
		VALUES (?, 12, ?, 'manual', '2026-05-18T00:00:00Z')
	`, secondChallengeID, photoID); err == nil {
		t.Fatal("insert vote with cross-challenge photo succeeded, want foreign key error")
	}

	if _, err := database.Exec(`
		INSERT INTO vote_orders (challenge_id, voter_user_id, position, photo_id)
		VALUES (?, 12, 0, ?)
	`, secondChallengeID, photoID); err == nil {
		t.Fatal("insert vote order with cross-challenge photo succeeded, want foreign key error")
	}
}

func openTestDB(t *testing.T) *sqlx.DB {
	t.Helper()

	databasePath := filepath.Join(t.TempDir(), "bot.sqlite")
	database, err := Open(context.Background(), Options{
		Path:          databasePath,
		MigrationsDir: "../../migrations",
	})
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	return database
}

func insertUser(t *testing.T, database *sqlx.DB, id int64) {
	t.Helper()

	if _, err := database.Exec(`
		INSERT INTO users (id, display_name, updated_at)
		VALUES (?, ?, '2026-05-18T00:00:00Z')
	`, id, "User"); err != nil {
		t.Fatalf("insert user: %v", err)
	}
}

func insertChallenge(t *testing.T, database *sqlx.DB, num int, state string) int64 {
	t.Helper()

	result, err := database.Exec(`
		INSERT INTO challenges (
			main_chat_id, num, theme, hashtag, state, accept_start_at, accept_until_at,
			reminder_at, created_by_user_id, created_at, updated_at
		)
		VALUES (-1001, ?, 'Theme', '#theme', ?, '2026-05-18T00:00:00Z',
			'2026-06-04T15:00:00Z', '2026-06-03T09:00:00Z', 10,
			'2026-05-18T00:00:00Z', '2026-05-18T00:00:00Z')
	`, num, state)
	if err != nil {
		t.Fatalf("insert challenge: %v", err)
	}

	id, err := result.LastInsertId()
	if err != nil {
		t.Fatalf("challenge LastInsertId: %v", err)
	}
	return id
}

func insertPhoto(t *testing.T, database *sqlx.DB, challengeID, authorID int64) int64 {
	t.Helper()

	result, err := database.Exec(`
		INSERT INTO photos (
			challenge_id, author_user_id, file_id, file_unique_id, source_chat_id,
			source_message_id, submitted_at, updated_at
		)
		VALUES (?, ?, 'file-1', 'unique-1', -1001, 101,
			'2026-05-18T00:00:00Z', '2026-05-18T00:00:00Z')
	`, challengeID, authorID)
	if err != nil {
		t.Fatalf("insert photo: %v", err)
	}

	id, err := result.LastInsertId()
	if err != nil {
		t.Fatalf("photo LastInsertId: %v", err)
	}
	return id
}

func insertVote(t *testing.T, database *sqlx.DB, challengeID, voterID, photoID int64) {
	t.Helper()

	if _, err := database.Exec(`
		INSERT INTO votes (challenge_id, voter_user_id, photo_id, kind, created_at)
		VALUES (?, ?, ?, 'manual', '2026-05-18T00:00:00Z')
	`, challengeID, voterID, photoID); err != nil {
		t.Fatalf("insert vote: %v", err)
	}
}
