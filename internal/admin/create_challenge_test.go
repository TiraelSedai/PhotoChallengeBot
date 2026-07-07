package admin

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/TiraelSedai/PhotoChallengeBot/internal/challenge"
	"github.com/TiraelSedai/PhotoChallengeBot/internal/db"
	"github.com/TiraelSedai/PhotoChallengeBot/internal/repository"
	"github.com/TiraelSedai/PhotoChallengeBot/internal/templates"
	"github.com/go-telegram/bot/models"
	"github.com/jmoiron/sqlx"
)

func TestCreateChallengeFlowPublishesAndPinsDefaultAnnouncement(t *testing.T) {
	t.Parallel()

	database := openAdminTestDB(t)
	defer database.Close()
	location := mustAdminLocation(t)
	publisher := newCreateChallengePublisherDeps(100)
	handler := newAdminTestHandler(t, database, location, publisher)

	for _, text := range []string{"/challenge", "Ночной город", "#night", "нет", "ОК", "ОК"} {
		if err := handler.HandleAdminChatMessage(context.Background(), adminMessage(text)); err != nil {
			t.Fatalf("HandleAdminChatMessage(%q) error = %v", text, err)
		}
	}

	if len(publisher.sent) != 7 {
		t.Fatalf("sent messages = %d, want 7: %#v", len(publisher.sent), publisher.sent)
	}
	mainAnnouncement := publisher.sent[5]
	if mainAnnouncement.chatID != -1001 {
		t.Fatalf("announcement chat = %d, want main chat", mainAnnouncement.chatID)
	}
	if !strings.Contains(mainAnnouncement.text, "Ночной город") || !strings.Contains(mainAnnouncement.text, "#night") {
		t.Fatalf("announcement text = %q, want theme and hashtag", mainAnnouncement.text)
	}
	if len(publisher.pins) != 1 || publisher.pins[0].chatID != -1001 || publisher.pins[0].messageID != mainAnnouncement.messageID {
		t.Fatalf("pins = %#v, want published announcement pinned", publisher.pins)
	}

	challengeRepo := repository.NewChallenges(database)
	open, err := challengeRepo.FindOpenByMainChatID(context.Background(), -1001)
	if err != nil {
		t.Fatalf("FindOpenByMainChatID() error = %v", err)
	}
	if open == nil {
		t.Fatal("open challenge = nil, want created challenge")
	}
	if open.AnnouncementMessageID == nil || *open.AnnouncementMessageID != int64(mainAnnouncement.messageID) {
		t.Fatalf("AnnouncementMessageID = %#v, want %d", open.AnnouncementMessageID, mainAnnouncement.messageID)
	}

	session, err := repository.NewAdminSessions(database).Get(context.Background(), -2002, 10)
	if err != nil {
		t.Fatalf("Get session after publish error = %v", err)
	}
	if session != nil {
		t.Fatalf("session after publish = %#v, want nil", session)
	}
}

func TestNewCreateChallengeHandlerPanicsOnNilBotUsername(t *testing.T) {
	database := openAdminTestDB(t)
	defer database.Close()
	location := mustAdminLocation(t)
	renderer, err := templates.Load(filepath.Join("..", "..", "templates"))
	if err != nil {
		t.Fatalf("Load templates: %v", err)
	}
	challengeRepo := repository.NewChallenges(database)
	defer func() {
		if recover() == nil {
			t.Fatal("NewCreateChallengeHandler() did not panic")
		}
	}()
	NewCreateChallengeHandler(CreateChallengeConfig{
		AdminChatID:   -2002,
		MainChatID:    -1001,
		Location:      location,
		Sessions:      repository.NewAdminSessions(database),
		Users:         repository.NewUsers(database),
		Challenges:    challenge.NewService(challengeRepo, location, time.Now),
		Announcements: challengeRepo,
		Renderer:      renderer,
		Publisher:     newCreateChallengePublisherDeps(0).mock,
	})
}

func TestCreateChallengeFlowUsesCustomApprovedText(t *testing.T) {
	t.Parallel()

	database := openAdminTestDB(t)
	defer database.Close()
	publisher := newCreateChallengePublisherDeps(200)
	handler := newAdminTestHandler(t, database, mustAdminLocation(t), publisher)

	for _, text := range []string{"/new", "Вода", "#water", "нет", "2026-06-01 2026-06-18"} {
		if err := handler.HandleAdminChatMessage(context.Background(), adminMessage(text)); err != nil {
			t.Fatalf("HandleAdminChatMessage(%q) error = %v", text, err)
		}
	}

	if err := handler.HandleAdminChatMessage(context.Background(), adminMessage("Кастомный анонс #water")); err != nil {
		t.Fatalf("HandleAdminChatMessage(custom) error = %v", err)
	}
	if got := publisher.countSendsTo(-1001); got != 0 {
		t.Fatalf("main sends after custom text = %d, want 0", got)
	}
	prompt := publisher.sent[len(publisher.sent)-1]
	preview := publisher.sent[len(publisher.sent)-2]
	if preview.chatID != -2002 || preview.text != "Кастомный анонс #water" || !preview.markdown {
		t.Fatalf("custom preview = %#v, want markdown custom text", preview)
	}
	if prompt.chatID != -2002 || prompt.text != approvePrompt {
		t.Fatalf("custom confirmation prompt = %#v", prompt)
	}

	if err := handler.HandleAdminChatMessage(context.Background(), adminMessage("ОК")); err != nil {
		t.Fatalf("HandleAdminChatMessage(approve) error = %v", err)
	}

	mainAnnouncement := publisher.lastSendTo(-1001)
	if mainAnnouncement.text != "Кастомный анонс #water" {
		t.Fatalf("main announcement = %q, want custom text", mainAnnouncement.text)
	}

	open, err := repository.NewChallenges(database).FindOpenByMainChatID(context.Background(), -1001)
	if err != nil {
		t.Fatalf("FindOpenByMainChatID() error = %v", err)
	}
	if open == nil {
		t.Fatal("open challenge = nil, want created challenge")
	}
	wantUntil := time.Date(2026, 6, 18, 18, 0, 0, 0, mustAdminLocation(t))
	if !open.AcceptUntilAt.Equal(wantUntil) {
		t.Fatalf("AcceptUntilAt = %s, want %s", open.AcceptUntilAt, wantUntil)
	}
}

func TestCreateChallengeFlowAsksPhotoAfterHashtag(t *testing.T) {
	t.Parallel()

	database := openAdminTestDB(t)
	defer database.Close()
	publisher := newCreateChallengePublisherDeps(150)
	handler := newAdminTestHandler(t, database, mustAdminLocation(t), publisher)

	for _, text := range []string{"/challenge", "Ночь", "#night"} {
		if err := handler.HandleAdminChatMessage(context.Background(), adminMessage(text)); err != nil {
			t.Fatalf("HandleAdminChatMessage(%q) error = %v", text, err)
		}
	}

	last := publisher.sent[len(publisher.sent)-1]
	if !strings.Contains(last.text, "Пришли картинку") || !strings.Contains(last.text, "нет") || !strings.Contains(last.text, "/skip") {
		t.Fatalf("photo prompt = %q, want picture request mentioning нет and /skip", last.text)
	}
	session, err := repository.NewAdminSessions(database).Get(context.Background(), -2002, 10)
	if err != nil {
		t.Fatalf("Get session error = %v", err)
	}
	if session == nil || session.Step != stepPhoto {
		t.Fatalf("session = %#v, want photo step", session)
	}
}

func TestCreateChallengeFlowSkipsWizardPhoto(t *testing.T) {
	t.Parallel()

	tests := []string{"нет", "НЕТ", "/skip", "/skip@PhotoChallengeBot"}
	for _, skipText := range tests {
		t.Run(skipText, func(t *testing.T) {
			t.Parallel()

			database := openAdminTestDB(t)
			defer database.Close()
			publisher := newCreateChallengePublisherDeps(160)
			handler := newAdminTestHandler(t, database, mustAdminLocation(t), publisher)

			for _, text := range []string{"/challenge", "Ночь", "#night", skipText} {
				if err := handler.HandleAdminChatMessage(context.Background(), adminMessage(text)); err != nil {
					t.Fatalf("HandleAdminChatMessage(%q) error = %v", text, err)
				}
			}

			last := publisher.sent[len(publisher.sent)-1]
			if !strings.Contains(last.text, "Пришли даты") {
				t.Fatalf("last message = %q, want dates prompt", last.text)
			}
			session, err := repository.NewAdminSessions(database).Get(context.Background(), -2002, 10)
			if err != nil {
				t.Fatalf("Get session error = %v", err)
			}
			if session == nil || session.Step != stepDates {
				t.Fatalf("session = %#v, want dates step", session)
			}
			if strings.Contains(session.PayloadJSON, `"photo_file_id"`) {
				t.Fatalf("session payload = %q, want no photo saved", session.PayloadJSON)
			}
		})
	}
}

