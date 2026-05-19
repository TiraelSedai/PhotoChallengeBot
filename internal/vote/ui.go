package vote

import (
	"fmt"

	"github.com/TiraelSedai/PhotoChallengeBot/internal/repository"
	"github.com/go-telegram/bot/models"
)

const (
	callbackPrefix = "vote"

	actionPrevious = "prev"
	actionNext     = "next"
	actionToggle   = "like"
	actionNoop     = "noop"
)

type View struct {
	ChallengeID int64
	Photo       repository.Photo
	Position    int
	Total       int
	Liked       bool
}

func caption(view View) string {
	return fmt.Sprintf("%d/%d", view.Position+1, view.Total)
}

func keyboard(view View) *models.InlineKeyboardMarkup {
	likeText := "♡"
	if view.Liked {
		likeText = "♥"
	}

	return &models.InlineKeyboardMarkup{
		InlineKeyboard: [][]models.InlineKeyboardButton{
			{
				{Text: "⬅️", CallbackData: callbackData(view.ChallengeID, view.Photo.ID, actionPrevious)},
				{Text: fmt.Sprintf("%d/%d", view.Position+1, view.Total), CallbackData: callbackData(view.ChallengeID, view.Photo.ID, actionNoop)},
				{Text: "➡️", CallbackData: callbackData(view.ChallengeID, view.Photo.ID, actionNext)},
			},
			{
				{Text: likeText, CallbackData: callbackData(view.ChallengeID, view.Photo.ID, actionToggle)},
			},
		},
	}
}

func callbackData(challengeID, photoID int64, action string) string {
	return fmt.Sprintf("%s:%d:%d:%s", callbackPrefix, challengeID, photoID, action)
}
