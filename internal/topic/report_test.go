package topic

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/TiraelSedai/PhotoChallengeBot/internal/repository"
)

func TestReporterPublishesSuggestionsToAdmin(t *testing.T) {
	now := time.Date(2026, 5, 22, 12, 0, 0, 0, time.UTC)
	challenge := votingChallenge(now)
	challenge.State = repository.ChallengeStateFinished
	deps := newTopicReportDeps(challenge)
	deps.suggestions = []repository.TopicSuggestion{
		{ChallengeID: challenge.ID, AuthorUserID: 11, Text: "Туман над рекой #тема"},
		{ChallengeID: challenge.ID, AuthorUserID: 12, Text: "Фонари после дождя #тема"},
	}
	deps.usersByID[11] = repository.User{ID: 11, Username: "alice", DisplayName: "Alice"}
	deps.usersByID[12] = repository.User{ID: 12, DisplayName: "Bob Example"}
	reporter := newTestReporter(deps, now)

	if err := reporter.PublishOne(context.Background(), challenge); err != nil {
		t.Fatalf("PublishOne() error = %v", err)
	}

	if len(deps.messages) != 1 {
		t.Fatalf("messages length = %d, want 1", len(deps.messages))
	}
	text := deps.messages[0]
	for _, want := range []string{
		"Темы, предложенные во время голосования за челлендж #0",
		"1. @alice: Туман над рекой #тема",
		"2. Bob Example: Фонари после дождя #тема",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("message = %q, want to contain %q", text, want)
		}
	}
	if deps.sentChallengeID != challenge.ID {
		t.Fatalf("sentChallengeID = %d, want %d", deps.sentChallengeID, challenge.ID)
	}
}

func TestNewReporterPanicsOnNilClock(t *testing.T) {
	deps := newTopicReportDeps(repository.Challenge{})
	defer func() {
		if recover() == nil {
			t.Fatal("NewReporter() did not panic")
		}
	}()
	NewReporter(ReportConfig{
		AdminChatID: 2002,
		Challenges:  deps.challenges,
		Suggestions: deps.suggestionsStore,
		Users:       deps.users,
		Publisher:   deps.publisher,
	})
}

func TestReporterPublishesEmptySuggestionReport(t *testing.T) {
	now := time.Date(2026, 5, 22, 12, 0, 0, 0, time.UTC)
	challenge := votingChallenge(now)
	challenge.State = repository.ChallengeStateFinished
	deps := newTopicReportDeps(challenge)
	reporter := newTestReporter(deps, now)

	if err := reporter.PublishOne(context.Background(), challenge); err != nil {
		t.Fatalf("PublishOne() error = %v", err)
	}

	want := []string{"Темы, предложенные во время голосования за челлендж #0:\n\nТем за время голосования не предложили."}
	if !reflect.DeepEqual(deps.messages, want) {
		t.Fatalf("messages = %v, want %v", deps.messages, want)
	}
	if deps.sentChallengeID != challenge.ID {
		t.Fatalf("sentChallengeID = %d, want %d", deps.sentChallengeID, challenge.ID)
	}
}

func TestReporterSplitsLongSuggestionReport(t *testing.T) {
	now := time.Date(2026, 5, 22, 12, 0, 0, 0, time.UTC)
	challenge := votingChallenge(now)
	challenge.State = repository.ChallengeStateFinished
	deps := newTopicReportDeps(challenge)
	deps.usersByID[11] = repository.User{ID: 11, Username: "alice", DisplayName: "Alice"}
	for i := range 12 {
		deps.suggestions = append(deps.suggestions, repository.TopicSuggestion{
			ChallengeID:  challenge.ID,
			AuthorUserID: 11,
			Text:         strings.Repeat("очень длинная тема ", 25) + "#тема",
			SuggestedAt:  now.Add(time.Duration(i) * time.Minute),
		})
	}
	reporter := newTestReporter(deps, now)

	if err := reporter.PublishOne(context.Background(), challenge); err != nil {
		t.Fatalf("PublishOne() error = %v", err)
	}

	if len(deps.messages) < 2 {
		t.Fatalf("messages length = %d, want split report", len(deps.messages))
	}
	for idx, message := range deps.messages {
		if len(message) > maxReportMessageLength {
			t.Fatalf("message %d length = %d, want <= %d", idx, len(message), maxReportMessageLength)
		}
	}
	if !strings.Contains(deps.messages[1], "продолжение") {
		t.Fatalf("second message = %q, want continuation header", deps.messages[1])
	}
	if deps.sentChallengeID != challenge.ID {
		t.Fatalf("sentChallengeID = %d, want %d", deps.sentChallengeID, challenge.ID)
	}
}

func TestReporterMarksSentAfterParentCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	now := time.Date(2026, 5, 22, 12, 0, 0, 0, time.UTC)
	challenge := votingChallenge(now)
	challenge.State = repository.ChallengeStateFinished
	deps := newTopicReportDeps(challenge)
	deps.failCanceledPersistence = true
	deps.afterSend = cancel
	reporter := newTestReporter(deps, now)

	if err := reporter.PublishOne(ctx, challenge); err != nil {
		t.Fatalf("PublishOne() error = %v", err)
	}
	if deps.sentChallengeID != challenge.ID {
		t.Fatalf("sentChallengeID = %d, want %d", deps.sentChallengeID, challenge.ID)
	}
}

