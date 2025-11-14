package main

import (
	"fmt"
	"log"
	"sync"
	"time"
)

/* Holds information for a single reminder. */
type Reminder struct {
	Server  string
	Channel string
	User    string
	Message string
	EndTime time.Time
	Timer   *time.Timer
}

var (
	/* Holds all active reminders, keyed by a server/channel string. */
	Reminders = make(map[string][]*Reminder)
	/* Protect against concurrent access. */
	ReminderMutex = sync.RWMutex{}
)

/* Schdules a new reminder. */
func AddReminder(settings *ServerConfig, reminder *Reminder) {
	ReminderMutex.Lock()
	defer ReminderMutex.Unlock()

	key := fmt.Sprintf("%s/%s", reminder.Server, reminder.Channel)
	Reminders[key] = append(Reminders[key], reminder)
	log.Printf("Scheduled reminder for %s in channel %s: %s", reminder.User,
		reminder.Channel, reminder.Message)

	/* Schedule the reminder. */
	reminder.Timer = time.AfterFunc(time.Until(reminder.EndTime), func() {
		/*
		 * When the timer fires, create a request for the LLM to generate
		 * the reminder message.
		 */
		reminderPrompt := fmt.Sprintf(settings.SysPrompts["reminder_fire"],
											reminder.User, reminder.Message)

		/* Send the reminder_fire prompt. */

		req := DeepseekRequest{
			channel:   reminder.Channel,
			request:   reminderPrompt,
			sysprompt: sysPrompt,
		}
		DeepseekQueue <- req

		/* Clean up the reminder after it's sent. */
		RemoveReminder(reminder)
	})
}

/* Remove reminder from the active reminders list. */
func RemoveReminder(reminder *Reminder) {
	log.Printf("Removing Reminder for:%s\n", reminder.Message)
	ReminderMutex.Lock()
	defer ReminderMutex.Unlock()

	key := fmt.Sprintf("%s/%s", reminder.Server, reminder.Channel)
	if reminders, ok := Reminders[key]; ok {
		for i, r := range reminders {
			if r == reminder {
				/* Stop the timer to prevent it from firing if hasn't already. */
				r.Timer.Stop()
				Reminders[key] = append(reminders[:i], reminders[i+1:]...)
				log.Printf("Removed reminder for %s in channel %s: %s", r.User, r.Channel, r.Message)
				break
			}
		}
	}
}
