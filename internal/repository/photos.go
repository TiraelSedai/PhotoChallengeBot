package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/jmoiron/sqlx"
)

type Photos struct {
	db *sqlx.DB
}

var ErrAmbiguousUsername = errors.New("ambiguous username")

type UpsertPhotoInput struct {
	ChallengeID     int64
	AuthorUserID    int64
	FileID          string
	FileUniqueID    string
	SourceChatID    int64
	SourceMessageID int
	Caption         string
	SubmittedAt     time.Time
}

func NewPhotos(db *sqlx.DB) *Photos {
	if db == nil {
		panic("sql db is nil")
	}
	return &Photos{db: db}
}

func (r *Photos) UpsertCurrent(ctx context.Context, input UpsertPhotoInput) (Photo, bool, error) {
	if input.SubmittedAt.IsZero() {
		input.SubmittedAt = time.Now().UTC()
	}

	tx, err := r.db.BeginTxx(ctx, nil)
	if err != nil {
		return Photo{}, false, fmt.Errorf("begin photo upsert: %w", err)
	}
	defer tx.Rollback()

	var existingID int64
	err = tx.GetContext(ctx, &existingID, `
		SELECT id
		FROM photos
		WHERE challenge_id = ? AND author_user_id = ?
	`, input.ChallengeID, input.AuthorUserID)
	exists := err == nil
	if err != nil && err != sql.ErrNoRows {
		return Photo{}, false, fmt.Errorf("query existing photo: %w", err)
	}

	_, err = tx.ExecContext(ctx, `
		INSERT INTO photos (
			challenge_id, author_user_id, file_id, file_unique_id, source_chat_id,
			source_message_id, caption, submitted_at, updated_at
		)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(challenge_id, author_user_id) DO UPDATE SET
			file_id = excluded.file_id,
			file_unique_id = excluded.file_unique_id,
			source_chat_id = excluded.source_chat_id,
			source_message_id = excluded.source_message_id,
			caption = excluded.caption,
			submitted_at = excluded.submitted_at,
			updated_at = excluded.updated_at
	`, input.ChallengeID, input.AuthorUserID, input.FileID,
		nullableString(input.FileUniqueID), input.SourceChatID, input.SourceMessageID,
		nullableString(input.Caption), timeString(input.SubmittedAt), timeString(input.SubmittedAt))
	if err != nil {
		return Photo{}, false, fmt.Errorf("upsert current photo: %w", err)
	}

	var row photoRow
	if err := tx.GetContext(ctx, &row, photoSelectSQL+`
		WHERE challenge_id = ? AND author_user_id = ?
	`, input.ChallengeID, input.AuthorUserID); err != nil {
		return Photo{}, false, fmt.Errorf("get upserted photo: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return Photo{}, false, fmt.Errorf("commit photo upsert: %w", err)
	}

	photo, err := row.photo()
	if err != nil {
		return Photo{}, false, err
	}
	return photo, exists, nil
}

func (r *Photos) Get(ctx context.Context, id int64) (Photo, error) {
	var row photoRow
	if err := r.db.GetContext(ctx, &row, photoSelectSQL+" WHERE id = ?", id); err != nil {
		return Photo{}, fmt.Errorf("get photo %d: %w", id, err)
	}
	return row.photo()
}

func (r *Photos) CountByChallenge(ctx context.Context, challengeID int64) (int, error) {
	var count int
	if err := r.db.GetContext(ctx, &count, `
		SELECT COUNT(*)
		FROM photos
		WHERE challenge_id = ?
	`, challengeID); err != nil {
		return 0, fmt.Errorf("count photos by challenge: %w", err)
	}
	return count, nil
}

func (r *Photos) ListByChallenge(ctx context.Context, challengeID int64) ([]Photo, error) {
	var rows []photoRow
	if err := r.db.SelectContext(ctx, &rows, photoSelectSQL+`
		WHERE challenge_id = ?
		ORDER BY id ASC
	`, challengeID); err != nil {
		return nil, fmt.Errorf("list photos by challenge: %w", err)
	}

	photos := make([]Photo, 0, len(rows))
	for _, row := range rows {
		photo, err := row.photo()
		if err != nil {
			return nil, err
		}
		photos = append(photos, photo)
	}
	return photos, nil
}

func (r *Photos) DeleteByAuthorID(ctx context.Context, challengeID, authorUserID int64) (*Photo, error) {
	return r.deleteCurrent(ctx, challengeID, "author_user_id = ?", authorUserID)
}

func (r *Photos) DeleteByUsername(ctx context.Context, challengeID int64, username string) (*Photo, error) {
	username = normalizeUsername(username)
	if username == "" {
		return nil, fmt.Errorf("username is required")
	}

	tx, err := r.db.BeginTxx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin photo deletion: %w", err)
	}
	defer tx.Rollback()

	var rows []photoRow
	if err := tx.SelectContext(ctx, &rows, photoSelectSQL+`
		JOIN users ON users.id = photos.author_user_id
		WHERE photos.challenge_id = ? AND LOWER(users.username) = LOWER(?)
		ORDER BY photos.author_user_id
		LIMIT 2
	`, challengeID, username); err != nil {
		return nil, fmt.Errorf("find photo by username before deletion: %w", err)
	}
	if len(rows) == 0 {
		if err := tx.Commit(); err != nil {
			return nil, fmt.Errorf("commit no-op photo deletion: %w", err)
		}
		return nil, nil
	}
	if len(rows) > 1 {
		return nil, ErrAmbiguousUsername
	}

	return r.deleteRow(ctx, tx, rows[0])
}

