package hashtag

import "testing"

func TestContainsMatchesHashtagWithBoundaries(t *testing.T) {
	for _, text := range []string{
		"#тема",
		"предлагаю #тема на потом",
		"предлагаю #ТЕМА",
		"предлагаю (#тема)",
	} {
		if !Contains(text, "#тема") {
			t.Fatalf("Contains(%q, #тема) = false, want true", text)
		}
	}
}

func TestContainsRejectsHashtagPrefixes(t *testing.T) {
	for _, text := range []string{
		"#тематика",
		"#тема_2026",
		"#темами",
		"слово#тема",
	} {
		if Contains(text, "#тема") {
			t.Fatalf("Contains(%q, #тема) = true, want false", text)
		}
	}
}
