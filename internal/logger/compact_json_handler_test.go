package logger

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"
)

func TestCompactJSONHandlerWritesInformationWithoutLevel(t *testing.T) {
	var buf bytes.Buffer
	handler := NewCompactJSONHandler(&buf, nil)
	record := slog.NewRecord(
		time.Date(2026, 5, 19, 12, 34, 56, 789, time.FixedZone("MSK", 3*60*60)),
		slog.LevelInfo,
		"starting bot",
		0,
	)
	record.AddAttrs(slog.Int64("main_chat_id", -100))

	if err := handler.Handle(context.Background(), record); err != nil {
		t.Fatalf("Handle() error = %v", err)
	}

	event := decodeLogEvent(t, buf.Bytes())
	if event["@t"] != "2026-05-19T09:34:56.000000789Z" {
		t.Fatalf("@t = %v", event["@t"])
	}
	if event["@mt"] != "starting bot" {
		t.Fatalf("@mt = %v", event["@mt"])
	}
	if _, ok := event["@l"]; ok {
		t.Fatalf("info event should not include @l: %v", event["@l"])
	}
	if event["main_chat_id"] != float64(-100) {
		t.Fatalf("main_chat_id = %v", event["main_chat_id"])
	}
}

func TestCompactJSONHandlerMapsNonInformationLevels(t *testing.T) {
	tests := []struct {
		name  string
		level slog.Level
		want  string
	}{
		{name: "debug", level: slog.LevelDebug, want: "Debug"},
		{name: "warning", level: slog.LevelWarn, want: "Warning"},
		{name: "error", level: slog.LevelError, want: "Error"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			handler := NewCompactJSONHandler(&buf, nil)
			record := slog.NewRecord(time.Date(2026, 5, 19, 0, 0, 0, 0, time.UTC), tt.level, "event", 0)

			if err := handler.Handle(context.Background(), record); err != nil {
				t.Fatalf("Handle() error = %v", err)
			}

			event := decodeLogEvent(t, buf.Bytes())
			if event["@l"] != tt.want {
				t.Fatalf("@l = %v, want %v", event["@l"], tt.want)
			}
		})
	}
}

func TestCompactJSONHandlerEncodesErrorsAsStrings(t *testing.T) {
	var buf bytes.Buffer
	handler := NewCompactJSONHandler(&buf, nil)
	record := slog.NewRecord(time.Date(2026, 5, 19, 0, 0, 0, 0, time.UTC), slog.LevelError, "load config", 0)
	record.AddAttrs(slog.Any("error", errors.New("missing token")))

	if err := handler.Handle(context.Background(), record); err != nil {
		t.Fatalf("Handle() error = %v", err)
	}

	event := decodeLogEvent(t, buf.Bytes())
	if event["error"] != "missing token" {
		t.Fatalf("error = %v", event["error"])
	}
}

func TestCompactJSONHandlerPreservesAttributeGroupScope(t *testing.T) {
	var buf bytes.Buffer
	handler := NewCompactJSONHandler(&buf, nil).
		WithAttrs([]slog.Attr{slog.String("request_id", "root")}).
		WithGroup("telegram").
		WithAttrs([]slog.Attr{slog.Int64("chat_id", 42)})
	record := slog.NewRecord(time.Date(2026, 5, 19, 0, 0, 0, 0, time.UTC), slog.LevelInfo, "route update", 0)
	record.AddAttrs(slog.Int64("update_id", 1001))

	if err := handler.Handle(context.Background(), record); err != nil {
		t.Fatalf("Handle() error = %v", err)
	}

	event := decodeLogEvent(t, buf.Bytes())
	if event["request_id"] != "root" {
		t.Fatalf("request_id = %v", event["request_id"])
	}
	telegram, ok := event["telegram"].(map[string]any)
	if !ok {
		t.Fatalf("telegram group = %#v", event["telegram"])
	}
	if telegram["chat_id"] != float64(42) {
		t.Fatalf("telegram.chat_id = %v", telegram["chat_id"])
	}
	if telegram["update_id"] != float64(1001) {
		t.Fatalf("telegram.update_id = %v", telegram["update_id"])
	}
}

func TestCompactJSONHandlerEnabledRespectsConfiguredLevel(t *testing.T) {
	handler := NewCompactJSONHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelWarn})

	if handler.Enabled(context.Background(), slog.LevelInfo) {
		t.Fatal("info should be disabled")
	}
	if !handler.Enabled(context.Background(), slog.LevelWarn) {
		t.Fatal("warn should be enabled")
	}
}

func decodeLogEvent(t *testing.T, data []byte) map[string]any {
	t.Helper()
	var event map[string]any
	if err := json.Unmarshal(data, &event); err != nil {
		t.Fatalf("json.Unmarshal() error = %v; data = %q", err, string(data))
	}
	return event
}
