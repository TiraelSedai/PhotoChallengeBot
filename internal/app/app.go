package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math/rand"
	"time"

	"github.com/TiraelSedai/PhotoChallengeBot/internal/admin"
	"github.com/TiraelSedai/PhotoChallengeBot/internal/bot"
	"github.com/TiraelSedai/PhotoChallengeBot/internal/challenge"
	"github.com/TiraelSedai/PhotoChallengeBot/internal/config"
	"github.com/TiraelSedai/PhotoChallengeBot/internal/db"
	"github.com/TiraelSedai/PhotoChallengeBot/internal/photo"
	"github.com/TiraelSedai/PhotoChallengeBot/internal/repository"
	"github.com/TiraelSedai/PhotoChallengeBot/internal/require"
	"github.com/TiraelSedai/PhotoChallengeBot/internal/results"
	"github.com/TiraelSedai/PhotoChallengeBot/internal/scheduler"
	"github.com/TiraelSedai/PhotoChallengeBot/internal/templates"
	"github.com/TiraelSedai/PhotoChallengeBot/internal/tg"
	"github.com/TiraelSedai/PhotoChallengeBot/internal/topic"
	"github.com/TiraelSedai/PhotoChallengeBot/internal/vote"
	tgbot "github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
)

type telegramRunner interface {
	EnsureIdentity(context.Context) error
	Run(context.Context) error
	SendMarkdown(context.Context, int64, string) (int, error)
	SendText(context.Context, int64, string) (int, error)
	SendTextReply(context.Context, int64, string, int) (int, error)
	SendPhoto(context.Context, int64, string, string, *models.InlineKeyboardMarkup) (int, error)
	EditPhoto(context.Context, int64, int, string, string, *models.InlineKeyboardMarkup) error
	AnswerCallback(context.Context, string, string) error
	Pin(context.Context, int64, int) error
	Username() string
}

type App struct {
	config          config.Config
	logger          *slog.Logger
	migrationsDir   string
	telegramFactory func(string, tgbot.HandlerFunc) (telegramRunner, error)
}

func New(cfg config.Config, logger *slog.Logger) *App {
	require.NotNil("logger", logger)
	require.NotNil("location", cfg.Location)
	return &App{
		config:        cfg,
		logger:        logger,
		migrationsDir: "migrations",
		telegramFactory: func(token string, handler tgbot.HandlerFunc) (telegramRunner, error) {
			return tg.New(token, handler)
		},
	}
}

