package session

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"klyra/pkg/llm"
)

func TestStoreSaveLoadAndList(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	session, err := store.LoadOrCreate("test/session", ".")
	if err != nil {
		t.Fatal(err)
	}
	if session.ID != "test-session" {
		t.Fatalf("expected sanitized id, got %q", session.ID)
	}
	session.Messages = []llm.Message{{Role: llm.RoleUser, Content: "hello"}}
	if err := store.Save(session); err != nil {
		t.Fatal(err)
	}
	loaded, err := store.Load("test-session")
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Messages) != 1 || loaded.Messages[0].Content != "hello" {
		t.Fatalf("unexpected loaded session: %+v", loaded)
	}
	sessions, err := store.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 1 {
		t.Fatalf("expected one session, got %d", len(sessions))
	}
}

// writeRawSession persists a session verbatim, bypassing Save()'s UpdatedAt
// re-stamping so tests can craft backdated sessions.
func writeRawSession(t *testing.T, store *Store, sess Session) {
	t.Helper()
	data, err := json.MarshalIndent(sess, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(store.dir, sess.ID+".json"), append(data, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
}

func seedSession(t *testing.T, store *Store, id string) Session {
	t.Helper()
	sess, err := store.LoadOrCreate(id, ".")
	if err != nil {
		t.Fatal(err)
	}
	sess.Messages = []llm.Message{{Role: llm.RoleUser, Content: "hi"}}
	if err := store.Save(sess); err != nil {
		t.Fatal(err)
	}
	return sess
}

func TestStoreRename(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	seedSession(t, store, "old")

	if err := store.Rename("old", "new"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Load("old"); err == nil {
		t.Fatal("old session should no longer exist")
	}
	loaded, err := store.Load("new")
	if err != nil {
		t.Fatalf("renamed session not found: %v", err)
	}
	if loaded.ID != "new" {
		t.Fatalf("expected id 'new', got %q", loaded.ID)
	}
	if len(loaded.Messages) != 1 {
		t.Fatalf("messages not preserved across rename: %+v", loaded.Messages)
	}
}

func TestStoreRenameRejectsExistingTarget(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	seedSession(t, store, "a")
	seedSession(t, store, "b")

	if err := store.Rename("a", "b"); err == nil {
		t.Fatal("expected rename to existing target to fail")
	}
	// Source must remain intact after a rejected rename.
	if _, err := store.Load("a"); err != nil {
		t.Fatalf("source session should survive a rejected rename: %v", err)
	}
}

func TestStoreDelete(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	seedSession(t, store, "doomed")

	if err := store.Delete("doomed"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Load("doomed"); err == nil {
		t.Fatal("deleted session should not load")
	}
	if err := store.Delete("doomed"); err == nil {
		t.Fatal("deleting a missing session should error")
	}
}

func TestStorePrune(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	old := seedSession(t, store, "stale")
	old.UpdatedAt = time.Now().UTC().Add(-48 * time.Hour)
	// Save() stamps UpdatedAt to now, so write the backdated timestamp directly.
	writeRawSession(t, store, old)
	seedSession(t, store, "fresh")

	count, err := store.Prune(24 * time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("expected 1 pruned session, got %d", count)
	}
	if _, err := store.Load("stale"); err == nil {
		t.Fatal("stale session should be pruned")
	}
	if _, err := store.Load("fresh"); err != nil {
		t.Fatalf("fresh session should survive prune: %v", err)
	}
}

func TestStoreExportImportRoundTrip(t *testing.T) {
	srcDir := t.TempDir()
	store, err := NewStore(srcDir)
	if err != nil {
		t.Fatal(err)
	}
	seedSession(t, store, "portable")

	exportPath := filepath.Join(t.TempDir(), "portable.json")
	if _, err := store.Export("portable", exportPath); err != nil {
		t.Fatal(err)
	}

	// Import into a fresh store (different workspace).
	store2, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	imported, err := store2.Import(exportPath, false)
	if err != nil {
		t.Fatal(err)
	}
	if imported.ID != "portable" || len(imported.Messages) != 1 {
		t.Fatalf("unexpected imported session: %+v", imported)
	}

	// Re-import without --overwrite must fail; with overwrite must succeed.
	if _, err := store2.Import(exportPath, false); err == nil {
		t.Fatal("re-import without overwrite should fail")
	}
	if _, err := store2.Import(exportPath, true); err != nil {
		t.Fatalf("re-import with overwrite should succeed: %v", err)
	}
}
