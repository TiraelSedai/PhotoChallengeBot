package admin

import "testing"

func TestIsValidHashtagAcceptsUnicodeLetters(t *testing.T) {
	for _, hashtag := range []string{"#вода", "#ночь_2026", "#東京"} {
		if !isValidHashtag(hashtag) {
			t.Fatalf("isValidHashtag(%q) = false, want true", hashtag)
		}
	}
}

func TestIsValidHashtagRejectsWhitespaceAndPunctuation(t *testing.T) {
	for _, hashtag := range []string{"#", "#two words", "#bad-tag"} {
		if isValidHashtag(hashtag) {
			t.Fatalf("isValidHashtag(%q) = true, want false", hashtag)
		}
	}
}
