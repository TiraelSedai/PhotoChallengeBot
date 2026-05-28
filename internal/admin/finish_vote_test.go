package admin

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/TiraelSedai/PhotoChallengeBot/internal/repository"
)

func TestFinishVoteHandlerFinishesVotingAndPublishesResults(t *testing.T) {
	challenge := repository.Challenge{
		ID:         42,
		MainChatID: -1001,
		State:      repository.ChallengeStateVoting,
	}
	challenges := &MoqFinishVoteChallenges{
		FindOpenByMainChatIDFunc: func(context.Context, int64) (*repository.Challenge, error) {
			return &challenge, nil
		},
		FinishVotingNowFunc: func(context.Context, int64, time.Time) (bool, error) {
			return true, nil
		},
	}
	publisher := &MoqFinishVotePublisher{}
	results := &MoqFinishVoteResults{}
	topics := &MoqFinishVoteTopics{}
	handler := NewFinishVoteHandler(FinishVoteConfig{
		AdminChatID: -2002,
		MainChatID:  -1001,
		Challenges:  challenges,
		Publisher:   publisher,
		Results:     results,
		Topics:      topics,
		BotUsername: func() string { return "PhotoChallengeBot" },
		Now:         func() time.Time { return time.Date(2026, 5, 19, 12, 0, 0, 0, time.UTC) },
	})

	if err := handler.HandleAdminChatMessage(context.Background(), adminMessage("/finish_vote")); err != nil {
		t.Fatalf("HandleAdminChatMessage() error = %v", err)
	}

	finishCalls := challenges.FinishVotingNowCalls()
	if len(finishCalls) != 1 || finishCalls[0].N != 42 {
		t.Fatalf("FinishVotingNow calls = %#v, want challenge 42", finishCalls)
	}
	publishCalls := results.PublishOneCalls()
	if len(publishCalls) != 1 || publishCalls[0].N != 42 {
		t.Fatalf("PublishOne calls = %#v, want challenge 42", publishCalls)
	}
	topicCalls := topics.PublishOneCalls()
	if len(topicCalls) != 1 || topicCalls[0].Challenge.ID != 42 || topicCalls[0].Challenge.State != repository.ChallengeStateFinished {
		t.Fatalf("topic PublishOne calls = %#v, want finished challenge 42", topicCalls)
	}
	if got := publisher.SendTextCalls()[0].S; got != finishVoteDoneMessage {
		t.Fatalf("sent = %q, want done message", got)
	}
}

func TestNewFinishVoteHandlerPanicsOnNilTopics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("NewFinishVoteHandler() did not panic")
		}
	}()
	NewFinishVoteHandler(FinishVoteConfig{
		AdminChatID: -2002,
		MainChatID:  -1001,
		Challenges:  &MoqFinishVoteChallenges{},
		Publisher:   &MoqFinishVotePublisher{},
		Results:     &MoqFinishVoteResults{},
		BotUsername: func() string { return "PhotoChallengeBot" },
		Now:         func() time.Time { return time.Date(2026, 5, 19, 12, 0, 0, 0, time.UTC) },
	})
}

func TestFinishVoteHandlerReportsAbsentVoting(t *testing.T) {
	challenges := &MoqFinishVoteChallenges{
		FindOpenByMainChatIDFunc: func(context.Context, int64) (*repository.Challenge, error) {
			return &repository.Challenge{ID: 42, State: repository.ChallengeStateActive}, nil
		},
	}
	publisher := &MoqFinishVotePublisher{}
	results := &MoqFinishVoteResults{}
	handler := NewFinishVoteHandler(FinishVoteConfig{
		AdminChatID: -2002,
		MainChatID:  -1001,
		Challenges:  challenges,
		Publisher:   publisher,
		Results:     results,
		Topics:      &MoqFinishVoteTopics{},
		BotUsername: func() string { return "PhotoChallengeBot" },
		Now:         func() time.Time { return time.Date(2026, 5, 19, 12, 0, 0, 0, time.UTC) },
	})

	if err := handler.HandleAdminChatMessage(context.Background(), adminMessage("/finish_vote")); err != nil {
		t.Fatalf("HandleAdminChatMessage() error = %v", err)
	}
	if len(challenges.FinishVotingNowCalls()) != 0 {
		t.Fatalf("FinishVotingNow calls = %#v, want none", challenges.FinishVotingNowCalls())
	}
	if len(results.PublishOneCalls()) != 0 {
		t.Fatalf("PublishOne calls = %#v, want none", results.PublishOneCalls())
	}
	if got := publisher.SendTextCalls()[0].S; got != finishVoteAbsentMessage {
		t.Fatalf("sent = %q, want absent message", got)
	}
}

