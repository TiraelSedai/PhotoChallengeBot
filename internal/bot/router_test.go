package bot

import (
	"context"
	"errors"
	"reflect"
	"strconv"
	"testing"

	"github.com/go-telegram/bot/models"
)

func TestRouterDispatchesMainChatMessage(t *testing.T) {
	recorder := &recordingHandlers{}
	router := newTestRouter(recorder)

	update := &models.Update{
		Message: &models.Message{
			ID:   10,
			Chat: models.Chat{ID: 1001, Type: models.ChatTypeSupergroup},
			Text: "photo",
		},
	}

	if err := router.Route(context.Background(), update); err != nil {
		t.Fatalf("Route() error = %v", err)
	}

	want := []string{"main:10"}
	if !reflect.DeepEqual(recorder.calls, want) {
		t.Fatalf("calls = %v, want %v", recorder.calls, want)
	}
}

func TestRouterDispatchesAdminChatMessage(t *testing.T) {
	recorder := &recordingHandlers{}
	router := newTestRouter(recorder)

	update := &models.Update{
		Message: &models.Message{
			ID:   11,
			Chat: models.Chat{ID: 2002, Type: models.ChatTypeGroup},
			Text: "/new",
		},
	}

	if err := router.Route(context.Background(), update); err != nil {
		t.Fatalf("Route() error = %v", err)
	}

	want := []string{"admin:11"}
	if !reflect.DeepEqual(recorder.calls, want) {
		t.Fatalf("calls = %v, want %v", recorder.calls, want)
	}
}

func TestRouterDispatchesPrivateStartPayload(t *testing.T) {
	recorder := &recordingHandlers{}
	router := newTestRouter(recorder)

	update := &models.Update{
		Message: &models.Message{
			ID:   12,
			Chat: models.Chat{ID: 3003, Type: models.ChatTypePrivate},
			Text: "/start 1001_42",
		},
	}

	if err := router.Route(context.Background(), update); err != nil {
		t.Fatalf("Route() error = %v", err)
	}

	want := []string{"start:1001_42"}
	if !reflect.DeepEqual(recorder.calls, want) {
		t.Fatalf("calls = %v, want %v", recorder.calls, want)
	}
}

func TestRouterDispatchesMentionedPrivateStartPayload(t *testing.T) {
	recorder := &recordingHandlers{}
	router := newTestRouter(recorder)

	update := &models.Update{
		Message: &models.Message{
			ID:   13,
			Chat: models.Chat{ID: 3003, Type: models.ChatTypePrivate},
			Text: "/start@PhotoChallengeBot 1001_42",
		},
	}

	if err := router.Route(context.Background(), update); err != nil {
		t.Fatalf("Route() error = %v", err)
	}

	want := []string{"start:1001_42"}
	if !reflect.DeepEqual(recorder.calls, want) {
		t.Fatalf("calls = %v, want %v", recorder.calls, want)
	}
}

func TestRouterIgnoresPrivateStartMentionedToOtherBot(t *testing.T) {
	recorder := &recordingHandlers{}
	router := newTestRouter(recorder)

	update := &models.Update{
		Message: &models.Message{
			ID:   18,
			Chat: models.Chat{ID: 3003, Type: models.ChatTypePrivate},
			Text: "/start@OtherBot 1001_42",
		},
	}

	if err := router.Route(context.Background(), update); err != nil {
		t.Fatalf("Route() error = %v", err)
	}
	if len(recorder.calls) != 0 {
		t.Fatalf("calls = %v, want no calls", recorder.calls)
	}
}

func TestRouterDispatchesCallbackQueryBeforeMessage(t *testing.T) {
	recorder := &recordingHandlers{}
	router := newTestRouter(recorder)

	update := &models.Update{
		Message: &models.Message{
			ID:   14,
			Chat: models.Chat{ID: 1001, Type: models.ChatTypeSupergroup},
		},
		CallbackQuery: &models.CallbackQuery{
			ID:   "callback-1",
			Data: "vote:next",
		},
	}

	if err := router.Route(context.Background(), update); err != nil {
		t.Fatalf("Route() error = %v", err)
	}

	want := []string{"callback:callback-1"}
	if !reflect.DeepEqual(recorder.calls, want) {
		t.Fatalf("calls = %v, want %v", recorder.calls, want)
	}
}

