package repository

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/jmoiron/sqlx"
)

type Users struct {
	db *sqlx.DB
}

func NewUsers(db *sqlx.DB) *Users {
	if db == nil {
		panic("sql db is nil")
	}
	return &Users{db: db}
}

func (r *Users) Upsert(ctx context.Context, user User) (User, error) {
	if user.ID == 0 {
		return User{}, fmt.Errorf("user id is required")
	}
	if user.UpdatedAt.IsZero() {
		user.UpdatedAt = time.Now().UTC()
	}
	user.DisplayName = displayName(user)
	user.Username = normalizeUsername(user.Username)

	_, err := r.db.ExecContext(ctx, `
		INSERT INTO users (id, username, first_name, last_name, display_name, updated_at)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			username = excluded.username,
			first_name = excluded.first_name,
			last_name = excluded.last_name,
			display_name = excluded.display_name,
			updated_at = excluded.updated_at
	`, user.ID, nullableString(user.Username), nullableString(user.FirstName),
		nullableString(user.LastName), user.DisplayName, timeString(user.UpdatedAt))
	if err != nil {
		return User{}, fmt.Errorf("upsert user: %w", err)
	}

	return r.Get(ctx, user.ID)
}

func (r *Users) Get(ctx context.Context, id int64) (User, error) {
	var row userRow
	if err := r.db.GetContext(ctx, &row, `
		SELECT id, username, first_name, last_name, display_name, updated_at
		FROM users
		WHERE id = ?
	`, id); err != nil {
		return User{}, fmt.Errorf("get user %d: %w", id, err)
	}
	return row.user()
}

func (r *Users) FindByUsername(ctx context.Context, username string) (*User, error) {
	username = normalizeUsername(username)
	if username == "" {
		return nil, fmt.Errorf("username is required")
	}

	var row userRow
	if err := r.db.GetContext(ctx, &row, `
		SELECT id, username, first_name, last_name, display_name, updated_at
		FROM users
		WHERE LOWER(username) = LOWER(?)
	`, username); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("find user by username: %w", err)
	}

	user, err := row.user()
	if err != nil {
		return nil, err
	}
	return &user, nil
}

type userRow struct {
	ID          int64          `db:"id"`
	Username    sql.NullString `db:"username"`
	FirstName   sql.NullString `db:"first_name"`
	LastName    sql.NullString `db:"last_name"`
	DisplayName string         `db:"display_name"`
	UpdatedAt   string         `db:"updated_at"`
}

func (r userRow) user() (User, error) {
	updatedAt, err := parseTime(r.UpdatedAt)
	if err != nil {
		return User{}, fmt.Errorf("parse user updated_at: %w", err)
	}
	return User{
		ID:          r.ID,
		Username:    stringFromNull(r.Username),
		FirstName:   stringFromNull(r.FirstName),
		LastName:    stringFromNull(r.LastName),
		DisplayName: r.DisplayName,
		UpdatedAt:   updatedAt,
	}, nil
}

func normalizeUsername(username string) string {
	return strings.TrimPrefix(strings.TrimSpace(username), "@")
}