func (r *Photos) deleteCurrent(
	ctx context.Context,
	challengeID int64,
	authorPredicate string,
	authorArg any,
) (*Photo, error) {
	tx, err := r.db.BeginTxx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin photo removal: %w", err)
	}
	defer tx.Rollback()

	var row photoRow
	if err := tx.GetContext(ctx, &row, photoSelectSQL+`
		WHERE challenge_id = ? AND `+authorPredicate, challengeID, authorArg); err != nil {
		if err == sql.ErrNoRows {
			if err := tx.Commit(); err != nil {
				return nil, fmt.Errorf("commit no-op photo deletion: %w", err)
			}
			return nil, nil
		}
		return nil, fmt.Errorf("get photo before deletion: %w", err)
	}

	return r.deleteRow(ctx, tx, row)
}

func (r *Photos) deleteRow(ctx context.Context, tx *sqlx.Tx, row photoRow) (*Photo, error) {
	if _, err := tx.ExecContext(ctx, `
		DELETE FROM photos
		WHERE id = ?
	`, row.ID); err != nil {
		return nil, fmt.Errorf("delete photo: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit photo deletion: %w", err)
	}

	photo, err := row.photo()
	if err != nil {
		return nil, err
	}
	return &photo, nil
}

const photoSelectSQL = `
	SELECT photos.id, photos.challenge_id, photos.author_user_id, photos.file_id,
		photos.file_unique_id, photos.source_chat_id, photos.source_message_id,
		photos.caption, photos.submitted_at, photos.updated_at
	FROM photos
`

type photoRow struct {
	ID              int64          `db:"id"`
	ChallengeID     int64          `db:"challenge_id"`
	AuthorUserID    int64          `db:"author_user_id"`
	FileID          string         `db:"file_id"`
	FileUniqueID    sql.NullString `db:"file_unique_id"`
	SourceChatID    int64          `db:"source_chat_id"`
	SourceMessageID int            `db:"source_message_id"`
	Caption         sql.NullString `db:"caption"`
	SubmittedAt     string         `db:"submitted_at"`
	UpdatedAt       string         `db:"updated_at"`
}

func (r photoRow) photo() (Photo, error) {
	submittedAt, err := parseTime(r.SubmittedAt)
	if err != nil {
		return Photo{}, fmt.Errorf("parse photo submitted_at: %w", err)
	}
	updatedAt, err := parseTime(r.UpdatedAt)
	if err != nil {
		return Photo{}, fmt.Errorf("parse photo updated_at: %w", err)
	}

	return Photo{
		ID:              r.ID,
		ChallengeID:     r.ChallengeID,
		AuthorUserID:    r.AuthorUserID,
		FileID:          r.FileID,
		FileUniqueID:    stringFromNull(r.FileUniqueID),
		SourceChatID:    r.SourceChatID,
		SourceMessageID: r.SourceMessageID,
		Caption:         stringFromNull(r.Caption),
		SubmittedAt:     submittedAt,
		UpdatedAt:       updatedAt,
	}, nil
}
