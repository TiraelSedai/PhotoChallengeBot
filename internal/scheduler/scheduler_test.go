package scheduler

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

func TestTickSendsReminderOnce(t *testing.T) {
	ctx := context.Background()
	database := openSchedulerTestDB(t)
	defer database.Close()

	now := testTime(48 * time.Hour)
	challenge := createSchedulerChallenge(t, database, repository.CreateChallengeInput{
		MainChatID:      -1001,
		Num:             1,
		Theme:           "Night",
		Hashtag:         "#night",
		AcceptStartAt:   testTime(0),
		AcceptUntilAt:   now.Add(time.Hour),
		ReminderAt:      now.Add(-time.Hour),
		CreatedByUserID: 10,
		CreatedAt:       testTime(0),
	})
	publisher := &recordingPublisher{}

	scheduler := newTestScheduler(database, Config{
		Challenges: repository.NewChallenges(database),
		Publisher:  publisher,
		Now:        func() time.Time { return now },
	})

	if err := scheduler.Tick(ctx); err != nil {
		t.Fatalf("Tick() error = %v", err)
	}

	if len(publisher.messages) != 1 {
		t.Fatalf("sent reminders = %d, want 1", len(publisher.messages))
	}
	if publisher.messages[0].chatID != -1001 {
		t.Fatalf("reminder chatID = %d, want -1001", publisher.messages[0].chatID)
	}

	stored, err := repository.NewChallenges(database).Get(ctx, challenge.ID)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if stored.ReminderSentAt == nil {
		t.Fatal("ReminderSentAt is nil, want timestamp")
	}
	if stored.ReminderMessageID == nil || *stored.ReminderMessageID != 1 {
		t.Fatalf("ReminderMessageID = %v, want 1", stored.ReminderMessageID)
	}
	if stored.State != repository.ChallengeStateActive {
		t.Fatalf("State = %q, want active", stored.State)
	}

	if err := scheduler.Tick(ctx); err != nil {
		t.Fatalf("second Tick() error = %v", err)
	}
	if len(publisher.messages) != 1 {
		t.Fatalf("sent reminders after second tick = %d, want 1", len(publisher.messages))
	}
}

func TestTickScopesWorkToConfiguredMainChat(t *testing.T) {
	ctx := context.Background()
	database := openSchedulerTestDB(t)
	defer database.Close()

	now := testTime(48 * time.Hour)
	other := createSchedulerChallenge(t, database, repository.CreateChallengeInput{
		MainChatID:      -2002,
		Num:             1,
		Theme:           "Street",
		Hashtag:         "#street",
		AcceptStartAt:   testTime(0),
		AcceptUntilAt:   now.Add(-time.Minute),
		ReminderAt:      now.Add(-time.Hour),
		CreatedByUserID: 10,
		CreatedAt:       testTime(0),
	})
	owned := createSchedulerChallenge(t, database, repository.CreateChallengeInput{
		MainChatID:      -1001,
		Num:             1,
		Theme:           "Night",
		Hashtag:         "#night",
		AcceptStartAt:   testTime(0),
		AcceptUntilAt:   now.Add(time.Hour),
		ReminderAt:      now.Add(-time.Hour),
		CreatedByUserID: 10,
		CreatedAt:       testTime(0),
	})
	publisher := &recordingPublisher{}
	scheduler := newTestScheduler(database, Config{
		MainChatID: -1001,
		Challenges: repository.NewChallenges(database),
		Publisher:  publisher,
		Now:        func() time.Time { return now },
		BatchSize:  1,
	})

	if err := scheduler.Tick(ctx); err != nil {
		t.Fatalf("Tick() error = %v", err)
	}
	if len(publisher.messages) != 1 {
		t.Fatalf("sent reminders = %d, want 1", len(publisher.messages))
	}
	if publisher.messages[0].chatID != -1001 {
		t.Fatalf("reminder chatID = %d, want -1001", publisher.messages[0].chatID)
	}

	challenges := repository.NewChallenges(database)
	storedOwned, err := challenges.Get(ctx, owned.ID)
	if err != nil {
		t.Fatalf("Get() owned error = %v", err)
	}
	if storedOwned.ReminderSentAt == nil {
		t.Fatal("owned ReminderSentAt is nil, want timestamp")
	}
	storedOther, err := challenges.Get(ctx, other.ID)
	if err != nil {
		t.Fatalf("Get() other error = %v", err)
	}
	if storedOther.State != repository.ChallengeStateActive {
		t.Fatalf("other State = %q, want active", storedOther.State)
	}
	if storedOther.ReminderSentAt != nil {
		t.Fatalf("other ReminderSentAt = %v, want nil", storedOther.ReminderSentAt)
	}
}

