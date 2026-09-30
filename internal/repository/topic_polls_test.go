package repository

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/TiraelSedai/PhotoChallengeBot/internal/db"
)

func TestTopicPollPublicationClaims(t *testing.T) {
	t.Parallel()
	database := openRepositoryTestDB(t)
	defer database.Close()
	ctx := context.Background()
	id := createRepositoryChallenge(t, database)
	repo := NewChallenges(database)
	claimedAt := testTime(50 * time.Hour)

	if claimed, err := repo.ClaimTopicPoll(ctx, id, claimedAt); err != nil || claimed {
		t.Fatalf("claim active challenge = %v, %v, want false, nil", claimed, err)
	}
	if _, err := database.Exec(`UPDATE challenges SET state = ?, finished_at = ? WHERE id = ?`, ChallengeStateFinished, timeString(claimedAt), id); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		chatID int64
		want   int
	}{{-1001, 1}, {-9999, 0}, {0, 1}} {
		due, err := repo.ListUnsentTopicPolls(ctx, tc.chatID, 0)
		if err != nil || len(due) != tc.want {
			t.Fatalf("list for chat %d = %v, %v, want %d rows", tc.chatID, due, err, tc.want)
		}
	}
	// The admin report and public poll have independent publication claims.
	if claimed, err := repo.ClaimTopicReport(ctx, id, claimedAt); err != nil || !claimed {
		t.Fatalf("claim report = %v, %v", claimed, err)
	}
	if claimed, err := repo.ClaimTopicPoll(ctx, id, claimedAt); err != nil || !claimed {
		t.Fatalf("claim poll = %v, %v", claimed, err)
	}
	if claimed, err := repo.ClaimTopicPoll(ctx, id, claimedAt.Add(time.Minute)); err != nil || claimed {
		t.Fatalf("claim while fresh = %v, %v, want false, nil", claimed, err)
	}
	reclaimedAt := claimedAt.Add(5 * time.Minute)
	if claimed, err := repo.ClaimTopicPoll(ctx, id, reclaimedAt); err != nil || !claimed {
		t.Fatalf("reclaim stale poll = %v, %v", claimed, err)
	}
	if sent, err := repo.MarkTopicPollSent(ctx, id, claimedAt, reclaimedAt); err != nil || sent {
		t.Fatalf("mark with stale claim = %v, %v, want false, nil", sent, err)
	}
	if err := repo.ReleaseTopicPollClaim(ctx, id, claimedAt); err != nil {
		t.Fatal(err)
	}
	challenge, err := repo.Get(ctx, id)
	if err != nil || challenge.TopicPollSendingAt == nil || !challenge.TopicPollSendingAt.Equal(reclaimedAt) {
		t.Fatalf("stale release changed current claim: %+v, %v", challenge, err)
	}
	if err := repo.ReleaseTopicPollClaim(ctx, id, reclaimedAt); err != nil {
		t.Fatal(err)
	}
	if claimed, err := repo.ClaimTopicPoll(ctx, id, reclaimedAt); err != nil || !claimed {
		t.Fatalf("retry released poll = %v, %v", claimed, err)
	}
	sentAt := reclaimedAt.Add(time.Second)
	if sent, err := repo.MarkTopicPollSent(ctx, id, reclaimedAt, sentAt); err != nil || !sent {
		t.Fatalf("mark sent = %v, %v", sent, err)
	}
	challenge, err = repo.Get(ctx, id)
	if err != nil || challenge.TopicPollSendingAt != nil || challenge.TopicPollSentAt == nil || !challenge.TopicPollSentAt.Equal(sentAt) {
		t.Fatalf("persisted poll state = %+v, %v", challenge, err)
	}
	if challenge.TopicReportSendingAt == nil || challenge.TopicReportSentAt != nil {
		t.Fatalf("poll changed report state: %+v", challenge)
	}
	if claimed, err := repo.ClaimTopicPoll(ctx, id, sentAt.Add(time.Hour)); err != nil || claimed {
		t.Fatalf("claim sent poll = %v, %v, want false, nil", claimed, err)
	}
	if due, err := repo.ListUnsentTopicPolls(ctx, -1001, 100); err != nil || len(due) != 0 {
		t.Fatalf("list after sent = %v, %v, want empty", due, err)
	}
	if sent, err := repo.MarkTopicPollSent(ctx, id, time.Time{}, sentAt); err == nil || sent {
		t.Fatalf("mark without claim = %v, %v, want false, error", sent, err)
	}
	if err := repo.ReleaseTopicPollClaim(ctx, id, time.Time{}); err == nil {
		t.Fatal("release without claim succeeded")
	}
}

