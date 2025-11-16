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
				sysprompt: "You are a helpful assistant. Confirm to the " +
					"user that their reminder has been scheduled in " +
					"your unique style.",
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

func requeueAsChat(originalReq DeepseekRequest) {
	/*
	 * Re-queue the original message for for regular chat processing.
	 * We'll jsut log that it wasn't a reminder for now as re-queueing
	 * requires finding the original default prompt which adds some complexity.
	 */
	log.Printf("LLM determined this was not a reminder: %s", originalReq.OriginalQuery)
}
