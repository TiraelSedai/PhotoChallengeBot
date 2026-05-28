package repository

import (
	"database/sql"
	"fmt"
	"strings"
	"time"
)

const (
	ChallengeStateActive   = "active"
	ChallengeStateVoting   = "voting"
	ChallengeStateFinished = "finished"

	dbTimeFormat = "2006-01-02T15:04:05.000000000Z"
)

type User struct {
	ID          int64
	Username    string
	FirstName   string
	LastName    string
	DisplayName string
	UpdatedAt   time.Time
}

type Challenge struct {
	ID                    int64
	MainChatID            int64
	Num                   int
	Theme                 string
	Hashtag               string
	State                 string
	AcceptStartAt         time.Time
	AcceptUntilAt         time.Time
	ReminderAt            time.Time
	ReminderSendingAt     *time.Time
	ReminderSentAt        *time.Time
	ReminderMessageID     *int64
	VoteStartedAt         *time.Time
	VoteUntilAt           *time.Time
	VoteSendingAt         *time.Time
	FinishedAt            *time.Time
	AnnouncementMessageID *int64
	VoteMessageID         *int64
	VotePinnedAt          *time.Time
	ResultsSendingAt      *time.Time
	ResultsMessageID      *int64
	ResultsPinnedAt       *time.Time
	AchievementsSendingAt *time.Time
	AchievementsMessageID *int64
	AchievementsSentAt    *time.Time
	TopicReportSendingAt  *time.Time
	TopicReportSentAt     *time.Time
	CreatedByUserID       int64
	CreatedAt             time.Time
	UpdatedAt             time.Time
}

type Photo struct {
	ID              int64
	ChallengeID     int64
	AuthorUserID    int64
	FileID          string
	FileUniqueID    string
	SourceChatID    int64
	SourceMessageID int
	Caption         string
	SubmittedAt     time.Time
	UpdatedAt       time.Time
}

type TopicSuggestion struct {
	ID              int64
	ChallengeID     int64
	AuthorUserID    int64
	SourceChatID    int64
	SourceMessageID int
	Text            string
	SuggestedAt     time.Time
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

func displayName(user User) string {
	if user.DisplayName != "" {
		return user.DisplayName
	}

	fullName := strings.TrimSpace(strings.Join([]string{user.FirstName, user.LastName}, " "))
	if fullName != "" {
		return fullName
	}
	if user.Username != "" {
		return "@" + strings.TrimPrefix(user.Username, "@")
	}
	return fmt.Sprintf("%d", user.ID)
}

func timeString(value time.Time) string {
	return value.UTC().Format(dbTimeFormat)
}

func parseTime(value string) (time.Time, error) {
	parsed, err := time.Parse(dbTimeFormat, value)
	if err == nil {
		return parsed, nil
	}
	parsed, err = time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return time.Time{}, err
	}
	return parsed, nil
}

func nullableString(value string) sql.NullString {
	if value == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: value, Valid: true}
}

func nullableInt64(value *int64) sql.NullInt64 {
	if value == nil {
		return sql.NullInt64{}
	}
	return sql.NullInt64{Int64: *value, Valid: true}
}

func nullableTimeString(value *time.Time) sql.NullString {
	if value == nil {
		return sql.NullString{}
	}
	return sql.NullString{String: timeString(*value), Valid: true}
}

func stringFromNull(value sql.NullString) string {
	if !value.Valid {
		return ""
	}
	return value.String
}

func int64PtrFromNull(value sql.NullInt64) *int64 {
	if !value.Valid {
		return nil
	}
	return &value.Int64
}

func timePtrFromNull(value sql.NullString) (*time.Time, error) {
	if !value.Valid {
		return nil, nil
	}
	parsed, err := parseTime(value.String)
	if err != nil {
		return nil, err
	}
	return &parsed, nil
}
