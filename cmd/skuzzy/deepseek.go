package main

import (
	"context"
	"log"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"
    deepseek "github.com/cohesion-org/deepseek-go"

)


var rGreet, _ = regexp.Compile(`(?i)(hi+$|howdy$|hello$|hey+$|hai+$|ahoy|greetings$)[\. ]*`)

var SysPrompts = make(map[string]string)

type DeepseekRequest struct {
	channel   string
	sysprompt string
	request   string
	reload    bool
	reset     bool
}

var DeepseekQueue = make(chan DeepseekRequest)

func Deepseek(settings ServerConfig, llm LLM) {
	model := ""
	switch llm.Model {

	case "deepseekchat":
		model = deepseek.DeepSeekChat
	default:
		log.Printf("Error, usupported deepseek model %s\n", llm.Model)
	}
	client := deepseek.NewClient(settings.DeepseekAPIKey)
	for {
		req := <-DeepseekQueue
		log.Printf("Deepseek processing query [%s]: %v\n", llm.Name, req)
		if req.reload {
			log.Printf("Reloading sys prompts\n")
			LoadSysPrompts(settings)
		}
		request := &deepseek.ChatCompletionRequest{
			Model: model,
			Messages: []deepseek.ChatCompletionMessage{
				{Role: deepseek.ChatMessageRoleSystem, Content: req.sysprompt + "." + settings.SysPromptGlobalPrefix},
				{Role: deepseek.ChatMessageRoleUser, Content: req.request},
			},
		}

		ctx := context.Background()
		response, err := client.CreateChatCompletion(ctx, request)
		if err != nil {
			log.Printf("Deepseek chat completion request returned an error: %v", err)
			continue
		}

		deepseek_response := response.Choices[0].Message.Content
		log.Printf("Deepseek response:%s\n", deepseek_response)
		send_irc(settings.Name, req.channel, deepseek_response)
	}
}

func FindPromptDeepseek(settings ServerConfig, from_channel string, query string) (string, string) {
	prefix := settings.Name + "/" + from_channel
	for key, value := range SysPrompts {
		if strings.HasPrefix(key, prefix) {
			if strings.HasSuffix(key, "/greet") && rGreet.MatchString(query) {
				log.Printf("Found greeting prompt %s for query:%s\n", key, query)
				return key, value
			} else if strings.HasSuffix(key, "/default") {
				log.Printf("Defaulting to prompt %s for query:%s\n", key, query)
				return key, value
			}
		}
	}
	return "", ""
}

func LoadSysPrompts(settings ServerConfig) {

	for k, text := range settings.SysPrompts {
		if strings.HasPrefix(strings.ToLower(text), "https://") {
			text = HttpsFetch(text)
		}

		for _, channel := range settings.Channels {
			for _, promptName := range channel.SysPromptsEnabled {
				if strings.EqualFold(promptName, k) {
					SysPrompts[settings.Name+"/"+channel.Name+"/"+promptName] = text
					log.Printf("Loaded Prompt '%s/%s/%s' -> Prompt: %s\n", settings.Name, channel.Name, promptName, text)
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
