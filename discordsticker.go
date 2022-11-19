package main

import (
	"flag"
	"io/ioutil"
	"log"
	"math/rand"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"discordsticker/sticker"
	"discordsticker/utils"

	"github.com/bwmarrin/discordgo"
)

const maxMsgLen = 2000

var (
	// server flags
	commandPrefix string
	tokenFilePath string
	resourcePath  string
	coolDown      time.Duration

	userHelp string
)

// replyNormal sends message back to the channel from where we got the message.
func replyNormal(s *discordgo.Session, m *discordgo.MessageCreate, reply string) {
	if _, err := s.ChannelMessageSend(m.ChannelID, reply); err != nil {
		log.Println("Failed to reply:", err)
	}
}

// replyDM sends message as DM.
// If the message was sent from a guild, only a done message will be sent to the guild.
func replyDM(s *discordgo.Session, m *discordgo.MessageCreate, reply string) {
	if m.GuildID == "" {
		// Got command from DM, simple case.
		if _, err := s.ChannelMessageSend(m.ChannelID, reply); err != nil {
			log.Println("Failed to reply:", err)
		}
		return
	}

	// Got command from guild but we replies with DM. The function informs guild on failure.
	informFailure := func() {
		if _, err := s.ChannelMessageSend(m.ChannelID, "Failed to send you a DM!"); err != nil {
			log.Println("Failed to inform failure to guild:", err)
		}
	}

	userChannel, err := s.UserChannelCreate(m.Author.ID)
	if err != nil {
		log.Println("Failed to create a DM channel:", err)
		informFailure()
		return
	}
	if _, err := s.ChannelMessageSend(userChannel.ID, reply); err != nil {
		log.Println("Failed to reply:", err)
		informFailure()
		return
	}
	if _, err := s.ChannelMessageSend(m.ChannelID, "Direct message is sent!"); err != nil {
		log.Println("Failed to send done message to guild:", err)
	}
}

func quotedMessagesToTrunks(lines []string) []string {
	const (
		head = "```\n"
		tail = "\n```\n"
	)
	ret := []string{}
	sb := strings.Builder{}
	for _, l := range lines {
		if len(head)+sb.Len()+1+len(l)+len(tail) <= maxMsgLen {
			sb.WriteString("\n")
			sb.WriteString(l)
			continue
		}
		if sb.Len() != 0 {
			ret = append(ret, head+sb.String()+tail)
			sb.Reset()
		}
		if len(head)+len(l)+len(tail) <= maxMsgLen {
			sb.WriteString(l)
		} else {
			sb.WriteString(l[:maxMsgLen-len(head)-len(" ...")-len(tail)])
			sb.WriteString(" ...")
		}
	}
	if sb.Len() != 0 {
		ret = append(ret, head+sb.String()+tail)
	}
	return ret
}

func handleList(s *discordgo.Session, m *discordgo.MessageCreate, sm *sticker.Manager, command []string) {
	sm.RLock()
	defer sm.RUnlock()

	helpMsg := "The characters inside the brackets are optional. That is, a sticker shown as `巧[克力]` can be specified with `巧`, `巧克`, and `巧克力`.\n"
	msgs := []string{}

	if len(command) == 0 {
		helpMsg += "Use `" + commandPrefix + "list <prefix>` to show only the matched stickers.\n"
		for _, s := range sm.Stickers() {
			msgs = append(msgs, s.StringWithHint())
		}
	} else {
		prefix := command[0]
		ss := sm.MatchedStickers(prefix)
		for _, s := range ss {
			msgs = append(msgs, s.StringWithHint())
		}
	}

	msgs = quotedMessagesToTrunks(msgs)
	if len(msgs) > 0 && len(msgs[len(msgs)-1])+len(helpMsg) <= maxMsgLen {
		msgs[len(msgs)-1] += helpMsg
	} else {
		msgs = append(msgs, helpMsg)
	}
	for _, msg := range msgs {
		replyDM(s, m, msg)
	}
}

