package history

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/TiraelSedai/PhotoChallengeBot/internal/repository"
	"github.com/jmoiron/sqlx"
)

const (
	DefaultImporterUserID = -1
	importerDisplayName   = "history import"

	// Mirrors the live challenge flow so imported rows look like finished
	// challenges: submissions close at 18:00, voting lasts two days.
	acceptCloseHour = 18
	reminderBefore  = 30 * time.Hour
	votingDuration  = 48 * time.Hour
)

type Options struct {
	MainChatID int64
	// ImporterUserID owns the imported rows via created_by_user_id; negative
	// so it can never collide with a real Telegram account.
	ImporterUserID   int64
	ResultsChatID    *int64
	ResultsMessageID *int64
	// UserIDs maps lowercased winner usernames to Telegram IDs; matched
	// winners get user_id backfilled right away.
	UserIDs  map[string]int64
	Location *time.Location
	Now      time.Time
	Wipe     bool
}

type Stats struct {
	Challenges    int
	Winners       int
	WinnersWithID int
	// UnmatchedUsernames are winners missing from the UserIDs mapping.
	UnmatchedUsernames []string
}

// Import seeds finished challenges and their winners in one transaction.
// Lifecycle timestamps (results_pinned_at, achievements_sent_at,
// topic_report_sent_at) are set so the scheduler never picks the rows up.
func Import(ctx context.Context, database *sqlx.DB, records []Record, opts Options) (Stats, error) {
	if database == nil {
		return Stats{}, fmt.Errorf("import history: database is required")
	}
	if len(records) == 0 {
		return Stats{}, fmt.Errorf("import history: no records to import")
	}
	if opts.MainChatID == 0 {
		return Stats{}, fmt.Errorf("import history: main chat id is required")
	}
	if opts.Location == nil {
		return Stats{}, fmt.Errorf("import history: location is required")
	}
	if opts.Now.IsZero() {
		opts.Now = time.Now().UTC()
	}
	if opts.ImporterUserID == 0 {
		opts.ImporterUserID = DefaultImporterUserID
	}

	tx, err := database.BeginTxx(ctx, nil)
	if err != nil {
		return Stats{}, fmt.Errorf("import history: begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if opts.Wipe {
		if _, err := tx.ExecContext(ctx, `DELETE FROM challenges`); err != nil {
			return Stats{}, fmt.Errorf("import history: wipe challenges: %w", err)
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM users WHERE id = ?`, opts.ImporterUserID); err != nil {
			return Stats{}, fmt.Errorf("import history: wipe importer user: %w", err)
		}
	} else {
		var existing int
		if err := tx.GetContext(ctx, &existing, `SELECT COUNT(*) FROM challenges`); err != nil {
			return Stats{}, fmt.Errorf("import history: count challenges: %w", err)
		}
		if existing > 0 {
			return Stats{}, fmt.Errorf("import history: challenges table already has %d rows; back up the database and re-run with wipe enabled", existing)
		}
	}

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO users (id, display_name, updated_at)
		VALUES (?, ?, ?)
		ON CONFLICT (id) DO NOTHING
	`, opts.ImporterUserID, importerDisplayName, ts(opts.Now)); err != nil {
		return Stats{}, fmt.Errorf("import history: create importer user: %w", err)
	}

	maxNum := 0
	for _, record := range records {
		if record.Num > maxNum {
			maxNum = record.Num
		}
	}

	var stats Stats
	unmatched := make(map[string]bool)
	for _, record := range records {
		acceptStart := atHour(record.Start, 0, opts.Location)
		acceptUntil := atHour(record.End, acceptCloseHour, opts.Location)
		reminderAt := acceptUntil.Add(-reminderBefore)
		finishedAt := acceptUntil.Add(votingDuration)

		var resultsMessageID, resultsChatID *int64
		if record.Num == maxNum {
			resultsMessageID = opts.ResultsMessageID
			resultsChatID = opts.ResultsChatID
		}

		result, err := tx.ExecContext(ctx, `
			INSERT INTO challenges (
				main_chat_id, num, theme, hashtag, state,
				accept_start_at, accept_until_at, reminder_at, reminder_sent_at,
				vote_started_at, vote_until_at, vote_pinned_at, finished_at,
				results_message_id, results_chat_id, results_pinned_at,
				achievements_sent_at, topic_report_sent_at,
				created_by_user_id, created_at, updated_at
			)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		`, opts.MainChatID, record.Num, record.Theme, record.Hashtag, repository.ChallengeStateFinished,
			ts(acceptStart), ts(acceptUntil), ts(reminderAt), ts(reminderAt),
			ts(acceptUntil), ts(finishedAt), ts(acceptUntil), ts(finishedAt),
			resultsMessageID, resultsChatID, ts(finishedAt),
			ts(finishedAt), ts(finishedAt),
			opts.ImporterUserID, ts(acceptStart), ts(opts.Now))
		if err != nil {
			return Stats{}, fmt.Errorf("import history: insert challenge %d: %w", record.Num, err)
		}
		challengeID, err := result.LastInsertId()
		if err != nil {
			return Stats{}, fmt.Errorf("import history: challenge %d last insert id: %w", record.Num, err)
		}
		stats.Challenges++

		for _, username := range record.Winners {
			var userID *int64
			if id, ok := opts.UserIDs[strings.ToLower(username)]; ok {
				userID = &id
				stats.WinnersWithID++
				// challenge_winners.user_id references users; winners of the
				// old chat are unknown to the bot, so seed them here.
				if _, err := tx.ExecContext(ctx, `
					INSERT INTO users (id, username, display_name, updated_at)
					VALUES (?, ?, ?, ?)
					ON CONFLICT (id) DO NOTHING
				`, id, username, username, ts(opts.Now)); err != nil {
					return Stats{}, fmt.Errorf("import history: insert winner user %q: %w", username, err)
				}
			} else if !unmatched[strings.ToLower(username)] {
				unmatched[strings.ToLower(username)] = true
				stats.UnmatchedUsernames = append(stats.UnmatchedUsernames, username)
			}
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO challenge_winners (challenge_id, username, user_id, created_at, updated_at)
				VALUES (?, ?, ?, ?, ?)
			`, challengeID, username, userID, ts(opts.Now), ts(opts.Now)); err != nil {
				return Stats{}, fmt.Errorf("import history: insert winner %q of challenge %d: %w", username, record.Num, err)
			}
			stats.Winners++
		}
	}

	if err := tx.Commit(); err != nil {
		return Stats{}, fmt.Errorf("import history: commit: %w", err)
	}
	return stats, nil
}

func atHour(day time.Time, hour int, loc *time.Location) time.Time {
	return time.Date(day.Year(), day.Month(), day.Day(), hour, 0, 0, 0, loc)
}

func ts(value time.Time) string {
	return value.UTC().Format(repository.DBTimeFormat)
}
