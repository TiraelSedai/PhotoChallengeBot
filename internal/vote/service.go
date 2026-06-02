package vote

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math/rand"
	"strconv"
	"strings"
	"time"

	"github.com/TiraelSedai/PhotoChallengeBot/internal/repository"
	"github.com/TiraelSedai/PhotoChallengeBot/internal/require"
	"github.com/go-telegram/bot/models"
)

const (
	invalidVoteLinkMessage = "Неверная ссылка"
	noPhotosMessage        = "Фотографий с таким тегом нет"
	inactiveVoteMessage    = "Эта голосовалка неактивна"
	notPrivateVoteMessage  = "Голосовать можно только в приватном чате"
	selfVoteMessage        = "Голосовать можно только за фото других участников!"
	ownPhotoMessage        = "Это твоё фото, дурачок"
	voteAcceptedMessage    = "Голос принят. Спасибо, пирожочек!"
)

var (
	ErrInvalidToken    = errors.New("invalid vote token")
	ErrVotingInactive  = errors.New("voting is inactive")
	ErrNoPhotos        = errors.New("no photos")
	ErrMissingCallback = errors.New("callback message is missing")
)

type challengeStore interface {
	Get(context.Context, int64) (repository.Challenge, error)
}

type userStore interface {
	Upsert(context.Context, repository.User) (repository.User, error)
}

type photoStore interface {
	ListByChallenge(context.Context, int64) ([]repository.Photo, error)
}

type voteStore interface {
	CreateVoteOrder(context.Context, int64, int64, []int64) ([]repository.VoteOrderItem, error)
	ListVoteOrder(context.Context, int64, int64) ([]repository.VoteOrderItem, error)
	GetProgress(context.Context, int64, int64) (*repository.VoteProgress, error)
	UpsertProgress(context.Context, repository.VoteProgress) (repository.VoteProgress, error)
	EnsureSelfVotes(context.Context, int64, []repository.Photo, time.Time) error
	ManualVoteExists(context.Context, int64, int64, int64) (bool, error)
	AddManualVote(context.Context, int64, int64, int64, time.Time) (repository.Vote, error)
	RemoveManualVote(context.Context, int64, int64, int64) (bool, error)
}

type publisher interface {
	SendPhoto(context.Context, int64, string, string, *models.InlineKeyboardMarkup) (int, error)
	EditPhoto(context.Context, int64, int, string, string, *models.InlineKeyboardMarkup) error
	SendText(context.Context, int64, string) (int, error)
	AnswerCallback(context.Context, string, string) error
}

type Config struct {
	Challenges challengeStore
	Users      userStore
	Photos     photoStore
	Votes      voteStore
	Publisher  publisher
	Now        func() time.Time
	Rand       *rand.Rand
}

type Service struct {
	challenges challengeStore
	users      userStore
	photos     photoStore
	votes      voteStore
	publisher  publisher
	now        func() time.Time
	rand       *rand.Rand
}

func NewService(cfg Config) *Service {
	require.NotNil("challenge repository", cfg.Challenges)
	require.NotNil("user repository", cfg.Users)
	require.NotNil("photo repository", cfg.Photos)
	require.NotNil("vote repository", cfg.Votes)
	require.NotNil("vote publisher", cfg.Publisher)
	require.NotNil("clock", cfg.Now)
	require.NotNil("rand", cfg.Rand)
	return &Service{
		challenges: cfg.Challenges,
		users:      cfg.Users,
		photos:     cfg.Photos,
		votes:      cfg.Votes,
		publisher:  cfg.Publisher,
		now:        cfg.Now,
		rand:       cfg.Rand,
	}
}

func (s *Service) HandlePrivateStart(ctx context.Context, message *models.Message, token string) error {
	if message == nil || message.From == nil {
		return nil
	}

	view, err := s.open(ctx, token, *message.From)
	if err != nil {
		return s.sendStartError(ctx, message.Chat.ID, err)
	}

	_, err = s.publisher.SendPhoto(ctx, message.Chat.ID, view.Photo.FileID, caption(view), keyboard(view))
	if err != nil {
		return fmt.Errorf("send vote photo: %w", err)
	}
	return nil
}

