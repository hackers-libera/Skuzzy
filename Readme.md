# Skuzzy

Skuzzy is an experimental IRC bot developed with the `#hackers` Libera.chat community in mind.

## Build

`go build -o skuzzy  cmd/skuzzy/*`

## Run

Refer to the sample `libera.yaml` config to get started.

`skuzzy server1.yaml server2.yaml`

## Interact

Type `/help` to display runtime commands for the bot operator.

```
Usage:

/quit 			Exit program
/help 			Print this message
/server			Switch to server, e.g.: /Server libera
/channel		Switch to channel, e.g.: /Channel #hackers
/info			Display information about the current server and channel
/interactive	turn interactive mode on or off
```
