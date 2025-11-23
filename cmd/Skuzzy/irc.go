package main

import (
	"crypto/tls"
	"encoding/base64"
	"fmt"
	//"io"
	"log"
	"net"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

type Connection struct {
	cx   net.Conn
	pong int64
}

var Connections = make(map[string]Connection)
var ConnectionsMutex = sync.RWMutex{}

func send_irc(server string, channel string, message string) {
	log.Printf("Sending to %v/%v:%v\n", server, channel, message)
	max := len(message)
	if max > 1600 {
		max = 1600
	}
	message = message[:max]
	remaining_message := ""
	if len(message) >= 397 {
		remaining_message = "..." + message[397:max]
		message = message[:397] + "..."

	}

	sanitizer := strings.NewReplacer(
		"\r", "",
		"\n", "",
		"\b", "",
	)

	message = sanitizer.Replace(message)

	msg := ""
	if channel != "" {
		msg = fmt.Sprintf("PRIVMSG %v :%s\r\n", channel, message)
	} else {

		msg = fmt.Sprintf("%s\r\n", message)
	}

	send_irc_raw(Connections[server], msg)

	if remaining_message != "" {
		send_irc(server, channel, remaining_message)
	}
}

func send_irc_raw(conn Connection, msg string) {
	ConnectionsMutex.RLock()
	defer ConnectionsMutex.RUnlock()
	_, err := conn.cx.Write([]byte(msg))
	if err != nil {
		log.Printf("Failed to send IRC message:%v\nError:%v\n", msg, err)
	}
	fmt.Printf("< %v", msg)
}
func send_irc_raw_secret(conn Connection, msg string) {
	ConnectionsMutex.RLock()
	defer ConnectionsMutex.RUnlock()
	_, err := conn.cx.Write([]byte(msg))
	if err != nil {
		log.Printf("Failed to send IRC message containing a secret:\nError:%v\n", err)
	}
}
func irc_connect(settings *ServerConfig) error {
	tlsConfig := &tls.Config{MinVersion: tls.VersionTLS12}
	conn, err := tls.Dial("tcp", settings.Host, tlsConfig)

	if err != nil {
		log.Printf("[%v] TLS connection to %v failed:%v", settings.Name, settings.Host, err)
		return err
	}
	ConnectionsMutex.Lock()
	Connections[settings.Name] = Connection{conn, time.Now().Unix()}
	ConnectionsMutex.Unlock()
	Server = settings.Name

	return nil
}

func CloseConnections() {
	ConnectionsMutex.Lock() /* We want a write lock to safely iterate and modify the map. */
	defer ConnectionsMutex.Unlock()
	for name, conn := range Connections {
		log.Printf("Closing %v\n", name)
		conn.cx.Close()
		/*
		 * Removed irc_disconnect in favour of directly handling connection closures
		 * to prevent deadlocks.
		 */
		delete(Connections, name)
	}
}

func mentioned(nick string, query string) bool {
	var rMention = regexp.MustCompile(`(?i)^` + nick + `[,:\- ]+`)
	log.Printf("Mention regex:%s\n", `(?i)^`+nick+`[,:\- ]+`)
	return rMention.MatchString(query)
}

/* Regex to parse reminder requests. */
var rReminder = regexp.MustCompile(`(?i)remind|reminder`)
var rListReminders = regexp.MustCompile(`(?i)(list|my) reminders`)
var rDeleteReminder = regexp.MustCompile(`(?i)(delete|remove) reminder (\d+)`)
var rChangeReminder = regexp.MustCompile(`(?i)(change|update) reminder (?:id )?(\d+)(?:[.:, ])?(.+)`)
var rRemember = regexp.MustCompile(`(?i)!remember (.+)`) /* Explicit mem storage. */

func Ping(settings *ServerConfig) {
	timeout := int64(499)
	sleep := time.Duration(int(timeout / 2))
	for {
		ConnectionsMutex.RLock()
		pong := Connections[settings.Name].pong
		ConnectionsMutex.RUnlock()

		latency := time.Now().Unix() - pong
		log.Printf("[PING] latency %d, pong %d\n", latency, pong)
		if pong > 0 && latency > timeout {
			log.Printf("[PING] %s has latency %d >= %d, starting a new server and breaking PING routine.\n", settings.Name, latency, timeout)
			go ServerRun(settings)
			break
		} else if pong > 0 && latency > timeout/2 {
			log.Printf("[PING] %s has latency %d >= %v.\n", settings.Name, latency, sleep)
			time.Sleep(sleep * time.Second)
			continue
		}
		send_irc_raw(Connections[settings.Name], "PING :HACK THE PLANET\r\n")
		time.Sleep(sleep * time.Second)
	}

}

func irc_loop(settings *ServerConfig) {
	log.Printf("IRC message loop for %s\n", settings.Name)
	ConnectionsMutex.RLock()
	cx := Connections[settings.Name].cx
	ConnectionsMutex.RUnlock()

	send_irc_raw(Connections[settings.Name], "CAP REQ :sasl\r\n")

	auth_sent := false
	breakout := false

	cx.SetReadDeadline(time.Now().Add(240 * time.Second))

	for {
		buf := make([]byte, 65535)
		nbytes, err := cx.Read(buf)
		if err != nil {
			log.Printf("[%v] Error receiving data from %s:%v\n", settings.Name, settings.Host, err)

			if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				log.Printf("[%s] Read timeout, sending PING to server.", settings.Name)
				send_irc_raw(Connections[settings.Name], fmt.Sprintf("PING %s\r\n", settings.Host))
				cx.SetReadDeadline(time.Now().Add(240 * time.Second))
				continue
			} else {
				break
			}
		} else if nbytes > 0 {
			cx.SetReadDeadline(time.Now().Add(240 * time.Second))
			for _, line := range strings.Split(string(buf), "\n") {
				response := strings.TrimSpace(line)
				words := strings.Split(response, " ")
				words_len := len(words)
				if len(words) > 1 && words[1] != "PRIVMSG" {
					select {
					case InteractQueue <- fmt.Sprintf("[>%s] %s\n", settings.Name, response):
					default:
					}
				}
				fmt.Printf(">%v\n", response)

				if !auth_sent && strings.HasSuffix(response, "ACK :sasl") {
					send_irc_raw(Connections[settings.Name], "AUTHENTICATE PLAIN\r\n")
				} else if !auth_sent && response == "AUTHENTICATE +" {
					PLAIN := []byte(fmt.Sprintf("\x00%s\x00%s", settings.SaslUser, settings.SaslPassword))
					auth_sent = true
					send_irc_raw_secret(Connections[settings.Name], fmt.Sprintf("AUTHENTICATE %s\r\n", base64.StdEncoding.EncodeToString(PLAIN)))
				} else if words_len >= 2 {
					if strings.HasPrefix(words[1], "90") && !slices.Contains([]string{"900", "901", "902", "903"}, words[1]) {
						auth_sent = false
						log.Printf("SASL authentication has failed!")
						breakout = true
					} else if words[1] == "903" {
						log.Printf("SASL authentication succesful")
						send_irc_raw(Connections[settings.Name], "CAP END\r\n")

						send_irc_raw(Connections[settings.Name], fmt.Sprintf("NICK %s\r\n", settings.Nick))
						send_irc_raw_secret(Connections[settings.Name], fmt.Sprintf("PRIVMSG NICKSERV :IDENTIFY %s %s\r\n", settings.Nick, settings.NickservPassword))
						send_irc_raw(Connections[settings.Name], fmt.Sprintf("USER %s 0 * :%s\r\n", settings.NickservUsername, settings.NickservUsername))
					} else if words[1] == "MODE" && words[0] == ":"+settings.Nick {

						for _, channel := range settings.Channels {
							send_irc_raw(Connections[settings.Name], fmt.Sprintf("JOIN %s\r\n", channel.Name))
							Channel = channel.Name
						}

					} else if words[0] == "PING" {
						send_irc_raw(Connections[settings.Name], strings.Replace(response, "PING", "PONG", 1)+"\r\n")
					} else if words[0] == "PONG" && strings.HasSuffix(response, ":HACK THE PLANET") {
						if conn, ok := Connections[settings.Name]; ok {
							conn.pong = time.Now().Unix()
							ConnectionsMutex.Lock()
							Connections[settings.Name] = conn
							ConnectionsMutex.Unlock()
							log.Println("!PONG!")
						} else {
							log.Println("PONG FAILURE")
						}
					} else if words[1] == "433" && len(settings.Nick) < 32 {
						if strings.HasSuffix(settings.Nick, "_") && len(settings.Nick) > 16 {
							settings.Nick = "_" + settings.Nick
						} else {
							settings.Nick = settings.Nick + "_"
						}
						send_irc_raw(Connections[settings.Name], fmt.Sprintf("NICK %s\r\n", settings.Nick))
					} else if words[1] == "PRIVMSG" && words_len >= 3 {
						from_channel := ""
						query := strings.TrimLeft(strings.Join(words[3:], " "), ":")
						user := strings.TrimLeft(words[0], ":")

						/* Handle messages from Discord bridge bots by stripping the <username> prefix. */
						r, _ := regexp.Compile(`^<.*?> `)
						if r.MatchString(query) {
							query = r.ReplaceAllString(query, "")
						}

						user = strings.Split(user, "!")[0]

						llm := ""
						var channel *ChannelConfig

						for i := range settings.Channels {
							ch := &settings.Channels[i]
							if strings.EqualFold(ch.Name, words[2]) {
								channel = ch
								from_channel = ch.Name
								privmsg := fmt.Sprintf("[>][%s/%s] <%s> %s\n", settings.Host, ch.Name, user, query)
								select {
								case InteractQueue <- privmsg:
								default:
								}
								log.Print(privmsg)
								llm = ch.LLM
								if len(ch.Backlog) > 10 {
									ch.Backlog = ch.Backlog[1:]
								}
								ch.Backlog = append(ch.Backlog, fmt.Sprintf("<%s> %s", user, query))
								break
							}
						}
						if strings.HasPrefix(query, `"`) && strings.HasSuffix(query, `"`) {
							go CheckRegexChallenge(settings.Name, from_channel, user, strings.Trim(query, `"`))
						}
						if channel != nil && strings.EqualFold(llm, "deepseek") {
							mention := mentioned(settings.Nick, query)
							if mention {
								/* Skuzzy mentioned, process the command. */
								log.Printf("Mentioned!\n")

								/* Handle explicit !remember command. */
								if matches := rRemember.FindStringSubmatch(query); len(matches) > 1 {
									if err := AddMemory(matches[1]); err != nil {
										log.Printf("Failed to add explicit memory: %v", err)
									}
									send_irc(settings.Name, from_channel, fmt.Sprintf("%s: Okay, I will " +
										"remember that: \"%s\"", user, matches[1]))
									return /* Don't process as regular LLM query. */
								}

								/* If bot mentioned and not !remember, add the query to memory. */
								if err := AddMemory(query); err != nil {
									log.Printf("Failed to add mentioned query to memory: %v", err)
								}

								nickPattern := `(?i)^` + regexp.QuoteMeta(settings.Nick) + `[:, ]*`
								r, _ := regexp.Compile(nickPattern)
								cleanQuery := r.ReplaceAllString(query, "")

								if rListReminders.MatchString(cleanQuery) {
									/* List reminders. */
									send_irc(settings.Name, from_channel, ListReminders(settings, user))
									log.Printf("Deepseek reminder listing query:\n%v\n", cleanQuery)
								} else if rDeleteReminder.MatchString(cleanQuery) {
									/* Delete a reminder. */
									matches := rDeleteReminder.FindStringSubmatch(cleanQuery)
									if len(matches) > 2 {
										id, err := strconv.Atoi(matches[2])
										if err == nil {
											send_irc(settings.Name, from_channel, DeleteReminder(settings,
												user, id))
											log.Printf("%s requested to delete reminder ID %d", user, id)
										}
									}
								} else if rChangeReminder.MatchString(cleanQuery) {
									/* Change reminder. */
									matches := rChangeReminder.FindStringSubmatch(cleanQuery)
									if len(matches) > 3 {
										id, err := strconv.Atoi(matches[2])
										newDetails := matches[3]
										if err == nil {
											req := DeepseekRequest{
												Channel:       from_channel,
												Server:        settings.Name,
												request:       fmt.Sprintf("Reminder ID: %d, Details: %s", id, newDetails),
												PromptName:    "reminder_change_parse",
												OriginalQuery: cleanQuery,
												User:          user,
											}
											DeepseekQueue <- req
											log.Printf("%s requested to change reminder ID %d with details: %s",
												user, id, newDetails)
										}
									}
								} else if rReminder.MatchString(cleanQuery) {
									req := DeepseekRequest{
										Channel:       from_channel,
										request:       cleanQuery,
										PromptName:    "reminder_parse",
										OriginalQuery: cleanQuery,
										User:          user,
									}
									DeepseekQueue <- req
									log.Printf("deepseek reminder parsing query:\n%v\n", req)
								} else {
									/* Handle other commands like @reset, @reload, or default chat. */
									reload := false
									reset := false
									if strings.Contains(cleanQuery, "@reset") {
										cleanQuery = strings.ReplaceAll(cleanQuery, "@reset", "")
										reset = true
									}
									if strings.Contains(cleanQuery, "@reload") {
										cleanQuery = strings.ReplaceAll(cleanQuery, "@reload", "")
										reload = true
									}

									/* Get the base prompt and text. */
									prompt, text := FindPrompt(settings, llm, from_channel, cleanQuery)

									/* Search memories and add to prompt. */
									memoryContext := ""
									var searchErr error
									if memoryContext, searchErr = SearchMemories(cleanQuery,
										5); searchErr != nil { /* Top 5 memories. */
										log.Printf("Error searching memories: %v", searchErr)
										memoryContext = "" /* Ensure it's empty on error. */
									}

									if memoryContext != "" {text=memoryContext+"\n"+text}

									text = strings.Replace(text, "{NICK}", settings.Nick, -1)
									text = strings.Replace(text, "{USER}", user, -1)
									text = strings.Replace(text, "{CHANNEL}", from_channel, -1)
									text = strings.Replace(text, "{VERSION}", "2.0", -1)
									text = strings.Replace(text, "{BACKLOG}", strings.Join(channel.Backlog, "\n"), -1)
									req := DeepseekRequest{
										Channel:       from_channel,
										Server:        settings.Name,
										sysprompt:     text, /* memoryContext prepended to 'text'. */
										request:       cleanQuery,
										reload:        reload,
										reset:         reset,
										OriginalQuery: cleanQuery,
										User:          user,
									}
									DeepseekQueue <- req
									log.Printf("Deepseek query [%s]:\n%v\n", prompt, req)
								}
							} else {
								prompt, _ := FindPrompt(settings, llm, from_channel, query)
								if len(prompt) == 0 || strings.HasSuffix(prompt, "/default") {
									log.Printf("Skipping LLM query due to default prompt and no mention.")
								} else {
									/* Get base prompt and text. */
									prompt, text := FindPrompt(settings, llm, from_channel, query)

									/* Search memories and add to prompt. */
									memoryContext := ""
									var searchErr error
									if memoryContext, searchErr = SearchMemories(query,
										5); searchErr != nil { /* Top 5 memories. */
										log.Printf("Error searching memories: %v", searchErr)
										memoryContext = "" /* ensure it's empty on error. */
									}

									if memoryContext != "" {text=memoryContext+"\n"+text}

									text = strings.Replace(text, "{NICK}", settings.Nick, -1)
									text = strings.Replace(text, "{USER}", user, -1)
									text = strings.Replace(text, "{CHANNEL}", from_channel, -1)
									text = strings.Replace(text, "{VERSION}", "2.0", -1)
									text = strings.Replace(text, "{BACKLOG}", strings.Join(channel.Backlog, "\n"), -1)

									req := DeepseekRequest{
										Channel:       from_channel,
										Server:        settings.Name,
										sysprompt:     text, /* memoryContext prepended to 'text'. */
										request:       query,
										reload:        false,
										reset:         false,
										OriginalQuery: query,
										User:          user,
									}
									DeepseekQueue <- req
									log.Printf("Deepseek query [%s]:\n%v\n", prompt, req)
								}
							}
						}
					}
				}
			}

		}
		if breakout {
			break
		}
	}
}

func FindPrompt(settings *ServerConfig, llm string, from_channel string, query string) (string, string) {

	if strings.EqualFold(llm, "deepseek") {
		return FindPromptDeepseek(settings, from_channel, query)
	}

	return "", ""
}
