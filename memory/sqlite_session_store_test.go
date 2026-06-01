package memory

import (
	"context"
	"testing"
	"time"

	"github.com/openbotstack/openbotstack-runtime/api/middleware"
	"github.com/openbotstack/openbotstack-runtime/persistence"

	auth "github.com/openbotstack/openbotstack-core/access/auth"
)

func setupMemoryTestDB(t *testing.T) *persistence.DB {
	t.Helper()
	db, err := persistence.Open(":memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := db.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

func ctxWithTenant(tenantID string) context.Context {
	user := &auth.User{ID: "user1", TenantID: tenantID, Name: "Test"}
	return middleware.WithUser(context.Background(), user)
}

func TestSessionStateStore_UpsertCreate(t *testing.T) {
	db := setupMemoryTestDB(t)
	defer func() { _ = db.Close() }()
	store := NewSQLiteSessionStateStore(db.DB)
	ctx := ctxWithTenant("tenant-a")

	meta := SessionMeta{
		SessionID:          "sess-1",
		TenantID:           "tenant-a",
		UserID:             "user-1",
		MessageCount:       1,
		LastMessagePreview: "Hello world",
		CreatedAt:          time.Now().UTC().Truncate(time.Millisecond),
		UpdatedAt:          time.Now().UTC().Truncate(time.Millisecond),
	}

	if err := store.UpsertSession(ctx, meta); err != nil {
		t.Fatalf("UpsertSession: %v", err)
	}

	got, err := store.GetSession(ctx, "sess-1")
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if got == nil {
		t.Fatal("expected session, got nil")
	}
	if got.SessionID != "sess-1" {
		t.Errorf("SessionID = %q, want %q", got.SessionID, "sess-1")
	}
	if got.EntryCount != 1 {
		t.Errorf("EntryCount = %d, want 1", got.EntryCount)
	}
	if got.LastEntry != "Hello world" {
		t.Errorf("LastEntry = %q, want %q", got.LastEntry, "Hello world")
	}
}

func TestSessionStateStore_UpsertUpdate(t *testing.T) {
	db := setupMemoryTestDB(t)
	defer func() { _ = db.Close() }()
	store := NewSQLiteSessionStateStore(db.DB)
	ctx := ctxWithTenant("tenant-a")

	now := time.Now().UTC().Truncate(time.Millisecond)
	meta1 := SessionMeta{
		SessionID:          "sess-1",
		TenantID:           "tenant-a",
		UserID:             "user-1",
		MessageCount:       1,
		LastMessagePreview: "First message",
		CreatedAt:          now,
		UpdatedAt:          now,
	}
	if err := store.UpsertSession(ctx, meta1); err != nil {
		t.Fatalf("first upsert: %v", err)
	}

	later := now.Add(5 * time.Minute)
	meta2 := SessionMeta{
		SessionID:          "sess-1",
		TenantID:           "tenant-a",
		UserID:             "user-1",
		MessageCount:       1,
		LastMessagePreview: "Second message",
		CreatedAt:          now,
		UpdatedAt:          later,
	}
	if err := store.UpsertSession(ctx, meta2); err != nil {
		t.Fatalf("second upsert: %v", err)
	}

	got, _ := store.GetSession(ctx, "sess-1")
	if got.EntryCount != 2 {
		t.Errorf("EntryCount = %d, want 2 (incremented)", got.EntryCount)
	}
	if got.LastEntry != "Second message" {
		t.Errorf("LastEntry = %q, want %q", got.LastEntry, "Second message")
	}
	if !got.CreatedAt.Equal(now) {
		t.Errorf("CreatedAt changed after update: got %v, want %v", got.CreatedAt, now)
	}
}

func TestSessionStateStore_ListSessions(t *testing.T) {
	db := setupMemoryTestDB(t)
	defer func() { _ = db.Close() }()
	store := NewSQLiteSessionStateStore(db.DB)
	ctx := ctxWithTenant("tenant-a")

	now := time.Now().UTC()
	for i, msg := range []string{"first", "second", "third"} {
		store.UpsertSession(ctx, SessionMeta{
			SessionID:          "sess-" + string(rune('A'+i)),
			TenantID:           "tenant-a",
			UserID:             "user-1",
			MessageCount:       1,
			LastMessagePreview: msg,
			CreatedAt:          now.Add(time.Duration(i) * time.Minute),
			UpdatedAt:          now.Add(time.Duration(i) * time.Minute),
		})
	}

	sessions, err := store.ListSessions(ctx)
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if len(sessions) != 3 {
		t.Fatalf("got %d sessions, want 3", len(sessions))
	}
	if sessions[0].SessionID != "sess-C" {
		t.Errorf("first session = %q, want %q (ordered by updated_at DESC)", sessions[0].SessionID, "sess-C")
	}
}

func TestSessionStateStore_ListSessions_TenantIsolation(t *testing.T) {
	db := setupMemoryTestDB(t)
	defer func() { _ = db.Close() }()
	store := NewSQLiteSessionStateStore(db.DB)
	ctxA := ctxWithTenant("tenant-a")
	ctxB := ctxWithTenant("tenant-b")

	now := time.Now().UTC()
	store.UpsertSession(ctxA, SessionMeta{SessionID: "s1", TenantID: "tenant-a", UserID: "u1", MessageCount: 1, LastMessagePreview: "A", CreatedAt: now, UpdatedAt: now})
	store.UpsertSession(ctxB, SessionMeta{SessionID: "s2", TenantID: "tenant-b", UserID: "u2", MessageCount: 1, LastMessagePreview: "B", CreatedAt: now, UpdatedAt: now})

	sessionsA, _ := store.ListSessions(ctxA)
	if len(sessionsA) != 1 || sessionsA[0].SessionID != "s1" {
		t.Errorf("tenant-a should see only s1, got %v", sessionsA)
	}

	sessionsB, _ := store.ListSessions(ctxB)
	if len(sessionsB) != 1 || sessionsB[0].SessionID != "s2" {
		t.Errorf("tenant-b should see only s2, got %v", sessionsB)
	}
}

func TestSessionStateStore_GetSession_NotFound(t *testing.T) {
	db := setupMemoryTestDB(t)
	defer func() { _ = db.Close() }()
	store := NewSQLiteSessionStateStore(db.DB)
	ctx := ctxWithTenant("tenant-a")

	got, err := store.GetSession(ctx, "nonexistent")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != nil {
		t.Error("expected nil for nonexistent session")
	}
}

func TestSessionStateStore_DeleteSession(t *testing.T) {
	db := setupMemoryTestDB(t)
	defer func() { _ = db.Close() }()
	store := NewSQLiteSessionStateStore(db.DB)
	ctx := ctxWithTenant("tenant-a")

	now := time.Now().UTC()
	store.UpsertSession(ctx, SessionMeta{SessionID: "s1", TenantID: "tenant-a", UserID: "u1", MessageCount: 1, LastMessagePreview: "msg", CreatedAt: now, UpdatedAt: now})

	if err := store.DeleteSession(ctx, "s1"); err != nil {
		t.Fatalf("DeleteSession: %v", err)
	}

	got, _ := store.GetSession(ctx, "s1")
	if got != nil {
		t.Error("session should be deleted")
	}
}

func TestSessionStateStore_DeleteSession_TenantIsolation(t *testing.T) {
	db := setupMemoryTestDB(t)
	defer func() { _ = db.Close() }()
	store := NewSQLiteSessionStateStore(db.DB)
	ctxA := ctxWithTenant("tenant-a")
	ctxB := ctxWithTenant("tenant-b")

	now := time.Now().UTC()
	store.UpsertSession(ctxA, SessionMeta{SessionID: "shared-id", TenantID: "tenant-a", UserID: "u1", MessageCount: 1, LastMessagePreview: "A", CreatedAt: now, UpdatedAt: now})
	store.UpsertSession(ctxB, SessionMeta{SessionID: "shared-id", TenantID: "tenant-b", UserID: "u2", MessageCount: 1, LastMessagePreview: "B", CreatedAt: now, UpdatedAt: now})

	store.DeleteSession(ctxA, "shared-id")

	got, _ := store.GetSession(ctxB, "shared-id")
	if got == nil {
		t.Error("tenant-b session should still exist")
	}
	if got.TenantID != "tenant-b" {
		t.Errorf("got tenant %q, want tenant-b", got.TenantID)
	}
}

func TestSessionStateStore_ListSessions_NoTenant_NoStrict(t *testing.T) {
	db := setupMemoryTestDB(t)
	defer func() { _ = db.Close() }()
	store := NewSQLiteSessionStateStore(db.DB)

	ctxA := ctxWithTenant("tenant-a")
	ctxNoAuth := context.Background()

	now := time.Now().UTC()
	store.UpsertSession(ctxA, SessionMeta{SessionID: "s1", TenantID: "tenant-a", UserID: "u1", MessageCount: 1, LastMessagePreview: "A", CreatedAt: now, UpdatedAt: now})

	// Without strict mode, no-auth context returns all sessions
	sessions, err := store.ListSessions(ctxNoAuth)
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if len(sessions) != 1 {
		t.Errorf("no-auth ListSessions returned %d, want 1 (backward compat)", len(sessions))
	}
}

func TestSessionStateStore_ListSessions_Strict_RejectsNoTenant(t *testing.T) {
	db := setupMemoryTestDB(t)
	defer func() { _ = db.Close() }()
	store := NewSQLiteSessionStateStore(db.DB, WithStrictTenant(true))

	ctxNoAuth := context.Background()

	_, err := store.ListSessions(ctxNoAuth)
	if err == nil {
		t.Error("expected error when strict mode and no tenant in context")
	}
}

func TestSessionStateStore_GetSession_Strict_RejectsNoTenant(t *testing.T) {
	db := setupMemoryTestDB(t)
	defer func() { _ = db.Close() }()
	store := NewSQLiteSessionStateStore(db.DB, WithStrictTenant(true))

	ctxNoAuth := context.Background()

	_, err := store.GetSession(ctxNoAuth, "any-id")
	if err == nil {
		t.Error("expected error when strict mode and no tenant in context")
	}
}