func TestCreateChallengeFlowRepromptsOnNonPhotoAtPhotoStep(t *testing.T) {
	t.Parallel()

	database := openAdminTestDB(t)
	defer database.Close()
	publisher := newCreateChallengePublisherDeps(170)
	handler := newAdminTestHandler(t, database, mustAdminLocation(t), publisher)

	for _, text := range []string{"/challenge", "Ночь", "#night", "вот такая тема"} {
		if err := handler.HandleAdminChatMessage(context.Background(), adminMessage(text)); err != nil {
			t.Fatalf("HandleAdminChatMessage(%q) error = %v", text, err)
		}
	}

	last := publisher.sent[len(publisher.sent)-1]
	if last.text != photoPrompt {
		t.Fatalf("last message = %q, want repeated photo prompt", last.text)
	}
	session, err := repository.NewAdminSessions(database).Get(context.Background(), -2002, 10)
	if err != nil {
		t.Fatalf("Get session error = %v", err)
	}
	if session == nil || session.Step != stepPhoto {
		t.Fatalf("session = %#v, want photo step preserved", session)
	}

	sentBefore := len(publisher.sent)
	if err := handler.HandleAdminChatMessage(context.Background(), adminMessage("/start")); err != nil {
		t.Fatalf("HandleAdminChatMessage(command) error = %v", err)
	}
	if len(publisher.sent) != sentBefore {
		t.Fatalf("sent after command = %d, want unchanged %d", len(publisher.sent), sentBefore)
	}
}

func TestCreateChallengeFlowPublishesPhotoAnnouncementFromWizardPhoto(t *testing.T) {
	t.Parallel()

	database := openAdminTestDB(t)
	defer database.Close()
	publisher := newCreateChallengePublisherDeps(180)
	handler := newAdminTestHandler(t, database, mustAdminLocation(t), publisher)

	for _, text := range []string{"/challenge", "Ночь", "#night"} {
		if err := handler.HandleAdminChatMessage(context.Background(), adminMessage(text)); err != nil {
			t.Fatalf("HandleAdminChatMessage(%q) error = %v", text, err)
		}
	}
	if err := handler.HandleAdminChatMessage(context.Background(), adminPhotoMessage("капшн игнорируется")); err != nil {
		t.Fatalf("HandleAdminChatMessage(photo) error = %v", err)
	}

	last := publisher.sent[len(publisher.sent)-1]
	if !strings.Contains(last.text, "Пришли даты") {
		t.Fatalf("last message = %q, want dates prompt after photo", last.text)
	}

	for _, text := range []string{"ОК", "ОК"} {
		if err := handler.HandleAdminChatMessage(context.Background(), adminMessage(text)); err != nil {
			t.Fatalf("HandleAdminChatMessage(%q) error = %v", text, err)
		}
	}

	mainAnnouncement := publisher.lastSendTo(-1001)
	if mainAnnouncement.photoFileID != "photo-big" {
		t.Fatalf("announcement photo = %q, want largest photo size", mainAnnouncement.photoFileID)
	}
	if !mainAnnouncement.markdown {
		t.Fatalf("announcement = %#v, want markdown draft caption", mainAnnouncement)
	}
	if !strings.Contains(mainAnnouncement.text, "Ночь") || !strings.Contains(mainAnnouncement.text, "#night") {
		t.Fatalf("announcement caption = %q, want generated draft", mainAnnouncement.text)
	}
	if strings.Contains(mainAnnouncement.text, "капшн игнорируется") {
		t.Fatalf("announcement caption = %q, want photo-step caption ignored", mainAnnouncement.text)
	}
	if len(publisher.pins) != 1 || publisher.pins[0].messageID != mainAnnouncement.messageID {
		t.Fatalf("pins = %#v, want photo announcement pinned", publisher.pins)
	}
}

func TestCreateChallengeFlowSendsPhotoDraftPreviewWithSeparatePrompt(t *testing.T) {
	t.Parallel()

	database := openAdminTestDB(t)
	defer database.Close()
	publisher := newCreateChallengePublisherDeps(190)
	handler := newAdminTestHandler(t, database, mustAdminLocation(t), publisher)

	for _, text := range []string{"/challenge", "Ночь", "#night"} {
		if err := handler.HandleAdminChatMessage(context.Background(), adminMessage(text)); err != nil {
			t.Fatalf("HandleAdminChatMessage(%q) error = %v", text, err)
		}
	}
	if err := handler.HandleAdminChatMessage(context.Background(), adminPhotoMessage("")); err != nil {
		t.Fatalf("HandleAdminChatMessage(photo) error = %v", err)
	}
	if err := handler.HandleAdminChatMessage(context.Background(), adminMessage("ОК")); err != nil {
		t.Fatalf("HandleAdminChatMessage(dates) error = %v", err)
	}

	prompt := publisher.sent[len(publisher.sent)-1]
	preview := publisher.sent[len(publisher.sent)-2]
	if preview.chatID != -2002 || preview.photoFileID != "photo-big" || !preview.markdown {
		t.Fatalf("draft preview = %#v, want markdown photo in admin chat", preview)
	}
	if strings.Contains(preview.text, approvePrompt) {
		t.Fatalf("draft preview caption = %q, want approve prompt sent separately", preview.text)
	}
	if prompt.chatID != -2002 || prompt.text != approvePrompt {
		t.Fatalf("prompt message = %#v, want separate approve prompt", prompt)
	}
}

func TestCreateChallengeFlowOverridesAnnouncementWithPhotoCaption(t *testing.T) {
	t.Parallel()

	database := openAdminTestDB(t)
	defer database.Close()
	publisher := newCreateChallengePublisherDeps(210)
	handler := newAdminTestHandler(t, database, mustAdminLocation(t), publisher)

	for _, text := range []string{"/challenge", "Вода", "#water", "нет", "ОК"} {
		if err := handler.HandleAdminChatMessage(context.Background(), adminMessage(text)); err != nil {
			t.Fatalf("HandleAdminChatMessage(%q) error = %v", text, err)
		}
	}
	if err := handler.HandleAdminChatMessage(context.Background(), adminPhotoMessage("Свой анонс #water")); err != nil {
		t.Fatalf("HandleAdminChatMessage(photo override) error = %v", err)
	}

	if got := publisher.countSendsTo(-1001); got != 0 {
		t.Fatalf("main sends after photo override = %d, want 0", got)
	}
	prompt := publisher.sent[len(publisher.sent)-1]
	echo := publisher.sent[len(publisher.sent)-2]
	if echo.chatID != -2002 || echo.photoFileID != "photo-big" || echo.text != "Свой анонс #water" || !echo.markdown {
		t.Fatalf("override echo = %#v, want markdown photo with caption", echo)
	}
	if prompt.text != approvePrompt {
		t.Fatalf("prompt = %q, want approve prompt", prompt.text)
	}

	if err := handler.HandleAdminChatMessage(context.Background(), adminMessage("ОК")); err != nil {
		t.Fatalf("HandleAdminChatMessage(approve) error = %v", err)
	}

	mainAnnouncement := publisher.lastSendTo(-1001)
	if mainAnnouncement.photoFileID != "photo-big" || mainAnnouncement.text != "Свой анонс #water" || !mainAnnouncement.markdown {
		t.Fatalf("announcement = %#v, want markdown photo with custom caption", mainAnnouncement)
	}
	if len(publisher.pins) != 1 || publisher.pins[0].messageID != mainAnnouncement.messageID {
		t.Fatalf("pins = %#v, want photo announcement pinned", publisher.pins)
	}
}

