package repository

import (
	"context"
	"testing"
	"time"

	"github.com/jmoiron/sqlx"
)

func TestChallengeWinnersUpsertManyBackfillsUserID(t *testing.T) {
	t.Parallel()

	database := openRepositoryTestDB(t)
	defer database.Close()

	ctx := context.Background()
	challengeID := createRepositoryChallenge(t, database)
	repo := NewChallengeWinners(database)
	now := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)

	if err := repo.UpsertMany(ctx, []ChallengeWinner{{
		ChallengeID: challengeID,
		Username:    "@alice ",
		CreatedAt:   now,
	}}); err != nil {
		t.Fatalf("UpsertMany() error = %v", err)
	}

	userID := int64(1001)
	if _, err := NewUsers(database).Upsert(ctx, User{ID: userID, FirstName: "Alice"}); err != nil {
		t.Fatalf("upsert user: %v", err)
	}
	if err := repo.UpsertMany(ctx, []ChallengeWinner{{
		ChallengeID: challengeID,
		Username:    "alice",
		UserID:      &userID,
		CreatedAt:   now.Add(time.Hour),
	}}); err != nil {
		t.Fatalf("second UpsertMany() error = %v", err)
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

	if err := repo.UpsertMany(ctx, []ChallengeWinner{{
		ChallengeID: challengeID,
		Username:    "alice",
		CreatedAt:   now.Add(2 * time.Hour),
	}}); err != nil {
		t.Fatalf("third UpsertMany() error = %v", err)
	}
	winners, err = repo.ListAll(ctx)
	if err != nil {
		t.Fatalf("ListAll() after nil-user upsert error = %v", err)
	}
	if rows := winners[challengeID]; rows[0].UserID == nil || *rows[0].UserID != userID {
		t.Fatalf("UserID after nil-user upsert = %v, want %d kept", rows[0].UserID, userID)
	}
}

func TestChallengeWinnersUpsertManyRejectsEmptyUsernameAtomically(t *testing.T) {
	t.Parallel()

	database := openRepositoryTestDB(t)
	defer database.Close()

	ctx := context.Background()
	challengeID := createRepositoryChallenge(t, database)
	repo := NewChallengeWinners(database)

	if err := repo.UpsertMany(ctx, []ChallengeWinner{
		{ChallengeID: challengeID, Username: "valid"},
		{ChallengeID: challengeID, Username: " @ "},
	}); err == nil {
		t.Fatal("UpsertMany() with empty username expected error")
	}

	winners, err := repo.ListAll(ctx)
	if err != nil {
		t.Fatalf("ListAll() error = %v", err)
	}
	if len(winners[challengeID]) != 0 {
		t.Fatalf("winners = %#v, want none after a rejected batch rolls back", winners[challengeID])
	}
}

func TestChallengeWinnersCountWinsByUserThrough(t *testing.T) {
	t.Parallel()

	database := openRepositoryTestDB(t)
	defer database.Close()

	ctx := context.Background()
	users := NewUsers(database)
	for _, user := range []User{
		{ID: 10, FirstName: "Admin"},
		{ID: 100, Username: "alice", FirstName: "Alice"},
		{ID: 200, Username: "bob", FirstName: "Bob"},
	} {
		if _, err := users.Upsert(ctx, user); err != nil {
			t.Fatalf("upsert user %d: %v", user.ID, err)
		}
	}

	base := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	c1 := createFinishedRepositoryChallenge(t, database, 1, base)
	c2 := createFinishedRepositoryChallenge(t, database, 2, base.Add(24*time.Hour))
	c2b := createFinishedRepositoryChallenge(t, database, 3, base.Add(24*time.Hour)) // same finished_at as c2, later id
	c3 := createFinishedRepositoryChallenge(t, database, 4, base.Add(48*time.Hour))
	cActive := createRepositoryChallengeWithNum(t, database, 5) // active, finished_at NULL

	alice, bob := int64(100), int64(200)
	repo := NewChallengeWinners(database)
	if err := repo.UpsertMany(ctx, []ChallengeWinner{
		{ChallengeID: c1, Username: "alice", UserID: &alice},
		{ChallengeID: c1, Username: "alice-alt", UserID: &alice}, // duplicate user in same challenge
		{ChallengeID: c2, Username: "alice", UserID: &alice},
		{ChallengeID: c2, Username: "bob", UserID: &bob},
		{ChallengeID: c2b, Username: "alice", UserID: &alice},
		{ChallengeID: c3, Username: "alice", UserID: &alice},
		{ChallengeID: c3, Username: "bob", UserID: &bob},
		{ChallengeID: cActive, Username: "alice", UserID: &alice}, // not finished, must be ignored
	}); err != nil {
		t.Fatalf("seed winners: %v", err)
	}

	cases := []struct {
		name       string
		userID     int64
		finishedAt time.Time
		challenge  int64
		want       int
	}{
		{"alice through c1 collapses duplicate", alice, base, c1, 1},
		{"alice through c2 excludes equal-time later id", alice, base.Add(24 * time.Hour), c2, 2},
		{"alice through c2b includes equal-time same id", alice, base.Add(24 * time.Hour), c2b, 3},
		{"alice through c3 ignores active challenge", alice, base.Add(48 * time.Hour), c3, 4},
		{"bob through c3", bob, base.Add(48 * time.Hour), c3, 2},
		{"bob through c1 before first win", bob, base, c1, 0},
	}
	for _, tc := range cases {
		got, err := repo.CountWinsByUserThrough(ctx, tc.userID, tc.finishedAt, tc.challenge)
		if err != nil {
			t.Fatalf("%s: CountWinsByUserThrough() error = %v", tc.name, err)
		}
		if got != tc.want {
			t.Fatalf("%s: count = %d, want %d", tc.name, got, tc.want)
		}
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

func createRepositoryChallengeWithNum(t *testing.T, database *sqlx.DB, num int) int64 {
	t.Helper()
	ctx := context.Background()
	if _, err := NewUsers(database).Upsert(ctx, User{ID: 10, FirstName: "Admin"}); err != nil {
		t.Fatalf("upsert admin: %v", err)
	}
	challenge, err := NewChallenges(database).Create(ctx, CreateChallengeInput{
		MainChatID:      -1001,
		Num:             num,
		Theme:           "Night",
		Hashtag:         "#night",
		AcceptStartAt:   testTime(0),
		AcceptUntilAt:   testTime(17 * 24 * time.Hour),
		ReminderAt:      testTime(17*24*time.Hour - 30*time.Hour),
		CreatedByUserID: 10,
		CreatedAt:       testTime(0),
	})
	if err != nil {
		t.Fatalf("create challenge %d: %v", num, err)
	}
	return challenge.ID
}

func createFinishedRepositoryChallenge(t *testing.T, database *sqlx.DB, num int, finishedAt time.Time) int64 {
	t.Helper()
	ctx := context.Background()
	challengeID := createRepositoryChallengeWithNum(t, database, num)
	if _, err := database.ExecContext(ctx, `
		UPDATE challenges SET state = ?, finished_at = ? WHERE id = ?
	`, ChallengeStateFinished, timeString(finishedAt), challengeID); err != nil {
		t.Fatalf("finish challenge %d: %v", num, err)
	}
	return challengeID
}
