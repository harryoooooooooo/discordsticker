# discordsticker

A Discord bot that helps post stickers. Greatly inspired by
[gidis](https://github.com/hhhhhojeihsu/gidis).

## Install & Run

First clone the repo:

```bash
git clone https://github.com/harryoooooooooo/discordsticker
```

Install to `${GOPATH}/bin` and run the binary.

```bash
cd discordsticker
go install discordsticker
discordsticker -help
```

Alternatively, run the bot directly with `go run`.

```bash
cd discordsticker
go run discordsticker -help
```

## Usage

By default the program uses the bot token in the file `./token`,
and reads the stickers in `./resources/`.

Under `./resources/`, there should be directories denoting the sticker groups;
Inside each group, there should be stickers.

The bot reads the messages with specific prefix (by default `!!`)
from DMs or guilds.
If the message is not a keyword, the bot finds out the sticker
which's name starts with the message, and post the sticker.

For example, with the file structure below, users can:
* Post `dog.png` with `!!d`, `!!do`, or `!!dog`. Because no other sitcker's name starts with d.
* Post `cat.png` with `!!cat`, or with group name `!!animals/c` or even `!!a/c`.
```
resources -- animals -- cat.png
          |          `- dog.png
          |
          |- fruits -- apple.png
          |         `- banana.png
          |
          `- vehicles -- bike.png
                      `- car.png
```

Users can get more info with `!!help`.
