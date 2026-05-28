package admin

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/TiraelSedai/PhotoChallengeBot/internal/repository"
	"github.com/TiraelSedai/PhotoChallengeBot/internal/require"
	"github.com/go-telegram/bot/models"
)

const (
	finishVoteDoneMessage                = "Голосование завершено. Результаты ниже."
	finishVoteAbsentMessage              = "Активного голосования нет."
	finishVotePublishFailedMessage       = "Голосование завершено, но результаты не удалось опубликовать автоматически. Планировщик попробует еще раз."
	finishVoteTopicsPublishFailedMessage = "Голосование завершено, но темы не удалось отправить в админку автоматически. Планировщик попробует еще раз."
)

type finishVoteChallenges interface {
	FindOpenByMainChatID(context.Context, int64) (*repository.Challenge, error)
	FinishVotingNow(context.Context, int64, time.Time) (bool, error)
}

type finishVotePublisher interface {
	SendText(context.Context, int64, string) (int, error)
}

type finishVoteResults interface {
	PublishOne(context.Context, int64) error
}

type finishVoteTopics interface {
	PublishOne(context.Context, repository.Challenge) error
}

type FinishVoteConfig struct {
	AdminChatID int64
	MainChatID  int64
	Challenges  finishVoteChallenges
	Publisher   finishVotePublisher
	Results     finishVoteResults
	Topics      finishVoteTopics
	BotUsername func() string
	Now         func() time.Time
}

type FinishVoteHandler struct {
	adminChatID int64
	mainChatID  int64
	challenges  finishVoteChallenges
	publisher   finishVotePublisher
	results     finishVoteResults
	topics      finishVoteTopics
	botUsername func() string
	now         func() time.Time
}

func NewFinishVoteHandler(cfg FinishVoteConfig) *FinishVoteHandler {
	require.NotNil("finish vote challenges repository", cfg.Challenges)
	require.NotNil("finish vote publisher", cfg.Publisher)
	require.NotNil("finish vote results publisher", cfg.Results)
	require.NotNil("finish vote topics publisher", cfg.Topics)
	require.NotNil("bot username provider", cfg.BotUsername)
	require.NotNil("clock", cfg.Now)
	return &FinishVoteHandler{
		adminChatID: cfg.AdminChatID,
		mainChatID:  cfg.MainChatID,
		challenges:  cfg.Challenges,
		publisher:   cfg.Publisher,
		results:     cfg.Results,
		topics:      cfg.Topics,
		botUsername: cfg.BotUsername,
		now:         cfg.Now,
	}
}

func (h *FinishVoteHandler) HandleAdminChatMessage(ctx context.Context, message *models.Message) error {
	if message == nil || message.Chat.ID != h.adminChatID {
		return nil
	}
	if !h.Handles(message.Text) {
		return nil
	}
	if isCommandMentionedToOtherBot(message.Text, h.currentBotUsername()) {
		return nil
	}

	open, err := h.challenges.FindOpenByMainChatID(ctx, h.mainChatID)
	if err != nil {
		return err
	}
	if open == nil || open.State != repository.ChallengeStateVoting {
		_, err := h.publisher.SendText(ctx, h.adminChatID, finishVoteAbsentMessage)
		return err
	}

	finishedAt := h.now()
	finished, err := h.challenges.FinishVotingNow(ctx, open.ID, finishedAt)
	if err != nil {
		return err
	}
	if !finished {
		_, err := h.publisher.SendText(ctx, h.adminChatID, finishVoteAbsentMessage)
		return err
	}
	finishedChallenge := *open
	finishedChallenge.State = repository.ChallengeStateFinished
	finishedChallenge.FinishedAt = &finishedAt
	topicErr := h.topics.PublishOne(ctx, finishedChallenge)
	if topicErr != nil {
		if _, sendErr := h.publisher.SendText(ctx, h.adminChatID, finishVoteTopicsPublishFailedMessage); sendErr != nil {
			topicErr = fmt.Errorf("publish topic report: %w; notify admin: %v", topicErr, sendErr)
		}
	}
	if err := h.results.PublishOne(ctx, open.ID); err != nil {
		if _, sendErr := h.publisher.SendText(ctx, h.adminChatID, finishVotePublishFailedMessage); sendErr != nil {
			return errors.Join(topicErr, fmt.Errorf("publish results: %w; notify admin: %v", err, sendErr))
		}
		return errors.Join(topicErr, err)
	}
	_, err = h.publisher.SendText(ctx, h.adminChatID, finishVoteDoneMessage)
	return errors.Join(topicErr, err)
}

func (h *FinishVoteHandler) Handles(text string) bool {
	return isFinishVoteCommand(text, h.currentBotUsername())
}

func (h *FinishVoteHandler) currentBotUsername() string {
	return h.botUsername()
}

func isFinishVoteCommand(text string, botUsername string) bool {
	normalized := strings.ToLower(strings.TrimSpace(text))
	parts := strings.SplitN(normalized, "@", 2)
	command := parts[0]
	if len(parts) == 2 {
		username := strings.TrimSpace(parts[1])
		if username == "" || username != strings.ToLower(strings.TrimPrefix(botUsername, "@")) {
			return false
		}
	}
	return command == "/finish_vote" ||
		command == "/finish_voting" ||
		normalized == "завершить голосование"
}