func handleAdd(s *discordgo.Session, m *discordgo.MessageCreate, sm *sticker.Manager, command []string) {
	sm.Lock()
	defer sm.Unlock()

	if len(command) != 2 {
		replyNormal(s, m, "Invalid format. Expect `"+commandPrefix+"add <sticker_name> <URL>`. Try again!")
		return
	}

	if err := sm.AddSticker(command[0], command[1]); err != nil {
		if err != sticker.UninformableErr {
			replyNormal(s, m, err.Error())
		} else {
			replyNormal(s, m, "Something goes wrong here! Please contact the admin.")
		}
		return
	}

	replyNormal(s, m, "Done!")
}

func handleRename(s *discordgo.Session, m *discordgo.MessageCreate, sm *sticker.Manager, command []string) {
	sm.Lock()
	defer sm.Unlock()

	if len(command) != 2 {
		replyNormal(s, m, "Invalid format. Expect `"+commandPrefix+"rename <sticker_name> <new_sticker_name>. Try again!`")
		return
	}

	if err := sm.RenameSticker(command[0], command[1]); err != nil {
		if err != sticker.UninformableErr {
			replyNormal(s, m, err.Error())
		} else {
			replyNormal(s, m, "Something goes wrong here! Please contact the admin.")
		}
		return
	}

	replyNormal(s, m, "Done!")
}

func handleRandom(s *discordgo.Session, m *discordgo.MessageCreate, sm *sticker.Manager, stickerIDs []string) {
	sm.RLock()
	defer sm.RUnlock()

	var stickers []*sticker.Sticker
	for _, id := range stickerIDs {
		stickers = append(stickers, sm.MatchedStickers(id)...)
	}

	if len(stickers) == 0 {
		replyNormal(s, m, "Cannot find any matched sticker. Find the sticker names with `"+commandPrefix+"list` command.")
		return
	}

	sticker := stickers[rand.Intn(len(stickers))]

	r, err := os.Open(sticker.Path())
	if err != nil {
		log.Println("Failed to open the image:", err)
		replyNormal(s, m, "Something goes wrong here! Please contact the admin.")
		return
	}
	defer r.Close()

	if _, err := s.ChannelFileSend(m.ChannelID, "sticker"+stickers[0].Ext(), r); err != nil {
		log.Println("Failed to post sticker:", err)
		replyNormal(s, m, "Failed to post sticker!")
		return
	}
}

func handleSticker(s *discordgo.Session, m *discordgo.MessageCreate, sm *sticker.Manager, stickerID string) {
	sm.RLock()
	defer sm.RUnlock()

	stickers := sm.MatchedStickers(stickerID)
	if len(stickers) == 0 {
		replyNormal(s, m, "Cannot find the sticker you're looking for. Find the sticker name with `"+commandPrefix+"list` command.")
		return
	}
	if len(stickers) > 1 {
		matchedStr := sticker.StickerListString(stickers)
		replyNormal(s, m, "Found more than one stickers! Please provide more specific prefix. Matched: "+matchedStr)
		return
	}

	r, err := os.Open(stickers[0].Path())
	if err != nil {
		log.Println("Failed to open the image:", err)
		replyNormal(s, m, "Something goes wrong here! Please contact the admin.")
		return
	}
	defer r.Close()

	if _, err := s.ChannelFileSend(m.ChannelID, "sticker"+stickers[0].Ext(), r); err != nil {
		log.Println("Failed to post sticker:", err)
		replyNormal(s, m, "Failed to post sticker!")
		return
	}
}

