package topic

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/TiraelSedai/PhotoChallengeBot/internal/publish"
	"github.com/TiraelSedai/PhotoChallengeBot/internal/repository"
	"github.com/TiraelSedai/PhotoChallengeBot/internal/require"
)

const (
	defaultReportSendTimeout = 10 * time.Second
	maxReportMessageLength   = 3500
	maxReportHeaderLength    = 256
	truncatedSuffix          = "..."
)

type reportChallenges interface {
	ListUnsentTopicReports(context.Context, int64, int) ([]repository.Challenge, error)
	ClaimTopicReport(context.Context, int64, time.Time) (bool, error)
	MarkTopicReportSent(context.Context, int64, time.Time, time.Time) (bool, error)
	ReleaseTopicReportClaim(context.Context, int64, time.Time) error
}

type reportSuggestions interface {
	ListByChallenge(context.Context, int64) ([]repository.TopicSuggestion, error)
}

type reportUsers interface {
	Get(context.Context, int64) (repository.User, error)
}

type reportPublisher interface {
	SendText(context.Context, int64, string) (int, error)
}

type ReportConfig struct {
	AdminChatID        int64
	Challenges         reportChallenges
	Suggestions        reportSuggestions
	Users              reportUsers
	Publisher          reportPublisher
	Now                func() time.Time
	SendTimeout        time.Duration
	PersistenceTimeout time.Duration
}

type Reporter struct {
	adminChatID int64
	challenges  reportChallenges
	suggestions reportSuggestions
	users       reportUsers
	publisher   reportPublisher
	now         func() time.Time
	sendFor     time.Duration
	persistFor  time.Duration
}

func NewReporter(cfg ReportConfig) *Reporter {
	require.NotNil("topic report challenges repository", cfg.Challenges)
	require.NotNil("topic report suggestions repository", cfg.Suggestions)
	require.NotNil("topic report users repository", cfg.Users)
	require.NotNil("topic report publisher", cfg.Publisher)
	require.NotNil("clock", cfg.Now)
	sendFor := cfg.SendTimeout
	if sendFor <= 0 {
		sendFor = defaultReportSendTimeout
	}
	persistFor := cfg.PersistenceTimeout
	if persistFor <= 0 {
		persistFor = defaultReportSendTimeout
	}
	if cfg.AdminChatID == 0 {
		panic("admin chat id is required")
	}
	return &Reporter{
		adminChatID: cfg.AdminChatID,
		challenges:  cfg.Challenges,
		suggestions: cfg.Suggestions,
		users:       cfg.Users,
		publisher:   cfg.Publisher,
		now:         cfg.Now,
		sendFor:     sendFor,
		persistFor:  persistFor,
	}
}

func (r *Reporter) PublishDue(ctx context.Context, mainChatID int64, limit int) error {
	due, err := r.challenges.ListUnsentTopicReports(ctx, mainChatID, limit)
	if err != nil {
		return err
	}
	for _, challenge := range due {
		if err := r.PublishOne(ctx, challenge); err != nil {
			return err
		}
	}
	return nil
}

func (r *Reporter) PublishOne(ctx context.Context, challenge repository.Challenge) error {
	if challenge.State != repository.ChallengeStateFinished || challenge.TopicReportSentAt != nil {
		return nil
	}

	_, err := publish.Attempt(ctx,
		publish.Config{PersistTimeout: r.persistFor},
		publish.Stage{Claim: r.challenges.ClaimTopicReport, Release: r.challenges.ReleaseTopicReportClaim},
		challenge.ID, r.now(),
		func(ctx context.Context, l *publish.Lease) error {
			messages, err := r.reportMessages(ctx, challenge)
			if err != nil {
				_ = l.Release(ctx)
				return err
			}

			for _, text := range messages {
				sendCtx, cancel := context.WithTimeout(ctx, r.sendFor)
				_, err = r.publisher.SendText(sendCtx, r.adminChatID, text)
				cancel()
				if err != nil {
					_ = l.Release(ctx)
					return fmt.Errorf("send topic report for challenge %d: %w", challenge.ID, err)
				}
			}

			return l.Commit(ctx, fmt.Sprintf("mark topic report sent for challenge %d", challenge.ID),
				func(pctx context.Context) (bool, error) {
					return r.challenges.MarkTopicReportSent(pctx, challenge.ID, l.ClaimedAt, r.now())
				})
		})
	return err
}

func (r *Reporter) reportMessages(ctx context.Context, challenge repository.Challenge) ([]string, error) {
	suggestions, err := r.suggestions.ListByChallenge(ctx, challenge.ID)
	if err != nil {
		return nil, err
	}
	header := truncateUTF8Bytes(topicReportHeader(challenge), maxReportHeaderLength)
	if len(suggestions) == 0 {
		return []string{header + ":\n\nТем за время голосования не предложили."}, nil
	}

	prefix := header + ":\n\n"
	continuationPrefix := header + " (продолжение):\n\n"
	messages := make([]string, 0, 1)
	current := prefix
	currentPrefix := prefix

	for idx, suggestion := range suggestions {
		user, err := r.users.Get(ctx, suggestion.AuthorUserID)
		if err != nil {
			return nil, err
		}
		line := truncateUTF8Bytes(topicReportLine(idx, user, suggestion), maxReportMessageLength-len(currentPrefix))
		separator := ""
		if current != currentPrefix {
			separator = "\n"
		}
		if len(current)+len(separator)+len(line) > maxReportMessageLength {
			messages = append(messages, current)
			current = continuationPrefix
			currentPrefix = continuationPrefix
			line = truncateUTF8Bytes(line, maxReportMessageLength-len(currentPrefix))
			separator = ""
		}
		current += separator + line
	}
	return append(messages, current), nil
}

func topicReportHeader(challenge repository.Challenge) string {
	var builder strings.Builder
	builder.WriteString("Темы, предложенные во время голосования за челлендж #")
	builder.WriteString(strconv.Itoa(challenge.Num))
	if challenge.Theme != "" {
		builder.WriteString(" «")
		builder.WriteString(challenge.Theme)
		builder.WriteString("»")
	}
	return builder.String()
}

func topicReportLine(idx int, user repository.User, suggestion repository.TopicSuggestion) string {
	return strconv.Itoa(idx+1) + ". " + authorName(user) + ": " + suggestion.Text
}

func truncateUTF8Bytes(value string, maxBytes int) string {
	if len(value) <= maxBytes {
		return value
	}
	if maxBytes <= len(truncatedSuffix) {
		return truncatedSuffix[:maxBytes]
	}

	limit := maxBytes - len(truncatedSuffix)
	end := 0
	for idx, r := range value {
		next := idx + len(string(r))
		if next > limit {
			break
		}
		end = next
	}
	return value[:end] + truncatedSuffix
}

func authorName(user repository.User) string {
	if user.Username != "" {
		return "@" + strings.TrimPrefix(user.Username, "@")
	}
	if user.DisplayName != "" {
		return user.DisplayName
	}
	return strconv.FormatInt(user.ID, 10)
}
