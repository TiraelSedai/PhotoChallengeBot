package hashtag

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

func Contains(text string, tag string) bool {
	start, _ := findTag(text, strings.TrimSpace(tag), 0)
	return start >= 0
}

// Remove removes matching hashtags using the same boundaries as Contains.
// The surrounding text, including whitespace, is preserved.
func Remove(text string, tag string) string {
	tag = strings.TrimSpace(tag)
	var result strings.Builder
	offset := 0
	for {
		start, end := findTag(text, tag, offset)
		if start < 0 {
			result.WriteString(text[offset:])
			return result.String()
		}
		result.WriteString(text[offset:start])
		offset = end
	}
}

func findTag(text, tag string, offset int) (int, int) {
	if !strings.HasPrefix(tag, "#") {
		return -1, -1
	}
	for idx, r := range text[offset:] {
		if r != '#' {
			continue
		}
		start := offset + idx
		if start > 0 {
			previous, _ := utf8.DecodeLastRuneInString(text[:start])
			if isHashtagRune(previous) {
				continue
			}
		}
		end := start + 1
		for _, next := range text[end:] {
			if !isHashtagRune(next) {
				break
			}
			end += utf8.RuneLen(next)
		}
		if strings.EqualFold(text[start:end], tag) {
			return start, end
		}
	}
	return -1, -1
}

func isHashtagRune(r rune) bool {
	return r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r)
}
