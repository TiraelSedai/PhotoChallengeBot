package admin

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/TiraelSedai/PhotoChallengeBot/internal/repository"
	"github.com/TiraelSedai/PhotoChallengeBot/internal/templates"
	"github.com/jmoiron/sqlx"
)

func TestCreateChallengeEditsUseSharedResultsTemplate(t *testing.T) {
	t.Parallel()

	const resultsURL = "https://t.me/c/1/42"
	const footer = "Другие итоги: [работы](" + resultsURL + ")."
	const customLink = "Свой *анонс* #water: [моя подпись](" + resultsURL + ")"
	for _, tt := range []struct {
		name  string
		photo bool
		text  string
		want  string
	}{
		{name: "text", text: "Свой *анонс* #water", want: "Свой *анонс* #water\n\n" + footer},
		{name: "photo", photo: true, text: "Свой *анонс* #water", want: "Свой *анонс* #water\n\n" + footer},
		{name: "custom link in text", text: customLink, want: customLink},
		{name: "custom link in photo", photo: true, text: customLink, want: customLink},
		{name: "bare link followed by nonbreaking space", text: resultsURL + "\u00a0- итоги", want: resultsURL + "\u00a0- итоги"},
		{name: "bare link in guillemets", text: "Итоги: «" + resultsURL + "»", want: "Итоги: «" + resultsURL + "»"},
		{name: "italic link in photo", photo: true, text: "_Итоги: " + resultsURL + "_", want: "_Итоги: " + resultsURL + "_"},
		{name: "empty photo caption", photo: true, want: footer},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			database := openAdminTestDB(t)
			defer database.Close()
			createAnnouncementHistory(t, database, 1, 42)
			publisher := newCreateChallengePublisherDeps(100)
			handler := newAdminTestHandler(t, database, mustAdminLocation(t), publisher)

			dir := t.TempDir()
			files, err := filepath.Glob(filepath.Join("..", "..", "templates", "*.md.tmpl"))
			if err != nil {
				t.Fatal(err)
			}
			for _, file := range files {
				content, err := os.ReadFile(file)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(dir, filepath.Base(file)), content, 0o600); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.WriteFile(filepath.Join(dir, templates.ChallengePreviousResultsTemplate), []byte("Другие итоги: [работы]({{mdLinkURL .}}).\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			renderer, err := templates.Load(dir)
			if err != nil {
				t.Fatal(err)
			}
			handler.renderer = renderer

			for _, text := range []string{"/challenge", "Вода", "#water", "нет", "ОК"} {
				if err := handler.HandleAdminChatMessage(ctx, adminMessage(text)); err != nil {
					t.Fatalf("prepare draft with %q: %v", text, err)
				}
			}
			if preview := publisher.lastSendTo(-2002); !strings.Contains(preview.text, footer) {
				t.Fatalf("default preview = %q, want shared footer %q", preview.text, footer)
			}

			message := adminMessage(tt.text)
			if tt.photo {
				message = adminPhotoMessage(tt.text)
			}
			if err := handler.HandleAdminChatMessage(ctx, message); err != nil {
				t.Fatalf("edit announcement: %v", err)
			}
			preview := publisher.sent[len(publisher.sent)-2]
			if preview.text != tt.want || !preview.markdown {
				t.Fatalf("custom preview = %#v, want Markdown %q", preview, tt.want)
			}
			if err := handler.HandleAdminChatMessage(ctx, adminMessage("ОК")); err != nil {
				t.Fatalf("approve announcement: %v", err)
			}
			published := publisher.lastSendTo(-1001)
			if published.text != preview.text || published.photoFileID != preview.photoFileID {
				t.Fatalf("published = %#v, want shown preview %#v", published, preview)
			}
			if (published.photoFileID != "") != tt.photo {
				t.Fatalf("published photo = %q, want photo %v", published.photoFileID, tt.photo)
			}
		})
	}
}

