package results

import (
	"context"
	"errors"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/TiraelSedai/PhotoChallengeBot/internal/db"
	"github.com/TiraelSedai/PhotoChallengeBot/internal/repository"
	"github.com/TiraelSedai/PhotoChallengeBot/internal/templates"
	"github.com/jmoiron/sqlx"
)

func TestPublisherPublishesPinsResultsAndSendsAchievement(t *testing.T) {
	database := openResultsTestDB(t)
	defer database.Close()
	challengeID := createFinishedChallenge(t, database)
	photoID := createResultPhoto(t, database, challengeID, 11, "file-1")
	otherPhotoID := createResultPhoto(t, database, challengeID, 12, "file-2")
	votes := repository.NewVotes(database)
	if _, err := votes.AddSelfVote(context.Background(), challengeID, 11, photoID, resultTestTime(time.Hour)); err != nil {
		t.Fatalf("add self vote: %v", err)
	}
	if _, err := votes.AddSelfVote(context.Background(), challengeID, 12, otherPhotoID, resultTestTime(time.Hour)); err != nil {
		t.Fatalf("add other self vote: %v", err)
	}
	if _, err := votes.AddManualVote(context.Background(), challengeID, 20, photoID, resultTestTime(2*time.Hour)); err != nil {
		t.Fatalf("add manual vote: %v", err)
	}

	publisher := &recordingResultsPublisher{}
	service := newResultsPublisher(t, database, publisher)
	if err := service.PublishDue(context.Background(), -1001, 10); err != nil {
		t.Fatalf("PublishDue() error = %v", err)
	}

	if len(publisher.markdown) != 1 {
		t.Fatalf("markdown messages = %d, want 1", len(publisher.markdown))
	}
	if !strings.Contains(publisher.markdown[0].text, "Итоги челленджа Night") {
		t.Fatalf("results text = %q, want challenge results", publisher.markdown[0].text)
	}
	if len(publisher.pins) != 1 || publisher.pins[0].messageID != 1 {
		t.Fatalf("pins = %#v, want results message pin", publisher.pins)
	}
	if len(publisher.texts) != 1 || !strings.Contains(publisher.texts[0].text, "1-й раз") {
		t.Fatalf("achievement texts = %#v, want first win achievement", publisher.texts)
	}

	stored, err := repository.NewChallenges(database).Get(context.Background(), challengeID)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if stored.ResultsMessageID == nil || *stored.ResultsMessageID != 1 || stored.ResultsPinnedAt == nil {
		t.Fatalf("stored results state = %#v, want message and pin", stored)
	}
	if stored.AchievementsSentAt == nil {
		t.Fatalf("AchievementsSentAt = nil, want marked")
	}
}

func TestPublisherRetriesExistingResultsPinWithoutDuplicateMessage(t *testing.T) {
	database := openResultsTestDB(t)
	defer database.Close()
	challengeID := createFinishedChallenge(t, database)
	createResultPhoto(t, database, challengeID, 11, "file-1")
	if _, err := database.Exec(`
		UPDATE challenges
		SET results_message_id = 42
		WHERE id = ?
	`, challengeID); err != nil {
		t.Fatalf("seed results message id: %v", err)
	}

	publisher := &recordingResultsPublisher{}
	service := newResultsPublisher(t, database, publisher)
	if err := service.PublishDue(context.Background(), -1001, 10); err != nil {
		t.Fatalf("PublishDue() error = %v", err)
	}

	if len(publisher.markdown) != 0 {
		t.Fatalf("markdown messages = %d, want no duplicate", len(publisher.markdown))
	}
	if len(publisher.pins) != 1 || publisher.pins[0].messageID != 42 {
		t.Fatalf("pins = %#v, want existing message pin", publisher.pins)
	}
}

