package main

import (
	"fmt"
	"log"
	"net"
	"os"
	"strconv"
	"strings"
)

var Server = ""
var Channel = ""
var Interactive = false
var InteractQueue = make(chan string)

const help_string = `
Usage:

/quit                             Exit program
/help                             Print this message
/server                           Switch to server, e.g.: /Server libera
/channel                          Switch to channel, e.g.: /Channel #hackers
/info                             Display information about the current server and channel
/interactive                      Turn interactive mode on or off
/admin reminders list             List all active reminders 
/admin reminders delete <id>      Delete a reminder by ID
/admin reminders purge            Delete all reminders
`

func interact(socketPath string) {
	os.Remove(socketPath)
	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: socketPath, Net: "unix"})
	if err != nil {
		log.Printf("[interact] Error listening: %v\n", err)
		return
	}
	defer listener.Close()
	log.Printf("[interact] Listening on Unix socket: %s\n", socketPath)
	buffer := make([]byte, 1024)
	for {
		conn, err := listener.AcceptUnix()
		if err != nil {
			log.Printf("[interact] Error accepting connection: %v\n", err)
			continue
		}
		go interact_output(conn)
		conn.Write([]byte("Connected to Skuzzy's bot control interface...\n"))
		for {
			n, err := conn.Read(buffer)
			if err != nil {
				log.Printf("[interact] Error reading from connection: %v\n", err)
				break
			}
			input := buffer[:n]
			interact_triage(string(input), conn)
		}
	}
}

func interact_triage(_input string, conn *net.UnixConn) {
	input := strings.TrimSpace(strings.ToLower(_input))
	if strings.HasPrefix(input, "/") {
		switch strings.Split(input, " ")[0] {
		case "/quit":
			conn.Write([]byte("Exiting...\r\n"))
			os.Exit(0)
		case "/server":
			Server = strings.TrimSpace(strings.Replace(input, "/server", "", 1))
			conn.Write([]byte(fmt.Sprintf("Output server set to %v\n", Server)))
		case "/channel":
			Channel = strings.TrimSpace(strings.Replace(input, "/channel", "", 1))
			conn.Write([]byte(fmt.Sprintf("Output channel set to %v\n", Channel)))
		case "/info":
			conn.Write([]byte(fmt.Sprintf("Current output:\nServer:%v\nChannel:%v\n", Server, Channel)))
		case "/interactive":
			if Interactive {
				Interactive = false
				conn.Write([]byte(fmt.Sprintf("Interactive mode turned off\n")))
			} else {
				Interactive = true
				conn.Write([]byte(fmt.Sprintf("Interactive mode turned on\n")))
			}
		case "/admin":
			parts := strings.Split(input, " ")
			if len(parts) < 2 {
				conn.Write([]byte("Usage: /admin <command>\n"))
				return
			}
			adminCommand := parts[1]
			switch adminCommand {
			case "reminders":
				if len(parts) < 3 {
					conn.Write([]byte("Usage: /admin reminders <list|delete|purge>\n"))
					return
				}
				reminderSubcommand := parts[2]
				switch reminderSubcommand {
				case "list":
					conn.Write([]byte(ListAllReminders() + "\n"))
				case "delete":
					if len(parts) < 4 {
						conn.Write([]byte("Usage: /admin reminders delete <id>\n"))
						return
					}
					id, err := strconv.Atoi(parts[3])
					if err != nil {
						conn.Write([]byte("Invalid reminder ID.\n"))
						return
					}
					conn.Write([]byte(AdminDeleteReminder(id) + "\n"))
				case "purge":
					conn.Write([]byte(PurgeAllReminders() + "\n"))
				default:
					conn.Write([]byte(fmt.Sprintf("Unknown admin reminders command: '%s'\n", reminderSubcommand)))
				}
			default:
				conn.Write([]byte(fmt.Sprintf("Unknown admin command: '%s'\n", adminCommand)))
			}
		default:
			conn.Write([]byte(fmt.Sprintf("Unknown command:'%v'\n", input)))
			fallthrough
		case "/help":
			conn.Write([]byte(fmt.Sprint(help_string)))
		}
	} else {
		if Interactive && Server != "" {
			send_irc(Server, Channel, _input)
			conn.Write([]byte(fmt.Sprintf("[<][%s/%s] %s\n", Server, Channel, _input)))
		}
	}
}

func interact_output(conn *net.UnixConn) {
	for {
		output := <-InteractQueue
		conn.Write([]byte(output))
	}
}
