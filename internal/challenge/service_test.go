package challenge

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/TiraelSedai/PhotoChallengeBot/internal/repository"
)

func TestCreateActiveUsesDefaultDatesInMoscow(t *testing.T) {
	t.Parallel()

	location := mustLoadLocation(t, "Europe/Moscow")
	store := &recordingStore{nextNum: 7}
	service := NewService(store, location)
	service.now = func() time.Time {
		return time.Date(2026, 5, 1, 11, 30, 0, 0, location)
	}

	challenge, err := service.CreateActive(context.Background(), CreateInput{
		MainChatID:      -1001,
		Theme:           "Ночь",
		Hashtag:         "#night",
		CreatedByUserID: 10,
	})
	if err != nil {
		t.Fatalf("CreateActive() error = %v", err)
	}

	wantStart := time.Date(2026, 5, 1, 0, 0, 0, 0, location)
	wantUntil := time.Date(2026, 5, 18, 18, 0, 0, 0, location)
	wantReminder := time.Date(2026, 5, 17, 12, 0, 0, 0, location)

	if !challenge.AcceptStartAt.Equal(wantStart) {
		t.Fatalf("AcceptStartAt = %s, want %s", challenge.AcceptStartAt, wantStart)
	}
	if !challenge.AcceptUntilAt.Equal(wantUntil) {
		t.Fatalf("AcceptUntilAt = %s, want %s", challenge.AcceptUntilAt, wantUntil)
	}
	if !challenge.ReminderAt.Equal(wantReminder) {
		t.Fatalf("ReminderAt = %s, want %s", challenge.ReminderAt, wantReminder)
	}
	if challenge.Num != 7 {
		t.Fatalf("Num = %d, want 7", challenge.Num)
	}
	if challenge.State != repository.ChallengeStateActive {
		t.Fatalf("State = %q, want active", challenge.State)
	}
}

func TestPlanDoesNotCreateChallenge(t *testing.T) {
	t.Parallel()

	location := mustLoadLocation(t, "Europe/Moscow")
	store := &recordingStore{nextNum: 3}
	service := NewService(store, location)
	service.now = func() time.Time {
		return time.Date(2026, 5, 1, 11, 30, 0, 0, location)
	}

	plan, err := service.Plan(context.Background(), CreateInput{
		MainChatID:      -1001,
		Theme:           "Ночь",
		Hashtag:         "#night",
		CreatedByUserID: 10,
	})
	if err != nil {
		t.Fatalf("Plan() error = %v", err)
	}
	if store.created.MainChatID != 0 {
		t.Fatalf("Plan() created challenge: %#v", store.created)
	}
	if plan.Num != 3 {
		t.Fatalf("Num = %d, want 3", plan.Num)
	}
}

func TestCreateActiveUsesCustomDates(t *testing.T) {
	t.Parallel()

	location := mustLoadLocation(t, "Europe/Moscow")
	start := time.Date(2026, 6, 3, 15, 0, 0, 0, location)
	end := time.Date(2026, 6, 10, 9, 0, 0, 0, location)
	store := &recordingStore{nextNum: 1}
	service := NewService(store, location)
	service.now = func() time.Time {
		return time.Date(2026, 5, 18, 12, 0, 0, 0, location)
	}

	challenge, err := service.CreateActive(context.Background(), CreateInput{
		MainChatID:      -1001,
		Theme:           "Water",
		Hashtag:         "#water",
		CreatedByUserID: 10,
		StartDate:       &start,
		EndDate:         &end,
	})
	if err != nil {
		t.Fatalf("CreateActive() error = %v", err)
	}

	wantStart := time.Date(2026, 6, 3, 0, 0, 0, 0, location)
	wantUntil := time.Date(2026, 6, 10, 18, 0, 0, 0, location)
	if !challenge.AcceptStartAt.Equal(wantStart) {
		t.Fatalf("AcceptStartAt = %s, want %s", challenge.AcceptStartAt, wantStart)
	}
	if !challenge.AcceptUntilAt.Equal(wantUntil) {
		t.Fatalf("AcceptUntilAt = %s, want %s", challenge.AcceptUntilAt, wantUntil)
	}
}

func TestCreateActiveRejectsExistingOpenChallenge(t *testing.T) {
	t.Parallel()

	store := &recordingStore{
		open: &repository.Challenge{ID: 42, State: repository.ChallengeStateVoting},
	}
	service := NewService(store, time.UTC)

	_, err := service.CreateActive(context.Background(), CreateInput{
		MainChatID:      -1001,
		Theme:           "Night",
		Hashtag:         "#night",
		CreatedByUserID: 10,
	})
	if err == nil || !strings.Contains(err.Error(), "open challenge already exists") {
		t.Fatalf("CreateActive() error = %v, want open challenge error", err)
	}
}

func TestCreateActiveRejectsEndDateBeforeStartDate(t *testing.T) {
	t.Parallel()

	location := mustLoadLocation(t, "Europe/Moscow")
	start := time.Date(2026, 6, 10, 0, 0, 0, 0, location)
	end := time.Date(2026, 6, 9, 0, 0, 0, 0, location)
	store := &recordingStore{nextNum: 1}
	service := NewService(store, location)

	_, err := service.CreateActive(context.Background(), CreateInput{
		MainChatID:      -1001,
		Theme:           "Night",
		Hashtag:         "#night",
		CreatedByUserID: 10,
		StartDate:       &start,
		EndDate:         &end,
	})
	if err == nil || !strings.Contains(err.Error(), "end date") {
		t.Fatalf("CreateActive() error = %v, want end date validation error", err)
	}
}

type recordingStore struct {
	open    *repository.Challenge
	nextNum int
	created repository.CreateChallengeInput
}

func (s *recordingStore) Create(_ context.Context, input repository.CreateChallengeInput) (repository.Challenge, error) {
	s.created = input
	return repository.Challenge{
		ID:              1,
		MainChatID:      input.MainChatID,
		Num:             input.Num,
		Theme:           input.Theme,
		Hashtag:         input.Hashtag,
		State:           input.State,
		AcceptStartAt:   input.AcceptStartAt,
		AcceptUntilAt:   input.AcceptUntilAt,
		ReminderAt:      input.ReminderAt,
		CreatedByUserID: input.CreatedByUserID,
		CreatedAt:       input.CreatedAt,
		UpdatedAt:       input.CreatedAt,
	}, nil
}

func (s *recordingStore) FindOpenByMainChatID(context.Context, int64) (*repository.Challenge, error) {
	return s.open, nil
}

func (s *recordingStore) NextNum(context.Context, int64) (int, error) {
	return s.nextNum, nil
}

func mustLoadLocation(t *testing.T, name string) *time.Location {
	t.Helper()
	location, err := time.LoadLocation(name)
	if err != nil {
		t.Fatalf("LoadLocation(%q): %v", name, err)
	}
	return location
}
