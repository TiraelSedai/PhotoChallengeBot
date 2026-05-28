package results

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/TiraelSedai/PhotoChallengeBot/internal/repository"
	"github.com/TiraelSedai/PhotoChallengeBot/internal/require"
	"github.com/TiraelSedai/PhotoChallengeBot/internal/templates"
)

var achievementMilestones = map[int]struct{}{
	1: {},
	3: {},
	5: {},
	7: {},
}

const defaultSendTimeout = 10 * time.Second

type challengeStore interface {
	Get(context.Context, int64) (repository.Challenge, error)
	ListUnpublishedResults(context.Context, int64, int) ([]repository.Challenge, error)
	ClaimResults(context.Context, int64, time.Time) (bool, error)
	SetResultsMessageID(context.Context, int64, int, time.Time, time.Time) (bool, error)
	RecordResultsMessageID(context.Context, int64, int, time.Time) (bool, error)
	MarkResultsPinned(context.Context, int64, time.Time, time.Time) (bool, error)
	ReleaseResultsClaim(context.Context, int64, time.Time) error
	ListUnsentAchievements(context.Context, int64, int) ([]repository.Challenge, error)
	ClaimAchievements(context.Context, int64, time.Time) (bool, error)
	MarkAchievementsSent(context.Context, int64, time.Time, time.Time) (bool, error)
	SetAchievementsMessageID(context.Context, int64, int, time.Time, time.Time) (bool, error)
	RecordAchievementsMessageID(context.Context, int64, int, time.Time) (bool, error)
	ReleaseAchievementsClaim(context.Context, int64, time.Time) error
}

type photoStore interface {
	ListByChallenge(context.Context, int64) ([]repository.Photo, error)
}

type voteStore interface {
	ListVotes(context.Context, int64) ([]repository.Vote, error)
	CountFinishedWinsByAuthorThrough(context.Context, int64, time.Time, int64) (int, error)
}

type userStore interface {
	Get(context.Context, int64) (repository.User, error)
}

type renderer interface {
	Results(templates.ResultsData) (string, error)
}

type publisher interface {
	SendMarkdown(context.Context, int64, string) (int, error)
	SendText(context.Context, int64, string) (int, error)
	Pin(context.Context, int64, int) error
}

type PublisherService struct {
	challenges challengeStore
	photos     photoStore
	votes      voteStore
	users      userStore
	renderer   renderer
	publisher  publisher
	now        func() time.Time
	sendFor    time.Duration
}

type PublishConfig struct {
	Challenges  challengeStore
	Photos      photoStore
	Votes       voteStore
	Users       userStore
	Renderer    renderer
	Publisher   publisher
	Now         func() time.Time
	SendTimeout time.Duration
}

func NewPublisher(cfg PublishConfig) *PublisherService {
	require.NotNil("results challenge repository", cfg.Challenges)
	require.NotNil("results photo repository", cfg.Photos)
	require.NotNil("results vote repository", cfg.Votes)
	require.NotNil("results user repository", cfg.Users)
	require.NotNil("results renderer", cfg.Renderer)
	require.NotNil("results publisher", cfg.Publisher)
	require.NotNil("clock", cfg.Now)
	sendFor := cfg.SendTimeout
	if sendFor <= 0 {
		sendFor = defaultSendTimeout
	}
	return &PublisherService{
		challenges: cfg.Challenges,
		photos:     cfg.Photos,
		votes:      cfg.Votes,
		users:      cfg.Users,
		renderer:   cfg.Renderer,
		publisher:  cfg.Publisher,
		now:        cfg.Now,
		sendFor:    sendFor,
	}
}

func (s *PublisherService) PublishDue(ctx context.Context, mainChatID int64, limit int) error {
	now := s.now()
	due, err := s.challenges.ListUnpublishedResults(ctx, mainChatID, limit)
	if err != nil {
		return err
	}

	for _, challenge := range due {
		if err := s.publishOne(ctx, challenge, now); err != nil {
			return err
		}
	}
	return s.PublishDueAchievements(ctx, mainChatID, limit)
}

func (s *PublisherService) PublishOne(ctx context.Context, challengeID int64) error {
	challenge, err := s.challenges.Get(ctx, challengeID)
	if err != nil {
		return err
	}
	if err := s.publishOne(ctx, challenge, s.now()); err != nil {
		return err
	}
	return s.PublishDueAchievements(ctx, challenge.MainChatID, 100)
}

