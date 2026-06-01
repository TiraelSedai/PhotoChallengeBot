package tg

import (
	"context"
	"errors"
	"fmt"

	tgbot "github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
)

func (r *Runner) SendMarkdown(ctx context.Context, chatID int64, text string) (int, error) {
	return r.send(ctx, chatID, text, models.ParseModeMarkdownV1)
}

func (r *Runner) SendText(ctx context.Context, chatID int64, text string) (int, error) {
	return r.send(ctx, chatID, text, "")
}

func (r *Runner) SendTextReply(ctx context.Context, chatID int64, text string, replyToMessageID int) (int, error) {
	return r.sendWithReply(ctx, chatID, text, "", replyToMessageID)
}

func (r *Runner) send(ctx context.Context, chatID int64, text string, parseMode models.ParseMode) (int, error) {
	return r.sendWithReply(ctx, chatID, text, parseMode, 0)
}

func (r *Runner) sendWithReply(ctx context.Context, chatID int64, text string, parseMode models.ParseMode, replyToMessageID int) (int, error) {
	var replyParameters *models.ReplyParameters
	if replyToMessageID != 0 {
		replyParameters = &models.ReplyParameters{MessageID: replyToMessageID}
	}
	message, err := r.client.SendMessage(ctx, &tgbot.SendMessageParams{
		ChatID:          chatID,
		Text:            text,
		ParseMode:       parseMode,
		ReplyParameters: replyParameters,
	})
	if err != nil {
		return 0, fmt.Errorf("send telegram message: %w", err)
	}
	if message == nil {
		return 0, errors.New("send telegram message: empty response")
	}
	return message.ID, nil
}

func (r *Runner) SendPhoto(
	ctx context.Context,
	chatID int64,
	fileID string,
	caption string,
	replyMarkup *models.InlineKeyboardMarkup,
) (int, error) {
	return r.sendPhoto(ctx, chatID, fileID, caption, "", replyMarkup)
}

func (r *Runner) SendMarkdownPhoto(ctx context.Context, chatID int64, fileID string, caption string) (int, error) {
	return r.sendPhoto(ctx, chatID, fileID, caption, models.ParseModeMarkdownV1, nil)
}

func (r *Runner) sendPhoto(
	ctx context.Context,
	chatID int64,
	fileID string,
	caption string,
	parseMode models.ParseMode,
	replyMarkup *models.InlineKeyboardMarkup,
) (int, error) {
	message, err := r.client.SendPhoto(ctx, &tgbot.SendPhotoParams{
		ChatID:      chatID,
		Photo:       &models.InputFileString{Data: fileID},
		Caption:     caption,
		ParseMode:   parseMode,
		ReplyMarkup: replyMarkup,
	})
	if err != nil {
		return 0, fmt.Errorf("send telegram photo: %w", err)
	}
	if message == nil {
		return 0, errors.New("send telegram photo: empty response")
	}
	return message.ID, nil
}

func (r *Runner) SendMarkdownPhotoGroup(ctx context.Context, chatID int64, fileIDs []string, captions []string) (int, error) {
	if len(fileIDs) == 0 {
		return 0, errors.New("send telegram photo group: no photos")
	}
	if len(fileIDs) != len(captions) {
		return 0, errors.New("send telegram photo group: caption count does not match photo count")
	}
	if len(fileIDs) == 1 {
		return r.SendMarkdownPhoto(ctx, chatID, fileIDs[0], captions[0])
	}

	media := make([]models.InputMedia, 0, len(fileIDs))
	for idx, fileID := range fileIDs {
		media = append(media, &models.InputMediaPhoto{
			Media:     fileID,
			Caption:   captions[idx],
			ParseMode: models.ParseModeMarkdownV1,
		})
	}
	messages, err := r.client.SendMediaGroup(ctx, &tgbot.SendMediaGroupParams{
		ChatID: chatID,
		Media:  media,
	})
	if err != nil {
		return 0, fmt.Errorf("send telegram photo group: %w", err)
	}
	if len(messages) == 0 || messages[0] == nil {
		return 0, errors.New("send telegram photo group: empty response")
	}
	return messages[0].ID, nil
}

func (r *Runner) EditPhoto(
	ctx context.Context,
	chatID int64,
	messageID int,
	fileID string,
	caption string,
	replyMarkup *models.InlineKeyboardMarkup,
) error {
	_, err := r.client.EditMessageMedia(ctx, &tgbot.EditMessageMediaParams{
		ChatID:    chatID,
		MessageID: messageID,
		Media: &models.InputMediaPhoto{
			Media:   fileID,
			Caption: caption,
		},
		ReplyMarkup: replyMarkup,
	})
	if err != nil {
		return fmt.Errorf("edit telegram photo: %w", err)
	}
	return nil
}

func (r *Runner) AnswerCallback(ctx context.Context, callbackID string, text string) error {
	ok, err := r.client.AnswerCallbackQuery(ctx, &tgbot.AnswerCallbackQueryParams{
		CallbackQueryID: callbackID,
		Text:            text,
	})
	if err != nil {
		return fmt.Errorf("answer telegram callback: %w", err)
	}
	if !ok {
		return errors.New("answer telegram callback: false response")
	}
	return nil
}

func (r *Runner) Pin(ctx context.Context, chatID int64, messageID int) error {
	ok, err := r.client.PinChatMessage(ctx, &tgbot.PinChatMessageParams{
		ChatID:    chatID,
		MessageID: messageID,
	})
	if err != nil {
		return fmt.Errorf("pin telegram message: %w", err)
	}
	if !ok {
		return errors.New("pin telegram message: false response")
	}
	return nil
}
