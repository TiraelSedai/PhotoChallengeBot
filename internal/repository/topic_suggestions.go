package repository

import (
	"context"
	"fmt"
	"time"

	"github.com/jmoiron/sqlx"
)

type TopicSuggestions struct {
	db *sqlx.DB
}

type CreateTopicSuggestionInput struct {
	ChallengeID      int64
	AuthorUserID     int64
	SourceChatID     int64
	SourceMessageID  int
	Text             string
	SuggestedAt      time.Time
	CreatedUpdatedAt time.Time
}

func NewTopicSuggestions(db *sqlx.DB) *TopicSuggestions {
	if db == nil {
		panic("sql db is nil")
	}
	return &TopicSuggestions{db: db}
}

func (r *TopicSuggestions) Create(ctx context.Context, input CreateTopicSuggestionInput) (TopicSuggestion, bool, error) {
	if input.SuggestedAt.IsZero() {
		input.SuggestedAt = time.Now().UTC()
	}
	if input.CreatedUpdatedAt.IsZero() {
		input.CreatedUpdatedAt = input.SuggestedAt
	}

	result, err := r.db.ExecContext(ctx, `
		INSERT OR IGNORE INTO topic_suggestions (
			challenge_id, author_user_id, source_chat_id, source_message_id, text,
			suggested_at, created_at, updated_at
		)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`, input.ChallengeID, input.AuthorUserID, input.SourceChatID, input.SourceMessageID,
		input.Text, timeString(input.SuggestedAt), timeString(input.CreatedUpdatedAt), timeString(input.CreatedUpdatedAt))
	if err != nil {
		return TopicSuggestion{}, false, fmt.Errorf("create topic suggestion: %w", err)
	}

	created, err := changed(result, "create topic suggestion")
	if err != nil {
		return TopicSuggestion{}, false, err
	}
	suggestion, err := r.getBySource(ctx, input.ChallengeID, input.SourceChatID, input.SourceMessageID)
	if err != nil {
		return TopicSuggestion{}, false, err
	}
	return suggestion, created, nil
}

func (r *TopicSuggestions) ListByChallenge(ctx context.Context, challengeID int64) ([]TopicSuggestion, error) {
	var rows []topicSuggestionRow
	if err := r.db.SelectContext(ctx, &rows, topicSuggestionSelectSQL+`
		WHERE challenge_id = ?
		ORDER BY suggested_at ASC, id ASC
	`, challengeID); err != nil {
		return nil, fmt.Errorf("list topic suggestions by challenge: %w", err)
	}

	suggestions := make([]TopicSuggestion, 0, len(rows))
	for _, row := range rows {
		suggestion, err := row.topicSuggestion()
		if err != nil {
			return nil, err
		}
		suggestions = append(suggestions, suggestion)
	}
	return suggestions, nil
}

func (r *TopicSuggestions) getBySource(ctx context.Context, challengeID int64, sourceChatID int64, sourceMessageID int) (TopicSuggestion, error) {
	var row topicSuggestionRow
	if err := r.db.GetContext(ctx, &row, topicSuggestionSelectSQL+`
		WHERE challenge_id = ? AND source_chat_id = ? AND source_message_id = ?
	`, challengeID, sourceChatID, sourceMessageID); err != nil {
		return TopicSuggestion{}, fmt.Errorf("get topic suggestion by source: %w", err)
	}
	return row.topicSuggestion()
}

const topicSuggestionSelectSQL = `
	SELECT id, challenge_id, author_user_id, source_chat_id, source_message_id,
		text, suggested_at, created_at, updated_at
	FROM topic_suggestions
`

type topicSuggestionRow struct {
	ID              int64  `db:"id"`
	ChallengeID     int64  `db:"challenge_id"`
	AuthorUserID    int64  `db:"author_user_id"`
	SourceChatID    int64  `db:"source_chat_id"`
	SourceMessageID int    `db:"source_message_id"`
	Text            string `db:"text"`
	SuggestedAt     string `db:"suggested_at"`
	CreatedAt       string `db:"created_at"`
	UpdatedAt       string `db:"updated_at"`
}

func (r topicSuggestionRow) topicSuggestion() (TopicSuggestion, error) {
	suggestedAt, err := parseTime(r.SuggestedAt)
	if err != nil {
		return TopicSuggestion{}, fmt.Errorf("parse topic suggestion suggested_at: %w", err)
	}
	createdAt, err := parseTime(r.CreatedAt)
	if err != nil {
		return TopicSuggestion{}, fmt.Errorf("parse topic suggestion created_at: %w", err)
	}
	updatedAt, err := parseTime(r.UpdatedAt)
	if err != nil {
		return TopicSuggestion{}, fmt.Errorf("parse topic suggestion updated_at: %w", err)
	}
	return TopicSuggestion{
		ID:              r.ID,
		ChallengeID:     r.ChallengeID,
		AuthorUserID:    r.AuthorUserID,
		SourceChatID:    r.SourceChatID,
		SourceMessageID: r.SourceMessageID,
		Text:            r.Text,
		SuggestedAt:     suggestedAt,
		CreatedAt:       createdAt,
		UpdatedAt:       updatedAt,
	}, nil
}
