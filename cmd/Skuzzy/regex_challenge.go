package main

import (
	"fmt"
	regexp "github.com/ando-masaki/go-pcre"
	"github.com/tjarratt/babble"
	"log"
	"math/rand"
	"sort"
	"strings"
	"sync"
	"time"
)

var babbler = babble.NewBabbler()

type RegexChallenge struct {
	settings *ServerConfig
	Channel  string
	Timer    int64
	Regex    *regexp.Regexp
}

var RegexChallengeMutex = sync.RWMutex{}

var RegexChallengeChannels = make(map[string]RegexChallenge)

var maxSleep = (3600 * 6)

func chalelngePrompt() string {
	word := babbler.Babble()
	return fmt.Sprintf(`Respond with a regular expression that will match a random string of your choosing.Seed:%s`, word)
}
func RegexChallengeWorker() {
	babbler.Count = 1
	time.Sleep(30 * time.Second) // Initial sleep while things get set up
	for {
		RegexChallengeMutex.Lock()
		for k, v := range RegexChallengeChannels {
			log.Printf("[RegexChallengeWorker] Processing RegexChallenge for %s\n", k)
			prompt, text := FindPrompt(v.settings, "deepseek", v.Channel, "", "regex\nchallenge")
			if strings.HasSuffix(prompt, "/regex_challenge") { // && (v.Timer == 0 || ((time.Now().Unix() - v.Timer) > (3600*2))) {
				v.Timer = time.Now().Unix()
				req := DeepseekRequest{
					Channel:       v.Channel,
					Server:        v.settings.Name,
					sysprompt:     text,
					request:       chalelngePrompt(),
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
		sleep_time := time.Duration((3600 * 2) + rand.Intn(maxSleep))
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
			_, text := FindPrompt(challenge.settings, "deepseek", req.Channel, "", "regex\nchallenge")
			challenge.Timer = time.Now().Unix()
			RegexChallengeChannels[req.Server+"/"+req.Channel] = challenge
			req := DeepseekRequest{
				Channel:       req.Channel,
				Server:        req.Server,
				sysprompt:     text,
				request:       chalelngePrompt(),
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
			send_irc(req.Server, req.Channel, "Regex Challenge:`"+rex+"`")
		}
	}
}

func CheckRegexChallenge(server string, channel string, user string, query string) {
	RegexChallengeMutex.Lock()
	defer RegexChallengeMutex.Unlock()
	defer func() {
		if r := recover(); r != nil {
			log.Println("[CheckRegexChallenge] Recovered from panic:", r)
		}
	}()

	if challenge, ok := RegexChallengeChannels[server+"/"+channel]; ok {
		if strings.Contains(query, ">") {
			query_s := strings.Split(query, ">")
			if len(query_s) > 1 {
				query = strings.TrimSpace(query_s[1])
				log.Println("New query after bridge user removal:" + query)
			}

		}
		user = strings.ToLower(user)
		if challenge.Regex.MatchString(query) {
			log.Printf("POINTS:%d %d %d\n", maxSleep, int(time.Now().Unix()), int(challenge.Timer))
			points := maxSleep - int(int(time.Now().Unix())-int(challenge.Timer))
			points = points / (maxSleep / 100)
			points += 1
			RegexSolved(server, channel, user, points)
			regex_scores := RegexScores(server, channel, 86400*30)

			if score, ok := regex_scores[user]; ok {

				send_irc(server, channel, fmt.Sprintf("Regex challenge solved! Congrats %s! Your new score is: %d (+%d) 🎉", user, score, points))
			} else {
				log.Printf("[CheckRegexChallenge] Warning, updated user score but updated score was not found!!\n")
			}
			_, text := FindPrompt(challenge.settings, "deepseek", channel, "", "regex\nchallenge")
			challenge.Timer = time.Now().Unix()
			RegexChallengeChannels[server+"/"+channel] = challenge
			req := DeepseekRequest{
				Channel:       channel,
				Server:        server,
				sysprompt:     text,
				request:       chalelngePrompt(),
				reload:        false,
				reset:         false,
				OriginalQuery: "regex\nchallenge",
				User:          "",
			}
			sleep_time := time.Duration(10 + rand.Intn(60))
			time.Sleep(sleep_time * time.Second)
			DeepseekQueue <- req
			log.Printf("[CheckRegexChallenge] New challenge request queued because the previous one was solved.\n")

		} else {
			log.Printf("[CheckRegexChallenge] Non-matching regex:%s\n", query)
			points := maxSleep - int(int(time.Now().Unix())-int(challenge.Timer))
			if points == 0 {
				points = 0 - maxSleep
			} else {
				points = 0 - points
			}
			regex_scores := RegexScores(server, channel, 86400*30)
			if score, ok := regex_scores[strings.ToLower(user)]; score > 1000 && ok {
				points = score - int(float64(score)*float64(0.25))

			} else {
				points = points / (maxSleep / 100)
				points -= 1
			}
			log.Printf("POINTS:%d %d %d\n", maxSleep, int(time.Now().Unix()), int(challenge.Timer))

			RegexSolved(server, channel, user, points)
			regex_scores = RegexScores(server, channel, 86400*30)

			if score, ok := regex_scores[strings.ToLower(user)]; ok {

				send_irc(server, channel, fmt.Sprintf("Bad regex %s. Try harder! Your new score is: %d (%d)", user, score, points))
			} else {
				log.Printf("[CheckRegexChallenge] Warning, updated user -score but updated score was not found!!\n")
			}

		}
	}
}

type KV struct {
	K string
	V int
}

func SendRegexScores(Server string, Channel string) {
	RegexChallengeMutex.Lock()
	defer RegexChallengeMutex.Unlock()
	if _, ok := RegexChallengeChannels[Server+"/"+Channel]; ok {
		regex_scores := RegexScores(Server, Channel, 86400*30)
		response := "Top 10 Regex Scores for the past 30 days: "
		var scores_slice []KV
		for user, score := range regex_scores {
			scores_slice = append(scores_slice, KV{user, score})
		}
		sort.Slice(scores_slice, func(i, j int) bool {
			return scores_slice[i].V > scores_slice[j].V
		})
		if len(scores_slice) > 10 {
			scores_slice = scores_slice[:10]
		}
		for i, scores := range scores_slice {
			response = fmt.Sprintf("%s | %s (%d): %d  |", response, scores.K, i+1, scores.V)
		}
		send_irc(Server, Channel, response)
	}
}

func NextRegexChallenge(Server string, Channel string, user string) {
	RegexChallengeMutex.Lock()
	defer RegexChallengeMutex.Unlock()
	user = strings.ToLower(user)
	if challenge, ok := RegexChallengeChannels[Server+"/"+Channel]; ok {

		_, text := FindPrompt(challenge.settings, "deepseek", Channel, "", "regex\nchallenge")
		challenge.Timer = time.Now().Unix()
		RegexChallengeChannels[Server+"/"+Channel] = challenge
		req := DeepseekRequest{
			Channel:       Channel,
			Server:        Server,
			sysprompt:     text,
			request:       chalelngePrompt(),
			reload:        false,
			reset:         false,
			OriginalQuery: "regex\nchallenge",
			User:          "",
		}
		RegexSolved(Server, Channel, user, -50)
		regex_scores := RegexScores(Server, Channel, 86400*30)

		if score, ok := regex_scores[user]; ok {

			send_irc(Server, Channel, fmt.Sprintf("Too hard %s? Your new score is: %d (-50); new regex challenge ahead!", user, score))
		} else {
			log.Printf("[NextRegexChallenge] Warning, updated user score but updated score was not found!!\n")
		}
		//sleep_time := time.Duration(10  + rand.Intn(90))
		//time.Sleep(sleep_time * time.Second)
		DeepseekQueue <- req
		log.Printf("[NextRegexChallenge] New challenge request queued because user requested it.\n")

	}
}

const charset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789@#%&-_<> =,"

func generateRandomString(length int) string {
	seededRand := rand.New(rand.NewSource(time.Now().UnixNano()))
	b := make([]byte, length)
	for i := range b {
		b[i] = charset[seededRand.Intn(len(charset))]
	}
	return string(b)
}
