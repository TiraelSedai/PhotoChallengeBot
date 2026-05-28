package admin

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/TiraelSedai/PhotoChallengeBot/internal/repository"
	"github.com/TiraelSedai/PhotoChallengeBot/internal/require"
	"github.com/go-telegram/bot/models"
)

type OpenChallengeFinder interface {
	FindOpenByMainChatID(context.Context, int64) (*repository.Challenge, error)
}

type PhotoDeleter interface {
	DeleteByAuthorID(context.Context, int64, int64) (*repository.Photo, error)
	DeleteByUsername(context.Context, int64, string) (*repository.Photo, error)
}

type DeletePhotoPublisher interface {
	SendText(context.Context, int64, string) (int, error)
}

type DeletePhotoHandler struct {
	adminChatID int64
	mainChatID  int64
	challenges  OpenChallengeFinder
	photos      PhotoDeleter
	publisher   DeletePhotoPublisher
	botUsername func() string
}

type DeletePhotoConfig struct {
	AdminChatID int64
	MainChatID  int64
	Challenges  OpenChallengeFinder
	Photos      PhotoDeleter
	Publisher   DeletePhotoPublisher
	BotUsername func() string
}

func NewDeletePhotoHandler(cfg DeletePhotoConfig) *DeletePhotoHandler {
	require.NotNil("open challenge finder", cfg.Challenges)
	require.NotNil("photo deleter", cfg.Photos)
	require.NotNil("delete photo publisher", cfg.Publisher)
	require.NotNil("bot username provider", cfg.BotUsername)
	switch {
	case cfg.AdminChatID == 0:
		panic("admin chat id is required")
	case cfg.MainChatID == 0:
		panic("main chat id is required")
	}
	return &DeletePhotoHandler{
		adminChatID: cfg.AdminChatID,
		mainChatID:  cfg.MainChatID,
		challenges:  cfg.Challenges,
		photos:      cfg.Photos,
		publisher:   cfg.Publisher,
		botUsername: cfg.BotUsername,
	}
}

func (h *DeletePhotoHandler) HandleAdminChatMessage(ctx context.Context, message *models.Message) error {
	if message == nil {
		return nil
	}
	if message.Chat.ID != h.adminChatID {
		return nil
	}

	text := strings.TrimSpace(message.Text)
	if text == "" {
		return nil
	}
	if isCommandMentionedToOtherBot(text, h.currentBotUsername()) {
		return nil
	}

	target, handled, valid := parseDeletePhotoCommand(text, h.currentBotUsername())
	if !handled {
		return nil
	}
	if !valid {
		_, err := h.publisher.SendText(ctx, h.adminChatID, "Не понял, кого удалить. Используй /delete_photo @username или /delete_photo 123456.")
		return err
	}

	challenge, err := h.challenges.FindOpenByMainChatID(ctx, h.mainChatID)
	if err != nil {
		return err
	}
	if challenge == nil {
		_, err := h.publisher.SendText(ctx, h.adminChatID, "Нет открытого челленджа для удаления фото.")
		return err
	}

	deleted, err := h.deletePhoto(ctx, challenge.ID, target)
	if errors.Is(err, repository.ErrAmbiguousUsername) {
		_, sendErr := h.publisher.SendText(ctx, h.adminChatID, "Нашел несколько участников с таким username. Удали фото по Telegram user ID.")
		return sendErr
	}
	if err != nil {
		return err
	}
	if deleted == nil {
		_, err := h.publisher.SendText(ctx, h.adminChatID, fmt.Sprintf("Фото участника %s в текущем челлендже не найдено.", target.label()))
		return err
	}

	_, err = h.publisher.SendText(ctx, h.adminChatID, fmt.Sprintf("Фото участника %s удалено из текущего челленджа.", target.label()))
	return err
}

func (h *DeletePhotoHandler) deletePhoto(ctx context.Context, challengeID int64, target deletePhotoTarget) (*repository.Photo, error) {
	if target.username != "" {
		return h.photos.DeleteByUsername(ctx, challengeID, target.username)
	}
	return h.photos.DeleteByAuthorID(ctx, challengeID, target.userID)
}

func (h *DeletePhotoHandler) currentBotUsername() string {
	return h.botUsername()
}

func (h *DeletePhotoHandler) Handles(text string) bool {
	if isCommandMentionedToOtherBot(text, h.currentBotUsername()) {
		return false
	}
	_, handled, _ := parseDeletePhotoCommand(text, h.currentBotUsername())
	return handled
}

type deletePhotoTarget struct {
	username string
	userID   int64
}

func (t deletePhotoTarget) label() string {
	if t.username != "" {
		return "@" + strings.TrimPrefix(t.username, "@")
	}
	return strconv.FormatInt(t.userID, 10)
}

func parseDeletePhotoCommand(text string, botUsername string) (deletePhotoTarget, bool, bool) {
	fields := strings.Fields(strings.TrimSpace(text))
	if len(fields) == 0 {
		return deletePhotoTarget{}, false, false
	}

	if isDeletePhotoCommand(fields[0], botUsername) {
		if len(fields) != 2 {
			return deletePhotoTarget{}, true, false
		}
		target, valid := parseDeletePhotoTarget(fields[1])
		return target, true, valid
	}

	if len(fields) >= 2 && strings.EqualFold(fields[0], "удалить") && strings.EqualFold(fields[1], "фото") {
		if len(fields) != 3 {
			return deletePhotoTarget{}, true, false
		}
		target, valid := parseDeletePhotoTarget(fields[2])
		return target, true, valid
	}

	return deletePhotoTarget{}, false, false
}

func isDeletePhotoCommand(value string, botUsername string) bool {
	command := strings.ToLower(strings.TrimSpace(value))
	botUsername = strings.ToLower(strings.TrimPrefix(botUsername, "@"))
	if strings.Contains(command, "@") {
		parts := strings.SplitN(command, "@", 2)
		command = parts[0]
		username := strings.TrimSpace(parts[1])
		if username == "" || username != botUsername {
			return false
		}
	}

	switch command {
	case "/delete_photo":
		return true
	default:
		return false
	}
}

func parseDeletePhotoTarget(value string) (deletePhotoTarget, bool) {
	value = strings.TrimSpace(value)
	if after, ok := strings.CutPrefix(value, "@"); ok {
		username := after
		if username == "" || strings.Contains(username, "@") {
			return deletePhotoTarget{}, false
		}
		return deletePhotoTarget{username: username}, true
	}

	if value == "" {
		return deletePhotoTarget{}, false
	}
	for _, r := range value {
		if r < '0' || r > '9' {
			return deletePhotoTarget{}, false
		}
	}

	userID, err := strconv.ParseInt(value, 10, 64)
	if err != nil || userID <= 0 {
		return deletePhotoTarget{}, false
	}
	return deletePhotoTarget{userID: userID}, true
}
