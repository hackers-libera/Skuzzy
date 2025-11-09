package main

import (
	"log"
	"os"
	"strings"
	"time"
)

func server(configuration string) {
	log.Printf("Loading %v\n", configuration)
	settings, err  := LoadServerConfig(configuration)
	if err != nil {
		return
	}
	log.Printf("Loaded settings for %v:\n%v\n", settings.Name, settings)
	for {
		log.Printf("Connecting to %s\n", settings.Name)
		err := irc_connect(settings)
		if err != nil {

			log.Fatalf("%v\n", err)
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
	for {
		time.Sleep(1 * time.Second)
	}
}
