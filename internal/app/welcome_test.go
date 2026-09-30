package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/TiraelSedai/PhotoChallengeBot/internal/config"
	"github.com/TiraelSedai/PhotoChallengeBot/internal/tg"
	tgbot "github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
)

func TestAppSendsEphemeralWelcomeOnJoin(t *testing.T) {
	for _, tt := range []struct {
		name      string
		forbidden bool
	}{
		{name: "success"}, {name: "API error", forbidden: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			sent := make(chan url.Values, 2)
			processed := make(chan struct{}, 2)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				switch r.URL.Path {
				case "/bottest/getMe":
					io.WriteString(w, `{"ok":true,"result":{"id":9,"is_bot":true,"first_name":"Bot","username":"PhotoChallengeBot"}}`)
				case "/bottest/getUpdates":
					if err := r.ParseMultipartForm(1 << 20); err != nil {
						t.Error(err)
						return
					}
					defer r.MultipartForm.RemoveAll()
					var allowed []string
					if err := json.Unmarshal([]byte(r.FormValue("allowed_updates")), &allowed); err != nil {
						t.Error(err)
					}
					for _, kind := range []string{"message", "callback_query", "chat_member"} {
						if !slices.Contains(allowed, kind) {
							t.Errorf("allowed_updates = %v, missing %s", allowed, kind)
						}
					}
					if r.FormValue("offset") == "1" {
						io.WriteString(w, `{"ok":true,"result":[{"update_id":1,"chat_member":{"chat":{"id":-1001,"type":"supergroup"},"from":{"id":99,"first_name":"Admin"},"old_chat_member":{"status":"left","user":{"id":42,"first_name":"Newcomer"}},"new_chat_member":{"status":"member","user":{"id":42,"first_name":"Newcomer"}}}},{"update_id":2,"message":{"message_id":10,"chat":{"id":-1001,"type":"supergroup"},"from":{"id":99},"new_chat_members":[{"id":42,"first_name":"Newcomer"}]}}]}`)
						return
					}
					<-ctx.Done()
					io.WriteString(w, `{"ok":true,"result":[]}`)
				case "/bottest/sendMessage":
					if err := r.ParseMultipartForm(1 << 20); err != nil {
						t.Error(err)
						return
					}
					defer r.MultipartForm.RemoveAll()
					if tt.forbidden {
						w.WriteHeader(http.StatusForbidden)
						io.WriteString(w, `{"ok":false,"error_code":403,"description":"Forbidden: not enough rights"}`)
					} else {
						io.WriteString(w, `{"ok":true,"result":{"ephemeral_message_id":123,"chat":{"id":-1001,"type":"supergroup"},"receiver_user":{"id":42}}}`)
					}
					select {
					case sent <- r.Form:
					default:
						t.Error("unexpected extra message")
					}
				default:
					t.Errorf("unexpected API call: %s", r.URL.Path)
					http.Error(w, "unexpected API call", http.StatusNotFound)
				}
			}))
			defer func() { cancel(); server.Close() }()
			var logs bytes.Buffer

			app := New(config.Config{
				TelegramBotToken: "test", MainChatID: -1001, AdminChatID: -2002,
				DatabasePath: filepath.Join(t.TempDir(), "bot.sqlite"),
				TemplatesDir: "../../templates", Location: time.UTC,
			}, slog.New(slog.NewTextHandler(&logs, nil)))
			app.migrationsDir = "../../migrations"
			app.telegramFactory = func(token string, handler tgbot.HandlerFunc) (telegramRunner, error) {
				return tg.New(token, func(ctx context.Context, client *tgbot.Bot, update *models.Update) {
					handler(ctx, client, update)
					select {
					case processed <- struct{}{}:
					case <-ctx.Done():
					}
				}, tgbot.WithServerURL(server.URL), tgbot.WithHTTPClient(time.Second, server.Client()))
			}
			done := make(chan error, 1)
			go func() { done <- app.Run(ctx) }()
			var form url.Values
			select {
			case form = <-sent:
			case err := <-done:
				t.Fatalf("app exited before welcome: %v", err)
			case <-ctx.Done():
				t.Fatal("timed out waiting for welcome")
			}
			for range 2 {
				select {
				case <-processed:
				case <-ctx.Done():
					t.Fatal("timed out waiting for join updates to finish")
				}
			}
			cancel()
			if err := <-done; !errors.Is(err, context.Canceled) {
				t.Fatalf("app exit = %v, want context.Canceled", err)
			}
			wantText := "👋 Привет! Рады тебя видеть 🖤\n\n" +
				"В чате есть два закрепа \u2014 прочитай их, пожалуйста.\n\n" +
				"Присылай интро-фотку и участвуй в движах, про них будут напоминалки.\n\n" +
				"*Важно*: на челлендж принимаются только свежие фотки \u2014 даты есть в закрепе. Если случайно прислал старую, пингани меня."
			for key, want := range map[string]string{
				"chat_id": "-1001", "text": wantText, "parse_mode": "Markdown",
				"ephemeral_message_parameters": `{"receiver_user_id":42}`,
			} {
				if got := form.Get(key); got != want {
					t.Errorf("%s = %q, want %q", key, got, want)
				}
			}
			if len(sent) != 0 {
				t.Fatal("duplicate welcome for the join service message")
			}

			if tt.forbidden {
				for _, want := range []string{"route telegram update", "send ephemeral telegram message to 42", "Forbidden: not enough rights"} {
					if !strings.Contains(logs.String(), want) {
						t.Errorf("logs missing %q: %s", want, logs.String())
					}
				}
			} else if strings.Contains(logs.String(), "level=ERROR") {
				t.Errorf("unexpected application error: %s", logs.String())
			}
		})
	}
}
