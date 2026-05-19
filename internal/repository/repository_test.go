package repository

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/TiraelSedai/PhotoChallengeBot/internal/db"
	"github.com/jmoiron/sqlx"
)

func TestUsersUpsertStoresTelegramProfile(t *testing.T) {
	t.Parallel()

	database := openRepositoryTestDB(t)
	defer database.Close()

	repo := NewUsers(database)
	now := time.Date(2026, 5, 18, 10, 0, 0, 0, time.UTC)

	user, err := repo.Upsert(context.Background(), User{
		ID:        1001,
		Username:  "@alice",
		FirstName: "Alice",
		LastName:  "Example",
		UpdatedAt: now,
	})
	if err != nil {
		t.Fatalf("Upsert() error = %v", err)
	}

	if user.Username != "alice" {
		t.Fatalf("Username = %q, want alice", user.Username)
	}
	if user.DisplayName != "Alice Example" {
		t.Fatalf("DisplayName = %q, want Alice Example", user.DisplayName)
	}

	updated, err := repo.Upsert(context.Background(), User{
		ID:        1001,
		Username:  "alice_new",
		FirstName: "Alice",
		UpdatedAt: now.Add(time.Hour),
	})
	if err != nil {
		t.Fatalf("second Upsert() error = %v", err)
	}
	if updated.Username != "alice_new" {
		t.Fatalf("updated Username = %q, want alice_new", updated.Username)
	}
}

