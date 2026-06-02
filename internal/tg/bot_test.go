package tg

import (
	"context"
	"errors"
	"testing"

	tgbot "github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
)

func TestRunnerGetsBotUsernameBeforeStarting(t *testing.T) {
	client := &MoqClient{
		GetMeFunc: func(context.Context) (*models.User, error) {
			return &models.User{
				ID:       42,
				IsBot:    true,
				Username: "PhotoChallengeBot",
			}, nil
		},
		StartFunc: func(context.Context) {},
	}
	runner := NewWithClient(client)

	if err := runner.Run(context.Background()); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if len(client.GetMeCalls()) != 1 {
		t.Fatal("GetMe was not called")
	}
	if len(client.StartCalls()) != 1 {
		t.Fatal("Start was not called")
	}
	if got := runner.Username(); got != "PhotoChallengeBot" {
		t.Fatalf("Username() = %q, want %q", got, "PhotoChallengeBot")
	}
}

func TestRunnerDoesNotFetchIdentityTwice(t *testing.T) {
	client := &MoqClient{
		GetMeFunc: func(context.Context) (*models.User, error) {
			return &models.User{
				ID:       42,
				IsBot:    true,
				Username: "PhotoChallengeBot",
			}, nil
		},
		StartFunc: func(context.Context) {},
	}
	runner := NewWithClient(client)

	if err := runner.EnsureIdentity(context.Background()); err != nil {
		t.Fatalf("EnsureIdentity() error = %v", err)
	}
	if err := runner.Run(context.Background()); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if got := len(client.GetMeCalls()); got != 1 {
		t.Fatalf("GetMe calls = %d, want 1", got)
	}
	if len(client.StartCalls()) != 1 {
		t.Fatal("Start was not called")
	}
}

