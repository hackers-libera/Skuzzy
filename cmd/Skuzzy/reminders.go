package main

import (
	"database/sql"
	"fmt"
	"log"
	"strings"
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

/*
 * Holds the timer objects for currently scheduled reminders.
 * This allows us to cancel a reminder's timer if it's changed or deleted.
 */
var (
	/* Protect against concurrent access. */
	ReminderMutex = sync.RWMutex{}
	activeTimers  = make(map[int]*time.Timer)
)

/* Save a new reminder to the database and scheudle it to fire. */
func AddReminder(settings *ServerConfig, reminder *Reminder) error {
	ReminderMutex.Lock()
	defer ReminderMutex.Unlock()

	/* Check user's current reminder count from the database. */
	var userReminderCount int
	err := DB.QueryRow("SELECT COUNT(*) FROM reminders WHERE server = ? AND user = ?",
		reminder.Server, reminder.User).Scan(&userReminderCount)
	if err != nil {
		if err != sql.ErrNoRows {
			log.Printf("Error counting reminders for user %s: %v", reminder.User, err)
			return fmt.Errorf("failed to count existing reminders: %w", err)
		}
	}
	if userReminderCount >= settings.MaxRemindersPerUser {
		send_irc(reminder.Server, reminder.Channel, fmt.Sprintf("%s: You have reached "+
			"your limit of %d active reminders "+
			"in this channel",
			reminder.User, settings.MaxRemindersPerUser))
		return nil
	}
	delta := time.Now().Unix() - reminder.EndTime.Unix()
	if delta < 1 || delta > (86400*7) {
		send_irc(reminder.Server, reminder.Channel, "Sorry, I can only schedule between a range of 1 minute and 7 day window.")
		return nil
	}
	/* Insert reminder into database. */
	result, err := DB.Exec("INSERT INTO reminders (server, channel, user, message, end_time) "+
		"VALUES (?, ?, ?, ?, ?)", reminder.Server, reminder.Channel, reminder.User, reminder.Message,
		reminder.EndTime.Unix())
	if err != nil {
		return fmt.Errorf("failed to insert reminder into DB: %w", err)
	}
	lastID, err := result.LastInsertId()
	if err != nil {
		return fmt.Errorf("failed to get last insert ID for reminder: %w", err)
	}
	reminder.ID = int(lastID)

	log.Printf("Scheduled reminder ID %d for %s: %s", reminder.ID, reminder.User, reminder.Message)

	/* Schedule the reminder to fire. */
	scheduleReminder(reminder, settings)
	return nil
}

/* Create the time.AfterFunc timer for a reminder. */
func scheduleReminder(r *Reminder, settings *ServerConfig) {
	/* We don't want to schedule a reminder that already passed. */
	if time.Now().After(r.EndTime) {
		fireReminder(r, settings)
		return
	}
	timer := time.AfterFunc(time.Until(r.EndTime), func() {
		fireReminder(r, settings)
	})
	activeTimers[r.ID] = timer
}

/* Remove reminder db and active timers after it's sent. */
func RemoveReminder(reminder *Reminder) {
	log.Printf("Removing reminder ID %d for: %s\n", reminder.ID, reminder.Message)
	ReminderMutex.Lock()
	defer ReminderMutex.Unlock()

	/* Stop timer if it's still active */
	if timer, ok := activeTimers[reminder.ID]; ok {
		timer.Stop()
		delete(activeTimers, reminder.ID)
	}

	/* Delete from database. */
	if _, err := DB.Exec("DELETE FROM reminders WHERE id = ?", reminder.ID); err != nil {
		log.Printf("Error deleting reminder ID %d from DB: %v", reminder.ID, err)
	}
}

/* Retrieve and format a user's active reminders. */
func ListReminders(settings *ServerConfig, user string) string {
	ReminderMutex.RLock()
	defer ReminderMutex.RUnlock()

	rows, err := DB.Query("SELECT id, message, end_time FROM reminders WHERE user = ? "+
		"ORDER BY end_time ASC", user)
	if err != nil {
		log.Printf("Error listing reminders for user %s: %v", user, err)
		return fmt.Sprintf("%s: Error retrieving your reminders.", user)
	}
	defer rows.Close()

	var reminders []Reminder
	for rows.Next() {
		var r Reminder
		var endtimeUnix int64
		if err := rows.Scan(&r.ID, &r.Message, &endtimeUnix); err != nil {
			log.Printf("Error scanning reminder row for user %s: %v", user, err)
			continue
		}
		r.EndTime = time.Unix(endtimeUnix, 0)
		reminders = append(reminders, r)
	}

	if len(reminders) == 0 {
		return fmt.Sprintf("%s: You have no active reminders.", user)
	}

	var response strings.Builder
	response.WriteString(fmt.Sprintf("%s: Your active reminders:", user))
	for i, r := range reminders {
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

/* Remove a reminder by ID for given user from db and stop its timer. */
func DeleteReminder(settings *ServerConfig, user string, id int) string {
	ReminderMutex.Lock()
	defer ReminderMutex.Unlock()

	var reminderMessage string
	err := DB.QueryRow("SELECT message FROM reminders WHERE id = ? AND user = ?", id,
		user).Scan(&reminderMessage)
	if err != nil {
		if err == sql.ErrNoRows {
			return fmt.Sprintf("%s: No reminder found with ID %d for you.", user, id)
		}
		log.Printf("Error querying reminder ID %d for user %s: %v", id, user, err)
		return fmt.Sprintf("%s: Error deleting reminder ID %d.", user, id)
	}

	/* Stop the associated timer. */
	if timer, ok := activeTimers[id]; ok {
		timer.Stop()
		delete(activeTimers, id)
	}

	/* Delete from db. */
	if _, err := DB.Exec("DELETE FROM reminders WHERE id = ? AND user = ?", id, user); err != nil {
		log.Printf("Error deleting reminder ID %d from DB: %v", id, err)
		return fmt.Sprintf("%s: Error deleting reminder ID %d", user, id)
	}

	log.Printf("%s deleted reminder ID %d: %s", user, id, reminderMessage)
	return fmt.Sprintf("%s: Reminder ID %d (\"%s\") has been deleted.", user, id, reminderMessage)
}

func LoadReminders(settings *ServerConfig) error {
	rows, err := DB.Query("SELECT id, server, channel, user, message, end_time "+
		"FROM reminders WHERE server = ?", settings.Name)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil /* No reminders to load. */
		}
		return fmt.Errorf("failed to query reminders: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var r Reminder
		var endTimeUnix int64
		if err := rows.Scan(&r.ID, &r.Server, &r.Channel, &r.User, &r.Message, &endTimeUnix); err != nil {
			log.Printf("Error scanning reminder row: %v", err)
			continue
		}
		delta := endTimeUnix - time.Now().Unix()
		if delta < 1 || delta > (86400*7) {
			log.Printf("Refusing to load a reminder that would trigger after %d seconds\n", delta)
			return nil
		}
		r.EndTime = time.Unix(endTimeUnix, 0)
		log.Printf("Loading reminder ID %d for %s: %s", r.ID, r.User, r.Message)
		scheduleReminder(&r, settings)
	}
	return nil
}

/* Sends the reminder notification and removes from db. */
func fireReminder(r *Reminder, settings *ServerConfig) {
	sysPrompt := settings.SysPrompts["reminder_fire"]
	sysPrompt = strings.Replace(sysPrompt, "{USER}", r.User, -1)
	sysPrompt = strings.Replace(sysPrompt, "{MESSAGE}", r.Message, -1)

	req := DeepseekRequest{
		Server:    r.Server,
		Channel:   r.Channel,
		request:   "The reminder is now due. Please generate the notification.",
		sysprompt: sysPrompt,
	}
	DeepseekQueue <- req

	/* Clean the reminder from db and active timers after it's sent. */
	RemoveReminder(r)
}

/* Modify an existing reminder by ID for given user. */
func ChangeReminder(settings *ServerConfig, user string, id int, newMessage string,
	newDurationMinutes int) string {

	ReminderMutex.Lock()
	defer ReminderMutex.Unlock()

	var r Reminder
	var endTimeUnix int64
	err := DB.QueryRow("SELECT id, server, channel, user, message, end_time FROM "+
		"reminders WHERE id = ? AND user = ?", id, user).Scan(
		&r.ID, &r.Server, &r.Channel, &r.User, &r.Message, &endTimeUnix)
	if err != nil {
		if err == sql.ErrNoRows {
			return fmt.Sprintf("%s: No reminder found with ID %d for you.", user, id)
		}
		log.Printf("Error querying reminder ID %d for user %s: %v", id, user, err)
		return fmt.Sprintf("%s: Error changing reminder ID %d.", user, id)
	}
	r.EndTime = time.Unix(endTimeUnix, 0)

	updateFields := []string{}
	updateValues := []interface{}{}
	madeChanges := false

	if newMessage != "" && newMessage != r.Message {
		r.Message = newMessage
		updateFields = append(updateFields, "message = ?")
		updateValues = append(updateValues, newMessage)
		madeChanges = true
	}

	if newDurationMinutes > 0 {
		/* Calc new EndTime. */
		r.EndTime = time.Now().Add(time.Duration(newDurationMinutes) * time.Minute)
		updateFields = append(updateFields, "end_time = ?")
		updateValues = append(updateValues, r.EndTime.Unix())
		madeChanges = true
	}

	if !madeChanges {
		return fmt.Sprintf("%s: No changes specified for reminder ID %d.", user, id)
	}

	if len(updateFields) > 0 {
		updateValues = append(updateValues, id, user)
		stmt := fmt.Sprintf("UPDATE reminders SET %s WHERE id = ? AND user = ?",
			strings.Join(updateFields, ", "))

		if _, err := DB.Exec(stmt, updateValues...); err != nil {
			log.Printf("Error updating reminder ID %d in DB: %v", id, err)
			return fmt.Sprintf("%s: Error changing reminder ID %d.", user, id)
		}
	}

	/* Reschedule timer. */
	if timer, ok := activeTimers[id]; ok {
		timer.Stop()
	}
	scheduleReminder(&r, settings)

	log.Printf("%s changed reminder ID %d. New Message \"%s\", New Duration: %d minutes",
		user, id, r.Message, newDurationMinutes)

	return fmt.Sprintf("%s: Reminder ID %d has been updated. New Message: \"%s\", due in %s",
		user, id, r.Message, formatDuration(time.Until(r.EndTime)))
}

/* Retrieve active reminders for all users. */
func ListAllReminders() string {
	ReminderMutex.RLock()
	defer ReminderMutex.RUnlock()

	rows, err := DB.Query("SELECT id, user, message, end_time, server, channel " +
		"FROM reminders ORDER BY end_time ASC")
	if err != nil {
		log.Printf("Error listing all reminders: %v", err)
		return "Error retrieving reminders."
	}
	defer rows.Close()

	var reminders []Reminder
	for rows.Next() {
		var r Reminder
		var endtimeUnix int64
		if err := rows.Scan(&r.ID, &r.User, &r.Message, &endtimeUnix, &r.Server,
			&r.Channel); err != nil {
			log.Printf("Error scaning reminder row: %v", err)
			continue
		}
		r.EndTime = time.Unix(endtimeUnix, 0)
		reminders = append(reminders, r)
	}

	if len(reminders) == 0 {
		return "No active reminders."
	}

	var response strings.Builder
	response.WriteString("All active reminders:\n")
	for _, r := range reminders {
		timeRemaining := time.Until(r.EndTime)
		response.WriteString(fmt.Sprintf("	ID: %d, User: %s, Server: %s, "+
			" Channel: %s, Message: \"%s\" (in %s)\n",
			r.ID, r.User, r.Server, r.Channel, r.Message, formatDuration(timeRemaining)))
	}
	return response.String()
}

/* Admin delete reminder by ID from db and stop its timer. */
func AdminDeleteReminder(id int) string {
	ReminderMutex.Lock()
	defer ReminderMutex.Unlock()

	var reminderMessage string
	err := DB.QueryRow("SELECT message FROM reminders WHERE id = ?", id).Scan(&reminderMessage)
	if err != nil {
		if err == sql.ErrNoRows {
			return fmt.Sprintf("No reminder found with ID %d.", id)
		}
		log.Printf("Error querying reminder ID %d: %v", id, err)
		return fmt.Sprintf("Error deleting reminder ID %d.", id)
	}

	/* Stop associated timer. */
	if timer, ok := activeTimers[id]; ok {
		timer.Stop()
		delete(activeTimers, id)
	}

	/* Delete from db. */
	if _, err := DB.Exec("DELETE FROM reminders WHERE id = ?", id); err != nil {
		log.Printf("Error deleting ID %d from DB: %v", id, err)
		return fmt.Sprintf("Error deleting reminder ID %d", id)
	}
	log.Printf("Admin deleted reminder ID %d: %s", id, reminderMessage)
	return fmt.Sprintf("Reminder ID %d (\"%s\") has been deleted.", id, reminderMessage)
}

/* Purge all reminders from the database. */
func PurgeAllReminders() string {
	ReminderMutex.Lock()
	defer ReminderMutex.Unlock()

	/* Stop all active timers. */
	for id, timer := range activeTimers {
		timer.Stop()
		delete(activeTimers, id)
	}

	/* Delete all reminders from db. */
	if _, err := DB.Exec("DELETE FROM reminders"); err != nil {
		log.Printf("Error purging reminders from DB: %v", err)
		return "Error purging reminders."
	}
	log.Println("Admin purging all reminders.")
	return "All reminders have been purged."
}