func (s *Service) HandleCallbackQuery(ctx context.Context, query *models.CallbackQuery) error {
	if query == nil {
		return nil
	}

	payload, ok := parseCallbackData(query.Data)
	if !ok {
		return nil
	}
	if payload.action == actionNoop {
		return s.publisher.AnswerCallback(ctx, query.ID, "")
	}
	if query.Message.Message == nil {
		return s.publisher.AnswerCallback(ctx, query.ID, inactiveVoteMessage)
	}
	message := query.Message.Message
	if message.Chat.Type != models.ChatTypePrivate || message.Chat.ID != query.From.ID {
		return s.publisher.AnswerCallback(ctx, query.ID, notPrivateVoteMessage)
	}

	result, err := s.applyCallback(ctx, payload.challengeID, payload.photoID, query.From.ID, payload.action)
	if err != nil {
		return s.answerCallbackError(ctx, query.ID, err)
	}
	if !result.changed {
		return s.publisher.AnswerCallback(ctx, query.ID, result.answer)
	}

	if err := s.publisher.EditPhoto(ctx, message.Chat.ID, message.ID, result.view.Photo.FileID, caption(result.view), keyboard(result.view)); err != nil {
		if answerErr := s.publisher.AnswerCallback(ctx, query.ID, result.answer); answerErr != nil {
			return fmt.Errorf("edit vote photo: %w; answer callback: %v", err, answerErr)
		}
		return fmt.Errorf("edit vote photo: %w", err)
	}
	return s.publisher.AnswerCallback(ctx, query.ID, result.answer)
}

func (s *Service) open(ctx context.Context, token string, user models.User) (View, error) {
	mainChatID, challengeID, err := ParseToken(token)
	if err != nil {
		return View{}, err
	}
	challenge, err := s.activeChallenge(ctx, mainChatID, challengeID)
	if err != nil {
		return View{}, err
	}

	voter, err := s.users.Upsert(ctx, repository.User{
		ID:        user.ID,
		Username:  user.Username,
		FirstName: user.FirstName,
		LastName:  user.LastName,
		UpdatedAt: s.now(),
	})
	if err != nil {
		return View{}, fmt.Errorf("upsert voting user: %w", err)
	}

	photos, err := s.photos.ListByChallenge(ctx, challenge.ID)
	if err != nil {
		return View{}, err
	}
	if len(photos) == 0 {
		return View{}, ErrNoPhotos
	}
	if err := s.votes.EnsureSelfVotes(ctx, challenge.ID, photos, s.now()); err != nil {
		return View{}, err
	}

	order, err := s.ensureOrder(ctx, challenge.ID, voter.ID, photos)
	if err != nil {
		return View{}, err
	}
	progress, err := s.ensureProgress(ctx, challenge.ID, voter.ID, 0)
	if err != nil {
		return View{}, err
	}
	return s.view(ctx, challenge.ID, voter.ID, order, photos, progress.CurrentPosition)
}

type callbackResult struct {
	view    View
	answer  string
	changed bool
}

