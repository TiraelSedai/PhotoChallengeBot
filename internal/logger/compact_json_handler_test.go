package logger

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"math"
	"runtime"
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

func TestCompactJSONHandlerSnapshotsHandlerOptions(t *testing.T) {
	var buf bytes.Buffer
	opts := &slog.HandlerOptions{
		ReplaceAttr: func(groups []string, attr slog.Attr) slog.Attr {
			if attr.Key == "token" {
				return slog.String(attr.Key, "REDACTED")
			}
			return attr
		},
	}
	handler := NewCompactJSONHandler(&buf, opts)
	opts.ReplaceAttr = nil

	record := slog.NewRecord(time.Date(2026, 5, 19, 0, 0, 0, 0, time.UTC), slog.LevelInfo, "authorized", 0)
	record.AddAttrs(slog.String("token", "secret"))

	if err := handler.Handle(context.Background(), record); err != nil {
		t.Fatalf("Handle() error = %v", err)
	}

	event := decodeLogEvent(t, buf.Bytes())
	if event["token"] != "REDACTED" {
		t.Fatalf("token = %v", event["token"])
	}
}

func TestCompactJSONHandlerAppliesReplaceAttrInsideGroups(t *testing.T) {
	var buf bytes.Buffer
	handler := NewCompactJSONHandler(&buf, &slog.HandlerOptions{
		ReplaceAttr: func(groups []string, attr slog.Attr) slog.Attr {
			if attr.Key == "token" {
				return slog.String(attr.Key, "REDACTED")
			}
			return attr
		},
	})
	record := slog.NewRecord(time.Date(2026, 5, 19, 0, 0, 0, 0, time.UTC), slog.LevelInfo, "authorized", 0)
	record.AddAttrs(slog.Group("auth", slog.String("token", "secret")))

	if err := handler.Handle(context.Background(), record); err != nil {
		t.Fatalf("Handle() error = %v", err)
	}

	event := decodeLogEvent(t, buf.Bytes())
	auth, ok := event["auth"].(map[string]any)
	if !ok {
		t.Fatalf("auth group = %#v", event["auth"])
	}
	if auth["token"] != "REDACTED" {
		t.Fatalf("auth.token = %v", auth["token"])
	}
}

func TestCompactJSONHandlerInlinesEmptyGroups(t *testing.T) {
	var buf bytes.Buffer
	handler := NewCompactJSONHandler(&buf, nil)
	record := slog.NewRecord(time.Date(2026, 5, 19, 0, 0, 0, 0, time.UTC), slog.LevelInfo, "grouped", 0)
	record.AddAttrs(
		slog.Group("", slog.String("inline", "root")),
		slog.Group("outer", slog.Group("", slog.String("nested", "value"))),
	)

	if err := handler.Handle(context.Background(), record); err != nil {
		t.Fatalf("Handle() error = %v", err)
	}

	event := decodeLogEvent(t, buf.Bytes())
	if _, ok := event[""]; ok {
		t.Fatalf("empty group key should not be emitted: %#v", event)
	}
	if event["inline"] != "root" {
		t.Fatalf("inline = %v", event["inline"])
	}
	outer, ok := event["outer"].(map[string]any)
	if !ok {
		t.Fatalf("outer group = %#v", event["outer"])
	}
	if _, ok := outer[""]; ok {
		t.Fatalf("nested empty group key should not be emitted: %#v", outer)
	}
	if outer["nested"] != "value" {
		t.Fatalf("outer.nested = %v", outer["nested"])
	}
}

func TestCompactJSONHandlerDoesNotDropEventForUnsupportedValues(t *testing.T) {
	var buf bytes.Buffer
	handler := NewCompactJSONHandler(&buf, nil)
	record := slog.NewRecord(time.Date(2026, 5, 19, 0, 0, 0, 0, time.UTC), slog.LevelInfo, "has unsupported value", 0)
	record.AddAttrs(slog.Any("callback", func() {}))

	if err := handler.Handle(context.Background(), record); err != nil {
		t.Fatalf("Handle() error = %v", err)
	}

	event := decodeLogEvent(t, buf.Bytes())
	if event["@mt"] != "has unsupported value" {
		t.Fatalf("@mt = %v", event["@mt"])
	}
	if event["callback"] == nil {
		t.Fatal("callback should be represented instead of dropping the event")
	}
}

func TestCompactJSONHandlerDoesNotDropEventForNonFiniteFloats(t *testing.T) {
	var buf bytes.Buffer
	handler := NewCompactJSONHandler(&buf, nil)
	record := slog.NewRecord(time.Date(2026, 5, 19, 0, 0, 0, 0, time.UTC), slog.LevelInfo, "has nan", 0)
	record.AddAttrs(slog.Float64("score", math.NaN()))

	if err := handler.Handle(context.Background(), record); err != nil {
		t.Fatalf("Handle() error = %v", err)
	}

	event := decodeLogEvent(t, buf.Bytes())
	if event["@mt"] != "has nan" {
		t.Fatalf("@mt = %v", event["@mt"])
	}
	if event["score"] != "NaN" {
		t.Fatalf("score = %v", event["score"])
	}
}

