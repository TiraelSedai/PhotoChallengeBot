package topic

import (
	"context"
	"time"

	"github.com/TiraelSedai/PhotoChallengeBot/internal/hashtag"
	"github.com/TiraelSedai/PhotoChallengeBot/internal/repository"
	"github.com/go-telegram/bot/models"
)

const themeSuggestionHashtag = "#тема"

type Challenges interface {
	FindOpenByMainChatID(context.Context, int64) (*repository.Challenge, error)
}

type Users interface {
	Upsert(context.Context, repository.User) (repository.User, error)
}

type Suggestions interface {
	Create(context.Context, repository.CreateTopicSuggestionInput) (repository.TopicSuggestion, bool, error)
}

type Config struct {
	MainChatID  int64
	Challenges  Challenges
	Users       Users
	Suggestions Suggestions
	Now         func() time.Time
}

type Service struct {
	mainChatID  int64
	challenges  Challenges
	users       Users
	suggestions Suggestions
	now         func() time.Time
}

func NewService(cfg Config) *Service {
	now := cfg.Now
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	switch {
	case cfg.MainChatID == 0:
		panic("main chat id is required")
	case cfg.Challenges == nil:
		panic("topic challenge repository is nil")
	case cfg.Users == nil:
		panic("topic user repository is nil")
	case cfg.Suggestions == nil:
		panic("topic suggestion repository is nil")
	}
	return &Service{
		mainChatID:  cfg.MainChatID,
		challenges:  cfg.Challenges,
		users:       cfg.Users,
		suggestions: cfg.Suggestions,
		now:         now,
	}
}

func (s *Service) HandleMainChatMessage(ctx context.Context, message *models.Message) error {
	if message == nil || message.Chat.ID != s.mainChatID {
		return nil
	}
	if message.From == nil || message.From.ID == 0 {
		return nil
	}

	text := message.Text
	if message.Caption != "" {
		text = message.Caption
	}
	if !hashtag.Contains(text, themeSuggestionHashtag) {
		return nil
	}

	challenge, err := s.challenges.FindOpenByMainChatID(ctx, s.mainChatID)
	if err != nil {
		return err
	}
	if challenge == nil || challenge.State != repository.ChallengeStateVoting {
		return nil
	}

	now := s.now()
	if challenge.VoteUntilAt == nil || !now.Before(*challenge.VoteUntilAt) {
		return nil
	}
	author, err := s.users.Upsert(ctx, repository.User{
		ID:        message.From.ID,
		Username:  message.From.Username,
		FirstName: message.From.FirstName,
		LastName:  message.From.LastName,
		UpdatedAt: now,
	})
	if err != nil {
		return err
	}

	_, _, err = s.suggestions.Create(ctx, repository.CreateTopicSuggestionInput{
		ChallengeID:      challenge.ID,
		AuthorUserID:     author.ID,
		SourceChatID:     message.Chat.ID,
		SourceMessageID:  message.ID,
		Text:             text,
		SuggestedAt:      now,
		CreatedUpdatedAt: now,
	})
	return err
}
