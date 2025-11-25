package main

import (
	"fmt"
	"log"
	"math/rand"
	regexp "github.com/ando-masaki/go-pcre"
	"strings"
	"sync"
	"time"
)

type RegexChallenge struct {
	settings *ServerConfig
	Channel  string
	Timer    int64
	Regex    *regexp.Regexp
}

var RegexChallengeMutex = sync.RWMutex{}

var RegexChallengeChannels = make(map[string]RegexChallenge)

func RegexChallengeWorker() {
	time.Sleep(30 * time.Second) // Initial sleep while things get set up
	for {
		RegexChallengeMutex.Lock()
		for k, v := range RegexChallengeChannels {
			log.Printf("[RegexChallengeWorker] Processing RegexChallenge for %s\n", k)
			prompt, text := FindPrompt(v.settings, "deepseek", v.Channel, "regex\nchallenge")
			if strings.HasSuffix(prompt, "/regex_challenge") { // && (v.Timer == 0 || ((time.Now().Unix() - v.Timer) > (3600*2))) {
				v.Timer = time.Now().Unix()
				req := DeepseekRequest{
					Channel:       v.Channel,
					Server:        v.settings.Name,
					sysprompt:     text,
					request:       "Respond with a regular expression challenge",
					reload:        false,
					reset:         false,
					OriginalQuery: "regex\nchallenge",
					User:          "",
				}
				DeepseekQueue <- req
				log.Printf("[RegexChallengeWorker] New challenge request queued")
			} else {
				log.Printf("[RegexChallengeWorker] Bad prompt or timer not ready. Prompt:%s,Timer:%d\n", prompt, v.Timer)
			}
		}
		RegexChallengeMutex.Unlock()
		sleep_time := time.Duration((3600*2)+rand.Intn(3600*6))
		time.Sleep(sleep_time * time.Second)

	}
}

func NewRegexChallenge(req DeepseekRequest, response string) {
	RegexChallengeMutex.Lock()
	defer RegexChallengeMutex.Unlock()
	if challenge, ok := RegexChallengeChannels[req.Server+"/"+req.Channel]; ok {
		rex := strings.TrimRight(strings.TrimLeft(strings.TrimSpace(response), `/`), `/`)
		newRegex, err := regexp.Compile(rex)
		if err != nil || len(rex) > 380 {
			log.Printf("[NewRegexChallenge] Faulty regex from deepseek:\n%s\n%v\n", response, err)
			_, text := FindPrompt(challenge.settings, "deepseek", req.Channel, "regex\nchallenge")
			challenge.Timer = time.Now().Unix()
			req := DeepseekRequest{
				Channel:       req.Channel,
				Server:        req.Server,
				sysprompt:     text,
				request:       "Respond with a regular expression challenge",
				reload:        false,
				reset:         false,
				OriginalQuery: "regex\nchallenge",
				User:          "",
			}
			DeepseekQueue <- req
			log.Printf("[NewRegexChallenge] New challenge request queued because of a faulty regex.\n")
		} else {
			challenge.Regex = newRegex
			RegexChallengeChannels[req.Server+"/"+req.Channel] = challenge
			log.Printf("[NewRegexChallenge] New regex challenge for [%s/%s]:%s\n", req.Server, req.Channel, rex)
			send_irc(req.Server, req.Channel, "Regex Challenge:"+rex)
		}
	}
}

func CheckRegexChallenge(server string, channel string, user string, query string) {
	RegexChallengeMutex.Lock()
	defer RegexChallengeMutex.Unlock()
	if challenge, ok := RegexChallengeChannels[server+"/"+channel]; ok {
		if challenge.Regex.MatchString(query) {
			send_irc(server, channel, fmt.Sprintf("Regex challenge solved! Congrats %s! ", user))
			_, text := FindPrompt(challenge.settings, "deepseek", channel, "regex\nchallenge")
			challenge.Timer = time.Now().Unix()
			req := DeepseekRequest{
				Channel:       channel,
				Server:        server,
				sysprompt:     text,
				request:       "Respond with a regular expression challenge",
				reload:        false,
				reset:         false,
				OriginalQuery: "regex\nchallenge",
				User:          "",
			}
			DeepseekQueue <- req
			log.Printf("[CheckRegexChallenge] New challenge request queued because the previous one was solved.\n")

		} else {
			log.Printf("[CheckRegexChallenge] Non-matching regex:%s\n", query)
		}
	}
}