func TestCreateChallengeFlowTextOverrideDropsWizardPhoto(t *testing.T) {
	t.Parallel()

	database := openAdminTestDB(t)
	defer database.Close()
	publisher := newCreateChallengePublisherDeps(220)
	handler := newAdminTestHandler(t, database, mustAdminLocation(t), publisher)

	for _, text := range []string{"/challenge", "Ночь", "#night"} {
		if err := handler.HandleAdminChatMessage(context.Background(), adminMessage(text)); err != nil {
			t.Fatalf("HandleAdminChatMessage(%q) error = %v", text, err)
		}
	}
	if err := handler.HandleAdminChatMessage(context.Background(), adminPhotoMessage("")); err != nil {
		t.Fatalf("HandleAdminChatMessage(photo) error = %v", err)
	}
	for _, text := range []string{"ОК", "Текстовый анонс без картинки", "ОК"} {
		if err := handler.HandleAdminChatMessage(context.Background(), adminMessage(text)); err != nil {
			t.Fatalf("HandleAdminChatMessage(%q) error = %v", text, err)
		}
	}

	mainAnnouncement := publisher.lastSendTo(-1001)
	if mainAnnouncement.photoFileID != "" {
		t.Fatalf("announcement = %#v, want text override to drop wizard photo", mainAnnouncement)
	}
	if mainAnnouncement.text != "Текстовый анонс без картинки" || !mainAnnouncement.markdown {
		t.Fatalf("announcement = %#v, want markdown custom text", mainAnnouncement)
	}
}

func TestCreateChallengeFlowIgnoresPhotoOutsidePhotoSteps(t *testing.T) {
	t.Parallel()

	database := openAdminTestDB(t)
	defer database.Close()
	publisher := newCreateChallengePublisherDeps(230)
	handler := newAdminTestHandler(t, database, mustAdminLocation(t), publisher)

	for _, text := range []string{"/challenge", "Ночь", "#night", "нет"} {
		if err := handler.HandleAdminChatMessage(context.Background(), adminMessage(text)); err != nil {
			t.Fatalf("HandleAdminChatMessage(%q) error = %v", text, err)
		}
	}
	sentBefore := len(publisher.sent)
	if err := handler.HandleAdminChatMessage(context.Background(), adminPhotoMessage("2026-06-01 2026-06-18")); err != nil {
		t.Fatalf("HandleAdminChatMessage(photo at dates) error = %v", err)
	}

	if len(publisher.sent) != sentBefore {
		t.Fatalf("sent after photo at dates step = %d, want unchanged %d", len(publisher.sent), sentBefore)
	}
	session, err := repository.NewAdminSessions(database).Get(context.Background(), -2002, 10)
	if err != nil {
		t.Fatalf("Get session error = %v", err)
	}
	if session == nil || session.Step != stepDates {
		t.Fatalf("session = %#v, want dates step preserved", session)
	}
}

func TestCreateChallengeFlowRestartsFromApprovalOnCreateCommand(t *testing.T) {
	t.Parallel()

	tests := []string{"/challenge", "/new"}
	for _, restartCommand := range tests {
		t.Run(restartCommand, func(t *testing.T) {
			t.Parallel()

			database := openAdminTestDB(t)
			defer database.Close()
			publisher := newCreateChallengePublisherDeps(225)
			handler := newAdminTestHandler(t, database, mustAdminLocation(t), publisher)

			for _, text := range []string{"/challenge", "Вода", "#water", "нет", "ОК"} {
				if err := handler.HandleAdminChatMessage(context.Background(), adminMessage(text)); err != nil {
					t.Fatalf("HandleAdminChatMessage(%q) error = %v", text, err)
				}
			}

			if err := handler.HandleAdminChatMessage(context.Background(), adminMessage(restartCommand)); err != nil {
				t.Fatalf("HandleAdminChatMessage(restart) error = %v", err)
			}
			if err := handler.HandleAdminChatMessage(context.Background(), adminMessage("Ночная улица")); err != nil {
				t.Fatalf("HandleAdminChatMessage(theme) error = %v", err)
			}

			if got := publisher.countSendsTo(-1001); got != 0 {
				t.Fatalf("main sends after restart = %d, want 0", got)
			}
			session, err := repository.NewAdminSessions(database).Get(context.Background(), -2002, 10)
			if err != nil {
				t.Fatalf("Get session error = %v", err)
			}
			if session == nil || session.Step != stepHashtag {
				t.Fatalf("session = %#v, want hashtag step after new theme", session)
			}
			if !strings.Contains(session.PayloadJSON, `"theme":"Ночная улица"`) {
				t.Fatalf("session payload = %q, want restarted theme", session.PayloadJSON)
			}
		})
	}
}

func TestCreateChallengeFlowCancelsFromApproval(t *testing.T) {
	t.Parallel()

	tests := []string{"/cancel", "отмена"}
	for _, cancelText := range tests {
		t.Run(cancelText, func(t *testing.T) {
			t.Parallel()

			database := openAdminTestDB(t)
			defer database.Close()
			publisher := newCreateChallengePublisherDeps(240)
			handler := newAdminTestHandler(t, database, mustAdminLocation(t), publisher)

			for _, text := range []string{"/challenge", "Вода", "#water", "нет", "ОК"} {
				if err := handler.HandleAdminChatMessage(context.Background(), adminMessage(text)); err != nil {
					t.Fatalf("HandleAdminChatMessage(%q) error = %v", text, err)
				}
			}
			if err := handler.HandleAdminChatMessage(context.Background(), adminMessage(cancelText)); err != nil {
				t.Fatalf("HandleAdminChatMessage(%q) error = %v", cancelText, err)
			}

			if got := publisher.countSendsTo(-1001); got != 0 {
				t.Fatalf("main sends after cancel = %d, want 0", got)
			}
			session, err := repository.NewAdminSessions(database).Get(context.Background(), -2002, 10)
			if err != nil {
				t.Fatalf("Get session error = %v", err)
			}
			if session != nil {
				t.Fatalf("session = %#v, want nil after cancel", session)
			}
			lastAdmin := publisher.sent[len(publisher.sent)-1]
			if !strings.Contains(lastAdmin.text, "Создание челленджа отменено") {
				t.Fatalf("last admin message = %q, want cancel confirmation", lastAdmin.text)
			}
		})
	}
}

func TestCreateChallengeFlowDoesNotUseSlashCommandAsCustomAnnouncement(t *testing.T) {
	t.Parallel()

	database := openAdminTestDB(t)
	defer database.Close()
	publisher := newCreateChallengePublisherDeps(245)
	handler := newAdminTestHandler(t, database, mustAdminLocation(t), publisher)

	for _, text := range []string{"/challenge", "Вода", "#water", "нет", "ОК"} {
		if err := handler.HandleAdminChatMessage(context.Background(), adminMessage(text)); err != nil {
			t.Fatalf("HandleAdminChatMessage(%q) error = %v", text, err)
		}
	}
	sentBefore := len(publisher.sent)
	if err := handler.HandleAdminChatMessage(context.Background(), adminMessage("/start")); err != nil {
		t.Fatalf("HandleAdminChatMessage(command) error = %v", err)
	}

	if len(publisher.sent) != sentBefore {
		t.Fatalf("sent after command = %d, want unchanged %d: %#v", len(publisher.sent), sentBefore, publisher.sent)
	}
	session, err := repository.NewAdminSessions(database).Get(context.Background(), -2002, 10)
	if err != nil {
		t.Fatalf("Get session error = %v", err)
	}
	if session == nil || !strings.Contains(session.PayloadJSON, `"draft_text"`) {
		t.Fatalf("session = %#v, want approval session preserved", session)
	}
	if strings.Contains(session.PayloadJSON, `/start`) {
		t.Fatalf("session payload = %q, want command not saved as announcement", session.PayloadJSON)
	}
}

func TestCreateChallengeFlowShowsDefaultDatesInDatePrompt(t *testing.T) {
	t.Parallel()

	database := openAdminTestDB(t)
	defer database.Close()
	publisher := newCreateChallengePublisherDeps(250)
	handler := newAdminTestHandler(t, database, mustAdminLocation(t), publisher)

	for _, text := range []string{"/challenge", "Еда", "#food", "нет"} {
		if err := handler.HandleAdminChatMessage(context.Background(), adminMessage(text)); err != nil {
			t.Fatalf("HandleAdminChatMessage(%q) error = %v", text, err)
		}
	}

	last := publisher.sent[len(publisher.sent)-1]
	if !strings.Contains(last.text, "`ОК` - с 2026-05-01 по 2026-05-18") {
		t.Fatalf("date prompt = %q, want explicit default date range", last.text)
	}
}

