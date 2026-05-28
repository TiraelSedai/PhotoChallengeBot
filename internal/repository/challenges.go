package repository

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/jmoiron/sqlx"
)

type Challenges struct {
	db *sqlx.DB
}

const (
	defaultVotingDuration   = 48 * time.Hour
	reminderClaimTimeout    = 5 * time.Minute
	voteStartClaimTimeout   = 5 * time.Minute
	resultsClaimTimeout     = 5 * time.Minute
	achievementClaimTimeout = 5 * time.Minute
	topicReportClaimTimeout = 5 * time.Minute
	publishedVotingDuration = 48 * time.Hour
)

type CreateChallengeInput struct {
	MainChatID      int64
	Num             int
	Theme           string
	Hashtag         string
	State           string
	AcceptStartAt   time.Time
	AcceptUntilAt   time.Time
	ReminderAt      time.Time
	CreatedByUserID int64
	CreatedAt       time.Time
}

func NewChallenges(db *sqlx.DB) *Challenges {
	if db == nil {
		panic("sql db is nil")
	}
	return &Challenges{db: db}
}

func (r *Challenges) Create(ctx context.Context, input CreateChallengeInput) (Challenge, error) {
	if input.State == "" {
		input.State = ChallengeStateActive
	}
	if input.CreatedAt.IsZero() {
		input.CreatedAt = time.Now().UTC()
	}
	result, err := r.db.ExecContext(ctx, `
		INSERT INTO challenges (
			main_chat_id, num, theme, hashtag, state, accept_start_at, accept_until_at,
			reminder_at, created_by_user_id, created_at, updated_at
		)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, input.MainChatID, input.Num, input.Theme, input.Hashtag, input.State,
		timeString(input.AcceptStartAt), timeString(input.AcceptUntilAt),
		timeString(input.ReminderAt), input.CreatedByUserID,
		timeString(input.CreatedAt), timeString(input.CreatedAt))
	if err != nil {
		return Challenge{}, fmt.Errorf("create challenge: %w", err)
	}

	id, err := result.LastInsertId()
	if err != nil {
		return Challenge{}, fmt.Errorf("challenge last insert id: %w", err)
	}
	return r.Get(ctx, id)
}

func (r *Challenges) NextNum(ctx context.Context, mainChatID int64) (int, error) {
	var next int
	if err := r.db.GetContext(ctx, &next, `
		SELECT COALESCE(MAX(num), 0) + 1
		FROM challenges
		WHERE main_chat_id = ?
	`, mainChatID); err != nil {
		return 0, fmt.Errorf("next challenge num: %w", err)
	}
	return next, nil
}

func (r *Challenges) SetAnnouncementMessageID(ctx context.Context, id int64, messageID int, updatedAt time.Time) error {
	if updatedAt.IsZero() {
		updatedAt = time.Now().UTC()
	}

	result, err := r.db.ExecContext(ctx, `
		UPDATE challenges
		SET announcement_message_id = ?, updated_at = ?
		WHERE id = ?
	`, messageID, timeString(updatedAt), id)
	if err != nil {
		return fmt.Errorf("set announcement message id: %w", err)
	}

	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("set announcement message id rows affected: %w", err)
	}
	if affected == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (r *Challenges) ClaimVoteStart(ctx context.Context, id int64, claimedAt time.Time) (bool, error) {
	if claimedAt.IsZero() {
		claimedAt = time.Now().UTC()
	}

	staleClaimBefore := claimedAt.Add(-voteStartClaimTimeout)
	result, err := r.db.ExecContext(ctx, `
		UPDATE challenges
		SET vote_sending_at = ?, updated_at = ?
		WHERE id = ?
			AND state = ?
			AND vote_started_at IS NOT NULL
			AND vote_until_at IS NOT NULL
			AND vote_pinned_at IS NULL
			AND (
				vote_sending_at IS NULL
				OR julianday(vote_sending_at) <= julianday(?)
			)
	`, timeString(claimedAt), timeString(claimedAt), id, ChallengeStateVoting,
		timeString(staleClaimBefore))
	if err != nil {
		return false, fmt.Errorf("claim vote start: %w", err)
	}
	return changed(result, "claim vote start")
}

func (r *Challenges) SetVoteMessageID(ctx context.Context, id int64, messageID int, claimedAt, updatedAt time.Time) (bool, error) {
	if updatedAt.IsZero() {
		updatedAt = time.Now().UTC()
	}
	if claimedAt.IsZero() {
		return false, fmt.Errorf("set vote message id: claimedAt is required")
	}

	result, err := r.db.ExecContext(ctx, `
		UPDATE challenges
		SET vote_message_id = ?, updated_at = ?
		WHERE id = ?
			AND state = ?
			AND vote_message_id IS NULL
			AND vote_sending_at = ?
			AND vote_pinned_at IS NULL
	`, messageID, timeString(updatedAt), id, ChallengeStateVoting, timeString(claimedAt))
	if err != nil {
		return false, fmt.Errorf("set vote message id: %w", err)
	}
	return changed(result, "set vote message id")
}

func (r *Challenges) RecordVoteMessageID(ctx context.Context, id int64, messageID int, updatedAt time.Time) (bool, error) {
	if updatedAt.IsZero() {
		updatedAt = time.Now().UTC()
	}

	result, err := r.db.ExecContext(ctx, `
		UPDATE challenges
		SET vote_message_id = COALESCE(vote_message_id, ?), updated_at = ?
		WHERE id = ?
			AND state = ?
			AND vote_pinned_at IS NULL
	`, messageID, timeString(updatedAt), id, ChallengeStateVoting)
	if err != nil {
		return false, fmt.Errorf("record vote message id: %w", err)
	}
	return changed(result, "record vote message id")
}

func (r *Challenges) MarkVoteStartPinned(ctx context.Context, id int64, claimedAt, pinnedAt time.Time) (bool, error) {
	if pinnedAt.IsZero() {
		pinnedAt = time.Now().UTC()
	}
	if claimedAt.IsZero() {
		return false, fmt.Errorf("mark vote start pinned: claimedAt is required")
	}

	result, err := r.db.ExecContext(ctx, `
		UPDATE challenges
		SET vote_pinned_at = ?,
			vote_sending_at = NULL,
			updated_at = ?
		WHERE id = ?
			AND state = ?
			AND vote_message_id IS NOT NULL
			AND vote_pinned_at IS NULL
			AND vote_sending_at = ?
	`, timeString(pinnedAt), timeString(pinnedAt), id, ChallengeStateVoting, timeString(claimedAt))
	if err != nil {
		return false, fmt.Errorf("mark vote start pinned: %w", err)
	}
	return changed(result, "mark vote start pinned")
}

func (r *Challenges) ReleaseVoteStartClaim(ctx context.Context, id int64, claimedAt time.Time) error {
	if claimedAt.IsZero() {
		return nil
	}

	_, err := r.db.ExecContext(ctx, `
		UPDATE challenges
		SET vote_sending_at = NULL, updated_at = ?
		WHERE id = ?
			AND vote_sending_at = ?
			AND vote_pinned_at IS NULL
	`, timeString(time.Now().UTC()), id, timeString(claimedAt))
	if err != nil {
		return fmt.Errorf("release vote start claim: %w", err)
	}
	return nil
}

func (r *Challenges) ListDueReminders(ctx context.Context, mainChatID int64, now time.Time, limit int) ([]Challenge, error) {
	if limit <= 0 {
		limit = 100
	}

	staleClaimBefore := now.Add(-reminderClaimTimeout)
	var rows []challengeRow
	if err := r.db.SelectContext(ctx, &rows, challengeSelectSQL+`
		WHERE state = ?
			AND reminder_sent_at IS NULL
			AND (? = 0 OR main_chat_id = ?)
		ORDER BY id ASC
	`, ChallengeStateActive, mainChatID, mainChatID); err != nil {
		return nil, fmt.Errorf("list due reminders: %w", err)
	}
	challenges, err := challengeRows(rows)
	if err != nil {
		return nil, err
	}

	due := make([]Challenge, 0, len(challenges))
	for _, challenge := range challenges {
		if !challenge.AcceptUntilAt.After(now) {
			continue
		}
		if challenge.ReminderAt.After(now) {
			continue
		}
		if challenge.ReminderSendingAt != nil && challenge.ReminderSendingAt.After(staleClaimBefore) {
			continue
		}
		due = append(due, challenge)
		if len(due) >= limit {
			break
		}
	}
	return due, nil
}

func (r *Challenges) ClaimReminder(ctx context.Context, id int64, claimedAt time.Time) (bool, error) {
	if claimedAt.IsZero() {
		claimedAt = time.Now().UTC()
	}

	staleClaimBefore := claimedAt.Add(-reminderClaimTimeout)
	result, err := r.db.ExecContext(ctx, `
		UPDATE challenges
		SET reminder_sending_at = ?, updated_at = ?
		WHERE id = ?
			AND state = ?
			AND reminder_sent_at IS NULL
			AND julianday(accept_until_at) > julianday(?)
			AND julianday(reminder_at) <= julianday(?)
			AND (
				reminder_sending_at IS NULL
				OR julianday(reminder_sending_at) <= julianday(?)
			)
	`, timeString(claimedAt), timeString(claimedAt), id, ChallengeStateActive,
		timeString(claimedAt), timeString(claimedAt), timeString(staleClaimBefore))
	if err != nil {
		return false, fmt.Errorf("claim reminder: %w", err)
	}
	return changed(result, "claim reminder")
}

func (r *Challenges) MarkReminderSent(ctx context.Context, id int64, messageID int, claimedAt, sentAt time.Time) (bool, error) {
	if sentAt.IsZero() {
		sentAt = time.Now().UTC()
	}
	if claimedAt.IsZero() {
		return false, fmt.Errorf("mark reminder sent: claimedAt is required")
	}

	result, err := r.db.ExecContext(ctx, `
		UPDATE challenges
		SET reminder_sent_at = ?,
			reminder_message_id = ?,
			reminder_sending_at = NULL,
			updated_at = ?
		WHERE id = ?
			AND state = ?
			AND reminder_sent_at IS NULL
			AND reminder_sending_at = ?
	`, timeString(sentAt), messageID, timeString(sentAt), id, ChallengeStateActive, timeString(claimedAt))
	if err != nil {
		return false, fmt.Errorf("mark reminder sent: %w", err)
	}
	return changed(result, "mark reminder sent")
}

func (r *Challenges) ReleaseReminderClaim(ctx context.Context, id int64, claimedAt time.Time) error {
	if claimedAt.IsZero() {
		return nil
	}

	_, err := r.db.ExecContext(ctx, `
		UPDATE challenges
		SET reminder_sending_at = NULL, updated_at = ?
		WHERE id = ?
			AND reminder_sending_at = ?
			AND reminder_sent_at IS NULL
	`, timeString(time.Now().UTC()), id, timeString(claimedAt))
	if err != nil {
		return fmt.Errorf("release reminder claim: %w", err)
	}
	return nil
}

func (r *Challenges) ListDueAcceptanceClosures(ctx context.Context, mainChatID int64, now time.Time, limit int) ([]Challenge, error) {
	if limit <= 0 {
		limit = 100
	}

	var rows []challengeRow
	if err := r.db.SelectContext(ctx, &rows, challengeSelectSQL+`
		WHERE state = ?
			AND (? = 0 OR main_chat_id = ?)
		ORDER BY id ASC
	`, ChallengeStateActive, mainChatID, mainChatID); err != nil {
		return nil, fmt.Errorf("list due acceptance closures: %w", err)
	}
	challenges, err := challengeRows(rows)
	if err != nil {
		return nil, err
	}

	due := make([]Challenge, 0, len(challenges))
	for _, challenge := range challenges {
		if challenge.AcceptUntilAt.After(now) {
			continue
		}
		due = append(due, challenge)
		if len(due) >= limit {
			break
		}
	}
	return due, nil
}

func (r *Challenges) StartVoting(ctx context.Context, id int64, startedAt time.Time) (bool, error) {
	if startedAt.IsZero() {
		startedAt = time.Now().UTC()
	}
	voteUntilAt := startedAt.Add(defaultVotingDuration)

	result, err := r.db.ExecContext(ctx, `
		UPDATE challenges
		SET state = ?,
			vote_started_at = COALESCE(vote_started_at, ?),
			vote_until_at = COALESCE(vote_until_at, ?),
			updated_at = ?
		WHERE id = ?
			AND state = ?
			AND julianday(accept_until_at) <= julianday(?)
	`, ChallengeStateVoting, timeString(startedAt), timeString(voteUntilAt), timeString(startedAt),
		id, ChallengeStateActive, timeString(startedAt))
	if err != nil {
		return false, fmt.Errorf("start voting: %w", err)
	}
	return changed(result, "start voting")
}

func (r *Challenges) ExtendVoteUntil(ctx context.Context, id int64, updatedAt time.Time) (*time.Time, error) {
	if updatedAt.IsZero() {
		updatedAt = time.Now().UTC()
	}
	voteUntilAt := updatedAt.Add(publishedVotingDuration)
	result, err := r.db.ExecContext(ctx, `
		UPDATE challenges
		SET vote_until_at = ?, updated_at = ?
		WHERE id = ?
			AND state = ?
			AND vote_pinned_at IS NULL
	`, timeString(voteUntilAt), timeString(updatedAt), id, ChallengeStateVoting)
	if err != nil {
		return nil, fmt.Errorf("extend vote until: %w", err)
	}
	extended, err := changed(result, "extend vote until")
	if err != nil {
		return nil, err
	}
	if !extended {
		return nil, nil
	}
	return &voteUntilAt, nil
}

func (r *Challenges) ListDueVotingClosures(ctx context.Context, mainChatID int64, now time.Time, limit int) ([]Challenge, error) {
	if limit <= 0 {
		limit = 100
	}

	var rows []challengeRow
	if err := r.db.SelectContext(ctx, &rows, challengeSelectSQL+`
		WHERE state = ?
			AND vote_until_at IS NOT NULL
			AND vote_pinned_at IS NOT NULL
			AND (? = 0 OR main_chat_id = ?)
		ORDER BY id ASC
	`, ChallengeStateVoting, mainChatID, mainChatID); err != nil {
		return nil, fmt.Errorf("list due voting closures: %w", err)
	}
	challenges, err := challengeRows(rows)
	if err != nil {
		return nil, err
	}

	due := make([]Challenge, 0, len(challenges))
	for _, challenge := range challenges {
		if challenge.VoteUntilAt == nil || challenge.VoteUntilAt.After(now) {
			continue
		}
		due = append(due, challenge)
		if len(due) >= limit {
			break
		}
	}
	return due, nil
}

func (r *Challenges) ListUnpublishedVoteStarts(ctx context.Context, mainChatID int64, limit int) ([]Challenge, error) {
	if limit <= 0 {
		limit = 100
	}

	var rows []challengeRow
	if err := r.db.SelectContext(ctx, &rows, challengeSelectSQL+`
		WHERE state = ?
			AND vote_started_at IS NOT NULL
			AND vote_until_at IS NOT NULL
			AND vote_pinned_at IS NULL
			AND (? = 0 OR main_chat_id = ?)
		ORDER BY id ASC
		LIMIT ?
	`, ChallengeStateVoting, mainChatID, mainChatID, limit); err != nil {
		return nil, fmt.Errorf("list unpublished vote starts: %w", err)
	}
	return challengeRows(rows)
}

func (r *Challenges) FinishVoting(ctx context.Context, id int64, finishedAt time.Time) (bool, error) {
	if finishedAt.IsZero() {
		finishedAt = time.Now().UTC()
	}

	result, err := r.db.ExecContext(ctx, `
		UPDATE challenges
		SET state = ?,
			finished_at = COALESCE(finished_at, ?),
			updated_at = ?
		WHERE id = ?
			AND state = ?
			AND vote_until_at IS NOT NULL
			AND julianday(vote_until_at) <= julianday(?)
	`, ChallengeStateFinished, timeString(finishedAt), timeString(finishedAt), id, ChallengeStateVoting, timeString(finishedAt))
	if err != nil {
		return false, fmt.Errorf("finish voting: %w", err)
	}
	return changed(result, "finish voting")
}

func (r *Challenges) FinishVotingNow(ctx context.Context, id int64, finishedAt time.Time) (bool, error) {
	if finishedAt.IsZero() {
		finishedAt = time.Now().UTC()
	}

	result, err := r.db.ExecContext(ctx, `
		UPDATE challenges
		SET state = ?,
			finished_at = COALESCE(finished_at, ?),
			updated_at = ?
		WHERE id = ?
			AND state = ?
			AND vote_until_at IS NOT NULL
	`, ChallengeStateFinished, timeString(finishedAt), timeString(finishedAt), id, ChallengeStateVoting)
	if err != nil {
		return false, fmt.Errorf("finish voting now: %w", err)
	}
	return changed(result, "finish voting now")
}

func (r *Challenges) ListUnpublishedResults(ctx context.Context, mainChatID int64, limit int) ([]Challenge, error) {
	var rows []challengeRow
	if err := r.db.SelectContext(ctx, &rows, challengeSelectSQL+`
		WHERE state = ?
			AND results_pinned_at IS NULL
			AND (? = 0 OR main_chat_id = ?)
		ORDER BY finished_at ASC, id ASC
		LIMIT ?
	`, ChallengeStateFinished, mainChatID, mainChatID, limit); err != nil {
		return nil, fmt.Errorf("list unpublished results: %w", err)
	}
	return challengeRows(rows)
}

func (r *Challenges) ClaimResults(ctx context.Context, id int64, claimedAt time.Time) (bool, error) {
	if claimedAt.IsZero() {
		claimedAt = time.Now().UTC()
	}
	staleClaimBefore := claimedAt.Add(-resultsClaimTimeout)
	result, err := r.db.ExecContext(ctx, `
		UPDATE challenges
		SET results_sending_at = ?, updated_at = ?
		WHERE id = ?
			AND state = ?
			AND results_pinned_at IS NULL
			AND (
				results_sending_at IS NULL
				OR julianday(results_sending_at) <= julianday(?)
			)
	`, timeString(claimedAt), timeString(claimedAt), id, ChallengeStateFinished, timeString(staleClaimBefore))
	if err != nil {
		return false, fmt.Errorf("claim results: %w", err)
	}
	return changed(result, "claim results")
}

func (r *Challenges) SetResultsMessageID(ctx context.Context, id int64, messageID int, claimedAt, updatedAt time.Time) (bool, error) {
	if updatedAt.IsZero() {
		updatedAt = time.Now().UTC()
	}
	if claimedAt.IsZero() {
		return false, fmt.Errorf("set results message id: claimedAt is required")
	}
	result, err := r.db.ExecContext(ctx, `
		UPDATE challenges
		SET results_message_id = ?, updated_at = ?
		WHERE id = ?
			AND state = ?
			AND results_message_id IS NULL
			AND results_sending_at = ?
			AND results_pinned_at IS NULL
	`, messageID, timeString(updatedAt), id, ChallengeStateFinished, timeString(claimedAt))
	if err != nil {
		return false, fmt.Errorf("set results message id: %w", err)
	}
	return changed(result, "set results message id")
}

func (r *Challenges) RecordResultsMessageID(ctx context.Context, id int64, messageID int, updatedAt time.Time) (bool, error) {
	if updatedAt.IsZero() {
		updatedAt = time.Now().UTC()
	}
	result, err := r.db.ExecContext(ctx, `
		UPDATE challenges
		SET results_message_id = COALESCE(results_message_id, ?), updated_at = ?
		WHERE id = ?
			AND state = ?
			AND results_pinned_at IS NULL
	`, messageID, timeString(updatedAt), id, ChallengeStateFinished)
	if err != nil {
		return false, fmt.Errorf("record results message id: %w", err)
	}
	return changed(result, "record results message id")
}

func (r *Challenges) MarkResultsPinned(ctx context.Context, id int64, claimedAt, pinnedAt time.Time) (bool, error) {
	if pinnedAt.IsZero() {
		pinnedAt = time.Now().UTC()
	}
	if claimedAt.IsZero() {
		return false, fmt.Errorf("mark results pinned: claimedAt is required")
	}
	result, err := r.db.ExecContext(ctx, `
		UPDATE challenges
		SET results_pinned_at = ?,
			results_sending_at = NULL,
			updated_at = ?
		WHERE id = ?
			AND state = ?
			AND results_message_id IS NOT NULL
			AND results_pinned_at IS NULL
			AND results_sending_at = ?
	`, timeString(pinnedAt), timeString(pinnedAt), id, ChallengeStateFinished, timeString(claimedAt))
	if err != nil {
		return false, fmt.Errorf("mark results pinned: %w", err)
	}
	return changed(result, "mark results pinned")
}

func (r *Challenges) ReleaseResultsClaim(ctx context.Context, id int64, claimedAt time.Time) error {
	if claimedAt.IsZero() {
		return fmt.Errorf("release results claim: claimedAt is required")
	}
	_, err := r.db.ExecContext(ctx, `
		UPDATE challenges
		SET results_sending_at = NULL, updated_at = ?
		WHERE id = ?
			AND results_sending_at = ?
			AND results_pinned_at IS NULL
	`, timeString(time.Now().UTC()), id, timeString(claimedAt))
	if err != nil {
		return fmt.Errorf("release results claim: %w", err)
	}
	return nil
}

func (r *Challenges) ListUnsentAchievements(ctx context.Context, mainChatID int64, limit int) ([]Challenge, error) {
	if limit <= 0 {
		limit = 100
	}
	var rows []challengeRow
	if err := r.db.SelectContext(ctx, &rows, challengeSelectSQL+`
		WHERE state = ?
			AND results_pinned_at IS NOT NULL
			AND achievements_sent_at IS NULL
			AND (? = 0 OR main_chat_id = ?)
		ORDER BY finished_at ASC, id ASC
		LIMIT ?
	`, ChallengeStateFinished, mainChatID, mainChatID, limit); err != nil {
		return nil, fmt.Errorf("list unsent achievements: %w", err)
	}
	return challengeRows(rows)
}

func (r *Challenges) ClaimAchievements(ctx context.Context, id int64, claimedAt time.Time) (bool, error) {
	if claimedAt.IsZero() {
		claimedAt = time.Now().UTC()
	}
	staleClaimBefore := claimedAt.Add(-achievementClaimTimeout)
	result, err := r.db.ExecContext(ctx, `
		UPDATE challenges
		SET achievements_sending_at = ?, updated_at = ?
		WHERE id = ?
			AND state = ?
			AND results_pinned_at IS NOT NULL
			AND achievements_sent_at IS NULL
			AND (
				achievements_sending_at IS NULL
				OR julianday(achievements_sending_at) <= julianday(?)
			)
	`, timeString(claimedAt), timeString(claimedAt), id, ChallengeStateFinished, timeString(staleClaimBefore))
	if err != nil {
		return false, fmt.Errorf("claim achievements: %w", err)
	}
	return changed(result, "claim achievements")
}

func (r *Challenges) MarkAchievementsSent(ctx context.Context, id int64, claimedAt, sentAt time.Time) (bool, error) {
	if sentAt.IsZero() {
		sentAt = time.Now().UTC()
	}
	if claimedAt.IsZero() {
		return false, fmt.Errorf("mark achievements sent: claimedAt is required")
	}
	result, err := r.db.ExecContext(ctx, `
		UPDATE challenges
		SET achievements_sent_at = ?,
			achievements_sending_at = NULL,
			updated_at = ?
		WHERE id = ?
			AND state = ?
			AND achievements_sent_at IS NULL
			AND achievements_sending_at = ?
	`, timeString(sentAt), timeString(sentAt), id, ChallengeStateFinished, timeString(claimedAt))
	if err != nil {
		return false, fmt.Errorf("mark achievements sent: %w", err)
	}
	return changed(result, "mark achievements sent")
}

func (r *Challenges) SetAchievementsMessageID(ctx context.Context, id int64, messageID int, claimedAt, updatedAt time.Time) (bool, error) {
	if updatedAt.IsZero() {
		updatedAt = time.Now().UTC()
	}
	if claimedAt.IsZero() {
		return false, fmt.Errorf("set achievements message id: claimedAt is required")
	}
	result, err := r.db.ExecContext(ctx, `
		UPDATE challenges
		SET achievements_message_id = COALESCE(achievements_message_id, ?),
			updated_at = ?
		WHERE id = ?
			AND state = ?
			AND achievements_sent_at IS NULL
			AND achievements_sending_at = ?
	`, messageID, timeString(updatedAt), id, ChallengeStateFinished, timeString(claimedAt))
	if err != nil {
		return false, fmt.Errorf("set achievements message id: %w", err)
	}
	return changed(result, "set achievements message id")
}

func (r *Challenges) RecordAchievementsMessageID(ctx context.Context, id int64, messageID int, updatedAt time.Time) (bool, error) {
	if updatedAt.IsZero() {
		updatedAt = time.Now().UTC()
	}
	result, err := r.db.ExecContext(ctx, `
		UPDATE challenges
		SET achievements_message_id = COALESCE(achievements_message_id, ?),
			updated_at = ?
		WHERE id = ?
			AND state = ?
			AND achievements_sent_at IS NULL
	`, messageID, timeString(updatedAt), id, ChallengeStateFinished)
	if err != nil {
		return false, fmt.Errorf("record achievements message id: %w", err)
	}
	return changed(result, "record achievements message id")
}

func (r *Challenges) ReleaseAchievementsClaim(ctx context.Context, id int64, claimedAt time.Time) error {
	if claimedAt.IsZero() {
		return fmt.Errorf("release achievements claim: claimedAt is required")
	}
	_, err := r.db.ExecContext(ctx, `
		UPDATE challenges
		SET achievements_sending_at = NULL, updated_at = ?
		WHERE id = ?
			AND achievements_sending_at = ?
			AND achievements_sent_at IS NULL
	`, timeString(time.Now().UTC()), id, timeString(claimedAt))
	if err != nil {
		return fmt.Errorf("release achievements claim: %w", err)
	}
	return nil
}

func (r *Challenges) ListUnsentTopicReports(ctx context.Context, mainChatID int64, limit int) ([]Challenge, error) {
	if limit <= 0 {
		limit = 100
	}
	var rows []challengeRow
	if err := r.db.SelectContext(ctx, &rows, challengeSelectSQL+`
		WHERE state = ?
			AND topic_report_sent_at IS NULL
			AND (? = 0 OR main_chat_id = ?)
		ORDER BY finished_at ASC, id ASC
		LIMIT ?
	`, ChallengeStateFinished, mainChatID, mainChatID, limit); err != nil {
		return nil, fmt.Errorf("list unsent topic reports: %w", err)
	}
	return challengeRows(rows)
}

func (r *Challenges) ClaimTopicReport(ctx context.Context, id int64, claimedAt time.Time) (bool, error) {
	if claimedAt.IsZero() {
		claimedAt = time.Now().UTC()
	}
	staleClaimBefore := claimedAt.Add(-topicReportClaimTimeout)
	result, err := r.db.ExecContext(ctx, `
		UPDATE challenges
		SET topic_report_sending_at = ?, updated_at = ?
		WHERE id = ?
			AND state = ?
			AND topic_report_sent_at IS NULL
			AND (
				topic_report_sending_at IS NULL
				OR julianday(topic_report_sending_at) <= julianday(?)
			)
	`, timeString(claimedAt), timeString(claimedAt), id, ChallengeStateFinished, timeString(staleClaimBefore))
	if err != nil {
		return false, fmt.Errorf("claim topic report: %w", err)
	}
	return changed(result, "claim topic report")
}

func (r *Challenges) MarkTopicReportSent(ctx context.Context, id int64, claimedAt, sentAt time.Time) (bool, error) {
	if sentAt.IsZero() {
		sentAt = time.Now().UTC()
	}
	if claimedAt.IsZero() {
		return false, fmt.Errorf("mark topic report sent: claimedAt is required")
	}
	result, err := r.db.ExecContext(ctx, `
		UPDATE challenges
		SET topic_report_sent_at = ?,
			topic_report_sending_at = NULL,
			updated_at = ?
		WHERE id = ?
			AND state = ?
			AND topic_report_sent_at IS NULL
			AND topic_report_sending_at = ?
	`, timeString(sentAt), timeString(sentAt), id, ChallengeStateFinished, timeString(claimedAt))
	if err != nil {
		return false, fmt.Errorf("mark topic report sent: %w", err)
	}
	return changed(result, "mark topic report sent")
}

func (r *Challenges) ReleaseTopicReportClaim(ctx context.Context, id int64, claimedAt time.Time) error {
	if claimedAt.IsZero() {
		return fmt.Errorf("release topic report claim: claimedAt is required")
	}
	_, err := r.db.ExecContext(ctx, `
		UPDATE challenges
		SET topic_report_sending_at = NULL, updated_at = ?
		WHERE id = ?
			AND topic_report_sending_at = ?
			AND topic_report_sent_at IS NULL
	`, timeString(time.Now().UTC()), id, timeString(claimedAt))
	if err != nil {
		return fmt.Errorf("release topic report claim: %w", err)
	}
	return nil
}

func (r *Challenges) Get(ctx context.Context, id int64) (Challenge, error) {
	var row challengeRow
	if err := r.db.GetContext(ctx, &row, challengeSelectSQL+" WHERE id = ?", id); err != nil {
		return Challenge{}, fmt.Errorf("get challenge %d: %w", id, err)
	}
	return row.challenge()
}

func (r *Challenges) FindOpenByMainChatID(ctx context.Context, mainChatID int64) (*Challenge, error) {
	var row challengeRow
	if err := r.db.GetContext(ctx, &row, challengeSelectSQL+`
		WHERE main_chat_id = ? AND state IN (?, ?)
		ORDER BY id DESC
		LIMIT 1
	`, mainChatID, ChallengeStateActive, ChallengeStateVoting); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("find open challenge: %w", err)
	}

	challenge, err := row.challenge()
	if err != nil {
		return nil, err
	}
	return &challenge, nil
}

const challengeSelectSQL = `
	SELECT id, main_chat_id, num, theme, hashtag, state, accept_start_at,
		accept_until_at, reminder_at, reminder_sending_at, reminder_sent_at,
		reminder_message_id, vote_started_at, vote_until_at, vote_sending_at,
		finished_at, announcement_message_id, vote_message_id, vote_pinned_at,
		results_sending_at, results_message_id, results_pinned_at, achievements_sending_at,
		achievements_message_id, achievements_sent_at, topic_report_sending_at,
		topic_report_sent_at,
		created_by_user_id, created_at, updated_at
	FROM challenges
`

type challengeRow struct {
	ID                    int64          `db:"id"`
	MainChatID            int64          `db:"main_chat_id"`
	Num                   int            `db:"num"`
	Theme                 string         `db:"theme"`
	Hashtag               string         `db:"hashtag"`
	State                 string         `db:"state"`
	AcceptStartAt         string         `db:"accept_start_at"`
	AcceptUntilAt         string         `db:"accept_until_at"`
	ReminderAt            string         `db:"reminder_at"`
	ReminderSendingAt     sql.NullString `db:"reminder_sending_at"`
	ReminderSentAt        sql.NullString `db:"reminder_sent_at"`
	ReminderMessageID     sql.NullInt64  `db:"reminder_message_id"`
	VoteStartedAt         sql.NullString `db:"vote_started_at"`
	VoteUntilAt           sql.NullString `db:"vote_until_at"`
	VoteSendingAt         sql.NullString `db:"vote_sending_at"`
	FinishedAt            sql.NullString `db:"finished_at"`
	AnnouncementMessageID sql.NullInt64  `db:"announcement_message_id"`
	VoteMessageID         sql.NullInt64  `db:"vote_message_id"`
	VotePinnedAt          sql.NullString `db:"vote_pinned_at"`
	ResultsSendingAt      sql.NullString `db:"results_sending_at"`
	ResultsMessageID      sql.NullInt64  `db:"results_message_id"`
	ResultsPinnedAt       sql.NullString `db:"results_pinned_at"`
	AchievementsSendingAt sql.NullString `db:"achievements_sending_at"`
	AchievementsMessageID sql.NullInt64  `db:"achievements_message_id"`
	AchievementsSentAt    sql.NullString `db:"achievements_sent_at"`
	TopicReportSendingAt  sql.NullString `db:"topic_report_sending_at"`
	TopicReportSentAt     sql.NullString `db:"topic_report_sent_at"`
	CreatedByUserID       int64          `db:"created_by_user_id"`
	CreatedAt             string         `db:"created_at"`
	UpdatedAt             string         `db:"updated_at"`
}

func challengeRows(rows []challengeRow) ([]Challenge, error) {
	challenges := make([]Challenge, 0, len(rows))
	for _, row := range rows {
		challenge, err := row.challenge()
		if err != nil {
			return nil, err
		}
		challenges = append(challenges, challenge)
	}
	return challenges, nil
}

func changed(result sql.Result, action string) (bool, error) {
	affected, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("%s rows affected: %w", action, err)
	}
	return affected > 0, nil
}

func (r challengeRow) challenge() (Challenge, error) {
	acceptStartAt, err := parseTime(r.AcceptStartAt)
	if err != nil {
		return Challenge{}, fmt.Errorf("parse challenge accept_start_at: %w", err)
	}
	acceptUntilAt, err := parseTime(r.AcceptUntilAt)
	if err != nil {
		return Challenge{}, fmt.Errorf("parse challenge accept_until_at: %w", err)
	}
	reminderAt, err := parseTime(r.ReminderAt)
	if err != nil {
		return Challenge{}, fmt.Errorf("parse challenge reminder_at: %w", err)
	}
	reminderSendingAt, err := timePtrFromNull(r.ReminderSendingAt)
	if err != nil {
		return Challenge{}, fmt.Errorf("parse challenge reminder_sending_at: %w", err)
	}
	reminderSentAt, err := timePtrFromNull(r.ReminderSentAt)
	if err != nil {
		return Challenge{}, fmt.Errorf("parse challenge reminder_sent_at: %w", err)
	}
	voteStartedAt, err := timePtrFromNull(r.VoteStartedAt)
	if err != nil {
		return Challenge{}, fmt.Errorf("parse challenge vote_started_at: %w", err)
	}
	voteUntilAt, err := timePtrFromNull(r.VoteUntilAt)
	if err != nil {
		return Challenge{}, fmt.Errorf("parse challenge vote_until_at: %w", err)
	}
	voteSendingAt, err := timePtrFromNull(r.VoteSendingAt)
	if err != nil {
		return Challenge{}, fmt.Errorf("parse challenge vote_sending_at: %w", err)
	}
	finishedAt, err := timePtrFromNull(r.FinishedAt)
	if err != nil {
		return Challenge{}, fmt.Errorf("parse challenge finished_at: %w", err)
	}
	votePinnedAt, err := timePtrFromNull(r.VotePinnedAt)
	if err != nil {
		return Challenge{}, fmt.Errorf("parse challenge vote_pinned_at: %w", err)
	}
	resultsSendingAt, err := timePtrFromNull(r.ResultsSendingAt)
	if err != nil {
		return Challenge{}, fmt.Errorf("parse challenge results_sending_at: %w", err)
	}
	resultsPinnedAt, err := timePtrFromNull(r.ResultsPinnedAt)
	if err != nil {
		return Challenge{}, fmt.Errorf("parse challenge results_pinned_at: %w", err)
	}
	achievementsSendingAt, err := timePtrFromNull(r.AchievementsSendingAt)
	if err != nil {
		return Challenge{}, fmt.Errorf("parse challenge achievements_sending_at: %w", err)
	}
	achievementsSentAt, err := timePtrFromNull(r.AchievementsSentAt)
	if err != nil {
		return Challenge{}, fmt.Errorf("parse challenge achievements_sent_at: %w", err)
	}
	topicReportSendingAt, err := timePtrFromNull(r.TopicReportSendingAt)
	if err != nil {
		return Challenge{}, fmt.Errorf("parse challenge topic_report_sending_at: %w", err)
	}
	topicReportSentAt, err := timePtrFromNull(r.TopicReportSentAt)
	if err != nil {
		return Challenge{}, fmt.Errorf("parse challenge topic_report_sent_at: %w", err)
	}
	createdAt, err := parseTime(r.CreatedAt)
	if err != nil {
		return Challenge{}, fmt.Errorf("parse challenge created_at: %w", err)
	}
	updatedAt, err := parseTime(r.UpdatedAt)
	if err != nil {
		return Challenge{}, fmt.Errorf("parse challenge updated_at: %w", err)
	}

	return Challenge{
		ID:                    r.ID,
		MainChatID:            r.MainChatID,
		Num:                   r.Num,
		Theme:                 r.Theme,
		Hashtag:               r.Hashtag,
		State:                 r.State,
		AcceptStartAt:         acceptStartAt,
		AcceptUntilAt:         acceptUntilAt,
		ReminderAt:            reminderAt,
		ReminderSendingAt:     reminderSendingAt,
		ReminderSentAt:        reminderSentAt,
		ReminderMessageID:     int64PtrFromNull(r.ReminderMessageID),
		VoteStartedAt:         voteStartedAt,
		VoteUntilAt:           voteUntilAt,
		VoteSendingAt:         voteSendingAt,
		FinishedAt:            finishedAt,
		AnnouncementMessageID: int64PtrFromNull(r.AnnouncementMessageID),
		VoteMessageID:         int64PtrFromNull(r.VoteMessageID),
		VotePinnedAt:          votePinnedAt,
		ResultsSendingAt:      resultsSendingAt,
		ResultsMessageID:      int64PtrFromNull(r.ResultsMessageID),
		ResultsPinnedAt:       resultsPinnedAt,
		AchievementsSendingAt: achievementsSendingAt,
		AchievementsMessageID: int64PtrFromNull(r.AchievementsMessageID),
		AchievementsSentAt:    achievementsSentAt,
		TopicReportSendingAt:  topicReportSendingAt,
		TopicReportSentAt:     topicReportSentAt,
		CreatedByUserID:       r.CreatedByUserID,
		CreatedAt:             createdAt,
		UpdatedAt:             updatedAt,
	}, nil
}
