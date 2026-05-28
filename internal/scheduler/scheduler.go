package scheduler

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/TiraelSedai/PhotoChallengeBot/internal/challenge"
	"github.com/TiraelSedai/PhotoChallengeBot/internal/repository"
	"github.com/TiraelSedai/PhotoChallengeBot/internal/require"
	"github.com/TiraelSedai/PhotoChallengeBot/internal/templates"
)

const (
	defaultInterval           = time.Minute
	defaultBatchSize          = 100
	defaultPersistenceTimeout = 5 * time.Second
	defaultSendTimeout        = 10 * time.Second

	reminderText = "Напоминалка: завтра последний день челленджа! Присылайте фотографии, если еще не сделали этого."
)

type Challenges interface {
	ListDueReminders(context.Context, int64, time.Time, int) ([]repository.Challenge, error)
	ClaimReminder(context.Context, int64, time.Time) (bool, error)
	MarkReminderSent(context.Context, int64, int, time.Time, time.Time) (bool, error)
	ReleaseReminderClaim(context.Context, int64, time.Time) error
	ListDueAcceptanceClosures(context.Context, int64, time.Time, int) ([]repository.Challenge, error)
	StartVoting(context.Context, int64, time.Time) (bool, error)
	ListUnpublishedVoteStarts(context.Context, int64, int) ([]repository.Challenge, error)
	ClaimVoteStart(context.Context, int64, time.Time) (bool, error)
	SetVoteMessageID(context.Context, int64, int, time.Time, time.Time) (bool, error)
	RecordVoteMessageID(context.Context, int64, int, time.Time) (bool, error)
	MarkVoteStartPinned(context.Context, int64, time.Time, time.Time) (bool, error)
	ReleaseVoteStartClaim(context.Context, int64, time.Time) error
	ExtendVoteUntil(context.Context, int64, time.Time) (*time.Time, error)
	ListDueVotingClosures(context.Context, int64, time.Time, int) ([]repository.Challenge, error)
	FinishVoting(context.Context, int64, time.Time) (bool, error)
}

type Publisher interface {
	SendMarkdown(context.Context, int64, string) (int, error)
	Pin(context.Context, int64, int) error
}

type PhotoCounter interface {
	CountByChallenge(context.Context, int64) (int, error)
}

type VoteStartRenderer interface {
	VoteStart(templates.VoteStartData) (string, error)
}

type ResultsPublisher interface {
	PublishDue(context.Context, int64, int) error
}

type TopicReporter interface {
	PublishDue(context.Context, int64, int) error
}

type Config struct {
	MainChatID         int64
	Challenges         Challenges
	Photos             PhotoCounter
	Renderer           VoteStartRenderer
	Results            ResultsPublisher
	Topics             TopicReporter
	Publisher          Publisher
	Logger             *slog.Logger
	Now                func() time.Time
	BotUsername        func() string
	Location           *time.Location
	Interval           time.Duration
	BatchSize          int
	PersistenceTimeout time.Duration
	SendTimeout        time.Duration
}

type Scheduler struct {
	challenges  Challenges
	photos      PhotoCounter
	renderer    VoteStartRenderer
	results     ResultsPublisher
	topics      TopicReporter
	publisher   Publisher
	logger      *slog.Logger
	now         func() time.Time
	botUsername func() string
	location    *time.Location
	interval    time.Duration
	batchSize   int
	mainChatID  int64
	persistFor  time.Duration
	sendFor     time.Duration
}

