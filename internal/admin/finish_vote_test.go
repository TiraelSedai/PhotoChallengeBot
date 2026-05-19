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
	challenges := &finishVoteChallenges{open: &challenge, finishResult: true}
	publisher := &finishVotePublisher{}
	results := &finishVoteResults{}
	handler := NewFinishVoteHandler(FinishVoteConfig{
		AdminChatID: -2002,
		MainChatID:  -1001,
		Challenges:  challenges,
		Publisher:   publisher,
		Results:     results,
		BotUsername: func() string { return "PhotoChallengeBot" },
		Now:         func() time.Time { return time.Date(2026, 5, 19, 12, 0, 0, 0, time.UTC) },
	})

	if err := handler.HandleAdminChatMessage(context.Background(), adminMessage("/finish_vote")); err != nil {
		t.Fatalf("HandleAdminChatMessage() error = %v", err)
	}

	if challenges.finishedID != 42 {
		t.Fatalf("finishedID = %d, want 42", challenges.finishedID)
	}
	if results.challengeID != 42 {
		t.Fatalf("published challengeID = %d, want 42", results.challengeID)
	}
	if got := publisher.sent[0]; got != finishVoteDoneMessage {
		t.Fatalf("sent = %q, want done message", got)
	}
}

func TestFinishVoteHandlerReportsAbsentVoting(t *testing.T) {
	challenges := &finishVoteChallenges{open: &repository.Challenge{ID: 42, State: repository.ChallengeStateActive}}
	publisher := &finishVotePublisher{}
	results := &finishVoteResults{}
	handler := NewFinishVoteHandler(FinishVoteConfig{
		AdminChatID: -2002,
		MainChatID:  -1001,
		Challenges:  challenges,
		Publisher:   publisher,
		Results:     results,
	})

	if err := handler.HandleAdminChatMessage(context.Background(), adminMessage("/finish_vote")); err != nil {
		t.Fatalf("HandleAdminChatMessage() error = %v", err)
	}
	if challenges.finishedID != 0 {
		t.Fatalf("finishedID = %d, want 0", challenges.finishedID)
	}
	if results.challengeID != 0 {
		t.Fatalf("published challengeID = %d, want 0", results.challengeID)
	}
	if got := publisher.sent[0]; got != finishVoteAbsentMessage {
		t.Fatalf("sent = %q, want absent message", got)
	}
}

func TestFinishVoteHandlerDoesNotPublishWhenFinishDoesNotChangeRow(t *testing.T) {
	challenge := repository.Challenge{
		ID:         42,
		MainChatID: -1001,
		State:      repository.ChallengeStateVoting,
	}
	challenges := &finishVoteChallenges{open: &challenge, finishResult: false}
	publisher := &finishVotePublisher{}
	results := &finishVoteResults{}
	handler := NewFinishVoteHandler(FinishVoteConfig{
		AdminChatID: -2002,
		MainChatID:  -1001,
		Challenges:  challenges,
		Publisher:   publisher,
		Results:     results,
	})

	if err := handler.HandleAdminChatMessage(context.Background(), adminMessage("/finish_vote")); err != nil {
		t.Fatalf("HandleAdminChatMessage() error = %v", err)
	}
	if results.challengeID != 0 {
		t.Fatalf("published challengeID = %d, want 0", results.challengeID)
	}
	if got := publisher.sent[0]; got != finishVoteAbsentMessage {
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
	challenges := &finishVoteChallenges{open: &challenge, finishResult: true}
	publisher := &finishVotePublisher{}
	results := &finishVoteResults{err: wantErr}
	handler := NewFinishVoteHandler(FinishVoteConfig{
		AdminChatID: -2002,
		MainChatID:  -1001,
		Challenges:  challenges,
		Publisher:   publisher,
		Results:     results,
	})

	err := handler.HandleAdminChatMessage(context.Background(), adminMessage("/finish_vote"))
	if !errors.Is(err, wantErr) {
		t.Fatalf("HandleAdminChatMessage() error = %v, want %v", err, wantErr)
	}
	if challenges.finishedID != 42 {
		t.Fatalf("finishedID = %d, want 42", challenges.finishedID)
	}
	if len(publisher.sent) != 1 || publisher.sent[0] != finishVotePublishFailedMessage {
		t.Fatalf("sent = %#v, want publish failure message", publisher.sent)
	}
}

type finishVoteChallenges struct {
	open         *repository.Challenge
	finishedID   int64
	finishResult bool
}

func (s *finishVoteChallenges) FindOpenByMainChatID(context.Context, int64) (*repository.Challenge, error) {
	return s.open, nil
}

func (s *finishVoteChallenges) FinishVotingNow(_ context.Context, id int64, _ time.Time) (bool, error) {
	s.finishedID = id
	if !s.finishResult {
		return false, nil
	}
	return true, nil
}

type finishVotePublisher struct {
	sent []string
}

func (p *finishVotePublisher) SendText(_ context.Context, _ int64, text string) (int, error) {
	p.sent = append(p.sent, text)
	return len(p.sent), nil
}

type finishVoteResults struct {
	challengeID int64
	err         error
}

func (p *finishVoteResults) PublishOne(_ context.Context, challengeID int64) error {
	p.challengeID = challengeID
	return p.err
}