func TestFinishVoteHandlerDoesNotPublishWhenFinishDoesNotChangeRow(t *testing.T) {
	challenge := repository.Challenge{
		ID:         42,
		MainChatID: -1001,
		State:      repository.ChallengeStateVoting,
	}
	challenges := &MoqFinishVoteChallenges{
		FindOpenByMainChatIDFunc: func(context.Context, int64) (*repository.Challenge, error) {
			return &challenge, nil
		},
	}
	publisher := &MoqFinishVotePublisher{}
	results := &MoqFinishVoteResults{}
	handler := NewFinishVoteHandler(FinishVoteConfig{
		AdminChatID: -2002,
		MainChatID:  -1001,
		Challenges:  challenges,
		Publisher:   publisher,
		Results:     results,
		Topics:      &MoqFinishVoteTopics{},
		BotUsername: func() string { return "PhotoChallengeBot" },
		Now:         func() time.Time { return time.Date(2026, 5, 19, 12, 0, 0, 0, time.UTC) },
	})

	if err := handler.HandleAdminChatMessage(context.Background(), adminMessage("/finish_vote")); err != nil {
		t.Fatalf("HandleAdminChatMessage() error = %v", err)
	}
	if len(results.PublishOneCalls()) != 0 {
		t.Fatalf("PublishOne calls = %#v, want none", results.PublishOneCalls())
	}
	if got := publisher.SendTextCalls()[0].S; got != finishVoteAbsentMessage {
		t.Fatalf("sent = %q, want absent message", got)
	}
}

func TestFinishVoteHandlerReportsPublishFailureToAdmin(t *testing.T) {
	wantErr := errors.New("publish failed")
	challenge := repository.Challenge{
		ID:         42,
		MainChatID: -1001,
		State:      repository.ChallengeStateVoting,
	}
	challenges := &MoqFinishVoteChallenges{
		FindOpenByMainChatIDFunc: func(context.Context, int64) (*repository.Challenge, error) {
			return &challenge, nil
		},
		FinishVotingNowFunc: func(context.Context, int64, time.Time) (bool, error) {
			return true, nil
		},
	}
	publisher := &MoqFinishVotePublisher{}
	results := &MoqFinishVoteResults{PublishOneFunc: func(context.Context, int64) error {
		return wantErr
	}}
	handler := NewFinishVoteHandler(FinishVoteConfig{
		AdminChatID: -2002,
		MainChatID:  -1001,
		Challenges:  challenges,
		Publisher:   publisher,
		Results:     results,
		Topics:      &MoqFinishVoteTopics{},
		BotUsername: func() string { return "PhotoChallengeBot" },
		Now:         func() time.Time { return time.Date(2026, 5, 19, 12, 0, 0, 0, time.UTC) },
	})

	err := handler.HandleAdminChatMessage(context.Background(), adminMessage("/finish_vote"))
	if !errors.Is(err, wantErr) {
		t.Fatalf("HandleAdminChatMessage() error = %v, want %v", err, wantErr)
	}
	finishCalls := challenges.FinishVotingNowCalls()
	if len(finishCalls) != 1 || finishCalls[0].N != 42 {
		t.Fatalf("FinishVotingNow calls = %#v, want challenge 42", finishCalls)
	}
	sendCalls := publisher.SendTextCalls()
	if len(sendCalls) != 1 || sendCalls[0].S != finishVotePublishFailedMessage {
		t.Fatalf("SendText calls = %#v, want publish failure message", sendCalls)
	}
}

func TestFinishVoteHandlerReportsTopicFailureAndStillPublishesResults(t *testing.T) {
	wantErr := errors.New("topics failed")
	challenge := repository.Challenge{
		ID:         42,
		MainChatID: -1001,
		State:      repository.ChallengeStateVoting,
	}
	challenges := &MoqFinishVoteChallenges{
		FindOpenByMainChatIDFunc: func(context.Context, int64) (*repository.Challenge, error) {
			return &challenge, nil
		},
		FinishVotingNowFunc: func(context.Context, int64, time.Time) (bool, error) {
			return true, nil
		},
	}
	publisher := &MoqFinishVotePublisher{}
	results := &MoqFinishVoteResults{}
	topics := &MoqFinishVoteTopics{PublishOneFunc: func(context.Context, repository.Challenge) error {
		return wantErr
	}}
	handler := NewFinishVoteHandler(FinishVoteConfig{
		AdminChatID: -2002,
		MainChatID:  -1001,
		Challenges:  challenges,
		Publisher:   publisher,
		Results:     results,
		Topics:      topics,
		BotUsername: func() string { return "PhotoChallengeBot" },
		Now:         func() time.Time { return time.Date(2026, 5, 19, 12, 0, 0, 0, time.UTC) },
	})

	err := handler.HandleAdminChatMessage(context.Background(), adminMessage("/finish_vote"))
	if !errors.Is(err, wantErr) {
		t.Fatalf("HandleAdminChatMessage() error = %v, want %v", err, wantErr)
	}
	publishCalls := results.PublishOneCalls()
	if len(publishCalls) != 1 || publishCalls[0].N != 42 {
		t.Fatalf("PublishOne calls = %#v, want challenge 42", publishCalls)
	}
	sendCalls := publisher.SendTextCalls()
	if len(sendCalls) != 2 ||
		sendCalls[0].S != finishVoteTopicsPublishFailedMessage ||
		sendCalls[1].S != finishVoteDoneMessage {
		t.Fatalf("SendText calls = %#v, want topic failure then done message", sendCalls)
	}
}