func TestRouterIgnoresUnsupportedUpdates(t *testing.T) {
	recorder := &recordingHandlers{}
	router := newTestRouter(recorder)

	update := &models.Update{
		Message: &models.Message{
			ID:   15,
			Chat: models.Chat{ID: 4004, Type: models.ChatTypeChannel},
			Text: "ignored",
		},
	}

	if err := router.Route(context.Background(), update); err != nil {
		t.Fatalf("Route() error = %v", err)
	}

	if len(recorder.calls) != 0 {
		t.Fatalf("calls = %v, want no calls", recorder.calls)
	}
}

func TestRouterReturnsHandlerError(t *testing.T) {
	wantErr := errors.New("handler failed")
	recorder := &recordingHandlers{err: wantErr}
	router := newTestRouter(recorder)

	update := &models.Update{
		Message: &models.Message{
			ID:   16,
			Chat: models.Chat{ID: 1001, Type: models.ChatTypeSupergroup},
		},
	}

	err := router.Route(context.Background(), update)
	if !errors.Is(err, wantErr) {
		t.Fatalf("Route() error = %v, want %v", err, wantErr)
	}
}

func TestHandlerFuncReportsRouteError(t *testing.T) {
	wantErr := errors.New("handler failed")
	recorder := &recordingHandlers{err: wantErr}
	var gotErr error
	router := NewRouter(Config{
		MainChatID:      1001,
		MainChatHandler: recorder,
		OnError: func(_ context.Context, _ *models.Update, err error) {
			gotErr = err
		},
	})

	update := &models.Update{
		Message: &models.Message{
			ID:   17,
			Chat: models.Chat{ID: 1001, Type: models.ChatTypeSupergroup},
		},
	}

	router.HandlerFunc()(context.Background(), nil, update)

	if !errors.Is(gotErr, wantErr) {
		t.Fatalf("reported error = %v, want %v", gotErr, wantErr)
	}
}

func TestMainChatHandlersRunInOrder(t *testing.T) {
	first := &recordingHandlers{}
	second := &recordingHandlers{}
	handlers := MainChatHandlers{first, second}
	message := &models.Message{ID: 21}

	if err := handlers.HandleMainChatMessage(context.Background(), message); err != nil {
		t.Fatalf("HandleMainChatMessage() error = %v", err)
	}
	if got := first.calls; !reflect.DeepEqual(got, []string{"main:21"}) {
		t.Fatalf("first calls = %v, want main call", got)
	}
	if got := second.calls; !reflect.DeepEqual(got, []string{"main:21"}) {
		t.Fatalf("second calls = %v, want main call", got)
	}
}

func TestMainChatHandlersStopOnError(t *testing.T) {
	wantErr := errors.New("main failed")
	first := &recordingHandlers{err: wantErr}
	second := &recordingHandlers{}
	handlers := MainChatHandlers{first, second}

	err := handlers.HandleMainChatMessage(context.Background(), &models.Message{ID: 22})
	if !errors.Is(err, wantErr) {
		t.Fatalf("HandleMainChatMessage() error = %v, want %v", err, wantErr)
	}
	if len(second.calls) != 0 {
		t.Fatalf("second calls = %v, want none after error", second.calls)
	}
}

type recordingHandlers struct {
	calls []string
	err   error
}

func newTestRouter(recorder *recordingHandlers) *Router {
	return NewRouter(Config{
		MainChatID:  1001,
		AdminChatID: 2002,
		BotUsername: func() string {
			return "PhotoChallengeBot"
		},
		MainChatHandler:     recorder,
		AdminChatHandler:    recorder,
		PrivateStartHandler: recorder,
		CallbackHandler:     recorder,
	})
}

func (h *recordingHandlers) HandleMainChatMessage(_ context.Context, message *models.Message) error {
	h.calls = append(h.calls, "main:"+strconv.Itoa(message.ID))
	return h.err
}

func (h *recordingHandlers) HandleAdminChatMessage(_ context.Context, message *models.Message) error {
	h.calls = append(h.calls, "admin:"+strconv.Itoa(message.ID))
	return h.err
}

func (h *recordingHandlers) HandlePrivateStart(_ context.Context, _ *models.Message, payload string) error {
	h.calls = append(h.calls, "start:"+payload)
	return h.err
}

func (h *recordingHandlers) HandleCallbackQuery(_ context.Context, query *models.CallbackQuery) error {
	h.calls = append(h.calls, "callback:"+query.ID)
	return h.err
}
