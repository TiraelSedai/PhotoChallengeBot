package history

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/TiraelSedai/PhotoChallengeBot/internal/db"
	"github.com/TiraelSedai/PhotoChallengeBot/internal/repository"
	"github.com/jmoiron/sqlx"
)

const importTestChatID = int64(-1009999)

func openHistoryTestDB(t *testing.T) *sqlx.DB {
	t.Helper()

	database, err := db.Open(context.Background(), db.Options{
		Path:          filepath.Join(t.TempDir(), "bot.sqlite"),
		MigrationsDir: "../../migrations",
	})
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	return database
}

func importTestOptions(t *testing.T) Options {
	t.Helper()

	resultsChatID := int64(-1001272818469)
	resultsMessageID := int64(143054)
	return Options{
		MainChatID:       importTestChatID,
		ResultsChatID:    &resultsChatID,
		ResultsMessageID: &resultsMessageID,
		Location:         mustMoscow(t),
		Now:              time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC),
	}
}

func parseImportFixture(t *testing.T) []Record {
	t.Helper()

	records, _, err := Parse(strings.NewReader(parserFixture), mustMoscow(t))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	return records
}

func TestImportSeedsFinishedChallenges(t *testing.T) {
	t.Parallel()

	database := openHistoryTestDB(t)
	defer database.Close()
	ctx := context.Background()
	records := parseImportFixture(t)

	stats, err := Import(ctx, database, records, importTestOptions(t))
	if err != nil {
		t.Fatalf("Import() error = %v", err)
	}
	if stats.Challenges != 3 || stats.Winners != 4 {
		t.Fatalf("stats = %+v, want 3 challenges and 4 winners", stats)
	}

	challenges := repository.NewChallenges(database)
	nextNum, err := challenges.NextNum(ctx, importTestChatID)
	if err != nil {
		t.Fatalf("NextNum() error = %v", err)
	}
	if nextNum != 5 {
		t.Fatalf("NextNum() = %d, want max imported num + 1 = 5", nextNum)
	}

	latest, err := challenges.FindLatestWithResults(ctx, importTestChatID)
	if err != nil {
		t.Fatalf("FindLatestWithResults() error = %v", err)
	}
	if latest == nil || latest.Num != 4 {
		t.Fatalf("latest with results = %+v, want challenge 4", latest)
	}
	if latest.ResultsMessageID == nil || *latest.ResultsMessageID != 143054 {
		t.Fatalf("ResultsMessageID = %v, want 143054", latest.ResultsMessageID)
	}
	if got := latest.ResultsChat(); got != -1001272818469 {
		t.Fatalf("ResultsChat() = %d, want old chat -1001272818469", got)
	}

	winners, err := repository.NewChallengeWinners(database).ListAll(ctx)
	if err != nil {
		t.Fatalf("ListAll() winners error = %v", err)
	}
	total := 0
	for _, challengeWinners := range winners {
		total += len(challengeWinners)
	}
	if total != 4 {
		t.Fatalf("winner rows = %d, want 4", total)
	}
}

func TestImportBackfillsWinnerUserIDs(t *testing.T) {
	t.Parallel()

	database := openHistoryTestDB(t)
	defer database.Close()
	ctx := context.Background()

	opts := importTestOptions(t)
	opts.UserIDs = map[string]int64{
		"freilin":          168659798,
		"thelonelywarrior": 213656937,
	}
	stats, err := Import(ctx, database, parseImportFixture(t), opts)
	if err != nil {
		t.Fatalf("Import() error = %v", err)
	}

	if stats.WinnersWithID != 2 {
		t.Fatalf("WinnersWithID = %d, want 2", stats.WinnersWithID)
	}
	if got := strings.Join(stats.UnmatchedUsernames, ","); got != "artemiy_mne,extra_winner" {
		t.Fatalf("UnmatchedUsernames = %q, want artemiy_mne,extra_winner", got)
	}

	rows := map[string]*int64{}
	winners, err := repository.NewChallengeWinners(database).ListAll(ctx)
	if err != nil {
		t.Fatalf("ListAll() winners error = %v", err)
	}
	for _, challengeWinners := range winners {
		for _, winner := range challengeWinners {
			rows[winner.Username] = winner.UserID
		}
	}
	if id := rows["freilin"]; id == nil || *id != 168659798 {
		t.Fatalf("freilin user_id = %v, want 168659798", id)
	}
	if id := rows["TheLonelyWarrior"]; id == nil || *id != 213656937 {
		t.Fatalf("TheLonelyWarrior user_id = %v, want case-insensitive match 213656937", id)
	}
	if id := rows["artemiy_mne"]; id != nil {
		t.Fatalf("artemiy_mne user_id = %v, want NULL for unmatched winner", id)
	}

	var displayName string
	if err := database.GetContext(ctx, &displayName, `
		SELECT display_name FROM users WHERE id = 168659798
	`); err != nil {
		t.Fatalf("winner user row satisfying the user_id FK: %v", err)
	}
	if displayName != "freilin" {
		t.Fatalf("winner display_name = %q, want freilin", displayName)
	}
}

