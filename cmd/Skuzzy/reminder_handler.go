package main

import (
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"
)

/* Holds structured data from the LLM's reminder parsing. */
type ReminderParseResult struct {
	IsReminder      bool   `json:"is_reminder"`
	DurationMinutes int    `json:"duration_minutes"`
	ReminderMessage string `json:"reminder_message"`
}

/* Holds structured data from the LLM's reminder change parsing. */
type ReminderChangeParseResult struct {
	ReminderID         int    `json:"reminder_id"`
	NewDurationMinutes int    `json:"new_duration_minutes"`
	NewReminderMessage string `json:"new_reminder_message"`
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
			requeueAsChat(settings, parsedData.OriginalReq)
			continue
		}

		if parsedData.OriginalReq.PromptName == "reminder_parse" {
			var result ReminderParseResult
			err := json.Unmarshal([]byte(parsedData.Result), &result)
			if err != nil {
				log.Printf("Error parsing reminder JSON from LLM: %v", err)
				requeueAsChat(settings, parsedData.OriginalReq)
				continue
			}
			if result.IsReminder && result.DurationMinutes > 0 {
				reminder := &Reminder{
					Server:  parsedData.OriginalReq.Server, /* We should use server from origreq. */
					Channel: parsedData.OriginalReq.Channel,
					User:    parsedData.OriginalReq.User,
					Message: result.ReminderMessage,
					EndTime: time.Now().Add(time.Duration(result.DurationMinutes) * time.Minute),
				}
				AddReminder(settings, reminder)

				/* Send a request to the LLM to confirm the reminder. */
				req := DeepseekRequest{
					Server:  parsedData.OriginalReq.Server,
					Channel: parsedData.OriginalReq.Channel,
					request: fmt.Sprintf("You have scheduled a reminder for %s in %d minutes to %s.",
						reminder.User, result.DurationMinutes, reminder.Message),
					sysprompt:  settings.SysPrompts["reminder_confirm"],
					PromptName: "reminder_confirm",
					User:       parsedData.OriginalReq.User,
				}
				DeepseekQueue <- req
			} else {
				/* Not a reminder, so re-queue as regular chat message. */
				requeueAsChat(settings, parsedData.OriginalReq)
			}
		} else if parsedData.OriginalReq.PromptName == "reminder_change_parse" {
			var result ReminderChangeParseResult
			err := json.Unmarshal([]byte(parsedData.Result), &result)
			if err != nil {
				log.Printf("Error parsing reminder change JSON from LLM: %v", err)
				send_irc(parsedData.OriginalReq.Server, parsedData.OriginalReq.Channel,
					fmt.Sprintf("%s: I could not understand your request to change reminder ID %d. "+
						"Try again but better!", parsedData.OriginalReq.User, result.ReminderID))
				continue
			}

			response := ChangeReminder(settings, parsedData.OriginalReq.User, result.ReminderID,
				result.NewReminderMessage, result.NewDurationMinutes)

			send_irc(parsedData.OriginalReq.Server, parsedData.OriginalReq.Channel, response)
		} else {
			log.Printf("Unkown prompt name received in ReminderHandler: %s",
				parsedData.OriginalReq.PromptName)
			requeueAsChat(settings, parsedData.OriginalReq) /* Fallback to chat. */
		}
	}
}

func requeueAsChat(settings *ServerConfig, originalReq DeepseekRequest) {
	log.Printf("LLM determined this was not a reminder, reqeueing as chat: %s",
		originalReq.OriginalQuery)

	_, text := FindPrompt(settings, "deepseek", originalReq.Channel, originalReq.OriginalQuery)

	/* Can't get backlog available here. Do we care? */
	text = strings.Replace(text, "{NICK}", settings.Nick, -1)
	text = strings.Replace(text, "{USER}", originalReq.User, -1)
	text = strings.Replace(text, "{CHANNEL}", originalReq.Channel, -1)

	/* Create and send the new request for general processing. */
	req := DeepseekRequest{
		Server:    originalReq.Server,
		Channel:   originalReq.Channel,
		sysprompt: text,
		request:   originalReq.OriginalQuery,
		User:      originalReq.User,
	}
	DeepseekQueue <- req
}
