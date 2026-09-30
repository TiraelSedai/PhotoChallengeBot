package tg

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	tgbot "github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
)

func TestRunnerSendsMultipleAnswerPollThroughTelegramSDK(t *testing.T) {
	const question = "Выбираем тему нового челленджа. Голосуем до пятницы, можно за несколько✨"
	options := []string{"осенний город", "стрит", "ложный масштаб"}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/bot123:test/sendPoll" {
			t.Errorf("request = %s %s, want POST sendPoll", r.Method, r.URL.Path)
		}
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Errorf("parse request: %v", err)
			http.Error(w, "invalid request", http.StatusBadRequest)
			return
		}
		defer r.MultipartForm.RemoveAll()
		for field, want := range map[string]string{
			"chat_id":                 "-1001234",
			"question":                question,
			"is_anonymous":            "true",
			"type":                    "regular",
			"allows_multiple_answers": "true",
		} {
			if got := r.FormValue(field); got != want {
				t.Errorf("%s = %q, want %q", field, got, want)
			}
		}
		var gotOptions []models.InputPollOption
		if err := json.Unmarshal([]byte(r.FormValue("options")), &gotOptions); err != nil {
			t.Errorf("decode options: %v", err)
		}
		gotTexts := make([]string, len(gotOptions))
		for idx, option := range gotOptions {
			gotTexts[idx] = option.Text
		}
		if !slices.Equal(gotTexts, options) {
			t.Errorf("options = %q, want %q", gotTexts, options)
		}
		if r.FormValue("close_date") != "" || r.FormValue("open_period") != "" {
			t.Error("poll unexpectedly has an automatic closing time")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"result":{"message_id":105}}`))
	}))
	t.Cleanup(server.Close)
	runner, err := New("123:test", func(context.Context, *tgbot.Bot, *models.Update) {}, tgbot.WithServerURL(server.URL))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	messageID, err := runner.SendPoll(context.Background(), -1001234, question, options)
	if err != nil || messageID != 105 {
		t.Fatalf("SendPoll() = (%d, %v), want (105, nil)", messageID, err)
	}
}

func TestRunnerReturnsSendPollErrors(t *testing.T) {
	wantErr := errors.New("telegram unavailable")
	for _, test := range []struct {
		name    string
		err     error
		wantMsg string
	}{
		{name: "API error", err: wantErr, wantMsg: "send telegram poll: telegram unavailable"},
		{name: "empty response", wantMsg: "send telegram poll: empty response"},
	} {
		t.Run(test.name, func(t *testing.T) {
			client := &MoqClient{SendPollFunc: func(context.Context, *tgbot.SendPollParams) (*models.Message, error) {
				return nil, test.err
			}}
			runner := NewWithClient(client)
			messageID, err := runner.SendPoll(context.Background(), -1001234, "Choose a theme", []string{"city", "forest"})
			if messageID != 0 || err == nil || !strings.Contains(err.Error(), test.wantMsg) {
				t.Fatalf("SendPoll() = (%d, %v), want (0, %q)", messageID, err, test.wantMsg)
			}
			if test.err != nil && !errors.Is(err, test.err) {
				t.Fatalf("SendPoll() error = %v, want wrapped %v", err, test.err)
			}
		})
	}
}
