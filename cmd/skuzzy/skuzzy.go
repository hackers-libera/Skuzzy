package main

import (
	"log"
	"os"
	"io"
	"strings"
	"time"
	"path/filepath"
)

func setupLogging(settings *ServerConfig) {
	logPath,err := filepath.Abs(settings.ServerLogFile)
  if err != nil {
		log.Fatalf("Failed to open server log file for %s, absolute path resolution failed: %v",settings.Name, err)
	}
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		log.Fatalf("Failed to open server log file for %s: %v",settings.Name, err)
	}
	defer logFile.Close()

	multiWriter := io.MultiWriter(os.Stdout, logFile)

	log.SetOutput(multiWriter)
}
func server(configuration string) {
	log.Printf("Loading %v\n", configuration)
	settings, err  := LoadServerConfig(configuration)
	if err != nil {
		return
	}
	setupLogging(settings)
	log.Printf("Loaded settings for %v:\n%v\n", settings.Name, settings)
	for {
		log.Printf("Connecting to %s (%s)\n", settings.Name,settings.Host)
		err := irc_connect(settings)
		if err != nil {

			log.Printf("Error connecting to %s (%s):%v\nReconnecting in 5 seconds...",settings.Name,settings.Host, err)
			time.Sleep(5 * time.Second)
			continue
		}

		LoadSysPrompts(settings)

		for _, llm := range settings.LLMS {
			if strings.EqualFold(llm.Type, "deepseek") {
				log.Printf("Starting LLM go routine for %s\n", llm.Name)
				go Deepseek(settings, llm)
			}
		}
		irc_loop(settings)
		time.Sleep(5 * time.Second)
	}
}

func main() {

	go interact() // Keyboard interaction with the bot operator

	for i, v := range os.Args {
		if i > 0 {
			go server(v)
		}

	}
	log.Printf("Started server go routines\n")

	// Sleep forever, exit when instructed or ctrl+c
	defer CloseConnections() /* Ensure connections are closed on exit. */
	for {
		time.Sleep(1 * time.Second)
	}
}
