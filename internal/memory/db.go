package memory

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	_ "modernc.org/sqlite"
)

type Memory struct {
	ID        string    `json:"id"`
	Content   string    `json:"content"`
	Source    string    `json:"source"`
	Tags      []string  `json:"tags"`
	CreatedAt time.Time `json:"created_at"`
}

type Edge struct {
	FromID  string `json:"from_id"`
	ToID    string `json:"to_id"`
	RelType string `json:"rel_type"`
}

type GraphData struct {
	Nodes []Memory `json:"nodes"`
	Edges []Edge   `json:"edges"`
}

type DB struct {
	db    *sql.DB
	mu    sync.RWMutex
	embed EmbedFunc
}

func Open() (*DB, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	dir := filepath.Join(home, ".turbolab")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", filepath.Join(dir, "memory.db"))
	if err != nil {
		return nil, err
	}
	db.Exec("PRAGMA journal_mode=WAL")
	db.Exec("PRAGMA foreign_keys=ON")
	if err := migrate(db); err != nil {
		db.Close()
		return nil, err
	}
	return &DB{db: db}, nil
}

func (d *DB) SetEmbedFunc(fn EmbedFunc) {
	d.mu.Lock()
	d.embed = fn
	d.mu.Unlock()
}

func (d *DB) embedFunc() EmbedFunc {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.embed
}

func migrate(db *sql.DB) error {
	// Recreate vectors table if it uses the old single-vector schema (no chunk_idx column).
	var vectorCols int
	db.QueryRow(`SELECT count(*) FROM pragma_table_info('vectors') WHERE name='chunk_idx'`).Scan(&vectorCols)
	if vectorCols == 0 {
		db.Exec(`DROP TABLE IF EXISTS vectors`)
	}

	stmts := []string{
		`CREATE TABLE IF NOT EXISTS memories (
			id TEXT PRIMARY KEY,
			content TEXT NOT NULL,
			source TEXT DEFAULT '',
			tags TEXT DEFAULT '[]',
			created_at INTEGER NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS edges (
			from_id TEXT NOT NULL,
			to_id TEXT NOT NULL,
			rel_type TEXT NOT NULL DEFAULT 'related',
			PRIMARY KEY (from_id, to_id, rel_type),
			FOREIGN KEY (from_id) REFERENCES memories(id) ON DELETE CASCADE,
			FOREIGN KEY (to_id) REFERENCES memories(id) ON DELETE CASCADE
		)`,
		`CREATE TABLE IF NOT EXISTS vectors (
			memory_id TEXT NOT NULL,
			chunk_idx INTEGER NOT NULL DEFAULT 0,
			embedding BLOB NOT NULL,
			PRIMARY KEY (memory_id, chunk_idx),
			FOREIGN KEY (memory_id) REFERENCES memories(id) ON DELETE CASCADE
		)`,
		`CREATE INDEX IF NOT EXISTS idx_memories_created ON memories(created_at DESC)`,
		`CREATE VIRTUAL TABLE IF NOT EXISTS memories_fts USING fts5(content, source)`,
	}
	for _, s := range stmts {
		if _, err := db.Exec(s); err != nil {
			return fmt.Errorf("migrate: %w", err)
		}
	}
	// Rebuild FTS index from memories table on every open (fast for personal scale).
	db.Exec(`DELETE FROM memories_fts`)
	db.Exec(`INSERT INTO memories_fts(rowid, content, source) SELECT rowid, content, source FROM memories`)
	return nil
}

func (d *DB) Add(content, source string, tags []string) (*Memory, error) {
	if tags == nil {
		tags = []string{}
	}
	tagsJSON, _ := json.Marshal(tags)
	m := &Memory{
		ID:        uuid.New().String(),
		Content:   content,
		Source:    source,
		Tags:      tags,
		CreatedAt: time.Now(),
	}
	res, err := d.db.Exec(
		`INSERT INTO memories (id, content, source, tags, created_at) VALUES (?, ?, ?, ?, ?)`,
		m.ID, m.Content, m.Source, string(tagsJSON), m.CreatedAt.Unix(),
	)
	if err != nil {
		return nil, err
	}
	rowid, _ := res.LastInsertId()
	d.db.Exec(`INSERT INTO memories_fts(rowid, content, source) VALUES (?, ?, ?)`, rowid, content, source)

	if fn := d.embedFunc(); fn != nil {
		go d.embedAndRelate(m.ID, content)
	}
	return m, nil
}

