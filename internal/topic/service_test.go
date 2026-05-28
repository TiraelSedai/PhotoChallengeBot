package topic

import (
	"context"
	"testing"
	"time"

	"github.com/TiraelSedai/PhotoChallengeBot/internal/repository"
	"github.com/go-telegram/bot/models"
)

func TestServiceStoresThemeSuggestionDuringVoting(t *testing.T) {
	now := time.Date(2026, 5, 20, 12, 0, 0, 0, time.UTC)
	store := newTopicTestStore(votingChallenge(now))
	service := newTestService(store, now)

	err := service.HandleMainChatMessage(context.Background(), topicMessage("предлагаю туман #тема"))
	if err != nil {
		t.Fatalf("HandleMainChatMessage() error = %v", err)
	}

	if len(store.suggestions) != 1 {
		t.Fatalf("suggestions length = %d, want 1", len(store.suggestions))
	}
	got := store.suggestions[0]
	if got.ChallengeID != 42 || got.AuthorUserID != 11 || got.SourceChatID != 1001 || got.SourceMessageID != 100 {
		t.Fatalf("suggestion = %#v, want message source and author", got)
	}
	if got.Text != "предлагаю туман #тема" {
		t.Fatalf("Text = %q, want original message text", got.Text)
	}
	if got.SuggestedAt != now {
		t.Fatalf("SuggestedAt = %s, want %s", got.SuggestedAt, now)
	}
	if _, ok := store.users[11]; !ok {
		t.Fatal("author was not upserted")
	}
}

func TestServiceStoresThemeSuggestionFromCaption(t *testing.T) {
	now := time.Date(2026, 5, 20, 12, 0, 0, 0, time.UTC)
	store := newTopicTestStore(votingChallenge(now))
	service := newTestService(store, now)
	message := topicMessage("")
	message.Caption = "архитектурные детали #тема"

	if err := service.HandleMainChatMessage(context.Background(), message); err != nil {
		t.Fatalf("HandleMainChatMessage() error = %v", err)
	}
	if len(store.suggestions) != 1 || store.suggestions[0].Text != "архитектурные детали #тема" {
		t.Fatalf("suggestions = %#v, want caption stored", store.suggestions)
	}
}

func TestServiceIgnoresSuggestionOutsideVoting(t *testing.T) {
	now := time.Date(2026, 5, 20, 12, 0, 0, 0, time.UTC)
	challenge := votingChallenge(now)
	challenge.State = repository.ChallengeStateActive
	store := newTopicTestStore(challenge)
	service := newTestService(store, now)

	if err := service.HandleMainChatMessage(context.Background(), topicMessage("туман #тема")); err != nil {
		t.Fatalf("HandleMainChatMessage() error = %v", err)
	}
	if len(store.suggestions) != 0 {
		t.Fatalf("suggestions length = %d, want 0 outside voting", len(store.suggestions))
	}
}

func TestServiceIgnoresSuggestionAfterVoteDeadline(t *testing.T) {
	now := time.Date(2026, 5, 20, 12, 0, 0, 0, time.UTC)
	challenge := votingChallenge(now)
	deadline := now
	challenge.VoteUntilAt = &deadline
	store := newTopicTestStore(challenge)
	service := newTestService(store, now)

	if err := service.HandleMainChatMessage(context.Background(), topicMessage("туман #тема")); err != nil {
		t.Fatalf("HandleMainChatMessage() error = %v", err)
	}
	if len(store.suggestions) != 0 {
		t.Fatalf("suggestions length = %d, want 0 after vote deadline", len(store.suggestions))
	}
}

func TestServiceIgnoresTextWithoutThemeHashtag(t *testing.T) {
	now := time.Date(2026, 5, 20, 12, 0, 0, 0, time.UTC)
	store := newTopicTestStore(votingChallenge(now))
	service := newTestService(store, now)

	for _, text := range []string{"просто сообщение", "#тематика", "#тема_2026"} {
		if err := service.HandleMainChatMessage(context.Background(), topicMessage(text)); err != nil {
			t.Fatalf("HandleMainChatMessage(%q) error = %v", text, err)
		}
	}
	if len(store.suggestions) != 0 {
		t.Fatalf("suggestions length = %d, want 0 without exact tag", len(store.suggestions))
	}
}

func newTestService(store *topicTestStore, now time.Time) *Service {
	return NewService(Config{
		MainChatID:  1001,
		Challenges:  store,
		Users:       store,
		Suggestions: store,
		Now: func() time.Time {
			return now
		},
	})
}

func votingChallenge(now time.Time) repository.Challenge {
	voteUntilAt := now.Add(time.Hour)
	return repository.Challenge{
		ID:            42,
		MainChatID:    1001,
		State:         repository.ChallengeStateVoting,
		AcceptStartAt: now.Add(-49 * time.Hour),
		AcceptUntilAt: now.Add(-time.Hour),
		VoteUntilAt:   &voteUntilAt,
	}
}

func topicMessage(text string) *models.Message {
	return &models.Message{
		ID:   100,
		Chat: models.Chat{ID: 1001, Type: models.ChatTypeSupergroup},
		From: &models.User{ID: 11, Username: "author", FirstName: "Topic", LastName: "Author"},
		Text: text,
	}
}

type topicTestStore struct {
	challenge   *repository.Challenge
	users       map[int64]repository.User
	suggestions []repository.CreateTopicSuggestionInput
}

func newTopicTestStore(challenge repository.Challenge) *topicTestStore {
	return &topicTestStore{
		challenge: &challenge,
		users:     make(map[int64]repository.User),
	}
}

func (s *topicTestStore) FindOpenByMainChatID(_ context.Context, mainChatID int64) (*repository.Challenge, error) {
	if s.challenge == nil || s.challenge.MainChatID != mainChatID {
		return nil, nil
	}
	return s.challenge, nil
}

func (s *topicTestStore) Upsert(_ context.Context, user repository.User) (repository.User, error) {
	s.users[user.ID] = user
	return user, nil
}

func (s *topicTestStore) Create(_ context.Context, input repository.CreateTopicSuggestionInput) (repository.TopicSuggestion, bool, error) {
	s.suggestions = append(s.suggestions, input)
	return repository.TopicSuggestion{ID: int64(len(s.suggestions))}, true, nil
}
