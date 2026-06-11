package tg

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	tgbot "github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
)

func (r *Runner) MemberDisplayName(ctx context.Context, chatID int64, userID int64) (string, error) {
	member, err := r.client.GetChatMember(ctx, &tgbot.GetChatMemberParams{ChatID: chatID, UserID: userID})
	if err != nil {
		return "", fmt.Errorf("get chat member %d: %w", userID, err)
	}
	user := memberUser(member)
	if user == nil {
		return "", fmt.Errorf("get chat member %d: empty response", userID)
	}

	name := strings.TrimSpace(strings.Join([]string{user.FirstName, user.LastName}, " "))
	if name != "" {
		return name, nil
	}
	if user.Username != "" {
		return "@" + user.Username, nil
	}
	return strconv.FormatInt(user.ID, 10), nil
}

func memberUser(member *models.ChatMember) *models.User {
	if member == nil {
		return nil
	}
	switch {
	case member.Owner != nil:
		return member.Owner.User
	case member.Administrator != nil:
		return &member.Administrator.User
	case member.Member != nil:
		return member.Member.User
	case member.Restricted != nil:
		return member.Restricted.User
	case member.Left != nil:
		return member.Left.User
	case member.Banned != nil:
		return member.Banned.User
	default:
		return nil
	}
}
