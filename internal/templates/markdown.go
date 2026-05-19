package templates

import (
	"fmt"
	"strings"
)

const ParseModeMarkdown = "Markdown"

func EscapeMarkdown(value string) string {
	return escapeMarkdown(value, false)
}

func EscapeMarkdownLinkURL(value string) string {
	var builder strings.Builder
	builder.Grow(len(value))

	for _, r := range value {
		switch r {
		case '\\', ')':
			builder.WriteRune('\\')
		}
		builder.WriteRune(r)
	}

	return builder.String()
}

func EscapeMarkdownLinkText(value string) string {
	return escapeMarkdown(value, true)
}

func escapeMarkdown(value string, linkText bool) string {
	var builder strings.Builder
	builder.Grow(len(value))

	for _, r := range value {
		switch r {
		case '\\', '_', '*', '`', '[':
			builder.WriteRune('\\')
		case ']':
			if linkText {
				builder.WriteRune('\\')
			}
		}
		builder.WriteRune(r)
	}

	return builder.String()
}

func markdownTemplateValue(value any) string {
	if value == nil {
		return ""
	}
	return EscapeMarkdown(fmt.Sprint(value))
}

func markdownLinkTextTemplateValue(value any) string {
	if value == nil {
		return ""
	}
	return EscapeMarkdownLinkText(fmt.Sprint(value))
}

func markdownLinkURLTemplateValue(value any) string {
	if value == nil {
		return ""
	}
	return EscapeMarkdownLinkURL(fmt.Sprint(value))
}
