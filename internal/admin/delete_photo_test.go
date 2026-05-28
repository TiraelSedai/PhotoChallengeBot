package admin

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/TiraelSedai/PhotoChallengeBot/internal/repository"
	"github.com/go-telegram/bot/models"
	"github.com/jmoiron/sqlx"
)

func TestDeletePhotoByUsernameRemovesCurrentChallengePhoto(t *testing.T) {
	t.Parallel()

	database := openAdminTestDB(t)
	defer database.Close()
	ctx := context.Background()
	challengeID := createDeletePhotoChallenge(t, database)
	photo := createDeletePhotoEntry(t, database, challengeID, 11, "Author", "file-1")
	publisher := &recordingPublisher{nextMessageID: 900}
	handler := newDeletePhotoTestHandler(database, publisher)

	if err := handler.HandleAdminChatMessage(ctx, adminMessage("/delete_photo @Author")); err != nil {
		t.Fatalf("HandleAdminChatMessage() error = %v", err)
	}

	if _, err := repository.NewPhotos(database).Get(ctx, photo.ID); err == nil {
		t.Fatal("deleted photo still exists")
	}
	if len(publisher.sent) != 1 {
		t.Fatalf("sent messages = %#v, want one admin confirmation", publisher.sent)
	}
	if publisher.sent[0].chatID != -2002 || !strings.Contains(publisher.sent[0].text, "удалено") {
		t.Fatalf("confirmation = %#v, want admin deletion confirmation", publisher.sent[0])
	}
	if len(publisher.pins) != 0 {
		t.Fatalf("pins = %#v, want no Telegram message actions", publisher.pins)
	}
}

func TestNewDeletePhotoHandlerPanicsOnNilBotUsername(t *testing.T) {
	database := openAdminTestDB(t)
	defer database.Close()
	defer func() {
		if recover() == nil {
			t.Fatal("NewDeletePhotoHandler() did not panic")
		}
	}()
	NewDeletePhotoHandler(DeletePhotoConfig{
		AdminChatID: -2002,
		MainChatID:  -1001,
		Challenges:  repository.NewChallenges(database),
		Photos:      repository.NewPhotos(database),
		Publisher:   &recordingPublisher{},
	})
}

func TestDeletePhotoByTelegramUserIDRemovesCurrentChallengePhoto(t *testing.T) {
	t.Parallel()

	database := openAdminTestDB(t)
	defer database.Close()
	ctx := context.Background()
	challengeID := createDeletePhotoChallenge(t, database)
	photo := createDeletePhotoEntry(t, database, challengeID, 12, "Author", "file-2")
	publisher := &recordingPublisher{nextMessageID: 950}
	handler := newDeletePhotoTestHandler(database, publisher)

	if err := handler.HandleAdminChatMessage(ctx, adminMessage("/delete_photo 12")); err != nil {
		t.Fatalf("HandleAdminChatMessage() error = %v", err)
	}

	if _, err := repository.NewPhotos(database).Get(ctx, photo.ID); err == nil {
		t.Fatal("deleted photo still exists")
	}
	if len(publisher.sent) != 1 {
		t.Fatalf("sent messages = %#v, want one admin confirmation", publisher.sent)
	}
	if publisher.sent[0].chatID != -2002 || !strings.Contains(publisher.sent[0].text, "12") {
		t.Fatalf("confirmation = %#v, want deleted user id in admin confirmation", publisher.sent[0])
	}
}