func TestCreateChallengeWaitsForDeliveredDraft(t *testing.T) {
	t.Parallel()

	for _, photo := range []bool{false, true} {
		name := "text"
		if photo {
			name = "photo"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			database := openAdminTestDB(t)
			defer database.Close()
			publisher := newCreateChallengePublisherDeps(100)
			location := mustAdminLocation(t)
			handler := newAdminTestHandler(t, database, location, publisher)
			for _, text := range []string{"/challenge", "Вода", "#water"} {
				if err := handler.HandleAdminChatMessage(ctx, adminMessage(text)); err != nil {
					t.Fatal(err)
				}
			}
			message := adminMessage("нет")
			if photo {
				message = adminPhotoMessage("")
			}
			if err := handler.HandleAdminChatMessage(ctx, message); err != nil {
				t.Fatal(err)
			}
			publisher.failSendForChat[-2002] = errors.New("preview unavailable")
			if err := handler.HandleAdminChatMessage(ctx, adminMessage("ОК")); err == nil {
				t.Fatal("preview succeeded, want Telegram failure")
			}

			delete(publisher.failSendForChat, -2002)
			handler = newAdminTestHandler(t, database, location, publisher)
			if err := handler.HandleAdminChatMessage(ctx, adminMessage("ОК")); err != nil {
				t.Fatalf("retry dates after restart: %v", err)
			}
			if got := publisher.countSendsTo(-1001); got != 0 {
				t.Fatalf("main chat received %d messages before draft was shown and approved", got)
			}
			if last := publisher.lastSendTo(-2002); !strings.Contains(last.text, approvePrompt) {
				t.Fatalf("retry response = %q, want preview approval prompt", last.text)
			}
			if err := handler.HandleAdminChatMessage(ctx, adminMessage("ОК")); err != nil {
				t.Fatalf("approve delivered draft: %v", err)
			}
			published := publisher.lastSendTo(-1001)
			if !strings.Contains(published.text, "#water") || (published.photoFileID != "") != photo {
				t.Fatalf("published = %#v, want approved draft, photo=%v", published, photo)
			}
		})
	}
}

func TestCreateChallengeKeepsPreviousResultsSnapshotAfterRestart(t *testing.T) {
	t.Parallel()

	for _, hasResults := range []bool{false, true} {
		name := "no previous results"
		if hasResults {
			name = "previous results"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			database := openAdminTestDB(t)
			defer database.Close()
			if hasResults {
				createAnnouncementHistory(t, database, 1, 42)
			}
			pending := createAnnouncementHistory(t, database, 2, 0)
			publisher := newCreateChallengePublisherDeps(200)
			location := mustAdminLocation(t)
			handler := newAdminTestHandler(t, database, location, publisher)
			for _, text := range []string{"/challenge", "Вода", "#water", "нет", "ОК"} {
				if err := handler.HandleAdminChatMessage(ctx, adminMessage(text)); err != nil {
					t.Fatalf("prepare draft with %q: %v", text, err)
				}
			}

			changed, err := repository.NewChallenges(database).RecordResultsMessageID(ctx, pending.ID, 99, time.Now())
			if err != nil || !changed {
				t.Fatalf("record newer results: changed=%v, error=%v", changed, err)
			}
			handler = newAdminTestHandler(t, database, location, publisher)
			if err := handler.HandleAdminChatMessage(ctx, adminMessage("Новый текст #water")); err != nil {
				t.Fatalf("edit after restart: %v", err)
			}
			preview := publisher.sent[len(publisher.sent)-2]
			if strings.Contains(preview.text, "https://t.me/c/1/99") {
				t.Fatalf("preview picked newer results instead of draft snapshot: %q", preview.text)
			}
			if got := strings.Contains(preview.text, "https://t.me/c/1/42"); got != hasResults {
				t.Fatalf("preview = %q, want original results link present: %v", preview.text, hasResults)
			}
			if !hasResults && preview.text != "Новый текст #water" {
				t.Fatalf("preview = %q, want unchanged custom text without results", preview.text)
			}
			if err := handler.HandleAdminChatMessage(ctx, adminMessage("ОК")); err != nil {
				t.Fatalf("approve after restart: %v", err)
			}
			if published := publisher.lastSendTo(-1001); published.text != preview.text {
				t.Fatalf("published = %q, want preview %q", published.text, preview.text)
			}
		})
	}
}

func TestCreateChallengeKeepsShownAnnouncementOnFailedEdit(t *testing.T) {
	t.Parallel()

	for _, failure := range []string{"template", "telegram"} {
		t.Run(failure, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			database := openAdminTestDB(t)
			defer database.Close()
			createAnnouncementHistory(t, database, 1, 42)
			publisher := newCreateChallengePublisherDeps(220)
			location := mustAdminLocation(t)
			handler := newAdminTestHandler(t, database, location, publisher)
			for _, text := range []string{"/challenge", "Вода", "#water", "нет", "ОК", "Выбранный текст #water"} {
				if err := handler.HandleAdminChatMessage(ctx, adminMessage(text)); err != nil {
					t.Fatal(err)
				}
			}
			shown := publisher.sent[len(publisher.sent)-2]
			sentBeforeEdit := len(publisher.sent)
			if failure == "template" {
				dir := t.TempDir()
				if err := os.WriteFile(filepath.Join(dir, templates.ChallengePreviousResultsTemplate), []byte("{{.MissingField}}"), 0o600); err != nil {
					t.Fatal(err)
				}
				renderer, err := templates.Load(dir)
				if err != nil {
					t.Fatal(err)
				}
				handler.renderer = renderer
			} else {
				publisher.failSendForChat[-2002] = errors.New("preview unavailable")
			}
			if err := handler.HandleAdminChatMessage(ctx, adminPhotoMessage("Неудачная правка #water")); err == nil {
				t.Fatal("edit succeeded, want preparation or delivery failure")
			}
			if len(publisher.sent) != sentBeforeEdit {
				t.Fatal("failed edit sent a partial preview")
			}
			delete(publisher.failSendForChat, -2002)
			handler = newAdminTestHandler(t, database, location, publisher)
			if err := handler.HandleAdminChatMessage(ctx, adminMessage("ОК")); err != nil {
				t.Fatalf("approve last shown announcement after restart: %v", err)
			}
			published := publisher.lastSendTo(-1001)
			if published.text != shown.text || published.photoFileID != shown.photoFileID {
				t.Fatalf("published = %#v, want previous preview %#v", published, shown)
			}
		})
	}
}

