package topic

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/TiraelSedai/PhotoChallengeBot/internal/db"
	"github.com/TiraelSedai/PhotoChallengeBot/internal/repository"
	"github.com/go-telegram/bot/models"
)

func TestReporterPublishesTopicPollFromCollectedSuggestions(t *testing.T) {
	for _, tc := range []struct {
		name     string
		authors  []int64
		want     []string
		pickLast bool
	}{
		{name: "keep all below limit", authors: []int64{1, 1, 2}, want: []string{"тема 0", "тема 1", "тема 2"}},
		{name: "keep all at limit", authors: []int64{1, 1, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10}},
		{name: "remove duplicate author topics first", authors: []int64{1, 1, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12}, want: []string{"тема 2", "тема 3", "тема 4", "тема 5", "тема 6", "тема 7", "тема 8", "тема 9", "тема 10", "тема 11", "тема 12", "тема 13"}},
		{name: "remove unique authors after duplicates", authors: []int64{1, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13}, want: []string{"тема 2", "тема 3", "тема 4", "тема 5", "тема 6", "тема 7", "тема 8", "тема 9", "тема 10", "тема 11", "тема 12", "тема 13"}},
		{name: "random choice reaches last topic and author", authors: []int64{1, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13}, pickLast: true, want: []string{"тема 0", "тема 2", "тема 3", "тема 4", "тема 5", "тема 6", "тема 7", "тема 8", "тема 9", "тема 10", "тема 11", "тема 12"}},
		{name: "random choice between duplicate authors", authors: []int64{1, 1, 2, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11}, pickLast: true, want: []string{"тема 0", "тема 1", "тема 2", "тема 4", "тема 5", "тема 6", "тема 7", "тема 8", "тема 9", "тема 10", "тема 11", "тема 12"}},
		{name: "stop removing duplicates at twelve", authors: []int64{1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1}, want: []string{"тема 1", "тема 2", "тема 3", "тема 4", "тема 5", "тема 6", "тема 7", "тема 8", "тема 9", "тема 10", "тема 11", "тема 12"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			texts := make([]string, len(tc.authors))
			for i := range texts {
				texts[i] = fmt.Sprintf("#тема тема %d", i)
			}
			cfg, challenge := topicPollFixture(t, tc.authors, texts)
			if tc.pickLast {
				cfg.RandomIndex = func(n int) int { return n - 1 }
			}
			publisher := cfg.Publisher.(*MoqReportPublisher)
			reporter := NewReporter(cfg)
			if err := reporter.PublishOne(context.Background(), challenge); err != nil {
				t.Fatal(err)
			}
			calls := publisher.SendPollCalls()
			if len(calls) != 1 {
				t.Fatalf("poll calls = %d, want 1", len(calls))
			}
			call := calls[0]
			if call.N != challenge.MainChatID || call.S != topicPollQuestion {
				t.Fatalf("poll = %+v", call)
			}
			want := tc.want
			if want == nil {
				for i := range texts {
					want = append(want, fmt.Sprintf("тема %d", i))
				}
			}
			if !reflect.DeepEqual(call.Strings, want) {
				t.Fatalf("options = %v, want %v", call.Strings, want)
			}
			reports := publisher.SendTextCalls()
			if len(reports) != 1 || reports[0].N != cfg.AdminChatID {
				t.Fatalf("reports = %+v", reports)
			}
			for _, text := range texts {
				if !strings.Contains(reports[0].S, text) {
					t.Fatalf("admin report lost %q", text)
				}
			}
			// A fresh reporter and stale challenge snapshot must not republish either phase.
			reporter = NewReporter(cfg)
			if err := reporter.PublishOne(context.Background(), challenge); err != nil {
				t.Fatal(err)
			}
			if err := reporter.PublishDue(context.Background(), challenge.MainChatID, 100); err != nil {
				t.Fatal(err)
			}
			if len(publisher.SendPollCalls()) != 1 || len(publisher.SendTextCalls()) != 1 {
				t.Fatal("publication repeated")
			}
		})
	}
}