func TestChallengesCreateAndFindOpen(t *testing.T) {
	t.Parallel()

	database := openRepositoryTestDB(t)
	defer database.Close()
	ctx := context.Background()

	users := NewUsers(database)
	if _, err := users.Upsert(ctx, User{ID: 10, FirstName: "Admin"}); err != nil {
		t.Fatalf("upsert admin: %v", err)
	}

	challenges := NewChallenges(database)
	created, err := challenges.Create(ctx, CreateChallengeInput{
		MainChatID:      -1001,
		Num:             3,
		Theme:           "Ночь",
		Hashtag:         "#night",
		AcceptStartAt:   testTime(0),
		AcceptUntilAt:   testTime(17 * 24 * time.Hour),
		ReminderAt:      testTime(17*24*time.Hour - 30*time.Hour),
		CreatedByUserID: 10,
		CreatedAt:       testTime(0),
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if created.State != ChallengeStateActive {
		t.Fatalf("State = %q, want active", created.State)
	}

	open, err := challenges.FindOpenByMainChatID(ctx, -1001)
	if err != nil {
		t.Fatalf("FindOpenByMainChatID() error = %v", err)
	}
	if open == nil || open.ID != created.ID {
		t.Fatalf("open challenge = %#v, want id %d", open, created.ID)
	}
}

func TestPhotosUpsertReplacesCurrentPhoto(t *testing.T) {
	t.Parallel()

	database := openRepositoryTestDB(t)
	defer database.Close()
	ctx := context.Background()
	challengeID := createRepositoryChallenge(t, database)
	photos := NewPhotos(database)

	first, replaced, err := photos.UpsertCurrent(ctx, UpsertPhotoInput{
		ChallengeID:     challengeID,
		AuthorUserID:    11,
		FileID:          "file-1",
		FileUniqueID:    "unique-1",
		SourceChatID:    -1001,
		SourceMessageID: 101,
		Caption:         "#night",
		SubmittedAt:     testTime(0),
	})
	if err != nil {
		t.Fatalf("first UpsertCurrent() error = %v", err)
	}
	if replaced {
		t.Fatal("first UpsertCurrent() replaced = true, want false")
	}

	second, replaced, err := photos.UpsertCurrent(ctx, UpsertPhotoInput{
		ChallengeID:     challengeID,
		AuthorUserID:    11,
		FileID:          "file-2",
		FileUniqueID:    "unique-2",
		SourceChatID:    -1001,
		SourceMessageID: 102,
		Caption:         "#night replacement",
		SubmittedAt:     testTime(time.Hour),
	})
	if err != nil {
		t.Fatalf("second UpsertCurrent() error = %v", err)
	}
	if !replaced {
		t.Fatal("second UpsertCurrent() replaced = false, want true")
	}
	if second.ID != first.ID {
		t.Fatalf("second ID = %d, want same row %d", second.ID, first.ID)
	}
	if second.FileID != "file-2" || second.SourceMessageID != 102 {
		t.Fatalf("second photo = %#v, want replacement data", second)
	}
}

func TestPhotosDeleteCurrentByAuthorID(t *testing.T) {
	t.Parallel()

	database := openRepositoryTestDB(t)
	defer database.Close()
	ctx := context.Background()
	challengeID := createRepositoryChallenge(t, database)
	photos := NewPhotos(database)

	photo := createRepositoryPhoto(t, photos, challengeID, 11, "file-1")
	deleted, err := photos.DeleteByAuthorID(ctx, challengeID, 11)
	if err != nil {
		t.Fatalf("DeleteByAuthorID() error = %v", err)
	}
	if deleted == nil || deleted.ID != photo.ID {
		t.Fatalf("deleted = %#v, want photo id %d", deleted, photo.ID)
	}

	if _, err := photos.Get(ctx, photo.ID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("Get(deleted) error = %v, want sql.ErrNoRows", err)
	}
}

func TestPhotosDeleteCurrentByUsername(t *testing.T) {
	t.Parallel()

	database := openRepositoryTestDB(t)
	defer database.Close()
	ctx := context.Background()
	challengeID := createRepositoryChallenge(t, database)
	photos := NewPhotos(database)

	photo := createRepositoryPhoto(t, photos, challengeID, 11, "file-1")
	deleted, err := photos.DeleteByUsername(ctx, challengeID, "@Author")
	if err != nil {
		t.Fatalf("DeleteByUsername() error = %v", err)
	}
	if deleted == nil || deleted.ID != photo.ID {
		t.Fatalf("deleted = %#v, want photo id %d", deleted, photo.ID)
	}
}

func TestPhotosDeleteCurrentByUsernameRejectsAmbiguousMatch(t *testing.T) {
	t.Parallel()

	database := openRepositoryTestDB(t)
	defer database.Close()
	ctx := context.Background()
	challengeID := createRepositoryChallenge(t, database)
	users := NewUsers(database)
	if _, err := users.Upsert(ctx, User{ID: 12, Username: "author", FirstName: "Other"}); err != nil {
		t.Fatalf("upsert duplicate username user: %v", err)
	}

	photos := NewPhotos(database)
	first := createRepositoryPhoto(t, photos, challengeID, 11, "file-1")
	second := createRepositoryPhoto(t, photos, challengeID, 12, "file-2")

	deleted, err := photos.DeleteByUsername(ctx, challengeID, "@Author")
	if !errors.Is(err, ErrAmbiguousUsername) {
		t.Fatalf("DeleteByUsername() error = %v, want ErrAmbiguousUsername", err)
	}
	if deleted != nil {
		t.Fatalf("deleted = %#v, want nil", deleted)
	}
	if _, err := photos.Get(ctx, first.ID); err != nil {
		t.Fatalf("first photo was deleted after ambiguous username: %v", err)
	}
	if _, err := photos.Get(ctx, second.ID); err != nil {
		t.Fatalf("second photo was deleted after ambiguous username: %v", err)
	}
}

func openRepositoryTestDB(t *testing.T) *sqlx.DB {
	t.Helper()

	database, err := db.Open(context.Background(), db.Options{
		Path:          filepath.Join(t.TempDir(), "bot.sqlite"),
		MigrationsDir: "../../migrations",
	})
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	return database
}

func createRepositoryChallenge(t *testing.T, database *sqlx.DB) int64 {
	t.Helper()
	ctx := context.Background()

	users := NewUsers(database)
	for _, user := range []User{
		{ID: 10, FirstName: "Admin"},
		{ID: 11, Username: "Author", FirstName: "Photo", LastName: "Author"},
	} {
		if _, err := users.Upsert(ctx, user); err != nil {
			t.Fatalf("upsert user %d: %v", user.ID, err)
		}
	}

	challenge, err := NewChallenges(database).Create(ctx, CreateChallengeInput{
		MainChatID:      -1001,
		Num:             1,
		Theme:           "Night",
		Hashtag:         "#night",
		AcceptStartAt:   testTime(0),
		AcceptUntilAt:   testTime(17 * 24 * time.Hour),
		ReminderAt:      testTime(17*24*time.Hour - 30*time.Hour),
		CreatedByUserID: 10,
		CreatedAt:       testTime(0),
	})
	if err != nil {
		t.Fatalf("create challenge: %v", err)
	}
	return challenge.ID
}

func createRepositoryPhoto(t *testing.T, photos *Photos, challengeID, authorID int64, fileID string) Photo {
	t.Helper()

	photo, _, err := photos.UpsertCurrent(context.Background(), UpsertPhotoInput{
		ChallengeID:     challengeID,
		AuthorUserID:    authorID,
		FileID:          fileID,
		FileUniqueID:    fileID + "-unique",
		SourceChatID:    -1001,
		SourceMessageID: 101,
		Caption:         "#night",
		SubmittedAt:     testTime(0),
	})
	if err != nil {
		t.Fatalf("upsert photo: %v", err)
	}
	return photo
}

func testTime(offset time.Duration) time.Time {
	return time.Date(2026, 5, 18, 12, 0, 0, 0, time.UTC).Add(offset)
}
