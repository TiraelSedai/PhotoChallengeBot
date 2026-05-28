package bot

import (
	"context"
	"strings"

	"github.com/TiraelSedai/PhotoChallengeBot/internal/require"
	tgbot "github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
)

type MainChatHandler interface {
	HandleMainChatMessage(context.Context, *models.Message) error
}

type MainChatHandlers []MainChatHandler

func (h MainChatHandlers) HandleMainChatMessage(ctx context.Context, message *models.Message) error {
	for _, handler := range h {
		if err := handler.HandleMainChatMessage(ctx, message); err != nil {
			return err
		}
	}
	return nil
}

type AdminChatHandler interface {
	HandleAdminChatMessage(context.Context, *models.Message) error
}

type PrivateStartHandler interface {
	HandlePrivateStart(context.Context, *models.Message, string) error
}

type CallbackQueryHandler interface {
	HandleCallbackQuery(context.Context, *models.CallbackQuery) error
}

type ErrorHandler func(context.Context, *models.Update, error)

type Config struct {
	MainChatID          int64
	AdminChatID         int64
	BotUsername         func() string
	MainChatHandler     MainChatHandler
	AdminChatHandler    AdminChatHandler
	PrivateStartHandler PrivateStartHandler
	CallbackHandler     CallbackQueryHandler
	OnError             ErrorHandler
}

type Router struct {
	mainChatID          int64
	adminChatID         int64
	botUsername         func() string
	mainChatHandler     MainChatHandler
	adminChatHandler    AdminChatHandler
	privateStartHandler PrivateStartHandler
	callbackHandler     CallbackQueryHandler
	onError             ErrorHandler
}

func NewRouter(cfg Config) *Router {
	require.NotNil("bot username provider", cfg.BotUsername)
	require.NotNil("main chat handler", cfg.MainChatHandler)
	require.NotNil("admin chat handler", cfg.AdminChatHandler)
	require.NotNil("private start handler", cfg.PrivateStartHandler)
	require.NotNil("callback handler", cfg.CallbackHandler)
	require.NotNil("error handler", cfg.OnError)
	return &Router{
		mainChatID:          cfg.MainChatID,
		adminChatID:         cfg.AdminChatID,
		botUsername:         cfg.BotUsername,
		mainChatHandler:     cfg.MainChatHandler,
		adminChatHandler:    cfg.AdminChatHandler,
		privateStartHandler: cfg.PrivateStartHandler,
		callbackHandler:     cfg.CallbackHandler,
		onError:             cfg.OnError,
	}
}

func (r *Router) HandlerFunc() tgbot.HandlerFunc {
	return func(ctx context.Context, _ *tgbot.Bot, update *models.Update) {
		if err := r.Route(ctx, update); err != nil {
			r.onError(ctx, update, err)
		}
	}
}

func (r *Router) Route(ctx context.Context, update *models.Update) error {
	if update == nil {
		return nil
	}

	if update.CallbackQuery != nil {
		return r.callbackHandler.HandleCallbackQuery(ctx, update.CallbackQuery)
	}

	if update.Message == nil {
		return nil
	}

	message := update.Message
	if isPrivateStart(message, r.currentBotUsername()) {
		return r.privateStartHandler.HandlePrivateStart(ctx, message, startPayload(message.Text, r.currentBotUsername()))
	}

	switch message.Chat.ID {
	case r.adminChatID:
		return r.adminChatHandler.HandleAdminChatMessage(ctx, message)
	case r.mainChatID:
		return r.mainChatHandler.HandleMainChatMessage(ctx, message)
	default:
		return nil
	}
}

func (r *Router) currentBotUsername() string {
	return r.botUsername()
}

func isPrivateStart(message *models.Message, botUsername string) bool {
	if message == nil || message.Chat.Type != models.ChatTypePrivate {
		return false
	}
	return startPayload(message.Text, botUsername) != ""
}

func startPayload(text string, botUsername string) string {
	fields := strings.Fields(text)
	if len(fields) != 2 {
		return ""
	}

	command := fields[0]
	if command == "/start" {
		return fields[1]
	}
	if after, ok := strings.CutPrefix(command, "/start@"); ok {
		username := after
		if username != "" && strings.EqualFold(username, strings.TrimPrefix(botUsername, "@")) {
			return fields[1]
		}
	}
	return ""
}
