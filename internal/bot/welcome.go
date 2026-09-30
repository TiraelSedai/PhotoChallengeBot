package bot

import (
	"context"

	"github.com/go-telegram/bot/models"
)

type welcomePublisher interface {
	SendEphemeralMarkdown(context.Context, int64, int64, string) error
}

const welcomeMessage = "👋 Привет! Рады тебя видеть 🖤\n\n" +
	"В чате есть два закрепа \u2014 прочитай их, пожалуйста.\n\n" +
	"Присылай интро-фотку и участвуй в движах, про них будут напоминалки.\n\n" +
	"*Важно*: на челлендж принимаются только свежие фотки \u2014 даты есть в закрепе. Если случайно прислал старую, пингани меня."

func (r *Router) welcomeNewMember(ctx context.Context, update *models.ChatMemberUpdated) error {
	switch update.OldChatMember.Type {
	case models.ChatMemberTypeLeft, models.ChatMemberTypeBanned:
	case models.ChatMemberTypeRestricted:
		if update.OldChatMember.Restricted.IsMember {
			return nil
		}
	default:
		return nil
	}
	user := joinedMember(update.NewChatMember)
	if user == nil || user.IsBot {
		return nil
	}
	return r.welcomePublisher.SendEphemeralMarkdown(ctx, update.Chat.ID, user.ID, welcomeMessage)
}

// joinedMember returns the user only when the status represents current membership.
func joinedMember(member models.ChatMember) *models.User {
	switch member.Type {
	case models.ChatMemberTypeOwner:
		return member.Owner.User
	case models.ChatMemberTypeAdministrator:
		return &member.Administrator.User
	case models.ChatMemberTypeMember:
		return member.Member.User
	case models.ChatMemberTypeRestricted:
		if member.Restricted.IsMember {
			return member.Restricted.User
		}
	}
	return nil
}
