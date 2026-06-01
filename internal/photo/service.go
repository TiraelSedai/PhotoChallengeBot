package photo

import (
	"context"
	"time"

	"github.com/TiraelSedai/PhotoChallengeBot/internal/hashtag"
	"github.com/TiraelSedai/PhotoChallengeBot/internal/repository"
	"github.com/TiraelSedai/PhotoChallengeBot/internal/require"
	"github.com/go-telegram/bot/models"
)

const (
	firstPhotoAcceptedMessage = "Фотка класс! Спасибо, принято."
	photoReplacedMessage      = "Фотка класс! Спасибо, принято. Старую конкурсную фотографию заменил на новую."
)

type challenges interface {
	FindOpenByMainChatID(context.Context, int64) (*repository.Challenge, error)
}

type users interface {
	Upsert(context.Context, repository.User) (repository.User, error)
}

type photos interface {
	UpsertCurrent(context.Context, repository.UpsertPhotoInput) (repository.Photo, bool, error)
}

type publisher interface {
	SendTextReply(context.Context, int64, string, int) (int, error)
}

type Service struct {
	mainChatID int64
	challenges challenges
	users      users
	photos     photos
	publisher  publisher
	now        func() time.Time
}

type Config struct {
	MainChatID int64
	Challenges challenges
	Users      users
	Photos     photos
	Publisher  publisher
	Now        func() time.Time
}

func NewService(cfg Config) *Service {
	require.NotNil("challenge repository", cfg.Challenges)
	require.NotNil("user repository", cfg.Users)
	require.NotNil("photo repository", cfg.Photos)
	require.NotNil("photo publisher", cfg.Publisher)
	require.NotNil("clock", cfg.Now)
	switch {
	case cfg.MainChatID == 0:
		panic("main chat id is required")
	}
	return &Service{
		mainChatID: cfg.MainChatID,
		challenges: cfg.Challenges,
		users:      cfg.Users,
		photos:     cfg.Photos,
		publisher:  cfg.Publisher,
		now:        cfg.Now,
	}
}

func (s *Service) HandleMainChatMessage(ctx context.Context, message *models.Message) error {
	if message == nil || message.Chat.ID != s.mainChatID {
		return nil
	}
	if message.From == nil || message.From.ID == 0 {
		return nil
	}
	if len(message.Photo) == 0 {
		return nil
	}
	if message.ForwardOrigin != nil || message.IsAutomaticForward {
		return nil
	}

	challenge, err := s.challenges.FindOpenByMainChatID(ctx, s.mainChatID)
	if err != nil {
		return err
	}
	if challenge == nil || challenge.State != repository.ChallengeStateActive {
		return nil
	}

	now := s.now()
	if now.Before(challenge.AcceptStartAt) || now.After(challenge.AcceptUntilAt) {
		return nil
	}
	if !hashtag.Contains(messageText(message), challenge.Hashtag) {
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

	bestPhoto := message.Photo[len(message.Photo)-1]
	_, replaced, err := s.photos.UpsertCurrent(ctx, repository.UpsertPhotoInput{
		ChallengeID:     challenge.ID,
		AuthorUserID:    author.ID,
		FileID:          bestPhoto.FileID,
		FileUniqueID:    bestPhoto.FileUniqueID,
		SourceChatID:    message.Chat.ID,
		SourceMessageID: message.ID,
		Caption:         message.Caption,
		SubmittedAt:     now,
	})
	if err != nil {
		return err
	}

	reply := firstPhotoAcceptedMessage
	if replaced {
		reply = photoReplacedMessage
	}
	if _, err := s.publisher.SendTextReply(ctx, s.mainChatID, reply, message.ID); err != nil {
		return err
	}
	return nil
}

func messageText(message *models.Message) string {
	if message == nil {
		return ""
	}
	if message.Caption != "" {
		return message.Caption
	}
	return message.Text
}
