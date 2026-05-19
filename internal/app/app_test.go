package app

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"path/filepath"
	"testing"
	"time"

	"github.com/TiraelSedai/PhotoChallengeBot/internal/config"
	appdb "github.com/TiraelSedai/PhotoChallengeBot/internal/db"
	tgbot "github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
)

func TestRunReturnsContextCancellation(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	cfg := config.Config{
		MainChatID:   -1001,
		AdminChatID:  -2002,
		DatabasePath: filepath.Join(t.TempDir(), "bot.sqlite"),
		TemplatesDir: filepath.Join("..", "..", "templates"),
	}
	app := New(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
	app.migrationsDir = "../../migrations"
	app.telegramFactory = func(string, tgbot.HandlerFunc) (telegramRunner, error) {
		return blockingTelegramRunner{}, nil
	}

	err := app.Run(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Run() error = %v, want context.Canceled", err)
	}
}

func TestRunStartsTelegramRunner(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	started := make(chan struct{})
	runner := blockingTelegramRunner{started: started}

	cfg := config.Config{
		TelegramBotToken: "token",
		MainChatID:       -1001,
		AdminChatID:      -2002,
		DatabasePath:     filepath.Join(t.TempDir(), "bot.sqlite"),
		TemplatesDir:     filepath.Join("..", "..", "templates"),
	}
	app := New(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
	app.migrationsDir = "../../migrations"
	app.telegramFactory = func(token string, handler tgbot.HandlerFunc) (telegramRunner, error) {
		if token != "token" {
			t.Fatalf("token = %q, want token", token)
		}
		if handler == nil {
			t.Fatal("handler is nil")
		}
		return runner, nil
	}

	errc := make(chan error, 1)
	go func() {
		errc <- app.Run(ctx)
	}()

	<-started
	cancel()

	if err := <-errc; !errors.Is(err, context.Canceled) {
		t.Fatalf("Run() error = %v, want context.Canceled", err)
	}
}

func TestRunTreatsUnexpectedRunnerExitAsError(t *testing.T) {
	t.Parallel()

	cfg := config.Config{
		TelegramBotToken: "token",
		MainChatID:       -1001,
		AdminChatID:      -2002,
		DatabasePath:     filepath.Join(t.TempDir(), "bot.sqlite"),
		TemplatesDir:     filepath.Join("..", "..", "templates"),
	}
	app := New(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
	app.migrationsDir = "../../migrations"
	app.telegramFactory = func(string, tgbot.HandlerFunc) (telegramRunner, error) {
		return immediateTelegramRunner{}, nil
	}

	err := app.Run(context.Background())
	if err == nil {
		t.Fatal("Run() error = nil, want unexpected runner exit error")
	}
}

func TestRunWrapsUnexpectedRunnerContextCancellation(t *testing.T) {
	t.Parallel()

	cfg := config.Config{
		TelegramBotToken: "token",
		MainChatID:       -1001,
		AdminChatID:      -2002,
		DatabasePath:     filepath.Join(t.TempDir(), "bot.sqlite"),
		TemplatesDir:     filepath.Join("..", "..", "templates"),
	}
	app := New(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
	app.migrationsDir = "../../migrations"
	app.telegramFactory = func(string, tgbot.HandlerFunc) (telegramRunner, error) {
		return immediateTelegramRunner{err: context.Canceled}, nil
	}

	err := app.Run(context.Background())
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Run() error = %v, want context.Canceled wrapper", err)
	}
	if err == context.Canceled {
		t.Fatal("Run() returned bare context.Canceled, want unexpected runner wrapper")
	}
}

func TestRunStartsScheduler(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	databasePath := filepath.Join(t.TempDir(), "bot.sqlite")
	seedDueReminder(t, databasePath, -1001)

	sent := make(chan int64, 1)
	runner := recordingTelegramRunner{sent: sent}
	cfg := config.Config{
		TelegramBotToken: "token",
		MainChatID:       -1001,
		AdminChatID:      -2002,
		DatabasePath:     databasePath,
		TemplatesDir:     filepath.Join("..", "..", "templates"),
	}
	app := New(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
	app.migrationsDir = "../../migrations"
	app.telegramFactory = func(string, tgbot.HandlerFunc) (telegramRunner, error) {
		return runner, nil
	}

	errc := make(chan error, 1)
	go func() {
		errc <- app.Run(ctx)
	}()

	select {
	case chatID := <-sent:
		if chatID != -1001 {
			t.Fatalf("scheduler sent chatID = %d, want -1001", chatID)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("scheduler did not send due reminder")
	}
	cancel()
	if err := <-errc; !errors.Is(err, context.Canceled) {
		t.Fatalf("Run() error = %v, want context.Canceled", err)
	}
}

func seedDueReminder(t *testing.T, databasePath string, mainChatID int64) {
	t.Helper()

	database, err := appdb.Open(context.Background(), appdb.Options{
		Path:          databasePath,
		MigrationsDir: "../../migrations",
	})
	if err != nil {
		t.Fatalf("Open() seed db error = %v", err)
	}
	defer database.Close()

	now := time.Now().UTC()
	if _, err := database.Exec(`
		INSERT INTO users (id, display_name, updated_at)
		VALUES (10, 'Admin', ?)
	`, now.Format(time.RFC3339Nano)); err != nil {
		t.Fatalf("insert seed user: %v", err)
	}
	if _, err := database.Exec(`
		INSERT INTO challenges (
			main_chat_id, num, theme, hashtag, state, accept_start_at,
			accept_until_at, reminder_at, created_by_user_id, created_at, updated_at
		)
		VALUES (?, 1, 'Night', '#night', 'active', ?, ?, ?, 10, ?, ?)
	`, mainChatID,
		now.Add(-time.Hour).Format(time.RFC3339Nano),
		now.Add(time.Hour).Format(time.RFC3339Nano),
		now.Add(-time.Minute).Format(time.RFC3339Nano),
		now.Format(time.RFC3339Nano),
		now.Format(time.RFC3339Nano)); err != nil {
		t.Fatalf("insert seed challenge: %v", err)
	}
}

type blockingTelegramRunner struct {
	started chan<- struct{}
}

func (r blockingTelegramRunner) EnsureIdentity(context.Context) error {
	return nil
}

func (r blockingTelegramRunner) Run(ctx context.Context) error {
	if r.started != nil {
		close(r.started)
	}
	<-ctx.Done()
	return nil
}

func (r blockingTelegramRunner) SendMarkdown(context.Context, int64, string) (int, error) {
	return 1, nil
}

func (r blockingTelegramRunner) SendText(context.Context, int64, string) (int, error) {
	return 1, nil
}

func (r blockingTelegramRunner) SendPhoto(context.Context, int64, string, string, *models.InlineKeyboardMarkup) (int, error) {
	return 1, nil
}

func (r blockingTelegramRunner) EditPhoto(context.Context, int64, int, string, string, *models.InlineKeyboardMarkup) error {
	return nil
}

func (r blockingTelegramRunner) AnswerCallback(context.Context, string, string) error {
	return nil
}

func (r blockingTelegramRunner) Pin(context.Context, int64, int) error {
	return nil
}

func (r blockingTelegramRunner) Username() string {
	return "PhotoChallengeBot"
}

type immediateTelegramRunner struct {
	err error
}

func (r immediateTelegramRunner) EnsureIdentity(context.Context) error {
	return nil
}

func (r immediateTelegramRunner) Run(context.Context) error {
	return r.err
}

func (r immediateTelegramRunner) SendMarkdown(context.Context, int64, string) (int, error) {
	return 1, nil
}

func (r immediateTelegramRunner) SendText(context.Context, int64, string) (int, error) {
	return 1, nil
}

func (r immediateTelegramRunner) SendPhoto(context.Context, int64, string, string, *models.InlineKeyboardMarkup) (int, error) {
	return 1, nil
}

func (r immediateTelegramRunner) EditPhoto(context.Context, int64, int, string, string, *models.InlineKeyboardMarkup) error {
	return nil
}

func (r immediateTelegramRunner) AnswerCallback(context.Context, string, string) error {
	return nil
}

func (r immediateTelegramRunner) Pin(context.Context, int64, int) error {
	return nil
}

func (r immediateTelegramRunner) Username() string {
	return "PhotoChallengeBot"
}

type recordingTelegramRunner struct {
	sent chan<- int64
}

func (r recordingTelegramRunner) EnsureIdentity(context.Context) error {
	return nil
}

func (r recordingTelegramRunner) Run(ctx context.Context) error {
	<-ctx.Done()
	return nil
}

func (r recordingTelegramRunner) SendMarkdown(_ context.Context, chatID int64, _ string) (int, error) {
	select {
	case r.sent <- chatID:
	default:
	}
	return 1, nil
}

func (r recordingTelegramRunner) SendText(context.Context, int64, string) (int, error) {
	return 1, nil
}

func (r recordingTelegramRunner) SendPhoto(context.Context, int64, string, string, *models.InlineKeyboardMarkup) (int, error) {
	return 1, nil
}

func (r recordingTelegramRunner) EditPhoto(context.Context, int64, int, string, string, *models.InlineKeyboardMarkup) error {
	return nil
}

func (r recordingTelegramRunner) AnswerCallback(context.Context, string, string) error {
	return nil
}

func (r recordingTelegramRunner) Pin(context.Context, int64, int) error {
	return nil
}

func (r recordingTelegramRunner) Username() string {
	return "PhotoChallengeBot"
}
