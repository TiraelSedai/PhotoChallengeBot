package vote

import (
	"context"
	"errors"
	"math/rand"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/TiraelSedai/PhotoChallengeBot/internal/db"
	"github.com/TiraelSedai/PhotoChallengeBot/internal/repository"
	"github.com/go-telegram/bot/models"
	"github.com/jmoiron/sqlx"
)

func TestHandlePrivateStartCreatesAndReusesVoteOrder(t *testing.T) {
	database := openVoteTestDB(t)
	defer database.Close()
	challengeID := createVotingChallenge(t, database, 11, 12)
	publisher := newVotePublisherDeps(nil)
	service := newVoteTestService(database, publisher)

	message := privateStartMessage(20, "/start "+voteStartToken(t, database, challengeID))
	if err := service.HandlePrivateStart(context.Background(), message, voteStartToken(t, database, challengeID)); err != nil {
		t.Fatalf("HandlePrivateStart() error = %v", err)
	}
	if len(publisher.photos) != 1 {
		t.Fatalf("sent photos = %d, want 1", len(publisher.photos))
	}

	votes := repository.NewVotes(database)
	firstOrder, err := votes.ListVoteOrder(context.Background(), challengeID, 20)
	if err != nil {
		t.Fatalf("ListVoteOrder() error = %v", err)
	}
	if len(firstOrder) != 2 {
		t.Fatalf("first order length = %d, want 2", len(firstOrder))
	}

	if err := service.HandlePrivateStart(context.Background(), message, voteStartToken(t, database, challengeID)); err != nil {
		t.Fatalf("second HandlePrivateStart() error = %v", err)
	}
	secondOrder, err := votes.ListVoteOrder(context.Background(), challengeID, 20)
	if err != nil {
		t.Fatalf("second ListVoteOrder() error = %v", err)
	}
	if len(secondOrder) != len(firstOrder) || secondOrder[0].PhotoID != firstOrder[0].PhotoID || secondOrder[1].PhotoID != firstOrder[1].PhotoID {
		t.Fatalf("second order = %#v, want reused %#v", secondOrder, firstOrder)
	}
}

func TestNewServicePanicsOnNilRand(t *testing.T) {
	database := openVoteTestDB(t)
	defer database.Close()
	defer func() {
		if recover() == nil {
			t.Fatal("NewService() did not panic")
		}
	}()
	NewService(Config{
		Challenges: repository.NewChallenges(database),
		Users:      repository.NewUsers(database),
		Photos:     repository.NewPhotos(database),
		Votes:      repository.NewVotes(database),
		Publisher:  newVotePublisherDeps(nil).mock,
		Now:        func() time.Time { return testVoteTime(19 * 24 * time.Hour) },
	})
}

func TestHandlePrivateStartRejectsInvalidOrFinishedToken(t *testing.T) {
	database := openVoteTestDB(t)
	defer database.Close()
	challengeID := createVotingChallenge(t, database, 11)
	if _, err := repository.NewChallenges(database).FinishVoting(context.Background(), challengeID, testVoteTime(21*24*time.Hour)); err != nil {
		t.Fatalf("finish voting: %v", err)
	}
	publisher := newVotePublisherDeps(nil)
	service := newVoteTestService(database, publisher)

	if err := service.HandlePrivateStart(context.Background(), privateStartMessage(20, "/start bad"), "bad"); err != nil {
		t.Fatalf("invalid HandlePrivateStart() error = %v", err)
	}
	if err := service.HandlePrivateStart(context.Background(), privateStartMessage(20, "/start "+voteStartToken(t, database, challengeID)), voteStartToken(t, database, challengeID)); err != nil {
		t.Fatalf("finished HandlePrivateStart() error = %v", err)
	}

	if len(publisher.photos) != 0 {
		t.Fatalf("sent photos = %d, want 0", len(publisher.photos))
	}
	if got := publisher.texts; len(got) != 2 || got[0].text != invalidVoteLinkMessage || got[1].text != inactiveVoteMessage {
		t.Fatalf("texts = %#v, want invalid and inactive messages", got)
	}
}