func TestPublisherPersistsResultsMessageAfterSendCancelsParentContext(t *testing.T) {
	database := openResultsTestDB(t)
	defer database.Close()
	challengeID := createFinishedChallenge(t, database)
	createResultPhoto(t, database, challengeID, 11, "file-1")
	ctx, cancel := context.WithCancel(context.Background())
	publisher := &recordingResultsPublisher{
		afterMarkdown:    cancel,
		pinChecksContext: true,
	}
	service := newResultsPublisher(t, database, publisher)

	if err := service.PublishDue(ctx, -1001, 10); !errors.Is(err, context.Canceled) {
		t.Fatalf("PublishDue() error = %v, want canceled pin", err)
	}
	stored, err := repository.NewChallenges(database).Get(context.Background(), challengeID)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if stored.ResultsMessageID == nil || *stored.ResultsMessageID != 1 {
		t.Fatalf("ResultsMessageID = %v, want persisted sent message", stored.ResultsMessageID)
	}
	if stored.ResultsPinnedAt != nil {
		t.Fatalf("ResultsPinnedAt = %v, want unpinned after canceled pin", stored.ResultsPinnedAt)
	}

	retryPublisher := &recordingResultsPublisher{}
	retryService := newResultsPublisher(t, database, retryPublisher)
	if err := retryService.PublishDue(context.Background(), -1001, 10); err != nil {
		t.Fatalf("retry PublishDue() error = %v", err)
	}
	if len(retryPublisher.markdown) != 0 {
		t.Fatalf("retry markdown messages = %#v, want no duplicate results message", retryPublisher.markdown)
	}
	if len(retryPublisher.pins) != 1 || retryPublisher.pins[0].messageID != 1 {
		t.Fatalf("retry pins = %#v, want pin persisted message", retryPublisher.pins)
	}
}

func TestPublisherRecordsResultsMessageIDAfterClaimedPersistFailure(t *testing.T) {
	database := openResultsTestDB(t)
	defer database.Close()
	challengeID := createFinishedChallenge(t, database)
	createResultPhoto(t, database, challengeID, 11, "file-1")
	publisher := &recordingResultsPublisher{}
	renderer, err := templates.Load(filepath.Join("..", "..", "templates"))
	if err != nil {
		t.Fatalf("load templates: %v", err)
	}
	service := NewPublisher(PublishConfig{
		Challenges: &failingResultsChallenges{
			Challenges:              repository.NewChallenges(database),
			failSetResultsMessageID: true,
		},
		Photos:    repository.NewPhotos(database),
		Votes:     repository.NewVotes(database),
		Users:     repository.NewUsers(database),
		Renderer:  renderer,
		Publisher: publisher,
		Now:       func() time.Time { return resultTestTime(4 * time.Hour) },
	})

	if err := service.PublishDue(context.Background(), -1001, 10); err != nil {
		t.Fatalf("PublishDue() error = %v", err)
	}
	if len(publisher.markdown) != 1 {
		t.Fatalf("markdown messages = %#v, want one results send", publisher.markdown)
	}
	if len(publisher.pins) != 1 || publisher.pins[0].messageID != 1 {
		t.Fatalf("pins = %#v, want sent message pinned", publisher.pins)
	}
	stored, err := repository.NewChallenges(database).Get(context.Background(), challengeID)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if stored.ResultsMessageID == nil || *stored.ResultsMessageID != 1 {
		t.Fatalf("ResultsMessageID = %v, want fallback-recorded id", stored.ResultsMessageID)
	}
}

func TestPublisherSendsBackloggedAchievementMilestonesInChallengeOrder(t *testing.T) {
	database := openResultsTestDB(t)
	defer database.Close()
	publisher := &recordingResultsPublisher{}
	service := newResultsPublisher(t, database, publisher)

	for idx := 1; idx <= 3; idx++ {
		challengeID := createFinishedChallengeWithNum(t, database, idx)
		photoID := createResultPhoto(t, database, challengeID, 11, "winner-file-"+strconv.Itoa(idx))
		if _, err := repository.NewVotes(database).AddSelfVote(context.Background(), challengeID, 11, photoID, resultTestTime(time.Duration(idx)*time.Hour)); err != nil {
			t.Fatalf("add self vote %d: %v", idx, err)
		}
		if _, err := repository.NewVotes(database).AddManualVote(context.Background(), challengeID, 20, photoID, resultTestTime(time.Duration(idx)*time.Hour)); err != nil {
			t.Fatalf("add manual vote %d: %v", idx, err)
		}
		if _, err := database.Exec(`
			UPDATE challenges
			SET results_message_id = ?, results_pinned_at = ?
			WHERE id = ?
		`, 100+idx, resultTestTime(time.Duration(idx)*time.Hour).Format(time.RFC3339Nano), challengeID); err != nil {
			t.Fatalf("mark results pinned %d: %v", idx, err)
		}
	}

	if err := service.PublishDueAchievements(context.Background(), -1001, 10); err != nil {
		t.Fatalf("PublishDueAchievements() error = %v", err)
	}

	if len(publisher.texts) != 2 {
		t.Fatalf("achievement messages = %#v, want 2", publisher.texts)
	}
	if !strings.Contains(publisher.texts[0].text, "1-й раз") || !strings.Contains(publisher.texts[1].text, "3-й раз") {
		t.Fatalf("achievement messages = %#v, want 1st and 3rd milestones", publisher.texts)
	}
}