func New(config Config) *Scheduler {
	require.NotNil("scheduler challenges repository", config.Challenges)
	require.NotNil("scheduler photo counter", config.Photos)
	require.NotNil("scheduler vote start renderer", config.Renderer)
	require.NotNil("scheduler publisher", config.Publisher)
	require.NotNil("scheduler results publisher", config.Results)
	require.NotNil("scheduler topic reporter", config.Topics)
	require.NotNil("scheduler logger", config.Logger)
	require.NotNil("clock", config.Now)
	require.NotNil("bot username provider", config.BotUsername)
	require.NotNil("location", config.Location)
	interval := config.Interval
	if interval <= 0 {
		interval = defaultInterval
	}
	batchSize := config.BatchSize
	if batchSize <= 0 {
		batchSize = defaultBatchSize
	}
	persistFor := config.PersistenceTimeout
	if persistFor <= 0 {
		persistFor = defaultPersistenceTimeout
	}
	sendFor := config.SendTimeout
	if sendFor <= 0 {
		sendFor = defaultSendTimeout
	}

	return &Scheduler{
		challenges:  config.Challenges,
		photos:      config.Photos,
		renderer:    config.Renderer,
		results:     config.Results,
		topics:      config.Topics,
		publisher:   config.Publisher,
		logger:      config.Logger,
		now:         config.Now,
		botUsername: config.BotUsername,
		location:    config.Location,
		interval:    interval,
		batchSize:   batchSize,
		mainChatID:  config.MainChatID,
		persistFor:  persistFor,
		sendFor:     sendFor,
	}
}

func (s *Scheduler) Run(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	s.tickAndLog(ctx)

	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			s.tickAndLog(ctx)
		}
	}
}

func (s *Scheduler) tickAndLog(ctx context.Context) {
	if err := s.Tick(ctx); err != nil && ctx.Err() == nil {
		s.logger.Error("run scheduler tick", "error", err)
	}
}

func (s *Scheduler) Tick(ctx context.Context) error {
	now := s.now().UTC()
	var errs []error
	if err := s.sendDueReminders(ctx, now); err != nil {
		errs = append(errs, err)
	}
	if err := s.closeDueAcceptance(ctx, now); err != nil {
		errs = append(errs, err)
	}
	if err := s.publishDueVoteStarts(ctx, now); err != nil {
		errs = append(errs, err)
	}
	if err := s.closeDueVoting(ctx, now); err != nil {
		errs = append(errs, err)
	}
	if err := s.topics.PublishDue(ctx, s.mainChatID, s.batchSize); err != nil {
		errs = append(errs, err)
	}
	if err := s.results.PublishDue(ctx, s.mainChatID, s.batchSize); err != nil {
		errs = append(errs, err)
	}
	return errors.Join(errs...)
}

func (s *Scheduler) sendDueReminders(ctx context.Context, now time.Time) error {
	due, err := s.challenges.ListDueReminders(ctx, s.mainChatID, now, s.batchSize)
	if err != nil {
		return err
	}
	if len(due) == 0 {
		return nil
	}

	var errs []error
	for _, challenge := range due {
		if !s.owns(challenge) {
			continue
		}
		claimedAt := now
		claimed, err := s.challenges.ClaimReminder(ctx, challenge.ID, claimedAt)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		if !claimed {
			continue
		}
		sendCtx, cancel := context.WithTimeout(ctx, s.sendFor)
		messageID, err := s.publisher.SendMarkdown(sendCtx, challenge.MainChatID, reminderText)
		cancel()
		if err != nil {
			persistCtx, cancel := s.persistenceContext(ctx)
			releaseErr := s.challenges.ReleaseReminderClaim(persistCtx, challenge.ID, claimedAt)
			cancel()
			if releaseErr != nil {
				errs = append(errs, fmt.Errorf("send reminder for challenge %d: %w; release claim: %v", challenge.ID, err, releaseErr))
				continue
			}
			errs = append(errs, fmt.Errorf("send reminder for challenge %d: %w", challenge.ID, err))
			continue
		}
		persistCtx, cancel := s.persistenceContext(ctx)
		marked, err := s.challenges.MarkReminderSent(persistCtx, challenge.ID, messageID, claimedAt, now)
		cancel()
		if err != nil {
			errs = append(errs, err)
			continue
		}
		if !marked {
			errs = append(errs, fmt.Errorf("mark reminder sent for challenge %d: claim no longer owned", challenge.ID))
			continue
		}
		s.logger.Info("sent challenge reminder", "challenge_id", challenge.ID, "main_chat_id", challenge.MainChatID)
	}
	return errors.Join(errs...)
}

