package admin

import (
	"context"

	"github.com/go-telegram/bot/models"
)

type Router struct {
	deletePhoto     DeletePhotoCommandHandler
	finishVote      FinishVoteCommandHandler
	createChallenge AdminMessageHandler
}

type RouterConfig struct {
	DeletePhoto     DeletePhotoCommandHandler
	FinishVote      FinishVoteCommandHandler
	CreateChallenge AdminMessageHandler
}

type AdminMessageHandler interface {
	HandleAdminChatMessage(context.Context, *models.Message) error
}

type DeletePhotoCommandHandler interface {
	AdminMessageHandler
	Handles(string) bool
}

type FinishVoteCommandHandler interface {
	AdminMessageHandler
	Handles(string) bool
}

func NewRouter(cfg RouterConfig) *Router {
	deletePhoto := cfg.DeletePhoto
	if deletePhoto == nil {
		deletePhoto = noOpCommandHandler{}
	}
	finishVote := cfg.FinishVote
	if finishVote == nil {
		finishVote = noOpCommandHandler{}
	}
	createChallenge := cfg.CreateChallenge
	if createChallenge == nil {
		createChallenge = noOpCommandHandler{}
	}
	return &Router{
		deletePhoto:     deletePhoto,
		finishVote:      finishVote,
		createChallenge: createChallenge,
	}
}

func (r *Router) HandleAdminChatMessage(ctx context.Context, message *models.Message) error {
	if message == nil {
		return nil
	}
	if r.deletePhoto.Handles(message.Text) {
		return r.deletePhoto.HandleAdminChatMessage(ctx, message)
	}
	if r.finishVote.Handles(message.Text) {
		return r.finishVote.HandleAdminChatMessage(ctx, message)
	}
	return r.createChallenge.HandleAdminChatMessage(ctx, message)
}

type noOpCommandHandler struct{}

func (noOpCommandHandler) HandleAdminChatMessage(context.Context, *models.Message) error {
	return nil
}

func (noOpCommandHandler) Handles(string) bool {
	return false
}
