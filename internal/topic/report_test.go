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
	store := newTopicReportStore(challenge)
	store.suggestions = []repository.TopicSuggestion{
		{ChallengeID: challenge.ID, AuthorUserID: 11, Text: "Туман над рекой #тема"},
		{ChallengeID: challenge.ID, AuthorUserID: 12, Text: "Фонари после дождя #тема"},
	}
	store.users[11] = repository.User{ID: 11, Username: "alice", DisplayName: "Alice"}
	store.users[12] = repository.User{ID: 12, DisplayName: "Bob Example"}
	publisher := &recordingTopicPublisher{}
	reporter := newTestReporter(store, publisher, now)

	if err := reporter.PublishOne(context.Background(), challenge); err != nil {
		t.Fatalf("PublishOne() error = %v", err)
	}

	if len(publisher.messages) != 1 {
		t.Fatalf("messages length = %d, want 1", len(publisher.messages))
	}
	text := publisher.messages[0]
	for _, want := range []string{
		"Темы, предложенные во время голосования за челлендж #0",
		"1. @alice: Туман над рекой #тема",
		"2. Bob Example: Фонари после дождя #тема",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("message = %q, want to contain %q", text, want)
		}
	}
	if store.sentChallengeID != challenge.ID {
		t.Fatalf("sentChallengeID = %d, want %d", store.sentChallengeID, challenge.ID)
	}
}

func TestNewReporterPanicsOnNilClock(t *testing.T) {
	store := newTopicReportStore(repository.Challenge{})
	defer func() {
		if recover() == nil {
			t.Fatal("NewReporter() did not panic")
		}
	}()
	NewReporter(ReportConfig{
		AdminChatID: 2002,
		Challenges:  store,
		Suggestions: store,
		Users:       store,
		Publisher:   &recordingTopicPublisher{},
	})
}

func TestReporterPublishesEmptySuggestionReport(t *testing.T) {
	now := time.Date(2026, 5, 22, 12, 0, 0, 0, time.UTC)
	challenge := votingChallenge(now)
	challenge.State = repository.ChallengeStateFinished
	store := newTopicReportStore(challenge)
	publisher := &recordingTopicPublisher{}
	reporter := newTestReporter(store, publisher, now)

	if err := reporter.PublishOne(context.Background(), challenge); err != nil {
		t.Fatalf("PublishOne() error = %v", err)
	}

	want := []string{"Темы, предложенные во время голосования за челлендж #0:\n\nТем за время голосования не предложили."}
	if !reflect.DeepEqual(publisher.messages, want) {
		t.Fatalf("messages = %v, want %v", publisher.messages, want)
	}
	if store.sentChallengeID != challenge.ID {
		t.Fatalf("sentChallengeID = %d, want %d", store.sentChallengeID, challenge.ID)
	}
}

func TestReporterSplitsLongSuggestionReport(t *testing.T) {
	now := time.Date(2026, 5, 22, 12, 0, 0, 0, time.UTC)
	challenge := votingChallenge(now)
	challenge.State = repository.ChallengeStateFinished
	store := newTopicReportStore(challenge)
	store.users[11] = repository.User{ID: 11, Username: "alice", DisplayName: "Alice"}
	for i := 0; i < 12; i++ {
		store.suggestions = append(store.suggestions, repository.TopicSuggestion{
			ChallengeID:  challenge.ID,
			AuthorUserID: 11,
			Text:         strings.Repeat("очень длинная тема ", 25) + "#тема",
			SuggestedAt:  now.Add(time.Duration(i) * time.Minute),
		})
	}
	publisher := &recordingTopicPublisher{}
	reporter := newTestReporter(store, publisher, now)

	if err := reporter.PublishOne(context.Background(), challenge); err != nil {
		t.Fatalf("PublishOne() error = %v", err)
	}

	if len(publisher.messages) < 2 {
		t.Fatalf("messages length = %d, want split report", len(publisher.messages))
	}
	for idx, message := range publisher.messages {
		if len(message) > maxReportMessageLength {
			t.Fatalf("message %d length = %d, want <= %d", idx, len(message), maxReportMessageLength)
		}
	}
	if !strings.Contains(publisher.messages[1], "продолжение") {
		t.Fatalf("second message = %q, want continuation header", publisher.messages[1])
	}
	if store.sentChallengeID != challenge.ID {
		t.Fatalf("sentChallengeID = %d, want %d", store.sentChallengeID, challenge.ID)
	}
}

func TestReporterMarksSentAfterParentCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	now := time.Date(2026, 5, 22, 12, 0, 0, 0, time.UTC)
	challenge := votingChallenge(now)
	challenge.State = repository.ChallengeStateFinished
	store := newTopicReportStore(challenge)
	store.failCanceledPersistence = true
	publisher := &recordingTopicPublisher{afterSend: cancel}
	reporter := newTestReporter(store, publisher, now)

	if err := reporter.PublishOne(ctx, challenge); err != nil {
		t.Fatalf("PublishOne() error = %v", err)
	}
	if store.sentChallengeID != challenge.ID {
		t.Fatalf("sentChallengeID = %d, want %d", store.sentChallengeID, challenge.ID)
	}
}