func TestTopicPollConcurrentClaimsHaveSingleOwner(t *testing.T) {
	t.Parallel()
	database := openRepositoryTestDB(t)
	defer database.Close()
	id := createRepositoryChallenge(t, database)
	if _, err := database.Exec(`UPDATE challenges SET state = ? WHERE id = ?`, ChallengeStateFinished, id); err != nil {
		t.Fatal(err)
	}
	repo := NewChallenges(database)
	claimedAt := testTime(50 * time.Hour)
	var owners atomic.Int32
	var workers sync.WaitGroup
	start := make(chan struct{})
	for range 12 {
		workers.Add(1)
		go func() {
			defer workers.Done()
			<-start
			claimed, err := repo.ClaimTopicPoll(context.Background(), id, claimedAt)
			if err != nil {
				t.Errorf("claim: %v", err)
			}
			if claimed {
				owners.Add(1)
			}
		}()
	}
	close(start)
	workers.Wait()
	if got := owners.Load(); got != 1 {
		t.Fatalf("successful concurrent claims = %d, want 1", got)
	}
}

func TestTopicPollMigrationSkipsHistoryAndPreservesOpenChallenges(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dir := t.TempDir()
	migrationDir := filepath.Join(dir, "migrations")
	if err := os.Mkdir(migrationDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"00001_init.sql", "00002_topic_suggestions.sql", "00003_challenge_winners.sql"} {
		content, err := os.ReadFile(filepath.Join("../../migrations", name))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(migrationDir, name), content, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	path := filepath.Join(dir, "bot.sqlite")
	database, err := db.Open(ctx, db.Options{Path: path, MigrationsDir: migrationDir})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewUsers(database).Upsert(ctx, User{ID: 10, FirstName: "Admin"}); err != nil {
		t.Fatal(err)
	}
	for i, state := range []string{ChallengeStateFinished, ChallengeStateVoting, ChallengeStateActive} {
		if _, err := database.Exec(`
			INSERT INTO challenges (main_chat_id, num, theme, hashtag, state, accept_start_at,
				accept_until_at, reminder_at, created_by_user_id, created_at, updated_at)
			VALUES (?, ?, 'Night', '#night', ?, ?, ?, ?, 10, ?, ?)
		`, -1001-i, i+1, state, timeString(testTime(0)), timeString(testTime(time.Hour)),
			timeString(testTime(0)), timeString(testTime(0)), timeString(testTime(0))); err != nil {
			t.Fatal(err)
		}
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}
	database, err = db.Open(ctx, db.Options{Path: path, MigrationsDir: "../../migrations"})
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	repo := NewChallenges(database)
	challenges, err := repo.ListAll(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(challenges) != 3 {
		t.Fatalf("challenge count = %d, want 3", len(challenges))
	}
	for _, challenge := range challenges {
		if challenge.State == ChallengeStateFinished {
			if challenge.TopicPollSentAt == nil || !challenge.TopicPollSentAt.Equal(challenge.UpdatedAt) {
				t.Fatalf("historical poll not marked sent: %+v", challenge)
			}
		} else if challenge.TopicPollSentAt != nil || challenge.TopicPollSendingAt != nil {
			t.Fatalf("open challenge changed: %+v", challenge)
		}
	}
	if _, err := database.Exec(`UPDATE challenges SET state = ? WHERE state = ?`, ChallengeStateFinished, ChallengeStateVoting); err != nil {
		t.Fatal(err)
	}
	if due, err := repo.ListUnsentTopicPolls(ctx, 0, 100); err != nil || len(due) != 1 || due[0].Num != 2 {
		t.Fatalf("pending polls after upgrade = %v, %v, want newly finished challenge 2", due, err)
	}
}
