package bot

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	"github.com/go-telegram/bot/models"
)

func TestRouterWelcomesOnlyJoiningPeople(t *testing.T) {
	for _, tt := range []struct {
		name, oldStatus, newStatus string
		chatID                     int64
		isBot, wantWelcome         bool
	}{
		{"join", `"status":"left"`, `"status":"member"`, 1001, false, true},
		{"rejoin after ban", `"status":"kicked"`, `"status":"member"`, 1001, false, true},
		{"join restricted", `"status":"left"`, `"status":"restricted","is_member":true`, 1001, false, true},
		{"restricted rejoin", `"status":"restricted","is_member":false`, `"status":"restricted","is_member":true`, 1001, false, true},
		{"join as admin", `"status":"left"`, `"status":"administrator"`, 1001, false, true},
		{"promotion", `"status":"member"`, `"status":"administrator"`, 1001, false, false},
		{"demotion", `"status":"administrator"`, `"status":"member"`, 1001, false, false},
		{"ownership transfer", `"status":"creator"`, `"status":"administrator"`, 1001, false, false},
		{"restriction lifted", `"status":"restricted","is_member":true`, `"status":"member"`, 1001, false, false},
		{"leaving", `"status":"member"`, `"status":"left"`, 1001, false, false},
		{"unban without join", `"status":"kicked"`, `"status":"left"`, 1001, false, false},
		{"unknown previous status", `"status":"future_status"`, `"status":"member"`, 1001, false, false},
		{"unknown new status", `"status":"left"`, `"status":"future_status"`, 1001, false, false},
		{"bot", `"status":"left"`, `"status":"member"`, 1001, true, false},
		{"admin chat", `"status":"left"`, `"status":"member"`, 2002, false, false},
		{"other chat", `"status":"left"`, `"status":"member"`, 3003, false, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var update models.Update
			data := fmt.Sprintf(`{"chat_member":{"chat":{"id":%d,"type":"supergroup"},"from":{"id":99},"old_chat_member":{%s,"user":{"id":42,"is_bot":%t}},"new_chat_member":{%s,"user":{"id":42,"is_bot":%t}}}}`, tt.chatID, tt.oldStatus, tt.isBot, tt.newStatus, tt.isBot)
			if err := json.Unmarshal([]byte(data), &update); err != nil {
				t.Fatal(err)
			}
			publisher := &MoqWelcomePublisher{}
			router := newTestRouter(newRouterDeps(nil))
			router.welcomePublisher = publisher
			if err := router.Route(context.Background(), &update); err != nil {
				t.Fatal(err)
			}
			calls := publisher.SendEphemeralMarkdownCalls()
			if !tt.wantWelcome {
				if len(calls) != 0 {
					t.Fatalf("unexpected welcome: %#v", calls)
				}
				return
			}
			if len(calls) != 1 {
				t.Fatalf("welcome calls = %d, want 1", len(calls))
			}
			if calls[0].N != tt.chatID || calls[0].N1 != 42 {
				t.Fatalf("welcome target = (%d, %d), want (%d, 42)", calls[0].N, calls[0].N1, tt.chatID)
			}
		})
	}
}

func TestRouterReportsWelcomeFailure(t *testing.T) {
	wantErr := errors.New("telegram unavailable")
	router := newTestRouter(newRouterDeps(nil))
	router.welcomePublisher = &MoqWelcomePublisher{SendEphemeralMarkdownFunc: func(context.Context, int64, int64, string) error {
		return wantErr
	}}
	var reported error
	router.onError = func(_ context.Context, _ *models.Update, err error) { reported = err }
	var update models.Update
	if err := json.Unmarshal([]byte(`{"chat_member":{"chat":{"id":1001},"old_chat_member":{"status":"left","user":{"id":42}},"new_chat_member":{"status":"member","user":{"id":42}}}}`), &update); err != nil {
		t.Fatal(err)
	}
	router.HandlerFunc()(context.Background(), nil, &update)
	if !errors.Is(reported, wantErr) {
		t.Fatalf("reported error = %v, want %v", reported, wantErr)
	}
}

func TestNewRouterRejectsNilWelcomePublisher(t *testing.T) {
	for _, publisher := range []welcomePublisher{nil, (*MoqWelcomePublisher)(nil)} {
		func() {
			defer func() {
				if recover() == nil {
					t.Error("NewRouter did not panic for nil welcome publisher")
				}
			}()
			deps := newRouterDeps(nil)
			NewRouter(Config{
				BotUsername:     func() string { return "PhotoChallengeBot" },
				MainChatHandler: deps.main, AdminChatHandler: deps.admin,
				PrivateStartHandler: deps.privateStart, CallbackHandler: deps.callback,
				WelcomePublisher: publisher, OnError: func(context.Context, *models.Update, error) {},
			})
		}()
	}
}
