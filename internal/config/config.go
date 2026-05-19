package config

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

const defaultTimezone = "Europe/Moscow"

type Config struct {
	TelegramBotToken string
	MainChatID       int64
	AdminChatID      int64
	DatabasePath     string
	TemplatesDir     string
	Location         *time.Location
}

func Load() (Config, error) {
	return load(os.LookupEnv)
}

func load(lookup func(string) (string, bool)) (Config, error) {
	token, err := required(lookup, "TELEGRAM_BOT_TOKEN")
	if err != nil {
		return Config{}, err
	}

	mainChatID, err := requiredInt64(lookup, "MAIN_CHAT_ID")
	if err != nil {
		return Config{}, err
	}

	adminChatID, err := requiredInt64(lookup, "ADMIN_CHAT_ID")
	if err != nil {
		return Config{}, err
	}

	databasePath, err := required(lookup, "DATABASE_PATH")
	if err != nil {
		return Config{}, err
	}

	templatesDir, err := required(lookup, "TEMPLATES_DIR")
	if err != nil {
		return Config{}, err
	}

	tz, ok := lookup("TZ")
	if !ok || tz == "" {
		tz = defaultTimezone
	}

	location, err := time.LoadLocation(tz)
	if err != nil {
		return Config{}, fmt.Errorf("load TZ %q: %w", tz, err)
	}

	return Config{
		TelegramBotToken: token,
		MainChatID:       mainChatID,
		AdminChatID:      adminChatID,
		DatabasePath:     databasePath,
		TemplatesDir:     templatesDir,
		Location:         location,
	}, nil
}

func required(lookup func(string) (string, bool), name string) (string, error) {
	value, ok := lookup(name)
	if !ok || value == "" {
		return "", fmt.Errorf("missing required environment variable %s", name)
	}
	return value, nil
}

func requiredInt64(lookup func(string) (string, bool), name string) (int64, error) {
	value, err := required(lookup, name)
	if err != nil {
		return 0, err
	}

	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("parse %s as int64: %w", name, err)
	}
	return parsed, nil
}