func TestTickRetriesReminderAfterSendFailure(t *testing.T) {
	ctx := context.Background()
	database := openSchedulerTestDB(t)
	defer database.Close()

	now := testTime(48 * time.Hour)
	challenge := createSchedulerChallenge(t, database, repository.CreateChallengeInput{
		MainChatID:      -1001,
		Num:             1,
		Theme:           "Night",
		Hashtag:         "#night",
		AcceptStartAt:   testTime(0),
		AcceptUntilAt:   now.Add(time.Hour),
		ReminderAt:      now.Add(-time.Hour),
		CreatedByUserID: 10,
		CreatedAt:       testTime(0),
	})
	closingChallenge := createSchedulerChallenge(t, database, repository.CreateChallengeInput{
		MainChatID:      -1002,
		Num:             1,
		Theme:           "Street",
		Hashtag:         "#street",
		AcceptStartAt:   testTime(0),
		AcceptUntilAt:   now.Add(-time.Minute),
		ReminderAt:      now.Add(-time.Hour),
		CreatedByUserID: 10,
		CreatedAt:       testTime(0),
	})
	publisher := &recordingPublisher{failNext: true}
	scheduler := newTestScheduler(database, Config{
		Challenges: repository.NewChallenges(database),
		Publisher:  publisher,
		Now:        func() time.Time { return now },
	})

	if err := scheduler.Tick(ctx); err == nil {
		t.Fatal("Tick() error = nil, want send error")
	}
	stored, err := repository.NewChallenges(database).Get(ctx, challenge.ID)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if stored.ReminderSentAt != nil {
		t.Fatalf("ReminderSentAt = %v, want nil", stored.ReminderSentAt)
	}
	if stored.ReminderSendingAt != nil {
		t.Fatalf("ReminderSendingAt = %v, want nil", stored.ReminderSendingAt)
	}
	closed, err := repository.NewChallenges(database).Get(ctx, closingChallenge.ID)
	if err != nil {
		t.Fatalf("Get() closing challenge error = %v", err)
	}
	if closed.State != repository.ChallengeStateVoting {
		t.Fatalf("closing challenge State = %q, want voting", closed.State)
	}

	if err := scheduler.Tick(ctx); err != nil {
		t.Fatalf("second Tick() error = %v", err)
	}
	if len(publisher.messages) != 2 {
		t.Fatalf("sent messages = %d, want 2", len(publisher.messages))
	}
	stored, err = repository.NewChallenges(database).Get(ctx, challenge.ID)
	if err != nil {
		t.Fatalf("Get() after retry error = %v", err)
	}
	if stored.ReminderSentAt == nil {
		t.Fatal("ReminderSentAt after retry is nil, want timestamp")
	}
}

func TestTickSendTimeoutDoesNotBlockClosures(t *testing.T) {
	ctx := context.Background()
	database := openSchedulerTestDB(t)
	defer database.Close()

	now := testTime(48 * time.Hour)
	reminderChallenge := createSchedulerChallenge(t, database, repository.CreateChallengeInput{
		MainChatID:      -1001,
		Num:             1,
		Theme:           "Night",
		Hashtag:         "#night",
		AcceptStartAt:   testTime(0),
		AcceptUntilAt:   now.Add(time.Hour),
		ReminderAt:      now.Add(-time.Hour),
		CreatedByUserID: 10,
		CreatedAt:       testTime(0),
	})
	closingChallenge := createSchedulerChallenge(t, database, repository.CreateChallengeInput{
		MainChatID:      -1002,
		Num:             1,
		Theme:           "Street",
		Hashtag:         "#street",
		AcceptStartAt:   testTime(0),
		AcceptUntilAt:   now.Add(-time.Minute),
		ReminderAt:      now.Add(-time.Hour),
		CreatedByUserID: 10,
		CreatedAt:       testTime(0),
	})

	scheduler := newTestScheduler(database, Config{
		Challenges:  repository.NewChallenges(database),
		Publisher:   &recordingPublisher{blockNext: true},
		Now:         func() time.Time { return now },
		SendTimeout: time.Millisecond,
	})
	if err := scheduler.Tick(ctx); err == nil {
		t.Fatal("Tick() error = nil, want send timeout")
	}

	challenges := repository.NewChallenges(database)
	reminderStored, err := challenges.Get(ctx, reminderChallenge.ID)
	if err != nil {
		t.Fatalf("Get() reminder challenge error = %v", err)
	}
	if reminderStored.ReminderSentAt != nil {
		t.Fatalf("ReminderSentAt = %v, want nil", reminderStored.ReminderSentAt)
	}
	closed, err := challenges.Get(ctx, closingChallenge.ID)
	if err != nil {
		t.Fatalf("Get() closing challenge error = %v", err)
	}
	if closed.State != repository.ChallengeStateVoting {
		t.Fatalf("closing challenge State = %q, want voting", closed.State)
	}
}

func TestTickReleasesReminderClaimAfterParentCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	database := openSchedulerTestDB(t)
	defer database.Close()

	now := testTime(48 * time.Hour)
	challenge := createSchedulerChallenge(t, database, repository.CreateChallengeInput{
		MainChatID:      -1001,
		Num:             1,
		Theme:           "Night",
		Hashtag:         "#night",
		AcceptStartAt:   testTime(0),
		AcceptUntilAt:   now.Add(time.Hour),
		ReminderAt:      now.Add(-time.Hour),
		CreatedByUserID: 10,
		CreatedAt:       testTime(0),
	})
	publisher := &recordingPublisher{cancelBeforeError: cancel}
	scheduler := newTestScheduler(database, Config{
		Challenges: repository.NewChallenges(database),
		Publisher:  publisher,
		Now:        func() time.Time { return now },
	})

	if err := scheduler.Tick(ctx); err == nil {
		t.Fatal("Tick() error = nil, want send error")
	}
	stored, err := repository.NewChallenges(database).Get(context.Background(), challenge.ID)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if stored.ReminderSentAt != nil {
		t.Fatalf("ReminderSentAt = %v, want nil", stored.ReminderSentAt)
	}
	if stored.ReminderSendingAt != nil {
		t.Fatalf("ReminderSendingAt = %v, want nil", stored.ReminderSendingAt)
	}
}

