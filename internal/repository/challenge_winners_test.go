package repository

import (
	"context"
	"testing"
	"time"
)

func TestChallengeWinnersUpsertBackfillsUserID(t *testing.T) {
	t.Parallel()

	database := openRepositoryTestDB(t)
	defer database.Close()

	ctx := context.Background()
	challengeID := createRepositoryChallenge(t, database)
	repo := NewChallengeWinners(database)
	now := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)

	if err := repo.Upsert(ctx, ChallengeWinner{
		ChallengeID: challengeID,
		Username:    "@alice ",
		CreatedAt:   now,
	}); err != nil {
		t.Fatalf("Upsert() error = %v", err)
	}

	userID := int64(1001)
	if _, err := NewUsers(database).Upsert(ctx, User{ID: userID, FirstName: "Alice"}); err != nil {
		t.Fatalf("upsert user: %v", err)
	}
	if err := repo.Upsert(ctx, ChallengeWinner{
		ChallengeID: challengeID,
		Username:    "alice",
		UserID:      &userID,
		CreatedAt:   now.Add(time.Hour),
	}); err != nil {
		t.Fatalf("second Upsert() error = %v", err)
	}

	winners, err := repo.ListAll(ctx)
	if err != nil {
		t.Fatalf("ListAll() error = %v", err)
	}
	rows := winners[challengeID]
	if len(rows) != 1 {
		t.Fatalf("winners count = %d, want 1", len(rows))
	}
	if rows[0].Username != "alice" {
		t.Fatalf("Username = %q, want alice", rows[0].Username)
	}
	if rows[0].UserID == nil || *rows[0].UserID != userID {
		t.Fatalf("UserID = %v, want %d", rows[0].UserID, userID)
	}

	if err := repo.Upsert(ctx, ChallengeWinner{
		ChallengeID: challengeID,
		Username:    "alice",
		CreatedAt:   now.Add(2 * time.Hour),
	}); err != nil {
		t.Fatalf("third Upsert() error = %v", err)
	}
	winners, err = repo.ListAll(ctx)
	if err != nil {
		t.Fatalf("ListAll() after nil-user upsert error = %v", err)
	}
	if rows := winners[challengeID]; rows[0].UserID == nil || *rows[0].UserID != userID {
		t.Fatalf("UserID after nil-user upsert = %v, want %d kept", rows[0].UserID, userID)
	}
}

func TestChallengeWinnersUpsertRejectsEmptyUsername(t *testing.T) {
	t.Parallel()

	database := openRepositoryTestDB(t)
	defer database.Close()

	repo := NewChallengeWinners(database)
	if err := repo.Upsert(context.Background(), ChallengeWinner{ChallengeID: 1, Username: " @ "}); err == nil {
		t.Fatal("Upsert() with empty username expected error")
	}
}

func TestChallengeResultsChatFallsBackToMainChat(t *testing.T) {
	t.Parallel()

	database := openRepositoryTestDB(t)
	defer database.Close()

	ctx := context.Background()
	challengeID := createRepositoryChallenge(t, database)
	repo := NewChallenges(database)

	challenge, err := repo.Get(ctx, challengeID)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got := challenge.ResultsChat(); got != challenge.MainChatID {
		t.Fatalf("ResultsChat() = %d, want main chat %d", got, challenge.MainChatID)
	}

	if _, err := database.ExecContext(ctx, `
		UPDATE challenges SET results_chat_id = ? WHERE id = ?
	`, int64(-1001272818469), challengeID); err != nil {
		t.Fatalf("set results_chat_id: %v", err)
	}

	challenge, err = repo.Get(ctx, challengeID)
	if err != nil {
		t.Fatalf("Get() after update error = %v", err)
	}
	if challenge.ResultsChatID == nil || *challenge.ResultsChatID != -1001272818469 {
		t.Fatalf("ResultsChatID = %v, want -1001272818469", challenge.ResultsChatID)
	}
	if got := challenge.ResultsChat(); got != -1001272818469 {
		t.Fatalf("ResultsChat() = %d, want -1001272818469", got)
	}
}
