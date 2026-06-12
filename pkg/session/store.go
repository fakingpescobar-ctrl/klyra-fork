package session

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"klyra/pkg/llm"
)

type Session struct {
	ID        string        `json:"id"`
	CWD       string        `json:"cwd"`
	CreatedAt time.Time     `json:"created_at"`
	UpdatedAt time.Time     `json:"updated_at"`
	Messages  []llm.Message `json:"messages"`
}

type Store struct {
	dir string
}

func NewStore(cwd string) (*Store, error) {
	root, err := filepath.Abs(cwd)
	if err != nil {
		return nil, err
	}
	dir := filepath.Join(root, ".agentcli", "sessions")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	return &Store{dir: dir}, nil
}

func (s *Store) Load(id string) (Session, error) {
	id = cleanID(id)
	if id == "" {
		return Session{}, fmt.Errorf("session id cannot be empty")
	}
	data, err := os.ReadFile(filepath.Join(s.dir, id+".json"))
	if err != nil {
		return Session{}, err
	}
	var session Session
	if err := json.Unmarshal(data, &session); err != nil {
		return Session{}, err
	}
	return session, nil
}

func (s *Store) LoadOrCreate(id, cwd string) (Session, error) {
	id = cleanID(id)
	if id == "" {
		id = time.Now().UTC().Format("20060102-150405")
	}
	session, err := s.Load(id)
	if err == nil {
		return session, nil
	}
	if !os.IsNotExist(err) {
		return Session{}, err
	}
	now := time.Now().UTC()
	return Session{ID: id, CWD: cwd, CreatedAt: now, UpdatedAt: now}, nil
}

func (s *Store) Save(session Session) error {
	session.ID = cleanID(session.ID)
	if session.ID == "" {
		return fmt.Errorf("session id cannot be empty")
	}
	session.UpdatedAt = time.Now().UTC()
	data, err := json.MarshalIndent(session, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(s.dir, session.ID+".json"), append(data, '\n'), 0o644)
}

func (s *Store) List() ([]Session, error) {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return nil, err
	}
	sessions := make([]Session, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(s.dir, entry.Name()))
		if err != nil {
			return nil, err
		}
		var session Session
		if err := json.Unmarshal(data, &session); err != nil {
			return nil, err
		}
		sessions = append(sessions, session)
	}
	return sessions, nil
}

// Rename changes a session's ID by rewriting the file under the new name and deleting the old one.
func (s *Store) Rename(oldID, newID string) error {
	oldID = cleanID(oldID)
	newID = cleanID(newID)
	if oldID == "" || newID == "" {
		return fmt.Errorf("session ids cannot be empty")
	}
	if oldID == newID {
		return nil
	}
	if _, err := s.Load(newID); err == nil {
		return fmt.Errorf("session %q already exists", newID)
	}
	sess, err := s.Load(oldID)
	if err != nil {
		return err
	}
	sess.ID = newID
	if err := s.Save(sess); err != nil {
		return err
	}
	return s.Delete(oldID)
}

// Delete removes a session by ID.
func (s *Store) Delete(id string) error {
	id = cleanID(id)
	if id == "" {
		return fmt.Errorf("session id cannot be empty")
	}
	path := filepath.Join(s.dir, id+".json")
	if err := os.Remove(path); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("session %q not found", id)
		}
		return err
	}
	return nil
}

// Prune deletes sessions not updated within olderThan duration. Returns count deleted.
func (s *Store) Prune(olderThan time.Duration) (int, error) {
	sessions, err := s.List()
	if err != nil {
		return 0, err
	}
	cutoff := time.Now().UTC().Add(-olderThan)
	count := 0
	for _, sess := range sessions {
		if sess.UpdatedAt.Before(cutoff) {
			if delErr := s.Delete(sess.ID); delErr == nil {
				count++
			}
		}
	}
	return count, nil
}

// Export writes a session to a JSON file at destPath. If destPath is empty, returns the JSON bytes.
func (s *Store) Export(id, destPath string) ([]byte, error) {
	sess, err := s.Load(id)
	if err != nil {
		return nil, err
	}
	data, err := json.MarshalIndent(sess, "", "  ")
	if err != nil {
		return nil, err
	}
	data = append(data, '\n')
	if destPath == "" {
		return data, nil
	}
	return nil, os.WriteFile(destPath, data, 0o644)
}

// Import loads a session from a JSON file and saves it into the store.
// Returns an error if a session with the same ID already exists unless overwrite is true.
func (s *Store) Import(srcPath string, overwrite bool) (Session, error) {
	data, err := os.ReadFile(srcPath)
	if err != nil {
		return Session{}, err
	}
	var sess Session
	if err := json.Unmarshal(data, &sess); err != nil {
		return Session{}, fmt.Errorf("invalid session file: %w", err)
	}
	if sess.ID == "" {
		return Session{}, fmt.Errorf("session file has no id")
	}
	if !overwrite {
		if _, err := s.Load(sess.ID); err == nil {
			return Session{}, fmt.Errorf("session %q already exists; use --overwrite to replace", sess.ID)
		}
	}
	if err := s.Save(sess); err != nil {
		return Session{}, err
	}
	return sess, nil
}

var unsafeID = regexp.MustCompile(`[^a-zA-Z0-9._-]+`)

func cleanID(id string) string {
	id = strings.TrimSpace(id)
	id = unsafeID.ReplaceAllString(id, "-")
	return strings.Trim(id, ".-")
}