func TestReminderMarkFailureIsAtLeastOnceDelivery(t *testing.T) {
	ctx := context.Background()
	database := openSchedulerTestDB(t)
	defer database.Close()

	now := testTime(48 * time.Hour)
	challenge := createSchedulerChallenge(t, database, repository.CreateChallengeInput{
		MainChatID:      -1001,
		Num:             1,
		Theme:           "Night",
		Hashtag:         "#night",
		AcceptStartAt:   testTime(0),
		AcceptUntilAt:   now.Add(time.Hour),
		ReminderAt:      now.Add(-time.Hour),
		CreatedByUserID: 10,
		CreatedAt:       testTime(0),
	})
	insertSchedulerPhoto(t, database, challenge.ID, 20, "photo-a")
	insertSchedulerPhoto(t, database, challenge.ID, 30, "photo-b")
	publisher := &recordingPublisher{}
	challenges := &failingMarkChallenges{Challenges: repository.NewChallenges(database), failNextMark: true}
	scheduler := newTestScheduler(database, Config{
		Challenges: challenges,
		Publisher:  publisher,
		Now:        func() time.Time { return now },
	})

	if err := scheduler.Tick(ctx); err == nil {
		t.Fatal("Tick() error = nil, want mark failure")
	}
	if len(publisher.messages) != 1 {
		t.Fatalf("sent reminders after mark failure = %d, want 1", len(publisher.messages))
	}
	stored, err := repository.NewChallenges(database).Get(ctx, challenge.ID)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if stored.ReminderSentAt != nil {
		t.Fatalf("ReminderSentAt = %v, want nil after mark failure", stored.ReminderSentAt)
	}

	now = now.Add(6 * time.Minute)
	if err := scheduler.Tick(ctx); err != nil {
		t.Fatalf("second Tick() error = %v", err)
	}
	if len(publisher.messages) != 2 {
		t.Fatalf("sent reminders after stale lease retry = %d, want 2", len(publisher.messages))
	}
	stored, err = repository.NewChallenges(database).Get(ctx, challenge.ID)
	if err != nil {
		t.Fatalf("Get() after retry error = %v", err)
	}
	if stored.ReminderSentAt == nil {
		t.Fatal("ReminderSentAt after retry is nil, want timestamp")
	}
}

func TestReminderClaimOwnerMustMarkSent(t *testing.T) {
	ctx := context.Background()
	database := openSchedulerTestDB(t)
	defer database.Close()

	now := testTime(48 * time.Hour)
	challenge := createSchedulerChallenge(t, database, repository.CreateChallengeInput{
		MainChatID:      -1001,
		Num:             1,
		Theme:           "Night",
		Hashtag:         "#night",
		AcceptStartAt:   testTime(0),
		AcceptUntilAt:   now.Add(time.Hour),
		ReminderAt:      now.Add(-time.Hour),
		CreatedByUserID: 10,
		CreatedAt:       testTime(0),
	})
	challenges := repository.NewChallenges(database)

	firstClaim := now.Add(-10 * time.Minute)
	claimed, err := challenges.ClaimReminder(ctx, challenge.ID, firstClaim)
	if err != nil {
		t.Fatalf("first ClaimReminder() error = %v", err)
	}
	if !claimed {
		t.Fatal("first ClaimReminder() = false, want true")
	}
	secondClaim := now
	claimed, err = challenges.ClaimReminder(ctx, challenge.ID, secondClaim)
	if err != nil {
		t.Fatalf("second ClaimReminder() error = %v", err)
	}
	if !claimed {
		t.Fatal("second ClaimReminder() = false, want true")
	}

	marked, err := challenges.MarkReminderSent(ctx, challenge.ID, 1, firstClaim, now)
	if err != nil {
		t.Fatalf("stale MarkReminderSent() error = %v", err)
	}
	if marked {
		t.Fatal("stale MarkReminderSent() = true, want false")
	}
	stored, err := challenges.Get(ctx, challenge.ID)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if stored.ReminderSentAt != nil {
		t.Fatalf("ReminderSentAt = %v, want nil", stored.ReminderSentAt)
	}

	marked, err = challenges.MarkReminderSent(ctx, challenge.ID, 2, secondClaim, now)
	if err != nil {
		t.Fatalf("current MarkReminderSent() error = %v", err)
	}
	if !marked {
		t.Fatal("current MarkReminderSent() = false, want true")
	}
	stored, err = challenges.Get(ctx, challenge.ID)
	if err != nil {
		t.Fatalf("Get() after current mark error = %v", err)
	}
	if stored.ReminderMessageID == nil || *stored.ReminderMessageID != 2 {
		t.Fatalf("ReminderMessageID = %v, want 2", stored.ReminderMessageID)
	}
}

func TestStateTransitionsRevalidateDueTime(t *testing.T) {
	ctx := context.Background()
	database := openSchedulerTestDB(t)
	defer database.Close()

	now := testTime(48 * time.Hour)
	challenge := createSchedulerChallenge(t, database, repository.CreateChallengeInput{
		MainChatID:      -1001,
		Num:             1,
		Theme:           "Night",
		Hashtag:         "#night",
		AcceptStartAt:   testTime(0),
		AcceptUntilAt:   now.Add(time.Minute),
		ReminderAt:      now.Add(time.Minute),
		CreatedByUserID: 10,
		CreatedAt:       testTime(0),
	})
	challenges := repository.NewChallenges(database)

	claimed, err := challenges.ClaimReminder(ctx, challenge.ID, now)
	if err != nil {
		t.Fatalf("ClaimReminder() error = %v", err)
	}
	if claimed {
		t.Fatal("ClaimReminder() = true before reminder_at, want false")
	}
	started, err := challenges.StartVoting(ctx, challenge.ID, now)
	if err != nil {
		t.Fatalf("StartVoting() error = %v", err)
	}
	if started {
		t.Fatal("StartVoting() = true before accept_until_at, want false")
	}

	if _, err := database.ExecContext(ctx, `
		UPDATE challenges
		SET state = 'voting',
			vote_started_at = ?,
			vote_until_at = ?,
			updated_at = ?
		WHERE id = ?
	`, timeString(now), timeString(now.Add(time.Minute)), timeString(now), challenge.ID); err != nil {
		t.Fatalf("set voting state: %v", err)
	}
	finished, err := challenges.FinishVoting(ctx, challenge.ID, now)
	if err != nil {
		t.Fatalf("FinishVoting() error = %v", err)
	}
	if finished {
		t.Fatal("FinishVoting() = true before vote_until_at, want false")
	}
}

