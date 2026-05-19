package admin

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
	"unicode"

	"github.com/TiraelSedai/PhotoChallengeBot/internal/challenge"
	"github.com/TiraelSedai/PhotoChallengeBot/internal/repository"
	"github.com/TiraelSedai/PhotoChallengeBot/internal/templates"
	"github.com/go-telegram/bot/models"
)

const (
	createChallengeFlow = "create_challenge"
	stepTheme           = "theme"
	stepHashtag         = "hashtag"
	stepDates           = "dates"
	stepApprove         = "approve"
)

type SessionStore interface {
	Get(context.Context, int64, int64) (*repository.AdminSession, error)
	Upsert(context.Context, repository.AdminSession) (repository.AdminSession, error)
	Clear(context.Context, int64, int64) error
}

type UserStore interface {
	Upsert(context.Context, repository.User) (repository.User, error)
}

type ChallengePlanner interface {
	Plan(context.Context, challenge.CreateInput) (challenge.Plan, error)
	CreateActive(context.Context, challenge.CreateInput) (repository.Challenge, error)
	CreatePlanned(context.Context, challenge.Plan) (repository.Challenge, error)
}

type ChallengeAnnouncements interface {
	Get(context.Context, int64) (repository.Challenge, error)
	SetAnnouncementMessageID(context.Context, int64, int, time.Time) error
}

type AnnouncementRenderer interface {
	ChallengeAnnouncement(templates.ChallengeAnnouncementData) (string, error)
}

type Publisher interface {
	SendMarkdown(context.Context, int64, string) (int, error)
	SendText(context.Context, int64, string) (int, error)
	Pin(context.Context, int64, int) error
}

type CreateChallengeHandler struct {
	adminChatID   int64
	mainChatID    int64
	location      *time.Location
	sessions      SessionStore
	users         UserStore
	challenges    ChallengePlanner
	announcements ChallengeAnnouncements
	renderer      AnnouncementRenderer
	publisher     Publisher
	botUsername   func() string
}

type CreateChallengeConfig struct {
	AdminChatID   int64
	MainChatID    int64
	Location      *time.Location
	Sessions      SessionStore
	Users         UserStore
	Challenges    ChallengePlanner
	Announcements ChallengeAnnouncements
	Renderer      AnnouncementRenderer
	Publisher     Publisher
	BotUsername   func() string
}

func NewCreateChallengeHandler(cfg CreateChallengeConfig) *CreateChallengeHandler {
	location := cfg.Location
	if location == nil {
		location = time.UTC
	}
	botUsername := cfg.BotUsername
	if botUsername == nil {
		botUsername = func() string { return "" }
	}
	switch {
	case cfg.AdminChatID == 0:
		panic("admin chat id is required")
	case cfg.MainChatID == 0:
		panic("main chat id is required")
	case cfg.Sessions == nil:
		panic("admin session store is nil")
	case cfg.Users == nil:
		panic("user store is nil")
	case cfg.Challenges == nil:
		panic("challenge planner is nil")
	case cfg.Announcements == nil:
		panic("challenge announcement store is nil")
	case cfg.Renderer == nil:
		panic("announcement renderer is nil")
	case cfg.Publisher == nil:
		panic("publisher is nil")
	}
	return &CreateChallengeHandler{
		adminChatID:   cfg.AdminChatID,
		mainChatID:    cfg.MainChatID,
		location:      location,
		sessions:      cfg.Sessions,
		users:         cfg.Users,
		challenges:    cfg.Challenges,
		announcements: cfg.Announcements,
		renderer:      cfg.Renderer,
		publisher:     cfg.Publisher,
		botUsername:   botUsername,
	}
}

func (h *CreateChallengeHandler) HandleAdminChatMessage(ctx context.Context, message *models.Message) error {
	if message == nil || message.From == nil {
		return nil
	}

	adminUser, err := h.users.Upsert(ctx, telegramUser(*message.From))
	if err != nil {
		return err
	}

	session, err := h.sessions.Get(ctx, h.adminChatID, adminUser.ID)
	if err != nil {
		return err
	}

	text := strings.TrimSpace(message.Text)
	if text == "" {
		return nil
	}
	if isCommandMentionedToOtherBot(text, h.currentBotUsername()) {
		return nil
	}
	if session == nil {
		if !isCreateChallengeCommand(text, h.currentBotUsername()) {
			return nil
		}
		return h.start(ctx, adminUser.ID)
	}
	if session.Flow != createChallengeFlow {
		return nil
	}

	switch session.Step {
	case stepTheme:
		return h.acceptTheme(ctx, adminUser.ID, text)
	case stepHashtag:
		return h.acceptHashtag(ctx, adminUser.ID, session.PayloadJSON, text)
	case stepDates:
		return h.acceptDates(ctx, adminUser.ID, session.PayloadJSON, text)
	case stepApprove:
		return h.approve(ctx, adminUser.ID, session.PayloadJSON, text)
	default:
		return h.sessions.Clear(ctx, h.adminChatID, adminUser.ID)
	}
}

