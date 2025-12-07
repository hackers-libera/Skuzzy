package main

import (
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"
)

var Help = `
Help about the commands I support is organized in the format of 'Command - Description'
General commands:
@topic - Send help message about solving the topic
!help - Send this help message
CTF Challenge:
!ctf_scores - Display the CTF score stats for the channel
!<hintname> - Display CTF hints (will be sent to your pirvate messages)
Regex Challenge
"solution" - A message in a regex challenge enabled channel beginning and ending with a double quote will be evaluated for ap possible solution.
!next regex - Take a -50 point hint and generate a new challenge
!reges scores - Display regex challenge score stats
LLM commands:
<botname>, - A message starting with the bot's nick and a separator (such as a comma or a colon) will initiate a chat-completion session.
<botname>, @@sysprompt=default - An LLM query containing this will cause the bot to load the prompt after '=' and remember that prompt for the user.
<botname>, remind me ... - have the LLM remind you of something after some time.
<botname>, @reload ... - if '@reload' is mentioned in the LLM query, the sys prompt is reloaded (before evaluation)
<botname>, what's your version? - If the default prompt is enabled, asking it its version will display the current version.
`

var TopicHelp = `
Figure out what the topic is to the fullest extent possible.
DO NOT use AI or LLM tools to solve the topic.
Be ready to explain your solution and thinking process.
When you figure out the whole thing, send your response to me directly, I will check if that matches the expected answer and post a message congartulating you in the channel.
When an OP is around, they will ask you questions about your solution and add you to the #hackers libera community.
You can request hints by typing the hint name such as '@hint1', '@hint2' and '@hint3' is directly to me or in the channel.
Each attempt to get a hint will cost you 10 points out of a maximum of 100 points for the topic challenge (level 1). So messaging '@hint1' twice will cost you 20 points, and your final point will be 80 points.
Being part of the #hackers libera community means:
- You get +V privileges (cosmetic, unless we enable moderation in times of spam)
- You can send OPs your Github id to get an invite to our Github community (commit access to repos, access to create your own repos,etc..)
- You can contribute your own challenges to the CTF
- You can get an 'about/hackers/<your username>' cloak, if you want it.
Happy hacking and don't forget to have fun!
`

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