func TestRunContinuesAfterTransientTickError(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	database := openSchedulerTestDB(t)
	defer database.Close()

	now := testTime(48 * time.Hour)
	challenge := createSchedulerChallenge(t, database, repository.CreateChallengeInput{
		MainChatID:      -1001,
		Num:             1,
		Theme:           "Night",
		Hashtag:         "#night",
		AcceptStartAt:   testTime(0),
		AcceptUntilAt:   now.Add(time.Hour),
		ReminderAt:      now.Add(-time.Hour),
		CreatedByUserID: 10,
		CreatedAt:       testTime(0),
	})

	publisher := &recordingPublisher{
		failNext: true,
		afterSend: func() {
			cancel()
		},
	}
	scheduler := newTestScheduler(database, Config{
		Challenges: repository.NewChallenges(database),
		Publisher:  publisher,
		Now:        func() time.Time { return now },
		Interval:   time.Millisecond,
	})

	errc := make(chan error, 1)
	go func() {
		errc <- scheduler.Run(ctx)
	}()

	select {
	case err := <-errc:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Run() error = %v, want context.Canceled", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run() did not retry after transient error")
	}
	if len(publisher.messages) != 1 {
		t.Fatalf("sent reminders = %d, want 1", len(publisher.messages))
	}
	stored, err := repository.NewChallenges(database).Get(context.Background(), challenge.ID)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if stored.ReminderSentAt == nil {
		t.Fatal("ReminderSentAt is nil after cancellation, want timestamp")
	}
}

func TestTickClosesAcceptanceWithoutStaleReminder(t *testing.T) {
	ctx := context.Background()
	database := openSchedulerTestDB(t)
	defer database.Close()

	now := testTime(48 * time.Hour)
	challenge := createSchedulerChallenge(t, database, repository.CreateChallengeInput{
		MainChatID:      -1001,
		Num:             1,
		Theme:           "Night",
		Hashtag:         "#night",
		AcceptStartAt:   testTime(0),
		AcceptUntilAt:   now.Add(-time.Minute),
		ReminderAt:      now.Add(-time.Hour),
		CreatedByUserID: 10,
		CreatedAt:       testTime(0),
	})
	insertSchedulerPhoto(t, database, challenge.ID, 20, "photo-a")
	insertSchedulerPhoto(t, database, challenge.ID, 30, "photo-b")
	publisher := &recordingPublisher{}
	scheduler := newTestScheduler(database, Config{
		Challenges: repository.NewChallenges(database),
		Publisher:  publisher,
		Now:        func() time.Time { return now },
	})

	if err := scheduler.Tick(ctx); err != nil {
		t.Fatalf("Tick() error = %v", err)
	}
	if len(publisher.messages) != 1 {
		t.Fatalf("sent vote start messages = %d, want 1", len(publisher.messages))
	}

	stored, err := repository.NewChallenges(database).Get(ctx, challenge.ID)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if stored.State != repository.ChallengeStateVoting {
		t.Fatalf("State = %q, want voting", stored.State)
	}
	if stored.VoteStartedAt == nil {
		t.Fatal("VoteStartedAt is nil, want timestamp")
	}
	if stored.VoteUntilAt == nil || !stored.VoteUntilAt.Equal(now.Add(48*time.Hour)) {
		t.Fatalf("VoteUntilAt = %v, want %s", stored.VoteUntilAt, now.Add(48*time.Hour))
	}
	if stored.VoteMessageID == nil || *stored.VoteMessageID != 1 {
		t.Fatalf("VoteMessageID = %v, want 1", stored.VoteMessageID)
	}
	if stored.VotePinnedAt == nil {
		t.Fatal("VotePinnedAt is nil, want timestamp")
	}
	if !strings.Contains(publisher.messages[0].text, "2 https://t.me/PhotoChallengeBot?start=-1001_"+strconv.FormatInt(challenge.ID, 10)) {
		t.Fatalf("vote start text = %q, want photo count and deep-link", publisher.messages[0].text)
	}
	if len(publisher.pins) != 1 || publisher.pins[0].chatID != -1001 || publisher.pins[0].messageID != 1 {
		t.Fatalf("pins = %#v, want vote start message pinned", publisher.pins)
	}
}

func TestTickRetriesVoteStartPinWithoutDuplicateMessage(t *testing.T) {
	ctx := context.Background()
	database := openSchedulerTestDB(t)
	defer database.Close()

	now := testTime(48 * time.Hour)
	challenge := createSchedulerChallenge(t, database, repository.CreateChallengeInput{
		MainChatID:      -1001,
		Num:             1,
		Theme:           "Night",
		Hashtag:         "#night",
		AcceptStartAt:   testTime(0),
		AcceptUntilAt:   now.Add(-time.Minute),
		ReminderAt:      now.Add(-time.Hour),
		CreatedByUserID: 10,
		CreatedAt:       testTime(0),
	})

	publisher := &recordingPublisher{failPin: true}
	scheduler := newTestScheduler(database, Config{
		Publisher: publisher,
		Now:       func() time.Time { return now },
	})

	if err := scheduler.Tick(ctx); err == nil {
		t.Fatal("Tick() error = nil, want pin error")
	}
	if len(publisher.messages) != 1 {
		t.Fatalf("sent vote start messages after pin failure = %d, want 1", len(publisher.messages))
	}
	stored, err := repository.NewChallenges(database).Get(ctx, challenge.ID)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if stored.VoteMessageID == nil || *stored.VoteMessageID != 1 {
		t.Fatalf("VoteMessageID = %v, want stored message id after pin failure", stored.VoteMessageID)
	}
	if stored.VotePinnedAt != nil {
		t.Fatalf("VotePinnedAt = %v, want nil after pin failure", stored.VotePinnedAt)
	}

	if err := scheduler.Tick(ctx); err != nil {
		t.Fatalf("second Tick() error = %v", err)
	}
	if len(publisher.messages) != 1 {
		t.Fatalf("sent vote start messages after retry = %d, want no duplicate", len(publisher.messages))
	}
	if len(publisher.pins) != 1 || publisher.pins[0].messageID != 1 {
		t.Fatalf("pins after retry = %#v, want original message pinned", publisher.pins)
	}
	stored, err = repository.NewChallenges(database).Get(ctx, challenge.ID)
	if err != nil {
		t.Fatalf("Get() after retry error = %v", err)
	}
	if stored.VotePinnedAt == nil {
		t.Fatal("VotePinnedAt after retry is nil, want timestamp")
	}
}

