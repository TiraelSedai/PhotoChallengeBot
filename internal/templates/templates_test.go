package templates

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadRendersTemplateWithStructData(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeTemplate(t, dir, "hello.md.tmpl", "Hello, {{md .Name}}!")

	renderer, err := Load(dir)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	got, err := renderer.Render("hello.md.tmpl", struct {
		Name string
	}{
		Name: "Ada_Lovelace *admin* [x]",
	})
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}

	want := `Hello, Ada\_Lovelace \*admin\* \[x]!`
	if got != want {
		t.Fatalf("Render() = %q, want %q", got, want)
	}
}

func TestLoadRendersTemplateWithMapData(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeTemplate(t, dir, "link.md.tmpl", "[{{mdLinkText .Text}}]({{mdLinkURL .URL}})")

	renderer, err := Load(dir)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	got, err := renderer.Render("link.md.tmpl", map[string]string{
		"Text": "vote_[now]",
		"URL":  `https://t.me/photo_bot?start=chat_1)\next`,
	})
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}

	want := `[vote\_\[now\]](https://t.me/photo_bot?start=chat_1\)\\next)`
	if got != want {
		t.Fatalf("Render() = %q, want %q", got, want)
	}
}

func TestEscapeMarkdownEscapesTelegramMarkdownMetaCharacters(t *testing.T) {
	t.Parallel()

	got := EscapeMarkdown("name_ *topic* `code` [label]")
	want := `name\_ \*topic\* \` + "`" + `code\` + "`" + ` \[label]`
	if got != want {
		t.Fatalf("EscapeMarkdown() = %q, want %q", got, want)
	}
}

func TestChallengeAnnouncementSnapshot(t *testing.T) {
	t.Parallel()

	renderer, err := Load(filepath.Join("..", "..", "templates"))
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	got, err := renderer.Render(ChallengeAnnouncementTemplate, ChallengeAnnouncementData{
		Num:        12,
		Theme:      "Ночь_город *финал* [test] (v2)",
		Hashtag:    "#photo_challenge[12]",
		StartDate:  "1 июня",
		EndDate:    "18 июня",
		EndWeekday: "четверга",
	})
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}

	want := `*Челлендж 12 — Ночь\_город \*финал\* \[test] (v2)*

Фото присылайте до 18:00 МСК четверга, 18 июня. Можно несколько, но в конкурс пойдёт только первое.

Фото должно быть снято с 1 июня по 18 июня. Под фото ставьте тег #photo\_challenge\[12].

Тему трактуем свободно, но без животных, нюдсов и жестокости.

В конце пришлю форму для голосования.

Победитель получит ачивку в Клубе, а его фото станет обложкой чата.
`
	if got != want {
		t.Fatalf("challenge announcement snapshot mismatch\n got:\n%s\nwant:\n%s", got, want)
	}
}

func TestChallengeAnnouncementIncludesPreviousResultsLink(t *testing.T) {
	t.Parallel()

	renderer, err := Load(filepath.Join("..", "..", "templates"))
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	got, err := renderer.Render(ChallengeAnnouncementTemplate, ChallengeAnnouncementData{
		Num:             12,
		Theme:           "Ночь",
		Hashtag:         "#photo",
		StartDate:       "1 июня",
		EndDate:         "18 июня",
		EndWeekday:      "четверга",
		PrevResultsLink: "https://t.me/c/1272818469/42",
	})
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}

	wantLine := "\n\nРезультаты прошлого челленджа — [вот тут](https://t.me/c/1272818469/42).\n"
	if !strings.HasSuffix(got, wantLine) {
		t.Fatalf("challenge announcement missing previous results link\n got:\n%s", got)
	}
}

func TestVoteStartSnapshot(t *testing.T) {
	t.Parallel()

	renderer, err := Load(filepath.Join("..", "..", "templates"))
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	got, err := renderer.VoteStart(VoteStartData{
		Theme:       "Ночь_город *финал* [test]",
		AmountPhoto: 31,
		VoteLink:    "https://t.me/photoshnaya_bot?start=-1001272818469_3",
		ResultsDate: "20 мая в 18:00 МСК",
	})
	if err != nil {
		t.Fatalf("VoteStart() error = %v", err)
	}

	want := `Челлендж «Ночь\_город \*финал\* \[test]» закончился. На голосование ушло 31 фото.

Смотрим работы и голосуем [вот тут](https://t.me/photoshnaya_bot?start=-1001272818469_3). Голосовать можно за несколько фото.

Победитель получит ачивку в Клубе, а его фото станет обложкой чата.

Итоги подведу 20 мая в 18:00 МСК.

До окончания голосования предлагайте новые темы с тегом #тема
`
	if got != want {
		t.Fatalf("vote start snapshot mismatch\n got:\n%s\nwant:\n%s", got, want)
	}
}

func TestResultsSnapshotSingleWinner(t *testing.T) {
	t.Parallel()

	renderer, err := Load(filepath.Join("..", "..", "templates"))
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	got, err := renderer.Results(ResultsData{
		Theme:       "Ночь",
		TotalVoters: 7,
		Winners:     []ResultLine{{AuthorHandle: "@ada", FullName: "Ada", Likes: 5, Winner: true}},
	})
	if err != nil {
		t.Fatalf("Results() error = %v", err)
	}

	want := `Итоги челленджа «Ночь».
Всего проголосовавших: 7

Победитель:

@ada, Ada — 5 лайков

Поздравляем! 🎉
`
	if got != want {
		t.Fatalf("results snapshot mismatch\n got:\n%s\nwant:\n%s", got, want)
	}
}

func TestResultsSnapshotMultipleWinners(t *testing.T) {
	t.Parallel()

	renderer, err := Load(filepath.Join("..", "..", "templates"))
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	got, err := renderer.Results(ResultsData{
		Theme:           "Ночь",
		MultipleWinners: true,
		TotalVoters:     9,
		Winners: []ResultLine{
			{AuthorHandle: "@ada", FullName: "Ada", Likes: 5, Winner: true},
			{AuthorHandle: "@bob", FullName: "Bob", Likes: 5, Winner: true},
		},
	})
	if err != nil {
		t.Fatalf("Results() error = %v", err)
	}

	want := `Итоги челленджа «Ночь».
Всего проголосовавших: 9

Победители:

• @ada, Ada — 5 лайков
• @bob, Bob — 5 лайков

Поздравляем! 🎉
`
	if got != want {
		t.Fatalf("results snapshot mismatch\n got:\n%s\nwant:\n%s", got, want)
	}
}

func TestResultsSnapshotNoWinners(t *testing.T) {
	t.Parallel()

	renderer, err := Load(filepath.Join("..", "..", "templates"))
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	got, err := renderer.Results(ResultsData{NoWinners: true})
	if err != nil {
		t.Fatalf("Results() error = %v", err)
	}

	want := "Пока нет победителя — ещё никто не проголосовал.\n"
	if got != want {
		t.Fatalf("results snapshot mismatch\n got:\n%q\nwant:\n%q", got, want)
	}
}

func writeTemplate(t *testing.T, dir, name, contents string) {
	t.Helper()

	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatalf("write test template: %v", err)
	}
}
