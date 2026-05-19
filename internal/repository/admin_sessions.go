package repository

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/jmoiron/sqlx"
)

type AdminSessions struct {
	db *sqlx.DB
}

type AdminSession struct {
	AdminChatID int64
	AdminUserID int64
	Flow        string
	Step        string
	PayloadJSON string
	UpdatedAt   time.Time
}

func NewAdminSessions(db *sqlx.DB) *AdminSessions {
	if db == nil {
		panic("sql db is nil")
	}
	return &AdminSessions{db: db}
}

func (r *AdminSessions) Get(ctx context.Context, adminChatID, adminUserID int64) (*AdminSession, error) {
	var row adminSessionRow
	if err := r.db.GetContext(ctx, &row, `
		SELECT admin_chat_id, admin_user_id, flow, step, payload_json, updated_at
		FROM admin_sessions
		WHERE admin_chat_id = ? AND admin_user_id = ?
	`, adminChatID, adminUserID); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("get admin session: %w", err)
	}

	session, err := row.adminSession()
	if err != nil {
		return nil, err
	}
	return &session, nil
}

func (r *AdminSessions) Upsert(ctx context.Context, session AdminSession) (AdminSession, error) {
	if session.UpdatedAt.IsZero() {
		session.UpdatedAt = time.Now().UTC()
	}

	if _, err := r.db.ExecContext(ctx, `
		INSERT INTO admin_sessions (
			admin_chat_id, admin_user_id, flow, step, payload_json, updated_at
		)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(admin_chat_id, admin_user_id) DO UPDATE SET
			flow = excluded.flow,
			step = excluded.step,
			payload_json = excluded.payload_json,
			updated_at = excluded.updated_at
	`, session.AdminChatID, session.AdminUserID, session.Flow, session.Step,
		session.PayloadJSON, timeString(session.UpdatedAt)); err != nil {
		return AdminSession{}, fmt.Errorf("upsert admin session: %w", err)
	}

	stored, err := r.Get(ctx, session.AdminChatID, session.AdminUserID)
	if err != nil {
		return AdminSession{}, err
	}
	if stored == nil {
		return AdminSession{}, fmt.Errorf("get upserted admin session: no row")
	}
	return *stored, nil
}

func (r *AdminSessions) Clear(ctx context.Context, adminChatID, adminUserID int64) error {
	if _, err := r.db.ExecContext(ctx, `
		DELETE FROM admin_sessions
		WHERE admin_chat_id = ? AND admin_user_id = ?
	`, adminChatID, adminUserID); err != nil {
		return fmt.Errorf("clear admin session: %w", err)
	}
	return nil
}

type adminSessionRow struct {
	AdminChatID int64  `db:"admin_chat_id"`
	AdminUserID int64  `db:"admin_user_id"`
	Flow        string `db:"flow"`
	Step        string `db:"step"`
	PayloadJSON string `db:"payload_json"`
	UpdatedAt   string `db:"updated_at"`
}

func (r adminSessionRow) adminSession() (AdminSession, error) {
	updatedAt, err := parseTime(r.UpdatedAt)
	if err != nil {
		return AdminSession{}, fmt.Errorf("parse admin session updated_at: %w", err)
	}
	return AdminSession{
		AdminChatID: r.AdminChatID,
		AdminUserID: r.AdminUserID,
		Flow:        r.Flow,
		Step:        r.Step,
		PayloadJSON: r.PayloadJSON,
		UpdatedAt:   updatedAt,
	}, nil
}
