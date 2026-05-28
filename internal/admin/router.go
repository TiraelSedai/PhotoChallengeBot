package admin

import (
	"context"

	"github.com/TiraelSedai/PhotoChallengeBot/internal/require"
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
	require.NotNil("delete photo handler", cfg.DeletePhoto)
	require.NotNil("finish vote handler", cfg.FinishVote)
	require.NotNil("create challenge handler", cfg.CreateChallenge)
	return &Router{
		deletePhoto:     cfg.DeletePhoto,
		finishVote:      cfg.FinishVote,
		createChallenge: cfg.CreateChallenge,
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