func TestReporterReleasesClaimWhenSendFails(t *testing.T) {
	now := time.Date(2026, 5, 22, 12, 0, 0, 0, time.UTC)
	challenge := votingChallenge(now)
	challenge.State = repository.ChallengeStateFinished
	deps := newTopicReportDeps(challenge)
	wantErr := errors.New("telegram failed")
	deps.publishErr = wantErr
	reporter := newTestReporter(deps, now)

	err := reporter.PublishOne(context.Background(), challenge)
	if !errors.Is(err, wantErr) {
		t.Fatalf("PublishOne() error = %v, want %v", err, wantErr)
	}
	if deps.releasedChallengeID != challenge.ID {
		t.Fatalf("releasedChallengeID = %d, want %d", deps.releasedChallengeID, challenge.ID)
	}
	if deps.sentChallengeID != 0 {
		t.Fatalf("sentChallengeID = %d, want 0 after send failure", deps.sentChallengeID)
	}
}

func TestReporterReleasesClaimAfterParentCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	now := time.Date(2026, 5, 22, 12, 0, 0, 0, time.UTC)
	challenge := votingChallenge(now)
	challenge.State = repository.ChallengeStateFinished
	deps := newTopicReportDeps(challenge)
	deps.failCanceledPersistence = true
	wantErr := errors.New("telegram failed")
	deps.publishErr = wantErr
	deps.beforeError = cancel
	reporter := newTestReporter(deps, now)

	err := reporter.PublishOne(ctx, challenge)
	if !errors.Is(err, wantErr) {
		t.Fatalf("PublishOne() error = %v, want %v", err, wantErr)
	}
	if deps.releasedChallengeID != challenge.ID {
		t.Fatalf("releasedChallengeID = %d, want %d", deps.releasedChallengeID, challenge.ID)
	}
}

func newTestReporter(deps *topicReportDeps, now time.Time) *Reporter {
	return NewReporter(ReportConfig{
		AdminChatID: 2002,
		Challenges:  deps.challenges,
		Suggestions: deps.suggestionsStore,
		Users:       deps.users,
		Publisher:   deps.publisher,
		Now: func() time.Time {
			return now
		},
	})
}

type topicReportDeps struct {
	challenge               repository.Challenge
	usersByID               map[int64]repository.User
	suggestions             []repository.TopicSuggestion
	messages                []string
	claimedChallengeID      int64
	sentChallengeID         int64
	releasedChallengeID     int64
	failCanceledPersistence bool
	publishErr              error
	afterSend               func()
	beforeError             context.CancelFunc
	challenges              *MoqReportChallenges
	suggestionsStore        *MoqReportSuggestions
	users                   *MoqReportUsers
	publisher               *MoqReportPublisher
}

func newTopicReportDeps(challenge repository.Challenge) *topicReportDeps {
	deps := &topicReportDeps{
		challenge: challenge,
		usersByID: make(map[int64]repository.User),
	}
	deps.challenges = &MoqReportChallenges{
		ListUnsentTopicReportsFunc: func(context.Context, int64, int) ([]repository.Challenge, error) {
			return []repository.Challenge{deps.challenge}, nil
		},
		ClaimTopicReportFunc: func(_ context.Context, id int64, _ time.Time) (bool, error) {
			deps.claimedChallengeID = id
			return true, nil
		},
		MarkTopicReportSentFunc: func(ctx context.Context, id int64, _, _ time.Time) (bool, error) {
			if deps.failCanceledPersistence && ctx.Err() != nil {
				return false, ctx.Err()
			}
			deps.sentChallengeID = id
			return true, nil
		},
		ReleaseTopicReportClaimFunc: func(ctx context.Context, id int64, _ time.Time) error {
			if deps.failCanceledPersistence && ctx.Err() != nil {
				return ctx.Err()
			}
			deps.releasedChallengeID = id
			return nil
		},
	}
	deps.suggestionsStore = &MoqReportSuggestions{ListByChallengeFunc: func(context.Context, int64) ([]repository.TopicSuggestion, error) {
		return deps.suggestions, nil
	}}
	deps.users = &MoqReportUsers{GetFunc: func(_ context.Context, id int64) (repository.User, error) {
		user, ok := deps.usersByID[id]
		if !ok {
			return repository.User{ID: id, DisplayName: "Unknown"}, nil
		}
		return user, nil
	}}
	deps.publisher = &MoqReportPublisher{SendTextFunc: func(_ context.Context, _ int64, text string) (int, error) {
		if deps.publishErr != nil {
			if deps.beforeError != nil {
				deps.beforeError()
			}
			return 0, deps.publishErr
		}
		deps.messages = append(deps.messages, text)
		if deps.afterSend != nil {
			deps.afterSend()
		}
		return len(deps.messages), nil
	}}
	return deps
}