func TestTickExtendsExpiredUnpublishedVoteWindowBeforePublishing(t *testing.T) {
	ctx := context.Background()
	database := openSchedulerTestDB(t)
	defer database.Close()

	now := testTime(96 * time.Hour)
	challenge := createSchedulerChallenge(t, database, repository.CreateChallengeInput{
		MainChatID:      -1001,
		Num:             1,
		Theme:           "Night",
		Hashtag:         "#night",
		AcceptStartAt:   testTime(0),
		AcceptUntilAt:   testTime(24 * time.Hour),
		ReminderAt:      testTime(24*time.Hour - time.Hour),
		CreatedByUserID: 10,
		CreatedAt:       testTime(0),
	})
	insertSchedulerPhoto(t, database, challenge.ID, 20, "photo-a")
	if _, err := database.ExecContext(ctx, `
		UPDATE challenges
		SET state = 'voting',
			vote_started_at = ?,
			vote_until_at = ?,
			updated_at = ?
		WHERE id = ?
	`, timeString(testTime(48*time.Hour)), timeString(now.Add(-time.Minute)), timeString(testTime(48*time.Hour)), challenge.ID); err != nil {
		t.Fatalf("seed expired unpublished voting: %v", err)
	}

	publisher := &recordingPublisher{}
	scheduler := newTestScheduler(database, Config{
		Publisher: publisher,
		Now:       func() time.Time { return now },
	})

	if err := scheduler.Tick(ctx); err != nil {
		t.Fatalf("Tick() error = %v", err)
	}
	if len(publisher.messages) != 1 {
		t.Fatalf("messages = %#v, want vote-start announcement", publisher.messages)
	}
	stored, err := repository.NewChallenges(database).Get(ctx, challenge.ID)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if stored.State != repository.ChallengeStateVoting {
		t.Fatalf("State = %q, want still voting", stored.State)
	}
	if stored.VoteUntilAt == nil || !stored.VoteUntilAt.After(now) {
		t.Fatalf("VoteUntilAt = %v, want extended future window", stored.VoteUntilAt)
	}
	if stored.VotePinnedAt == nil {
		t.Fatal("VotePinnedAt = nil, want published vote start pinned")
	}
}

func TestTickDoesNotFinishExpiredVotingBeforeVoteStartPinned(t *testing.T) {
	ctx := context.Background()
	database := openSchedulerTestDB(t)
	defer database.Close()

	now := testTime(96 * time.Hour)
	challenge := createSchedulerChallenge(t, database, repository.CreateChallengeInput{
		MainChatID:      -1001,
		Num:             1,
		Theme:           "Night",
		Hashtag:         "#night",
		AcceptStartAt:   testTime(0),
		AcceptUntilAt:   testTime(24 * time.Hour),
		ReminderAt:      testTime(24*time.Hour - time.Hour),
		CreatedByUserID: 10,
		CreatedAt:       testTime(0),
	})
	insertSchedulerPhoto(t, database, challenge.ID, 20, "photo-a")
	if _, err := database.ExecContext(ctx, `
		UPDATE challenges
		SET state = 'voting',
			vote_started_at = ?,
			vote_until_at = ?,
			updated_at = ?
		WHERE id = ?
	`, timeString(testTime(48*time.Hour)), timeString(now.Add(-time.Minute)), timeString(testTime(48*time.Hour)), challenge.ID); err != nil {
		t.Fatalf("seed expired unpublished voting: %v", err)
	}

	scheduler := newTestScheduler(database, Config{
		Publisher: &recordingPublisher{failNext: true},
		Now:       func() time.Time { return now },
	})

	if err := scheduler.Tick(ctx); err == nil {
		t.Fatal("Tick() error = nil, want vote-start send error")
	}
	stored, err := repository.NewChallenges(database).Get(ctx, challenge.ID)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if stored.State != repository.ChallengeStateVoting {
		t.Fatalf("State = %q, want not finished before vote start pin", stored.State)
	}
	if stored.VotePinnedAt != nil {
		t.Fatalf("VotePinnedAt = %v, want nil after failed announcement", stored.VotePinnedAt)
	}
	if stored.VoteUntilAt == nil || !stored.VoteUntilAt.Before(now) {
		t.Fatalf("VoteUntilAt = %v, want unchanged expired deadline after failed announcement", stored.VoteUntilAt)
	}
}

func TestTickDoesNotExtendVotingWindowWhenVoteLinkFails(t *testing.T) {
	ctx := context.Background()
	database := openSchedulerTestDB(t)
	defer database.Close()

	now := testTime(96 * time.Hour)
	challenge := seedExpiredUnpublishedVoting(t, ctx, database, now)

	scheduler := newTestScheduler(database, Config{
		Publisher:   &recordingPublisher{},
		BotUsername: func() string { return "" },
		Now:         func() time.Time { return now },
	})

	if err := scheduler.Tick(ctx); err == nil {
		t.Fatal("Tick() error = nil, want vote link error")
	}
	assertExpiredUnpublishedVoting(t, ctx, database, challenge.ID, now)
}

func TestTickDoesNotExtendVotingWindowWhenVoteStartRenderFails(t *testing.T) {
	ctx := context.Background()
	database := openSchedulerTestDB(t)
	defer database.Close()

	now := testTime(96 * time.Hour)
	challenge := seedExpiredUnpublishedVoting(t, ctx, database, now)

	scheduler := newTestScheduler(database, Config{
		Renderer:  failingVoteStartRenderer{},
		Publisher: &recordingPublisher{},
		Now:       func() time.Time { return now },
	})

	if err := scheduler.Tick(ctx); err == nil {
		t.Fatal("Tick() error = nil, want vote start render error")
	}
	assertExpiredUnpublishedVoting(t, ctx, database, challenge.ID, now)
}