func TestImportKeepsSchedulerIdle(t *testing.T) {
	t.Parallel()

	database := openHistoryTestDB(t)
	defer database.Close()
	ctx := context.Background()

	if _, err := Import(ctx, database, parseImportFixture(t), importTestOptions(t)); err != nil {
		t.Fatalf("Import() error = %v", err)
	}

	challenges := repository.NewChallenges(database)
	farFuture := time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)

	due, err := challenges.ListDueReminders(ctx, importTestChatID, farFuture, 100)
	if err != nil {
		t.Fatalf("ListDueReminders() error = %v", err)
	}
	if len(due) != 0 {
		t.Fatalf("due reminders = %d, want 0", len(due))
	}

	closures, err := challenges.ListDueAcceptanceClosures(ctx, importTestChatID, farFuture, 100)
	if err != nil {
		t.Fatalf("ListDueAcceptanceClosures() error = %v", err)
	}
	if len(closures) != 0 {
		t.Fatalf("due acceptance closures = %d, want 0", len(closures))
	}

	votingClosures, err := challenges.ListDueVotingClosures(ctx, importTestChatID, farFuture, 100)
	if err != nil {
		t.Fatalf("ListDueVotingClosures() error = %v", err)
	}
	if len(votingClosures) != 0 {
		t.Fatalf("due voting closures = %d, want 0", len(votingClosures))
	}

	unpublished, err := challenges.ListUnpublishedResults(ctx, importTestChatID, 100)
	if err != nil {
		t.Fatalf("ListUnpublishedResults() error = %v", err)
	}
	if len(unpublished) != 0 {
		t.Fatalf("unpublished results = %d, want 0", len(unpublished))
	}

	achievements, err := challenges.ListUnsentAchievements(ctx, importTestChatID, 100)
	if err != nil {
		t.Fatalf("ListUnsentAchievements() error = %v", err)
	}
	if len(achievements) != 0 {
		t.Fatalf("unsent achievements = %d, want 0", len(achievements))
	}

	reports, err := challenges.ListUnsentTopicReports(ctx, importTestChatID, 100)
	if err != nil {
		t.Fatalf("ListUnsentTopicReports() error = %v", err)
	}
	if len(reports) != 0 {
		t.Fatalf("unsent topic reports = %d, want 0", len(reports))
	}
}

func TestImportRefusesNonEmptyDBAndWipes(t *testing.T) {
	t.Parallel()

	database := openHistoryTestDB(t)
	defer database.Close()
	ctx := context.Background()
	records := parseImportFixture(t)
	opts := importTestOptions(t)

	if _, err := Import(ctx, database, records, opts); err != nil {
		t.Fatalf("first Import() error = %v", err)
	}

	if _, err := Import(ctx, database, records, opts); err == nil {
		t.Fatal("second Import() expected refusal on non-empty challenges table")
	}

	opts.Wipe = true
	stats, err := Import(ctx, database, records, opts)
	if err != nil {
		t.Fatalf("Import() with wipe error = %v", err)
	}
	if stats.Challenges != 3 {
		t.Fatalf("stats after wipe = %+v, want 3 challenges", stats)
	}

	var count int
	if err := database.GetContext(ctx, &count, `SELECT COUNT(*) FROM challenges`); err != nil {
		t.Fatalf("count challenges: %v", err)
	}
	if count != 3 {
		t.Fatalf("challenges after wipe = %d, want 3", count)
	}
	if err := database.GetContext(ctx, &count, `SELECT COUNT(*) FROM challenge_winners`); err != nil {
		t.Fatalf("count winners: %v", err)
	}
	if count != 4 {
		t.Fatalf("winners after wipe = %d, want 4 (cascade cleared old rows)", count)
	}
}