func TestDeletePhotoByUsernameReportsAmbiguousMatch(t *testing.T) {
	t.Parallel()

	database := openAdminTestDB(t)
	defer database.Close()
	ctx := context.Background()
	challengeID := createDeletePhotoChallenge(t, database)
	first := createDeletePhotoEntry(t, database, challengeID, 21, "SameName", "file-1")
	second := createDeletePhotoEntry(t, database, challengeID, 22, "samename", "file-2")
	publisher := &recordingPublisher{nextMessageID: 975}
	handler := newDeletePhotoTestHandler(database, publisher)

	if err := handler.HandleAdminChatMessage(ctx, adminMessage("/delete_photo @samename")); err != nil {
		t.Fatalf("HandleAdminChatMessage() error = %v", err)
	}

	photos := repository.NewPhotos(database)
	if _, err := photos.Get(ctx, first.ID); err != nil {
		t.Fatalf("first photo removed on ambiguous username: %v", err)
	}
	if _, err := photos.Get(ctx, second.ID); err != nil {
		t.Fatalf("second photo removed on ambiguous username: %v", err)
	}
	if len(publisher.sent) != 1 {
		t.Fatalf("sent messages = %#v, want one admin error", publisher.sent)
	}
	if !strings.Contains(publisher.sent[0].text, "несколько") {
		t.Fatalf("admin error = %q, want ambiguity explanation", publisher.sent[0].text)
	}
}

func TestDeletePhotoReportsInvalidInputToAdmin(t *testing.T) {
	t.Parallel()

	database := openAdminTestDB(t)
	defer database.Close()
	publisher := &recordingPublisher{nextMessageID: 980}
	handler := newDeletePhotoTestHandler(database, publisher)

	if err := handler.HandleAdminChatMessage(context.Background(), adminMessage("/delete_photo username")); err != nil {
		t.Fatalf("HandleAdminChatMessage() error = %v", err)
	}

	if len(publisher.sent) != 1 {
		t.Fatalf("sent messages = %#v, want one admin error", publisher.sent)
	}
	if !strings.Contains(publisher.sent[0].text, "/delete_photo @username") {
		t.Fatalf("admin error = %q, want usage hint", publisher.sent[0].text)
	}
}

func TestDeletePhotoReportsMissingCurrentChallengePhoto(t *testing.T) {
	t.Parallel()

	database := openAdminTestDB(t)
	defer database.Close()
	ctx := context.Background()
	createDeletePhotoChallenge(t, database)
	publisher := &recordingPublisher{nextMessageID: 985}
	handler := newDeletePhotoTestHandler(database, publisher)

	if err := handler.HandleAdminChatMessage(ctx, adminMessage("/delete_photo 404")); err != nil {
		t.Fatalf("HandleAdminChatMessage() error = %v", err)
	}

	if len(publisher.sent) != 1 {
		t.Fatalf("sent messages = %#v, want one admin error", publisher.sent)
	}
	if !strings.Contains(publisher.sent[0].text, "не найдено") {
		t.Fatalf("admin error = %q, want not found explanation", publisher.sent[0].text)
	}
}

func TestDeletePhotoIgnoresNonAdminChat(t *testing.T) {
	t.Parallel()

	database := openAdminTestDB(t)
	defer database.Close()
	ctx := context.Background()
	challengeID := createDeletePhotoChallenge(t, database)
	photo := createDeletePhotoEntry(t, database, challengeID, 11, "Author", "file-1")
	publisher := &recordingPublisher{nextMessageID: 990}
	handler := newDeletePhotoTestHandler(database, publisher)

	message := adminMessage("/delete_photo @Author")
	message.Chat.ID = -1001
	message.Chat.Type = models.ChatTypeSupergroup
	if err := handler.HandleAdminChatMessage(ctx, message); err != nil {
		t.Fatalf("HandleAdminChatMessage() error = %v", err)
	}

	if _, err := repository.NewPhotos(database).Get(ctx, photo.ID); err != nil {
		t.Fatalf("photo removed from non-admin chat: %v", err)
	}
	if len(publisher.sent) != 0 {
		t.Fatalf("sent messages = %#v, want none", publisher.sent)
	}
}

