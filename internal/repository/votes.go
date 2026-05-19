package repository

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/jmoiron/sqlx"
)

const (
	VoteKindManual = "manual"
	VoteKindSelf   = "self"
)

type Votes struct {
	db *sqlx.DB
}

type VoteOrderItem struct {
	ChallengeID int64
	VoterUserID int64
	Position    int
	PhotoID     int64
}

type VoteProgress struct {
	ChallengeID     int64
	VoterUserID     int64
	CurrentPosition int
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

type Vote struct {
	ChallengeID int64
	VoterUserID int64
	PhotoID     int64
	Kind        string
	CreatedAt   time.Time
}

func NewVotes(db *sqlx.DB) *Votes {
	if db == nil {
		panic("sql db is nil")
	}
	return &Votes{db: db}
}

func (r *Votes) CreateVoteOrder(
	ctx context.Context,
	challengeID int64,
	voterUserID int64,
	photoIDs []int64,
) ([]VoteOrderItem, error) {
	tx, err := r.db.BeginTxx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin vote order creation: %w", err)
	}
	defer tx.Rollback()

	items := make([]VoteOrderItem, 0, len(photoIDs))
	for position, photoID := range photoIDs {
		item := VoteOrderItem{
			ChallengeID: challengeID,
			VoterUserID: voterUserID,
			Position:    position,
			PhotoID:     photoID,
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT OR IGNORE INTO vote_orders (challenge_id, voter_user_id, position, photo_id)
			VALUES (?, ?, ?, ?)
		`, item.ChallengeID, item.VoterUserID, item.Position, item.PhotoID); err != nil {
			return nil, fmt.Errorf("insert vote order item at position %d: %w", position, err)
		}
		items = append(items, item)
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit vote order creation: %w", err)
	}
	stored, err := r.ListVoteOrder(ctx, challengeID, voterUserID)
	if err != nil {
		return nil, err
	}
	if len(stored) == 0 && len(items) > 0 {
		return nil, fmt.Errorf("create vote order: no rows stored")
	}
	return stored, nil
}

func (r *Votes) ListVoteOrder(ctx context.Context, challengeID, voterUserID int64) ([]VoteOrderItem, error) {
	var rows []voteOrderRow
	if err := r.db.SelectContext(ctx, &rows, `
		SELECT challenge_id, voter_user_id, position, photo_id
		FROM vote_orders
		WHERE challenge_id = ? AND voter_user_id = ?
		ORDER BY position ASC
	`, challengeID, voterUserID); err != nil {
		return nil, fmt.Errorf("list vote order: %w", err)
	}

	items := make([]VoteOrderItem, 0, len(rows))
	for _, row := range rows {
		items = append(items, row.voteOrderItem())
	}
	return items, nil
}

func (r *Votes) GetProgress(ctx context.Context, challengeID, voterUserID int64) (*VoteProgress, error) {
	var row voteProgressRow
	if err := r.db.GetContext(ctx, &row, `
		SELECT challenge_id, voter_user_id, current_position, created_at, updated_at
		FROM vote_progress
		WHERE challenge_id = ? AND voter_user_id = ?
	`, challengeID, voterUserID); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("get vote progress: %w", err)
	}

	progress, err := row.voteProgress()
	if err != nil {
		return nil, err
	}
	return &progress, nil
}

func (r *Votes) UpsertProgress(ctx context.Context, progress VoteProgress) (VoteProgress, error) {
	if progress.UpdatedAt.IsZero() {
		progress.UpdatedAt = time.Now().UTC()
	}
	if progress.CreatedAt.IsZero() {
		progress.CreatedAt = progress.UpdatedAt
	}

	if _, err := r.db.ExecContext(ctx, `
		INSERT INTO vote_progress (
			challenge_id, voter_user_id, current_position, created_at, updated_at
		)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(challenge_id, voter_user_id) DO UPDATE SET
			current_position = excluded.current_position,
			updated_at = excluded.updated_at
	`, progress.ChallengeID, progress.VoterUserID, progress.CurrentPosition,
		timeString(progress.CreatedAt), timeString(progress.UpdatedAt)); err != nil {
		return VoteProgress{}, fmt.Errorf("upsert vote progress: %w", err)
	}

	stored, err := r.GetProgress(ctx, progress.ChallengeID, progress.VoterUserID)
	if err != nil {
		return VoteProgress{}, err
	}
	if stored == nil {
		return VoteProgress{}, fmt.Errorf("get upserted vote progress: no row")
	}
	return *stored, nil
}

func (r *Votes) AddManualVote(
	ctx context.Context,
	challengeID int64,
	voterUserID int64,
	photoID int64,
	createdAt time.Time,
) (Vote, error) {
	return r.addVote(ctx, Vote{
		ChallengeID: challengeID,
		VoterUserID: voterUserID,
		PhotoID:     photoID,
		Kind:        VoteKindManual,
		CreatedAt:   createdAt,
	})
}

func (r *Votes) AddSelfVote(
	ctx context.Context,
	challengeID int64,
	voterUserID int64,
	photoID int64,
	createdAt time.Time,
) (Vote, error) {
	return r.addVote(ctx, Vote{
		ChallengeID: challengeID,
		VoterUserID: voterUserID,
		PhotoID:     photoID,
		Kind:        VoteKindSelf,
		CreatedAt:   createdAt,
	})
}

func (r *Votes) EnsureSelfVotes(ctx context.Context, challengeID int64, photos []Photo, createdAt time.Time) error {
	if createdAt.IsZero() {
		createdAt = time.Now().UTC()
	}

	tx, err := r.db.BeginTxx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin self vote creation: %w", err)
	}
	defer tx.Rollback()

	for _, photo := range photos {
		if photo.ChallengeID != challengeID {
			return fmt.Errorf("self vote photo %d belongs to challenge %d, want %d", photo.ID, photo.ChallengeID, challengeID)
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO votes (challenge_id, voter_user_id, photo_id, kind, created_at)
			VALUES (?, ?, ?, ?, ?)
			ON CONFLICT(challenge_id, voter_user_id, photo_id) DO NOTHING
		`, challengeID, photo.AuthorUserID, photo.ID, VoteKindSelf, timeString(createdAt)); err != nil {
			return fmt.Errorf("insert self vote for photo %d: %w", photo.ID, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit self vote creation: %w", err)
	}
	return nil
}

func (r *Votes) RemoveManualVote(ctx context.Context, challengeID, voterUserID, photoID int64) (bool, error) {
	result, err := r.db.ExecContext(ctx, `
		DELETE FROM votes
		WHERE challenge_id = ? AND voter_user_id = ? AND photo_id = ? AND kind = ?
	`, challengeID, voterUserID, photoID, VoteKindManual)
	if err != nil {
		return false, fmt.Errorf("remove manual vote: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("manual vote removal rows affected: %w", err)
	}
	return rowsAffected > 0, nil
}

func (r *Votes) ManualVoteExists(ctx context.Context, challengeID, voterUserID, photoID int64) (bool, error) {
	var count int
	if err := r.db.GetContext(ctx, &count, `
		SELECT COUNT(*)
		FROM votes
		WHERE challenge_id = ? AND voter_user_id = ? AND photo_id = ? AND kind = ?
	`, challengeID, voterUserID, photoID, VoteKindManual); err != nil {
		return false, fmt.Errorf("check manual vote: %w", err)
	}
	return count > 0, nil
}

func (r *Votes) ListVotes(ctx context.Context, challengeID int64) ([]Vote, error) {
	var rows []voteRow
	if err := r.db.SelectContext(ctx, &rows, `
		SELECT challenge_id, voter_user_id, photo_id, kind, created_at
		FROM votes
		WHERE challenge_id = ?
		ORDER BY photo_id ASC, voter_user_id ASC
	`, challengeID); err != nil {
		return nil, fmt.Errorf("list votes: %w", err)
	}

	votes := make([]Vote, 0, len(rows))
	for _, row := range rows {
		vote, err := row.vote()
		if err != nil {
			return nil, err
		}
		votes = append(votes, vote)
	}
	return votes, nil
}

func (r *Votes) CountFinishedWinsByAuthor(ctx context.Context, authorUserID int64) (int, error) {
	return r.CountFinishedWinsByAuthorThrough(ctx, authorUserID, time.Now().UTC().AddDate(100, 0, 0), 1<<62)
}

func (r *Votes) CountFinishedWinsByAuthorThrough(ctx context.Context, authorUserID int64, finishedAt time.Time, challengeID int64) (int, error) {
	var count int
	if err := r.db.GetContext(ctx, &count, `
		WITH photo_scores AS (
			SELECT
				challenges.id AS challenge_id,
				photos.id AS photo_id,
				photos.author_user_id AS author_user_id,
				SUM(CASE WHEN votes.kind = 'manual' THEN 1 ELSE 0 END) AS manual_votes,
				COUNT(votes.photo_id) AS total_votes
			FROM challenges
			JOIN photos ON photos.challenge_id = challenges.id
			LEFT JOIN votes ON votes.challenge_id = photos.challenge_id
				AND votes.photo_id = photos.id
			WHERE challenges.state = 'finished'
				AND challenges.finished_at IS NOT NULL
				AND (
					julianday(challenges.finished_at) < julianday(?)
					OR (
						julianday(challenges.finished_at) = julianday(?)
						AND challenges.id <= ?
					)
				)
			GROUP BY challenges.id, photos.id, photos.author_user_id
		),
		challenge_manual_votes AS (
			SELECT challenge_id, SUM(manual_votes) AS manual_votes
			FROM photo_scores
			GROUP BY challenge_id
		),
		challenge_max_scores AS (
			SELECT challenge_id, MAX(total_votes) AS max_votes
			FROM photo_scores
			GROUP BY challenge_id
		)
		SELECT COUNT(*)
		FROM photo_scores
		JOIN challenge_manual_votes USING (challenge_id)
		JOIN challenge_max_scores USING (challenge_id)
		WHERE photo_scores.author_user_id = ?
			AND challenge_manual_votes.manual_votes > 0
			AND photo_scores.total_votes = challenge_max_scores.max_votes
	`, timeString(finishedAt), timeString(finishedAt), challengeID, authorUserID); err != nil {
		return 0, fmt.Errorf("count finished wins by author through challenge: %w", err)
	}
	return count, nil
}

func (r *Votes) addVote(ctx context.Context, vote Vote) (Vote, error) {
	if vote.CreatedAt.IsZero() {
		vote.CreatedAt = time.Now().UTC()
	}

	if _, err := r.db.ExecContext(ctx, `
		INSERT INTO votes (challenge_id, voter_user_id, photo_id, kind, created_at)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(challenge_id, voter_user_id, photo_id) DO NOTHING
	`, vote.ChallengeID, vote.VoterUserID, vote.PhotoID, vote.Kind, timeString(vote.CreatedAt)); err != nil {
		return Vote{}, fmt.Errorf("add vote: %w", err)
	}

	stored, err := r.getVote(ctx, vote.ChallengeID, vote.VoterUserID, vote.PhotoID)
	if err != nil {
		return Vote{}, err
	}
	if stored.Kind != vote.Kind {
		return Vote{}, fmt.Errorf("vote already exists with kind %q", stored.Kind)
	}
	return stored, nil
}

func (r *Votes) getVote(ctx context.Context, challengeID, voterUserID, photoID int64) (Vote, error) {
	var row voteRow
	if err := r.db.GetContext(ctx, &row, `
		SELECT challenge_id, voter_user_id, photo_id, kind, created_at
		FROM votes
		WHERE challenge_id = ? AND voter_user_id = ? AND photo_id = ?
	`, challengeID, voterUserID, photoID); err != nil {
		return Vote{}, fmt.Errorf("get vote: %w", err)
	}
	return row.vote()
}

type voteOrderRow struct {
	ChallengeID int64 `db:"challenge_id"`
	VoterUserID int64 `db:"voter_user_id"`
	Position    int   `db:"position"`
	PhotoID     int64 `db:"photo_id"`
}

func (r voteOrderRow) voteOrderItem() VoteOrderItem {
	return VoteOrderItem{
		ChallengeID: r.ChallengeID,
		VoterUserID: r.VoterUserID,
		Position:    r.Position,
		PhotoID:     r.PhotoID,
	}
}

type voteProgressRow struct {
	ChallengeID     int64  `db:"challenge_id"`
	VoterUserID     int64  `db:"voter_user_id"`
	CurrentPosition int    `db:"current_position"`
	CreatedAt       string `db:"created_at"`
	UpdatedAt       string `db:"updated_at"`
}

func (r voteProgressRow) voteProgress() (VoteProgress, error) {
	createdAt, err := parseTime(r.CreatedAt)
	if err != nil {
		return VoteProgress{}, fmt.Errorf("parse vote progress created_at: %w", err)
	}
	updatedAt, err := parseTime(r.UpdatedAt)
	if err != nil {
		return VoteProgress{}, fmt.Errorf("parse vote progress updated_at: %w", err)
	}
	return VoteProgress{
		ChallengeID:     r.ChallengeID,
		VoterUserID:     r.VoterUserID,
		CurrentPosition: r.CurrentPosition,
		CreatedAt:       createdAt,
		UpdatedAt:       updatedAt,
	}, nil
}

type voteRow struct {
	ChallengeID int64  `db:"challenge_id"`
	VoterUserID int64  `db:"voter_user_id"`
	PhotoID     int64  `db:"photo_id"`
	Kind        string `db:"kind"`
	CreatedAt   string `db:"created_at"`
}

func (r voteRow) vote() (Vote, error) {
	createdAt, err := parseTime(r.CreatedAt)
	if err != nil {
		return Vote{}, fmt.Errorf("parse vote created_at: %w", err)
	}
	return Vote{
		ChallengeID: r.ChallengeID,
		VoterUserID: r.VoterUserID,
		PhotoID:     r.PhotoID,
		Kind:        r.Kind,
		CreatedAt:   createdAt,
	}, nil
}