func (a *App) Run(ctx context.Context) error {
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	a.logger.Info("starting photo challenge bot", "main_chat_id", a.config.MainChatID)
	database, err := db.Open(runCtx, db.Options{
		Path:          a.config.DatabasePath,
		MigrationsDir: a.migrationsDir,
	})
	if err != nil {
		return err
	}
	defer database.Close()

	renderer, err := templates.Load(a.config.TemplatesDir)
	if err != nil {
		return err
	}

	var router *bot.Router
	telegramRunner, err := a.telegramFactory(a.config.TelegramBotToken, func(ctx context.Context, _ *tgbot.Bot, update *models.Update) {
		if router == nil {
			a.logger.Error("route telegram update", "error", "router is not initialized")
			return
		}
		if err := router.Route(ctx, update); err != nil {
			a.logger.Error("route telegram update", "error", err)
		}
	})
	if err != nil {
		return err
	}
	if err := telegramRunner.EnsureIdentity(runCtx); err != nil {
		return err
	}

	now := func() time.Time { return time.Now().UTC() }
	challenges := repository.NewChallenges(database)
	users := repository.NewUsers(database)
	photos := repository.NewPhotos(database)
	votes := repository.NewVotes(database)
	topicSuggestions := repository.NewTopicSuggestions(database)
	resultsPublisher := results.NewPublisher(results.PublishConfig{
		Challenges: challenges,
		Photos:     photos,
		Votes:      votes,
		Users:      users,
		Renderer:   renderer,
		Publisher:  telegramRunner,
		Now:        now,
	})
	topicReporter := topic.NewReporter(topic.ReportConfig{
		AdminChatID: a.config.AdminChatID,
		Challenges:  challenges,
		Suggestions: topicSuggestions,
		Users:       users,
		Publisher:   telegramRunner,
		Now:         now,
	})
	createChallengeHandler := admin.NewCreateChallengeHandler(admin.CreateChallengeConfig{
		AdminChatID:   a.config.AdminChatID,
		MainChatID:    a.config.MainChatID,
		Location:      a.config.Location,
		Sessions:      repository.NewAdminSessions(database),
		Users:         users,
		Challenges:    challenge.NewService(challenges, a.config.Location, now),
		Announcements: challenges,
		Renderer:      renderer,
		Publisher:     telegramRunner,
		BotUsername:   telegramRunner.Username,
	})
	deletePhotoHandler := admin.NewDeletePhotoHandler(admin.DeletePhotoConfig{
		AdminChatID: a.config.AdminChatID,
		MainChatID:  a.config.MainChatID,
		Challenges:  challenges,
		Photos:      photos,
		Publisher:   telegramRunner,
		BotUsername: telegramRunner.Username,
	})
	finishVoteHandler := admin.NewFinishVoteHandler(admin.FinishVoteConfig{
		AdminChatID: a.config.AdminChatID,
		MainChatID:  a.config.MainChatID,
		Challenges:  challenges,
		Publisher:   telegramRunner,
		Results:     resultsPublisher,
		Topics:      topicReporter,
		BotUsername: telegramRunner.Username,
		Now:         now,
	})
	adminHandler := admin.NewRouter(admin.RouterConfig{
		DeletePhoto:     deletePhotoHandler,
		FinishVote:      finishVoteHandler,
		CreateChallenge: createChallengeHandler,
	})
	photoHandler := photo.NewService(photo.Config{
		MainChatID: a.config.MainChatID,
		Challenges: challenges,
		Users:      users,
		Photos:     photos,
		Publisher:  telegramRunner,
		Now:        now,
	})
	topicHandler := topic.NewService(topic.Config{
		MainChatID:  a.config.MainChatID,
		Challenges:  challenges,
		Users:       users,
		Suggestions: topicSuggestions,
		Now:         now,
	})
	voteHandler := vote.NewService(vote.Config{
		Challenges: challenges,
		Users:      users,
		Photos:     photos,
		Votes:      votes,
		Publisher:  telegramRunner,
		Now:        now,
		Rand:       rand.New(rand.NewSource(time.Now().UnixNano())),
	})

	router = bot.NewRouter(bot.Config{
		MainChatID:          a.config.MainChatID,
		AdminChatID:         a.config.AdminChatID,
		BotUsername:         telegramRunner.Username,
		MainChatHandler:     bot.MainChatHandlers{topicHandler, photoHandler},
		AdminChatHandler:    adminHandler,
		PrivateStartHandler: voteHandler,
		CallbackHandler:     voteHandler,
		OnError: func(ctx context.Context, update *models.Update, err error) {
			a.logger.ErrorContext(ctx, "route telegram update", "error", err)
		},
	})

	schedulerLoop := scheduler.New(scheduler.Config{
		MainChatID:  a.config.MainChatID,
		Challenges:  challenges,
		Photos:      photos,
		Renderer:    renderer,
		Results:     resultsPublisher,
		Topics:      topicReporter,
		Publisher:   telegramRunner,
		Logger:      a.logger,
		Now:         now,
		BotUsername: telegramRunner.Username,
		Location:    a.config.Location,
	})

	errc := make(chan error, 2)
	go func() {
		errc <- telegramRunner.Run(runCtx)
	}()
	go func() {
		errc <- schedulerLoop.Run(runCtx)
	}()

	remaining := 2
	var firstErr error
	select {
	case err := <-errc:
		if ctx.Err() == nil {
			if err != nil {
				firstErr = fmt.Errorf("app runner exited unexpectedly: %w", err)
			} else {
				firstErr = errors.New("app runner exited unexpectedly")
			}
		} else {
			firstErr = err
		}
		if firstErr == nil && ctx.Err() == nil {
			firstErr = errors.New("app runner exited unexpectedly")
		}
		remaining--
	case <-ctx.Done():
		firstErr = ctx.Err()
	}

	cancel()
	for ; remaining > 0; remaining-- {
		err := <-errc
		if firstErr == nil && err != nil && !errors.Is(err, context.Canceled) {
			firstErr = err
		}
	}

	if ctx.Err() != nil {
		a.logger.Info("stopping photo challenge bot")
		return ctx.Err()
	}
	if firstErr != nil {
		return firstErr
	}
	return nil
}
