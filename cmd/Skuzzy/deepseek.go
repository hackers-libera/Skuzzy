package main

import (
	"context"
	"github.com/ando-masaki/go-pcre"
	deepseek "github.com/cohesion-org/deepseek-go"
	"io"
	"log"
	"net/http"
	"regexp"
	"strings"
	"sync" /* For RWMutex. */
	"time"
)

var rGreet, _ = regexp.Compile(`(?i)(hi+$|howdy$|hello$|hey+$|hai+$|ahoy|greetings$)[\. ]*`)

var SysPrompts = make(map[string]string)
var SysPromptsMutex = sync.RWMutex{}

type DeepseekRequest struct {
	Channel       string
	Server        string
	sysprompt     string
	request       string
	reload        bool
	reset         bool
	PromptName    string
	OriginalQuery string /* Store orig query for re-processing if needed. */
	User          string /* Going to need the user who sent the message too. */
}

var DeepseekQueue = make(chan DeepseekRequest)

func Deepseek(settings *ServerConfig, llm LLM) {
	model := ""
	switch llm.Model {

	case "deepseekchat":
		model = deepseek.DeepSeekChat
	default:
		log.Printf("[Deepseek] Error, usupported deepseek model %s\n", llm.Model)
	}
	client := deepseek.NewClient(settings.DeepseekAPIKey)
	for {
		req := <-DeepseekQueue
		log.Printf("[Deepseek]  Deepseek processing query [%s]: %v\n", llm.Name, req)
		if req.reload {
			log.Printf("[Deepseek]  Reloading sys prompts\n")
			LoadSysPrompts(settings)
		}

		/* Determine sysprompt to use based on PromptName. */
		currentSysPrompt := req.sysprompt
		switch req.PromptName {
		case "reminder_parse":
			/* Specific prompt for parsing reminders. */
			currentSysPrompt = settings.SysPrompts["reminder_parse"]
		case "reminder_change_parse":
			currentSysPrompt = settings.SysPrompts["reminder_change_parse"]
		}
		log.Printf("[Deepseek] System prompt:%s\n", currentSysPrompt)
		request := &deepseek.ChatCompletionRequest{
			Model: model,
			Messages: []deepseek.ChatCompletionMessage{
				{Role: deepseek.ChatMessageRoleSystem, Content: currentSysPrompt +
					"." + settings.SysPromptGlobalPrefix},
				{Role: deepseek.ChatMessageRoleUser, Content: req.request},
			},
		}

		ctx := context.Background()
		response, err := client.CreateChatCompletion(ctx, request)
		if err != nil {
			log.Printf("[Deepseek] Deepseek chat completion request returned an error: %v", err)
			continue
		}

		deepseek_response := response.Choices[0].Message.Content
		if req.PromptName == "reminder_parse" || req.PromptName == "reminder_change_parse" {
			ReminderParseQueue <- struct {
				Result      string
				OriginalReq DeepseekRequest
			}{Result: deepseek_response, OriginalReq: req}
		} else if strings.EqualFold(req.OriginalQuery, "regex\nchallenge") {
			log.Printf("[Deepseek] Regex challenge response:%s\n", deepseek_response)
			go NewRegexChallenge(req, deepseek_response)

		} else {
			log.Printf("[Deepseek] Deepseek response:%s\n", deepseek_response)
			send_irc(settings.Name, req.Channel, deepseek_response)
		}
	}
}

func FindPromptDeepseek(settings *ServerConfig, from_channel string, user string, query string) (string, string) {
	prefix := settings.Name + "/" + from_channel
	SysPromptsMutex.RLock()
	prompt := ""
	text := ""
	preferred_prompt := GetPreference(settings.Name, from_channel, user, "sysprompt")
	defer SysPromptsMutex.RUnlock()
	for key, value := range SysPrompts {
		if strings.HasPrefix(key, prefix) {
			log.Printf(">P>%s\n", key)
			if strings.HasSuffix(key, "/greet") && rGreet.MatchString(query) {
				log.Printf("[FindPromptDeepseek] Found greeting prompt %s for query:%s\n", key, query)
				prompt = key
				text = value
				break
			} else if strings.HasSuffix(key, "/regex_challenge") && query == "regex\nchallenge" {
				log.Printf("[FindPromptDeepseek] Found regex challenge prompt\n")
				prompt = key
				text = value
				break
			} else if preferred_prompt != "" && strings.HasSuffix(key, "/"+preferred_prompt) {
				log.Printf("[FindPromptDeepseek] Found user preferred prompt %s for query:%s\n", key, query)

				prompt = key
				text = value
				break
			} else if strings.HasSuffix(key, "/default") {
				log.Printf("[FindPromptDeepseek] Defaulting to prompt %s for query:%s\n", key, query)

				prompt = key
				text = value

			}
		}
	}

	return prompt, text
}

func LoadSysPrompts(settings *ServerConfig) {

	SysPromptsMutex.Lock()
	defer SysPromptsMutex.Unlock()
	for k, text := range settings.SysPrompts {
		if strings.HasPrefix(strings.ToLower(text), "https://") {
			text = HttpsFetch(text)
		}

		for _, channel := range settings.Channels {
			for _, promptName := range channel.SysPromptsEnabled {
				if strings.EqualFold(promptName, k) {
					SysPrompts[settings.Name+"/"+channel.Name+"/"+promptName] = text
					log.Printf("Loaded Prompt '%s/%s/%s' -> Prompt: %s\n", settings.Name, channel.Name, promptName, text)
					if strings.EqualFold(promptName, "regex_challenge") {
						RegexChallengeMutex.Lock()
						RegexChallengeChannels[settings.Name+"/"+channel.Name] = RegexChallenge{settings, channel.Name, time.Now().Unix(), pcre.MustCompile("^\n\n$"), "^$", false, 0}
						RegexChallengeMutex.Unlock()
					}
				}
			}
		}
	}
}

func HttpsFetch(url string) string {
	client := &http.Client{
		Timeout: 60 * time.Second,
	}

	response, err := client.Get(url)
	if err != nil {
		log.Printf("Error making an https request for %s:%s\n", url, err)
		return ""
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		log.Printf("Error making an https request for %s, Status not OK:%s\n", url, response.Status)
		return ""
	}

	body, err := io.ReadAll(response.Body)
	if err != nil {
		log.Printf("Error reading the HTTPS rersponse body for %s:%s\n", url, err)
		return ""
	}
	return string(body)
}
