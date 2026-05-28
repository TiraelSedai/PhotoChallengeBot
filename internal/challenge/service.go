package challenge

import (
	"context"
	"fmt"
	"time"

	"github.com/TiraelSedai/PhotoChallengeBot/internal/repository"
	"github.com/TiraelSedai/PhotoChallengeBot/internal/require"
)

const (
	defaultDurationDays = 17
	acceptCloseHour     = 18
	reminderBefore      = 30 * time.Hour
)

type store interface {
	Create(context.Context, repository.CreateChallengeInput) (repository.Challenge, error)
	FindOpenByMainChatID(context.Context, int64) (*repository.Challenge, error)
	NextNum(context.Context, int64) (int, error)
}

type Service struct {
	store    store
	location *time.Location
	now      func() time.Time
}

type CreateInput struct {
	MainChatID      int64
	Theme           string
	Hashtag         string
	CreatedByUserID int64
	StartDate       *time.Time
	EndDate         *time.Time
}

type Plan struct {
	Input         CreateInput
	Num           int
	AcceptStartAt time.Time
	AcceptUntilAt time.Time
	ReminderAt    time.Time
	CreatedAt     time.Time
}

func NewService(store store, location *time.Location, now func() time.Time) *Service {
	require.NotNil("challenge store", store)
	require.NotNil("location", location)
	require.NotNil("clock", now)
	return &Service{
		store:    store,
		location: location,
		now:      now,
	}
}

func (s *Service) CreateActive(ctx context.Context, input CreateInput) (repository.Challenge, error) {
	plan, err := s.Plan(ctx, input)
	if err != nil {
		return repository.Challenge{}, err
	}

	return s.CreatePlanned(ctx, plan)
}

func (s *Service) CreatePlanned(ctx context.Context, plan Plan) (repository.Challenge, error) {
	if plan.Num == 0 {
		return repository.Challenge{}, fmt.Errorf("challenge num is required")
	}
	if plan.Input.MainChatID == 0 {
		return repository.Challenge{}, fmt.Errorf("main chat id is required")
	}
	if plan.Input.CreatedByUserID == 0 {
		return repository.Challenge{}, fmt.Errorf("created by user id is required")
	}
	if plan.Input.Theme == "" {
		return repository.Challenge{}, fmt.Errorf("theme is required")
	}
	if plan.Input.Hashtag == "" {
		return repository.Challenge{}, fmt.Errorf("hashtag is required")
	}
	if plan.AcceptStartAt.IsZero() || plan.AcceptUntilAt.IsZero() || plan.ReminderAt.IsZero() {
		return repository.Challenge{}, fmt.Errorf("planned challenge dates are required")
	}
	if plan.CreatedAt.IsZero() {
		plan.CreatedAt = s.now().In(s.location)
	}

	return s.store.Create(ctx, repository.CreateChallengeInput{
		MainChatID:      plan.Input.MainChatID,
		Num:             plan.Num,
		Theme:           plan.Input.Theme,
		Hashtag:         plan.Input.Hashtag,
		State:           repository.ChallengeStateActive,
		AcceptStartAt:   plan.AcceptStartAt,
		AcceptUntilAt:   plan.AcceptUntilAt,
		ReminderAt:      plan.ReminderAt,
		CreatedByUserID: plan.Input.CreatedByUserID,
		CreatedAt:       plan.CreatedAt,
	})
}

func (s *Service) Plan(ctx context.Context, input CreateInput) (Plan, error) {
	if input.MainChatID == 0 {
		return Plan{}, fmt.Errorf("main chat id is required")
	}
	if input.CreatedByUserID == 0 {
		return Plan{}, fmt.Errorf("created by user id is required")
	}
	if input.Theme == "" {
		return Plan{}, fmt.Errorf("theme is required")
	}
	if input.Hashtag == "" {
		return Plan{}, fmt.Errorf("hashtag is required")
	}

	open, err := s.store.FindOpenByMainChatID(ctx, input.MainChatID)
	if err != nil {
		return Plan{}, err
	}
	if open != nil {
		return Plan{}, fmt.Errorf("open challenge already exists")
	}

	startDate := s.localDate(input.StartDate)
	endDate := startDate.AddDate(0, 0, defaultDurationDays)
	if input.EndDate != nil {
		endDate = dateOnly((*input.EndDate).In(s.location), s.location)
	}
	if endDate.Before(startDate) {
		return Plan{}, fmt.Errorf("end date must be on or after start date")
	}

	acceptUntilAt := time.Date(endDate.Year(), endDate.Month(), endDate.Day(), acceptCloseHour, 0, 0, 0, s.location)
	createdAt := s.now().In(s.location)

	num, err := s.store.NextNum(ctx, input.MainChatID)
	if err != nil {
		return Plan{}, err
	}

	return Plan{
		Input:         input,
		Num:           num,
		AcceptStartAt: startDate,
		AcceptUntilAt: acceptUntilAt,
		ReminderAt:    acceptUntilAt.Add(-reminderBefore),
		CreatedAt:     createdAt,
	}, nil
}

func (s *Service) localDate(value *time.Time) time.Time {
	if value == nil {
		now := s.now().In(s.location)
		return dateOnly(now, s.location)
	}
	return dateOnly((*value).In(s.location), s.location)
}

func dateOnly(value time.Time, location *time.Location) time.Time {
	return time.Date(value.Year(), value.Month(), value.Day(), 0, 0, 0, 0, location)
}