func (s *Service) applyCallback(ctx context.Context, challengeID int64, clickedPhotoID int64, voterID int64, action string) (callbackResult, error) {
	challenge, err := s.challenges.Get(ctx, challengeID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return callbackResult{}, ErrVotingInactive
		}
		return callbackResult{}, err
	}
	if err := ensureVoting(challenge, s.now()); err != nil {
		return callbackResult{}, err
	}

	photos, err := s.photos.ListByChallenge(ctx, challenge.ID)
	if err != nil {
		return callbackResult{}, err
	}
	if len(photos) == 0 {
		return callbackResult{}, ErrNoPhotos
	}
	if err := s.votes.EnsureSelfVotes(ctx, challenge.ID, photos, s.now()); err != nil {
		return callbackResult{}, err
	}
	order, err := s.votes.ListVoteOrder(ctx, challenge.ID, voterID)
	if err != nil {
		return callbackResult{}, err
	}
	if len(order) == 0 {
		return callbackResult{}, ErrVotingInactive
	}

	position, liveTotal, err := clickedPosition(order, photos, clickedPhotoID)
	if err != nil {
		return callbackResult{}, err
	}
	previousPosition := position

	answer := ""
	changed := false
	switch action {
	case actionPrevious:
		if position > 0 {
			position--
			changed = position != previousPosition
		}
	case actionNext:
		if position < liveTotal-1 {
			position++
			changed = position != previousPosition
		}
	case actionToggle:
		current, err := s.view(ctx, challenge.ID, voterID, order, photos, position)
		if err != nil {
			return callbackResult{}, err
		}
		if current.Photo.AuthorUserID == voterID {
			return callbackResult{view: current, answer: selfVoteMessage}, nil
		}
		liked, err := s.votes.ManualVoteExists(ctx, challenge.ID, voterID, current.Photo.ID)
		if err != nil {
			return callbackResult{}, err
		}
		if liked {
			if _, err := s.votes.RemoveManualVote(ctx, challenge.ID, voterID, current.Photo.ID); err != nil {
				return callbackResult{}, err
			}
		} else if _, err := s.votes.AddManualVote(ctx, challenge.ID, voterID, current.Photo.ID, s.now()); err != nil {
			return callbackResult{}, err
		}
		answer = voteAcceptedMessage
		changed = true
	case actionOwnPhoto:
		current, err := s.view(ctx, challenge.ID, voterID, order, photos, position)
		if err != nil {
			return callbackResult{}, err
		}
		if !current.OwnPhoto {
			return callbackResult{}, ErrVotingInactive
		}
		return callbackResult{view: current, answer: ownPhotoMessage}, nil
	default:
		return callbackResult{}, ErrVotingInactive
	}

	if _, err := s.ensureProgress(ctx, challenge.ID, voterID, position); err != nil {
		return callbackResult{}, err
	}
	view, err := s.view(ctx, challenge.ID, voterID, order, photos, position)
	if err != nil {
		return callbackResult{}, err
	}
	return callbackResult{
		view:    view,
		answer:  answer,
		changed: changed,
	}, nil
}

func (s *Service) activeChallenge(ctx context.Context, mainChatID, challengeID int64) (repository.Challenge, error) {
	challenge, err := s.challenges.Get(ctx, challengeID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return repository.Challenge{}, ErrVotingInactive
		}
		return repository.Challenge{}, err
	}
	if challenge.MainChatID != mainChatID {
		return repository.Challenge{}, ErrInvalidToken
	}
	if err := ensureVoting(challenge, s.now()); err != nil {
		return repository.Challenge{}, err
	}
	return challenge, nil
}

func ensureVoting(challenge repository.Challenge, now time.Time) error {
	if challenge.State != repository.ChallengeStateVoting || challenge.VoteUntilAt == nil || !challenge.VoteUntilAt.After(now) {
		return ErrVotingInactive
	}
	return nil
}

func (s *Service) ensureOrder(ctx context.Context, challengeID, voterID int64, photos []repository.Photo) ([]repository.VoteOrderItem, error) {
	order, err := s.votes.ListVoteOrder(ctx, challengeID, voterID)
	if err != nil {
		return nil, err
	}
	if len(order) > 0 {
		return order, nil
	}

	photoIDs := make([]int64, 0, len(photos))
	for _, photo := range photos {
		photoIDs = append(photoIDs, photo.ID)
	}
	shuffle(photoIDs, s.rand)
	return s.votes.CreateVoteOrder(ctx, challengeID, voterID, photoIDs)
}

func (s *Service) ensureProgress(ctx context.Context, challengeID, voterID int64, position int) (repository.VoteProgress, error) {
	return s.votes.UpsertProgress(ctx, repository.VoteProgress{
		ChallengeID:     challengeID,
		VoterUserID:     voterID,
		CurrentPosition: position,
		UpdatedAt:       s.now(),
	})
}