func TestCompactJSONHandlerOmitsZeroRecordTime(t *testing.T) {
	var buf bytes.Buffer
	handler := NewCompactJSONHandler(&buf, nil)
	record := slog.NewRecord(time.Time{}, slog.LevelInfo, "without time", 0)

	if err := handler.Handle(context.Background(), record); err != nil {
		t.Fatalf("Handle() error = %v", err)
	}

	event := decodeLogEvent(t, buf.Bytes())
	if _, ok := event["@t"]; ok {
		t.Fatalf("zero time should be omitted: %#v", event["@t"])
	}
}

func TestCompactJSONHandlerAppliesReplaceAttrToBuiltIns(t *testing.T) {
	var buf bytes.Buffer
	handler := NewCompactJSONHandler(&buf, &slog.HandlerOptions{
		ReplaceAttr: func(groups []string, attr slog.Attr) slog.Attr {
			switch attr.Key {
			case "@t":
				return slog.Attr{}
			case "@mt":
				return slog.String(attr.Key, "replaced message")
			case "@l":
				return slog.String(attr.Key, "Fatal")
			default:
				return attr
			}
		},
	})
	record := slog.NewRecord(time.Date(2026, 5, 19, 0, 0, 0, 0, time.UTC), slog.LevelError, "original message", 0)

	if err := handler.Handle(context.Background(), record); err != nil {
		t.Fatalf("Handle() error = %v", err)
	}

	event := decodeLogEvent(t, buf.Bytes())
	if _, ok := event["@t"]; ok {
		t.Fatalf("@t should be removed: %#v", event["@t"])
	}
	if event["@mt"] != "replaced message" {
		t.Fatalf("@mt = %v", event["@mt"])
	}
	if event["@l"] != "Fatal" {
		t.Fatalf("@l = %v", event["@l"])
	}
}

func TestCompactJSONHandlerPassesSourceToReplaceAttr(t *testing.T) {
	var buf bytes.Buffer
	seenSource := false
	handler := NewCompactJSONHandler(&buf, &slog.HandlerOptions{
		AddSource: true,
		ReplaceAttr: func(groups []string, attr slog.Attr) slog.Attr {
			if attr.Key != slog.SourceKey {
				return attr
			}
			if _, ok := attr.Value.Any().(*slog.Source); ok {
				seenSource = true
			}
			return attr
		},
	})
	pc, _, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller() failed")
	}
	record := slog.NewRecord(time.Date(2026, 5, 19, 0, 0, 0, 0, time.UTC), slog.LevelInfo, "with source", pc)

	if err := handler.Handle(context.Background(), record); err != nil {
		t.Fatalf("Handle() error = %v", err)
	}

	if !seenSource {
		t.Fatal("ReplaceAttr should receive source as *slog.Source")
	}
	event := decodeLogEvent(t, buf.Bytes())
	source, ok := event["source"].(map[string]any)
	if !ok {
		t.Fatalf("source = %#v", event["source"])
	}
	if source["function"] == nil || source["file"] == nil || source["line"] == nil {
		t.Fatalf("source should use compact lower-case shape: %#v", source)
	}
}

func TestCompactJSONHandlerDoesNotEmitEmptyGroupsAfterReplaceAttrDropsChildren(t *testing.T) {
	var buf bytes.Buffer
	handler := NewCompactJSONHandler(&buf, &slog.HandlerOptions{
		ReplaceAttr: func(groups []string, attr slog.Attr) slog.Attr {
			if attr.Key == "token" {
				return slog.Attr{}
			}
			return attr
		},
	}).
		WithGroup("auth").
		WithAttrs([]slog.Attr{slog.String("token", "secret")})
	record := slog.NewRecord(time.Date(2026, 5, 19, 0, 0, 0, 0, time.UTC), slog.LevelInfo, "redacted", 0)
	record.AddAttrs(slog.Group("outer", slog.String("token", "secret")))

	if err := handler.Handle(context.Background(), record); err != nil {
		t.Fatalf("Handle() error = %v", err)
	}

	event := decodeLogEvent(t, buf.Bytes())
	if _, ok := event["auth"]; ok {
		t.Fatalf("empty WithGroup path should not be emitted: %#v", event["auth"])
	}
	if _, ok := event["outer"]; ok {
		t.Fatalf("empty slog.Group should not be emitted: %#v", event["outer"])
	}
}

