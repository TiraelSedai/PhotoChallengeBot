package repository

import (
	"context"
	"testing"
	"time"
)

func TestVotesCreateAndListVoteOrder(t *testing.T) {
	t.Parallel()

	database := openRepositoryTestDB(t)
	defer database.Close()
	ctx := context.Background()
	challengeID := createRepositoryChallenge(t, database)
	users := NewUsers(database)
	if _, err := users.Upsert(ctx, User{ID: 12, FirstName: "Second", LastName: "Author"}); err != nil {
		t.Fatalf("upsert second author: %v", err)
	}
	if _, err := users.Upsert(ctx, User{ID: 20, FirstName: "Voter"}); err != nil {
		t.Fatalf("upsert voter: %v", err)
	}
	photos := NewPhotos(database)
	firstPhoto := createRepositoryPhoto(t, photos, challengeID, 11, "file-1")
	secondPhoto := createRepositoryPhoto(t, photos, challengeID, 12, "file-2")
	votes := NewVotes(database)

	created, err := votes.CreateVoteOrder(ctx, challengeID, 20, []int64{secondPhoto.ID, firstPhoto.ID})
	if err != nil {
		t.Fatalf("CreateVoteOrder() error = %v", err)
	}
	if len(created) != 2 {
		t.Fatalf("created order length = %d, want 2", len(created))
	}
	if created[0].Position != 0 || created[0].PhotoID != secondPhoto.ID {
		t.Fatalf("created[0] = %#v, want position 0 photo %d", created[0], secondPhoto.ID)
	}

	listed, err := votes.ListVoteOrder(ctx, challengeID, 20)
	if err != nil {
		t.Fatalf("ListVoteOrder() error = %v", err)
	}
	if len(listed) != 2 {
		t.Fatalf("listed order length = %d, want 2", len(listed))
	}
	if listed[0].PhotoID != secondPhoto.ID || listed[1].PhotoID != firstPhoto.ID {
		t.Fatalf("listed order = %#v, want stored order", listed)
	}

	createdAgain, err := votes.CreateVoteOrder(ctx, challengeID, 20, []int64{firstPhoto.ID})
	if err != nil {
		t.Fatalf("duplicate CreateVoteOrder() error = %v", err)
	}
	if len(createdAgain) != 2 || createdAgain[0].PhotoID != secondPhoto.ID || createdAgain[1].PhotoID != firstPhoto.ID {
		t.Fatalf("duplicate CreateVoteOrder() = %#v, want existing order", createdAgain)
	}
}

func TestVotesProgressPersistsAcrossRepositoryInstances(t *testing.T) {
	t.Parallel()

	database := openRepositoryTestDB(t)
	defer database.Close()
	ctx := context.Background()
	challengeID := createRepositoryChallenge(t, database)
	if _, err := NewUsers(database).Upsert(ctx, User{ID: 20, FirstName: "Voter"}); err != nil {
		t.Fatalf("upsert voter: %v", err)
	}
	votes := NewVotes(database)
	firstUpdatedAt := testTime(time.Hour)

	progress, err := votes.UpsertProgress(ctx, VoteProgress{
		ChallengeID:     challengeID,
		VoterUserID:     20,
		CurrentPosition: 1,
		UpdatedAt:       firstUpdatedAt,
	})
	if err != nil {
		t.Fatalf("UpsertProgress() error = %v", err)
	}
	if progress.CurrentPosition != 1 {
		t.Fatalf("CurrentPosition = %d, want 1", progress.CurrentPosition)
	}

	restartedVotes := NewVotes(database)
	persisted, err := restartedVotes.GetProgress(ctx, challengeID, 20)
	if err != nil {
		t.Fatalf("GetProgress() after restart error = %v", err)
	}
	if persisted == nil || persisted.CurrentPosition != 1 {
		t.Fatalf("persisted progress = %#v, want position 1", persisted)
	}

	updated, err := restartedVotes.UpsertProgress(ctx, VoteProgress{
		ChallengeID:     challengeID,
		VoterUserID:     20,
		CurrentPosition: 3,
		UpdatedAt:       testTime(2 * time.Hour),
	})
	if err != nil {
		t.Fatalf("second UpsertProgress() error = %v", err)
	}
	if updated.CurrentPosition != 3 {
		t.Fatalf("updated CurrentPosition = %d, want 3", updated.CurrentPosition)
	}
	if !updated.CreatedAt.Equal(progress.CreatedAt) {
		t.Fatalf("CreatedAt changed from %s to %s", progress.CreatedAt, updated.CreatedAt)
	}
}

