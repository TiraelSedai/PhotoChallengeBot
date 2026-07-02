package app

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/TiraelSedai/PhotoChallengeBot/internal/config"
	appdb "github.com/TiraelSedai/PhotoChallengeBot/internal/db"
	"github.com/TiraelSedai/PhotoChallengeBot/internal/repository"
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

func TestLogKnownChallengesReportsWinnersAndResultsLink(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	database, err := appdb.Open(ctx, appdb.Options{
		Path:          filepath.Join(t.TempDir(), "bot.sqlite"),
		MigrationsDir: "../../migrations",
	})
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer database.Close()

	users := repository.NewUsers(database)
	if _, err := users.Upsert(ctx, repository.User{ID: 10, FirstName: "Admin"}); err != nil {
		t.Fatalf("upsert user: %v", err)
	}
	now := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)
	challengeRepo := repository.NewChallenges(database)
	seeded, err := challengeRepo.Create(ctx, repository.CreateChallengeInput{
		MainChatID:      -1001,
		Num:             107,
		Theme:           "Жёлтый",
		Hashtag:         "#жёлтый",
		State:           repository.ChallengeStateFinished,
		AcceptStartAt:   now.Add(-48 * time.Hour),
		AcceptUntilAt:   now.Add(-24 * time.Hour),
		ReminderAt:      now.Add(-30 * time.Hour),
		CreatedByUserID: 10,
		CreatedAt:       now,
	})
	if err != nil {
		t.Fatalf("create challenge: %v", err)
	}
	if _, err := database.Exec(`
		UPDATE challenges SET results_message_id = 143054, results_chat_id = -1001272818469 WHERE id = ?
	`, seeded.ID); err != nil {
		t.Fatalf("set results link: %v", err)
	}

	winnersRepo := repository.NewChallengeWinners(database)
	resolvedID := int64(42)
	unresolvedID := int64(77)
	for _, id := range []int64{resolvedID, unresolvedID} {
		if _, err := users.Upsert(ctx, repository.User{ID: id, FirstName: "Winner"}); err != nil {
			t.Fatalf("upsert winner user %d: %v", id, err)
		}
	}
	if err := winnersRepo.UpsertMany(ctx, []repository.ChallengeWinner{
		{ChallengeID: seeded.ID, Username: "alice"},
		{ChallengeID: seeded.ID, Username: "bob", UserID: &resolvedID},
		{ChallengeID: seeded.ID, Username: "carol", UserID: &unresolvedID},
	}); err != nil {
		t.Fatalf("upsert winners: %v", err)
	}

	runner := &MoqTelegramRunner{
		MemberDisplayNameFunc: func(_ context.Context, chatID int64, userID int64) (string, error) {
			if chatID != -1001 {
				t.Fatalf("MemberDisplayName chatID = %d, want -1001", chatID)
			}
			if userID == resolvedID {
				return "Bob Winner", nil
			}
			return "", errors.New("member not found")
		},
	}

	var buf bytes.Buffer
	cfg := config.Config{
		MainChatID:   -1001,
		AdminChatID:  -2002,
		DatabasePath: "unused",
		TemplatesDir: filepath.Join("..", "..", "templates"),
		Location:     time.UTC,
	}
	app := New(cfg, slog.New(slog.NewTextHandler(&buf, nil)))
	if err := app.logKnownChallenges(ctx, challengeRepo, winnersRepo, runner); err != nil {
		t.Fatalf("logKnownChallenges() error = %v", err)
	}

	logged := buf.String()
	for _, want := range []string{
		"known challenge",
		"num=107",
		"theme=Жёлтый",
		"results_link=https://t.me/c/1272818469/143054",
		`winners="alice, bob, carol"`,
		"name=alice",
		`name="Bob Winner"`,
		"name=77",
		"wins=1",
		`challenges="Жёлтый (#жёлтый)"`,
		"count=1",
	} {
		if !strings.Contains(logged, want) {
			t.Fatalf("log output %q does not contain %q", logged, want)
		}
	}
	for _, level := range []string{"level=WARN", "level=ERROR"} {
		if strings.Contains(logged, level) {
			t.Fatalf("startup report must stay at info, got %s in %q", level, logged)
		}
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