func TestReporterRetriesOnlyFailedTopicPublication(t *testing.T) {
	for _, failedPhase := range []string{"poll", "report"} {
		t.Run(failedPhase, func(t *testing.T) {
			cfg, challenge := topicPollFixture(t, []int64{1, 2}, []string{"#тема лес", "море #тема"})
			publisher := cfg.Publisher.(*MoqReportPublisher)
			wantErr := errors.New("telegram unavailable")
			if failedPhase == "poll" {
				publisher.SendPollFunc = func(context.Context, int64, string, []string) (int, error) { return 0, wantErr }
			} else {
				publisher.SendTextFunc = func(context.Context, int64, string) (int, error) { return 0, wantErr }
			}
			if err := NewReporter(cfg).PublishOne(context.Background(), challenge); !errors.Is(err, wantErr) {
				t.Fatalf("error = %v", err)
			}
			publisher.SendPollFunc = func(context.Context, int64, string, []string) (int, error) { return 1, nil }
			publisher.SendTextFunc = func(context.Context, int64, string) (int, error) { return 1, nil }
			if err := NewReporter(cfg).PublishDue(context.Background(), challenge.MainChatID, 100); err != nil {
				t.Fatal(err)
			}
			wantPolls, wantReports := 1, 1
			if failedPhase == "poll" {
				wantPolls++
			} else {
				wantReports++
			}
			if len(publisher.SendPollCalls()) != wantPolls || len(publisher.SendTextCalls()) != wantReports {
				t.Fatalf("poll calls = %d, report calls = %d", len(publisher.SendPollCalls()), len(publisher.SendTextCalls()))
			}
		})
	}
}

func TestReporterNormalizesTopicPollOptions(t *testing.T) {
	cfg, challenge := topicPollFixture(t, []int64{1, 2, 3, 4}, []string{"#ТЕМА \n осенний  лес", "#тема", "#тема " + strings.Repeat("я", 120), "#тема #тематический вечер"})
	if err := NewReporter(cfg).PublishOne(context.Background(), challenge); err != nil {
		t.Fatal(err)
	}
	options := cfg.Publisher.(*MoqReportPublisher).SendPollCalls()[0].Strings
	if !reflect.DeepEqual(options, []string{"осенний лес", strings.Repeat("я", 97) + "...", "#тематический вечер"}) {
		t.Fatalf("options = %v", options)
	}
	for _, text := range options {
		if !utf8.ValidString(text) || utf8.RuneCountInString(text) > 100 {
			t.Fatalf("invalid option %q", text)
		}
	}
}

func TestReporterRemovesOnlyStandaloneSuggestionHashtags(t *testing.T) {
	for _, tc := range []struct {
		text string
		want string
	}{
		{"#тема слово#тема", "слово#тема"},
		{"#ТЕМА #тематический вечер #тема_2026", "#тематический вечер #тема_2026"},
		{"лес #тема и море #ТЕМА", "лес и море"},
		{"#тема#тема лес", "#тема лес"},
	} {
		t.Run(tc.text, func(t *testing.T) {
			cfg, challenge := topicPollFixture(t, []int64{1}, []string{tc.text})
			if err := NewReporter(cfg).PublishOne(context.Background(), challenge); err != nil {
				t.Fatal(err)
			}
			calls := cfg.Publisher.(*MoqReportPublisher).SendPollCalls()
			if len(calls) != 1 || !reflect.DeepEqual(calls[0].Strings, []string{tc.want}) {
				t.Fatalf("poll calls = %+v, want option %q", calls, tc.want)
			}
		})
	}
}