func (s *Scheduler) closeDueAcceptance(ctx context.Context, now time.Time) error {
	due, err := s.challenges.ListDueAcceptanceClosures(ctx, s.mainChatID, now, s.batchSize)
	if err != nil {
		return err
	}

	var errs []error
	for _, challenge := range due {
		if !s.owns(challenge) {
			continue
		}
		changed, err := s.challenges.StartVoting(ctx, challenge.ID, now)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		if changed {
			s.logger.Info("closed challenge acceptance", "challenge_id", challenge.ID, "main_chat_id", challenge.MainChatID)
		}
	}
	return errors.Join(errs...)
}

func (s *Scheduler) publishDueVoteStarts(ctx context.Context, now time.Time) error {
	due, err := s.challenges.ListUnpublishedVoteStarts(ctx, s.mainChatID, s.batchSize)
	if err != nil {
		return err
	}
	if len(due) == 0 {
		return nil
	}

	var errs []error
	for _, item := range due {
		if !s.owns(item) {
			continue
		}
		claimedAt := now
		claimed, err := s.challenges.ClaimVoteStart(ctx, item.ID, claimedAt)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		if !claimed {
			continue
		}

		if item.VoteMessageID != nil {
			s.pinVoteStart(ctx, item, int(*item.VoteMessageID), claimedAt, now, &errs)
			continue
		}

		photoCount, err := s.photos.CountByChallenge(ctx, item.ID)
		if err != nil {
			errs = append(errs, err)
			s.releaseVoteStartClaim(ctx, item.ID, claimedAt, &errs)
			continue
		}
		voteLink, err := challenge.VoteLink(s.botUsername(), item.MainChatID, item.ID)
		if err != nil {
			errs = append(errs, fmt.Errorf("build vote link for challenge %d: %w", item.ID, err))
			s.releaseVoteStartClaim(ctx, item.ID, claimedAt, &errs)
			continue
		}
		voteUntilAt := publishVoteUntil(item, now)
		text, err := s.renderer.VoteStart(templates.VoteStartData{
			Theme:       item.Theme,
			AmountPhoto: photoCount,
			VoteLink:    voteLink,
			ResultsDate: challenge.VotingEndsText(voteUntilAt, s.location),
		})
		if err != nil {
			errs = append(errs, err)
			s.releaseVoteStartClaim(ctx, item.ID, claimedAt, &errs)
			continue
		}

		messageID, marked, ok := s.ensureVoteMessage(ctx, item, text, claimedAt, now, &errs)
		if !ok {
			continue
		}
		if !marked {
			errs = append(errs, fmt.Errorf("set vote message id for challenge %d: claim no longer owned", item.ID))
			continue
		}

		s.pinVoteStart(ctx, item, messageID, claimedAt, now, &errs)
	}
	return errors.Join(errs...)
}

func publishVoteUntil(item repository.Challenge, now time.Time) time.Time {
	return challenge.VotingEndsAt(now)
}

func (s *Scheduler) ensureVoteWindow(ctx context.Context, item repository.Challenge, now time.Time, errs *[]error) (time.Time, bool) {
	persistCtx, cancel := s.persistenceContext(ctx)
	extended, err := s.challenges.ExtendVoteUntil(persistCtx, item.ID, now)
	cancel()
	if err != nil {
		*errs = append(*errs, err)
		return time.Time{}, false
	}
	if extended == nil {
		*errs = append(*errs, fmt.Errorf("extend vote window for challenge %d: row not changed", item.ID))
		return time.Time{}, false
	}
	return *extended, true
}