func TestReporterReleasesClaimWhenSendFails(t *testing.T) {
	now := time.Date(2026, 5, 22, 12, 0, 0, 0, time.UTC)
	challenge := votingChallenge(now)
	challenge.State = repository.ChallengeStateFinished
	store := newTopicReportStore(challenge)
	wantErr := errors.New("telegram failed")
	publisher := &recordingTopicPublisher{err: wantErr}
	reporter := newTestReporter(store, publisher, now)

	err := reporter.PublishOne(context.Background(), challenge)
	if !errors.Is(err, wantErr) {
		t.Fatalf("PublishOne() error = %v, want %v", err, wantErr)
	}
	if store.releasedChallengeID != challenge.ID {
		t.Fatalf("releasedChallengeID = %d, want %d", store.releasedChallengeID, challenge.ID)
	}
	if store.sentChallengeID != 0 {
		t.Fatalf("sentChallengeID = %d, want 0 after send failure", store.sentChallengeID)
	}
}

func TestReporterReleasesClaimAfterParentCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	now := time.Date(2026, 5, 22, 12, 0, 0, 0, time.UTC)
	challenge := votingChallenge(now)
	challenge.State = repository.ChallengeStateFinished
	store := newTopicReportStore(challenge)
	store.failCanceledPersistence = true
	wantErr := errors.New("telegram failed")
	publisher := &recordingTopicPublisher{err: wantErr, beforeError: cancel}
	reporter := newTestReporter(store, publisher, now)

	err := reporter.PublishOne(ctx, challenge)
	if !errors.Is(err, wantErr) {
		t.Fatalf("PublishOne() error = %v, want %v", err, wantErr)
	}
	if store.releasedChallengeID != challenge.ID {
		t.Fatalf("releasedChallengeID = %d, want %d", store.releasedChallengeID, challenge.ID)
	}
}

func newTestReporter(store *topicReportStore, publisher *recordingTopicPublisher, now time.Time) *Reporter {
	return NewReporter(ReportConfig{
		AdminChatID: 2002,
		Challenges:  store,
		Suggestions: store,
		Users:       store,
		Publisher:   publisher,
		Now: func() time.Time {
			return now
		},
	})
}

type topicReportStore struct {
	challenge               repository.Challenge
	users                   map[int64]repository.User
	suggestions             []repository.TopicSuggestion
	claimedChallengeID      int64
	sentChallengeID         int64
	releasedChallengeID     int64
	failCanceledPersistence bool
}

func newTopicReportStore(challenge repository.Challenge) *topicReportStore {
	return &topicReportStore{
		challenge: challenge,
		users:     make(map[int64]repository.User),
	}
}

func (s *topicReportStore) ListUnsentTopicReports(_ context.Context, _ int64, _ int) ([]repository.Challenge, error) {
	return []repository.Challenge{s.challenge}, nil
}

func (s *topicReportStore) ClaimTopicReport(_ context.Context, id int64, _ time.Time) (bool, error) {
	s.claimedChallengeID = id
	return true, nil
}

func (s *topicReportStore) MarkTopicReportSent(ctx context.Context, id int64, _, _ time.Time) (bool, error) {
	if s.failCanceledPersistence && ctx.Err() != nil {
		return false, ctx.Err()
	}
	s.sentChallengeID = id
	return true, nil
}

func (s *topicReportStore) ReleaseTopicReportClaim(ctx context.Context, id int64, _ time.Time) error {
	if s.failCanceledPersistence && ctx.Err() != nil {
		return ctx.Err()
	}
	s.releasedChallengeID = id
	return nil
}

func (s *topicReportStore) ListByChallenge(_ context.Context, _ int64) ([]repository.TopicSuggestion, error) {
	return s.suggestions, nil
}

func (s *topicReportStore) Get(_ context.Context, id int64) (repository.User, error) {
	user, ok := s.users[id]
	if !ok {
		return repository.User{ID: id, DisplayName: "Unknown"}, nil
	}
	return user, nil
}

type recordingTopicPublisher struct {
	messages    []string
	err         error
	afterSend   func()
	beforeError context.CancelFunc
}

func (p *recordingTopicPublisher) SendText(_ context.Context, _ int64, text string) (int, error) {
	if p.err != nil {
		if p.beforeError != nil {
			p.beforeError()
		}
		return 0, p.err
	}
	p.messages = append(p.messages, text)
	if p.afterSend != nil {
		p.afterSend()
	}
	return len(p.messages), nil
}
