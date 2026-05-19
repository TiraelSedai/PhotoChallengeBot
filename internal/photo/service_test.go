package photo

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/TiraelSedai/PhotoChallengeBot/internal/repository"
	"github.com/go-telegram/bot/models"
)

func TestServiceAcceptsFirstPhotoWithChallengeHashtag(t *testing.T) {
	now := time.Date(2026, 5, 18, 12, 0, 0, 0, time.UTC)
	store := newPhotoTestStore(activeChallenge(now, "#вода"))
	publisher := &recordingPublisher{}
	service := newTestService(store, publisher, now)

	err := service.HandleMainChatMessage(context.Background(), photoMessage("снято сегодня #вода"))
	if err != nil {
		t.Fatalf("HandleMainChatMessage() error = %v", err)
	}

	if len(store.photos) != 1 {
		t.Fatalf("photos length = %d, want 1", len(store.photos))
	}
	got := store.photos[0]
	if got.ChallengeID != 42 || got.AuthorUserID != 11 || got.FileID != "large-file" || got.Caption != "снято сегодня #вода" {
		t.Fatalf("stored photo = %#v", got)
	}
	if got.SubmittedAt != now {
		t.Fatalf("SubmittedAt = %s, want %s", got.SubmittedAt, now)
	}

	wantMessages := []string{firstPhotoAcceptedMessage}
	if !reflect.DeepEqual(publisher.messages, wantMessages) {
		t.Fatalf("messages = %v, want %v", publisher.messages, wantMessages)
	}
}

func TestServiceReplacesExistingPhoto(t *testing.T) {
	now := time.Date(2026, 5, 18, 12, 0, 0, 0, time.UTC)
	store := newPhotoTestStore(activeChallenge(now, "#tag"))
	publisher := &recordingPublisher{}
	service := newTestService(store, publisher, now)

	if err := service.HandleMainChatMessage(context.Background(), photoMessage("#tag first")); err != nil {
		t.Fatalf("first HandleMainChatMessage() error = %v", err)
	}

	second := photoMessage("#tag second")
	second.ID = 101
	second.Photo = []models.PhotoSize{{FileID: "new-file", FileUniqueID: "new-unique"}}
	if err := service.HandleMainChatMessage(context.Background(), second); err != nil {
		t.Fatalf("second HandleMainChatMessage() error = %v", err)
	}

	if len(store.photos) != 1 {
		t.Fatalf("photos length = %d, want 1", len(store.photos))
	}
	got := store.photos[0]
	if got.FileID != "new-file" || got.SourceMessageID != 101 || got.Caption != "#tag second" {
		t.Fatalf("stored replacement = %#v", got)
	}

	wantMessages := []string{firstPhotoAcceptedMessage, photoReplacedMessage}
	if !reflect.DeepEqual(publisher.messages, wantMessages) {
		t.Fatalf("messages = %v, want %v", publisher.messages, wantMessages)
	}
}

func TestServiceIgnoresPhotoWithoutMatchingHashtag(t *testing.T) {
	now := time.Date(2026, 5, 18, 12, 0, 0, 0, time.UTC)
	store := newPhotoTestStore(activeChallenge(now, "#tag"))
	publisher := &recordingPublisher{}
	service := newTestService(store, publisher, now)

	if err := service.HandleMainChatMessage(context.Background(), photoMessage("#tagged is not the same tag")); err != nil {
		t.Fatalf("HandleMainChatMessage() error = %v", err)
	}

	if len(store.photos) != 0 {
		t.Fatalf("photos length = %d, want 0", len(store.photos))
	}
	if len(publisher.messages) != 0 {
		t.Fatalf("messages = %v, want none", publisher.messages)
	}
}