func TestCreateChallengeFlowUsesPromptedDefaultDatesAfterClockMoves(t *testing.T) {
	t.Parallel()

	database := openAdminTestDB(t)
	defer database.Close()
	location := mustAdminLocation(t)
	publisher := newCreateChallengePublisherDeps(275)
	now := time.Date(2026, 5, 1, 23, 59, 0, 0, location)
	handler := newAdminTestHandlerWithClock(t, database, location, publisher, func() time.Time {
		return now
	})

	for _, text := range []string{"/challenge", "Еда", "#food", "нет"} {
		if err := handler.HandleAdminChatMessage(context.Background(), adminMessage(text)); err != nil {
			t.Fatalf("HandleAdminChatMessage(%q) error = %v", text, err)
		}
	}
	now = time.Date(2026, 5, 2, 0, 1, 0, 0, location)
	for _, text := range []string{"ОК", "ОК"} {
		if err := handler.HandleAdminChatMessage(context.Background(), adminMessage(text)); err != nil {
			t.Fatalf("HandleAdminChatMessage(%q) error = %v", text, err)
		}
	}

	open, err := repository.NewChallenges(database).FindOpenByMainChatID(context.Background(), -1001)
	if err != nil {
		t.Fatalf("FindOpenByMainChatID() error = %v", err)
	}
	if open == nil {
		t.Fatal("open challenge = nil, want created challenge")
	}
	wantStart := time.Date(2026, 5, 1, 0, 0, 0, 0, location)
	wantUntil := time.Date(2026, 5, 18, 18, 0, 0, 0, location)
	if !open.AcceptStartAt.Equal(wantStart) {
		t.Fatalf("AcceptStartAt = %s, want prompted %s", open.AcceptStartAt, wantStart)
	}
	if !open.AcceptUntilAt.Equal(wantUntil) {
		t.Fatalf("AcceptUntilAt = %s, want prompted %s", open.AcceptUntilAt, wantUntil)
	}
}

func TestCreateChallengeFlowRejectsBadDatesWithoutAdvancing(t *testing.T) {
	t.Parallel()

	database := openAdminTestDB(t)
	defer database.Close()
	publisher := newCreateChallengePublisherDeps(300)
	handler := newAdminTestHandler(t, database, mustAdminLocation(t), publisher)

	for _, text := range []string{"/challenge", "Еда", "#food", "нет", "not-a-date"} {
		if err := handler.HandleAdminChatMessage(context.Background(), adminMessage(text)); err != nil {
			t.Fatalf("HandleAdminChatMessage(%q) error = %v", text, err)
		}
	}

	last := publisher.sent[len(publisher.sent)-1]
	if !strings.Contains(last.text, "Не понял даты") {
		t.Fatalf("last message = %q, want date error", last.text)
	}

	session, err := repository.NewAdminSessions(database).Get(context.Background(), -2002, 10)
	if err != nil {
		t.Fatalf("Get session error = %v", err)
	}
	if session == nil || session.Step != stepDates {
		t.Fatalf("session = %#v, want dates step", session)
	}
}

func TestCreateChallengeFlowReportsSemanticDateErrors(t *testing.T) {
	t.Parallel()

	database := openAdminTestDB(t)
	defer database.Close()
	publisher := newCreateChallengePublisherDeps(325)
	handler := newAdminTestHandler(t, database, mustAdminLocation(t), publisher)

	for _, text := range []string{"/challenge", "Еда", "#food", "нет", "2026-06-18 2026-06-01"} {
		if err := handler.HandleAdminChatMessage(context.Background(), adminMessage(text)); err != nil {
			t.Fatalf("HandleAdminChatMessage(%q) error = %v", text, err)
		}
	}

	last := publisher.sent[len(publisher.sent)-1]
	if !strings.Contains(last.text, "Не получилось подготовить челлендж") {
		t.Fatalf("last message = %q, want semantic date error", last.text)
	}
	session, err := repository.NewAdminSessions(database).Get(context.Background(), -2002, 10)
	if err != nil {
		t.Fatalf("Get session error = %v", err)
	}
	if session == nil || session.Step != stepDates {
		t.Fatalf("session = %#v, want dates step", session)
	}
}

func TestCreateChallengeFlowIgnoresEmptyAdminMessages(t *testing.T) {
	t.Parallel()

	database := openAdminTestDB(t)
	defer database.Close()
	publisher := newCreateChallengePublisherDeps(350)
	handler := newAdminTestHandler(t, database, mustAdminLocation(t), publisher)

	for _, text := range []string{"/challenge", "Еда", "#food", "нет"} {
		if err := handler.HandleAdminChatMessage(context.Background(), adminMessage(text)); err != nil {
			t.Fatalf("HandleAdminChatMessage(%q) error = %v", text, err)
		}
	}
	sentBefore := len(publisher.sent)
	if err := handler.HandleAdminChatMessage(context.Background(), adminMessage("")); err != nil {
		t.Fatalf("HandleAdminChatMessage(empty) error = %v", err)
	}
	if len(publisher.sent) != sentBefore {
		t.Fatalf("sent messages after empty = %d, want unchanged %d", len(publisher.sent), sentBefore)
	}
	session, err := repository.NewAdminSessions(database).Get(context.Background(), -2002, 10)
	if err != nil {
		t.Fatalf("Get session error = %v", err)
	}
	if session == nil || session.Step != stepDates {
		t.Fatalf("session = %#v, want dates step", session)
	}
}

func TestCreateChallengeFlowIgnoresOtherBotMentionInsideSession(t *testing.T) {
	t.Parallel()

	database := openAdminTestDB(t)
	defer database.Close()
	publisher := newCreateChallengePublisherDeps(375)
	handler := newAdminTestHandler(t, database, mustAdminLocation(t), publisher)

	if err := handler.HandleAdminChatMessage(context.Background(), adminMessage("/challenge")); err != nil {
		t.Fatalf("HandleAdminChatMessage(start) error = %v", err)
	}
	sentBefore := len(publisher.sent)
	if err := handler.HandleAdminChatMessage(context.Background(), adminMessage("/challenge@OtherBot")); err != nil {
		t.Fatalf("HandleAdminChatMessage(other bot) error = %v", err)
	}
	if len(publisher.sent) != sentBefore {
		t.Fatalf("sent messages after other bot command = %d, want unchanged %d", len(publisher.sent), sentBefore)
	}
	session, err := repository.NewAdminSessions(database).Get(context.Background(), -2002, 10)
	if err != nil {
		t.Fatalf("Get session error = %v", err)
	}
	if session == nil || session.Step != stepTheme {
		t.Fatalf("session = %#v, want theme step", session)
	}
}

func TestCreateChallengeFlowRecoversAfterPublishFailure(t *testing.T) {
	t.Parallel()

	database := openAdminTestDB(t)
	defer database.Close()
	publisher := newCreateChallengePublisherDeps(400)
	publisher.failSendForChat[-1001] = errors.New("telegram unavailable")
	handler := newAdminTestHandler(t, database, mustAdminLocation(t), publisher)

	for _, text := range []string{"/challenge", "Ночь", "#night", "нет", "ОК"} {
		if err := handler.HandleAdminChatMessage(context.Background(), adminMessage(text)); err != nil {
			t.Fatalf("HandleAdminChatMessage(%q) error = %v", text, err)
		}
	}
	if err := handler.HandleAdminChatMessage(context.Background(), adminMessage("ОК")); err == nil || !strings.Contains(err.Error(), "telegram unavailable") {
		t.Fatalf("approve error = %v, want telegram unavailable", err)
	}

	session, err := repository.NewAdminSessions(database).Get(context.Background(), -2002, 10)
	if err != nil {
		t.Fatalf("Get session error = %v", err)
	}
	if session == nil || !strings.Contains(session.PayloadJSON, `"challenge_id"`) {
		t.Fatalf("session = %#v, want saved challenge_id for retry", session)
	}
	if !strings.Contains(session.PayloadJSON, `"announcement_markdown":true`) {
		t.Fatalf("session = %#v, want selected template announcement persisted", session)
	}

	delete(publisher.failSendForChat, -1001)
	if err := handler.HandleAdminChatMessage(context.Background(), adminMessage("ОК")); err != nil {
		t.Fatalf("retry approve error = %v", err)
	}

	open, err := repository.NewChallenges(database).FindOpenByMainChatID(context.Background(), -1001)
	if err != nil {
		t.Fatalf("FindOpenByMainChatID() error = %v", err)
	}
	if open == nil || open.Num != 1 {
		t.Fatalf("open = %#v, want original challenge", open)
	}
	if len(publisher.pins) != 1 {
		t.Fatalf("pins = %#v, want retry to pin announcement", publisher.pins)
	}
}