func TestHandlePrivateStartAcceptsIDOnlyToken(t *testing.T) {
	database := openVoteTestDB(t)
	defer database.Close()
	challengeID := createVotingChallenge(t, database, 11)
	publisher := newVotePublisherDeps(nil)
	service := newVoteTestService(database, publisher)

	token := "-1001_" + itoa(challengeID)
	if err := service.HandlePrivateStart(context.Background(), privateStartMessage(20, "/start "+token), token); err != nil {
		t.Fatalf("HandlePrivateStart() error = %v", err)
	}
	if len(publisher.photos) != 1 {
		t.Fatalf("photos = %#v, want vote photo for routing token", publisher.photos)
	}
	if len(publisher.texts) != 0 {
		t.Fatalf("texts = %#v, want no error message", publisher.texts)
	}
}

func TestHandlePrivateStartReturnsUnexpectedBackendErrorAfterNotifyingUser(t *testing.T) {
	database := openVoteTestDB(t)
	challengeID := createVotingChallenge(t, database, 11)
	publisher := newVotePublisherDeps(nil)
	service := newVoteTestService(database, publisher)
	if err := database.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	token := voteStartToken(t, database, challengeID)
	err := service.HandlePrivateStart(context.Background(), privateStartMessage(20, "/start "+token), token)
	if err == nil {
		t.Fatal("HandlePrivateStart() error = nil, want backend error")
	}
	if len(publisher.texts) != 1 || publisher.texts[0].text != inactiveVoteMessage {
		t.Fatalf("texts = %#v, want inactive notification", publisher.texts)
	}
}

func TestHandleCallbackNavigatesAndTogglesManualVote(t *testing.T) {
	database := openVoteTestDB(t)
	defer database.Close()
	challengeID := createVotingChallenge(t, database, 11, 12)
	publisher := newVotePublisherDeps(nil)
	service := newVoteTestService(database, publisher)
	ctx := context.Background()

	if err := service.HandlePrivateStart(ctx, privateStartMessage(20, "/start "+voteStartToken(t, database, challengeID)), voteStartToken(t, database, challengeID)); err != nil {
		t.Fatalf("HandlePrivateStart() error = %v", err)
	}
	order, err := repository.NewVotes(database).ListVoteOrder(ctx, challengeID, 20)
	if err != nil {
		t.Fatalf("ListVoteOrder() error = %v", err)
	}

	query := voteCallback(20, "cb-like", callbackData(challengeID, order[0].PhotoID, actionToggle))
	if err := service.HandleCallbackQuery(ctx, query); err != nil {
		t.Fatalf("like HandleCallbackQuery() error = %v", err)
	}
	if len(publisher.edits) != 1 {
		t.Fatalf("edits = %d, want 1", len(publisher.edits))
	}
	if got := publisher.answers[len(publisher.answers)-1].text; got != voteAcceptedMessage {
		t.Fatalf("answer = %q, want vote accepted", got)
	}

	progress, err := repository.NewVotes(database).GetProgress(ctx, challengeID, 20)
	if err != nil {
		t.Fatalf("GetProgress() error = %v", err)
	}
	if progress == nil || progress.CurrentPosition != 0 {
		t.Fatalf("progress after like = %#v, want position 0", progress)
	}

	query = voteCallback(20, "cb-next", callbackData(challengeID, order[0].PhotoID, actionNext))
	if err := service.HandleCallbackQuery(ctx, query); err != nil {
		t.Fatalf("next HandleCallbackQuery() error = %v", err)
	}
	progress, err = repository.NewVotes(database).GetProgress(ctx, challengeID, 20)
	if err != nil {
		t.Fatalf("GetProgress() after next error = %v", err)
	}
	if progress == nil || progress.CurrentPosition != 1 {
		t.Fatalf("progress after next = %#v, want position 1", progress)
	}
}

func TestHandleCallbackReturnsUnexpectedBackendErrorAfterAnswering(t *testing.T) {
	database := openVoteTestDB(t)
	challengeID := createVotingChallenge(t, database, 11)
	publisher := newVotePublisherDeps(nil)
	service := newVoteTestService(database, publisher)
	if err := database.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	err := service.HandleCallbackQuery(context.Background(), voteCallback(20, "cb-next", callbackData(challengeID, 1, actionNext)))
	if err == nil {
		t.Fatal("HandleCallbackQuery() error = nil, want backend error")
	}
	if len(publisher.answers) != 1 || publisher.answers[0].text != inactiveVoteMessage {
		t.Fatalf("answers = %#v, want inactive callback answer", publisher.answers)
	}
}