func TestTickResetsVotingWindowFromSuccessfulVoteStartRetry(t *testing.T) {
	ctx := context.Background()
	database := openSchedulerTestDB(t)
	defer database.Close()

	firstAttemptAt := testTime(48 * time.Hour)
	challenge := createSchedulerChallenge(t, database, repository.CreateChallengeInput{
		MainChatID:      -1001,
		Num:             1,
		Theme:           "Night",
		Hashtag:         "#night",
		AcceptStartAt:   testTime(0),
		AcceptUntilAt:   firstAttemptAt.Add(-time.Minute),
		ReminderAt:      firstAttemptAt.Add(-time.Hour),
		CreatedByUserID: 10,
		CreatedAt:       testTime(0),
	})
	insertSchedulerPhoto(t, database, challenge.ID, 20, "photo-a")
	publisher := &recordingPublisher{failNext: true}
	now := firstAttemptAt
	scheduler := newTestScheduler(database, Config{
		Publisher: publisher,
		Now:       func() time.Time { return now },
	})

	if err := scheduler.Tick(ctx); err == nil {
		t.Fatal("first Tick() error = nil, want vote-start send error")
	}
	stored, err := repository.NewChallenges(database).Get(ctx, challenge.ID)
	if err != nil {
		t.Fatalf("Get() after first attempt error = %v", err)
	}
	firstDeadline := firstAttemptAt.Add(48 * time.Hour)
	if stored.VoteUntilAt == nil || !stored.VoteUntilAt.Equal(firstDeadline) {
		t.Fatalf("VoteUntilAt after failed attempt = %v, want initial deadline %s", stored.VoteUntilAt, firstDeadline)
	}

	now = firstAttemptAt.Add(time.Hour)
	if err := scheduler.Tick(ctx); err != nil {
		t.Fatalf("retry Tick() error = %v", err)
	}
	stored, err = repository.NewChallenges(database).Get(ctx, challenge.ID)
	if err != nil {
		t.Fatalf("Get() after retry error = %v", err)
	}
	wantDeadline := now.Add(48 * time.Hour)
	if stored.VoteUntilAt == nil || !stored.VoteUntilAt.Equal(wantDeadline) {
		t.Fatalf("VoteUntilAt after retry = %v, want reset deadline %s", stored.VoteUntilAt, wantDeadline)
	}
	if stored.VotePinnedAt == nil {
		t.Fatal("VotePinnedAt = nil, want vote-start pinned after retry")
	}
}

func TestTickRecordsVoteMessageIDAfterClaimedPersistFailure(t *testing.T) {
	ctx := context.Background()
	database := openSchedulerTestDB(t)
	defer database.Close()

	now := testTime(48 * time.Hour)
	challenge := createSchedulerChallenge(t, database, repository.CreateChallengeInput{
		MainChatID:      -1001,
		Num:             1,
		Theme:           "Night",
		Hashtag:         "#night",
		AcceptStartAt:   testTime(0),
		AcceptUntilAt:   now.Add(-time.Minute),
		ReminderAt:      now.Add(-time.Hour),
		CreatedByUserID: 10,
		CreatedAt:       testTime(0),
	})
	insertSchedulerPhoto(t, database, challenge.ID, 20, "photo-a")
	challenges := &failingMarkChallenges{
		Challenges:           repository.NewChallenges(database),
		failSetVoteMessageID: true,
	}
	publisher := &recordingPublisher{}
	scheduler := newTestScheduler(database, Config{
		Challenges: challenges,
		Publisher:  publisher,
		Now:        func() time.Time { return now },
	})

	if err := scheduler.Tick(ctx); err != nil {
		t.Fatalf("Tick() error = %v", err)
	}
	if len(publisher.messages) != 1 {
		t.Fatalf("messages = %#v, want one vote-start send", publisher.messages)
	}
	if len(publisher.pins) != 1 || publisher.pins[0].messageID != 1 {
		t.Fatalf("pins = %#v, want sent message pinned", publisher.pins)
	}
	stored, err := repository.NewChallenges(database).Get(ctx, challenge.ID)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if stored.VoteMessageID == nil || *stored.VoteMessageID != 1 {
		t.Fatalf("VoteMessageID = %v, want fallback-recorded id", stored.VoteMessageID)
	}
}

func TestTickRetriesExistingVoteStartPinWithoutRendering(t *testing.T) {
	ctx := context.Background()
	database := openSchedulerTestDB(t)
	defer database.Close()

	now := testTime(48 * time.Hour)
	challenge := createSchedulerChallenge(t, database, repository.CreateChallengeInput{
		MainChatID:      -1001,
		Num:             1,
		Theme:           "Night",
		Hashtag:         "#night",
		AcceptStartAt:   testTime(0),
		AcceptUntilAt:   now.Add(-time.Minute),
		ReminderAt:      now.Add(-time.Hour),
		CreatedByUserID: 10,
		CreatedAt:       testTime(0),
	})
	challenges := repository.NewChallenges(database)
	if started, err := challenges.StartVoting(ctx, challenge.ID, now); err != nil {
		t.Fatalf("StartVoting() error = %v", err)
	} else if !started {
		t.Fatal("StartVoting() = false, want true")
	}
	claimedAt := now.Add(-10 * time.Minute)
	if claimed, err := challenges.ClaimVoteStart(ctx, challenge.ID, claimedAt); err != nil {
		t.Fatalf("ClaimVoteStart() error = %v", err)
	} else if !claimed {
		t.Fatal("ClaimVoteStart() = false, want true")
	}
	if marked, err := challenges.SetVoteMessageID(ctx, challenge.ID, 42, claimedAt, now); err != nil {
		t.Fatalf("SetVoteMessageID() error = %v", err)
	} else if !marked {
		t.Fatal("SetVoteMessageID() = false, want true")
	}
	if err := challenges.ReleaseVoteStartClaim(ctx, challenge.ID, claimedAt); err != nil {
		t.Fatalf("ReleaseVoteStartClaim() error = %v", err)
	}

	publisher := &recordingPublisher{}
	scheduler := newTestScheduler(database, Config{
		Renderer:    failingVoteStartRenderer{},
		Publisher:   publisher,
		Now:         func() time.Time { return now },
		BatchSize:   10,
		SendTimeout: time.Second,
	})

	if err := scheduler.Tick(ctx); err != nil {
		t.Fatalf("Tick() error = %v", err)
	}
	if len(publisher.messages) != 0 {
		t.Fatalf("sent vote start messages = %d, want 0", len(publisher.messages))
	}
	if len(publisher.pins) != 1 || publisher.pins[0].messageID != 42 {
		t.Fatalf("pins = %#v, want stored message pinned", publisher.pins)
	}
	stored, err := challenges.Get(ctx, challenge.ID)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if stored.VotePinnedAt == nil {
		t.Fatal("VotePinnedAt is nil, want timestamp")
	}
}