func TestCreateChallengeFlowRecoversCustomTextAfterPublishFailure(t *testing.T) {
	t.Parallel()

	database := openAdminTestDB(t)
	defer database.Close()
	publisher := newCreateChallengePublisherDeps(450)
	publisher.failSendForChat[-1001] = errors.New("telegram unavailable")
	handler := newAdminTestHandler(t, database, mustAdminLocation(t), publisher)

	for _, text := range []string{"/challenge", "Ночь", "#night", "нет", "ОК"} {
		if err := handler.HandleAdminChatMessage(context.Background(), adminMessage(text)); err != nil {
			t.Fatalf("HandleAdminChatMessage(%q) error = %v", text, err)
		}
	}
	if err := handler.HandleAdminChatMessage(context.Background(), adminMessage("custom_text_with_unmatched_underscore_")); err != nil {
		t.Fatalf("custom text error = %v", err)
	}
	if err := handler.HandleAdminChatMessage(context.Background(), adminMessage("ОК")); err == nil {
		t.Fatal("custom approve error = nil, want send failure")
	}

	delete(publisher.failSendForChat, -1001)
	if err := handler.HandleAdminChatMessage(context.Background(), adminMessage("ОК")); err != nil {
		t.Fatalf("retry approve error = %v", err)
	}

	mainAnnouncement := publisher.lastSendTo(-1001)
	if !mainAnnouncement.markdown {
		t.Fatalf("custom announcement retried without markdown: %#v", mainAnnouncement)
	}
	if mainAnnouncement.text != "custom_text_with_unmatched_underscore_" {
		t.Fatalf("retried announcement = %q, want original custom text", mainAnnouncement.text)
	}
}

func TestCreateChallengeFlowRecoversAfterPinFailure(t *testing.T) {
	t.Parallel()

	database := openAdminTestDB(t)
	defer database.Close()
	publisher := newCreateChallengePublisherDeps(500)
	publisher.failPin = errors.New("pin failed")
	handler := newAdminTestHandler(t, database, mustAdminLocation(t), publisher)

	for _, text := range []string{"/challenge", "Ночь", "#night", "нет", "ОК"} {
		if err := handler.HandleAdminChatMessage(context.Background(), adminMessage(text)); err != nil {
			t.Fatalf("HandleAdminChatMessage(%q) error = %v", text, err)
		}
	}
	if err := handler.HandleAdminChatMessage(context.Background(), adminMessage("ОК")); err == nil || !strings.Contains(err.Error(), "pin failed") {
		t.Fatalf("approve error = %v, want pin failed", err)
	}

	mainSends := publisher.countSendsTo(-1001)
	publisher.failPin = nil
	if err := handler.HandleAdminChatMessage(context.Background(), adminMessage("ОК")); err != nil {
		t.Fatalf("retry approve error = %v", err)
	}
	if got := publisher.countSendsTo(-1001); got != mainSends {
		t.Fatalf("main sends after retry = %d, want unchanged %d", got, mainSends)
	}
	if len(publisher.pins) != 1 {
		t.Fatalf("pins = %#v, want one successful pin", publisher.pins)
	}
}

func TestCreateChallengeFlowRecoversAfterAnnouncementMessageIDPersistFailure(t *testing.T) {
	t.Parallel()

	database := openAdminTestDB(t)
	defer database.Close()
	publisher := newCreateChallengePublisherDeps(550)
	challengeRepo := repository.NewChallenges(database)
	failSetOnce := errors.New("persist announcement id failed")
	announcements := &MoqChallengeAnnouncements{
		GetFunc: challengeRepo.Get,
		SetAnnouncementMessageIDFunc: func(ctx context.Context, id int64, messageID int, updatedAt time.Time) error {
			if failSetOnce != nil {
				err := failSetOnce
				failSetOnce = nil
				return err
			}
			return challengeRepo.SetAnnouncementMessageID(ctx, id, messageID, updatedAt)
		},
	}
	handler := newAdminTestHandlerWithAnnouncements(t, database, mustAdminLocation(t), publisher, announcements)

	for _, text := range []string{"/challenge", "Ночь", "#night", "нет", "ОК"} {
		if err := handler.HandleAdminChatMessage(context.Background(), adminMessage(text)); err != nil {
			t.Fatalf("HandleAdminChatMessage(%q) error = %v", text, err)
		}
	}
	if err := handler.HandleAdminChatMessage(context.Background(), adminMessage("ОК")); err == nil || !strings.Contains(err.Error(), "persist announcement id failed") {
		t.Fatalf("approve error = %v, want persist announcement id failed", err)
	}

	mainSends := publisher.countSendsTo(-1001)
	session, err := repository.NewAdminSessions(database).Get(context.Background(), -2002, 10)
	if err != nil {
		t.Fatalf("Get session error = %v", err)
	}
	if session == nil || !strings.Contains(session.PayloadJSON, `"announcement_message_id"`) {
		t.Fatalf("session = %#v, want sent announcement message id saved for retry", session)
	}

	if err := handler.HandleAdminChatMessage(context.Background(), adminMessage("ОК")); err != nil {
		t.Fatalf("retry approve error = %v", err)
	}
	if got := publisher.countSendsTo(-1001); got != mainSends {
		t.Fatalf("main sends after retry = %d, want unchanged %d", got, mainSends)
	}
	if len(publisher.pins) != 1 {
		t.Fatalf("pins = %#v, want one successful pin", publisher.pins)
	}
}

func TestCreateChallengeFlowRecoversAfterSessionSaveFailureAfterAnnouncementSend(t *testing.T) {
	t.Parallel()

	database := openAdminTestDB(t)
	defer database.Close()
	publisher := newCreateChallengePublisherDeps(575)
	handler := newAdminTestHandler(t, database, mustAdminLocation(t), publisher)
	sessionRepo := repository.NewAdminSessions(database)
	saveSessionErr := errors.New("save session failed")
	handler.sessions = &MoqSessionStore{
		GetFunc: sessionRepo.Get,
		UpsertFunc: func(ctx context.Context, session repository.AdminSession) (repository.AdminSession, error) {
			if saveSessionErr != nil && strings.Contains(session.PayloadJSON, `"announcement_message_id"`) {
				err := saveSessionErr
				saveSessionErr = nil
				return repository.AdminSession{}, err
			}
			return sessionRepo.Upsert(ctx, session)
		},
		ClearFunc: sessionRepo.Clear,
	}

	for _, text := range []string{"/challenge", "Ночь", "#night", "нет", "ОК"} {
		if err := handler.HandleAdminChatMessage(context.Background(), adminMessage(text)); err != nil {
			t.Fatalf("HandleAdminChatMessage(%q) error = %v", text, err)
		}
	}
	if err := handler.HandleAdminChatMessage(context.Background(), adminMessage("ОК")); err == nil || !strings.Contains(err.Error(), "save session failed") {
		t.Fatalf("approve error = %v, want save session failed", err)
	}

	mainSends := publisher.countSendsTo(-1001)
	if err := handler.HandleAdminChatMessage(context.Background(), adminMessage("ОК")); err != nil {
		t.Fatalf("retry approve error = %v", err)
	}
	if got := publisher.countSendsTo(-1001); got != mainSends {
		t.Fatalf("main sends after retry = %d, want unchanged %d", got, mainSends)
	}
	if len(publisher.pins) != 1 {
		t.Fatalf("pins = %#v, want one successful pin", publisher.pins)
	}
}