func TestPublisherSendsTiedAchievementWinnersInSingleClaimedMessage(t *testing.T) {
	database := openResultsTestDB(t)
	defer database.Close()
	publisher := &recordingResultsPublisher{}
	service := newResultsPublisher(t, database, publisher)

	challengeID := createFinishedChallenge(t, database)
	firstPhotoID := createResultPhoto(t, database, challengeID, 11, "winner-file-1")
	secondPhotoID := createResultPhoto(t, database, challengeID, 12, "winner-file-2")
	votes := repository.NewVotes(database)
	if _, err := votes.AddManualVote(context.Background(), challengeID, 20, firstPhotoID, resultTestTime(time.Hour)); err != nil {
		t.Fatalf("add first manual vote: %v", err)
	}
	if _, err := votes.AddManualVote(context.Background(), challengeID, 20, secondPhotoID, resultTestTime(time.Hour)); err != nil {
		t.Fatalf("add second manual vote: %v", err)
	}
	if _, err := database.Exec(`
		UPDATE challenges
		SET results_message_id = 101, results_pinned_at = ?
		WHERE id = ?
	`, resultTestTime(time.Hour).Format(time.RFC3339Nano), challengeID); err != nil {
		t.Fatalf("mark results pinned: %v", err)
	}

	if err := service.PublishDueAchievements(context.Background(), -1001, 10); err != nil {
		t.Fatalf("PublishDueAchievements() error = %v", err)
	}
	if len(publisher.texts) != 1 {
		t.Fatalf("achievement texts = %#v, want one combined message", publisher.texts)
	}
	if !strings.Contains(publisher.texts[0].text, "@winner") || !strings.Contains(publisher.texts[0].text, "@other") {
		t.Fatalf("achievement text = %q, want both tied winners", publisher.texts[0].text)
	}
}

func TestPublisherSkipsAchievementsWithActiveClaim(t *testing.T) {
	database := openResultsTestDB(t)
	defer database.Close()
	publisher := &recordingResultsPublisher{}
	service := newResultsPublisher(t, database, publisher)

	challengeID := createFinishedChallenge(t, database)
	photoID := createResultPhoto(t, database, challengeID, 11, "winner-file")
	if _, err := repository.NewVotes(database).AddManualVote(context.Background(), challengeID, 20, photoID, resultTestTime(time.Hour)); err != nil {
		t.Fatalf("add manual vote: %v", err)
	}
	if _, err := database.Exec(`
		UPDATE challenges
		SET results_message_id = 101,
			results_pinned_at = ?,
			achievements_sending_at = ?
		WHERE id = ?
	`, resultTestTime(time.Hour).Format(time.RFC3339Nano), resultTestTime(4*time.Hour).Format(time.RFC3339Nano), challengeID); err != nil {
		t.Fatalf("seed active achievement claim: %v", err)
	}

	if err := service.PublishDueAchievements(context.Background(), -1001, 10); err != nil {
		t.Fatalf("PublishDueAchievements() error = %v", err)
	}
	if len(publisher.texts) != 0 {
		t.Fatalf("achievement texts = %#v, want none while claim is active", publisher.texts)
	}
}

