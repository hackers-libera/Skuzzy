package main

import (
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func setupLogging(LogFile string) {
	logPath, err := filepath.Abs(LogFile)
	if err != nil {
		log.Fatalf("Failed to open global log file; absolute path resolution failed: %v", err)
	}
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		log.Fatalf("Failed to open global log file: %v", err)
	}
	defer logFile.Close()

	multiWriter := io.MultiWriter(os.Stdout, logFile)

	log.SetOutput(multiWriter)
	log.Println("Logging started")
}

func ServerRun(settings *ServerConfig) {
	log.Printf("Connecting to %s (%s)\n", settings.Name, settings.Host)
	err := irc_connect(settings)
	if err != nil {

		log.Printf("Error connecting to %s (%s):%v\nReconnecting in 5 seconds...", settings.Name, settings.Host, err)
		return
	}

	LoadSysPrompts(settings)

	err = LoadReminders(settings)
	if err != nil {
		log.Printf("Error loading reminders for %s: %v", settings.Name, err)
	}

	for _, llm := range settings.LLMS {
		if strings.EqualFold(llm.Type, "deepseek") {
			log.Printf("Starting LLM go routine for %s\n", llm.Name)
			go Deepseek(settings, llm)
		}
	}
	//go Ping(settings)
	irc_loop(settings)
}
func server(configuration string) {
	log.Printf("Loading %v\n", configuration)
	settings, err := LoadServerConfig(configuration)
	if err != nil {
		return
	}
	go ReminderHandler(settings) /* Start reminder handler goroutine for this server. */
	log.Printf("Loaded settings for %v\n", settings.Name)
	for {
		ServerRun(settings)
		time.Sleep(5 * time.Second)
	}
}

func main() {
	log.Println("[Main] Starting up.")

	var servers []string
	db_path := "skuzzy.db"
	for i, v := range os.Args {
		if i > 0 {
			if strings.HasSuffix(v, ".sock") {
				go interact(v) // Keyboard interaction with the bot operator
				continue
			}
			if strings.HasSuffix(v, ".log") {
				setupLogging(v)
				continue
			}
			if strings.HasSuffix(v, ".db") {
				db_path = v
				continue
			}
			servers = append(servers, v)
		}

	}
	if err := InitDB(db_path); err != nil {
		log.Fatalf("[Main] Failed to initialize database: %v", err)
	}
	for _, s := range servers {
		go server(s)
	}
	log.Printf("[Main] Started server go routines\n")
	go RegexChallengeWorker()
	// Sleep forever, exit when instructed or ctrl+c
	defer CloseConnections() /* Ensure connections are closed on exit. */
	for {
		time.Sleep(1 * time.Second)
	}
}