func TestCreateChallengeFlowUsesMarkdownForCustomAnnouncement(t *testing.T) {
	t.Parallel()

	database := openAdminTestDB(t)
	defer database.Close()
	publisher := newCreateChallengePublisherDeps(600)
	handler := newAdminTestHandler(t, database, mustAdminLocation(t), publisher)

	for _, text := range []string{"/challenge@PhotoChallengeBot", "Тема", "#topic", "нет", "ОК", "custom_text_with_unmatched_underscore_", "ОК"} {
		if err := handler.HandleAdminChatMessage(context.Background(), adminMessage(text)); err != nil {
			t.Fatalf("HandleAdminChatMessage(%q) error = %v", text, err)
		}
	}

	mainAnnouncement := publisher.lastSendTo(-1001)
	if !mainAnnouncement.markdown {
		t.Fatalf("custom announcement sent without markdown: %#v", mainAnnouncement)
	}
	if mainAnnouncement.text != "custom_text_with_unmatched_underscore_" {
		t.Fatalf("custom announcement = %q", mainAnnouncement.text)
	}
}

func TestCreateChallengeFlowUsesMarkdownForCustomPhotoCaption(t *testing.T) {
	t.Parallel()

	database := openAdminTestDB(t)
	defer database.Close()
	publisher := newCreateChallengePublisherDeps(630)
	handler := newAdminTestHandler(t, database, mustAdminLocation(t), publisher)

	for _, text := range []string{"/challenge", "Ночь", "#night", "нет", "ОК"} {
		if err := handler.HandleAdminChatMessage(context.Background(), adminMessage(text)); err != nil {
			t.Fatalf("HandleAdminChatMessage(%q) error = %v", text, err)
		}
	}
	customAnnouncement := "Любой кастомный капшн\n\ncaption_with_markdown_*as is*"
	if err := handler.HandleAdminChatMessage(context.Background(), adminPhotoMessage(customAnnouncement)); err != nil {
		t.Fatalf("HandleAdminChatMessage(photo override) error = %v", err)
	}
	if err := handler.HandleAdminChatMessage(context.Background(), adminMessage("ОК")); err != nil {
		t.Fatalf("HandleAdminChatMessage(approve) error = %v", err)
	}

	mainAnnouncement := publisher.lastSendTo(-1001)
	if mainAnnouncement.photoFileID != "photo-big" || !mainAnnouncement.markdown {
		t.Fatalf("announcement = %#v, want markdown photo announcement", mainAnnouncement)
	}
	if mainAnnouncement.text != customAnnouncement {
		t.Fatalf("announcement caption = %q, want %q", mainAnnouncement.text, customAnnouncement)
	}
}

func TestCreateChallengeFlowAddsPreviousResultsToCustomAnnouncement(t *testing.T) {
	t.Parallel()

	database := openAdminTestDB(t)
	defer database.Close()
	publisher := newCreateChallengePublisherDeps(635)
	challengeRepo := repository.NewChallenges(database)
	resultsChatID := int64(-1001272818469)
	resultsMessageID := int64(144934)
	announcements := &MoqChallengeAnnouncements{
		GetFunc:                      challengeRepo.Get,
		SetAnnouncementMessageIDFunc: challengeRepo.SetAnnouncementMessageID,
		FindLatestWithResultsFunc: func(ctx context.Context, mainChatID int64) (*repository.Challenge, error) {
			return &repository.Challenge{
				MainChatID:       mainChatID,
				Num:              108,
				ResultsChatID:    &resultsChatID,
				ResultsMessageID: &resultsMessageID,
			}, nil
		},
	}
	handler := newAdminTestHandlerWithAnnouncements(t, database, mustAdminLocation(t), publisher, announcements)

	for _, text := range []string{"/challenge", "Вода", "#water", "нет", "ОК"} {
		if err := handler.HandleAdminChatMessage(context.Background(), adminMessage(text)); err != nil {
			t.Fatalf("HandleAdminChatMessage(%q) error = %v", text, err)
		}
	}
	if err := handler.HandleAdminChatMessage(context.Background(), adminMessage("Кастомный анонс #water")); err != nil {
		t.Fatalf("HandleAdminChatMessage(custom) error = %v", err)
	}

	preview := publisher.sent[len(publisher.sent)-2]
	if !strings.Contains(preview.text, "Кастомный анонс #water") ||
		!strings.Contains(preview.text, "https://t.me/c/1272818469/144934") ||
		!preview.markdown {
		t.Fatalf("custom preview = %#v, want markdown custom text with previous results", preview)
	}

	if err := handler.HandleAdminChatMessage(context.Background(), adminMessage("ОК")); err != nil {
		t.Fatalf("HandleAdminChatMessage(approve) error = %v", err)
	}

	mainAnnouncement := publisher.lastSendTo(-1001)
	if !strings.Contains(mainAnnouncement.text, "Кастомный анонс #water") ||
		!strings.Contains(mainAnnouncement.text, "https://t.me/c/1272818469/144934") ||
		!mainAnnouncement.markdown {
		t.Fatalf("announcement = %#v, want markdown custom text with previous results", mainAnnouncement)
	}
}

func TestCreateChallengeFlowAddsPreviousResultsToCustomPhotoCaption(t *testing.T) {
	t.Parallel()

	database := openAdminTestDB(t)
	defer database.Close()
	publisher := newCreateChallengePublisherDeps(637)
	challengeRepo := repository.NewChallenges(database)
	resultsMessageID := int64(144934)
	announcements := &MoqChallengeAnnouncements{
		GetFunc:                      challengeRepo.Get,
		SetAnnouncementMessageIDFunc: challengeRepo.SetAnnouncementMessageID,
		FindLatestWithResultsFunc: func(ctx context.Context, mainChatID int64) (*repository.Challenge, error) {
			return &repository.Challenge{
				MainChatID:       mainChatID,
				Num:              108,
				ResultsMessageID: &resultsMessageID,
			}, nil
		},
	}
	handler := newAdminTestHandlerWithAnnouncements(t, database, mustAdminLocation(t), publisher, announcements)

	for _, text := range []string{"/challenge", "Вода", "#water", "нет", "ОК"} {
		if err := handler.HandleAdminChatMessage(context.Background(), adminMessage(text)); err != nil {
			t.Fatalf("HandleAdminChatMessage(%q) error = %v", text, err)
		}
	}
	if err := handler.HandleAdminChatMessage(context.Background(), adminPhotoMessage("Свой анонс #water")); err != nil {
		t.Fatalf("HandleAdminChatMessage(photo override) error = %v", err)
	}

	preview := publisher.sent[len(publisher.sent)-2]
	if preview.photoFileID != "photo-big" ||
		!strings.Contains(preview.text, "Свой анонс #water") ||
		!strings.Contains(preview.text, "https://t.me/c/1/144934") ||
		!preview.markdown {
		t.Fatalf("photo preview = %#v, want markdown custom caption with previous results", preview)
	}

	if err := handler.HandleAdminChatMessage(context.Background(), adminMessage("ОК")); err != nil {
		t.Fatalf("HandleAdminChatMessage(approve) error = %v", err)
	}

	mainAnnouncement := publisher.lastSendTo(-1001)
	if mainAnnouncement.photoFileID != "photo-big" ||
		!strings.Contains(mainAnnouncement.text, "Свой анонс #water") ||
		!strings.Contains(mainAnnouncement.text, "https://t.me/c/1/144934") ||
		!mainAnnouncement.markdown {
		t.Fatalf("announcement = %#v, want markdown custom caption with previous results", mainAnnouncement)
	}
}

func TestCreateChallengeFlowDoesNotPersistCustomAnnouncementWhenPreviewFails(t *testing.T) {
	t.Parallel()

	database := openAdminTestDB(t)
	defer database.Close()
	publisher := newCreateChallengePublisherDeps(640)
	handler := newAdminTestHandler(t, database, mustAdminLocation(t), publisher)

	for _, text := range []string{"/challenge", "Ночь", "#night", "нет", "ОК"} {
		if err := handler.HandleAdminChatMessage(context.Background(), adminMessage(text)); err != nil {
			t.Fatalf("HandleAdminChatMessage(%q) error = %v", text, err)
		}
	}

	publisher.failSendForChat[-2002] = errors.New("telegram rejected markdown")
	if err := handler.HandleAdminChatMessage(context.Background(), adminMessage("custom_text_with_unmatched_underscore_")); err == nil {
		t.Fatal("custom preview error = nil, want send failure")
	}

	session, err := repository.NewAdminSessions(database).Get(context.Background(), -2002, 10)
	if err != nil {
		t.Fatalf("Get session error = %v", err)
	}
	if session == nil || session.Step != stepApprove {
		t.Fatalf("session = %#v, want approve step", session)
	}
	if strings.Contains(session.PayloadJSON, `"announcement_selected"`) ||
		strings.Contains(session.PayloadJSON, `"announcement_text"`) ||
		strings.Contains(session.PayloadJSON, "custom_text_with_unmatched_underscore_") {
		t.Fatalf("session payload = %q, want failed custom announcement not persisted", session.PayloadJSON)
	}
}

