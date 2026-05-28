package logger

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"math"
	"reflect"
	"runtime"
	"strings"
	"sync"

	"github.com/TiraelSedai/PhotoChallengeBot/internal/require"
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
	require.NotNil("logger writer", w)
	require.NotNil("logger handler options", opts)
	optsCopy := *opts
	return &CompactJSONHandler{
		writer: w,
		opts:   &optsCopy,
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
	entry := make(map[string]any)
	if !r.Time.IsZero() {
		h.addAttr(entry, nil, slog.Time("@t", r.Time.UTC()))
	}
	h.addAttr(entry, nil, slog.String("@mt", r.Message))
	if h.opts.AddSource && r.PC != 0 {
		frame, _ := runtime.CallersFrames([]uintptr{r.PC}).Next()
		h.addAttr(entry, nil, slog.Any(slog.SourceKey, &slog.Source{
			Function: frame.Function,
			File:     frame.File,
			Line:     frame.Line,
		}))
	}

	if level := compactLevel(r.Level); level != "" {
		h.addAttr(entry, nil, slog.String("@l", level))
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
	if len(groups) == 0 {
		h.addAttrToMap(entry, groups, attr)
		return
	}
	staged := make(map[string]any)
	h.addAttrToMap(staged, groups, attr)
	if len(staged) == 0 {
		return
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
	mergeEntryMap(current, staged)
}

func (h *CompactJSONHandler) addAttrToMap(entry map[string]any, groups []string, attr slog.Attr) {
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

	if attr.Value.Kind() == slog.KindGroup {
		h.addGroupAttr(entry, groups, attr)
		return
	}
	entry[attr.Key] = attrValueToAny(attr.Value)
}

func (h *CompactJSONHandler) addGroupAttr(entry map[string]any, groups []string, attr slog.Attr) {
	groupAttrs := attr.Value.Group()
	if len(groupAttrs) == 0 {
		return
	}
	if attr.Key == "" {
		for _, groupAttr := range groupAttrs {
			h.addAttrToMap(entry, groups, groupAttr)
		}
		return
	}

	child := make(map[string]any)
	childGroups := append(append([]string{}, groups...), attr.Key)
	for _, groupAttr := range groupAttrs {
		h.addAttrToMap(child, childGroups, groupAttr)
	}
	if len(child) == 0 {
		return
	}
	if existing, ok := entry[attr.Key].(map[string]any); ok {
		mergeEntryMap(existing, child)
		return
	}
	entry[attr.Key] = child
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

func attrValueToAny(value slog.Value) any {
	value = value.Resolve()
	switch value.Kind() {
	case slog.KindBool:
		return value.Bool()
	case slog.KindDuration:
		return value.Duration()
	case slog.KindFloat64:
		floatValue := value.Float64()
		if math.IsNaN(floatValue) || math.IsInf(floatValue, 0) {
			return fmt.Sprint(floatValue)
		}
		return floatValue
	case slog.KindInt64:
		return value.Int64()
	case slog.KindString:
		return value.String()
	case slog.KindTime:
		return value.Time()
	case slog.KindUint64:
		return value.Uint64()
	}

	return jsonSafeAny(value.Any())
}

func jsonSafeAny(value any) any {
	if isTypedNil(value) {
		return nil
	}
	if err, ok := value.(error); ok {
		return err.Error()
	}
	if source, ok := value.(*slog.Source); ok {
		return map[string]any{
			"function": source.Function,
			"file":     source.File,
			"line":     source.Line,
		}
	}
	if value == nil {
		return nil
	}
	if jsonData, err := json.Marshal(value); err == nil {
		return json.RawMessage(jsonData)
	}
	return fmt.Sprint(value)
}

func mergeEntryMap(dst map[string]any, src map[string]any) {
	for key, value := range src {
		if existing, ok := dst[key].(map[string]any); ok {
			if nested, ok := value.(map[string]any); ok {
				mergeEntryMap(existing, nested)
				continue
			}
		}
		dst[key] = value
	}
}

func isTypedNil(value any) bool {
	if value == nil {
		return false
	}
	reflectValue := reflect.ValueOf(value)
	switch reflectValue.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return reflectValue.IsNil()
	default:
		return false
	}
}