func TestPublisherMarksAchievementsSentAfterSendCancelsParentContext(t *testing.T) {
	database := openResultsTestDB(t)
	defer database.Close()
	challengeID := createFinishedChallenge(t, database)
	photoID := createResultPhoto(t, database, challengeID, 11, "winner-file")
	if _, err := repository.NewVotes(database).AddManualVote(context.Background(), challengeID, 20, photoID, resultTestTime(time.Hour)); err != nil {
		t.Fatalf("add manual vote: %v", err)
	}
	if _, err := database.Exec(`
		UPDATE challenges
		SET results_message_id = 101, results_pinned_at = ?
		WHERE id = ?
	`, resultTestTime(time.Hour).Format(time.RFC3339Nano), challengeID); err != nil {
		t.Fatalf("mark results pinned: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	publisher := &recordingResultsPublisher{afterText: cancel}
	service := newResultsPublisher(t, database, publisher)

	if err := service.PublishDueAchievements(ctx, -1001, 10); err != nil {
		t.Fatalf("PublishDueAchievements() error = %v", err)
	}
	stored, err := repository.NewChallenges(database).Get(context.Background(), challengeID)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if stored.AchievementsSentAt == nil {
		t.Fatal("AchievementsSentAt = nil, want persisted after send canceled parent context")
	}
}

func TestPublisherRecordsAchievementMessageAfterClaimedPersistFailure(t *testing.T) {
	database := openResultsTestDB(t)
	defer database.Close()
	challengeID := createFinishedChallenge(t, database)
	photoID := createResultPhoto(t, database, challengeID, 11, "winner-file")
	if _, err := repository.NewVotes(database).AddManualVote(context.Background(), challengeID, 20, photoID, resultTestTime(time.Hour)); err != nil {
		t.Fatalf("add manual vote: %v", err)
	}
	if _, err := database.Exec(`
		UPDATE challenges
		SET results_message_id = 101, results_pinned_at = ?
		WHERE id = ?
	`, resultTestTime(time.Hour).Format(time.RFC3339Nano), challengeID); err != nil {
		t.Fatalf("mark results pinned: %v", err)
	}
	publisher := &recordingResultsPublisher{}
	renderer, err := templates.Load(filepath.Join("..", "..", "templates"))
	if err != nil {
		t.Fatalf("load templates: %v", err)
	}
	service := NewPublisher(PublishConfig{
		Challenges: &failingResultsChallenges{
			Challenges:                   repository.NewChallenges(database),
			failSetAchievementsMessageID: true,
		},
		Photos:    repository.NewPhotos(database),
		Votes:     repository.NewVotes(database),
		Users:     repository.NewUsers(database),
		Renderer:  renderer,
		Publisher: publisher,
		Now:       func() time.Time { return resultTestTime(4 * time.Hour) },
	})

	if err := service.PublishDueAchievements(context.Background(), -1001, 10); err != nil {
		t.Fatalf("PublishDueAchievements() error = %v", err)
	}
	stored, err := repository.NewChallenges(database).Get(context.Background(), challengeID)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if stored.AchievementsMessageID == nil || *stored.AchievementsMessageID != 1 {
		t.Fatalf("AchievementsMessageID = %v, want fallback-recorded id", stored.AchievementsMessageID)
	}
	if stored.AchievementsSentAt == nil {
		t.Fatal("AchievementsSentAt = nil, want marked")
	}
}

func TestPublisherDoesNotDuplicateAchievementAfterMarkSentFailure(t *testing.T) {
	database := openResultsTestDB(t)
	defer database.Close()
	challengeID := createFinishedChallenge(t, database)
	photoID := createResultPhoto(t, database, challengeID, 11, "winner-file")
	if _, err := repository.NewVotes(database).AddManualVote(context.Background(), challengeID, 20, photoID, resultTestTime(time.Hour)); err != nil {
		t.Fatalf("add manual vote: %v", err)
	}
	if _, err := database.Exec(`
		UPDATE challenges
		SET results_message_id = 101, results_pinned_at = ?
		WHERE id = ?
	`, resultTestTime(time.Hour).Format(time.RFC3339Nano), challengeID); err != nil {
		t.Fatalf("mark results pinned: %v", err)
	}
	publisher := &recordingResultsPublisher{}
	renderer, err := templates.Load(filepath.Join("..", "..", "templates"))
	if err != nil {
		t.Fatalf("load templates: %v", err)
	}
	service := NewPublisher(PublishConfig{
		Challenges: &failingResultsChallenges{
			Challenges:               repository.NewChallenges(database),
			failMarkAchievementsSent: true,
		},
		Photos:    repository.NewPhotos(database),
		Votes:     repository.NewVotes(database),
		Users:     repository.NewUsers(database),
		Renderer:  renderer,
		Publisher: publisher,
		Now:       func() time.Time { return resultTestTime(4 * time.Hour) },
	})

	if err := service.PublishDueAchievements(context.Background(), -1001, 10); err == nil {
		t.Fatal("first PublishDueAchievements() error = nil, want mark failure")
	}
	if len(publisher.texts) != 1 {
		t.Fatalf("texts after first attempt = %#v, want one send", publisher.texts)
	}
	if _, err := database.Exec(`
		UPDATE challenges
		SET achievements_sending_at = ?
		WHERE id = ?
	`, resultTestTime(3*time.Hour).Format(time.RFC3339Nano), challengeID); err != nil {
		t.Fatalf("make achievement claim stale: %v", err)
	}

	retryService := newResultsPublisher(t, database, publisher)
	if err := retryService.PublishDueAchievements(context.Background(), -1001, 10); err != nil {
		t.Fatalf("retry PublishDueAchievements() error = %v", err)
	}
	if len(publisher.texts) != 1 {
		t.Fatalf("texts after retry = %#v, want no duplicate send", publisher.texts)
	}
	stored, err := repository.NewChallenges(database).Get(context.Background(), challengeID)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if stored.AchievementsSentAt == nil {
		t.Fatal("AchievementsSentAt = nil, want retry to mark sent")
	}
}

func TestPublisherBlocksAchievementsBehindEarlierUnpublishedResults(t *testing.T) {
	database := openResultsTestDB(t)
	defer database.Close()
	publisher := &recordingResultsPublisher{}
	service := newResultsPublisher(t, database, publisher)

	firstChallengeID := createFinishedChallengeWithNum(t, database, 1)
	firstPhotoID := createResultPhoto(t, database, firstChallengeID, 11, "winner-file-1")
	if _, err := repository.NewVotes(database).AddManualVote(context.Background(), firstChallengeID, 20, firstPhotoID, resultTestTime(time.Hour)); err != nil {
		t.Fatalf("add first manual vote: %v", err)
	}

	secondChallengeID := createFinishedChallengeWithNum(t, database, 2)
	secondPhotoID := createResultPhoto(t, database, secondChallengeID, 11, "winner-file-2")
	if _, err := repository.NewVotes(database).AddManualVote(context.Background(), secondChallengeID, 20, secondPhotoID, resultTestTime(2*time.Hour)); err != nil {
		t.Fatalf("add second manual vote: %v", err)
	}
	if _, err := database.Exec(`
		UPDATE challenges
		SET results_message_id = 102, results_pinned_at = ?
		WHERE id = ?
	`, resultTestTime(2*time.Hour).Format(time.RFC3339Nano), secondChallengeID); err != nil {
		t.Fatalf("mark second results pinned: %v", err)
	}

	if err := service.PublishDueAchievements(context.Background(), -1001, 10); err == nil {
		t.Fatal("PublishDueAchievements() error = nil, want earlier unpublished results error")
	}
	if len(publisher.texts) != 0 {
		t.Fatalf("achievement messages = %#v, want none before earlier results", publisher.texts)
	}
	stored, err := repository.NewChallenges(database).Get(context.Background(), secondChallengeID)
	if err != nil {
		t.Fatalf("Get second challenge error = %v", err)
	}
	if stored.AchievementsSentAt != nil {
		t.Fatalf("second AchievementsSentAt = %v, want nil while earlier results are unpublished", stored.AchievementsSentAt)
	}
}

func TestPublisherStopsAchievementsAfterFirstSendFailure(t *testing.T) {
	database := openResultsTestDB(t)
	defer database.Close()
	sendErr := errors.New("telegram send failed")
	publisher := &recordingResultsPublisher{textErr: sendErr}
	service := newResultsPublisher(t, database, publisher)

	challengeID := createFinishedChallenge(t, database)
	firstPhotoID := createResultPhoto(t, database, challengeID, 11, "winner-file-1")
	secondPhotoID := createResultPhoto(t, database, challengeID, 12, "winner-file-2")
	votes := repository.NewVotes(database)
	if _, err := votes.AddManualVote(context.Background(), challengeID, 20, firstPhotoID, resultTestTime(time.Hour)); err != nil {
		t.Fatalf("add first manual vote: %v", err)
	}
	if _, err := votes.AddManualVote(context.Background(), challengeID, 20, secondPhotoID, resultTestTime(time.Hour)); err != nil {
		t.Fatalf("add second manual vote: %v", err)
	}
	if _, err := database.Exec(`
		UPDATE challenges
		SET results_message_id = 101, results_pinned_at = ?
		WHERE id = ?
	`, resultTestTime(time.Hour).Format(time.RFC3339Nano), challengeID); err != nil {
		t.Fatalf("mark results pinned: %v", err)
	}

	if err := service.PublishDueAchievements(context.Background(), -1001, 10); !errors.Is(err, sendErr) {
		t.Fatalf("PublishDueAchievements() error = %v, want %v", err, sendErr)
	}
	if publisher.textAttempts != 1 {
		t.Fatalf("text attempts = %d, want fail-fast after first send", publisher.textAttempts)
	}
	if len(publisher.texts) != 0 {
		t.Fatalf("sent texts = %#v, want no successful sends", publisher.texts)
	}
	stored, err := repository.NewChallenges(database).Get(context.Background(), challengeID)
	if err != nil {
		t.Fatalf("Get challenge error = %v", err)
	}
	if stored.AchievementsSentAt != nil {
		t.Fatalf("AchievementsSentAt = %v, want nil after send failure", stored.AchievementsSentAt)
	}
}

func openResultsTestDB(t *testing.T) *sqlx.DB {
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

func newResultsPublisher(t *testing.T, database *sqlx.DB, publisher *recordingResultsPublisher) *PublisherService {
	t.Helper()
	renderer, err := templates.Load(filepath.Join("..", "..", "templates"))
	if err != nil {
		t.Fatalf("load templates: %v", err)
	}
	return NewPublisher(PublishConfig{
		Challenges: repository.NewChallenges(database),
		Photos:     repository.NewPhotos(database),
		Votes:      repository.NewVotes(database),
		Users:      repository.NewUsers(database),
		Renderer:   renderer,
		Publisher:  publisher,
		Now:        func() time.Time { return resultTestTime(4 * time.Hour) },
	})
}

func createFinishedChallenge(t *testing.T, database *sqlx.DB) int64 {
	return createFinishedChallengeWithNum(t, database, 1)
}

func createFinishedChallengeWithNum(t *testing.T, database *sqlx.DB, num int) int64 {
	t.Helper()
	ctx := context.Background()
	users := repository.NewUsers(database)
	for _, user := range []repository.User{
		{ID: 10, FirstName: "Admin"},
		{ID: 11, Username: "winner", FirstName: "Win", LastName: "Ner"},
		{ID: 12, Username: "other", FirstName: "Other"},
		{ID: 20, Username: "voter", FirstName: "Voter"},
	} {
		if _, err := users.Upsert(ctx, user); err != nil {
			t.Fatalf("upsert user %d: %v", user.ID, err)
		}
	}
	challenge, err := repository.NewChallenges(database).Create(ctx, repository.CreateChallengeInput{
		MainChatID:      -1001,
		Num:             num,
		Theme:           "Night",
		Hashtag:         "#night",
		State:           repository.ChallengeStateFinished,
		AcceptStartAt:   resultTestTime(-72 * time.Hour),
		AcceptUntilAt:   resultTestTime(-48 * time.Hour),
		ReminderAt:      resultTestTime(-60 * time.Hour),
		CreatedByUserID: 10,
		CreatedAt:       resultTestTime(time.Duration(num)*time.Hour - 72*time.Hour),
	})
	if err != nil {
		t.Fatalf("create finished challenge: %v", err)
	}
	if _, err := database.Exec(`
		UPDATE challenges
		SET finished_at = ?
		WHERE id = ?
	`, resultTestTime(time.Duration(num)*time.Hour).Format(time.RFC3339Nano), challenge.ID); err != nil {
		t.Fatalf("set finished_at: %v", err)
	}
	return challenge.ID
}

func createResultPhoto(t *testing.T, database *sqlx.DB, challengeID, authorID int64, fileID string) int64 {
	t.Helper()
	photo, _, err := repository.NewPhotos(database).UpsertCurrent(context.Background(), repository.UpsertPhotoInput{
		ChallengeID:     challengeID,
		AuthorUserID:    authorID,
		FileID:          fileID,
		FileUniqueID:    fileID + "-unique",
		SourceChatID:    -1001,
		SourceMessageID: 100,
		Caption:         "#night",
		SubmittedAt:     resultTestTime(-time.Hour),
	})
	if err != nil {
		t.Fatalf("upsert photo: %v", err)
	}
	return photo.ID
}

type recordingResultsPublisher struct {
	markdown         []messageCall
	texts            []messageCall
	pins             []pinCall
	textAttempts     int
	textErr          error
	afterMarkdown    func()
	afterText        func()
	pinChecksContext bool
}

type messageCall struct {
	chatID int64
	text   string
}

type pinCall struct {
	chatID    int64
	messageID int
}

func (p *recordingResultsPublisher) SendMarkdown(_ context.Context, chatID int64, text string) (int, error) {
	p.markdown = append(p.markdown, messageCall{chatID: chatID, text: text})
	if p.afterMarkdown != nil {
		p.afterMarkdown()
	}
	return len(p.markdown), nil
}

func (p *recordingResultsPublisher) SendText(_ context.Context, chatID int64, text string) (int, error) {
	p.textAttempts++
	if p.textErr != nil {
		return 0, p.textErr
	}
	p.texts = append(p.texts, messageCall{chatID: chatID, text: text})
	if p.afterText != nil {
		p.afterText()
	}
	return len(p.texts), nil
}

func (p *recordingResultsPublisher) Pin(ctx context.Context, chatID int64, messageID int) error {
	if p.pinChecksContext && ctx.Err() != nil {
		return ctx.Err()
	}
	p.pins = append(p.pins, pinCall{chatID: chatID, messageID: messageID})
	return nil
}

func resultTestTime(offset time.Duration) time.Time {
	return time.Date(2026, 5, 18, 12, 0, 0, 0, time.UTC).Add(offset)
}

type failingResultsChallenges struct {
	*repository.Challenges
	failSetResultsMessageID      bool
	failSetAchievementsMessageID bool
	failMarkAchievementsSent     bool
}

func (c *failingResultsChallenges) SetResultsMessageID(
	ctx context.Context,
	id int64,
	messageID int,
	claimedAt time.Time,
	updatedAt time.Time,
) (bool, error) {
	if c.failSetResultsMessageID {
		c.failSetResultsMessageID = false
		return false, errors.New("set results message id failed")
	}
	return c.Challenges.SetResultsMessageID(ctx, id, messageID, claimedAt, updatedAt)
}

func (c *failingResultsChallenges) SetAchievementsMessageID(
	ctx context.Context,
	id int64,
	messageID int,
	claimedAt time.Time,
	updatedAt time.Time,
) (bool, error) {
	if c.failSetAchievementsMessageID {
		c.failSetAchievementsMessageID = false
		return false, errors.New("set achievements message id failed")
	}
	return c.Challenges.SetAchievementsMessageID(ctx, id, messageID, claimedAt, updatedAt)
}

func (c *failingResultsChallenges) MarkAchievementsSent(
	ctx context.Context,
	id int64,
	claimedAt time.Time,
	sentAt time.Time,
) (bool, error) {
	if c.failMarkAchievementsSent {
		c.failMarkAchievementsSent = false
		return false, errors.New("mark achievements sent failed")
	}
	return c.Challenges.MarkAchievementsSent(ctx, id, claimedAt, sentAt)
}
