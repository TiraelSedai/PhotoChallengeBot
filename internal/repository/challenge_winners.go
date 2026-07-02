package repository

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/jmoiron/sqlx"
)

type ChallengeWinners struct {
	db *sqlx.DB
}

func NewChallengeWinners(db *sqlx.DB) *ChallengeWinners {
	if db == nil {
		panic("sql db is nil")
	}
	return &ChallengeWinners{db: db}
}

// UpsertMany records all winners of an announced challenge atomically. The transaction
// only guards against a partial write within this batch; per AGENTS.md a crash between
// the Telegram announcement and this write is an end-of-world scenario (Docker restart at
// the wrong moment), not a retry contract, so nothing compensates for that case here.
func (r *ChallengeWinners) UpsertMany(ctx context.Context, winners []ChallengeWinner) error {
	if len(winners) == 0 {
		return nil
	}

	tx, err := r.db.BeginTxx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin challenge winners upsert: %w", err)
	}
	defer tx.Rollback()

	for _, winner := range winners {
		winner.Username = normalizeUsername(winner.Username)
		if winner.Username == "" {
			return fmt.Errorf("upsert challenge winner: username is required")
		}
		if winner.CreatedAt.IsZero() {
			winner.CreatedAt = time.Now().UTC()
		}
		if winner.UpdatedAt.IsZero() {
			winner.UpdatedAt = winner.CreatedAt
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO challenge_winners (challenge_id, username, user_id, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?)
			ON CONFLICT (challenge_id, username) DO UPDATE SET
				user_id = COALESCE(excluded.user_id, user_id),
				updated_at = excluded.updated_at
		`, winner.ChallengeID, winner.Username, nullableInt64(winner.UserID),
			timeString(winner.CreatedAt), timeString(winner.UpdatedAt)); err != nil {
			return fmt.Errorf("upsert challenge winner: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit challenge winners upsert: %w", err)
	}
	return nil
}

// CountWinsByUserThrough counts a user's finished-challenge wins from the canonical
// challenge_winners table up to and including the given challenge. This table records
// each finished challenge's winner directly, so it stays correct even for older
// challenges whose photos/votes were never stored — unlike recomputing wins from the
// votes table. Rows with a NULL user_id (winners whose Telegram id was never resolved)
// are unattributable and thus not counted.
func (r *ChallengeWinners) CountWinsByUserThrough(ctx context.Context, userID int64, finishedAt time.Time, challengeID int64) (int, error) {
	var count int
	if err := r.db.GetContext(ctx, &count, `
		SELECT COUNT(DISTINCT cw.challenge_id)
		FROM challenge_winners cw
		JOIN challenges c ON c.id = cw.challenge_id
		WHERE cw.user_id = ?
			AND c.state = 'finished'
			AND c.finished_at IS NOT NULL
			AND (
				julianday(c.finished_at) < julianday(?)
				OR (
					julianday(c.finished_at) = julianday(?)
					AND c.id <= ?
				)
			)
	`, userID, timeString(finishedAt), timeString(finishedAt), challengeID); err != nil {
		return 0, fmt.Errorf("count wins by user through challenge: %w", err)
	}
	return count, nil
}

func (r *ChallengeWinners) ListAll(ctx context.Context) (map[int64][]ChallengeWinner, error) {
	var rows []challengeWinnerRow
	if err := r.db.SelectContext(ctx, &rows, challengeWinnerSelectSQL+`
		ORDER BY challenge_id ASC, username ASC
	`); err != nil {
		return nil, fmt.Errorf("list all challenge winners: %w", err)
	}

	winners := make(map[int64][]ChallengeWinner, len(rows))
	for _, row := range rows {
		winner, err := row.challengeWinner()
		if err != nil {
			return nil, err
		}
		winners[winner.ChallengeID] = append(winners[winner.ChallengeID], winner)
	}
	return winners, nil
}

const challengeWinnerSelectSQL = `
	SELECT challenge_id, username, user_id, created_at, updated_at
	FROM challenge_winners
`

type challengeWinnerRow struct {
	ChallengeID int64         `db:"challenge_id"`
	Username    string        `db:"username"`
	UserID      sql.NullInt64 `db:"user_id"`
	CreatedAt   string        `db:"created_at"`
	UpdatedAt   string        `db:"updated_at"`
}

func (r challengeWinnerRow) challengeWinner() (ChallengeWinner, error) {
	createdAt, err := parseTime(r.CreatedAt)
	if err != nil {
		return ChallengeWinner{}, fmt.Errorf("parse challenge winner created_at: %w", err)
	}
	updatedAt, err := parseTime(r.UpdatedAt)
	if err != nil {
		return ChallengeWinner{}, fmt.Errorf("parse challenge winner updated_at: %w", err)
	}
	return ChallengeWinner{
		ChallengeID: r.ChallengeID,
		Username:    r.Username,
		UserID:      int64PtrFromNull(r.UserID),
		CreatedAt:   createdAt,
		UpdatedAt:   updatedAt,
	}, nil
}
