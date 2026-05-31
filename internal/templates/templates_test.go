package templates

import (
	"os"
	"path/filepath"
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
		Theme:      "Ночь_город *финал* [test]",
		Hashtag:    "#photo_challenge[12]",
		StartDate:  "1 июня",
		EndDate:    "18 июня",
		EndWeekday: "четверга",
	})
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}

	want := `*Челлендж 12 - Ночь\_город \*финал\* \[test]*

Свежие фото присылайте в чат с 1 июня:
- до вечера четверга, 18 июня;
- на конкурс идет одна актуальная фотография от участника;
- если прислать новую фотографию с тегом челленджа, она заменит старую;
- фото должно быть сделано в промежутке с 1 июня по 18 июня;
- под фото для конкурса пишите тег #photo\_challenge\[12]

Тема трактуется свободно, но:
- без животных;
- без нюдсов;
- без жестокости.

В конце пришлю форму для голосования. Победитель получит ачивку в клубе, а фоточка отправится на обложку чата.
`
	if got != want {
		t.Fatalf("challenge announcement snapshot mismatch\n got:\n%s\nwant:\n%s", got, want)
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

	want := `Закончился челлендж Ночь\_город \*финал\* \[test]: 31 фоток ушло на голосование.

Рассматриваем работы и голосуем [вот тут](https://t.me/photoshnaya_bot?start=-1001272818469_3)
Голосовать можно за несколько фоток.

Победитель получит ачивку в клубе, а фоточка отправится на обложку чата.

Итоги подведу 20 мая в 18:00 МСК.

До окончания голосования предлагайте новые темы с тегом #тема
`
	if got != want {
		t.Fatalf("vote start snapshot mismatch\n got:\n%s\nwant:\n%s", got, want)
	}
}

func writeTemplate(t *testing.T, dir, name, contents string) {
	t.Helper()

	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatalf("write test template: %v", err)
	}
}
