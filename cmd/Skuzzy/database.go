package main

import (
	"database/sql"
	"fmt"
	"log"
	"strings"

	_ "github.com/mattn/go-sqlite3"
)

var DB *sql.DB

var stopWords = map[string]struct{}{
	"a": {}, "about": {}, "an": {}, "and": {}, "are": {}, "as": {}, "at": {}, "be": {},
	"by": {}, "for": {}, "from": {}, "how": {}, "i": {}, "in": {}, "is": {}, "it": {},
	"of": {}, "on": {}, "or": {}, "that": {}, "the": {}, "this": {}, "to": {},
	"was": {}, "what": {}, "when": {}, "where": {}, "who": {}, "will": {}, "with": {},
	"my": {}, "it's": {}, "you": {}, "your": {},
}

/* Initialise the database and creates tables if they don't exist. */
func InitDB(filepath string) error {
	db, err := sql.Open("sqlite3", filepath)
	if err != nil {
		return fmt.Errorf("Failed to open database: %w", err)
	}

	/* Store embedding as a TEXT field containing JSON. */
	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS memories (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		text TEXT NOT NULL UNIQUE,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		);
	`)
	if err != nil {
		return fmt.Errorf("Failed to create memories table: %w", err)
	}

	/* Create FTS5 virtual table for full-text search. */
	_, err = db.Exec(`
		CREATE VIRTUAL TABLE IF NOT EXISTS memories_fts USING fts5(
		text,
		content='memories',
		content_rowid='id'
		);
	`)
	if err != nil {
		return fmt.Errorf("Failed to create FTS table: %w", err)
	}

	/* Triggers to keep FTS table in sync with memories table. */
	_, err = db.Exec(`
		CREATE TRIGGER IF NOT EXISTS memories_ai AFTER INSERT ON memories BEGIN
		INSERT INTO memories_fts(rowid, text) VALUES (new.id, new.text);
		END;
	`)
	if err != nil {
		return fmt.Errorf("Failed to create insert trigger: %w", err)
	}

	_, err = db.Exec(`
		CREATE TRIGGER IF NOT EXISTS memories_ad AFTER DELETE ON memories BEGIN
		INSERT INTO memories_fts(memories_fts, rowid, text) VALUES('delete', old.id, old.text);
		END;
	`)
	if err != nil {
		return fmt.Errorf("Failed to create delete trigger: %w", err)
	}

	_, err = db.Exec(`
		CREATE TRIGGER IF NOT EXISTS memories_au AFTER UPDATE ON memories BEGIN
		INSERT INTO memories_fts(memories_fts, rowid, text) VALUES('delete', old.id, old.text);
		INSERT INTO memories_fts(rowid, text) VALUES (new.id, new.text);
		END;
	`)
	if err != nil {
		return fmt.Errorf("Failed to create update trigger: %w", err)
	}

	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS regex_challenge_scores (
		user TEXT PRIMARY KEY,
		score INTEGER NOT NULL DEFAULT 0
		);
	`)
	if err != nil {
		return fmt.Errorf("Failed to create regex_challenge_scores table: %w", err)
	}

	DB = db
	log.Println("Database init success.")
	return nil
}

/* Stores a text string in the database. */
func AddMemory(text string) error {
	_, err := DB.Exec("INSERT OR IGNORE INTO memories (text) VALUES (?)", text)
	if err != nil {
		return fmt.Errorf("Failed to insert memory: %w", err)
	}

	log.Printf("Adding memory: %s", text)
	return nil
}

/* Finds the top N memories matching keywords from the query. */
func SearchMemories(query string, topN int) (string, error) {
	/* Split by space and use non-empty, non-stop-word strings. */
	rawKeywords := strings.Fields(strings.ToLower(query))
	var keywords []string
	for _, word := range rawKeywords {
		/* Remove punctuation from word. */
		word = strings.Trim(word, ".,!?;:")
		if _, isStopWord := stopWords[word]; !isStopWord && len(word) > 1 {
			keywords = append(keywords, word)
		}
	}
	if len(keywords) == 0 {
		return "", nil /* No keywrods to search for. */
	}

	/* Use FTS5 for searching. */
	ftsQuery := strings.Join(keywords, " OR ")
	queryStr := "SELECT text FROM memories_fts WHERE memories_FTS MATCH ? ORDER BY rank LIMIT ?"

	rows, err := DB.Query(queryStr, ftsQuery, topN)
	if err != nil {
		return "", fmt.Errorf("Failed to search memories: %w", err)
	}
	defer rows.Close()

	var memories []string
	for rows.Next() {
		var text string
		if err := rows.Scan(&text); err != nil {
			log.Printf("Failed to scan memory row: %v", err)
			continue
		}
		memories = append(memories, text)
	}

	if len(memories) == 0 {
		return "", nil /* No memories found. */
	}

	return "Here is some relevant context from my memory:\n- " + strings.Join(memories,"\n- "), nil
}
