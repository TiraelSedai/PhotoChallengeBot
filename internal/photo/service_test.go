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
	deps := newPhotoServiceDeps(activeChallenge(now, "#вода"))
	service := newTestService(deps, now)

	err := service.HandleMainChatMessage(context.Background(), photoMessage("снято сегодня #вода"))
	if err != nil {
		t.Fatalf("HandleMainChatMessage() error = %v", err)
	}

	if len(deps.photos) != 1 {
		t.Fatalf("photos length = %d, want 1", len(deps.photos))
	}
	got := deps.photos[0]
	if got.ChallengeID != 42 || got.AuthorUserID != 11 || got.FileID != "large-file" || got.Caption != "снято сегодня #вода" {
		t.Fatalf("stored photo = %#v", got)
	}
	if got.SubmittedAt != now {
		t.Fatalf("SubmittedAt = %s, want %s", got.SubmittedAt, now)
	}

	wantMessages := []string{firstPhotoAcceptedMessage}
	if !reflect.DeepEqual(deps.messages, wantMessages) {
		t.Fatalf("messages = %v, want %v", deps.messages, wantMessages)
	}
}

func TestNewServicePanicsOnNilClock(t *testing.T) {
	deps := newPhotoServiceDeps(activeChallenge(time.Date(2026, 5, 18, 12, 0, 0, 0, time.UTC), "#tag"))
	defer func() {
		if recover() == nil {
			t.Fatal("NewService() did not panic")
		}
	}()
	NewService(Config{
		MainChatID: 1001,
		Challenges: deps.challenges,
		Users:      deps.users,
		Photos:     deps.photoStore,
		Publisher:  deps.publisher,
	})
}

func TestServiceReplacesExistingPhoto(t *testing.T) {
	now := time.Date(2026, 5, 18, 12, 0, 0, 0, time.UTC)
	deps := newPhotoServiceDeps(activeChallenge(now, "#tag"))
	service := newTestService(deps, now)

	if err := service.HandleMainChatMessage(context.Background(), photoMessage("#tag first")); err != nil {
		t.Fatalf("first HandleMainChatMessage() error = %v", err)
	}

	second := photoMessage("#tag second")
	second.ID = 101
	second.Photo = []models.PhotoSize{{FileID: "new-file", FileUniqueID: "new-unique"}}
	if err := service.HandleMainChatMessage(context.Background(), second); err != nil {
		t.Fatalf("second HandleMainChatMessage() error = %v", err)
	}

	if len(deps.photos) != 1 {
		t.Fatalf("photos length = %d, want 1", len(deps.photos))
	}
	got := deps.photos[0]
	if got.FileID != "new-file" || got.SourceMessageID != 101 || got.Caption != "#tag second" {
		t.Fatalf("stored replacement = %#v", got)
	}

	wantMessages := []string{firstPhotoAcceptedMessage, photoReplacedMessage}
	if !reflect.DeepEqual(deps.messages, wantMessages) {
		t.Fatalf("messages = %v, want %v", deps.messages, wantMessages)
	}
}

func TestServiceIgnoresPhotoWithoutMatchingHashtag(t *testing.T) {
	now := time.Date(2026, 5, 18, 12, 0, 0, 0, time.UTC)
	deps := newPhotoServiceDeps(activeChallenge(now, "#tag"))
	service := newTestService(deps, now)

	if err := service.HandleMainChatMessage(context.Background(), photoMessage("#tagged is not the same tag")); err != nil {
		t.Fatalf("HandleMainChatMessage() error = %v", err)
	}

	if len(deps.photos) != 0 {
		t.Fatalf("photos length = %d, want 0", len(deps.photos))
	}
	if len(deps.messages) != 0 {
		t.Fatalf("messages = %v, want none", deps.messages)
	}
}

func TestServiceIgnoresClosedOrVotingChallenge(t *testing.T) {
	now := time.Date(2026, 5, 18, 19, 0, 0, 0, time.UTC)
	closed := activeChallenge(now, "#tag")
	closed.AcceptUntilAt = now.Add(-time.Minute)
	deps := newPhotoServiceDeps(closed)
	service := newTestService(deps, now)

	if err := service.HandleMainChatMessage(context.Background(), photoMessage("#tag")); err != nil {
		t.Fatalf("HandleMainChatMessage() error = %v", err)
	}
	if len(deps.photos) != 0 {
		t.Fatalf("photos length = %d, want 0 after accept window", len(deps.photos))
	}

	voting := activeChallenge(now.Add(-time.Hour), "#tag")
	voting.State = repository.ChallengeStateVoting
	voting.AcceptUntilAt = now.Add(time.Hour)
	deps.challenge = &voting
	if err := service.HandleMainChatMessage(context.Background(), photoMessage("#tag")); err != nil {
		t.Fatalf("HandleMainChatMessage() voting error = %v", err)
	}
	if len(deps.photos) != 0 {
		t.Fatalf("photos length = %d, want 0 during voting", len(deps.photos))
	}
}

func newTestService(deps *photoServiceDeps, now time.Time) *Service {
	return NewService(Config{
		MainChatID: 1001,
		Challenges: deps.challenges,
		Users:      deps.users,
		Photos:     deps.photoStore,
		Publisher:  deps.publisher,
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

type photoServiceDeps struct {
	challenge  *repository.Challenge
	usersByID  map[int64]repository.User
	photos     []repository.Photo
	messages   []string
	challenges *MoqChallenges
	users      *MoqUsers
	photoStore *MoqPhotos
	publisher  *MoqPublisher
}

func newPhotoServiceDeps(challenge repository.Challenge) *photoServiceDeps {
	deps := &photoServiceDeps{
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
	deps.photoStore = &MoqPhotos{UpsertCurrentFunc: func(_ context.Context, input repository.UpsertPhotoInput) (repository.Photo, bool, error) {
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
		for idx, existing := range deps.photos {
			if existing.ChallengeID == input.ChallengeID && existing.AuthorUserID == input.AuthorUserID {
				photo.ID = existing.ID
				deps.photos[idx] = photo
				return photo, true, nil
			}
		}
		deps.photos = append(deps.photos, photo)
		return photo, false, nil
	}}
	deps.publisher = &MoqPublisher{SendTextFunc: func(_ context.Context, _ int64, text string) (int, error) {
		deps.messages = append(deps.messages, text)
		return len(deps.messages), nil
	}}
	return deps
}
