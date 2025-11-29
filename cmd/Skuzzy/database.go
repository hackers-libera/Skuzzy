package main

import (
	"database/sql"
	"fmt"
	_ "github.com/mattn/go-sqlite3"
	"log"
	"regexp"
	"strings"
	"time"
)

var DB *sql.DB

/* Initialise the database and creates tables if they don't exist. */
func InitDB(filepath string) error {
	db, err := sql.Open("sqlite3", filepath)
	if err != nil {
		return fmt.Errorf("Failed to open database: %w", err)
	}

	/* Regex challenge */
	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS regex_challenge_scores (
		user TEXT,
		server TEXT,
		channel TEXT,
		score INTEGER NOT NULL DEFAULT 0,
		last_attempt INTEGER NOT NULL DEFAULT 0
		);
	`)
	if err != nil {
		return fmt.Errorf("Failed to create regex_challenge_scores table: %w", err)
	}

	/* CTF */
	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS ctf_scores (
		user TEXT PRIMARY KEY,
		level INTEGER NOT NULL DEFAULT 0,
		attempts INTEGER NOT NULL DEFAULT 0,
		hints INTEGER NOT NULL DEFAULT 0,
		last_attempt INTEGER NOT NULL DEFAULT 0

		);
	`)
	if err != nil {
		return fmt.Errorf("Failed to create ctf_scores table: %w", err)
	}

	DB = db
	log.Println("Database init success.")
	return nil
}

var cleanUser = regexp.MustCompile(`^[a-zA-Z0-9-_\.\{\}<>@!~\^\*&\(\)=` + "`]*$")

func RegexSolved(server string, channel string, user string, points int) {
	channel = strings.ToLower(channel)
	user = strings.ToLower(user)

	if !cleanUser.MatchString(user) {
		log.Printf("[RegexSolved] Warning, unable to update scorse for user %s, bad characters in the user name\n", user)
	}
	var score int
	err := DB.QueryRow("SELECT score FROM regex_challenge_scores WHERE user = ? AND server = ? AND channel = ?", user, server, channel).Scan(&score)
	if err != nil {
		if err == sql.ErrNoRows {
			log.Printf("No user found with ID %d\n", user)
		} else {
			log.Printf("[RegexSolved] Warning, unexpected error when searching for user in db:%v\n", err)
			return
		}
	}

	score += points

	statement, err := DB.Prepare("INSERT OR REPLACE INTO regex_challenge_scores (user, server, channel, score, last_attempt) VALUES (?, ?, ?, ?, ?)")
	if err != nil {
		log.Printf("[RegexSolved] Error, unable to prepare statement for updating %s's score:%v\n", user, err)
		return
	}
	defer statement.Close()

	_, err = statement.Exec(user, server, channel, score, int(time.Now().Unix()))
	if err != nil {
		log.Printf("[RegexSolved] Error, unable to Execute statement for updating %s's score:%v\n", user, err)
		return
	}
	log.Printf("[RegexSolved] Updated %s's score with %d\n", user, score)
}

func RegexScores(server string, channel string, oldest int) map[string]int {
	var scores = make(map[string]int)
	channel = strings.ToLower(channel)
	rows, err := DB.Query("SELECT user, score, last_attempt FROM regex_challenge_scores WHERE server = ? AND channel = ?", server, channel)
	if err != nil {
		if err == sql.ErrNoRows {
			log.Printf("[RegexScores] No rows in the regex challenge scores table")
			return scores
		} else {
			log.Printf("[RegexScores] Warning, unexpected error when searching for scores:%v\n", err)
			return scores
		}
	}
	defer rows.Close()

	var user string
	var score int
	var last_attempt int

	for rows.Next() {
		err := rows.Scan(&user, &score, &last_attempt)
		if err != nil {
			log.Printf("[RegexScores] Error reading row:%v\n", err)
			break
		}
		if (int(time.Now().Unix()) - last_attempt) > oldest {
			log.Printf("[RegexScores] Skipping user/score %s/%d, because %d is more than %s seconds old\n", user, score, last_attempt, oldest)
			continue
		}
		scores[user] = score
	}

	return scores
}
