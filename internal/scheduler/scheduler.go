package scheduler

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/TiraelSedai/PhotoChallengeBot/internal/challenge"
	"github.com/TiraelSedai/PhotoChallengeBot/internal/publish"
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

type challenges interface {
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

type publisher interface {
	SendMarkdown(context.Context, int64, string) (int, error)
	Pin(context.Context, int64, int) error
}

type photoCounter interface {
	CountByChallenge(context.Context, int64) (int, error)
}

type voteStartRenderer interface {
	VoteStart(templates.VoteStartData) (string, error)
}

type resultsPublisher interface {
	PublishDue(context.Context, int64, int) error
}

type topicReporter interface {
	PublishDue(context.Context, int64, int) error
}

type Config struct {
	MainChatID         int64
	Challenges         challenges
	Photos             photoCounter
	Renderer           voteStartRenderer
	Results            resultsPublisher
	Topics             topicReporter
	Publisher          publisher
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
	challenges  challenges
	photos      photoCounter
	renderer    voteStartRenderer
	results     resultsPublisher
	topics      topicReporter
	publisher   publisher
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
		_, err := publish.Attempt(ctx,
			publish.Config{PersistTimeout: s.persistFor},
			publish.Stage{Claim: s.challenges.ClaimReminder, Release: s.challenges.ReleaseReminderClaim},
			challenge.ID, now,
			func(ctx context.Context, l *publish.Lease) error {
				sendCtx, cancel := context.WithTimeout(ctx, s.sendFor)
				messageID, err := s.publisher.SendMarkdown(sendCtx, challenge.MainChatID, reminderText)
				cancel()
				if err != nil {
					if releaseErr := l.Release(ctx); releaseErr != nil {
						return fmt.Errorf("send reminder for challenge %d: %w; release claim: %v", challenge.ID, err, releaseErr)
					}
					return fmt.Errorf("send reminder for challenge %d: %w", challenge.ID, err)
				}
				if err := l.Commit(ctx, fmt.Sprintf("mark reminder sent for challenge %d", challenge.ID),
					func(pctx context.Context) (bool, error) {
						return s.challenges.MarkReminderSent(pctx, challenge.ID, messageID, l.ClaimedAt, now)
					}); err != nil {
					return err
				}
				s.logger.Info("sent challenge reminder", "challenge_id", challenge.ID, "main_chat_id", challenge.MainChatID)
				return nil
			})
		if err != nil {
			errs = append(errs, err)
		}
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
		_, err := publish.Attempt(ctx,
			publish.Config{PersistTimeout: s.persistFor},
			publish.Stage{Claim: s.challenges.ClaimVoteStart, Release: s.challenges.ReleaseVoteStartClaim},
			item.ID, now,
			func(ctx context.Context, l *publish.Lease) error {
				var itemErrs []error
				if item.VoteMessageID != nil {
					s.pinVoteStart(ctx, l, item, int(*item.VoteMessageID), now, &itemErrs)
					return errors.Join(itemErrs...)
				}

				photoCount, err := s.photos.CountByChallenge(ctx, item.ID)
				if err != nil {
					itemErrs = append(itemErrs, err)
					if releaseErr := l.Release(ctx); releaseErr != nil {
						itemErrs = append(itemErrs, releaseErr)
					}
					return errors.Join(itemErrs...)
				}
				voteLink, err := challenge.VoteLink(s.botUsername(), item.MainChatID, item.ID)
				if err != nil {
					itemErrs = append(itemErrs, fmt.Errorf("build vote link for challenge %d: %w", item.ID, err))
					if releaseErr := l.Release(ctx); releaseErr != nil {
						itemErrs = append(itemErrs, releaseErr)
					}
					return errors.Join(itemErrs...)
				}
				voteUntilAt := publishVoteUntil(item, now)
				text, err := s.renderer.VoteStart(templates.VoteStartData{
					Theme:       item.Theme,
					AmountPhoto: photoCount,
					VoteLink:    voteLink,
					ResultsDate: challenge.VotingEndsText(voteUntilAt, s.location),
				})
				if err != nil {
					itemErrs = append(itemErrs, err)
					if releaseErr := l.Release(ctx); releaseErr != nil {
						itemErrs = append(itemErrs, releaseErr)
					}
					return errors.Join(itemErrs...)
				}

				sendCtx, cancel := context.WithTimeout(ctx, s.sendFor)
				messageID, err := s.publisher.SendMarkdown(sendCtx, item.MainChatID, text)
				cancel()
				if err != nil {
					itemErrs = append(itemErrs, fmt.Errorf("send vote start for challenge %d: %w", item.ID, err))
					if releaseErr := l.Release(ctx); releaseErr != nil {
						itemErrs = append(itemErrs, releaseErr)
					}
					return errors.Join(itemErrs...)
				}

				if err := l.CommitOrRecord(ctx,
					fmt.Sprintf("set vote message id for challenge %d", item.ID),
					"sent vote message id",
					func(pctx context.Context) (bool, error) {
						return s.challenges.SetVoteMessageID(pctx, item.ID, messageID, l.ClaimedAt, now)
					},
					func(pctx context.Context) (bool, error) {
						return s.challenges.RecordVoteMessageID(pctx, item.ID, messageID, now)
					}); err != nil {
					itemErrs = append(itemErrs, err)
					return errors.Join(itemErrs...)
				}

				s.pinVoteStart(ctx, l, item, messageID, now, &itemErrs)
				return errors.Join(itemErrs...)
			})
		if err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func publishVoteUntil(item repository.Challenge, now time.Time) time.Time {
	return challenge.VotingEndsAt(now)
}

func (s *Scheduler) ensureVoteWindow(ctx context.Context, item repository.Challenge, now time.Time, errs *[]error) bool {
	persistCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), s.persistFor)
	extended, err := s.challenges.ExtendVoteUntil(persistCtx, item.ID, now)
	cancel()
	if err != nil {
		*errs = append(*errs, err)
		return false
	}
	if extended == nil {
		*errs = append(*errs, fmt.Errorf("extend vote window for challenge %d: row not changed", item.ID))
		return false
	}
	return true
}

func (s *Scheduler) pinVoteStart(
	ctx context.Context,
	l *publish.Lease,
	item repository.Challenge,
	messageID int,
	now time.Time,
	errs *[]error,
) {
	if !s.ensureVoteWindow(ctx, item, now, errs) {
		if releaseErr := l.Release(ctx); releaseErr != nil {
			*errs = append(*errs, releaseErr)
		}
		return
	}

	pinCtx, cancel := context.WithTimeout(ctx, s.sendFor)
	err := s.publisher.Pin(pinCtx, item.MainChatID, messageID)
	cancel()
	if err != nil {
		*errs = append(*errs, fmt.Errorf("pin vote start for challenge %d: %w", item.ID, err))
		if releaseErr := l.Release(ctx); releaseErr != nil {
			*errs = append(*errs, releaseErr)
		}
		return
	}

	if err := l.Commit(ctx, fmt.Sprintf("mark vote start pinned for challenge %d", item.ID),
		func(pctx context.Context) (bool, error) {
			return s.challenges.MarkVoteStartPinned(pctx, item.ID, l.ClaimedAt, now)
		}); err != nil {
		*errs = append(*errs, err)
		return
	}
	s.logger.Info("published challenge vote start", "challenge_id", item.ID, "main_chat_id", item.MainChatID)
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