func TestRunnerReturnsGetMeErrorWithoutStarting(t *testing.T) {
	wantErr := errors.New("telegram unavailable")
	client := &MoqClient{GetMeFunc: func(context.Context) (*models.User, error) {
		return nil, wantErr
	}}
	runner := NewWithClient(client)

	err := runner.Run(context.Background())
	if !errors.Is(err, wantErr) {
		t.Fatalf("Run() error = %v, want %v", err, wantErr)
	}
	if len(client.StartCalls()) != 0 {
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
	client := &MoqClient{
		SendMessageFunc: func(_ context.Context, _ *tgbot.SendMessageParams) (*models.Message, error) {
			return &models.Message{ID: 101}, nil
		},
		PinChatMessageFunc: func(context.Context, *tgbot.PinChatMessageParams) (bool, error) {
			return true, nil
		},
	}
	runner := NewWithClient(client)

	messageID, err := runner.SendMarkdown(context.Background(), -1001, "*hello*")
	if err != nil {
		t.Fatalf("SendMarkdown() error = %v", err)
	}
	if messageID != 101 {
		t.Fatalf("messageID = %d, want 101", messageID)
	}
	sendCalls := client.SendMessageCalls()
	if len(sendCalls) != 1 || sendCalls[0].SendMessageParams.ChatID != int64(-1001) || sendCalls[0].SendMessageParams.Text != "*hello*" {
		t.Fatalf("send calls = %#v, want markdown message", sendCalls)
	}
	if sendCalls[0].SendMessageParams.ParseMode != models.ParseModeMarkdownV1 {
		t.Fatalf("ParseMode = %q, want Markdown", sendCalls[0].SendMessageParams.ParseMode)
	}

	if err := runner.Pin(context.Background(), -1001, messageID); err != nil {
		t.Fatalf("Pin() error = %v", err)
	}
	pinCalls := client.PinChatMessageCalls()
	if len(pinCalls) != 1 || pinCalls[0].PinChatMessageParams.ChatID != int64(-1001) || pinCalls[0].PinChatMessageParams.MessageID != 101 {
		t.Fatalf("pin calls = %#v, want message 101", pinCalls)
	}
	if pinCalls[0].PinChatMessageParams.DisableNotification {
		t.Fatal("DisableNotification = true, want notified pin")
	}
}

func TestRunnerSendsPlainTextMessage(t *testing.T) {
	client := &MoqClient{SendMessageFunc: func(_ context.Context, _ *tgbot.SendMessageParams) (*models.Message, error) {
		return &models.Message{ID: 102}, nil
	}}
	runner := NewWithClient(client)

	messageID, err := runner.SendText(context.Background(), -1001, "plain_text")
	if err != nil {
		t.Fatalf("SendText() error = %v", err)
	}
	if messageID != 102 {
		t.Fatalf("messageID = %d, want 102", messageID)
	}
	sendCalls := client.SendMessageCalls()
	if sendCalls[0].SendMessageParams.ParseMode != "" {
		t.Fatalf("ParseMode = %q, want empty", sendCalls[0].SendMessageParams.ParseMode)
	}
}

func TestRunnerSendsPlainTextReply(t *testing.T) {
	client := &MoqClient{SendMessageFunc: func(_ context.Context, _ *tgbot.SendMessageParams) (*models.Message, error) {
		return &models.Message{ID: 103}, nil
	}}
	runner := NewWithClient(client)

	messageID, err := runner.SendTextReply(context.Background(), -1001, "accepted", 77)
	if err != nil {
		t.Fatalf("SendTextReply() error = %v", err)
	}
	if messageID != 103 {
		t.Fatalf("messageID = %d, want 103", messageID)
	}
	sendCalls := client.SendMessageCalls()
	if len(sendCalls) != 1 {
		t.Fatalf("send calls = %d, want 1", len(sendCalls))
	}
	reply := sendCalls[0].SendMessageParams.ReplyParameters
	if reply == nil || reply.MessageID != 77 {
		t.Fatalf("ReplyParameters = %#v, want message 77", reply)
	}
	if sendCalls[0].SendMessageParams.ParseMode != "" {
		t.Fatalf("ParseMode = %q, want empty", sendCalls[0].SendMessageParams.ParseMode)
	}
}

func TestRunnerSendsMarkdownPhoto(t *testing.T) {
	client := &MoqClient{SendPhotoFunc: func(_ context.Context, _ *tgbot.SendPhotoParams) (*models.Message, error) {
		return &models.Message{ID: 104}, nil
	}}
	runner := NewWithClient(client)

	messageID, err := runner.SendMarkdownPhoto(context.Background(), -1001, "file-1", "*caption*")
	if err != nil {
		t.Fatalf("SendMarkdownPhoto() error = %v", err)
	}
	if messageID != 104 {
		t.Fatalf("messageID = %d, want 104", messageID)
	}
	sendCalls := client.SendPhotoCalls()
	if len(sendCalls) != 1 {
		t.Fatalf("send photo calls = %d, want 1", len(sendCalls))
	}
	params := sendCalls[0].SendPhotoParams
	if params.ChatID != int64(-1001) || params.Caption != "*caption*" {
		t.Fatalf("SendPhotoParams = %#v, want markdown photo caption", params)
	}
	if params.ParseMode != models.ParseModeMarkdownV1 {
		t.Fatalf("ParseMode = %q, want Markdown", params.ParseMode)
	}
}

func TestRunnerSendsMarkdownPhotoGroup(t *testing.T) {
	client := &MoqClient{SendMediaGroupFunc: func(_ context.Context, _ *tgbot.SendMediaGroupParams) ([]*models.Message, error) {
		return []*models.Message{{ID: 201}, {ID: 202}}, nil
	}}
	runner := NewWithClient(client)

	messageID, err := runner.SendMarkdownPhotoGroup(context.Background(), -1001, []string{"file-1", "file-2"}, []string{"*first*", "second"})
	if err != nil {
		t.Fatalf("SendMarkdownPhotoGroup() error = %v", err)
	}
	if messageID != 201 {
		t.Fatalf("messageID = %d, want first media message id", messageID)
	}
	sendCalls := client.SendMediaGroupCalls()
	if len(sendCalls) != 1 {
		t.Fatalf("send media group calls = %d, want 1", len(sendCalls))
	}
	params := sendCalls[0].SendMediaGroupParams
	if params.ChatID != int64(-1001) || len(params.Media) != 2 {
		t.Fatalf("SendMediaGroupParams = %#v, want two media items", params)
	}
	first, ok := params.Media[0].(*models.InputMediaPhoto)
	if !ok {
		t.Fatalf("first media = %T, want photo", params.Media[0])
	}
	if first.Media != "file-1" || first.Caption != "*first*" || first.ParseMode != models.ParseModeMarkdownV1 {
		t.Fatalf("first media = %#v, want markdown photo", first)
	}
	second, ok := params.Media[1].(*models.InputMediaPhoto)
	if !ok {
		t.Fatalf("second media = %T, want photo", params.Media[1])
	}
	if second.Media != "file-2" || second.Caption != "second" || second.ParseMode != models.ParseModeMarkdownV1 {
		t.Fatalf("second media = %#v, want markdown photo", second)
	}
}