func buildUserHelp() string {
	sb := strings.Builder{}
	sb.WriteString("Please send commands with the prefix `")
	sb.WriteString(commandPrefix)
	sb.WriteString("`. Available commands:\n\n")
	for _, t := range []struct {
		command string
		desc    string
	}{{
		"help",
		"Show this message.",
	}, {
		"list [<prefix>]",
		"If `<prefix>` is not given, list all stickers; Otherwise, list all stickers matching `<prefix>`.",
	}, {
		"add <sticker_name> <URL>",
		"Download and save the image at `<URL>` as a new sticker.",
	}, {
		"rename <sticker_name> <new_sticker_name>",
		"Move the sticker on `<sticker_name>` to `<new_sticker_name>`.",
	}, {
		"random <sticker_prefix>...",
		"All stickers that match the prefixes will be collected, and a random one will be post.",
	}, {
		"<sticker_name>",
		"A command that does not match any of the above is considered a sticker name. Use `" + commandPrefix + "list` to view the available stickers.",
	}} {
		sb.WriteString("`")
		sb.WriteString(commandPrefix)
		sb.WriteString(t.command)
		sb.WriteString("`\n")
		sb.WriteString(t.desc)
		sb.WriteString("\n\n")
	}
	return sb.String()
}

func main() {
	var (
		commandPrefixPtr = flag.String("command-prefix", "!!", "The prefix for the users to trigger the bot.")
		tokenFilePathPtr = flag.String("token-file", "token", "The file that contains the bot token.")
		resourcePathPtr  = flag.String("resource-path", "resources", "The root directory of the resources. Each directory in it will become the group name.")
		coolDownPtr      = flag.Duration("cool-dowm", 5*time.Second, "The cool down interval for each channel to post a sticker (including 'random' command).")
	)

	flag.Parse()
	if !flag.Parsed() {
		flag.PrintDefaults()
	}

	commandPrefix = *commandPrefixPtr
	tokenFilePath = *tokenFilePathPtr
	resourcePath = *resourcePathPtr
	coolDown = *coolDownPtr

	userHelp = buildUserHelp()

	log.SetFlags(log.Ldate | log.Ltime | log.Lmicroseconds | log.Lshortfile | log.Lmsgprefix)

	log.Println("Starting...")
	log.Println("\tcommand prefix     =", commandPrefix)
	log.Println("\ttoken file         =", tokenFilePath)
	log.Println("\tresource directory =", resourcePath)
	log.Println("\tcool down interval =", coolDown)

	rand.Seed(time.Now().UnixNano())

	sm, err := sticker.NewManager(resourcePath)
	if err != nil {
		log.Fatalln("Failed to collect the sticker info:", err)
	}

	token, err := ioutil.ReadFile(tokenFilePath)
	if err != nil {
		log.Fatalln("Failed to read the token:", err)
	}

	s, err := discordgo.New("Bot " + strings.TrimSpace(string(token)))
	if err != nil {
		log.Fatalln("Failed to create Discord session:", err)
	}

	cd := utils.NewCoolDownCounter()

	s.AddHandler(func(s *discordgo.Session, m *discordgo.MessageCreate) {
		// Ignore all messages created by the bot itself.
		if m.Author.ID == s.State.User.ID {
			return
		}

		// Only handle messages with certain prefix.
		if !strings.HasPrefix(m.Content, commandPrefix) {
			return
		}

		command := strings.Fields(m.Content[2:])
		if len(command) == 0 {
			command = []string{"help"}
		}

		switch command[0] {
		case "help":
			replyDM(s, m, userHelp)
		case "list":
			handleList(s, m, sm, command[1:])
		case "add":
			handleAdd(s, m, sm, command[1:])
		case "rename":
			handleRename(s, m, sm, command[1:])
		case "random":
			if coolDown == 0 || cd.CoolDown(coolDown, m.ChannelID) {
				handleRandom(s, m, sm, command[1:])
			}
		default: // Consider the command name as the sticker ID.
			if coolDown == 0 || cd.CoolDown(coolDown, m.ChannelID) {
				handleSticker(s, m, sm, command[0])
			}
		}
	})

	s.Identify.Intents = discordgo.IntentsGuildMessages | discordgo.IntentsDirectMessages

	err = s.Open()
	if err != nil {
		log.Fatalln("Failed to open a connection:", err)
	}
	defer s.Close()

	log.Println("Bot is running now. Press CTRL-C to exit.")
	shutdownCh := make(chan os.Signal, 1)
	signal.Notify(shutdownCh, syscall.SIGINT, syscall.SIGTERM, os.Interrupt, os.Kill)

	<-shutdownCh
	log.Println("Bye~")
}