func TestCreateChallengeCanApprovePhotoWhenOnlyPromptFails(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	database := openAdminTestDB(t)
	defer database.Close()
	publisher := newCreateChallengePublisherDeps(240)
	handler := newAdminTestHandler(t, database, mustAdminLocation(t), publisher)
	for _, text := range []string{"/challenge", "Вода", "#water"} {
		if err := handler.HandleAdminChatMessage(ctx, adminMessage(text)); err != nil {
			t.Fatal(err)
		}
	}
	if err := handler.HandleAdminChatMessage(ctx, adminPhotoMessage("")); err != nil {
		t.Fatal(err)
	}
	sendMarkdown := publisher.mock.SendMarkdownFunc
	promptErr := errors.New("prompt unavailable")
	publisher.mock.SendMarkdownFunc = func(ctx context.Context, chatID int64, text string) (int, error) {
		if chatID == -2002 && text == approvePrompt {
			return 0, promptErr
		}
		return sendMarkdown(ctx, chatID, text)
	}
	if err := handler.HandleAdminChatMessage(ctx, adminMessage("ОК")); !errors.Is(err, promptErr) {
		t.Fatalf("prepare draft error = %v, want prompt failure", err)
	}
	shown := publisher.lastSendTo(-2002)
	if shown.photoFileID != "photo-big" {
		t.Fatalf("last message = %#v, want delivered photo preview", shown)
	}
	publisher.mock.SendMarkdownFunc = sendMarkdown
	handler = newAdminTestHandler(t, database, mustAdminLocation(t), publisher)
	if err := handler.HandleAdminChatMessage(ctx, adminMessage("ОК")); err != nil {
		t.Fatalf("approve delivered photo after restart: %v", err)
	}
	published := publisher.lastSendTo(-1001)
	if published.text != shown.text || published.photoFileID != shown.photoFileID {
		t.Fatalf("published = %#v, want shown preview %#v", published, shown)
	}
}

func TestCreateChallengeRejectsEditsAfterAnnouncementWasSent(t *testing.T) {
	t.Parallel()

	for _, photo := range []bool{false, true} {
		name := "text"
		if photo {
			name = "photo"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			database := openAdminTestDB(t)
			defer database.Close()
			publisher := newCreateChallengePublisherDeps(250)
			location := mustAdminLocation(t)
			handler := newAdminTestHandler(t, database, location, publisher)
			for _, text := range []string{"/challenge", "Вода", "#water", "нет", "ОК"} {
				if err := handler.HandleAdminChatMessage(ctx, adminMessage(text)); err != nil {
					t.Fatal(err)
				}
			}
			publisher.failPin = errors.New("pin unavailable")
			if err := handler.HandleAdminChatMessage(ctx, adminMessage("ОК")); err == nil {
				t.Fatal("approval succeeded, want pin failure")
			}
			original := publisher.lastSendTo(-1001)
			sentBeforeEdit := len(publisher.sent)
			handler = newAdminTestHandler(t, database, location, publisher)
			message := adminMessage("Новый текст #water")
			if photo {
				message = adminPhotoMessage("Новый текст #water")
			}
			if err := handler.HandleAdminChatMessage(ctx, message); err != nil {
				t.Fatalf("respond to edit after publication: %v", err)
			}
			for _, sent := range publisher.sent[sentBeforeEdit:] {
				if strings.Contains(sent.text, "Новый текст #water") || sent.photoFileID != "" {
					t.Fatalf("bot offered new preview %#v, but the main announcement was already sent", sent)
				}
			}
			if response := publisher.lastSendTo(-2002); !strings.Contains(response.text, "уже опубликован") {
				t.Fatalf("edit response = %q, want explanation that announcement was already published", response.text)
			}
			publisher.failPin = nil
			if err := handler.HandleAdminChatMessage(ctx, adminMessage("ОК")); err != nil {
				t.Fatalf("retry pin: %v", err)
			}
			if got := publisher.countSendsTo(-1001); got != 1 {
				t.Fatalf("main messages = %d, want original announcement only", got)
			}
			if len(publisher.pins) != 1 || publisher.pins[0].messageID != original.messageID {
				t.Fatalf("pins = %#v, want original announcement %d pinned", publisher.pins, original.messageID)
			}
		})
	}
}