const chunkSize    = 1500
const chunkOverlap = 200

func chunkText(text string) []string {
	if len(text) <= chunkSize {
		return []string{text}
	}
	var chunks []string
	for start := 0; start < len(text); {
		end := start + chunkSize
		if end > len(text) {
			end = len(text)
		}
		chunks = append(chunks, text[start:end])
		if end == len(text) {
			break
		}
		start += chunkSize - chunkOverlap
	}
	return chunks
}

func (d *DB) embedAndRelate(id, content string) {
	fn := d.embedFunc()
	if fn == nil {
		return
	}
	chunks := chunkText(content)
	var firstVec []float32
	for i, chunk := range chunks {
		vec, err := fn(chunk)
		if err != nil || len(vec) == 0 {
			continue
		}
		d.db.Exec(`INSERT OR REPLACE INTO vectors (memory_id, chunk_idx, embedding) VALUES (?, ?, ?)`, id, i, float32ToBlob(vec))
		if firstVec == nil {
			firstVec = vec
		}
	}
	if firstVec == nil {
		return
	}
	scored, err := d.semanticSearchByVec(firstVec, 6, 0.75)
	if err != nil {
		return
	}
	for _, s := range scored {
		if s.ID == id {
			continue
		}
		d.Relate(id, s.ID, "similar")
	}
}

func (d *DB) Delete(id string) error {
	var rowid int64
	d.db.QueryRow(`SELECT rowid FROM memories WHERE id = ?`, id).Scan(&rowid)
	_, err := d.db.Exec(`DELETE FROM memories WHERE id = ?`, id)
	if err != nil {
		return err
	}
	if rowid > 0 {
		d.db.Exec(`DELETE FROM memories_fts WHERE rowid = ?`, rowid)
	}
	return nil
}

func (d *DB) GetByID(id string) (*Memory, error) {
	row := d.db.QueryRow(`SELECT id, content, source, tags, created_at FROM memories WHERE id = ?`, id)
	return scanMemory(row)
}

// Search tries semantic search when an embed func is available, falls back to FTS5.
func (d *DB) Search(query string, limit int) ([]Memory, error) {
	if strings.TrimSpace(query) == "" {
		return d.List(limit, 0)
	}
	if fn := d.embedFunc(); fn != nil {
		vec, err := fn(query)
		if err == nil && len(vec) > 0 {
			scored, err := d.semanticSearchByVec(vec, limit, 0.3)
			if err == nil && len(scored) > 0 {
				mems := make([]Memory, len(scored))
				for i, s := range scored {
					mems[i] = s.Memory
				}
				return mems, nil
			}
		}
	}
	return d.ftsSearch(query, limit)
}

// SemanticSearch embeds the query and returns scored results above minScore.
func (d *DB) SemanticSearch(query string, limit int, minScore float32) ([]ScoredMemory, error) {
	fn := d.embedFunc()
	if fn == nil {
		return nil, fmt.Errorf("no embedding model available")
	}
	vec, err := fn(query)
	if err != nil {
		return nil, err
	}
	return d.semanticSearchByVec(vec, limit, minScore)
}

