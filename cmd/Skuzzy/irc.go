package main

import (
	"crypto/tls"
	"encoding/base64"
	"fmt"
	"io"
	"log"
	"net"
	"regexp"
	"slices"
	"strings"
	"time"
	"sync"
)

type Connection struct {
	cx net.Conn
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
	ConnectionsMutex.RLock()
	send_irc_raw(Connections[server], msg)
	ConnectionsMutex.RUnlock()

	if remaining_message != "" {
		send_irc(server, channel, remaining_message)
	}
}

func send_irc_raw(conn Connection, msg string) {
	_, err := conn.cx.Write([]byte(msg))
	if err != nil {
		log.Printf("Failed to send IRC message:%v\nError:%v\n", msg, err)
	}
	fmt.Printf("< %v", msg)
}
func send_irc_raw_secret(conn Connection, msg string) {
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
	Connections[settings.Name] = Connection{conn}
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

	return strings.HasPrefix(strings.ToLower(query), strings.ToLower(nick)) 
}

/* Regex to parse reminder requests. */
var rReminder = regexp.MustCompile(`(?i)remind|reminder`)

func irc_loop(settings *ServerConfig) {
	log.Printf("IRC message loop for %s\n", settings.Name)
	ConnectionsMutex.RLock()
	cx := Connections[settings.Name].cx
	ConnectionsMutex.RUnlock()

	ConnectionsMutex.RLock()
	send_irc_raw(Connections[settings.Name], "CAP REQ :sasl\r\n")
	ConnectionsMutex.RUnlock()

	auth_sent := false
	breakout := false

	cx.SetReadDeadline(time.Now().Add(240*time.Second))

	for {
		buf := make([]byte, 65535)
		nbytes, err := cx.Read(buf)
		if err != nil {
			log.Printf("[%v] Error receiving data from %s:%v\n", settings.Name, settings.Host, err)
			if err == io.EOF {
				break
			}
			if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				log.Printf("[%s] Read timeout, sending PING to server.", settings.Name)
				ConnectionsMutex.RLock()
				send_irc_raw(Connections[settings.Name], fmt.Sprintf("PING %s\r\n", settings.Host))
				ConnectionsMutex.RUnlock()
				cx.SetReadDeadline(time.Now().Add(240*time.Second))
				continue
			}
		} else if nbytes > 0 {
			cx.SetReadDeadline(time.Now().Add(240*time.Second))
			for _, line := range strings.Split(string(buf), "\n") {
				response := strings.TrimSpace(line)
				words := strings.Split(response, " ")
				words_len := len(words)

				fmt.Printf(">%v\n", response)

				if !auth_sent && strings.HasSuffix(response, "ACK :sasl") {
					ConnectionsMutex.RLock()
					send_irc_raw(Connections[settings.Name], "AUTHENTICATE PLAIN\r\n")
					ConnectionsMutex.RUnlock()
				} else if !auth_sent && response == "AUTHENTICATE +" {
					PLAIN := []byte(fmt.Sprintf("\x00%s\x00%s", settings.SaslUser, settings.SaslPassword))
					auth_sent = true
					ConnectionsMutex.RLock()
					send_irc_raw_secret(Connections[settings.Name], fmt.Sprintf("AUTHENTICATE %s\r\n", base64.StdEncoding.EncodeToString(PLAIN)))
					ConnectionsMutex.RUnlock()
				} else if words_len >= 2 {
					if strings.HasPrefix(words[1], "90") && !slices.Contains([]string{"900", "901", "902", "903"}, words[1]) {
						auth_sent = false
						log.Printf("SASL authentication has failed!")
						breakout = true
					} else if words[1] == "903" {
						log.Printf("SASL authentication succesful")
						ConnectionsMutex.RLock()
						send_irc_raw(Connections[settings.Name], "CAP END\r\n")
						ConnectionsMutex.RUnlock()

						ConnectionsMutex.RLock()
						send_irc_raw(Connections[settings.Name], fmt.Sprintf("NICK %s\r\n", settings.Nick))
						ConnectionsMutex.RUnlock()
						ConnectionsMutex.RLock()
						send_irc_raw_secret(Connections[settings.Name], fmt.Sprintf("PRIVMSG NICKSERV :IDENTIFY %s %s\r\n", settings.Nick, settings.NickservPassword))
						ConnectionsMutex.RUnlock()
						ConnectionsMutex.RLock()
						send_irc_raw(Connections[settings.Name], fmt.Sprintf("USER %s 0 * :%s\r\n", settings.NickservUsername, settings.NickservUsername))
						ConnectionsMutex.RUnlock()
					} else if words[1] == "MODE" && words[0] == ":"+settings.Nick {

						for _, channel := range settings.Channels {
							ConnectionsMutex.RLock()
							send_irc_raw(Connections[settings.Name], fmt.Sprintf("JOIN %s\r\n", channel.Name))
							ConnectionsMutex.RUnlock()
							Channel = channel.Name
						}

					} else if words[0] == "PING" {
						ConnectionsMutex.RLock()
						send_irc_raw(Connections[settings.Name], strings.Replace(response, "PING", "PONG", 1)+"\r\n")
						ConnectionsMutex.RUnlock()
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
								log.Printf("[%s/%s] %s\n", settings.Host, ch.Name, query)
								llm = ch.LLM
								if len(ch.Backlog) > 10 {
									ch.Backlog = ch.Backlog[1:]
								}
								ch.Backlog = append(ch.Backlog, fmt.Sprintf("<%s> %s", user, query))
								break
							}
						}
						if channel != nil && strings.EqualFold(llm, "deepseek") {
							reset := false

							var handledAsReminder bool
							/* Check for reminder keyword. */
							if rReminder.MatchString(query) {
								req := DeepseekRequest {
									channel:				from_channel,
									request:				query,
									PromptName:			"reminder_parse",
									OriginalQuery:	query,
									User:						user,
								}
								DeepseekQueue <- req
								log.Printf("Deepseek reminder parsing query:\n%v\n", req)
								handledAsReminder = true
							}
							
							if !handledAsReminder {
								reload := false
								mention := mentioned(settings.Nick, query)

								if strings.Contains(query, "@reset") {
									query = strings.ReplaceAll(query, "@reset", "")
									reset = true
								}
								if strings.Contains(query, "@reload") {
									query = strings.ReplaceAll(query, "@reload", "")
									reload = true
								}
								prompt, text := FindPrompt(settings, llm, from_channel, query)
								text = strings.Replace(text, "{NICK}", settings.Nick,-1)
								text = strings.Replace(text, "{USER}", user,-1)
								text = strings.Replace(text, "{CHANNEL}", from_channel,-1)
								text = strings.Replace(text, "{BACKLOG}", strings.Join(channel.Backlog,"\n"),-1)
								if !mention && strings.HasSuffix(prompt, "/default") {
									log.Printf("Skipping LLM query due to default prompt, mention, or " +
														"reminder request.")
								} else {

									req := DeepseekRequest{from_channel, text, query, reload, reset, "", query, user}
									DeepseekQueue <- req
									log.Printf("Deepseek query:\n%v\n", req)
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
