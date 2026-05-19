package repository

import (
	"context"
	"testing"
	"time"
)

func TestAdminSessionsUpsertGetAndClear(t *testing.T) {
	t.Parallel()

	database := openRepositoryTestDB(t)
	defer database.Close()
	ctx := context.Background()
	sessions := NewAdminSessions(database)

	created, err := sessions.Upsert(ctx, AdminSession{
		AdminChatID: 1001,
		AdminUserID: 2002,
		Flow:        "challenge_create",
		Step:        "theme",
		PayloadJSON: `{"theme":"night"}`,
		UpdatedAt:   testTime(0),
	})
	if err != nil {
		t.Fatalf("Upsert() error = %v", err)
	}
	if created.Flow != "challenge_create" || created.Step != "theme" {
		t.Fatalf("created session = %#v, want flow and step", created)
	}

	restartedSessions := NewAdminSessions(database)
	stored, err := restartedSessions.Get(ctx, 1001, 2002)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if stored == nil {
		t.Fatal("Get() returned nil, want session")
	}
	if stored.PayloadJSON != `{"theme":"night"}` {
		t.Fatalf("PayloadJSON = %q, want stored payload", stored.PayloadJSON)
	}

	updated, err := restartedSessions.Upsert(ctx, AdminSession{
		AdminChatID: 1001,
		AdminUserID: 2002,
		Flow:        "challenge_create",
		Step:        "hashtag",
		PayloadJSON: `{"theme":"night","hashtag":"#night"}`,
		UpdatedAt:   testTime(time.Hour),
	})
	if err != nil {
		t.Fatalf("second Upsert() error = %v", err)
	}
	if updated.Step != "hashtag" || updated.PayloadJSON != `{"theme":"night","hashtag":"#night"}` {
		t.Fatalf("updated session = %#v, want replaced step and payload", updated)
	}

	if err := restartedSessions.Clear(ctx, 1001, 2002); err != nil {
		t.Fatalf("Clear() error = %v", err)
	}
	cleared, err := restartedSessions.Get(ctx, 1001, 2002)
	if err != nil {
		t.Fatalf("Get() after Clear() error = %v", err)
	}
	if cleared != nil {
		t.Fatalf("Get() after Clear() = %#v, want nil", cleared)
	}
}
