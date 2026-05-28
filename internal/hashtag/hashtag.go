package hashtag

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

func Contains(text string, tag string) bool {
	tag = strings.ToLower(strings.TrimSpace(tag))
	if tag == "" || !strings.HasPrefix(tag, "#") {
		return false
	}

	lower := strings.ToLower(text)
	for offset := 0; offset < len(lower); {
		idx := strings.Index(lower[offset:], tag)
		if idx < 0 {
			return false
		}
		start := offset + idx
		end := start + len(tag)
		if boundaryBefore(lower, start) && boundaryAfter(lower, end) {
			return true
		}
		offset = end
	}
	return false
}

func boundaryBefore(value string, idx int) bool {
	if idx == 0 {
		return true
	}
	r, _ := utf8.DecodeLastRuneInString(value[:idx])
	return !isHashtagRune(r)
}

func boundaryAfter(value string, idx int) bool {
	if idx >= len(value) {
		return true
	}
	r, _ := utf8.DecodeRuneInString(value[idx:])
	return !isHashtagRune(r)
}

func isHashtagRune(r rune) bool {
	return r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r)
}