func (d *DB) semanticSearchByVec(vec []float32, limit int, minScore float32) ([]ScoredMemory, error) {
	rows, err := d.db.Query(`SELECT memory_id, embedding FROM vectors`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	best := map[string]float32{}
	for rows.Next() {
		var memID string
		var blob []byte
		if err := rows.Scan(&memID, &blob); err != nil {
			continue
		}
		sim := cosineSim(vec, blobToFloat32(blob))
		if sim >= minScore && sim > best[memID] {
			best[memID] = sim
		}
	}

	var scored []ScoredMemory
	for memID, score := range best {
		m, err := d.GetByID(memID)
		if err != nil {
			continue
		}
		scored = append(scored, ScoredMemory{Memory: *m, Score: score})
	}
	sortScored(scored)
	if len(scored) > limit {
		scored = scored[:limit]
	}
	return scored, nil
}

func (d *DB) ftsSearch(query string, limit int) ([]Memory, error) {
	match := ftsEscape(query)
	if match == "" {
		return d.List(limit, 0)
	}
	rows, err := d.db.Query(`
		SELECT m.id, m.content, m.source, m.tags, m.created_at
		FROM memories_fts
		JOIN memories m ON memories_fts.rowid = m.rowid
		WHERE memories_fts MATCH ?
		ORDER BY rank
		LIMIT ?`, match, limit)
	if err != nil {
		// FTS match error (e.g. bad syntax) — fall back to LIKE
		return d.likeSearch(query, limit)
	}
	defer rows.Close()
	result, err := scanMemories(rows)
	if err != nil || len(result) == 0 {
		return d.likeSearch(query, limit)
	}
	return result, nil
}

func (d *DB) likeSearch(query string, limit int) ([]Memory, error) {
	terms := queryTerms(query)
	if len(terms) == 0 {
		return d.List(limit, 0)
	}
	var conds []string
	var args []any
	for _, t := range terms {
		like := "%" + t + "%"
		conds = append(conds, "(LOWER(content) LIKE ? OR LOWER(source) LIKE ?)")
		args = append(args, like, like)
	}
	args = append(args, limit)
	rows, err := d.db.Query(
		`SELECT id, content, source, tags, created_at FROM memories WHERE `+
			strings.Join(conds, " OR ")+` ORDER BY created_at DESC LIMIT ?`,
		args...,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanMemories(rows)
}

// ftsEscape converts a user query into a safe FTS5 OR MATCH expression.
// Terms are OR'd so a question like "what's my name?" finds "my name is william"
// because "my" and "name" appear in the memory even though "what's" does not.
func ftsEscape(q string) string {
	terms := queryTerms(q)
	if len(terms) == 0 {
		return ""
	}
	quoted := make([]string, 0, len(terms))
	for _, t := range terms {
		escaped := strings.ReplaceAll(t, `"`, `""`)
		quoted = append(quoted, `"`+escaped+`"*`)
	}
	return strings.Join(quoted, " OR ")
}

// queryTerms lowercases q, strips punctuation, and returns words >= 2 chars.
func queryTerms(q string) []string {
	var terms []string
	for _, t := range strings.Fields(strings.ToLower(q)) {
		t = strings.Trim(t, "?!.,;:'\"()[]{}")
		if len(t) >= 2 {
			terms = append(terms, t)
		}
	}
	return terms
}

func (d *DB) List(limit, offset int) ([]Memory, error) {
	rows, err := d.db.Query(
		`SELECT id, content, source, tags, created_at FROM memories ORDER BY created_at DESC LIMIT ? OFFSET ?`,
		limit, offset,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanMemories(rows)
}

func (d *DB) Relate(fromID, toID, relType string) error {
	if relType == "" {
		relType = "related"
	}
	_, err := d.db.Exec(
		`INSERT OR IGNORE INTO edges (from_id, to_id, rel_type) VALUES (?, ?, ?)`,
		fromID, toID, relType,
	)
	return err
}

func (d *DB) Unrelate(fromID, toID, relType string) error {
	_, err := d.db.Exec(
		`DELETE FROM edges WHERE from_id = ? AND to_id = ? AND rel_type = ?`,
		fromID, toID, relType,
	)
	return err
}

func (d *DB) GetRelated(id string, depth int) ([]Memory, []Edge, error) {
	if depth <= 0 {
		depth = 1
	}
	visited := map[string]bool{id: true}
	queue := []string{id}
	var allEdges []Edge

	for step := 0; step < depth && len(queue) > 0; step++ {
		var next []string
		for _, cur := range queue {
			rows, err := d.db.Query(
				`SELECT from_id, to_id, rel_type FROM edges WHERE from_id = ? OR to_id = ?`,
				cur, cur,
			)
			if err != nil {
				continue
			}
			for rows.Next() {
				var e Edge
				rows.Scan(&e.FromID, &e.ToID, &e.RelType)
				allEdges = append(allEdges, e)
				neighbor := e.ToID
				if neighbor == cur {
					neighbor = e.FromID
				}
				if !visited[neighbor] {
					visited[neighbor] = true
					next = append(next, neighbor)
				}
			}
			rows.Close()
		}
		queue = next
	}

	var mems []Memory
	for vid := range visited {
		if m, err := d.GetByID(vid); err == nil {
			mems = append(mems, *m)
		}
	}
	if mems == nil {
		mems = []Memory{}
	}
	if allEdges == nil {
		allEdges = []Edge{}
	}
	return mems, allEdges, nil
}

func (d *DB) GraphData() (*GraphData, error) {
	mems, err := d.List(1000, 0)
	if err != nil {
		return nil, err
	}
	rows, err := d.db.Query(`SELECT from_id, to_id, rel_type FROM edges`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var edges []Edge
	for rows.Next() {
		var e Edge
		rows.Scan(&e.FromID, &e.ToID, &e.RelType)
		edges = append(edges, e)
	}
	if edges == nil {
		edges = []Edge{}
	}
	return &GraphData{Nodes: mems, Edges: edges}, nil
}

// RebuildEmbeddings embeds all memories that don't yet have a vector.
// Runs in a goroutine; call and forget.
func (d *DB) RebuildEmbeddings() {
	go func() {
		fn := d.embedFunc()
		if fn == nil {
			return
		}
		rows, err := d.db.Query(`SELECT id, content FROM memories WHERE id NOT IN (SELECT memory_id FROM vectors)`)
		if err != nil {
			return
		}
		type item struct{ id, content string }
		var items []item
		for rows.Next() {
			var it item
			rows.Scan(&it.id, &it.content)
			items = append(items, it)
		}
		rows.Close()

		for _, it := range items {
			vec, err := fn(it.content)
			if err != nil || len(vec) == 0 {
				continue
			}
			d.db.Exec(`INSERT OR REPLACE INTO vectors (memory_id, embedding) VALUES (?, ?)`, it.id, float32ToBlob(vec))
		}
		// Auto-relate newly embedded memories
		for _, it := range items {
			d.embedAndRelate(it.id, it.content)
		}
	}()
}

func (d *DB) Stats() (total, vectorized int, err error) {
	d.db.QueryRow(`SELECT count(*) FROM memories`).Scan(&total)
	d.db.QueryRow(`SELECT count(DISTINCT memory_id) FROM vectors`).Scan(&vectorized)
	return
}

func (d *DB) Close() error {
	return d.db.Close()
}

func scanMemory(row *sql.Row) (*Memory, error) {
	var m Memory
	var createdAt int64
	var tagsJSON string
	if err := row.Scan(&m.ID, &m.Content, &m.Source, &tagsJSON, &createdAt); err != nil {
		return nil, err
	}
	m.CreatedAt = time.Unix(createdAt, 0)
	json.Unmarshal([]byte(tagsJSON), &m.Tags)
	if m.Tags == nil {
		m.Tags = []string{}
	}
	return &m, nil
}

func scanMemories(rows *sql.Rows) ([]Memory, error) {
	var result []Memory
	for rows.Next() {
		var m Memory
		var createdAt int64
		var tagsJSON string
		if err := rows.Scan(&m.ID, &m.Content, &m.Source, &tagsJSON, &createdAt); err != nil {
			continue
		}
		m.CreatedAt = time.Unix(createdAt, 0)
		json.Unmarshal([]byte(tagsJSON), &m.Tags)
		if m.Tags == nil {
			m.Tags = []string{}
		}
		result = append(result, m)
	}
	if result == nil {
		result = []Memory{}
	}
	return result, rows.Err()
}