func (h *CreateChallengeHandler) start(ctx context.Context, adminUserID int64) error {
	if err := h.savePayload(ctx, adminUserID, stepTheme, createChallengePayload{}); err != nil {
		return err
	}
	_, err := h.publisher.SendMarkdown(ctx, h.adminChatID, "Пришли тему челленджа.")
	return err
}

func (h *CreateChallengeHandler) acceptTheme(ctx context.Context, adminUserID int64, text string) error {
	if text == "" {
		_, err := h.publisher.SendMarkdown(ctx, h.adminChatID, "Тема не должна быть пустой.")
		return err
	}
	if err := h.savePayload(ctx, adminUserID, stepHashtag, createChallengePayload{Theme: text}); err != nil {
		return err
	}
	_, err := h.publisher.SendMarkdown(ctx, h.adminChatID, "Пришли хештег челленджа в формате #tag.")
	return err
}

func (h *CreateChallengeHandler) acceptHashtag(ctx context.Context, adminUserID int64, payloadJSON, text string) error {
	payload, err := decodePayload(payloadJSON)
	if err != nil {
		return err
	}
	if !isValidHashtag(text) {
		_, err := h.publisher.SendMarkdown(ctx, h.adminChatID, "Хештег должен быть одним словом в формате #tag.")
		return err
	}

	payload.Hashtag = text
	if err := h.savePayload(ctx, adminUserID, stepDates, payload); err != nil {
		return err
	}
	_, err = h.publisher.SendMarkdown(ctx, h.adminChatID, "Пришли даты: `ОК` для дефолта или `YYYY-MM-DD YYYY-MM-DD`.")
	return err
}

func (h *CreateChallengeHandler) acceptDates(ctx context.Context, adminUserID int64, payloadJSON, text string) error {
	payload, err := decodePayload(payloadJSON)
	if err != nil {
		return err
	}

	startDate, endDate, err := parseDateRange(text, h.location)
	if err != nil {
		_, sendErr := h.publisher.SendMarkdown(ctx, h.adminChatID, "Не понял даты. Пришли `ОК` или `YYYY-MM-DD YYYY-MM-DD`.")
		return sendErr
	}

	input := challenge.CreateInput{
		MainChatID:      h.mainChatID,
		Theme:           payload.Theme,
		Hashtag:         payload.Hashtag,
		CreatedByUserID: adminUserID,
		StartDate:       startDate,
		EndDate:         endDate,
	}
	plan, err := h.challenges.Plan(ctx, input)
	if err != nil {
		_, sendErr := h.publisher.SendMarkdown(ctx, h.adminChatID, "Не получилось подготовить челлендж. Проверь даты и попробуй еще раз.")
		return sendErr
	}

	draft, err := h.renderer.ChallengeAnnouncement(announcementData(plan))
	if err != nil {
		return err
	}

	payload.StartDate = dateString(plan.AcceptStartAt)
	payload.EndDate = dateString(plan.AcceptUntilAt)
	payload.Num = plan.Num
	payload.AcceptStartAt = timeString(plan.AcceptStartAt)
	payload.AcceptUntilAt = timeString(plan.AcceptUntilAt)
	payload.ReminderAt = timeString(plan.ReminderAt)
	payload.DraftText = draft
	if err := h.savePayload(ctx, adminUserID, stepApprove, payload); err != nil {
		return err
	}

	_, err = h.publisher.SendMarkdown(ctx, h.adminChatID, draft+"\n\nДля публикации пришли `ОК`; любое другое сообщение уйдет в основной чат как текст анонса.")
	return err
}