func TestHandleCallbackLikesPhotoFromClickedMessageNotPersistedProgress(t *testing.T) {
	database := openVoteTestDB(t)
	defer database.Close()
	challengeID := createVotingChallenge(t, database, 11, 12)
	publisher := newVotePublisherDeps(nil)
	service := newVoteTestService(database, publisher)
	ctx := context.Background()

	if err := service.HandlePrivateStart(ctx, privateStartMessage(20, "/start "+voteStartToken(t, database, challengeID)), voteStartToken(t, database, challengeID)); err != nil {
		t.Fatalf("HandlePrivateStart() error = %v", err)
	}
	votes := repository.NewVotes(database)
	order, err := votes.ListVoteOrder(ctx, challengeID, 20)
	if err != nil {
		t.Fatalf("ListVoteOrder() error = %v", err)
	}
	if err := service.HandleCallbackQuery(ctx, voteCallback(20, "cb-next", callbackData(challengeID, order[0].PhotoID, actionNext))); err != nil {
		t.Fatalf("next HandleCallbackQuery() error = %v", err)
	}
	if err := service.HandleCallbackQuery(ctx, voteCallback(20, "cb-like-old", callbackData(challengeID, order[0].PhotoID, actionToggle))); err != nil {
		t.Fatalf("like old message HandleCallbackQuery() error = %v", err)
	}

	firstLiked, err := votes.ManualVoteExists(ctx, challengeID, 20, order[0].PhotoID)
	if err != nil {
		t.Fatalf("ManualVoteExists(first) error = %v", err)
	}
	secondLiked, err := votes.ManualVoteExists(ctx, challengeID, 20, order[1].PhotoID)
	if err != nil {
		t.Fatalf("ManualVoteExists(second) error = %v", err)
	}
	if !firstLiked || secondLiked {
		t.Fatalf("manual votes first=%v second=%v, want only clicked old-message photo liked", firstLiked, secondLiked)
	}
}

func TestHandleCallbackNavigatesLiveOrderAfterPhotoDeletion(t *testing.T) {
	database := openVoteTestDB(t)
	defer database.Close()
	challengeID := createVotingChallenge(t, database, 11, 12, 13)
	publisher := newVotePublisherDeps(nil)
	service := newVoteTestService(database, publisher)
	ctx := context.Background()

	if err := service.HandlePrivateStart(ctx, privateStartMessage(20, "/start "+voteStartToken(t, database, challengeID)), voteStartToken(t, database, challengeID)); err != nil {
		t.Fatalf("HandlePrivateStart() error = %v", err)
	}
	votes := repository.NewVotes(database)
	order, err := votes.ListVoteOrder(ctx, challengeID, 20)
	if err != nil {
		t.Fatalf("ListVoteOrder() error = %v", err)
	}
	if len(order) != 3 {
		t.Fatalf("order length = %d, want 3", len(order))
	}
	deletedPhoto, err := repository.NewPhotos(database).Get(ctx, order[1].PhotoID)
	if err != nil {
		t.Fatalf("Get deleted photo candidate error = %v", err)
	}
	if _, err := repository.NewPhotos(database).DeleteByAuthorID(ctx, challengeID, deletedPhoto.AuthorUserID); err != nil {
		t.Fatalf("DeleteByAuthorID() error = %v", err)
	}

	if err := service.HandleCallbackQuery(ctx, voteCallback(20, "cb-next", callbackData(challengeID, order[2].PhotoID, actionNext))); err != nil {
		t.Fatalf("next HandleCallbackQuery() error = %v", err)
	}
	progress, err := votes.GetProgress(ctx, challengeID, 20)
	if err != nil {
		t.Fatalf("GetProgress() error = %v", err)
	}
	if progress == nil || progress.CurrentPosition != 0 {
		t.Fatalf("progress = %#v, want wrapped first live position", progress)
	}
	if len(publisher.edits) == 0 || publisher.edits[len(publisher.edits)-1].fileID != "file-1" {
		t.Fatalf("last edit = %#v, want first live photo after wrap", publisher.edits)
	}
}

