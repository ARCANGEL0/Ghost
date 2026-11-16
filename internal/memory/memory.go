package memory

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	memoryDir  = ".ghost"
	memoryFile = "memory.json"
)

type MemoryStore struct {
	db          *sql.DB
	projectPath string
}

type StoredFact struct {
	Key           string  `json:"key"`
	Value         string  `json:"value"`
	Category      string  `json:"category"`
	SourceSession string  `json:"source_session,omitempty"`
	Confidence    float64 `json:"confidence,omitempty"`
}

type memoryHit struct {
	StoredFact
	updatedAt int64
	score     int
}

func NewMemoryStore(conn *sql.DB, workspaceRoot string) (*MemoryStore, error) {
	if conn == nil {
		return nil, fmt.Errorf("memory database is not available")
	}
	projectPath, err := filepath.Abs(workspaceRoot)
	if err != nil {
		return nil, fmt.Errorf("resolve project path: %w", err)
	}
	store := &MemoryStore{db: conn, projectPath: filepath.Clean(projectPath)}
	if err := store.importLegacy(); err != nil {
		return nil, err
	}
	return store, nil
}

func (s *MemoryStore) Set(key, value string) error {
	return s.SetFact(StoredFact{Key: key, Value: value, Category: "manual", Confidence: 1}, "")
}

func (s *MemoryStore) SetFact(fact StoredFact, sessionID string) error {
	if strings.TrimSpace(fact.Key) == "" || strings.TrimSpace(fact.Value) == "" {
		return fmt.Errorf("memory key and value are required")
	}
	if fact.Confidence <= 0 {
		fact.Confidence = 1
	}
	now := time.Now().UnixMilli()
	_, err := s.db.Exec(`
		INSERT INTO project_memories (
			project_path, key, value, category, source_session, confidence, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(project_path, key) DO UPDATE SET
			value = excluded.value,
			category = excluded.category,
			source_session = excluded.source_session,
			confidence = excluded.confidence,
			updated_at = excluded.updated_at`,
		s.projectPath, fact.Key, fact.Value, fact.Category, sessionID, fact.Confidence, now, now,
	)
	if err != nil {
		return fmt.Errorf("save project memory: %w", err)
	}
	return nil
}

func (s *MemoryStore) AddExtracted(fact StoredFact, sessionID string) error {
	if strings.TrimSpace(fact.Key) == "" || strings.TrimSpace(fact.Value) == "" {
		return fmt.Errorf("memory key and value are required")
	}
	if fact.Confidence <= 0 {
		fact.Confidence = 0.7
	}
	now := time.Now().UnixMilli()
	_, err := s.db.Exec(`
		INSERT OR IGNORE INTO project_memories (
			project_path, key, value, category, source_session, confidence, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		s.projectPath, fact.Key, fact.Value, fact.Category, sessionID, fact.Confidence, now, now,
	)
	if err != nil {
		return fmt.Errorf("save extracted memory: %w", err)
	}
	return nil
}

func (s *MemoryStore) Get(key string) (string, bool) {
	var value string
	err := s.db.QueryRow(`SELECT value FROM project_memories WHERE project_path = ? AND key = ?`,
		s.projectPath, key).Scan(&value)
	return value, err == nil
}

func (s *MemoryStore) Delete(key string) error {
	_, err := s.db.Exec(`DELETE FROM project_memories WHERE project_path = ? AND key = ?`, s.projectPath, key)
	if err != nil {
		return fmt.Errorf("delete project memory: %w", err)
	}
	return nil
}

func (s *MemoryStore) GetAll() map[string]string {
	rows, err := s.db.Query(`SELECT key, value FROM project_memories WHERE project_path = ? ORDER BY key`, s.projectPath)
	if err != nil {
		return map[string]string{}
	}
	defer rows.Close()
	values := make(map[string]string)
	for rows.Next() {
		var key, value string
		if rows.Scan(&key, &value) == nil {
			values[key] = value
		}
	}
	return values
}

func (s *MemoryStore) Search(query string, limit int) ([]StoredFact, error) {
	if limit <= 0 {
		limit = 8
	}
	rows, err := s.db.Query(`
		SELECT key, value, category, source_session, confidence, updated_at
		FROM project_memories WHERE project_path = ?`, s.projectPath)
	if err != nil {
		return nil, fmt.Errorf("search project memory: %w", err)
	}
	defer rows.Close()
	terms := memoryTerms(query)
	var hits []memoryHit
	for rows.Next() {
		var hit memoryHit
		if err := rows.Scan(&hit.Key, &hit.Value, &hit.Category, &hit.SourceSession, &hit.Confidence, &hit.updatedAt); err != nil {
			return nil, fmt.Errorf("read project memory: %w", err)
		}
		hit.score = memoryScore(hit.StoredFact, terms)
		if hit.score > 0 {
			hits = append(hits, hit)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read project memory: %w", err)
	}
	sort.SliceStable(hits, func(i, j int) bool {
		if hits[i].score == hits[j].score {
			return hits[i].updatedAt > hits[j].updatedAt
		}
		return hits[i].score > hits[j].score
	})
	if len(hits) > limit {
		hits = hits[:limit]
	}
	facts := make([]StoredFact, len(hits))
	for i, hit := range hits {
		facts[i] = hit.StoredFact
		_, _ = s.db.Exec(`UPDATE project_memories SET use_count = use_count + 1 WHERE project_path = ? AND key = ?`, s.projectPath, hit.Key)
	}
	return facts, nil
}

func (s *MemoryStore) importLegacy() error {
	path := filepath.Join(s.projectPath, memoryDir, memoryFile)
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read existing project memory: %w", err)
	}
	var legacy map[string]string
	if err := json.Unmarshal(data, &legacy); err != nil {
		return fmt.Errorf("read existing project memory: %w", err)
	}
	for key, value := range legacy {
		if err := s.AddExtracted(StoredFact{Key: key, Value: value, Category: "legacy", Confidence: 0.8}, ""); err != nil {
			return err
		}
	}
	return nil
}

func memoryTerms(query string) []string {
	seen := make(map[string]struct{})
	var terms []string
	for _, term := range strings.FieldsFunc(strings.ToLower(query), func(r rune) bool {
		return !(r >= 'a' && r <= 'z') && !(r >= '0' && r <= '9')
	}) {
		if len(term) < 3 {
			continue
		}
		if _, ok := seen[term]; ok {
			continue
		}
		seen[term] = struct{}{}
		terms = append(terms, term)
	}
	return terms
}

func memoryScore(fact StoredFact, terms []string) int {
	key := strings.ToLower(fact.Key)
	category := strings.ToLower(fact.Category)
	value := strings.ToLower(fact.Value)
	score := 0
	for _, term := range terms {
		if strings.Contains(key, term) {
			score += 3
		}
		if strings.Contains(category, term) {
			score += 2
		}
		if strings.Contains(value, term) {
			score++
		}
	}
	return score
}
