package tg

import (
	"context"
	"errors"
	"testing"

	tgbot "github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
)

func TestRunnerGetsBotUsernameBeforeStarting(t *testing.T) {
	client := &fakeClient{
		me: &models.User{
			ID:       42,
			IsBot:    true,
			Username: "PhotoChallengeBot",
		},
	}
	runner := NewWithClient(client)

	if err := runner.Run(context.Background()); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if !client.getMeCalled {
		t.Fatal("GetMe was not called")
	}
	if !client.startCalled {
		t.Fatal("Start was not called")
	}
	if got := runner.Username(); got != "PhotoChallengeBot" {
		t.Fatalf("Username() = %q, want %q", got, "PhotoChallengeBot")
	}
}

func TestRunnerDoesNotFetchIdentityTwice(t *testing.T) {
	client := &fakeClient{
		me: &models.User{
			ID:       42,
			IsBot:    true,
			Username: "PhotoChallengeBot",
		},
	}
	runner := NewWithClient(client)

	if err := runner.EnsureIdentity(context.Background()); err != nil {
		t.Fatalf("EnsureIdentity() error = %v", err)
	}
	if err := runner.Run(context.Background()); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if client.getMeCalls != 1 {
		t.Fatalf("GetMe calls = %d, want 1", client.getMeCalls)
	}
	if !client.startCalled {
		t.Fatal("Start was not called")
	}
}

func TestRunnerReturnsGetMeErrorWithoutStarting(t *testing.T) {
	wantErr := errors.New("telegram unavailable")
	client := &fakeClient{err: wantErr}
	runner := NewWithClient(client)

	err := runner.Run(context.Background())
	if !errors.Is(err, wantErr) {
		t.Fatalf("Run() error = %v, want %v", err, wantErr)
	}
	if client.startCalled {
		t.Fatal("Start was called after GetMe error")
	}
}

func TestRunnerRejectsNilClient(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("NewWithClient(nil) did not panic")
		}
	}()

	NewWithClient(nil)
}

func TestRunnerSendsAndPinsMarkdownMessage(t *testing.T) {
	client := &fakeClient{
		sentMessage: &models.Message{ID: 101},
		pinOK:       true,
	}
	runner := NewWithClient(client)

	messageID, err := runner.SendMarkdown(context.Background(), -1001, "*hello*")
	if err != nil {
		t.Fatalf("SendMarkdown() error = %v", err)
	}
	if messageID != 101 {
		t.Fatalf("messageID = %d, want 101", messageID)
	}
	if client.sendParams == nil || client.sendParams.ChatID != int64(-1001) || client.sendParams.Text != "*hello*" {
		t.Fatalf("send params = %#v, want markdown message", client.sendParams)
	}
	if client.sendParams.ParseMode != models.ParseModeMarkdownV1 {
		t.Fatalf("ParseMode = %q, want Markdown", client.sendParams.ParseMode)
	}

	if err := runner.Pin(context.Background(), -1001, messageID); err != nil {
		t.Fatalf("Pin() error = %v", err)
	}
	if client.pinParams == nil || client.pinParams.ChatID != int64(-1001) || client.pinParams.MessageID != 101 {
		t.Fatalf("pin params = %#v, want message 101", client.pinParams)
	}
}

func TestRunnerSendsPlainTextMessage(t *testing.T) {
	client := &fakeClient{
		sentMessage: &models.Message{ID: 102},
	}
	runner := NewWithClient(client)

	messageID, err := runner.SendText(context.Background(), -1001, "plain_text")
	if err != nil {
		t.Fatalf("SendText() error = %v", err)
	}
	if messageID != 102 {
		t.Fatalf("messageID = %d, want 102", messageID)
	}
	if client.sendParams.ParseMode != "" {
		t.Fatalf("ParseMode = %q, want empty", client.sendParams.ParseMode)
	}
}

type fakeClient struct {
	me           *models.User
	err          error
	sentMessage  *models.Message
	sentPhoto    *models.Message
	sendParams   *tgbot.SendMessageParams
	photoParams  *tgbot.SendPhotoParams
	editParams   *tgbot.EditMessageMediaParams
	answerOK     bool
	answerParams *tgbot.AnswerCallbackQueryParams
	pinOK        bool
	pinParams    *tgbot.PinChatMessageParams
	getMeCalled  bool
	getMeCalls   int
	startCalled  bool
}

func (c *fakeClient) GetMe(context.Context) (*models.User, error) {
	c.getMeCalled = true
	c.getMeCalls++
	return c.me, c.err
}

func (c *fakeClient) SendMessage(_ context.Context, params *tgbot.SendMessageParams) (*models.Message, error) {
	c.sendParams = params
	return c.sentMessage, c.err
}

func (c *fakeClient) SendPhoto(_ context.Context, params *tgbot.SendPhotoParams) (*models.Message, error) {
	c.photoParams = params
	return c.sentPhoto, c.err
}

func (c *fakeClient) EditMessageMedia(_ context.Context, params *tgbot.EditMessageMediaParams) (*models.Message, error) {
	c.editParams = params
	return &models.Message{ID: params.MessageID}, c.err
}

func (c *fakeClient) AnswerCallbackQuery(_ context.Context, params *tgbot.AnswerCallbackQueryParams) (bool, error) {
	c.answerParams = params
	return c.answerOK, c.err
}

func (c *fakeClient) PinChatMessage(_ context.Context, params *tgbot.PinChatMessageParams) (bool, error) {
	c.pinParams = params
	return c.pinOK, c.err
}

func (c *fakeClient) Start(context.Context) {
	c.startCalled = true
}