func (s *PublisherService) publishOne(ctx context.Context, challenge repository.Challenge, now time.Time) error {
	if challenge.State != repository.ChallengeStateFinished || challenge.ResultsPinnedAt != nil {
		return nil
	}

	claimedAt := now
	claimed, err := s.challenges.ClaimResults(ctx, challenge.ID, claimedAt)
	if err != nil {
		return err
	}
	if !claimed {
		return nil
	}

	messageID := 0
	if challenge.ResultsMessageID != nil {
		messageID = int(*challenge.ResultsMessageID)
	} else {
		text, err := s.render(ctx, challenge)
		if err != nil {
			s.releaseClaim(ctx, challenge.ID, claimedAt)
			return err
		}
		sendCtx, cancel := context.WithTimeout(ctx, s.sendFor)
		sentID, err := s.publisher.SendMarkdown(sendCtx, challenge.MainChatID, text)
		cancel()
		if err != nil {
			s.releaseClaim(ctx, challenge.ID, claimedAt)
			return fmt.Errorf("send results for challenge %d: %w", challenge.ID, err)
		}
		persistCtx, cancel := s.persistContext(ctx)
		marked, err := s.challenges.SetResultsMessageID(persistCtx, challenge.ID, sentID, claimedAt, now)
		cancel()
		if err != nil {
			persistCtx, cancel = s.persistContext(ctx)
			recorded, recordErr := s.challenges.RecordResultsMessageID(persistCtx, challenge.ID, sentID, now)
			cancel()
			if recordErr != nil {
				return fmt.Errorf("%w; record sent results message id: %v", err, recordErr)
			}
			if !recorded {
				return fmt.Errorf("%w; record sent results message id: row not changed", err)
			}
			marked = true
		}
		if !marked {
			return fmt.Errorf("set results message id for challenge %d: claim no longer owned", challenge.ID)
		}
		messageID = sentID
	}

	pinCtx, cancel := context.WithTimeout(ctx, s.sendFor)
	err = s.publisher.Pin(pinCtx, challenge.MainChatID, messageID)
	cancel()
	if err != nil {
		s.releaseClaim(ctx, challenge.ID, claimedAt)
		return fmt.Errorf("pin results for challenge %d: %w", challenge.ID, err)
	}
	persistCtx, cancel := s.persistContext(ctx)
	pinned, err := s.challenges.MarkResultsPinned(persistCtx, challenge.ID, claimedAt, now)
	cancel()
	if err != nil {
		return err
	}
	if !pinned {
		return fmt.Errorf("mark results pinned for challenge %d: claim no longer owned", challenge.ID)
	}
	return nil
}

func (s *PublisherService) PublishDueAchievements(ctx context.Context, mainChatID int64, limit int) error {
	due, err := s.challenges.ListUnsentAchievements(ctx, mainChatID, limit)
	if err != nil {
		return err
	}
	for _, challenge := range due {
		if err := s.ensureEarlierResultsPublished(ctx, challenge); err != nil {
			return err
		}
		if err := s.publishAchievements(ctx, challenge); err != nil {
			return err
		}
	}
	return nil
}

func (s *PublisherService) ensureEarlierResultsPublished(ctx context.Context, challenge repository.Challenge) error {
	unpublished, err := s.challenges.ListUnpublishedResults(ctx, challenge.MainChatID, 1)
	if err != nil {
		return err
	}
	if len(unpublished) == 0 || !sameOrEarlier(unpublished[0], challenge) {
		return nil
	}
	return fmt.Errorf("publish achievements for challenge %d: earlier results for challenge %d are not published", challenge.ID, unpublished[0].ID)
}

func sameOrEarlier(candidate, current repository.Challenge) bool {
	if candidate.FinishedAt == nil {
		return true
	}
	if current.FinishedAt == nil {
		return candidate.ID <= current.ID
	}
	if candidate.FinishedAt.Before(*current.FinishedAt) {
		return true
	}
	return candidate.FinishedAt.Equal(*current.FinishedAt) && candidate.ID <= current.ID
}

func (s *PublisherService) publishAchievements(ctx context.Context, challenge repository.Challenge) error {
	claimedAt := s.now()
	claimed, err := s.challenges.ClaimAchievements(ctx, challenge.ID, claimedAt)
	if err != nil {
		return err
	}
	if !claimed {
		return nil
	}
	if challenge.AchievementsMessageID != nil {
		return s.markAchievementsSent(ctx, challenge.ID, claimedAt, s.now())
	}

	photos, votes, err := s.challengeInputs(ctx, challenge.ID)
	if err != nil {
		s.releaseAchievementsClaim(ctx, challenge.ID, claimedAt)
		return err
	}
	result := Calculate(photos, votes)
	if result.NoWinners {
		return s.markAchievementsSent(ctx, challenge.ID, claimedAt, s.now())
	}

	messages := make([]string, 0, len(result.Works))
	for _, work := range result.Works {
		if !work.Winner {
			continue
		}
		if challenge.FinishedAt == nil {
			s.releaseAchievementsClaim(ctx, challenge.ID, claimedAt)
			return fmt.Errorf("publish achievement for challenge %d: finished_at is empty", challenge.ID)
		}
		winCount, err := s.votes.CountFinishedWinsByAuthorThrough(ctx, work.AuthorUserID, *challenge.FinishedAt, challenge.ID)
		if err != nil {
			s.releaseAchievementsClaim(ctx, challenge.ID, claimedAt)
			return err
		}
		if _, ok := achievementMilestones[winCount]; !ok {
			continue
		}
		user, err := s.users.Get(ctx, work.AuthorUserID)
		if err != nil {
			s.releaseAchievementsClaim(ctx, challenge.ID, claimedAt)
			return err
		}
		messages = append(messages, fmt.Sprintf("%s выигрывает фоточеллендж в %d-й раз. Можно выдавать ачивку.", authorHandle(user), winCount))
	}
	if len(messages) == 0 {
		return s.markAchievementsSent(ctx, challenge.ID, claimedAt, s.now())
	}
	sendCtx, cancel := context.WithTimeout(ctx, s.sendFor)
	messageID, err := s.publisher.SendText(sendCtx, challenge.MainChatID, strings.Join(messages, "\n"))
	cancel()
	if err != nil {
		s.releaseAchievementsClaim(ctx, challenge.ID, claimedAt)
		return err
	}
	if err := s.rememberAchievementsMessage(ctx, challenge.ID, messageID, claimedAt, s.now()); err != nil {
		return err
	}
	return s.markAchievementsSent(ctx, challenge.ID, claimedAt, s.now())
}