func TestCreateChallengeResumesLegacyAnnouncements(t *testing.T) {
	t.Parallel()

	const oldDraft = "Старый черновик #water\n\n[Старые результаты](https://t.me/c/1/41)"
	const oldCustom = "Выбранный *текст* #water\n\n[Мои итоги](https://t.me/c/1/41)"
	for _, tt := range []struct {
		name        string
		selected    bool
		edit        bool
		photo       bool
		wantOldText string
	}{
		{name: "approve shown draft", wantOldText: oldDraft},
		{name: "approve shown custom photo", selected: true, photo: true, wantOldText: oldCustom},
		{name: "edit text", edit: true},
		{name: "edit photo", edit: true, photo: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			database := openAdminTestDB(t)
			defer database.Close()
			createAnnouncementHistory(t, database, 1, 41)
			createAnnouncementHistory(t, database, 2, 42)
			legacy := map[string]any{
				"theme":           "Вода",
				"hashtag":         "#water",
				"start_date":      "2026-05-01",
				"end_date":        "2026-05-18",
				"num":             3,
				"accept_start_at": "2026-04-30T21:00:00Z",
				"accept_until_at": "2026-05-18T15:00:00Z",
				"reminder_at":     "2026-05-17T09:00:00Z",
				"draft_text":      oldDraft,
			}
			if tt.selected {
				legacy["announcement_selected"] = true
				legacy["announcement_markdown"] = true
				legacy["announcement_text"] = oldCustom
				legacy["photo_file_id"] = "legacy-photo"
			}
			payload, err := json.Marshal(legacy)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := repository.NewAdminSessions(database).Upsert(ctx, repository.AdminSession{
				AdminChatID: -2002, AdminUserID: 10, Flow: "create_challenge", Step: "approve", PayloadJSON: string(payload),
			}); err != nil {
				t.Fatalf("restore legacy session: %v", err)
			}
			publisher := newCreateChallengePublisherDeps(300)
			handler := newAdminTestHandler(t, database, mustAdminLocation(t), publisher)
			want := tt.wantOldText
			if tt.edit {
				message := adminMessage("Обновлённый анонс #water")
				if tt.photo {
					message = adminPhotoMessage("Обновлённый анонс #water")
				}
				if err := handler.HandleAdminChatMessage(ctx, message); err != nil {
					t.Fatalf("edit legacy announcement: %v", err)
				}
				want = publisher.sent[len(publisher.sent)-2].text
				if !strings.Contains(want, "https://t.me/c/1/42") || strings.Contains(want, "https://t.me/c/1/41") {
					t.Fatalf("edited legacy preview = %q, want current results URL", want)
				}
			}
			if err := handler.HandleAdminChatMessage(ctx, adminMessage("ОК")); err != nil {
				t.Fatalf("approve legacy announcement: %v", err)
			}
			published := publisher.lastSendTo(-1001)
			if published.text != want || !published.markdown || (published.photoFileID != "") != tt.photo {
				t.Fatalf("published = %#v, want shown Markdown %q, photo=%v", published, want, tt.photo)
			}
		})
	}
}

func createAnnouncementHistory(t *testing.T, database *sqlx.DB, num, messageID int) repository.Challenge {
	t.Helper()
	ctx := context.Background()
	if _, err := repository.NewUsers(database).Upsert(ctx, repository.User{ID: 10, FirstName: "Admin"}); err != nil {
		t.Fatal(err)
	}
	challenges := repository.NewChallenges(database)
	now := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)
	created, err := challenges.Create(ctx, repository.CreateChallengeInput{
		MainChatID: -1001, Num: num, Theme: "Предыдущий", Hashtag: "#previous",
		State: repository.ChallengeStateFinished, AcceptStartAt: now, AcceptUntilAt: now.AddDate(0, 0, 17),
		ReminderAt: now.AddDate(0, 0, 16), CreatedByUserID: 10, CreatedAt: now,
	})
	if err != nil {
		t.Fatalf("create challenge history: %v", err)
	}
	if messageID != 0 {
		changed, err := challenges.RecordResultsMessageID(ctx, created.ID, messageID, now)
		if err != nil || !changed {
			t.Fatalf("record previous results: changed=%v, error=%v", changed, err)
		}
	}
	return created
}