func TestServiceIgnoresClosedOrVotingChallenge(t *testing.T) {
	now := time.Date(2026, 5, 18, 19, 0, 0, 0, time.UTC)
	closed := activeChallenge(now, "#tag")
	closed.AcceptUntilAt = now.Add(-time.Minute)
	store := newPhotoTestStore(closed)
	publisher := &recordingPublisher{}
	service := newTestService(store, publisher, now)

	if err := service.HandleMainChatMessage(context.Background(), photoMessage("#tag")); err != nil {
		t.Fatalf("HandleMainChatMessage() error = %v", err)
	}
	if len(store.photos) != 0 {
		t.Fatalf("photos length = %d, want 0 after accept window", len(store.photos))
	}

	voting := activeChallenge(now.Add(-time.Hour), "#tag")
	voting.State = repository.ChallengeStateVoting
	voting.AcceptUntilAt = now.Add(time.Hour)
	store.challenge = &voting
	if err := service.HandleMainChatMessage(context.Background(), photoMessage("#tag")); err != nil {
		t.Fatalf("HandleMainChatMessage() voting error = %v", err)
	}
	if len(store.photos) != 0 {
		t.Fatalf("photos length = %d, want 0 during voting", len(store.photos))
	}
}

func newTestService(store *photoTestStore, publisher *recordingPublisher, now time.Time) *Service {
	return NewService(Config{
		MainChatID: 1001,
		Challenges: store,
		Users:      store,
		Photos:     store,
		Publisher:  publisher,
		Now: func() time.Time {
			return now
		},
	})
}

func activeChallenge(now time.Time, hashtag string) repository.Challenge {
	return repository.Challenge{
		ID:            42,
		MainChatID:    1001,
		State:         repository.ChallengeStateActive,
		Hashtag:       hashtag,
		AcceptStartAt: now.Add(-24 * time.Hour),
		AcceptUntilAt: now.Add(time.Hour),
	}
}

func photoMessage(caption string) *models.Message {
	return &models.Message{
		ID:   100,
		Chat: models.Chat{ID: 1001, Type: models.ChatTypeSupergroup},
		From: &models.User{ID: 11, Username: "author", FirstName: "Photo", LastName: "Author"},
		Photo: []models.PhotoSize{
			{FileID: "small-file", FileUniqueID: "small-unique"},
			{FileID: "large-file", FileUniqueID: "large-unique"},
		},
		Caption: caption,
	}
}

type photoTestStore struct {
	challenge *repository.Challenge
	users     map[int64]repository.User
	photos    []repository.Photo
}

func newPhotoTestStore(challenge repository.Challenge) *photoTestStore {
	return &photoTestStore{
		challenge: &challenge,
		users:     make(map[int64]repository.User),
	}
}

func (s *photoTestStore) FindOpenByMainChatID(_ context.Context, mainChatID int64) (*repository.Challenge, error) {
	if s.challenge == nil || s.challenge.MainChatID != mainChatID {
		return nil, nil
	}
	return s.challenge, nil
}

func (s *photoTestStore) Upsert(_ context.Context, user repository.User) (repository.User, error) {
	s.users[user.ID] = user
	return user, nil
}

func (s *photoTestStore) UpsertCurrent(_ context.Context, input repository.UpsertPhotoInput) (repository.Photo, bool, error) {
	photo := repository.Photo{
		ID:              1,
		ChallengeID:     input.ChallengeID,
		AuthorUserID:    input.AuthorUserID,
		FileID:          input.FileID,
		FileUniqueID:    input.FileUniqueID,
		SourceChatID:    input.SourceChatID,
		SourceMessageID: input.SourceMessageID,
		Caption:         input.Caption,
		SubmittedAt:     input.SubmittedAt,
		UpdatedAt:       input.SubmittedAt,
	}
	for idx, existing := range s.photos {
		if existing.ChallengeID == input.ChallengeID && existing.AuthorUserID == input.AuthorUserID {
			photo.ID = existing.ID
			s.photos[idx] = photo
			return photo, true, nil
		}
	}
	s.photos = append(s.photos, photo)
	return photo, false, nil
}

type recordingPublisher struct {
	messages []string
}

func (p *recordingPublisher) SendText(_ context.Context, _ int64, text string) (int, error) {
	p.messages = append(p.messages, text)
	return len(p.messages), nil
}
