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
	CTF  *CTFConfig
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

	ConnectionsMutex.RLock()
	conn, ok := Connections[server]
	ConnectionsMutex.RUnlock()
	if !ok || conn.cx == nil {
		log.Printf("Error: no valid IRC connection found for '%s'. Message not sent: %s", server, message)
		return /* If no valid connection. Exit. */
	}

	msg := ""
	if channel != "" {
		msg = fmt.Sprintf("PRIVMSG %v :%s\r\n", channel, message)
	} else {

		msg = fmt.Sprintf("%s\r\n", message)
	}

	send_irc_raw(conn, msg)

	if remaining_message != "" {
		send_irc(server, channel, remaining_message)
	}
}

func send_irc_raw(conn Connection, msg string) {
	if len(msg) < 1 {return}
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

	if len(settings.CtfConfigPath) > 0 {
		ctfconfig, err := LoadCTFConfig(settings.CtfConfigPath)
		if err == nil {
			Connections[settings.Name] = Connection{conn, time.Now().Unix(), ctfconfig}
		} else {
			Connections[settings.Name] = Connection{conn, time.Now().Unix(), nil}
		}
	} else {
		Connections[settings.Name] = Connection{conn, time.Now().Unix(), nil}
	}

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
						_query := query
						user := strings.TrimLeft(words[0], ":")

						user = strings.Split(user, "!")[0]
						/* Handle messages from Discord bridge bots by stripping the <username> prefix. */

						bridge_user, _query := BridgeUser(query)
						if bridge_user != "" {
							for _, relay_bot := range settings.RelayBots {
								if strings.EqualFold(bridge_user, relay_bot) {
									user = bridge_user
									query = _query
									break
								}
							}
						}
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
						if channel != nil {
							if strings.EqualFold(query, "!help") {
								sendHelp(settings, from_channel, user)
								continue
							}
							if strings.EqualFold(query, "!topic") {
								sendTopicHelp(settings, from_channel, user)
								continue
							}

							if strings.HasPrefix(query, `"`) && strings.HasSuffix(query, `"`) {
								log.Printf("Debug: query has double quotes:[%s]\n", query)
								go CheckRegexChallenge(settings.Name, from_channel, user, strings.Trim(query, `"`))
							} else {
								log.Printf("Debug: query has NO double quotes:[%s]\n", query)
							}
							if strings.EqualFold(query, "!regex scores") {
								SendRegexScores(settings.Name, from_channel)
								continue
							}
							if strings.EqualFold(query, "!next regex") {
								NextRegexChallenge(settings.Name, from_channel, user)
								continue
							}
							if strings.Contains(query, "@@") {
								query = ParsePreferences(settings.Name, from_channel, user, query)
							}
							if strings.HasPrefix(query, "!") {

								for k, ctf := range Connections[settings.Name].CTF.CTFFlags {
									for hintname, hint := range ctf.Hints {
										if strings.EqualFold("!"+hintname, query) && strings.EqualFold(from_channel, ctf.Channel) {
											send_irc(settings.Name, user, hint)
											send_irc(settings.Name, from_channel, "Check your private messages "+user+", I just sent you the hint:"+hintname+" for "+k+".")
											CTFHintTaken(settings, ctf, user)
											break
										}
									}
								}
								if strings.EqualFold(query, "!ctf_scores") {
									SendCTFScores(settings.Name, from_channel)

								}
							}
						} else if strings.EqualFold(settings.Nick, words[2]) {
							handlePM(settings, user, query)
							continue
						}
						if channel != nil && strings.EqualFold(llm, "deepseek") {
							mention := mentioned(settings.Nick, query)
							if mention {
								/* Skuzzy mentioned, process the command. */
								log.Printf("Mentioned!\n")
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
										Server:        settings.Name,
										request:       cleanQuery,
										PromptName:    "reminder_parse",
										OriginalQuery: cleanQuery,
										User:          user,
									}
									DeepseekQueue <- req
									log.Printf("Deepseek reminder parsing query:\n%v\n", req)
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
									prompt, text := FindPrompt(settings, llm, from_channel, user, cleanQuery)
									text = strings.Replace(text, "{NICK}", settings.Nick, -1)
									text = strings.Replace(text, "{USER}", user, -1)
									text = strings.Replace(text, "{CHANNEL}", from_channel, -1)
									text = strings.Replace(text, "{VERSION}", "2.0", -1)
									text = strings.Replace(text, "{BACKLOG}", strings.Join(channel.Backlog, "\n"), -1)
									req := DeepseekRequest{
										Channel:       from_channel,
										Server:        settings.Name,
										sysprompt:     text,
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
								prompt, _ := FindPrompt(settings, llm, from_channel, user, query)
								if len(prompt) == 0 || !mention { //|| strings.HasSuffix(prompt, "/default") {
									log.Printf("Skipping LLM query due to null prompt and no mention.")
								} else {
									prompt, text := FindPrompt(settings, llm, from_channel, user, query)
									text = strings.Replace(text, "{NICK}", settings.Nick, -1)
									text = strings.Replace(text, "{USER}", user, -1)
									text = strings.Replace(text, "{CHANNEL}", from_channel, -1)
									text = strings.Replace(text, "{VERSION}", "2.0", -1)
									text = strings.Replace(text, "{BACKLOG}", strings.Join(channel.Backlog, "\n"), -1)

									req := DeepseekRequest{
										Channel:       from_channel,
										Server:        settings.Name,
										sysprompt:     text,
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

func FindPrompt(settings *ServerConfig, llm string, from_channel string, user string, query string) (string, string) {

	if strings.EqualFold(llm, "deepseek") {
		return FindPromptDeepseek(settings, from_channel, user, query)
	}

	return "", ""
}

var rPrefs = regexp.MustCompile(`@@(\w+)=(\w+)`)

func ParsePreferences(server, channel, user, query string) string {

	matches := rPrefs.FindAllStringSubmatch(query, -1)

	for _, match := range matches {
		log.Printf("[ParsePreferences] Parsing preference %s/%s[%s]\n", match[0], match[1], match[2])
		if !CleanUser.MatchString(match[1]) {
			log.Printf("[ParsePreferences] Warning, invalid preference for query %s:%s\n", query, match[1])

		}
		query = strings.Replace(query, match[0], "", -1)
		SetPreference(server, channel, user, match[1], match[2])
	}

	return query

}

var rBridge = regexp.MustCompile(`^<([^>]+)>.*$`)

func BridgeUser(query string) (string, string) {

	matches := rBridge.FindAllStringSubmatch(query, -1)
	log.Printf("[BridgeUser] Debug:%s\n", query)
	for _, match := range matches {
		log.Printf("[BridgeUser] Found [%s]:%s\n", match[1], match[0])
		return match[1], strings.TrimSpace(strings.Replace(query, "<"+match[1]+">", "", -1))

	}
	return "", query

}

func handlePM(settings *ServerConfig, user, query string) {

	if strings.HasSuffix(query, "help") {
		sendHelp(settings, user, user)
	}
	if strings.HasSuffix(query, "topic") {
		sendTopicHelp(settings, user, user)
	}
	if Connections[settings.Name].CTF != nil {
		for k, ctf := range Connections[settings.Name].CTF.CTFFlags {
			if strings.EqualFold(query, ctf.Flag) {
				CTFSolved(settings, k, ctf, user)
				solve_message := fmt.Sprintf("Congrats to %s on finding the flag for '%s'!! 🎉🎉🎉. New level:%d",
					user, k, ctf.Level)
				send_irc(settings.Name, ctf.Channel, solve_message)
			}
			for hintname, hint := range ctf.Hints {
				if strings.EqualFold("!"+hintname, query) {
					send_irc(settings.Name, user, hint)
					CTFHintTaken(settings, ctf, user)
				}
			}
		}
	}
}

func SendCTFScores(server, channel string) {
	ctfscores := CTFScores(server, channel)
	response := "CTF Scores for " + channel + ": "
	for user, score_msg := range ctfscores {
		response = fmt.Sprintf("%s | %s: %s |", response, user, score_msg)
	}
	send_irc(server, channel, response)
}
func sendHelp(settings *ServerConfig, target, user string) {
	send_irc(settings.Name, target, target+", Check your private messages. Sending you my usage instructions.")

	for _, v := range strings.Split(Help, "\n") {
		send_irc(settings.Name, user, v)
		time.Sleep(1 * time.Second)
	}
}

func sendTopicHelp(settings *ServerConfig, target, user string) {
	send_irc(settings.Name, target, target+", Check your private messages. Sending you help about the topic CTF challenge.")
	for _, v := range strings.Split(TopicHelp, "\n") {
		send_irc(settings.Name, user, v)
		time.Sleep(1 * time.Second)
	}
}
