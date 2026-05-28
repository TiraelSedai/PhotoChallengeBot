package repository

import (
	"context"
	"testing"
	"time"
)

func TestTopicSuggestionsCreateDeduplicatesSourceMessage(t *testing.T) {
	t.Parallel()

	database := openRepositoryTestDB(t)
	defer database.Close()
	ctx := context.Background()
	challengeID := createRepositoryChallenge(t, database)
	topics := NewTopicSuggestions(database)

	first, created, err := topics.Create(ctx, CreateTopicSuggestionInput{
		ChallengeID:      challengeID,
		AuthorUserID:     11,
		SourceChatID:     -1001,
		SourceMessageID:  777,
		Text:             "Давайте снимем туман #тема",
		SuggestedAt:      testTime(time.Hour),
		CreatedUpdatedAt: testTime(time.Hour),
	})
	if err != nil {
		t.Fatalf("first Create() error = %v", err)
	}
	if !created {
		t.Fatal("first Create() created = false, want true")
	}

	second, created, err := topics.Create(ctx, CreateTopicSuggestionInput{
		ChallengeID:      challengeID,
		AuthorUserID:     11,
		SourceChatID:     -1001,
		SourceMessageID:  777,
		Text:             "Дубликат не должен перезаписать текст #тема",
		SuggestedAt:      testTime(2 * time.Hour),
		CreatedUpdatedAt: testTime(2 * time.Hour),
	})
	if err != nil {
		t.Fatalf("second Create() error = %v", err)
	}
	if created {
		t.Fatal("second Create() created = true, want false")
	}
	if second.ID != first.ID {
		t.Fatalf("second ID = %d, want existing row %d", second.ID, first.ID)
	}
	if second.Text != first.Text {
		t.Fatalf("second Text = %q, want original %q", second.Text, first.Text)
	}
}

func TestTopicSuggestionsListByChallengeOrdersBySuggestedAt(t *testing.T) {
	t.Parallel()

	database := openRepositoryTestDB(t)
	defer database.Close()
	ctx := context.Background()
	challengeID := createRepositoryChallenge(t, database)
	topics := NewTopicSuggestions(database)

	for _, input := range []CreateTopicSuggestionInput{
		{
			ChallengeID:      challengeID,
			AuthorUserID:     11,
			SourceChatID:     -1001,
			SourceMessageID:  2,
			Text:             "Позже #тема",
			SuggestedAt:      testTime(2 * time.Hour),
			CreatedUpdatedAt: testTime(2 * time.Hour),
		},
		{
			ChallengeID:      challengeID,
			AuthorUserID:     11,
			SourceChatID:     -1001,
			SourceMessageID:  1,
			Text:             "Раньше #тема",
			SuggestedAt:      testTime(time.Hour),
			CreatedUpdatedAt: testTime(time.Hour),
		},
	} {
		if _, _, err := topics.Create(ctx, input); err != nil {
			t.Fatalf("Create(%d) error = %v", input.SourceMessageID, err)
		}
	}

	suggestions, err := topics.ListByChallenge(ctx, challengeID)
	if err != nil {
		t.Fatalf("ListByChallenge() error = %v", err)
	}
	if len(suggestions) != 2 {
		t.Fatalf("suggestions length = %d, want 2", len(suggestions))
	}
	if suggestions[0].Text != "Раньше #тема" || suggestions[1].Text != "Позже #тема" {
		t.Fatalf("suggestions order = %#v, want suggested_at order", suggestions)
	}
}

func TestChallengesClaimAndMarkTopicReportSent(t *testing.T) {
	t.Parallel()

	database := openRepositoryTestDB(t)
	defer database.Close()
	ctx := context.Background()
	challengeID := createRepositoryChallenge(t, database)
	challenges := NewChallenges(database)
	finishedAt := testTime(50 * time.Hour)

	if _, err := database.Exec(`
		UPDATE challenges
		SET state = ?, finished_at = ?, updated_at = ?
		WHERE id = ?
	`, ChallengeStateFinished, timeString(finishedAt), timeString(finishedAt), challengeID); err != nil {
		t.Fatalf("finish challenge: %v", err)
	}

	due, err := challenges.ListUnsentTopicReports(ctx, -1001, 100)
	if err != nil {
		t.Fatalf("ListUnsentTopicReports() error = %v", err)
	}
	if len(due) != 1 || due[0].ID != challengeID {
		t.Fatalf("due = %#v, want challenge %d", due, challengeID)
	}

	claimedAt := finishedAt.Add(time.Minute)
	claimed, err := challenges.ClaimTopicReport(ctx, challengeID, claimedAt)
	if err != nil {
		t.Fatalf("ClaimTopicReport() error = %v", err)
	}
	if !claimed {
		t.Fatal("ClaimTopicReport() = false, want true")
	}

	claimedAgain, err := challenges.ClaimTopicReport(ctx, challengeID, claimedAt.Add(time.Minute))
	if err != nil {
		t.Fatalf("second ClaimTopicReport() error = %v", err)
	}
	if claimedAgain {
		t.Fatal("second ClaimTopicReport() = true, want false while claim is fresh")
	}

	sent, err := challenges.MarkTopicReportSent(ctx, challengeID, claimedAt, claimedAt.Add(2*time.Minute))
	if err != nil {
		t.Fatalf("MarkTopicReportSent() error = %v", err)
	}
	if !sent {
		t.Fatal("MarkTopicReportSent() = false, want true")
	}

	due, err = challenges.ListUnsentTopicReports(ctx, -1001, 100)
	if err != nil {
		t.Fatalf("second ListUnsentTopicReports() error = %v", err)
	}
	if len(due) != 0 {
		t.Fatalf("due after sent = %#v, want none", due)
	}
}