func (h *CreateChallengeHandler) approve(ctx context.Context, adminUserID int64, payloadJSON, text string) error {
	payload, err := decodePayload(payloadJSON)
	if err != nil {
		return err
	}

	startDate, err := parseOptionalDate(payload.StartDate, h.location)
	if err != nil {
		return err
	}
	endDate, err := parseOptionalDate(payload.EndDate, h.location)
	if err != nil {
		return err
	}
	acceptStartAt, err := parseTime(payload.AcceptStartAt)
	if err != nil {
		return err
	}
	acceptUntilAt, err := parseTime(payload.AcceptUntilAt)
	if err != nil {
		return err
	}
	reminderAt, err := parseTime(payload.ReminderAt)
	if err != nil {
		return err
	}

	if !payload.AnnouncementSelected {
		announcementText := strings.TrimSpace(text)
		payload.AnnouncementText = announcementText
		payload.AnnouncementMarkdown = false
		if isOK(announcementText) {
			payload.AnnouncementText = payload.DraftText
			payload.AnnouncementMarkdown = true
		}
		payload.AnnouncementSelected = true
		if err := h.savePayload(ctx, adminUserID, stepApprove, payload); err != nil {
			return err
		}
	}

	challengeID := payload.ChallengeID
	if challengeID == 0 {
		challengeRow, err := h.challenges.CreatePlanned(ctx, challenge.Plan{
			Input: challenge.CreateInput{
				MainChatID:      h.mainChatID,
				Theme:           payload.Theme,
				Hashtag:         payload.Hashtag,
				CreatedByUserID: adminUserID,
				StartDate:       startDate,
				EndDate:         endDate,
			},
			Num:           payload.Num,
			AcceptStartAt: acceptStartAt,
			AcceptUntilAt: acceptUntilAt,
			ReminderAt:    reminderAt,
		})
		if err != nil {
			if clearErr := h.sessions.Clear(ctx, h.adminChatID, adminUserID); clearErr != nil {
				return clearErr
			}
			_, sendErr := h.publisher.SendMarkdown(ctx, h.adminChatID, "Не получилось создать челлендж. Начни создание заново.")
			return sendErr
		}

		payload.ChallengeID = challengeRow.ID
		challengeID = challengeRow.ID
		if err := h.savePayload(ctx, adminUserID, stepApprove, payload); err != nil {
			return err
		}
	}

	send := h.publisher.SendText
	if payload.AnnouncementMarkdown {
		send = h.publisher.SendMarkdown
	}

	messageID := payload.AnnouncementMessageID
	if messageID == 0 {
		challengeRow, err := h.announcements.Get(ctx, challengeID)
		if err != nil {
			return err
		}
		if challengeRow.AnnouncementMessageID != nil {
			messageID = int(*challengeRow.AnnouncementMessageID)
		} else {
			messageID, err = send(ctx, h.mainChatID, payload.AnnouncementText)
			if err != nil {
				return err
			}
			payload.AnnouncementMessageID = messageID
			if err := h.announcements.SetAnnouncementMessageID(ctx, challengeID, messageID, time.Now().In(h.location)); err != nil {
				if saveErr := h.savePayload(ctx, adminUserID, stepApprove, payload); saveErr != nil {
					return fmt.Errorf("%w; save sent announcement id: %v", err, saveErr)
				}
				return err
			}
		}

		payload.AnnouncementMessageID = messageID
		if err := h.savePayload(ctx, adminUserID, stepApprove, payload); err != nil {
			return err
		}
	} else if err := h.announcements.SetAnnouncementMessageID(ctx, challengeID, messageID, time.Now().In(h.location)); err != nil {
		return err
	}
	if err := h.publisher.Pin(ctx, h.mainChatID, messageID); err != nil {
		return err
	}
	if err := h.sessions.Clear(ctx, h.adminChatID, adminUserID); err != nil {
		return err
	}

	_, err = h.publisher.SendMarkdown(ctx, h.adminChatID, "Челлендж опубликован.")
	return err
}

func (h *CreateChallengeHandler) savePayload(ctx context.Context, adminUserID int64, step string, payload createChallengePayload) error {
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("encode create challenge payload: %w", err)
	}
	_, err = h.sessions.Upsert(ctx, repository.AdminSession{
		AdminChatID: h.adminChatID,
		AdminUserID: adminUserID,
		Flow:        createChallengeFlow,
		Step:        step,
		PayloadJSON: string(payloadJSON),
	})
	return err
}

func (h *CreateChallengeHandler) currentBotUsername() string {
	return h.botUsername()
}

type createChallengePayload struct {
	Theme                 string `json:"theme,omitempty"`
	Hashtag               string `json:"hashtag,omitempty"`
	StartDate             string `json:"start_date,omitempty"`
	EndDate               string `json:"end_date,omitempty"`
	Num                   int    `json:"num,omitempty"`
	AcceptStartAt         string `json:"accept_start_at,omitempty"`
	AcceptUntilAt         string `json:"accept_until_at,omitempty"`
	ReminderAt            string `json:"reminder_at,omitempty"`
	DraftText             string `json:"draft_text,omitempty"`
	AnnouncementSelected  bool   `json:"announcement_selected,omitempty"`
	AnnouncementText      string `json:"announcement_text,omitempty"`
	AnnouncementMarkdown  bool   `json:"announcement_markdown,omitempty"`
	ChallengeID           int64  `json:"challenge_id,omitempty"`
	AnnouncementMessageID int    `json:"announcement_message_id,omitempty"`
}

func decodePayload(payloadJSON string) (createChallengePayload, error) {
	var payload createChallengePayload
	if err := json.Unmarshal([]byte(payloadJSON), &payload); err != nil {
		return createChallengePayload{}, fmt.Errorf("decode create challenge payload: %w", err)
	}
	return payload, nil
}