func TestHandleCallbackRejectsNonPrivateChat(t *testing.T) {
	database := openVoteTestDB(t)
	defer database.Close()
	challengeID := createVotingChallenge(t, database, 11, 12)
	publisher := newVotePublisherDeps(nil)
	service := newVoteTestService(database, publisher)
	ctx := context.Background()

	if err := service.HandlePrivateStart(ctx, privateStartMessage(20, "/start "+voteStartToken(t, database, challengeID)), voteStartToken(t, database, challengeID)); err != nil {
		t.Fatalf("HandlePrivateStart() error = %v", err)
	}
	order, err := repository.NewVotes(database).ListVoteOrder(ctx, challengeID, 20)
	if err != nil {
		t.Fatalf("ListVoteOrder() error = %v", err)
	}
	query := voteCallback(20, "cb-group", callbackData(challengeID, order[0].PhotoID, actionToggle))
	query.Message.Message.Chat = models.Chat{ID: -1001, Type: models.ChatTypeSupergroup}

	if err := service.HandleCallbackQuery(ctx, query); err != nil {
		t.Fatalf("HandleCallbackQuery() error = %v", err)
	}
	liked, err := repository.NewVotes(database).ManualVoteExists(ctx, challengeID, 20, order[0].PhotoID)
	if err != nil {
		t.Fatalf("ManualVoteExists() error = %v", err)
	}
	if liked {
		t.Fatal("manual vote exists after non-private callback, want rejected")
	}
	if len(publisher.edits) != 0 {
		t.Fatalf("edits = %#v, want no edits for non-private callback", publisher.edits)
	}
	if len(publisher.answers) != 1 || publisher.answers[0].text != notPrivateVoteMessage {
		t.Fatalf("answers = %#v, want non-private rejection", publisher.answers)
	}
}

func TestHandleCallbackKeepsManualVoteWhenEditFails(t *testing.T) {
	database := openVoteTestDB(t)
	defer database.Close()
	challengeID := createVotingChallenge(t, database, 11, 12)
	publisher := newVotePublisherDeps(errors.New("telegram edit failed"))
	service := newVoteTestService(database, publisher)
	ctx := context.Background()

	if err := service.HandlePrivateStart(ctx, privateStartMessage(20, "/start "+voteStartToken(t, database, challengeID)), voteStartToken(t, database, challengeID)); err != nil {
		t.Fatalf("HandlePrivateStart() error = %v", err)
	}
	votes := repository.NewVotes(database)
	order, err := votes.ListVoteOrder(ctx, challengeID, 20)
	if err != nil {
		t.Fatalf("ListVoteOrder() error = %v", err)
	}

	if err := service.HandleCallbackQuery(ctx, voteCallback(20, "cb-like", callbackData(challengeID, order[0].PhotoID, actionToggle))); err == nil {
		t.Fatal("HandleCallbackQuery() error = nil, want edit error")
	}
	liked, err := votes.ManualVoteExists(ctx, challengeID, 20, order[0].PhotoID)
	if err != nil {
		t.Fatalf("ManualVoteExists() error = %v", err)
	}
	if !liked {
		t.Fatal("manual vote does not exist after edit failure, want persisted vote")
	}
	if len(publisher.answers) != 1 || publisher.answers[0].text != voteAcceptedMessage {
		t.Fatalf("answers = %#v, want vote accepted acknowledgement", publisher.answers)
	}
}

func TestHandleCallbackKeepsProgressWhenEditFails(t *testing.T) {
	database := openVoteTestDB(t)
	defer database.Close()
	challengeID := createVotingChallenge(t, database, 11, 12)
	publisher := newVotePublisherDeps(errors.New("telegram edit failed"))
	service := newVoteTestService(database, publisher)
	ctx := context.Background()

	if err := service.HandlePrivateStart(ctx, privateStartMessage(20, "/start "+voteStartToken(t, database, challengeID)), voteStartToken(t, database, challengeID)); err != nil {
		t.Fatalf("HandlePrivateStart() error = %v", err)
	}
	votes := repository.NewVotes(database)
	order, err := votes.ListVoteOrder(ctx, challengeID, 20)
	if err != nil {
		t.Fatalf("ListVoteOrder() error = %v", err)
	}

	if err := service.HandleCallbackQuery(ctx, voteCallback(20, "cb-next", callbackData(challengeID, order[0].PhotoID, actionNext))); err == nil {
		t.Fatal("HandleCallbackQuery() error = nil, want edit error")
	}
	progress, err := votes.GetProgress(ctx, challengeID, 20)
	if err != nil {
		t.Fatalf("GetProgress() error = %v", err)
	}
	if progress == nil || progress.CurrentPosition != 1 {
		t.Fatalf("progress = %#v, want persisted next position", progress)
	}
	if len(publisher.answers) != 1 || publisher.answers[0].text != "" {
		t.Fatalf("answers = %#v, want navigation callback acknowledgement", publisher.answers)
	}
}

