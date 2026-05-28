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
		Location:     time.UTC,
	}
	app := New(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
	app.migrationsDir = "../../migrations"
	app.telegramFactory = func(string, tgbot.HandlerFunc) (telegramRunner, error) {
		return &MoqTelegramRunner{RunFunc: func(ctx context.Context) error {
			<-ctx.Done()
			return nil
		}}, nil
	}

	err := app.Run(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Run() error = %v, want context.Canceled", err)
	}
}

func TestNewPanicsOnNilLogger(t *testing.T) {
	t.Parallel()

	defer func() {
		if recover() == nil {
			t.Fatal("New() did not panic")
		}
	}()
	New(config.Config{}, nil)
}

func TestNewPanicsOnNilLocation(t *testing.T) {
	t.Parallel()

	defer func() {
		if recover() == nil {
			t.Fatal("New() did not panic")
		}
	}()
	New(config.Config{}, slog.New(slog.NewTextHandler(io.Discard, nil)))
}

func TestRunStartsTelegramRunner(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	started := make(chan struct{})
	runner := &MoqTelegramRunner{RunFunc: func(ctx context.Context) error {
		close(started)
		<-ctx.Done()
		return nil
	}}

	cfg := config.Config{
		TelegramBotToken: "token",
		MainChatID:       -1001,
		AdminChatID:      -2002,
		DatabasePath:     filepath.Join(t.TempDir(), "bot.sqlite"),
		TemplatesDir:     filepath.Join("..", "..", "templates"),
		Location:         time.UTC,
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
		Location:         time.UTC,
	}
	app := New(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
	app.migrationsDir = "../../migrations"
	app.telegramFactory = func(string, tgbot.HandlerFunc) (telegramRunner, error) {
		return &MoqTelegramRunner{}, nil
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
		Location:         time.UTC,
	}
	app := New(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
	app.migrationsDir = "../../migrations"
	app.telegramFactory = func(string, tgbot.HandlerFunc) (telegramRunner, error) {
		return &MoqTelegramRunner{RunFunc: func(context.Context) error {
			return context.Canceled
		}}, nil
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
	runner := &MoqTelegramRunner{
		RunFunc: func(ctx context.Context) error {
			<-ctx.Done()
			return nil
		},
		SendMarkdownFunc: func(_ context.Context, chatID int64, _ string) (int, error) {
			select {
			case sent <- chatID:
			default:
			}
			return 1, nil
		},
	}
	cfg := config.Config{
		TelegramBotToken: "token",
		MainChatID:       -1001,
		AdminChatID:      -2002,
		DatabasePath:     databasePath,
		TemplatesDir:     filepath.Join("..", "..", "templates"),
		Location:         time.UTC,
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
