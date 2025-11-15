package main

import (
	"fmt"
	"log"
	"sync"
	"time"
)

/* Holds information for a single reminder. */
type Reminder struct {
	ID      int
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
	ReminderMutex  = sync.RWMutex{}
	nextReminderID = 1
)

/* Schdules a new reminder. */
func AddReminder(settings *ServerConfig, reminder *Reminder) {
	ReminderMutex.Lock()
	defer ReminderMutex.Unlock()

	/* Try eat up all our RAM with your silly reminders now!!11! */
	userReminderCount := 0
	key := fmt.Sprintf("%s/%s", reminder.Server, reminder.Channel)
	if existingReminders, ok := Reminders[key]; ok {
		for _, r := range existingReminders {
			if r.User == reminder.User {
				userReminderCount++
			}
		}
	}
	if userReminderCount >= settings.MaxRemindersPerUser {
		send_irc(reminder.Server, reminder.Channel, fmt.Sprintf("%s: You have reached "+
			"your limit of %d active reminders "+
			"in this channel",
			reminder.User, settings.MaxRemindersPerUser))
		return
	}

	reminder.ID = nextReminderID
	nextReminderID++
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

/* Retrieve and format a user's active reminders. */
func ListReminders(settings *ServerConfig, user string) string {
	ReminderMutex.RLock()
	defer ReminderMutex.RUnlock()

	var userReminders []*Reminder
	for _, reminderList := range Reminders {
		for _, r := range reminderList {
			if r.User == user {
				userReminders = append(userReminders, r)
			}
		}
	}

	if len(userReminders) == 0 {
		return fmt.Sprintf("%s: You have no active reminders.", user)
	}

	var response strings.Builder
	response.WriteString(fmt.Sprintf("%s: Your active reminders:", user))
	for i, r := range userReminders {
		timeRemaining := time.Until(r.EndTime)
		response.WriteString(fmt.Sprintf(" %d. ID: %d - \"%s\" (in %s) ",
			i+1, r.ID, r.Message, formatDuration(timeRemaining)))
	}
	return response.String()
}

/* Format duration. */
func formatDuration(d time.Duration) string {
	/* Currently, we only do whole minutes, but just in case that changes. */
	if d < time.Minute {
		return fmt.Sprintf("%d seconds", int(d.Seconds()))
	} else if d < time.Hour {
		return fmt.Sprintf("%d minutes", int(d.Minutes()))
	} else if d < 24*time.Hour {
		return fmt.Sprintf("%d hours %d minutes", int(d.Hours()), int(d.Minutes())%60)
	} else {
		return fmt.Sprintf("%d days %d hours", int(d.Hours()/24), int(d.Hours())%24)
	}
}

/* Remove a reminder by ID for given user. */
func DeleteReminder(settings *ServerConfig, user string, id int) string {
	ReminderMutex.Lock()
	defer ReminderMutex.Unlock()

	var response string
	found := false

	for key, reminderList := range Reminders {
		for i, r := range reminderList {
			if r.User == user && r.ID == id {
				/* Stop the timer to prevent it from firing. */
				r.Timer.Stop()

				/* Remove the reminder from the slice. */
				Reminders[key] = append(reminderList[:i], reminderList[i+1:]...)
				log.Printf("%s deleted reminder ID %d: %s", user, id, r.Message)
				response = fmt.Sprintf("%s: Reminder ID %d (\"%s\") has been deleted.", user,
					id, r.Message)
				found = true
				break
			}
		}
		if found {
			if len(Reminders[key]) == 0 { /* List empty, remove key from map. */
				delete(Reminders, key)
			}
			break
		}
	}

	if !found {
		response = fmt.Sprintf("%s: No reminder found with ID %d for you.", user, id)
	}
	return response
}

/* Modify an existing reminder by ID for given user. */
func ChangeReminder(settings *ServerConfig, user string, id int, newMessage string,
	newDurationMinutes int) string {

	ReminderMutex.Lock()
	defer ReminderMutex.Unlock()

	var response string
	found := false

	for _, reminderList := range Reminders {
		for _, r := range reminderList {
			if r.User == user && r.ID == id {
				/* Update message if provided. */
				if newMessage != "" {
					r.Message = newMessage
				}

				/* Reschedule if new duration is provided. */
				if newDurationMinutes > 0 {
					/* Stop old timer. */
					r.Timer.Stop()
					/* Calculate new EndTime. */
					r.EndTime = time.Now().Add(time.Duration(newDurationMinutes) * time.Minute)
					/* Start new timer. */
					r.Timer = time.AfterFunc(time.Until(r.EndTime), func() {
						reminderPrompt := fmt.Sprintf(settings.SysPrompts["reminder_fire"],
							r.User, r.Message)
						req := DeepseekRequest{
							channel:   r.Channel,
							request:   reminderPrompt,
							sysprompt: settings.SysPrompts["reminder_fire"],
						}
						DeepseekQueue <- req
						RemoveReminder(r)
					})
				}

				log.Printf("%s changed reminder ID %d. New message \"%s\", New Duration: %d minutes",
					user, id, r.Message, newDurationMinutes)

				response = fmt.Sprintf("%s: Reminder ID %d has been updated. New message: "+
					"\"%s\", due in %s.", user, id, r.Message, formatDuration(time.Until(r.EndTime)))
				found = true
				break
			}
		}
		if found {
			break
		}
	}

	if !found {
		response = fmt.Sprintf("%s: No reminder found with ID %d for you.", user, id)
	}
	return response
}