func TestTickDoesNotCloseFractionalFutureAcceptance(t *testing.T) {
	ctx := context.Background()
	database := openSchedulerTestDB(t)
	defer database.Close()

	now := testTime(48 * time.Hour).Truncate(time.Second)
	challenge := createSchedulerChallenge(t, database, repository.CreateChallengeInput{
		MainChatID:      -1001,
		Num:             1,
		Theme:           "Night",
		Hashtag:         "#night",
		AcceptStartAt:   testTime(0),
		AcceptUntilAt:   now.Add(500 * time.Millisecond),
		ReminderAt:      now.Add(time.Hour),
		CreatedByUserID: 10,
		CreatedAt:       testTime(0),
	})

	scheduler := newTestScheduler(database, Config{
		Challenges: repository.NewChallenges(database),
		Publisher:  &recordingPublisher{},
		Now:        func() time.Time { return now },
	})
	if err := scheduler.Tick(ctx); err != nil {
		t.Fatalf("Tick() error = %v", err)
	}

	stored, err := repository.NewChallenges(database).Get(ctx, challenge.ID)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if stored.State != repository.ChallengeStateActive {
		t.Fatalf("State = %q, want active", stored.State)
	}
}

func TestTickClosesMixedFormatAcceptanceBoundary(t *testing.T) {
	ctx := context.Background()
	database := openSchedulerTestDB(t)
	defer database.Close()

	now := testTime(48 * time.Hour).Truncate(time.Second)
	challenge := createSchedulerChallenge(t, database, repository.CreateChallengeInput{
		MainChatID:      -1001,
		Num:             1,
		Theme:           "Night",
		Hashtag:         "#night",
		AcceptStartAt:   testTime(0),
		AcceptUntilAt:   now.Add(time.Hour),
		ReminderAt:      now.Add(time.Hour),
		CreatedByUserID: 10,
		CreatedAt:       testTime(0),
	})
	if _, err := database.ExecContext(ctx, `
		UPDATE challenges
		SET accept_until_at = ?
		WHERE id = ?
	`, now.Format(time.RFC3339Nano), challenge.ID); err != nil {
		t.Fatalf("set mixed-format accept_until_at: %v", err)
	}

	scheduler := newTestScheduler(database, Config{
		Challenges: repository.NewChallenges(database),
		Publisher:  &recordingPublisher{},
		Now:        func() time.Time { return now },
	})
	if err := scheduler.Tick(ctx); err != nil {
		t.Fatalf("Tick() error = %v", err)
	}

	stored, err := repository.NewChallenges(database).Get(ctx, challenge.ID)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if stored.State != repository.ChallengeStateVoting {
		t.Fatalf("State = %q, want voting", stored.State)
	}
}

func TestTickClosesVotingWhenDue(t *testing.T) {
	ctx := context.Background()
	database := openSchedulerTestDB(t)
	defer database.Close()

	now := testTime(96 * time.Hour)
	challenge := createSchedulerChallenge(t, database, repository.CreateChallengeInput{
		MainChatID:      -1001,
		Num:             1,
		Theme:           "Night",
		Hashtag:         "#night",
		AcceptStartAt:   testTime(0),
		AcceptUntilAt:   testTime(24 * time.Hour),
		ReminderAt:      testTime(24*time.Hour - time.Hour),
		CreatedByUserID: 10,
		CreatedAt:       testTime(0),
	})
	if _, err := database.ExecContext(ctx, `
		UPDATE challenges
		SET state = 'voting',
			vote_started_at = ?,
			vote_until_at = ?,
			vote_message_id = 42,
			vote_pinned_at = ?,
			updated_at = ?
		WHERE id = ?
	`, timeString(testTime(48*time.Hour)), timeString(now.Add(-time.Minute)), timeString(testTime(48*time.Hour)), timeString(testTime(48*time.Hour)), challenge.ID); err != nil {
		t.Fatalf("mark challenge voting: %v", err)
	}

	scheduler := newTestScheduler(database, Config{
		Challenges: repository.NewChallenges(database),
		Publisher:  &recordingPublisher{},
		Now:        func() time.Time { return now },
	})
	if err := scheduler.Tick(ctx); err != nil {
		t.Fatalf("Tick() error = %v", err)
	}

	stored, err := repository.NewChallenges(database).Get(ctx, challenge.ID)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if stored.State != repository.ChallengeStateFinished {
		t.Fatalf("State = %q, want finished", stored.State)
	}
	if stored.FinishedAt == nil {
		t.Fatal("FinishedAt is nil, want timestamp")
	}
}

func newTestScheduler(database *sqlx.DB, config Config) *Scheduler {
	if config.Challenges == nil {
		config.Challenges = repository.NewChallenges(database)
	}
	if config.Photos == nil {
		config.Photos = repository.NewPhotos(database)
	}
	if config.Renderer == nil {
		config.Renderer = voteStartRenderer{}
	}
	if config.BotUsername == nil {
		config.BotUsername = func() string {
			return "PhotoChallengeBot"
		}
	}
	return New(config)
}