func TestDeletePhotoMentionedBotCommands(t *testing.T) {
	t.Parallel()

	database := openAdminTestDB(t)
	defer database.Close()
	ctx := context.Background()
	challengeID := createDeletePhotoChallenge(t, database)
	otherBotPhoto := createDeletePhotoEntry(t, database, challengeID, 11, "Other", "file-1")
	publisher := &recordingPublisher{nextMessageID: 995}
	handler := newDeletePhotoTestHandler(database, publisher)

	if err := handler.HandleAdminChatMessage(ctx, adminMessage("/delete_photo@OtherBot @Other")); err != nil {
		t.Fatalf("other bot HandleAdminChatMessage() error = %v", err)
	}
	if _, err := repository.NewPhotos(database).Get(ctx, otherBotPhoto.ID); err != nil {
		t.Fatalf("photo removed by other-bot mention: %v", err)
	}
	if len(publisher.sent) != 0 {
		t.Fatalf("sent messages after other-bot command = %#v, want none", publisher.sent)
	}

	currentBotPhoto := createDeletePhotoEntry(t, database, challengeID, 12, "Current", "file-2")
	if err := handler.HandleAdminChatMessage(ctx, adminMessage("/delete_photo@PhotoChallengeBot @Current")); err != nil {
		t.Fatalf("current bot HandleAdminChatMessage() error = %v", err)
	}
	if _, err := repository.NewPhotos(database).Get(ctx, currentBotPhoto.ID); err == nil {
		t.Fatal("current-bot mentioned command did not delete photo")
	}
	if len(publisher.sent) != 1 || !strings.Contains(publisher.sent[0].text, "удалено") {
		t.Fatalf("sent messages = %#v, want one deletion confirmation", publisher.sent)
	}
}

func newDeletePhotoTestHandler(database *sqlx.DB, publisher *recordingPublisher) *DeletePhotoHandler {
	return NewDeletePhotoHandler(DeletePhotoConfig{
		AdminChatID: -2002,
		MainChatID:  -1001,
		Challenges:  repository.NewChallenges(database),
		Photos:      repository.NewPhotos(database),
		Publisher:   publisher,
		BotUsername: func() string {
			return "PhotoChallengeBot"
		},
	})
}

func createDeletePhotoChallenge(t *testing.T, database *sqlx.DB) int64 {
	t.Helper()

	if _, err := repository.NewUsers(database).Upsert(context.Background(), repository.User{
		ID:        10,
		Username:  "admin",
		FirstName: "Admin",
	}); err != nil {
		t.Fatalf("upsert admin user: %v", err)
	}

	now := time.Date(2026, 6, 1, 12, 0, 0, 0, mustAdminLocation(t))
	challenge, err := repository.NewChallenges(database).Create(context.Background(), repository.CreateChallengeInput{
		MainChatID:      -1001,
		Num:             1,
		Theme:           "Theme",
		Hashtag:         "#theme",
		State:           repository.ChallengeStateActive,
		AcceptStartAt:   now.Add(-time.Hour),
		AcceptUntilAt:   now.Add(time.Hour),
		ReminderAt:      now,
		CreatedByUserID: 10,
		CreatedAt:       now,
	})
	if err != nil {
		t.Fatalf("create challenge: %v", err)
	}
	return challenge.ID
}

func createDeletePhotoEntry(t *testing.T, database *sqlx.DB, challengeID, userID int64, username, fileID string) repository.Photo {
	t.Helper()

	ctx := context.Background()
	if _, err := repository.NewUsers(database).Upsert(ctx, repository.User{
		ID:        userID,
		Username:  username,
		FirstName: "Photo",
		LastName:  "Author",
	}); err != nil {
		t.Fatalf("upsert user: %v", err)
	}

	photo, _, err := repository.NewPhotos(database).UpsertCurrent(ctx, repository.UpsertPhotoInput{
		ChallengeID:     challengeID,
		AuthorUserID:    userID,
		FileID:          fileID,
		SourceChatID:    -1001,
		SourceMessageID: int(userID),
		SubmittedAt:     time.Date(2026, 6, 1, 12, 30, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("upsert photo: %v", err)
	}
	return photo
}
