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

func challengePrompt() string {
	word := babbler.Babble()
	return fmt.Sprintf(`Respond with a regular expression that will match a random string of your choosing.Seed:%s`, word)
}
func RegexChallengeWorker() {
	babbler.Count = 1
	time.Sleep(30 * time.Second)       // Initial sleep while things get set up
	const newChallengeThreshold = 1800 /* 30 minutes. */
	for {
		RegexChallengeMutex.Lock()
		for k, v := range RegexChallengeChannels {
			prompt, text := FindPrompt(v.settings, "deepseek", v.Channel, "", "regex\nchallenge")
			if !strings.HasSuffix(prompt, "/regex_challenge") {
				log.Printf("[RegexChallengeWorker] Bad prompt for %s. Prompt: '%s'. "+
					"Resetting timer to retry soon.\n", k, prompt)
				v.Timer = 0 /* Reset timer. Worker will try queueing new challenge later. */
				RegexChallengeChannels[k] = v
				continue
			}
			log.Printf("[RegexChallengeWorker] Channel: %s, v.Timer: %d, time.Now().Unix(): %d, "+
				"staleThreshold: %d\n", k, v.Timer, time.Now().Unix(), newChallengeThreshold)
			log.Printf("[RegexChallengeWorker] time elapsed: %d, Condition result: %t\n",
				(time.Now().Unix() - v.Timer), (v.Timer == 0 || ((time.Now().Unix() - v.Timer) > newChallengeThreshold)))
			/*
			 * A challenge is stale if it's been active > 2 hours or
			 * there's no timer (initial state).
			 */
			if v.Timer == 0 || ((time.Now().Unix() - v.Timer) > newChallengeThreshold) {
				log.Printf("[RegexChallengeWorker] Processing new RegexChallenge for %s\n", k)
				v.Timer = time.Now().Unix()
				RegexChallengeChannels[k] = v /* Update map with new timer. */
				req := DeepseekRequest{
					Channel:       v.Channel,
					Server:        v.settings.Name,
					sysprompt:     text,
					request:       challengePrompt(),
					reload:        false,
					reset:         false,
					OriginalQuery: "regex\nchallenge",
					User:          "",
				}
				DeepseekQueue <- req
				log.Printf("[RegexChallengeWorker] New challenge request queued for %s "+
					"because existing one is stale or missing.", k)
			}
		}
		RegexChallengeMutex.Unlock()
		/* Sleep for 30-60 mins. */
		sleep_time := time.Duration(1800 + rand.Intn(1800)) /* 30 mins + random (0-30 mins). */
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
				request:       challengePrompt(),
				reload:        false,
				reset:         false,
				OriginalQuery: "regex\nchallenge",
				User:          "",
			}
			DeepseekQueue <- req
			log.Printf("[NewRegexChallenge] New challenge request queued because of a faulty regex.\n")
		} else {
			/* Set timer when valid regex is received and ready to be announced. */
			challenge.Timer = time.Now().Unix()
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
		if len(query) > 1 && strings.HasPrefix(query, "\"") && strings.HasSuffix(query, "\"") {
			query = query[1 : len(query)-1]
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
			/* Set timer to 0 to trigger new challenge on the worker's next run. */
			challenge.Timer = 0
			RegexChallengeChannels[server+"/"+channel] = challenge
			log.Printf("[CheckRegexChallenge] Regex challenge solved in %s/%s. Next challenge "+
				"will be generated by worker.", server, channel)
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
		RegexSolved(Server, Channel, user, -50)
		regex_scores := RegexScores(Server, Channel, 86400*30)

		if score, ok := regex_scores[user]; ok {
			send_irc(Server, Channel, fmt.Sprintf("Too hard %s? Your new score is: %d (-50); new regex challenge ahead!", user, score))
		} else {
			log.Printf("[NextRegexChallenge] Warning, updated user score but updated score was not found!!\n")
		}
		/* Queue new challenge immediately. */
		_, text := FindPrompt(challenge.settings, "deeepseek", Channel, "", "regex\nchallenge")
		req := DeepseekRequest{
			Channel:       Channel,
			Server:        Server,
			sysprompt:     text,
			request:       challengePrompt(),
			OriginalQuery: "regex\nchallenge",
		}
		DeepseekQueue <- req
		/* Also update timer to now. */
		challenge.Timer = time.Now().Unix()
		RegexChallengeChannels[Server+"/"+Channel] = challenge
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