func TestCompactJSONHandlerMergesDuplicateGroupsInsideWithGroup(t *testing.T) {
	var buf bytes.Buffer
	handler := NewCompactJSONHandler(&buf, nil).WithGroup("outer")
	record := slog.NewRecord(time.Date(2026, 5, 19, 0, 0, 0, 0, time.UTC), slog.LevelInfo, "duplicate groups", 0)
	record.AddAttrs(
		slog.Group("inner", slog.Int("a", 1)),
		slog.Group("inner", slog.Int("b", 2)),
	)

	if err := handler.Handle(context.Background(), record); err != nil {
		t.Fatalf("Handle() error = %v", err)
	}

	event := decodeLogEvent(t, buf.Bytes())
	outer, ok := event["outer"].(map[string]any)
	if !ok {
		t.Fatalf("outer = %#v", event["outer"])
	}
	inner, ok := outer["inner"].(map[string]any)
	if !ok {
		t.Fatalf("outer.inner = %#v", outer["inner"])
	}
	if inner["a"] != float64(1) {
		t.Fatalf("outer.inner.a = %v", inner["a"])
	}
	if inner["b"] != float64(2) {
		t.Fatalf("outer.inner.b = %v", inner["b"])
	}
}

func TestCompactJSONHandlerHandlesTypedNilSpecialValues(t *testing.T) {
	var buf bytes.Buffer
	handler := NewCompactJSONHandler(&buf, nil)
	var source *slog.Source
	var err *typedNilError
	record := slog.NewRecord(time.Date(2026, 5, 19, 0, 0, 0, 0, time.UTC), slog.LevelInfo, "typed nils", 0)
	record.AddAttrs(
		slog.Any("source", source),
		slog.Any("error", err),
	)

	if err := handler.Handle(context.Background(), record); err != nil {
		t.Fatalf("Handle() error = %v", err)
	}

	event := decodeLogEvent(t, buf.Bytes())
	if event["source"] != nil {
		t.Fatalf("source = %#v", event["source"])
	}
	if event["error"] != nil {
		t.Fatalf("error = %#v", event["error"])
	}
}

func TestCompactJSONHandlerResolvesLogValuerAttrs(t *testing.T) {
	var buf bytes.Buffer
	handler := NewCompactJSONHandler(&buf, nil)
	record := slog.NewRecord(time.Date(2026, 5, 19, 0, 0, 0, 0, time.UTC), slog.LevelInfo, "log valuer", 0)
	record.AddAttrs(
		slog.Any("token", redactedLogValue{secret: "secret"}),
		slog.Any("user", groupedLogValue{id: 42, name: "alice"}),
	)

	if err := handler.Handle(context.Background(), record); err != nil {
		t.Fatalf("Handle() error = %v", err)
	}

	event := decodeLogEvent(t, buf.Bytes())
	if event["token"] != "REDACTED" {
		t.Fatalf("token = %v", event["token"])
	}
	user, ok := event["user"].(map[string]any)
	if !ok {
		t.Fatalf("user = %#v", event["user"])
	}
	if user["id"] != float64(42) {
		t.Fatalf("user.id = %v", user["id"])
	}
	if user["name"] != "alice" {
		t.Fatalf("user.name = %v", user["name"])
	}
}

type redactedLogValue struct {
	secret string
}

func (v redactedLogValue) LogValue() slog.Value {
	return slog.StringValue("REDACTED")
}

type groupedLogValue struct {
	id   int
	name string
}

func (v groupedLogValue) LogValue() slog.Value {
	return slog.GroupValue(
		slog.Int("id", v.id),
		slog.String("name", v.name),
	)
}

type typedNilError struct{}

func (e *typedNilError) Error() string {
	return e.Error()
}

func TestCompactJSONHandlerMarshalsJSONSafeAnyValuesOnce(t *testing.T) {
	var buf bytes.Buffer
	calls := 0
	handler := NewCompactJSONHandler(&buf, nil)
	record := slog.NewRecord(time.Date(2026, 5, 19, 0, 0, 0, 0, time.UTC), slog.LevelInfo, "custom json", 0)
	record.AddAttrs(slog.Any("custom", countingJSONValue{calls: &calls}))

	if err := handler.Handle(context.Background(), record); err != nil {
		t.Fatalf("Handle() error = %v", err)
	}

	if calls != 1 {
		t.Fatalf("MarshalJSON calls = %d, want 1", calls)
	}
	event := decodeLogEvent(t, buf.Bytes())
	custom, ok := event["custom"].(map[string]any)
	if !ok {
		t.Fatalf("custom = %#v", event["custom"])
	}
	if custom["ok"] != true {
		t.Fatalf("custom.ok = %v", custom["ok"])
	}
}

type countingJSONValue struct {
	calls *int
}

func (v countingJSONValue) MarshalJSON() ([]byte, error) {
	*v.calls++
	return []byte(`{"ok":true}`), nil
}

func decodeLogEvent(t *testing.T, data []byte) map[string]any {
	t.Helper()
	var event map[string]any
	if err := json.Unmarshal(data, &event); err != nil {
		t.Fatalf("json.Unmarshal() error = %v; data = %q", err, string(data))
	}
	return event
}
