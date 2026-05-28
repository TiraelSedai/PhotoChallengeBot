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
	deps := newTopicServiceDeps(votingChallenge(now))
	service := newTestService(deps, now)

	err := service.HandleMainChatMessage(context.Background(), topicMessage("предлагаю туман #тема"))
	if err != nil {
		t.Fatalf("HandleMainChatMessage() error = %v", err)
	}

	if len(deps.suggestions) != 1 {
		t.Fatalf("suggestions length = %d, want 1", len(deps.suggestions))
	}
	got := deps.suggestions[0]
	if got.ChallengeID != 42 || got.AuthorUserID != 11 || got.SourceChatID != 1001 || got.SourceMessageID != 100 {
		t.Fatalf("suggestion = %#v, want message source and author", got)
	}
	if got.Text != "предлагаю туман #тема" {
		t.Fatalf("Text = %q, want original message text", got.Text)
	}
	if got.SuggestedAt != now {
		t.Fatalf("SuggestedAt = %s, want %s", got.SuggestedAt, now)
	}
	if _, ok := deps.usersByID[11]; !ok {
		t.Fatal("author was not upserted")
	}
}

func TestNewServicePanicsOnNilClock(t *testing.T) {
	deps := newTopicServiceDeps(votingChallenge(time.Date(2026, 5, 20, 12, 0, 0, 0, time.UTC)))
	defer func() {
		if recover() == nil {
			t.Fatal("NewService() did not panic")
		}
	}()
	NewService(Config{
		MainChatID:  1001,
		Challenges:  deps.challenges,
		Users:       deps.users,
		Suggestions: deps.suggestionsStore,
	})
}

func TestServiceStoresThemeSuggestionFromCaption(t *testing.T) {
	now := time.Date(2026, 5, 20, 12, 0, 0, 0, time.UTC)
	deps := newTopicServiceDeps(votingChallenge(now))
	service := newTestService(deps, now)
	message := topicMessage("")
	message.Caption = "архитектурные детали #тема"

	if err := service.HandleMainChatMessage(context.Background(), message); err != nil {
		t.Fatalf("HandleMainChatMessage() error = %v", err)
	}
	if len(deps.suggestions) != 1 || deps.suggestions[0].Text != "архитектурные детали #тема" {
		t.Fatalf("suggestions = %#v, want caption stored", deps.suggestions)
	}
}

func TestServiceIgnoresSuggestionOutsideVoting(t *testing.T) {
	now := time.Date(2026, 5, 20, 12, 0, 0, 0, time.UTC)
	challenge := votingChallenge(now)
	challenge.State = repository.ChallengeStateActive
	deps := newTopicServiceDeps(challenge)
	service := newTestService(deps, now)

	if err := service.HandleMainChatMessage(context.Background(), topicMessage("туман #тема")); err != nil {
		t.Fatalf("HandleMainChatMessage() error = %v", err)
	}
	if len(deps.suggestions) != 0 {
		t.Fatalf("suggestions length = %d, want 0 outside voting", len(deps.suggestions))
	}
}

func TestServiceIgnoresSuggestionAfterVoteDeadline(t *testing.T) {
	now := time.Date(2026, 5, 20, 12, 0, 0, 0, time.UTC)
	challenge := votingChallenge(now)
	deadline := now
	challenge.VoteUntilAt = &deadline
	deps := newTopicServiceDeps(challenge)
	service := newTestService(deps, now)

	if err := service.HandleMainChatMessage(context.Background(), topicMessage("туман #тема")); err != nil {
		t.Fatalf("HandleMainChatMessage() error = %v", err)
	}
	if len(deps.suggestions) != 0 {
		t.Fatalf("suggestions length = %d, want 0 after vote deadline", len(deps.suggestions))
	}
}

func TestServiceIgnoresTextWithoutThemeHashtag(t *testing.T) {
	now := time.Date(2026, 5, 20, 12, 0, 0, 0, time.UTC)
	deps := newTopicServiceDeps(votingChallenge(now))
	service := newTestService(deps, now)

	for _, text := range []string{"просто сообщение", "#тематика", "#тема_2026"} {
		if err := service.HandleMainChatMessage(context.Background(), topicMessage(text)); err != nil {
			t.Fatalf("HandleMainChatMessage(%q) error = %v", text, err)
		}
	}
	if len(deps.suggestions) != 0 {
		t.Fatalf("suggestions length = %d, want 0 without exact tag", len(deps.suggestions))
	}
}

func newTestService(deps *topicServiceDeps, now time.Time) *Service {
	return NewService(Config{
		MainChatID:  1001,
		Challenges:  deps.challenges,
		Users:       deps.users,
		Suggestions: deps.suggestionsStore,
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

type topicServiceDeps struct {
	challenge        *repository.Challenge
	usersByID        map[int64]repository.User
	suggestions      []repository.CreateTopicSuggestionInput
	challenges       *MoqChallenges
	users            *MoqUsers
	suggestionsStore *MoqSuggestions
}

func newTopicServiceDeps(challenge repository.Challenge) *topicServiceDeps {
	deps := &topicServiceDeps{
		challenge: &challenge,
		usersByID: make(map[int64]repository.User),
	}
	deps.challenges = &MoqChallenges{FindOpenByMainChatIDFunc: func(_ context.Context, mainChatID int64) (*repository.Challenge, error) {
		if deps.challenge == nil || deps.challenge.MainChatID != mainChatID {
			return nil, nil
		}
		return deps.challenge, nil
	}}
	deps.users = &MoqUsers{UpsertFunc: func(_ context.Context, user repository.User) (repository.User, error) {
		deps.usersByID[user.ID] = user
		return user, nil
	}}
	deps.suggestionsStore = &MoqSuggestions{CreateFunc: func(_ context.Context, input repository.CreateTopicSuggestionInput) (repository.TopicSuggestion, bool, error) {
		deps.suggestions = append(deps.suggestions, input)
		return repository.TopicSuggestion{ID: int64(len(deps.suggestions))}, true, nil
	}}
	return deps
}
