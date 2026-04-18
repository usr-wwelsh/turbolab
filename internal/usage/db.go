package usage

import (
	"database/sql"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

type DB struct {
	db *sql.DB
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
	db, err := sql.Open("sqlite", filepath.Join(dir, "usage.db"))
	if err != nil {
		return nil, err
	}
	_, err = db.Exec(`CREATE TABLE IF NOT EXISTS requests (
		id                INTEGER PRIMARY KEY AUTOINCREMENT,
		ts                INTEGER NOT NULL,
		model             TEXT    NOT NULL DEFAULT '',
		prompt_tokens     INTEGER NOT NULL DEFAULT 0,
		completion_tokens INTEGER NOT NULL DEFAULT 0
	)`)
	if err != nil {
		db.Close()
		return nil, err
	}
	return &DB{db: db}, nil
}

func (d *DB) Record(model string, promptTokens, completionTokens int) {
	d.db.Exec(
		`INSERT INTO requests (ts, model, prompt_tokens, completion_tokens) VALUES (?, ?, ?, ?)`,
		time.Now().Unix(), model, promptTokens, completionTokens,
	)
}

type DayStat struct {
	Date             string `json:"date"`
	PromptTokens     int    `json:"prompt_tokens"`
	CompletionTokens int    `json:"completion_tokens"`
	Requests         int    `json:"requests"`
}

type Summary struct {
	Days             []DayStat `json:"days"`
	TotalPrompt      int       `json:"total_prompt_tokens"`
	TotalCompletion  int       `json:"total_completion_tokens"`
	TotalRequests    int       `json:"total_requests"`
}

func (d *DB) Summary(days int) (*Summary, error) {
	cutoff := time.Now().AddDate(0, 0, -days).Unix()
	rows, err := d.db.Query(`
		SELECT date(ts, 'unixepoch') AS day,
		       SUM(prompt_tokens), SUM(completion_tokens), COUNT(*)
		FROM requests
		WHERE ts >= ?
		GROUP BY day
		ORDER BY day ASC
	`, cutoff)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	s := &Summary{Days: []DayStat{}}
	for rows.Next() {
		var ds DayStat
		if err := rows.Scan(&ds.Date, &ds.PromptTokens, &ds.CompletionTokens, &ds.Requests); err != nil {
			return nil, err
		}
		s.Days = append(s.Days, ds)
		s.TotalPrompt += ds.PromptTokens
		s.TotalCompletion += ds.CompletionTokens
		s.TotalRequests += ds.Requests
	}
	return s, nil
}

func (d *DB) Close() error {
	return d.db.Close()
}
