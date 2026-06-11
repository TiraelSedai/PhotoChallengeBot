package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"
	_ "time/tzdata"

	"github.com/TiraelSedai/PhotoChallengeBot/internal/db"
	"github.com/TiraelSedai/PhotoChallengeBot/internal/history"
	"github.com/TiraelSedai/PhotoChallengeBot/internal/logger"
)

var resultsLinkPattern = regexp.MustCompile(`^https://t\.me/c/(\d+)/(\d+)$`)

func main() {
	log := slog.New(logger.NewCompactJSONHandler(os.Stderr, &slog.HandlerOptions{}))
	slog.SetDefault(log)

	csvPath := flag.String("csv", "", "path to the historical challenges CSV (required)")
	userIDsPath := flag.String("user-ids", "", "path to the username,telegram-id CSV for winner id backfill")
	dbPath := flag.String("db", os.Getenv("DATABASE_PATH"), "path to the sqlite database (default $DATABASE_PATH)")
	migrationsDir := flag.String("migrations", "migrations", "goose migrations directory")
	mainChatID := flag.Int64("main-chat-id", envInt64("MAIN_CHAT_ID"), "new main chat id, e.g. -100123456 (default $MAIN_CHAT_ID)")
	lastResultsLink := flag.String("last-results-link", "", "https://t.me/c/<chat>/<msg> link to the latest historical results message")
	wipe := flag.Bool("wipe", false, "delete existing challenges and the importer user before importing")
	flag.Parse()

	if err := run(log, *csvPath, *userIDsPath, *dbPath, *migrationsDir, *mainChatID, *lastResultsLink, *wipe); err != nil {
		log.Error("import failed", "error", err)
		os.Exit(1)
	}
}

func run(log *slog.Logger, csvPath, userIDsPath, dbPath, migrationsDir string, mainChatID int64, lastResultsLink string, wipe bool) error {
	if csvPath == "" {
		return fmt.Errorf("-csv is required")
	}
	if dbPath == "" {
		return fmt.Errorf("-db or DATABASE_PATH is required")
	}
	if mainChatID == 0 {
		return fmt.Errorf("-main-chat-id or MAIN_CHAT_ID is required")
	}

	location, err := time.LoadLocation("Europe/Moscow")
	if err != nil {
		return fmt.Errorf("load location: %w", err)
	}
	opts := history.Options{
		MainChatID: mainChatID,
		Location:   location,
		Now:        time.Now().UTC(),
		Wipe:       wipe,
	}

	if lastResultsLink != "" {
		chatID, messageID, err := parseResultsLink(lastResultsLink)
		if err != nil {
			return err
		}
		opts.ResultsChatID = &chatID
		opts.ResultsMessageID = &messageID
	}

	if userIDsPath != "" {
		userIDs, idWarnings, err := loadUserIDs(userIDsPath)
		if err != nil {
			return err
		}
		for _, warning := range idWarnings {
			log.Warn("user ids quirk", "detail", warning)
		}
		opts.UserIDs = userIDs
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	file, err := os.Open(csvPath)
	if err != nil {
		return fmt.Errorf("open csv: %w", err)
	}
	defer file.Close()

	records, warnings, err := history.Parse(file, opts.Location)
	if err != nil {
		return err
	}
	for _, warning := range warnings {
		log.Warn("csv quirk", "detail", warning)
	}

	database, err := db.Open(ctx, db.Options{Path: dbPath, MigrationsDir: migrationsDir})
	if err != nil {
		return err
	}
	defer database.Close()

	stats, err := history.Import(ctx, database, records, opts)
	if err != nil {
		return err
	}
	log.Info("history imported",
		"challenges", stats.Challenges,
		"winners", stats.Winners,
		"winners_with_id", stats.WinnersWithID,
		"first_num", records[0].Num,
		"last_num", records[len(records)-1].Num,
		"main_chat_id", mainChatID,
	)
	if len(opts.UserIDs) > 0 && len(stats.UnmatchedUsernames) > 0 {
		log.Warn("winners without telegram id",
			"count", len(stats.UnmatchedUsernames),
			"usernames", strings.Join(stats.UnmatchedUsernames, ", "),
		)
	}
	return nil
}

func loadUserIDs(path string) (map[string]int64, []string, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, nil, fmt.Errorf("open user ids: %w", err)
	}
	defer file.Close()
	return history.ParseUserIDs(file)
}

func parseResultsLink(link string) (int64, int64, error) {
	match := resultsLinkPattern.FindStringSubmatch(link)
	if match == nil {
		return 0, 0, fmt.Errorf("last results link %q does not match https://t.me/c/<chat>/<msg>", link)
	}
	chatID, err := strconv.ParseInt("-100"+match[1], 10, 64)
	if err != nil {
		return 0, 0, fmt.Errorf("parse results chat id: %w", err)
	}
	messageID, err := strconv.ParseInt(match[2], 10, 64)
	if err != nil {
		return 0, 0, fmt.Errorf("parse results message id: %w", err)
	}
	return chatID, messageID, nil
}

func envInt64(name string) int64 {
	value, err := strconv.ParseInt(os.Getenv(name), 10, 64)
	if err != nil {
		return 0
	}
	return value
}
