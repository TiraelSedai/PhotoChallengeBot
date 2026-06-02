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
	actionOwnPhoto = "own"
)

type View struct {
	ChallengeID int64
	Photo       repository.Photo
	Position    int
	Total       int
	Liked       bool
	OwnPhoto    bool
}

func keyboard(view View) *models.InlineKeyboardMarkup {
	likeText := "♡"
	likeAction := actionToggle
	if view.Liked {
		likeText = "♥"
	}
	if view.OwnPhoto {
		likeText = "💚"
		likeAction = actionOwnPhoto
	}

	nav := make([]models.InlineKeyboardButton, 0, 3)
	if view.Position > 0 {
		nav = append(nav, models.InlineKeyboardButton{
			Text:         "⬅️",
			CallbackData: callbackData(view.ChallengeID, view.Photo.ID, actionPrevious),
		})
	}
	nav = append(nav, models.InlineKeyboardButton{
		Text:         fmt.Sprintf("%d/%d", view.Position+1, view.Total),
		CallbackData: callbackData(view.ChallengeID, view.Photo.ID, actionNoop),
	})
	if view.Position < view.Total-1 {
		nav = append(nav, models.InlineKeyboardButton{
			Text:         "➡️",
			CallbackData: callbackData(view.ChallengeID, view.Photo.ID, actionNext),
		})
	}

	return &models.InlineKeyboardMarkup{
		InlineKeyboard: [][]models.InlineKeyboardButton{
			nav,
			{
				{Text: likeText, CallbackData: callbackData(view.ChallengeID, view.Photo.ID, likeAction)},
			},
		},
	}
}

func callbackData(challengeID, photoID int64, action string) string {
	return fmt.Sprintf("%s:%d:%d:%s", callbackPrefix, challengeID, photoID, action)
}