func telegramUser(user models.User) repository.User {
	return repository.User{
		ID:        user.ID,
		Username:  user.Username,
		FirstName: user.FirstName,
		LastName:  user.LastName,
	}
}

func isCreateChallengeCommand(text string, botUsername string) bool {
	normalized := strings.ToLower(strings.TrimSpace(text))
	parts := strings.SplitN(normalized, "@", 2)
	command := parts[0]
	if len(parts) == 2 {
		username := strings.TrimSpace(parts[1])
		if username == "" || username != strings.ToLower(strings.TrimPrefix(botUsername, "@")) {
			return false
		}
	}
	return command == "/new" ||
		command == "/challenge" ||
		normalized == "создать челлендж"
}

func isCommandMentionedToOtherBot(text string, botUsername string) bool {
	fields := strings.Fields(strings.TrimSpace(text))
	if len(fields) == 0 {
		return false
	}
	command := strings.ToLower(fields[0])
	parts := strings.SplitN(command, "@", 2)
	if len(parts) != 2 {
		return false
	}
	username := strings.TrimSpace(parts[1])
	return username != "" && username != strings.ToLower(strings.TrimPrefix(botUsername, "@"))
}

func isOK(text string) bool {
	normalized := strings.ToLower(strings.TrimSpace(text))
	return normalized == "ok" || normalized == "ок"
}

func isValidHashtag(value string) bool {
	if !strings.HasPrefix(value, "#") || len([]rune(value)) < 2 {
		return false
	}
	for i, r := range value {
		if i == 0 {
			continue
		}
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' {
			continue
		}
		return false
	}
	return true
}

func parseDateRange(text string, location *time.Location) (*time.Time, *time.Time, error) {
	if isOK(text) || strings.TrimSpace(text) == "" {
		return nil, nil, nil
	}

	fields := strings.Fields(text)
	switch len(fields) {
	case 1:
		endDate, err := parseDate(fields[0], location)
		if err != nil {
			return nil, nil, err
		}
		return nil, &endDate, nil
	case 2:
		startDate, err := parseDate(fields[0], location)
		if err != nil {
			return nil, nil, err
		}
		endDate, err := parseDate(fields[1], location)
		if err != nil {
			return nil, nil, err
		}
		return &startDate, &endDate, nil
	default:
		return nil, nil, fmt.Errorf("expected one or two dates")
	}
}

func parseOptionalDate(value string, location *time.Location) (*time.Time, error) {
	if value == "" {
		return nil, nil
	}
	parsed, err := parseDate(value, location)
	if err != nil {
		return nil, err
	}
	return &parsed, nil
}

func parseDate(value string, location *time.Location) (time.Time, error) {
	for _, layout := range []string{"2006-01-02", "02.01.2006"} {
		parsed, err := time.ParseInLocation(layout, value, location)
		if err == nil {
			return time.Date(parsed.Year(), parsed.Month(), parsed.Day(), 0, 0, 0, 0, location), nil
		}
	}
	return time.Time{}, fmt.Errorf("parse date %q", value)
}

func timeString(value time.Time) string {
	return value.Format(time.RFC3339Nano)
}

func parseTime(value string) (time.Time, error) {
	if value == "" {
		return time.Time{}, fmt.Errorf("time is required")
	}
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return time.Time{}, err
	}
	return parsed, nil
}

func dateString(value time.Time) string {
	return value.Format("2006-01-02")
}

func optionalDateString(value *time.Time) string {
	if value == nil {
		return ""
	}
	return dateString(*value)
}

func announcementData(plan challenge.Plan) templates.ChallengeAnnouncementData {
	return templates.ChallengeAnnouncementData{
		Num:        plan.Num,
		Theme:      plan.Input.Theme,
		Hashtag:    plan.Input.Hashtag,
		StartDate:  russianDate(plan.AcceptStartAt),
		EndDate:    russianDate(plan.AcceptUntilAt),
		EndWeekday: russianWeekday(plan.AcceptUntilAt),
	}
}

func russianDate(value time.Time) string {
	months := [...]string{
		"января", "февраля", "марта", "апреля", "мая", "июня",
		"июля", "августа", "сентября", "октября", "ноября", "декабря",
	}
	return fmt.Sprintf("%d %s", value.Day(), months[int(value.Month())-1])
}

func russianWeekday(value time.Time) string {
	weekdays := map[time.Weekday]string{
		time.Monday:    "понедельник",
		time.Tuesday:   "вторник",
		time.Wednesday: "среда",
		time.Thursday:  "четверг",
		time.Friday:    "пятница",
		time.Saturday:  "суббота",
		time.Sunday:    "воскресенье",
	}
	return weekdays[value.Weekday()]
}
