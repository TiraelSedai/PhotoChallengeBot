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

func (r *ChallengeWinners) Upsert(ctx context.Context, winner ChallengeWinner) error {
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

	if _, err := r.db.ExecContext(ctx, `
		INSERT INTO challenge_winners (challenge_id, username, user_id, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT (challenge_id, username) DO UPDATE SET
			user_id = COALESCE(excluded.user_id, user_id),
			updated_at = excluded.updated_at
	`, winner.ChallengeID, winner.Username, nullableInt64(winner.UserID),
		timeString(winner.CreatedAt), timeString(winner.UpdatedAt)); err != nil {
		return fmt.Errorf("upsert challenge winner: %w", err)
	}
	return nil
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