func TestReporterSkipsEmptyPollAndAcceptsSingleTopic(t *testing.T) {
	for _, texts := range [][]string{nil, {"#тема"}, {"#тема лес"}} {
		cfg, challenge := topicPollFixture(t, []int64{1}, texts)
		reporter := NewReporter(cfg)
		if err := reporter.PublishDue(context.Background(), challenge.MainChatID, 100); err != nil {
			t.Fatal(err)
		}
		if err := reporter.PublishDue(context.Background(), challenge.MainChatID, 100); err != nil {
			t.Fatal(err)
		}
		want := 0
		if len(texts) == 1 && texts[0] != "#тема" {
			want = 1
		}
		if got := len(cfg.Publisher.(*MoqReportPublisher).SendPollCalls()); got != want {
			t.Fatalf("poll calls = %d, want %d", got, want)
		}
		stored, err := cfg.Challenges.(*repository.Challenges).Get(context.Background(), challenge.ID)
		if err != nil || stored.TopicPollSentAt == nil {
			t.Fatalf("poll phase not completed: %+v, %v", stored, err)
		}
	}
}

func TestReporterCommitsPollAfterContextCancellation(t *testing.T) {
	cfg, challenge := topicPollFixture(t, []int64{1, 2}, []string{"#тема лес", "#тема море"})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cfg.Publisher.(*MoqReportPublisher).SendPollFunc = func(context.Context, int64, string, []string) (int, error) { cancel(); return 1, nil }
	if err := NewReporter(cfg).PublishOne(ctx, challenge); err != nil {
		t.Fatal(err)
	}
	if err := NewReporter(cfg).PublishDue(context.Background(), challenge.MainChatID, 100); err != nil {
		t.Fatal(err)
	}
	if len(cfg.Publisher.(*MoqReportPublisher).SendPollCalls()) != 1 {
		t.Fatal("poll repeated after cancellation")
	}
}

func TestNewReporterRejectsNilRandomIndex(t *testing.T) {
	cfg, _ := topicPollFixture(t, nil, nil)
	cfg.RandomIndex = nil
	defer func() {
		if recover() == nil {
			t.Fatal("NewReporter accepted nil random index")
		}
	}()
	NewReporter(cfg)
}

func topicPollFixture(t *testing.T, authors []int64, texts []string) (ReportConfig, repository.Challenge) {
	t.Helper()
	ctx := context.Background()
	database, err := db.Open(ctx, db.Options{Path: filepath.Join(t.TempDir(), "topics.sqlite"), MigrationsDir: "../../migrations"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	users := repository.NewUsers(database)
	if _, err := users.Upsert(ctx, repository.User{ID: 99, FirstName: "Admin"}); err != nil {
		t.Fatal(err)
	}
	challenges := repository.NewChallenges(database)
	now := time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)
	challenge, err := challenges.Create(ctx, repository.CreateChallengeInput{MainChatID: -1001, Num: 112, Theme: "Эстетика", Hashtag: "#challenge", AcceptStartAt: now.Add(-time.Hour), AcceptUntilAt: now, ReminderAt: now.Add(-time.Hour), CreatedByUserID: 99, CreatedAt: now})
	if err != nil {
		t.Fatal(err)
	}
	if changed, err := challenges.StartVoting(ctx, challenge.ID, now); err != nil || !changed {
		t.Fatalf("start voting: %v, %v", changed, err)
	}
	suggestions := repository.NewTopicSuggestions(database)
	service := NewService(Config{MainChatID: challenge.MainChatID, Challenges: challenges, Users: users, Suggestions: suggestions, Now: func() time.Time { return now }})
	for i, text := range texts {
		if err := service.HandleMainChatMessage(ctx, &models.Message{ID: i + 1, Chat: models.Chat{ID: challenge.MainChatID}, From: &models.User{ID: authors[i], FirstName: fmt.Sprintf("Author %d", authors[i])}, Text: text}); err != nil {
			t.Fatal(err)
		}
	}
	if changed, err := challenges.FinishVotingNow(ctx, challenge.ID, now.Add(time.Hour)); err != nil || !changed {
		t.Fatalf("finish voting: %v, %v", changed, err)
	}
	challenge, err = challenges.Get(ctx, challenge.ID)
	if err != nil {
		t.Fatal(err)
	}
	return ReportConfig{AdminChatID: -2002, Challenges: challenges, Suggestions: suggestions, Users: users, Publisher: &MoqReportPublisher{}, Now: func() time.Time { return now.Add(time.Hour) }, RandomIndex: func(int) int { return 0 }}, challenge
}
