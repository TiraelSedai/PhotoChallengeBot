package config

import (
	"strings"
	"testing"
)

func TestLoadUsesRequiredEnvironment(t *testing.T) {
	t.Parallel()

	cfg, err := load(mapLookup(map[string]string{
		"TELEGRAM_BOT_TOKEN": "token",
		"MAIN_CHAT_ID":       "-1001272818469",
		"ADMIN_CHAT_ID":      "-100123",
		"DATABASE_PATH":      "/data/bot.sqlite",
		"TEMPLATES_DIR":      "templates",
		"TZ":                 "UTC",
	}))
	if err != nil {
		t.Fatalf("load() error = %v", err)
	}

	if cfg.TelegramBotToken != "token" {
		t.Fatalf("TelegramBotToken = %q", cfg.TelegramBotToken)
	}
	if cfg.MainChatID != -1001272818469 {
		t.Fatalf("MainChatID = %d", cfg.MainChatID)
	}
	if cfg.AdminChatID != -100123 {
		t.Fatalf("AdminChatID = %d", cfg.AdminChatID)
	}
	if cfg.DatabasePath != "/data/bot.sqlite" {
		t.Fatalf("DatabasePath = %q", cfg.DatabasePath)
	}
	if cfg.TemplatesDir != "templates" {
		t.Fatalf("TemplatesDir = %q", cfg.TemplatesDir)
	}
	if got := cfg.Location.String(); got != "UTC" {
		t.Fatalf("Location = %q, want UTC", got)
	}
}

func TestLoadDefaultsTimezoneToMoscow(t *testing.T) {
	t.Parallel()

	cfg, err := load(mapLookup(map[string]string{
		"TELEGRAM_BOT_TOKEN": "token",
		"MAIN_CHAT_ID":       "-1001272818469",
		"ADMIN_CHAT_ID":      "-100123",
		"DATABASE_PATH":      "/data/bot.sqlite",
		"TEMPLATES_DIR":      "templates",
	}))
	if err != nil {
		t.Fatalf("load() error = %v", err)
	}

	if got := cfg.Location.String(); got != defaultTimezone {
		t.Fatalf("Location = %q, want %s", got, defaultTimezone)
	}
}

func TestLoadRejectsMissingRequiredEnvironment(t *testing.T) {
	t.Parallel()

	_, err := load(mapLookup(map[string]string{
		"MAIN_CHAT_ID":  "-1001272818469",
		"ADMIN_CHAT_ID": "-100123",
		"DATABASE_PATH": "/data/bot.sqlite",
		"TEMPLATES_DIR": "templates",
	}))
	if err == nil {
		t.Fatal("load() error = nil, want missing TELEGRAM_BOT_TOKEN")
	}
	if !strings.Contains(err.Error(), "TELEGRAM_BOT_TOKEN") {
		t.Fatalf("load() error = %q, want TELEGRAM_BOT_TOKEN", err)
	}
}

func TestLoadRejectsInvalidChatID(t *testing.T) {
	t.Parallel()

	_, err := load(mapLookup(map[string]string{
		"TELEGRAM_BOT_TOKEN": "token",
		"MAIN_CHAT_ID":       "not-a-number",
		"ADMIN_CHAT_ID":      "-100123",
		"DATABASE_PATH":      "/data/bot.sqlite",
		"TEMPLATES_DIR":      "templates",
	}))
	if err == nil {
		t.Fatal("load() error = nil, want invalid MAIN_CHAT_ID")
	}
	if !strings.Contains(err.Error(), "MAIN_CHAT_ID") {
		t.Fatalf("load() error = %q, want MAIN_CHAT_ID", err)
	}
}

func mapLookup(values map[string]string) func(string) (string, bool) {
	return func(key string) (string, bool) {
		value, ok := values[key]
		return value, ok
	}
}
