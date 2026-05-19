package tg

import (
	"context"
	"errors"
	"fmt"
	"sync"

	tgbot "github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
)

type Client interface {
	GetMe(context.Context) (*models.User, error)
	SendMessage(context.Context, *tgbot.SendMessageParams) (*models.Message, error)
	SendPhoto(context.Context, *tgbot.SendPhotoParams) (*models.Message, error)
	EditMessageMedia(context.Context, *tgbot.EditMessageMediaParams) (*models.Message, error)
	AnswerCallbackQuery(context.Context, *tgbot.AnswerCallbackQueryParams) (bool, error)
	PinChatMessage(context.Context, *tgbot.PinChatMessageParams) (bool, error)
	Start(context.Context)
}

type Runner struct {
	client   Client
	mu       sync.RWMutex
	username string
}

func New(token string, handler tgbot.HandlerFunc, options ...tgbot.Option) (*Runner, error) {
	opts := make([]tgbot.Option, 0, len(options)+2)
	opts = append(opts, options...)
	opts = append(opts,
		tgbot.WithDefaultHandler(handler),
		tgbot.WithSkipGetMe(),
	)

	client, err := tgbot.New(token, opts...)
	if err != nil {
		return nil, fmt.Errorf("create telegram bot: %w", err)
	}

	return NewWithClient(client), nil
}

func NewWithClient(client Client) *Runner {
	if client == nil {
		panic("telegram client is nil")
	}
	return &Runner{client: client}
}

func (r *Runner) Run(ctx context.Context) error {
	if err := r.EnsureIdentity(ctx); err != nil {
		return err
	}
	r.client.Start(ctx)
	return nil
}

func (r *Runner) EnsureIdentity(ctx context.Context) error {
	r.mu.RLock()
	if r.username != "" {
		r.mu.RUnlock()
		return nil
	}
	r.mu.RUnlock()

	me, err := r.client.GetMe(ctx)
	if err != nil {
		return fmt.Errorf("get telegram bot identity: %w", err)
	}
	if me == nil {
		return errors.New("get telegram bot identity: empty response")
	}

	r.mu.Lock()
	r.username = me.Username
	r.mu.Unlock()
	return nil
}

func (r *Runner) Username() string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.username
}