func TestHandleCallbackRejectsSelfManualVote(t *testing.T) {
	database := openVoteTestDB(t)
	defer database.Close()
	challengeID := createVotingChallenge(t, database, 11)
	publisher := newVotePublisherDeps(nil)
	service := newVoteTestService(database, publisher)
	ctx := context.Background()

	if err := service.HandlePrivateStart(ctx, privateStartMessage(11, "/start "+voteStartToken(t, database, challengeID)), voteStartToken(t, database, challengeID)); err != nil {
		t.Fatalf("HandlePrivateStart() error = %v", err)
	}
	order, err := repository.NewVotes(database).ListVoteOrder(ctx, challengeID, 11)
	if err != nil {
		t.Fatalf("ListVoteOrder() error = %v", err)
	}
	if err := service.HandleCallbackQuery(ctx, voteCallback(11, "cb-self", callbackData(challengeID, order[0].PhotoID, actionToggle))); err != nil {
		t.Fatalf("self like HandleCallbackQuery() error = %v", err)
	}
	if got := publisher.answers[len(publisher.answers)-1].text; got != selfVoteMessage {
		t.Fatalf("answer = %q, want self vote message", got)
	}

	votes, err := repository.NewVotes(database).ListVotes(ctx, challengeID)
	if err != nil {
		t.Fatalf("ListVotes() error = %v", err)
	}
	for _, vote := range votes {
		if vote.Kind == repository.VoteKindManual {
			t.Fatalf("manual self vote stored: %#v", vote)
		}
	}
}

func TestHandleCallbackAnswersSelfVoteWithoutEditingUnchangedMessage(t *testing.T) {
	database := openVoteTestDB(t)
	defer database.Close()
	challengeID := createVotingChallenge(t, database, 11)
	publisher := newVotePublisherDeps(errors.New("message is not modified"))
	service := newVoteTestService(database, publisher)
	ctx := context.Background()

	if err := service.HandlePrivateStart(ctx, privateStartMessage(11, "/start "+voteStartToken(t, database, challengeID)), voteStartToken(t, database, challengeID)); err != nil {
		t.Fatalf("HandlePrivateStart() error = %v", err)
	}
	order, err := repository.NewVotes(database).ListVoteOrder(ctx, challengeID, 11)
	if err != nil {
		t.Fatalf("ListVoteOrder() error = %v", err)
	}
	if err := service.HandleCallbackQuery(ctx, voteCallback(11, "cb-self", callbackData(challengeID, order[0].PhotoID, actionToggle))); err != nil {
		t.Fatalf("self like HandleCallbackQuery() error = %v", err)
	}

	if len(publisher.edits) != 0 {
		t.Fatalf("edits = %d, want 0 for unchanged self-vote view", len(publisher.edits))
	}
	if got := publisher.answers[len(publisher.answers)-1].text; got != selfVoteMessage {
		t.Fatalf("answer = %q, want self vote message", got)
	}
}

func openVoteTestDB(t *testing.T) *sqlx.DB {
	t.Helper()

	database, err := db.Open(context.Background(), db.Options{
		Path:          filepath.Join(t.TempDir(), "bot.sqlite"),
		MigrationsDir: "../../migrations",
	})
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	return database
}

func createVotingChallenge(t *testing.T, database *sqlx.DB, authorIDs ...int64) int64 {
	t.Helper()
	ctx := context.Background()
	users := repository.NewUsers(database)
	if _, err := users.Upsert(ctx, repository.User{ID: 10, FirstName: "Admin"}); err != nil {
		t.Fatalf("upsert admin: %v", err)
	}
	for _, authorID := range authorIDs {
		if _, err := users.Upsert(ctx, repository.User{ID: authorID, FirstName: "Author"}); err != nil {
			t.Fatalf("upsert author %d: %v", authorID, err)
		}
	}

	challenge, err := repository.NewChallenges(database).Create(ctx, repository.CreateChallengeInput{
		MainChatID:      -1001,
		Num:             1,
		Theme:           "Night",
		Hashtag:         "#night",
		AcceptStartAt:   testVoteTime(0),
		AcceptUntilAt:   testVoteTime(17 * 24 * time.Hour),
		ReminderAt:      testVoteTime(17*24*time.Hour - 30*time.Hour),
		CreatedByUserID: 10,
		CreatedAt:       testVoteTime(0),
	})
	if err != nil {
		t.Fatalf("create challenge: %v", err)
	}
	for idx, authorID := range authorIDs {
		if _, _, err := repository.NewPhotos(database).UpsertCurrent(ctx, repository.UpsertPhotoInput{
			ChallengeID:     challenge.ID,
			AuthorUserID:    authorID,
			FileID:          "file-" + itoa(int64(idx+1)),
			FileUniqueID:    "unique-" + itoa(int64(idx+1)),
			SourceChatID:    -1001,
			SourceMessageID: 100 + idx,
			Caption:         "#night",
			SubmittedAt:     testVoteTime(time.Duration(idx) * time.Hour),
		}); err != nil {
			t.Fatalf("upsert photo %d: %v", idx, err)
		}
	}
	if _, err := repository.NewChallenges(database).StartVoting(ctx, challenge.ID, testVoteTime(18*24*time.Hour)); err != nil {
		t.Fatalf("start voting: %v", err)
	}
	return challenge.ID
}