func (s *Service) view(
	ctx context.Context,
	challengeID int64,
	voterID int64,
	order []repository.VoteOrderItem,
	photos []repository.Photo,
	position int,
) (View, error) {
	byID := make(map[int64]repository.Photo, len(photos))
	for _, photo := range photos {
		byID[photo.ID] = photo
	}

	currentOrder := liveOrder(order, byID)
	if len(currentOrder) == 0 {
		return View{}, ErrNoPhotos
	}
	position = clampPosition(position, len(currentOrder))
	photo := byID[currentOrder[position].PhotoID]
	liked, err := s.votes.ManualVoteExists(ctx, challengeID, voterID, photo.ID)
	if err != nil {
		return View{}, err
	}
	return View{
		ChallengeID: challengeID,
		Photo:       photo,
		Position:    position,
		Total:       len(currentOrder),
		Liked:       liked,
		OwnPhoto:    photo.AuthorUserID == voterID,
	}, nil
}

func liveOrder(order []repository.VoteOrderItem, photos map[int64]repository.Photo) []repository.VoteOrderItem {
	items := make([]repository.VoteOrderItem, 0, len(order))
	for _, item := range order {
		if _, ok := photos[item.PhotoID]; ok {
			items = append(items, item)
		}
	}
	return items
}

func clickedPosition(order []repository.VoteOrderItem, photos []repository.Photo, clickedPhotoID int64) (int, int, error) {
	byID := make(map[int64]repository.Photo, len(photos))
	for _, photo := range photos {
		byID[photo.ID] = photo
	}
	currentOrder := liveOrder(order, byID)
	for position, item := range currentOrder {
		if item.PhotoID == clickedPhotoID {
			return position, len(currentOrder), nil
		}
	}
	return 0, 0, ErrVotingInactive
}

func shuffle(values []int64, rnd *rand.Rand) {
	rnd.Shuffle(len(values), func(i, j int) {
		values[i], values[j] = values[j], values[i]
	})
}

func clampPosition(position, total int) int {
	if total <= 0 || position < 0 {
		return 0
	}
	if position >= total {
		return total - 1
	}
	return position
}

func ParseToken(token string) (int64, int64, error) {
	parts := strings.Split(token, "_")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return 0, 0, ErrInvalidToken
	}
	mainChatID, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return 0, 0, ErrInvalidToken
	}
	challengeID, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		return 0, 0, ErrInvalidToken
	}
	return mainChatID, challengeID, nil
}

type callbackPayload struct {
	challengeID int64
	photoID     int64
	action      string
}

func parseCallbackData(data string) (callbackPayload, bool) {
	parts := strings.Split(data, ":")
	if len(parts) != 4 || parts[0] != callbackPrefix {
		return callbackPayload{}, false
	}
	challengeID, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		return callbackPayload{}, false
	}
	photoID, err := strconv.ParseInt(parts[2], 10, 64)
	if err != nil || photoID == 0 {
		return callbackPayload{}, false
	}
	switch parts[3] {
	case actionPrevious, actionNext, actionToggle, actionNoop, actionOwnPhoto:
		return callbackPayload{challengeID: challengeID, photoID: photoID, action: parts[3]}, true
	default:
		return callbackPayload{}, false
	}
}

func (s *Service) sendStartError(ctx context.Context, chatID int64, err error) error {
	message := inactiveVoteMessage
	expected := true
	if errors.Is(err, ErrInvalidToken) {
		message = invalidVoteLinkMessage
	} else if errors.Is(err, ErrNoPhotos) {
		message = noPhotosMessage
	} else if !errors.Is(err, ErrVotingInactive) {
		expected = false
	}
	_, sendErr := s.publisher.SendText(ctx, chatID, message)
	if sendErr != nil {
		return fmt.Errorf("send vote start error: %w", sendErr)
	}
	if !expected {
		return err
	}
	return nil
}

func (s *Service) answerCallbackError(ctx context.Context, callbackID string, err error) error {
	message := inactiveVoteMessage
	expected := true
	if errors.Is(err, ErrNoPhotos) {
		message = noPhotosMessage
	} else if !errors.Is(err, ErrInvalidToken) && !errors.Is(err, ErrVotingInactive) {
		expected = false
	}
	if answerErr := s.publisher.AnswerCallback(ctx, callbackID, message); answerErr != nil {
		return answerErr
	}
	if !expected {
		return err
	}
	return nil
}
