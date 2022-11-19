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

By default the program uses the bot token in the file `token`,
and reads the stickers in `resources/`.

Stickers are placed under `resources/`.
Sub-directories are acceptable but not recommended;
Support of sub-directories could be deprecated in the future.

The bot reads the messages with specific prefix (by default `!!`)
from DMs or guilds.
If the message doesn't start with `/`, the bot finds out the sticker
which's name contains with the patterns, and post the sticker.
Note that the file extensions are omitted,
and all filesystem separators or slashes in the sticker name will be replaced with `-`.

## Example

With the file structure below, users:
* Can post `sakura-miko.png` with `!!miko`, `!!sakura`, etc. As long as they are not the contained by other names.
* Cannot post any stickers with `!!kami` because there are three stickers matching the search.
* Can post `ookami-mio.png` with `!!kami mio`, `!!ookami`, etc.
* Can randomly post one of `sakura-miko.png` and `shirakami-fubuki.png` with `!!/random ra`.
```
resources -- sakura-miko.png
          |
          |- ookami-mio.png
          |
          |- shirakami-fubuki.png
          |
          `- inukami-korone.png
```

Users can get more info with `!!/help`.
