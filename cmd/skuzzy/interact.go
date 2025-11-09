package main

import (
	"bufio"
	"fmt"
	"log"
	"os"
	"strings"
)

var Server = ""
var Channel = ""
var Interactive = false

const help_string = `
Usage:

/quit 			Exit program
/help 			Print this message
/server			Switch to server, e.g.: /Server libera
/channel		Switch to channel, e.g.: /Channel #hackers
/info			Display information about the current server and channel
/interactive	turn interactive mode on or off
`

func interact() {
	reader := bufio.NewReader(os.Stdin)

	for {
		input, _ := reader.ReadString('\n')
		interact_triage(input)
	}
}

func interact_triage(_input string) {
	input := strings.TrimSpace(strings.ToLower(_input))
	if strings.HasPrefix(input, "/") {
		switch strings.Split(input, " ")[0] {
		case "/quit":
			os.Exit(0)
		case "/server":
			Server = strings.TrimSpace(strings.Replace(input, "/server", "", 1))
			log.Printf("Output server set to %v\n", Server)
		case "/channel":
			Channel = strings.TrimSpace(strings.Replace(input, "/channel", "", 1))
			log.Printf("Output channel set to %v\n", Channel)
		case "/info":
			fmt.Printf("Current output:\nServer:%v\nChannel:%v\n", Server, Channel)
		case "/interactive":
			if Interactive {
				Interactive = false
				log.Printf("Interactive mode turned off\n")

			} else {
				Interactive = true
				log.Printf("Interactive mode turned on\n")
			}
		default:
			fmt.Printf("Unknown command:'%v'\n", input)
			fallthrough
		case "/help":
			fmt.Println(help_string)
		}
	} else {
		if Interactive && Server != "" {
			send_irc(Server, Channel, _input)
		}
	}
}