func (s *Scheduler) ensureVoteMessage(
	ctx context.Context,
	item repository.Challenge,
	text string,
	claimedAt time.Time,
	now time.Time,
	errs *[]error,
) (int, bool, bool) {
	if item.VoteMessageID != nil {
		return int(*item.VoteMessageID), true, true
	}

	sendCtx, cancel := context.WithTimeout(ctx, s.sendFor)
	messageID, err := s.publisher.SendMarkdown(sendCtx, item.MainChatID, text)
	cancel()
	if err != nil {
		*errs = append(*errs, fmt.Errorf("send vote start for challenge %d: %w", item.ID, err))
		s.releaseVoteStartClaim(ctx, item.ID, claimedAt, errs)
		return 0, false, false
	}

	persistCtx, cancel := s.persistenceContext(ctx)
	marked, err := s.challenges.SetVoteMessageID(persistCtx, item.ID, messageID, claimedAt, now)
	cancel()
	if err != nil {
		persistCtx, cancel = s.persistenceContext(ctx)
		recorded, recordErr := s.challenges.RecordVoteMessageID(persistCtx, item.ID, messageID, now)
		cancel()
		if recordErr != nil {
			*errs = append(*errs, fmt.Errorf("%w; record sent vote message id: %v", err, recordErr))
			return 0, false, false
		}
		if !recorded {
			*errs = append(*errs, fmt.Errorf("%w; record sent vote message id: row not changed", err))
			return 0, false, false
		}
		return messageID, true, true
	}
	return messageID, marked, true
}

func (s *Scheduler) pinVoteStart(
	ctx context.Context,
	item repository.Challenge,
	messageID int,
	claimedAt time.Time,
	now time.Time,
	errs *[]error,
) {
	if voteUntilAt, ok := s.ensureVoteWindow(ctx, item, now, errs); ok {
		item.VoteUntilAt = &voteUntilAt
	} else {
		s.releaseVoteStartClaim(ctx, item.ID, claimedAt, errs)
		return
	}

	pinCtx, cancel := context.WithTimeout(ctx, s.sendFor)
	err := s.publisher.Pin(pinCtx, item.MainChatID, messageID)
	cancel()
	if err != nil {
		*errs = append(*errs, fmt.Errorf("pin vote start for challenge %d: %w", item.ID, err))
		s.releaseVoteStartClaim(ctx, item.ID, claimedAt, errs)
		return
	}

	persistCtx, cancel := s.persistenceContext(ctx)
	pinned, err := s.challenges.MarkVoteStartPinned(persistCtx, item.ID, claimedAt, now)
	cancel()
	if err != nil {
		*errs = append(*errs, err)
		return
	}
	if !pinned {
		*errs = append(*errs, fmt.Errorf("mark vote start pinned for challenge %d: claim no longer owned", item.ID))
		return
	}
	s.logger.Info("published challenge vote start", "challenge_id", item.ID, "main_chat_id", item.MainChatID)
}

func (s *Scheduler) releaseVoteStartClaim(ctx context.Context, challengeID int64, claimedAt time.Time, errs *[]error) {
	persistCtx, cancel := s.persistenceContext(ctx)
	err := s.challenges.ReleaseVoteStartClaim(persistCtx, challengeID, claimedAt)
	cancel()
	if err != nil {
		*errs = append(*errs, err)
	}
}

func (s *Scheduler) closeDueVoting(ctx context.Context, now time.Time) error {
	due, err := s.challenges.ListDueVotingClosures(ctx, s.mainChatID, now, s.batchSize)
	if err != nil {
		return err
	}

	var errs []error
	for _, challenge := range due {
		if !s.owns(challenge) {
			continue
		}
		changed, err := s.challenges.FinishVoting(ctx, challenge.ID, now)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		if changed {
			s.logger.Info("closed challenge voting", "challenge_id", challenge.ID, "main_chat_id", challenge.MainChatID)
		}
	}
	return errors.Join(errs...)
}

func (s *Scheduler) owns(challenge repository.Challenge) bool {
	return s.mainChatID == 0 || challenge.MainChatID == s.mainChatID
}

func (s *Scheduler) persistenceContext(ctx context.Context) (context.Context, context.CancelFunc) {
	if ctx == nil {
		return context.WithTimeout(context.Background(), s.persistFor)
	}
	return context.WithTimeout(context.WithoutCancel(ctx), s.persistFor)
}
