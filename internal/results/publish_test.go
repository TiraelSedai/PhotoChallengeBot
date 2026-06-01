package results

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

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

	publisher := newResultsPublisherDeps()
	service := newResultsPublisher(t, database, publisher)
	if err := service.PublishDue(context.Background(), -1001, 10); err != nil {
		t.Fatalf("PublishDue() error = %v", err)
	}

	if len(publisher.markdown) != 0 {
		t.Fatalf("markdown messages = %d, want none when winner photo is sent", len(publisher.markdown))
	}
	if len(publisher.photos) != 1 {
		t.Fatalf("photo messages = %#v, want winner photo", publisher.photos)
	}
	if publisher.photos[0].fileID != "file-1" {
		t.Fatalf("winner photo file id = %q, want file-1", publisher.photos[0].fileID)
	}
	if !strings.Contains(publisher.photos[0].caption, "Итоги челленджа Night") {
		t.Fatalf("results caption = %q, want challenge results", publisher.photos[0].caption)
	}
	if len(publisher.photoGroups) != 1 || len(publisher.photoGroups[0].fileIDs) != 2 {
		t.Fatalf("ranking groups = %#v, want one group with two photos", publisher.photoGroups)
	}
	if publisher.photoGroups[0].fileIDs[0] != "file-1" || publisher.photoGroups[0].fileIDs[1] != "file-2" {
		t.Fatalf("ranking file ids = %#v, want winner repeated first", publisher.photoGroups[0].fileIDs)
	}
	if !strings.Contains(publisher.photoGroups[0].captions[0], "1. @winner, Win Ner") ||
		!strings.Contains(publisher.photoGroups[0].captions[0], "Лайков: 2") {
		t.Fatalf("first ranking caption = %q, want winner place and likes", publisher.photoGroups[0].captions[0])
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

func TestPublisherSendsRankingPhotosInBatchesOfTen(t *testing.T) {
	database := openResultsTestDB(t)
	defer database.Close()
	challengeID := createFinishedChallenge(t, database)
	votes := repository.NewVotes(database)
	for idx := 1; idx <= 23; idx++ {
		authorID := int64(100 + idx)
		createResultUser(t, database, repository.User{
			ID:        authorID,
			Username:  fmt.Sprintf("user%02d", idx),
			FirstName: fmt.Sprintf("User %02d", idx),
		})
		photoID := createResultPhoto(t, database, challengeID, authorID, fmt.Sprintf("file-%02d", idx))
		if _, err := votes.AddSelfVote(context.Background(), challengeID, authorID, photoID, resultTestTime(time.Hour)); err != nil {
			t.Fatalf("add self vote %d: %v", idx, err)
		}
		if idx == 1 {
			if _, err := votes.AddManualVote(context.Background(), challengeID, 20, photoID, resultTestTime(2*time.Hour)); err != nil {
				t.Fatalf("add manual vote %d: %v", idx, err)
			}
		}
	}

	publisher := newResultsPublisherDeps()
	service := newResultsPublisher(t, database, publisher)
	if err := service.PublishDue(context.Background(), -1001, 10); err != nil {
		t.Fatalf("PublishDue() error = %v", err)
	}

	if len(publisher.photos) != 1 || publisher.photos[0].fileID != "file-01" {
		t.Fatalf("winner photo messages = %#v, want first photo as winner", publisher.photos)
	}
	if strings.Contains(publisher.photos[0].caption, "Все работы") {
		t.Fatalf("winner caption includes full ranking block: %q", publisher.photos[0].caption)
	}
	if len(publisher.photos[0].caption) > 1024 {
		t.Fatalf("winner caption length = %d, want Telegram-safe caption", len(publisher.photos[0].caption))
	}
	if len(publisher.photoGroups) != 3 {
		t.Fatalf("photo groups = %d, want 3", len(publisher.photoGroups))
	}
	for idx, want := range []int{10, 10, 3} {
		if len(publisher.photoGroups[idx].fileIDs) != want {
			t.Fatalf("group %d photo count = %d, want %d", idx, len(publisher.photoGroups[idx].fileIDs), want)
		}
	}
	if publisher.photoGroups[0].fileIDs[0] != "file-01" {
		t.Fatalf("first ranking photo = %q, want winner repeated as first place", publisher.photoGroups[0].fileIDs[0])
	}
	captionChecks := map[int]string{
		0:  "1. @user01, User 01\nЛайков: 2",
		9:  "10. @user10, User 10\nЛайков: 1",
		10: "11. @user11, User 11\nЛайков: 1",
		19: "20. @user20, User 20\nЛайков: 1",
		20: "21. @user21, User 21\nЛайков: 1",
		22: "23. @user23, User 23\nЛайков: 1",
	}
	for position, want := range captionChecks {
		group := position / 10
		offset := position % 10
		if got := publisher.photoGroups[group].captions[offset]; got != want {
			t.Fatalf("caption %d = %q, want %q", position+1, got, want)
		}
	}
}

func TestPublisherUsesCompactCaptionWhenWinnerSummaryExceedsTelegramLimit(t *testing.T) {
	database := openResultsTestDB(t)
	defer database.Close()
	challengeID := createFinishedChallengeWithTheme(t, database, 1, "Long "+strings.Repeat("theme_", 60))
	votes := repository.NewVotes(database)
	for idx := 1; idx <= 11; idx++ {
		authorID := int64(300 + idx)
		createResultUser(t, database, repository.User{
			ID:        authorID,
			Username:  fmt.Sprintf("winner_%02d", idx),
			FirstName: strings.Repeat(fmt.Sprintf("Winner%02d ", idx), 25),
		})
		photoID := createResultPhoto(t, database, challengeID, authorID, fmt.Sprintf("winner-file-%02d", idx))
		if _, err := votes.AddManualVote(context.Background(), challengeID, 20, photoID, resultTestTime(time.Hour)); err != nil {
			t.Fatalf("add manual vote %d: %v", idx, err)
		}
	}

	publisher := newResultsPublisherDeps()
	service := newResultsPublisher(t, database, publisher)
	if err := service.PublishDue(context.Background(), -1001, 10); err != nil {
		t.Fatalf("PublishDue() error = %v", err)
	}

	if len(publisher.photoGroups) < 1 {
		t.Fatalf("photo groups = %#v, want winner summary group", publisher.photoGroups)
	}
	caption := publisher.photoGroups[0].captions[0]
	if utf8.RuneCountInString(caption) > telegramPhotoCaptionLimit {
		t.Fatalf("summary caption length = %d, want <= %d", utf8.RuneCountInString(caption), telegramPhotoCaptionLimit)
	}
	if !strings.Contains(caption, "Победителей: 11") {
		t.Fatalf("summary caption = %q, want compact winner count", caption)
	}
	if strings.Contains(caption, "winner\\_11") {
		t.Fatalf("summary caption = %q, want compact caption instead of full winner list", caption)
	}
}

func TestPublisherKeepsRankingCaptionWithinTelegramLimit(t *testing.T) {
	database := openResultsTestDB(t)
	defer database.Close()
	challengeID := createFinishedChallenge(t, database)
	authorID := int64(501)
	createResultUser(t, database, repository.User{
		ID:        authorID,
		Username:  strings.Repeat("long_name_", 30),
		FirstName: strings.Repeat("very_long_display_name_", 80),
	})
	photoID := createResultPhoto(t, database, challengeID, authorID, "long-name-file")
	if _, err := repository.NewVotes(database).AddManualVote(context.Background(), challengeID, 20, photoID, resultTestTime(time.Hour)); err != nil {
		t.Fatalf("add manual vote: %v", err)
	}

	publisher := newResultsPublisherDeps()
	service := newResultsPublisher(t, database, publisher)
	if err := service.PublishDue(context.Background(), -1001, 10); err != nil {
		t.Fatalf("PublishDue() error = %v", err)
	}

	if len(publisher.photos) != 2 {
		t.Fatalf("photo messages = %#v, want winner summary and ranking photo", publisher.photos)
	}
	rankingCaption := publisher.photos[1].caption
	if utf8.RuneCountInString(rankingCaption) > telegramPhotoCaptionLimit {
		t.Fatalf("ranking caption length = %d, want <= %d", utf8.RuneCountInString(rankingCaption), telegramPhotoCaptionLimit)
	}
	if !strings.HasPrefix(rankingCaption, "1. @long\\_name\\_") {
		t.Fatalf("ranking caption = %q, want escaped author prefix", rankingCaption)
	}
	if !strings.Contains(rankingCaption, "...\nЛайков: 1") {
		t.Fatalf("ranking caption = %q, want shortened name and likes", rankingCaption)
	}
}

func TestNewPublisherPanicsOnNilClock(t *testing.T) {
	database := openResultsTestDB(t)
	defer database.Close()
	renderer, err := templates.Load(filepath.Join("..", "..", "templates"))
	if err != nil {
		t.Fatalf("load templates: %v", err)
	}
	defer func() {
		if recover() == nil {
			t.Fatal("NewPublisher() did not panic")
		}
	}()
	NewPublisher(PublishConfig{
		Challenges: repository.NewChallenges(database),
		Photos:     repository.NewPhotos(database),
		Votes:      repository.NewVotes(database),
		Users:      repository.NewUsers(database),
		Renderer:   renderer,
		Publisher:  newResultsPublisherDeps().mock,
	})
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

	publisher := newResultsPublisherDeps()
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
	publisher := newResultsPublisherDeps()
	publisher.afterMarkdown = cancel
	publisher.pinChecksContext = true
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

	retryPublisher := newResultsPublisherDeps()
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
	publisher := newResultsPublisherDeps()
	renderer, err := templates.Load(filepath.Join("..", "..", "templates"))
	if err != nil {
		t.Fatalf("load templates: %v", err)
	}
	challenges := newResultsChallengeDeps(repository.NewChallenges(database))
	challenges.failSetResultsMessageID = true
	service := NewPublisher(PublishConfig{
		Challenges: challenges.mock,
		Photos:     repository.NewPhotos(database),
		Votes:      repository.NewVotes(database),
		Users:      repository.NewUsers(database),
		Renderer:   renderer,
		Publisher:  publisher.mock,
		Now:        func() time.Time { return resultTestTime(4 * time.Hour) },
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
	publisher := newResultsPublisherDeps()
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
	publisher := newResultsPublisherDeps()
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
	publisher := newResultsPublisherDeps()
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
	publisher := newResultsPublisherDeps()
	publisher.afterText = cancel
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
	publisher := newResultsPublisherDeps()
	renderer, err := templates.Load(filepath.Join("..", "..", "templates"))
	if err != nil {
		t.Fatalf("load templates: %v", err)
	}
	challenges := newResultsChallengeDeps(repository.NewChallenges(database))
	challenges.failSetAchievementsMessageID = true
	service := NewPublisher(PublishConfig{
		Challenges: challenges.mock,
		Photos:     repository.NewPhotos(database),
		Votes:      repository.NewVotes(database),
		Users:      repository.NewUsers(database),
		Renderer:   renderer,
		Publisher:  publisher.mock,
		Now:        func() time.Time { return resultTestTime(4 * time.Hour) },
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
	publisher := newResultsPublisherDeps()
	renderer, err := templates.Load(filepath.Join("..", "..", "templates"))
	if err != nil {
		t.Fatalf("load templates: %v", err)
	}
	challenges := newResultsChallengeDeps(repository.NewChallenges(database))
	challenges.failMarkAchievementsSent = true
	service := NewPublisher(PublishConfig{
		Challenges: challenges.mock,
		Photos:     repository.NewPhotos(database),
		Votes:      repository.NewVotes(database),
		Users:      repository.NewUsers(database),
		Renderer:   renderer,
		Publisher:  publisher.mock,
		Now:        func() time.Time { return resultTestTime(4 * time.Hour) },
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
	publisher := newResultsPublisherDeps()
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
	publisher := newResultsPublisherDeps()
	publisher.textErr = sendErr
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

func newResultsPublisher(t *testing.T, database *sqlx.DB, publisher *resultsPublisherDeps) *PublisherService {
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
		Publisher:  publisher.mock,
		Now:        func() time.Time { return resultTestTime(4 * time.Hour) },
	})
}

func createFinishedChallenge(t *testing.T, database *sqlx.DB) int64 {
	return createFinishedChallengeWithNum(t, database, 1)
}

func createFinishedChallengeWithNum(t *testing.T, database *sqlx.DB, num int) int64 {
	return createFinishedChallengeWithTheme(t, database, num, "Night")
}

func createFinishedChallengeWithTheme(t *testing.T, database *sqlx.DB, num int, theme string) int64 {
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
		Theme:           theme,
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

func createResultUser(t *testing.T, database *sqlx.DB, user repository.User) {
	t.Helper()
	user.UpdatedAt = resultTestTime(-2 * time.Hour)
	if _, err := repository.NewUsers(database).Upsert(context.Background(), user); err != nil {
		t.Fatalf("upsert user %d: %v", user.ID, err)
	}
}

type resultsPublisherDeps struct {
	mock             *MoqPublisher
	markdown         []messageCall
	photos           []photoMessageCall
	photoGroups      []photoGroupCall
	texts            []messageCall
	pins             []pinCall
	nextMessageID    int
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

type photoMessageCall struct {
	chatID  int64
	fileID  string
	caption string
}

type photoGroupCall struct {
	chatID   int64
	fileIDs  []string
	captions []string
}

type pinCall struct {
	chatID    int64
	messageID int
}

func newResultsPublisherDeps() *resultsPublisherDeps {
	deps := &resultsPublisherDeps{}
	deps.mock = &MoqPublisher{
		SendMarkdownFunc: func(_ context.Context, chatID int64, text string) (int, error) {
			deps.markdown = append(deps.markdown, messageCall{chatID: chatID, text: text})
			if deps.afterMarkdown != nil {
				deps.afterMarkdown()
			}
			return deps.nextID(), nil
		},
		SendMarkdownPhotoFunc: func(_ context.Context, chatID int64, fileID string, caption string) (int, error) {
			deps.photos = append(deps.photos, photoMessageCall{chatID: chatID, fileID: fileID, caption: caption})
			return deps.nextID(), nil
		},
		SendMarkdownPhotoGroupFunc: func(_ context.Context, chatID int64, fileIDs []string, captions []string) (int, error) {
			deps.photoGroups = append(deps.photoGroups, photoGroupCall{
				chatID:   chatID,
				fileIDs:  append([]string(nil), fileIDs...),
				captions: append([]string(nil), captions...),
			})
			return deps.nextID(), nil
		},
		SendTextFunc: func(_ context.Context, chatID int64, text string) (int, error) {
			deps.textAttempts++
			if deps.textErr != nil {
				return 0, deps.textErr
			}
			deps.texts = append(deps.texts, messageCall{chatID: chatID, text: text})
			if deps.afterText != nil {
				deps.afterText()
			}
			return deps.nextID(), nil
		},
		PinFunc: func(ctx context.Context, chatID int64, messageID int) error {
			if deps.pinChecksContext && ctx.Err() != nil {
				return ctx.Err()
			}
			deps.pins = append(deps.pins, pinCall{chatID: chatID, messageID: messageID})
			return nil
		},
	}
	return deps
}

func (d *resultsPublisherDeps) nextID() int {
	d.nextMessageID++
	return d.nextMessageID
}

type resultsChallengeDeps struct {
	mock                         *MoqChallengeStore
	challenges                   *repository.Challenges
	failSetResultsMessageID      bool
	failSetAchievementsMessageID bool
	failMarkAchievementsSent     bool
}

func newResultsChallengeDeps(challenges *repository.Challenges) *resultsChallengeDeps {
	deps := &resultsChallengeDeps{challenges: challenges}
	deps.mock = &MoqChallengeStore{
		GetFunc:                         challenges.Get,
		ListUnpublishedResultsFunc:      challenges.ListUnpublishedResults,
		ClaimResultsFunc:                challenges.ClaimResults,
		RecordResultsMessageIDFunc:      challenges.RecordResultsMessageID,
		MarkResultsPinnedFunc:           challenges.MarkResultsPinned,
		ReleaseResultsClaimFunc:         challenges.ReleaseResultsClaim,
		ListUnsentAchievementsFunc:      challenges.ListUnsentAchievements,
		ClaimAchievementsFunc:           challenges.ClaimAchievements,
		RecordAchievementsMessageIDFunc: challenges.RecordAchievementsMessageID,
		ReleaseAchievementsClaimFunc:    challenges.ReleaseAchievementsClaim,
		SetResultsMessageIDFunc: func(ctx context.Context, id int64, messageID int, claimedAt time.Time, updatedAt time.Time) (bool, error) {
			if deps.failSetResultsMessageID {
				deps.failSetResultsMessageID = false
				return false, errors.New("set results message id failed")
			}
			return deps.challenges.SetResultsMessageID(ctx, id, messageID, claimedAt, updatedAt)
		},
		SetAchievementsMessageIDFunc: func(ctx context.Context, id int64, messageID int, claimedAt time.Time, updatedAt time.Time) (bool, error) {
			if deps.failSetAchievementsMessageID {
				deps.failSetAchievementsMessageID = false
				return false, errors.New("set achievements message id failed")
			}
			return deps.challenges.SetAchievementsMessageID(ctx, id, messageID, claimedAt, updatedAt)
		},
		MarkAchievementsSentFunc: func(ctx context.Context, id int64, claimedAt time.Time, sentAt time.Time) (bool, error) {
			if deps.failMarkAchievementsSent {
				deps.failMarkAchievementsSent = false
				return false, errors.New("mark achievements sent failed")
			}
			return deps.challenges.MarkAchievementsSent(ctx, id, claimedAt, sentAt)
		},
	}
	return deps
}

func resultTestTime(offset time.Duration) time.Time {
	return time.Date(2026, 5, 18, 12, 0, 0, 0, time.UTC).Add(offset)
}
