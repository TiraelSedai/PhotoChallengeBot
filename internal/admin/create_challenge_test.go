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

	for _, text := range []string{"/challenge", "Ночной город", "#night", "ОК", "ОК"} {
		if err := handler.HandleAdminChatMessage(context.Background(), adminMessage(text)); err != nil {
			t.Fatalf("HandleAdminChatMessage(%q) error = %v", text, err)
		}
	}

	if len(publisher.sent) != 6 {
		t.Fatalf("sent messages = %d, want 6: %#v", len(publisher.sent), publisher.sent)
	}
	mainAnnouncement := publisher.sent[4]
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

	for _, text := range []string{"/new", "Вода", "#water", "2026-06-01 2026-06-18"} {
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
	lastAdmin := publisher.sent[len(publisher.sent)-1]
	if lastAdmin.chatID != -2002 || lastAdmin.text != "Кастомный анонс #water\n\n"+approvePrompt {
		t.Fatalf("custom confirmation = %#v", lastAdmin)
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

			for _, text := range []string{"/challenge", "Вода", "#water", "ОК"} {
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

			for _, text := range []string{"/challenge", "Вода", "#water", "ОК"} {
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

	for _, text := range []string{"/challenge", "Вода", "#water", "ОК"} {
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

	for _, text := range []string{"/challenge", "Еда", "#food"} {
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

	for _, text := range []string{"/challenge", "Еда", "#food"} {
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

	for _, text := range []string{"/challenge", "Еда", "#food", "not-a-date"} {
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

	for _, text := range []string{"/challenge", "Еда", "#food", "2026-06-18 2026-06-01"} {
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

	for _, text := range []string{"/challenge", "Еда", "#food"} {
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

	for _, text := range []string{"/challenge", "Ночь", "#night", "ОК"} {
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

	for _, text := range []string{"/challenge", "Ночь", "#night", "ОК"} {
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
	if mainAnnouncement.markdown {
		t.Fatalf("custom announcement retried as markdown: %#v", mainAnnouncement)
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

	for _, text := range []string{"/challenge", "Ночь", "#night", "ОК"} {
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

	for _, text := range []string{"/challenge", "Ночь", "#night", "ОК"} {
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

	for _, text := range []string{"/challenge", "Ночь", "#night", "ОК"} {
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

func TestCreateChallengeFlowUsesPlainTextForCustomAnnouncement(t *testing.T) {
	t.Parallel()

	database := openAdminTestDB(t)
	defer database.Close()
	publisher := newCreateChallengePublisherDeps(600)
	handler := newAdminTestHandler(t, database, mustAdminLocation(t), publisher)

	for _, text := range []string{"/challenge@PhotoChallengeBot", "Тема", "#topic", "ОК", "custom_text_with_unmatched_underscore_", "ОК"} {
		if err := handler.HandleAdminChatMessage(context.Background(), adminMessage(text)); err != nil {
			t.Fatalf("HandleAdminChatMessage(%q) error = %v", text, err)
		}
	}

	mainAnnouncement := publisher.lastSendTo(-1001)
	if mainAnnouncement.markdown {
		t.Fatalf("custom announcement sent as markdown: %#v", mainAnnouncement)
	}
	if mainAnnouncement.text != "custom_text_with_unmatched_underscore_" {
		t.Fatalf("custom announcement = %q", mainAnnouncement.text)
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

	for _, text := range []string{"/challenge", "Ночь", "#night", "ОК"} {
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

	for _, text := range []string{"/challenge", "Ночь", "#night", "ОК"} {
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
	chatID    int64
	messageID int
	text      string
	markdown  bool
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
