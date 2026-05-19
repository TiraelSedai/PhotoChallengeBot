package photo

import (
	"context"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/TiraelSedai/PhotoChallengeBot/internal/repository"
	"github.com/go-telegram/bot/models"
)

const (
	firstPhotoAcceptedMessage = "Фотка класс! Спасибо, принято."
	photoReplacedMessage      = "Фотка класс! Спасибо, принято. Старую конкурсную фотографию заменил на новую."
)

type Challenges interface {
	FindOpenByMainChatID(context.Context, int64) (*repository.Challenge, error)
}

type Users interface {
	Upsert(context.Context, repository.User) (repository.User, error)
}

type Photos interface {
	UpsertCurrent(context.Context, repository.UpsertPhotoInput) (repository.Photo, bool, error)
}

type Publisher interface {
	SendText(context.Context, int64, string) (int, error)
}

type Service struct {
	mainChatID int64
	challenges Challenges
	users      Users
	photos     Photos
	publisher  Publisher
	now        func() time.Time
}

type Config struct {
	MainChatID int64
	Challenges Challenges
	Users      Users
	Photos     Photos
	Publisher  Publisher
	Now        func() time.Time
}

func NewService(cfg Config) *Service {
	now := cfg.Now
	if now == nil {
		now = time.Now
	}
	switch {
	case cfg.MainChatID == 0:
		panic("main chat id is required")
	case cfg.Challenges == nil:
		panic("challenge repository is nil")
	case cfg.Users == nil:
		panic("user repository is nil")
	case cfg.Photos == nil:
		panic("photo repository is nil")
	case cfg.Publisher == nil:
		panic("photo publisher is nil")
	}
	return &Service{
		mainChatID: cfg.MainChatID,
		challenges: cfg.Challenges,
		users:      cfg.Users,
		photos:     cfg.Photos,
		publisher:  cfg.Publisher,
		now:        now,
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
	if !hasHashtag(messageText(message), challenge.Hashtag) {
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
	if _, err := s.publisher.SendText(ctx, s.mainChatID, reply); err != nil {
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

func hasHashtag(text string, hashtag string) bool {
	hashtag = strings.ToLower(strings.TrimSpace(hashtag))
	if hashtag == "" || !strings.HasPrefix(hashtag, "#") {
		return false
	}

	lower := strings.ToLower(text)
	for offset := 0; offset < len(lower); {
		idx := strings.Index(lower[offset:], hashtag)
		if idx < 0 {
			return false
		}
		start := offset + idx
		end := start + len(hashtag)
		if isTagBoundaryBefore(lower, start) && isTagBoundaryAfter(lower, end) {
			return true
		}
		offset = end
	}
	return false
}

func isTagBoundaryBefore(value string, idx int) bool {
	if idx == 0 {
		return true
	}
	r, _ := utf8.DecodeLastRuneInString(value[:idx])
	return !isHashtagRune(r)
}

func isTagBoundaryAfter(value string, idx int) bool {
	if idx >= len(value) {
		return true
	}
	r, _ := utf8.DecodeRuneInString(value[idx:])
	return !isHashtagRune(r)
}

func isHashtagRune(r rune) bool {
	return r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r)
}
