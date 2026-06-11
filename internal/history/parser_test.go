package history

import (
	"strings"
	"testing"
	"time"
)

const parserFixture = `,,,,,,,,,,,,,,,,
,✖️ Вастрик.Фотки,,,,,,,,,,,,,,,
,,,,,,,,,,одна победа,☆,,,,https://example.com,
,,с,по,тема,тег,"фото
+ кол-во фото","форма
+ кол-во ответов",,,tg,клуб,🎟,☆,,Рандомайзер,
1,TRUE,27.9.21,4.10.21,Круглое,#неделякруглого,20,—,,фото,freilin,YieldNull,TRUE,TRUE,,,
2,TRUE,5.10.21,11.10.21,Сложная геометрия,#неделя2,13,17 ответов,,фото,artemiy_mne,B91,TRUE,TRUE,,,
,TRUE,5.10.21,11.10.21,Сложная геометрия,#неделя2,13,17 ответов,,фото,@extra_winner,extra,TRUE,TRUE,,,
,TRUE,5.10.21,11.10.21,Сложная геометрия,#неделя2,13,17 ответов,,фото,Artemiy_Mne,B91,TRUE,TRUE,,,
4,TRUE,23.10.22,2.10.22,Ранняя осень,комплиментарныецвета ,21,26 ответов,,фото,TheLonelyWarrior,argz,TRUE,TRUE,,,
,FALSE,,,Вертикальное,,,,,,,,FALSE,FALSE,,,
,FALSE,,,Весна,,,,,,,,FALSE,FALSE,,,
`

func mustMoscow(t *testing.T) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation("Europe/Moscow")
	if err != nil {
		t.Fatalf("LoadLocation() error = %v", err)
	}
	return loc
}

func TestParseReadsChallengesWinnersAndQuirks(t *testing.T) {
	t.Parallel()

	loc := mustMoscow(t)
	records, warnings, err := Parse(strings.NewReader(parserFixture), loc)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	if len(records) != 3 {
		t.Fatalf("records = %d, want 3", len(records))
	}

	first := records[0]
	if first.Num != 1 || first.Theme != "Круглое" || first.Hashtag != "#неделякруглого" {
		t.Fatalf("first record = %+v", first)
	}
	wantStart := time.Date(2021, 9, 27, 0, 0, 0, 0, loc)
	if !first.Start.Equal(wantStart) {
		t.Fatalf("first.Start = %s, want %s", first.Start, wantStart)
	}
	if len(first.Winners) != 1 || first.Winners[0] != "freilin" {
		t.Fatalf("first.Winners = %v", first.Winners)
	}

	second := records[1]
	if len(second.Winners) != 2 || second.Winners[0] != "artemiy_mne" || second.Winners[1] != "extra_winner" {
		t.Fatalf("second.Winners = %v, want continuation winner without @ and case-insensitive dedup", second.Winners)
	}

	third := records[2]
	if third.Num != 4 {
		t.Fatalf("third.Num = %d, want 4", third.Num)
	}
	if third.Hashtag != "#комплиментарныецвета" {
		t.Fatalf("third.Hashtag = %q, want leading # added", third.Hashtag)
	}

	wantWarnings := []string{"jump from 2 to 4", "end 02.10.2022 is before start 23.10.2022", "duplicate winner"}
	for _, want := range wantWarnings {
		found := false
		for _, warning := range warnings {
			if strings.Contains(warning, want) {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("warnings = %v, want one containing %q", warnings, want)
		}
	}
}

func TestParseSniffsSemicolonDelimiter(t *testing.T) {
	t.Parallel()

	fixture := `;;с;по;тема;тег;"фото
+ кол-во фото";форма;;;tg;клуб;🎟;☆;;;
1;TRUE;27.9.21;4.10.21;Круглое;#неделякруглого;20;—;;фото;freilin;YieldNull;TRUE;TRUE;;;
;TRUE;27.9.21;4.10.21;Круглое;#неделякруглого;20;—;;фото;extra_winner;extra;TRUE;TRUE;;;
`
	records, _, err := Parse(strings.NewReader(fixture), mustMoscow(t))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("records = %d, want 1", len(records))
	}
	if got := records[0].Winners; len(got) != 2 || got[0] != "freilin" || got[1] != "extra_winner" {
		t.Fatalf("Winners = %v, want [freilin extra_winner]", got)
	}
}

func TestParseFailsOnDecreasingNumbers(t *testing.T) {
	t.Parallel()

	fixture := `2,TRUE,5.10.21,11.10.21,Тема,#тег,,,,,winner,,,,,,
1,TRUE,12.10.21,19.10.21,Тема2,#тег2,,,,,winner2,,,,,,
`
	if _, _, err := Parse(strings.NewReader(fixture), mustMoscow(t)); err == nil {
		t.Fatal("Parse() expected error on decreasing challenge numbers")
	}
}

func TestParseFailsWithoutChallenges(t *testing.T) {
	t.Parallel()

	fixture := `,FALSE,,,Вертикальное,,,,,,,,FALSE,FALSE,,,
`
	if _, _, err := Parse(strings.NewReader(fixture), mustMoscow(t)); err == nil {
		t.Fatal("Parse() expected error when no challenges found")
	}
}