func TestCreateChallengeFlowDoesNotPersistCustomPhotoCaptionWhenPreviewFails(t *testing.T) {
	t.Parallel()

	database := openAdminTestDB(t)
	defer database.Close()
	publisher := newCreateChallengePublisherDeps(645)
	handler := newAdminTestHandler(t, database, mustAdminLocation(t), publisher)

	for _, text := range []string{"/challenge", "Ночь", "#night", "нет", "ОК"} {
		if err := handler.HandleAdminChatMessage(context.Background(), adminMessage(text)); err != nil {
			t.Fatalf("HandleAdminChatMessage(%q) error = %v", text, err)
		}
	}

	publisher.failSendForChat[-2002] = errors.New("telegram rejected markdown")
	if err := handler.HandleAdminChatMessage(context.Background(), adminPhotoMessage("custom_photo_caption_with_unmatched_underscore_")); err == nil {
		t.Fatal("custom photo preview error = nil, want send failure")
	}

	session, err := repository.NewAdminSessions(database).Get(context.Background(), -2002, 10)
	if err != nil {
		t.Fatalf("Get session error = %v", err)
	}
	if session == nil || session.Step != stepApprove {
		t.Fatalf("session = %#v, want approve step", session)
	}
	if strings.Contains(session.PayloadJSON, `"announcement_selected"`) ||
		strings.Contains(session.PayloadJSON, `"announcement_text"`) ||
		strings.Contains(session.PayloadJSON, "custom_photo_caption_with_unmatched_underscore_") {
		t.Fatalf("session payload = %q, want failed custom photo caption not persisted", session.PayloadJSON)
	}
}

func TestCreateChallengeFlowIgnoresCommandMentionedToOtherBot(t *testing.T) {
	t.Parallel()

	database := openAdminTestDB(t)
	defer database.Close()
	publisher := newCreateChallengePublisherDeps(650)
	handler := newAdminTestHandler(t, database, mustAdminLocation(t), publisher)

	if err := handler.HandleAdminChatMessage(context.Background(), adminMessage("/challenge@OtherBot")); err != nil {
		t.Fatalf("HandleAdminChatMessage() error = %v", err)
	}
	if len(publisher.sent) != 0 {
		t.Fatalf("sent = %#v, want no messages", publisher.sent)
	}
}

func TestCreateChallengeFlowRejectsInvalidHashtag(t *testing.T) {
	t.Parallel()

	database := openAdminTestDB(t)
	defer database.Close()
	publisher := newCreateChallengePublisherDeps(700)
	handler := newAdminTestHandler(t, database, mustAdminLocation(t), publisher)

	for _, text := range []string{"/challenge", "Ночь", "#night city"} {
		if err := handler.HandleAdminChatMessage(context.Background(), adminMessage(text)); err != nil {
			t.Fatalf("HandleAdminChatMessage(%q) error = %v", text, err)
		}
	}

	last := publisher.sent[len(publisher.sent)-1]
	if !strings.Contains(last.text, "Хештег должен быть одним словом") {
		t.Fatalf("last message = %q, want hashtag validation error", last.text)
	}
	session, err := repository.NewAdminSessions(database).Get(context.Background(), -2002, 10)
	if err != nil {
		t.Fatalf("Get session error = %v", err)
	}
	if session == nil || session.Step != stepHashtag {
		t.Fatalf("session = %#v, want hashtag step", session)
	}
}

func TestCreateChallengeFlowPersistsResolvedDefaultDates(t *testing.T) {
	t.Parallel()

	database := openAdminTestDB(t)
	defer database.Close()
	publisher := newCreateChallengePublisherDeps(800)
	handler := newAdminTestHandler(t, database, mustAdminLocation(t), publisher)

	for _, text := range []string{"/challenge", "Ночь", "#night", "нет", "ОК"} {
		if err := handler.HandleAdminChatMessage(context.Background(), adminMessage(text)); err != nil {
			t.Fatalf("HandleAdminChatMessage(%q) error = %v", text, err)
		}
	}

	session, err := repository.NewAdminSessions(database).Get(context.Background(), -2002, 10)
	if err != nil {
		t.Fatalf("Get session error = %v", err)
	}
	if session == nil || !strings.Contains(session.PayloadJSON, `"start_date"`) || !strings.Contains(session.PayloadJSON, `"end_date"`) {
		t.Fatalf("session = %#v, want resolved dates persisted", session)
	}
	if strings.Contains(session.PayloadJSON, `"start_date":""`) || strings.Contains(session.PayloadJSON, `"end_date":""`) {
		t.Fatalf("session = %#v, want non-empty resolved dates", session)
	}
}

func TestCreateChallengeFlowDoesNotPublishWithStalePlannedNumber(t *testing.T) {
	t.Parallel()

	database := openAdminTestDB(t)
	defer database.Close()
	publisher := newCreateChallengePublisherDeps(850)
	handler := newAdminTestHandler(t, database, mustAdminLocation(t), publisher)

	for _, text := range []string{"/challenge", "Ночь", "#night", "нет", "ОК"} {
		if err := handler.HandleAdminChatMessage(context.Background(), adminMessage(text)); err != nil {
			t.Fatalf("HandleAdminChatMessage(%q) error = %v", text, err)
		}
	}

	challengeRepo := repository.NewChallenges(database)
	created, err := challengeRepo.Create(context.Background(), repository.CreateChallengeInput{
		MainChatID:      -1001,
		Num:             1,
		Theme:           "Other",
		Hashtag:         "#other",
		State:           repository.ChallengeStateActive,
		AcceptStartAt:   time.Date(2026, 7, 1, 0, 0, 0, 0, mustAdminLocation(t)),
		AcceptUntilAt:   time.Date(2026, 7, 18, 18, 0, 0, 0, mustAdminLocation(t)),
		ReminderAt:      time.Date(2026, 7, 17, 12, 0, 0, 0, mustAdminLocation(t)),
		CreatedByUserID: 10,
		CreatedAt:       time.Date(2026, 7, 1, 0, 0, 0, 0, mustAdminLocation(t)),
	})
	if err != nil {
		t.Fatalf("create competing challenge: %v", err)
	}
	if _, err := database.Exec(`UPDATE challenges SET state = 'finished', finished_at = updated_at WHERE id = ?`, created.ID); err != nil {
		t.Fatalf("finish competing challenge: %v", err)
	}

	mainSends := publisher.countSendsTo(-1001)
	if err := handler.HandleAdminChatMessage(context.Background(), adminMessage("ОК")); err != nil {
		t.Fatalf("approve with stale planned number error = %v", err)
	}
	if got := publisher.countSendsTo(-1001); got != mainSends {
		t.Fatalf("main sends after stale approve = %d, want unchanged %d", got, mainSends)
	}
	session, err := repository.NewAdminSessions(database).Get(context.Background(), -2002, 10)
	if err != nil {
		t.Fatalf("Get session error = %v", err)
	}
	if session != nil {
		t.Fatalf("session = %#v, want cleared after stale planned number", session)
	}
	last := publisher.sent[len(publisher.sent)-1]
	if !strings.Contains(last.text, "Не получилось создать челлендж") {
		t.Fatalf("last message = %q, want create failure notice", last.text)
	}
}

func TestRussianWeekdayUsesGenitiveCase(t *testing.T) {
	t.Parallel()

	tests := []struct {
		weekday time.Weekday
		want    string
	}{
		{time.Monday, "понедельника"},
		{time.Tuesday, "вторника"},
		{time.Wednesday, "среды"},
		{time.Thursday, "четверга"},
		{time.Friday, "пятницы"},
		{time.Saturday, "субботы"},
		{time.Sunday, "воскресенья"},
	}

	for _, tt := range tests {
		value := time.Date(2026, 6, 1+int(tt.weekday-time.Monday), 12, 0, 0, 0, time.UTC)
		if got := russianWeekday(value); got != tt.want {
			t.Fatalf("russianWeekday(%s) = %q, want %q", tt.weekday, got, tt.want)
		}
	}
}