func openSchedulerTestDB(t *testing.T) *sqlx.DB {
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

func createSchedulerChallenge(t *testing.T, database *sqlx.DB, input repository.CreateChallengeInput) repository.Challenge {
	t.Helper()
	ctx := context.Background()

	if _, err := repository.NewUsers(database).Upsert(ctx, repository.User{ID: input.CreatedByUserID, FirstName: "Admin"}); err != nil {
		t.Fatalf("upsert creator: %v", err)
	}
	challenge, err := repository.NewChallenges(database).Create(ctx, input)
	if err != nil {
		t.Fatalf("create challenge: %v", err)
	}
	return challenge
}

func seedExpiredUnpublishedVoting(t *testing.T, ctx context.Context, database *sqlx.DB, now time.Time) repository.Challenge {
	t.Helper()

	challenge := createSchedulerChallenge(t, database, repository.CreateChallengeInput{
		MainChatID:      -1001,
		Num:             1,
		Theme:           "Night",
		Hashtag:         "#night",
		AcceptStartAt:   testTime(0),
		AcceptUntilAt:   testTime(24 * time.Hour),
		ReminderAt:      testTime(24*time.Hour - time.Hour),
		CreatedByUserID: 10,
		CreatedAt:       testTime(0),
	})
	insertSchedulerPhoto(t, database, challenge.ID, 20, "photo-a")
	if _, err := database.ExecContext(ctx, `
		UPDATE challenges
		SET state = 'voting',
			vote_started_at = ?,
			vote_until_at = ?,
			updated_at = ?
		WHERE id = ?
	`, timeString(testTime(48*time.Hour)), timeString(now.Add(-time.Minute)), timeString(testTime(48*time.Hour)), challenge.ID); err != nil {
		t.Fatalf("seed expired unpublished voting: %v", err)
	}
	return challenge
}

func assertExpiredUnpublishedVoting(t *testing.T, ctx context.Context, database *sqlx.DB, challengeID int64, now time.Time) {
	t.Helper()

	stored, err := repository.NewChallenges(database).Get(ctx, challengeID)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if stored.State != repository.ChallengeStateVoting {
		t.Fatalf("State = %q, want not finished before vote start pin", stored.State)
	}
	if stored.VotePinnedAt != nil {
		t.Fatalf("VotePinnedAt = %v, want nil after failed announcement", stored.VotePinnedAt)
	}
	if stored.VoteUntilAt == nil || !stored.VoteUntilAt.Before(now) {
		t.Fatalf("VoteUntilAt = %v, want unchanged expired deadline after failed announcement", stored.VoteUntilAt)
	}
}

func insertSchedulerPhoto(t *testing.T, database *sqlx.DB, challengeID, authorUserID int64, fileID string) {
	t.Helper()

	ctx := context.Background()
	if _, err := repository.NewUsers(database).Upsert(ctx, repository.User{ID: authorUserID, FirstName: "User"}); err != nil {
		t.Fatalf("upsert photo author: %v", err)
	}
	if _, _, err := repository.NewPhotos(database).UpsertCurrent(ctx, repository.UpsertPhotoInput{
		ChallengeID:     challengeID,
		AuthorUserID:    authorUserID,
		FileID:          fileID,
		SourceChatID:    -1001,
		SourceMessageID: int(authorUserID),
		SubmittedAt:     testTime(time.Hour),
	}); err != nil {
		t.Fatalf("insert scheduler photo: %v", err)
	}
}

func testTime(offset time.Duration) time.Time {
	return time.Date(2026, 5, 18, 12, 0, 0, 0, time.UTC).Add(offset)
}

func timeString(value time.Time) string {
	return value.UTC().Format(time.RFC3339Nano)
}

type recordingPublisher struct {
	failNext          bool
	failPin           bool
	blockNext         bool
	cancelBeforeError context.CancelFunc
	afterSend         func()
	messages          []recordedMessage
	pins              []recordedPin
}

type recordedMessage struct {
	chatID int64
	text   string
}

type recordedPin struct {
	chatID    int64
	messageID int
}

func (p *recordingPublisher) SendMarkdown(ctx context.Context, chatID int64, text string) (int, error) {
	if p.failNext {
		p.failNext = false
		return 0, errors.New("send failed")
	}
	if p.blockNext {
		p.blockNext = false
		<-ctx.Done()
		return 0, ctx.Err()
	}
	if p.cancelBeforeError != nil {
		p.cancelBeforeError()
		return 0, context.Canceled
	}
	p.messages = append(p.messages, recordedMessage{chatID: chatID, text: text})
	if p.afterSend != nil {
		p.afterSend()
	}
	return len(p.messages), nil
}

func (p *recordingPublisher) Pin(_ context.Context, chatID int64, messageID int) error {
	if p.failPin {
		p.failPin = false
		return errors.New("pin failed")
	}
	p.pins = append(p.pins, recordedPin{chatID: chatID, messageID: messageID})
	return nil
}

type voteStartRenderer struct{}

func (voteStartRenderer) VoteStart(data templates.VoteStartData) (string, error) {
	return "vote: " + data.Theme + " " + strconv.Itoa(data.AmountPhoto) + " " + data.VoteLink + " " + data.ResultsDate, nil
}

type failingVoteStartRenderer struct{}

func (failingVoteStartRenderer) VoteStart(templates.VoteStartData) (string, error) {
	return "", errors.New("render failed")
}

type failingMarkChallenges struct {
	*repository.Challenges
	failNextMark         bool
	failSetVoteMessageID bool
}

func (c *failingMarkChallenges) MarkReminderSent(
	ctx context.Context,
	id int64,
	messageID int,
	claimedAt time.Time,
	sentAt time.Time,
) (bool, error) {
	if c.failNextMark {
		c.failNextMark = false
		return false, errors.New("mark failed")
	}
	return c.Challenges.MarkReminderSent(ctx, id, messageID, claimedAt, sentAt)
}

func (c *failingMarkChallenges) SetVoteMessageID(
	ctx context.Context,
	id int64,
	messageID int,
	claimedAt time.Time,
	updatedAt time.Time,
) (bool, error) {
	if c.failSetVoteMessageID {
		c.failSetVoteMessageID = false
		return false, errors.New("set vote message id failed")
	}
	return c.Challenges.SetVoteMessageID(ctx, id, messageID, claimedAt, updatedAt)
}