func TestVotesManualVoteAddRemoveIsIdempotent(t *testing.T) {
	t.Parallel()

	database := openRepositoryTestDB(t)
	defer database.Close()
	ctx := context.Background()
	challengeID := createRepositoryChallenge(t, database)
	if _, err := NewUsers(database).Upsert(ctx, User{ID: 20, FirstName: "Voter"}); err != nil {
		t.Fatalf("upsert voter: %v", err)
	}
	photo := createRepositoryPhoto(t, NewPhotos(database), challengeID, 11, "file-1")
	votes := NewVotes(database)

	first, err := votes.AddManualVote(ctx, challengeID, 20, photo.ID, testTime(0))
	if err != nil {
		t.Fatalf("AddManualVote() error = %v", err)
	}
	second, err := votes.AddManualVote(ctx, challengeID, 20, photo.ID, testTime(time.Hour))
	if err != nil {
		t.Fatalf("second AddManualVote() error = %v", err)
	}
	if first.CreatedAt != second.CreatedAt {
		t.Fatalf("second AddManualVote() changed CreatedAt from %s to %s", first.CreatedAt, second.CreatedAt)
	}

	listed, err := votes.ListVotes(ctx, challengeID)
	if err != nil {
		t.Fatalf("ListVotes() error = %v", err)
	}
	if len(listed) != 1 {
		t.Fatalf("ListVotes() length = %d, want 1", len(listed))
	}
	if listed[0].Kind != VoteKindManual {
		t.Fatalf("vote kind = %q, want manual", listed[0].Kind)
	}

	removed, err := votes.RemoveManualVote(ctx, challengeID, 20, photo.ID)
	if err != nil {
		t.Fatalf("RemoveManualVote() error = %v", err)
	}
	if !removed {
		t.Fatal("RemoveManualVote() removed = false, want true")
	}
	removed, err = votes.RemoveManualVote(ctx, challengeID, 20, photo.ID)
	if err != nil {
		t.Fatalf("second RemoveManualVote() error = %v", err)
	}
	if removed {
		t.Fatal("second RemoveManualVote() removed = true, want false")
	}

	listed, err = votes.ListVotes(ctx, challengeID)
	if err != nil {
		t.Fatalf("ListVotes() after remove error = %v", err)
	}
	if len(listed) != 0 {
		t.Fatalf("ListVotes() after remove length = %d, want 0", len(listed))
	}
}

func TestVotesRejectsMixedKindDuplicate(t *testing.T) {
	t.Parallel()

	database := openRepositoryTestDB(t)
	defer database.Close()
	ctx := context.Background()
	challengeID := createRepositoryChallenge(t, database)
	if _, err := NewUsers(database).Upsert(ctx, User{ID: 20, FirstName: "Voter"}); err != nil {
		t.Fatalf("upsert voter: %v", err)
	}
	photo := createRepositoryPhoto(t, NewPhotos(database), challengeID, 11, "file-1")
	votes := NewVotes(database)

	if _, err := votes.AddManualVote(ctx, challengeID, 20, photo.ID, testTime(0)); err != nil {
		t.Fatalf("AddManualVote() error = %v", err)
	}
	if _, err := votes.AddSelfVote(ctx, challengeID, 20, photo.ID, testTime(time.Hour)); err == nil {
		t.Fatal("AddSelfVote() after manual vote succeeded, want mixed-kind conflict error")
	}
}

func TestVotesRejectCrossChallengePhoto(t *testing.T) {
	t.Parallel()

	database := openRepositoryTestDB(t)
	defer database.Close()
	ctx := context.Background()
	firstChallengeID := createRepositoryChallenge(t, database)
	if _, err := database.Exec(`
		UPDATE challenges
		SET state = 'finished'
		WHERE id = ?
	`, firstChallengeID); err != nil {
		t.Fatalf("finish first challenge: %v", err)
	}
	secondChallenge, err := NewChallenges(database).Create(ctx, CreateChallengeInput{
		MainChatID:      -1001,
		Num:             2,
		Theme:           "Day",
		Hashtag:         "#day",
		AcceptStartAt:   testTime(24 * time.Hour),
		AcceptUntilAt:   testTime(18 * 24 * time.Hour),
		ReminderAt:      testTime(18*24*time.Hour - 30*time.Hour),
		CreatedByUserID: 10,
		CreatedAt:       testTime(24 * time.Hour),
	})
	if err != nil {
		t.Fatalf("create second challenge: %v", err)
	}
	if _, err := NewUsers(database).Upsert(ctx, User{ID: 20, FirstName: "Voter"}); err != nil {
		t.Fatalf("upsert voter: %v", err)
	}
	photo := createRepositoryPhoto(t, NewPhotos(database), firstChallengeID, 11, "file-1")
	votes := NewVotes(database)

	if _, err := votes.CreateVoteOrder(ctx, secondChallenge.ID, 20, []int64{photo.ID}); err == nil {
		t.Fatal("CreateVoteOrder() with cross-challenge photo succeeded, want error")
	}
	if _, err := votes.AddManualVote(ctx, secondChallenge.ID, 20, photo.ID, testTime(0)); err == nil {
		t.Fatal("AddManualVote() with cross-challenge photo succeeded, want error")
	}
}
