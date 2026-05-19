package logger

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"maps"
	"runtime"
	"strings"
	"sync"
	"time"
)

type CompactJSONHandler struct {
	writer io.Writer
	opts   *slog.HandlerOptions
	attrs  []scopedAttr
	groups []string
	mu     *sync.Mutex
}

type scopedAttr struct {
	groups []string
	attr   slog.Attr
}

func NewCompactJSONHandler(w io.Writer, opts *slog.HandlerOptions) *CompactJSONHandler {
	if w == nil {
		panic("logger: nil writer")
	}
	if opts == nil {
		opts = &slog.HandlerOptions{}
	}
	return &CompactJSONHandler{
		writer: w,
		opts:   opts,
		attrs:  make([]scopedAttr, 0),
		groups: make([]string, 0),
		mu:     &sync.Mutex{},
	}
}

func (h *CompactJSONHandler) Enabled(ctx context.Context, level slog.Level) bool {
	minLevel := slog.LevelInfo
	if h.opts.Level != nil {
		minLevel = h.opts.Level.Level()
	}
	return level >= minLevel
}

func (h *CompactJSONHandler) Handle(ctx context.Context, r slog.Record) error {
	entry := map[string]any{
		"@t":  r.Time.UTC().Format(time.RFC3339Nano),
		"@mt": r.Message,
	}
	if h.opts.AddSource && r.PC != 0 {
		frame, _ := runtime.CallersFrames([]uintptr{r.PC}).Next()
		entry["source"] = map[string]any{
			"function": frame.Function,
			"file":     frame.File,
			"line":     frame.Line,
		}
	}

	if level := compactLevel(r.Level); level != "" {
		entry["@l"] = level
	}

	for _, attr := range h.attrs {
		h.addAttr(entry, attr.groups, attr.attr)
	}
	r.Attrs(func(attr slog.Attr) bool {
		h.addAttr(entry, h.groups, attr)
		return true
	})

	jsonData, err := json.Marshal(entry)
	if err != nil {
		return err
	}
	jsonData = append(jsonData, '\n')

	h.mu.Lock()
	defer h.mu.Unlock()
	_, err = h.writer.Write(jsonData)
	return err
}

func (h *CompactJSONHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	newAttrs := make([]scopedAttr, 0, len(h.attrs)+len(attrs))
	newAttrs = append(newAttrs, h.attrs...)
	for _, attr := range attrs {
		newAttrs = append(newAttrs, scopedAttr{
			groups: append([]string{}, h.groups...),
			attr:   attr,
		})
	}

	return &CompactJSONHandler{
		writer: h.writer,
		opts:   h.opts,
		attrs:  newAttrs,
		groups: append([]string{}, h.groups...),
		mu:     h.mu,
	}
}

func (h *CompactJSONHandler) WithGroup(name string) slog.Handler {
	if name == "" {
		return h
	}
	newGroups := append([]string{}, h.groups...)
	newGroups = append(newGroups, name)

	return &CompactJSONHandler{
		writer: h.writer,
		opts:   h.opts,
		attrs:  append([]scopedAttr{}, h.attrs...),
		groups: newGroups,
		mu:     h.mu,
	}
}

func (h *CompactJSONHandler) addAttr(entry map[string]any, groups []string, attr slog.Attr) {
	attr.Value = attr.Value.Resolve()
	if attr.Equal(slog.Attr{}) {
		return
	}
	if h.opts.ReplaceAttr != nil && attr.Value.Kind() != slog.KindGroup {
		attr = h.opts.ReplaceAttr(groups, attr)
		attr.Value = attr.Value.Resolve()
		if attr.Equal(slog.Attr{}) {
			return
		}
	}

	current := entry
	for _, group := range groups {
		child, ok := current[group].(map[string]any)
		if !ok {
			child = make(map[string]any)
			current[group] = child
		}
		current = child
	}
	if attr.Value.Kind() == slog.KindGroup {
		groupValues := groupValueToMap(attr.Value)
		if len(groupValues) == 0 {
			return
		}
		if existing, ok := current[attr.Key].(map[string]any); ok {
			maps.Copy(existing, groupValues)
			return
		}
		current[attr.Key] = groupValues
		return
	}
	current[attr.Key] = attrValueToAny(attr.Value)
}

func compactLevel(level slog.Level) string {
	levelStr := strings.ToUpper(level.String())
	if levelStr == "INFO" {
		return ""
	}
	switch levelStr {
	case "WARN":
		return "Warning"
	case "ERROR":
		return "Error"
	case "DEBUG":
		return "Debug"
	default:
		return levelStr
	}
}

func groupValueToMap(value slog.Value) map[string]any {
	result := make(map[string]any)
	for _, attr := range value.Group() {
		attr.Value = attr.Value.Resolve()
		if attr.Equal(slog.Attr{}) {
			continue
		}
		if attr.Value.Kind() == slog.KindGroup {
			nested := groupValueToMap(attr.Value)
			if len(nested) > 0 {
				result[attr.Key] = nested
			}
			continue
		}
		result[attr.Key] = attrValueToAny(attr.Value)
	}
	return result
}

func attrValueToAny(value slog.Value) any {
	value = value.Resolve()
	if err, ok := value.Any().(error); ok {
		return err.Error()
	}
	if value.Kind() == slog.KindGroup {
		return groupValueToMap(value)
	}
	return value.Any()
}