func TestCreateChallengeDraftLinksPreviousResultsFromImportedChat(t *testing.T) {
	t.Parallel()

	database := openAdminTestDB(t)
	defer database.Close()
	publisher := newCreateChallengePublisherDeps(900)
	resultsChatID := int64(-1001272818469)
	resultsMessageID := int64(143054)
	announcements := &MoqChallengeAnnouncements{
		FindLatestWithResultsFunc: func(ctx context.Context, mainChatID int64) (*repository.Challenge, error) {
			return &repository.Challenge{
				MainChatID:       mainChatID,
				Num:              107,
				ResultsChatID:    &resultsChatID,
				ResultsMessageID: &resultsMessageID,
			}, nil
		},
	}
	handler := newAdminTestHandlerWithAnnouncements(t, database, mustAdminLocation(t), publisher, announcements)

	for _, text := range []string{"/challenge", "Жёлтый", "#жёлтый", "нет", "ОК"} {
		if err := handler.HandleAdminChatMessage(context.Background(), adminMessage(text)); err != nil {
			t.Fatalf("HandleAdminChatMessage(%q) error = %v", text, err)
		}
	}

	draft := publisher.lastSendTo(-2002)
	if !strings.Contains(draft.text, "https://t.me/c/1272818469/143054") {
		t.Fatalf("draft = %q, want previous results link pointing to the imported chat", draft.text)
	}
}

func newAdminTestHandler(t *testing.T, database *sqlx.DB, location *time.Location, publisher *createChallengePublisherDeps) *CreateChallengeHandler {
	t.Helper()

	return newAdminTestHandlerWithClock(t, database, location, publisher, func() time.Time {
		return time.Date(2026, 5, 1, 11, 30, 0, 0, location)
	})
}

func newAdminTestHandlerWithClock(
	t *testing.T,
	database *sqlx.DB,
	location *time.Location,
	publisher *createChallengePublisherDeps,
	now func() time.Time,
) *CreateChallengeHandler {
	t.Helper()

	return newAdminTestHandlerWithAnnouncementsAndClock(t, database, location, publisher, nil, now)
}

func newAdminTestHandlerWithAnnouncements(
	t *testing.T,
	database *sqlx.DB,
	location *time.Location,
	publisher *createChallengePublisherDeps,
	announcements challengeAnnouncements,
) *CreateChallengeHandler {
	t.Helper()

	return newAdminTestHandlerWithAnnouncementsAndClock(t, database, location, publisher, announcements, func() time.Time {
		return time.Date(2026, 5, 1, 11, 30, 0, 0, location)
	})
}

func newAdminTestHandlerWithAnnouncementsAndClock(
	t *testing.T,
	database *sqlx.DB,
	location *time.Location,
	publisher *createChallengePublisherDeps,
	announcements challengeAnnouncements,
	now func() time.Time,
) *CreateChallengeHandler {
	t.Helper()

	renderer, err := templates.Load(filepath.Join("..", "..", "templates"))
	if err != nil {
		t.Fatalf("Load templates: %v", err)
	}
	challengeRepo := repository.NewChallenges(database)
	if announcements == nil {
		announcements = challengeRepo
	}
	return NewCreateChallengeHandler(CreateChallengeConfig{
		AdminChatID:   -2002,
		MainChatID:    -1001,
		Location:      location,
		Sessions:      repository.NewAdminSessions(database),
		Users:         repository.NewUsers(database),
		Challenges:    challenge.NewService(challengeRepo, location, now),
		Announcements: announcements,
		Renderer:      renderer,
		Publisher:     publisher.mock,
		BotUsername: func() string {
			return "PhotoChallengeBot"
		},
	})
}

func openAdminTestDB(t *testing.T) *sqlx.DB {
	t.Helper()

	database, err := db.Open(context.Background(), db.Options{
		Path:          filepath.Join(t.TempDir(), "bot.sqlite"),
		MigrationsDir: "../../migrations",
	})
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	return database
}

func adminMessage(text string) *models.Message {
	return &models.Message{
		ID:   1,
		Text: text,
		Chat: models.Chat{ID: -2002, Type: models.ChatTypeGroup},
		From: &models.User{
			ID:        10,
			Username:  "admin",
			FirstName: "Admin",
		},
	}
}

func adminPhotoMessage(caption string) *models.Message {
	message := adminMessage("")
	message.Caption = caption
	message.Photo = []models.PhotoSize{
		{FileID: "photo-small", FileUniqueID: "photo-small-uid"},
		{FileID: "photo-big", FileUniqueID: "photo-big-uid"},
	}
	return message
}

type createChallengePublisherDeps struct {
	mock            *MoqCreateChallengePublisher
	nextMessageID   int
	failSendForChat map[int64]error
	failPin         error
	sent            []sentMessage
	pins            []pinnedMessage
}

func newCreateChallengePublisherDeps(nextMessageID int) *createChallengePublisherDeps {
	deps := &createChallengePublisherDeps{
		nextMessageID:   nextMessageID,
		failSendForChat: make(map[int64]error),
	}
	deps.mock = &MoqCreateChallengePublisher{
		SendMarkdownFunc: func(_ context.Context, chatID int64, text string) (int, error) {
			if err := deps.failSendForChat[chatID]; err != nil {
				return 0, err
			}
			deps.nextMessageID++
			messageID := deps.nextMessageID
			deps.sent = append(deps.sent, sentMessage{
				chatID:    chatID,
				messageID: messageID,
				text:      text,
				markdown:  true,
			})
			return messageID, nil
		},
		SendTextFunc: func(_ context.Context, chatID int64, text string) (int, error) {
			if err := deps.failSendForChat[chatID]; err != nil {
				return 0, err
			}
			deps.nextMessageID++
			messageID := deps.nextMessageID
			deps.sent = append(deps.sent, sentMessage{
				chatID:    chatID,
				messageID: messageID,
				text:      text,
			})
			return messageID, nil
		},
		SendPhotoFunc: func(_ context.Context, chatID int64, fileID string, caption string, _ *models.InlineKeyboardMarkup) (int, error) {
			if err := deps.failSendForChat[chatID]; err != nil {
				return 0, err
			}
			deps.nextMessageID++
			messageID := deps.nextMessageID
			deps.sent = append(deps.sent, sentMessage{
				chatID:      chatID,
				messageID:   messageID,
				text:        caption,
				photoFileID: fileID,
			})
			return messageID, nil
		},
		SendMarkdownPhotoFunc: func(_ context.Context, chatID int64, fileID string, caption string) (int, error) {
			if err := deps.failSendForChat[chatID]; err != nil {
				return 0, err
			}
			deps.nextMessageID++
			messageID := deps.nextMessageID
			deps.sent = append(deps.sent, sentMessage{
				chatID:      chatID,
				messageID:   messageID,
				text:        caption,
				markdown:    true,
				photoFileID: fileID,
			})
			return messageID, nil
		},
		PinFunc: func(_ context.Context, chatID int64, messageID int) error {
			if deps.failPin != nil {
				return deps.failPin
			}
			deps.pins = append(deps.pins, pinnedMessage{
				chatID:    chatID,
				messageID: messageID,
			})
			return nil
		},
	}
	return deps
}

func (p *createChallengePublisherDeps) countSendsTo(chatID int64) int {
	var count int
	for _, sent := range p.sent {
		if sent.chatID == chatID {
			count++
		}
	}
	return count
}

func (p *createChallengePublisherDeps) lastSendTo(chatID int64) sentMessage {
	for i := len(p.sent) - 1; i >= 0; i-- {
		if p.sent[i].chatID == chatID {
			return p.sent[i]
		}
	}
	return sentMessage{}
}

type sentMessage struct {
	chatID      int64
	messageID   int
	text        string
	markdown    bool
	photoFileID string
}

type pinnedMessage struct {
	chatID    int64
	messageID int
}

func mustAdminLocation(t *testing.T) *time.Location {
	t.Helper()
	location, err := time.LoadLocation("Europe/Moscow")
	if err != nil {
		t.Fatalf("LoadLocation: %v", err)
	}
	return location
}
