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
	deps := newRouterDeps(nil)
	router := newTestRouter(deps)

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
	if !reflect.DeepEqual(deps.calls, want) {
		t.Fatalf("calls = %v, want %v", deps.calls, want)
	}
}

func TestNewRouterPanicsOnNilErrorHandler(t *testing.T) {
	deps := newRouterDeps(nil)
	defer func() {
		if recover() == nil {
			t.Fatal("NewRouter() did not panic")
		}
	}()
	NewRouter(Config{
		MainChatID:          1001,
		AdminChatID:         2002,
		BotUsername:         func() string { return "PhotoChallengeBot" },
		MainChatHandler:     deps.main,
		AdminChatHandler:    deps.admin,
		PrivateStartHandler: deps.privateStart,
		CallbackHandler:     deps.callback,
	})
}

func TestNewRouterPanicsOnTypedNilMainChatHandler(t *testing.T) {
	var mainChatHandler *MoqMainChatHandler
	deps := newRouterDeps(nil)
	defer func() {
		if recover() == nil {
			t.Fatal("NewRouter() did not panic")
		}
	}()
	NewRouter(Config{
		MainChatID:          1001,
		AdminChatID:         2002,
		BotUsername:         func() string { return "PhotoChallengeBot" },
		MainChatHandler:     mainChatHandler,
		AdminChatHandler:    deps.admin,
		PrivateStartHandler: deps.privateStart,
		CallbackHandler:     deps.callback,
		OnError:             func(context.Context, *models.Update, error) {},
	})
}

func TestRouterDispatchesAdminChatMessage(t *testing.T) {
	deps := newRouterDeps(nil)
	router := newTestRouter(deps)

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
	if !reflect.DeepEqual(deps.calls, want) {
		t.Fatalf("calls = %v, want %v", deps.calls, want)
	}
}

func TestRouterDispatchesPrivateStartPayload(t *testing.T) {
	deps := newRouterDeps(nil)
	router := newTestRouter(deps)

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
	if !reflect.DeepEqual(deps.calls, want) {
		t.Fatalf("calls = %v, want %v", deps.calls, want)
	}
}

func TestRouterDispatchesMentionedPrivateStartPayload(t *testing.T) {
	deps := newRouterDeps(nil)
	router := newTestRouter(deps)

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
	if !reflect.DeepEqual(deps.calls, want) {
		t.Fatalf("calls = %v, want %v", deps.calls, want)
	}
}

func TestRouterIgnoresPrivateStartMentionedToOtherBot(t *testing.T) {
	deps := newRouterDeps(nil)
	router := newTestRouter(deps)

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
	if len(deps.calls) != 0 {
		t.Fatalf("calls = %v, want no calls", deps.calls)
	}
}

func TestRouterDispatchesCallbackQueryBeforeMessage(t *testing.T) {
	deps := newRouterDeps(nil)
	router := newTestRouter(deps)

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
	if !reflect.DeepEqual(deps.calls, want) {
		t.Fatalf("calls = %v, want %v", deps.calls, want)
	}
}

func TestRouterIgnoresUnsupportedUpdates(t *testing.T) {
	deps := newRouterDeps(nil)
	router := newTestRouter(deps)

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

	if len(deps.calls) != 0 {
		t.Fatalf("calls = %v, want no calls", deps.calls)
	}
}

func TestRouterReturnsHandlerError(t *testing.T) {
	wantErr := errors.New("handler failed")
	deps := newRouterDeps(wantErr)
	router := newTestRouter(deps)

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
	deps := newRouterDeps(wantErr)
	var gotErr error
	router := NewRouter(Config{
		MainChatID:  1001,
		AdminChatID: 2002,
		BotUsername: func() string {
			return "PhotoChallengeBot"
		},
		MainChatHandler:     deps.main,
		AdminChatHandler:    deps.admin,
		PrivateStartHandler: deps.privateStart,
		CallbackHandler:     deps.callback,
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
	var calls []string
	first := &MoqMainChatHandler{HandleMainChatMessageFunc: func(_ context.Context, message *models.Message) error {
		calls = append(calls, "first")
		return nil
	}}
	second := &MoqMainChatHandler{HandleMainChatMessageFunc: func(_ context.Context, message *models.Message) error {
		calls = append(calls, "second")
		return nil
	}}
	handlers := MainChatHandlers{first, second}
	message := &models.Message{ID: 21}

	if err := handlers.HandleMainChatMessage(context.Background(), message); err != nil {
		t.Fatalf("HandleMainChatMessage() error = %v", err)
	}
	if !reflect.DeepEqual(calls, []string{"first", "second"}) {
		t.Fatalf("calls = %v, want both handlers", calls)
	}
}

func TestMainChatHandlersStopOnError(t *testing.T) {
	wantErr := errors.New("main failed")
	first := &MoqMainChatHandler{HandleMainChatMessageFunc: func(context.Context, *models.Message) error {
		return wantErr
	}}
	second := &MoqMainChatHandler{}
	handlers := MainChatHandlers{first, second}

	err := handlers.HandleMainChatMessage(context.Background(), &models.Message{ID: 22})
	if !errors.Is(err, wantErr) {
		t.Fatalf("HandleMainChatMessage() error = %v, want %v", err, wantErr)
	}
	if len(second.HandleMainChatMessageCalls()) != 0 {
		t.Fatalf("second calls = %#v, want none after error", second.HandleMainChatMessageCalls())
	}
}

type routerDeps struct {
	calls        []string
	err          error
	main         *MoqMainChatHandler
	admin        *MoqAdminChatHandler
	privateStart *MoqPrivateStartHandler
	callback     *MoqCallbackQueryHandler
}

func newRouterDeps(err error) *routerDeps {
	deps := &routerDeps{err: err}
	deps.main = &MoqMainChatHandler{HandleMainChatMessageFunc: func(_ context.Context, message *models.Message) error {
		deps.calls = append(deps.calls, "main:"+strconv.Itoa(message.ID))
		return deps.err
	}}
	deps.admin = &MoqAdminChatHandler{HandleAdminChatMessageFunc: func(_ context.Context, message *models.Message) error {
		deps.calls = append(deps.calls, "admin:"+strconv.Itoa(message.ID))
		return deps.err
	}}
	deps.privateStart = &MoqPrivateStartHandler{HandlePrivateStartFunc: func(_ context.Context, _ *models.Message, payload string) error {
		deps.calls = append(deps.calls, "start:"+payload)
		return deps.err
	}}
	deps.callback = &MoqCallbackQueryHandler{HandleCallbackQueryFunc: func(_ context.Context, query *models.CallbackQuery) error {
		deps.calls = append(deps.calls, "callback:"+query.ID)
		return deps.err
	}}
	return deps
}

func newTestRouter(deps *routerDeps) *Router {
	return NewRouter(Config{
		MainChatID:  1001,
		AdminChatID: 2002,
		BotUsername: func() string {
			return "PhotoChallengeBot"
		},
		MainChatHandler:     deps.main,
		AdminChatHandler:    deps.admin,
		PrivateStartHandler: deps.privateStart,
		CallbackHandler:     deps.callback,
		OnError:             func(context.Context, *models.Update, error) {},
	})
}
