package main

import (
	"encoding/json"
	"fmt"
	"log"
	"time"
)

/* Holds structured data from the LLM's reminder parsing. */
type ReminderParseResult struct {
	IsReminder      bool   `json:"is_reminder"`
	DurationMinutes int    `json:"duration_minutes"`
	ReminderMessage string `json:"reminder_message"`
}

/* Channel for receiving parsed reminder data from the LLM. */
var ReminderParseQueue = make(chan struct {
	Result      string
	OriginalReq DeepseekRequest
})

/* Processes the parsed reminder data from the LLM. */
func ReminderHandler(settings *ServerConfig) {
	for {
		parsedData := <-ReminderParseQueue
		var result ReminderParseResult
		err := json.Unmarshal([]byte(parsedData.Result), &result)
		if err != nil {
			log.Printf("Error parsing reminder JSON from LLM: %v", err)
			requeueAsChat(parsedData.OriginalReq)
			continue
		}

		if result.IsReminder && result.DurationMinutes > 0 {
			/* It's valid, so go ahead and schedule. */
			reminder := &Reminder{
				Server:  settings.Name,
				Channel: parsedData.OriginalReq.channel,
				User:    parsedData.OriginalReq.User,
				Message: result.ReminderMessage,
				EndTime: time.Now().Add(time.Duration(result.DurationMinutes) * time.Minute),
			}
			AddReminder(settings, reminder)

			/* Now send a request to the LLM to confirm the reminder. */
			req := DeepseekRequest{
				channel: parsedData.OriginalReq.channel,
				request: fmt.Sprintf("You have scheduled a reminder for %s in %d minutes to %s.",
					reminder.User, result.DurationMinutes, reminder.Message),
				sysprompt: settings.SysPrompts["reminder_confirm"],
				PromptName: "reminder_confirm",
				User:       parsedData.OriginalReq.User,
			}
			DeepseekQueue <- req
		} else {
			/* Not a reminder, so re-queue as a regular chat message. */
			requeueAsChat(parsedData.OriginalReq)
		}
	}
}

func requeueAsChat(settings *ServerConfig, originalReq DeepseekRequest) {
	log.Printf("LLM determined this was not a reminder, reqeueing as chat: %s",
		originalReq.OriginalQuery)

	_, text := FindPrompt(settings, "deepseek", originalReq.channel, originalReq.OriginalQuery)

	/* Can't get backlog available here. Do we care? */
	text = strings.Replace(text, "{NICK}", settings.Nick, -1)
	text = strings.Replace(text, "{USER}", originalReq.User, -1)
	text = strings.Replace(text, "{CHANNEL}", originalReq.channel, -1)

	/* Create and send the new request for general processing. */
	req := DeepseekRequest{
		Server:    originalReq.Server,
		channel:   originalReq.channel,
		sysprompt: text,
		request:   originalReq.OriginalQuery,
		User:      originalReq.User,
	}
	DeepseekQueue <- req
}