func voteStartToken(t *testing.T, database *sqlx.DB, challengeID int64) string {
	t.Helper()
	return "-1001_" + itoa(challengeID)
}

func newVoteTestService(database *sqlx.DB, publisher *votePublisherDeps) *Service {
	return NewService(Config{
		Challenges: repository.NewChallenges(database),
		Users:      repository.NewUsers(database),
		Photos:     repository.NewPhotos(database),
		Votes:      repository.NewVotes(database),
		Publisher:  publisher.mock,
		Now:        func() time.Time { return testVoteTime(19 * 24 * time.Hour) },
		Rand:       rand.New(rand.NewSource(1)),
	})
}

func privateStartMessage(userID int64, text string) *models.Message {
	return &models.Message{
		ID:   1,
		From: &models.User{ID: userID, FirstName: "Voter"},
		Chat: models.Chat{ID: userID, Type: models.ChatTypePrivate},
		Text: text,
	}
}

func voteCallback(userID int64, id string, data string) *models.CallbackQuery {
	return &models.CallbackQuery{
		ID:   id,
		From: models.User{ID: userID, FirstName: "Voter"},
		Data: data,
		Message: models.MaybeInaccessibleMessage{
			Message: &models.Message{
				ID:   1000,
				Chat: models.Chat{ID: userID, Type: models.ChatTypePrivate},
			},
		},
	}
}

type votePublisherDeps struct {
	mock    *MoqPublisher
	photos  []photoCall
	edits   []photoCall
	texts   []textCall
	answers []answerCall
	editErr error
}

type photoCall struct {
	chatID    int64
	messageID int
	fileID    string
	caption   string
	markup    *models.InlineKeyboardMarkup
}

type textCall struct {
	chatID int64
	text   string
}

type answerCall struct {
	id   string
	text string
}

func newVotePublisherDeps(editErr error) *votePublisherDeps {
	deps := &votePublisherDeps{editErr: editErr}
	deps.mock = &MoqPublisher{
		SendPhotoFunc: func(_ context.Context, chatID int64, fileID string, caption string, markup *models.InlineKeyboardMarkup) (int, error) {
			deps.photos = append(deps.photos, photoCall{chatID: chatID, messageID: len(deps.photos) + 1, fileID: fileID, caption: caption, markup: markup})
			return len(deps.photos), nil
		},
		EditPhotoFunc: func(_ context.Context, chatID int64, messageID int, fileID string, caption string, markup *models.InlineKeyboardMarkup) error {
			if deps.editErr != nil {
				return deps.editErr
			}
			deps.edits = append(deps.edits, photoCall{chatID: chatID, messageID: messageID, fileID: fileID, caption: caption, markup: markup})
			return nil
		},
		SendTextFunc: func(_ context.Context, chatID int64, text string) (int, error) {
			deps.texts = append(deps.texts, textCall{chatID: chatID, text: text})
			return len(deps.texts), nil
		},
		AnswerCallbackFunc: func(_ context.Context, id string, text string) error {
			deps.answers = append(deps.answers, answerCall{id: id, text: text})
			return nil
		},
	}
	return deps
}

func itoa(value int64) string {
	return strconv.FormatInt(value, 10)
}

func testVoteTime(offset time.Duration) time.Time {
	return time.Date(2026, 5, 18, 12, 0, 0, 0, time.UTC).Add(offset)
}