func (s *PublisherService) render(ctx context.Context, challenge repository.Challenge) (string, error) {
	photos, votes, err := s.challengeInputs(ctx, challenge.ID)
	if err != nil {
		return "", err
	}
	result := Calculate(photos, votes)
	data, err := s.templateData(ctx, challenge.Theme, result)
	if err != nil {
		return "", err
	}
	return s.renderer.Results(data)
}

func (s *PublisherService) challengeInputs(ctx context.Context, challengeID int64) ([]repository.Photo, []repository.Vote, error) {
	photos, err := s.photos.ListByChallenge(ctx, challengeID)
	if err != nil {
		return nil, nil, err
	}
	votes, err := s.votes.ListVotes(ctx, challengeID)
	if err != nil {
		return nil, nil, err
	}
	return photos, votes, nil
}

func (s *PublisherService) templateData(ctx context.Context, theme string, result Result) (templates.ResultsData, error) {
	works := make([]templates.ResultLine, 0, len(result.Works))
	winners := make([]templates.ResultLine, 0, len(result.WinnerPhotoIDs))
	for _, work := range result.Works {
		user, err := s.users.Get(ctx, work.AuthorUserID)
		if err != nil {
			return templates.ResultsData{}, err
		}
		line := templates.ResultLine{
			AuthorHandle: authorHandle(user),
			FullName:     fullName(user),
			Likes:        work.TotalVotes,
			Winner:       work.Winner,
		}
		works = append(works, line)
		if work.Winner {
			winners = append(winners, line)
		}
	}
	return templates.ResultsData{
		Theme:           theme,
		NoWinners:       result.NoWinners,
		MultipleWinners: len(winners) > 1,
		Winners:         winners,
		Works:           works,
	}, nil
}

func authorHandle(user repository.User) string {
	if user.Username != "" {
		return "@" + strings.TrimPrefix(user.Username, "@")
	}
	return strconv.FormatInt(user.ID, 10)
}

func fullName(user repository.User) string {
	name := strings.TrimSpace(strings.Join([]string{user.FirstName, user.LastName}, " "))
	if name != "" {
		return name
	}
	if user.DisplayName != "" {
		return user.DisplayName
	}
	return authorHandle(user)
}

func (s *PublisherService) releaseClaim(ctx context.Context, challengeID int64, claimedAt time.Time) {
	persistCtx, cancel := s.persistContext(ctx)
	_ = s.challenges.ReleaseResultsClaim(persistCtx, challengeID, claimedAt)
	cancel()
}

func (s *PublisherService) markAchievementsSent(ctx context.Context, challengeID int64, claimedAt, sentAt time.Time) error {
	persistCtx, cancel := s.persistContext(ctx)
	marked, err := s.challenges.MarkAchievementsSent(persistCtx, challengeID, claimedAt, sentAt)
	cancel()
	if err != nil {
		return err
	}
	if !marked {
		return fmt.Errorf("mark achievements sent for challenge %d: claim no longer owned", challengeID)
	}
	return nil
}

func (s *PublisherService) rememberAchievementsMessage(ctx context.Context, challengeID int64, messageID int, claimedAt, sentAt time.Time) error {
	persistCtx, cancel := s.persistContext(ctx)
	marked, err := s.challenges.SetAchievementsMessageID(persistCtx, challengeID, messageID, claimedAt, sentAt)
	cancel()
	if err == nil {
		if !marked {
			return fmt.Errorf("set achievements message id for challenge %d: claim no longer owned", challengeID)
		}
		return nil
	}

	persistCtx, cancel = s.persistContext(ctx)
	recorded, recordErr := s.challenges.RecordAchievementsMessageID(persistCtx, challengeID, messageID, sentAt)
	cancel()
	if recordErr != nil {
		return fmt.Errorf("%w; record sent achievements message id: %v", err, recordErr)
	}
	if !recorded {
		return fmt.Errorf("%w; record sent achievements message id: row not changed", err)
	}
	return nil
}

func (s *PublisherService) releaseAchievementsClaim(ctx context.Context, challengeID int64, claimedAt time.Time) {
	persistCtx, cancel := s.persistContext(ctx)
	_ = s.challenges.ReleaseAchievementsClaim(persistCtx, challengeID, claimedAt)
	cancel()
}

func (s *PublisherService) persistContext(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(ctx), s.sendFor)
}
