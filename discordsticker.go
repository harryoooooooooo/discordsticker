package main

import (
	"encoding/json"
	"flag"
	"fmt"
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
	// server configs
	commandPrefix string
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

func buildPatternGroups(fields []string) [][]string {
	var toks []string
	for _, f := range fields {
		for _, s := range strings.SplitAfter(f, "/") {
			if s == "" {
				continue
			}
			if s[len(s)-1] == '/' {
				if len(s) > 1 {
					toks = append(toks, s[:len(s)-1])
				}
				toks = append(toks, "/")
			} else {
				toks = append(toks, s)
			}
		}
	}
	var ret [][]string
	begin := 0
	end := 0
	for end < len(toks) {
		if toks[end] == "/" {
			ret = append(ret, toks[begin:end])
			begin = end + 1
		}
		end++
	}
	return append(ret, toks[begin:end])
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

	var ss []*sticker.Sticker
	if len(command) == 0 {
		ss = sm.Stickers()
	} else {
		ss = sm.MatchedStickers(buildPatternGroups(command))
	}

	if len(ss) == 0 {
		replyDM(s, m, "No matched stickers found!")
		return
	}

	msgs := make([]string, len(ss))
	for i, s := range ss {
		msgs[i] = s.Name()
	}

	for _, msg := range quotedMessagesToTrunks(msgs) {
		replyDM(s, m, msg)
	}
}

func handleAdd(s *discordgo.Session, m *discordgo.MessageCreate, sm *sticker.Manager, command []string) {
	sm.Lock()
	defer sm.Unlock()

	if len(command) != 2 {
		replyNormal(s, m, "Invalid format. Expect `"+commandPrefix+"/add <sticker_name> <URL>`. Try again!")
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
		replyNormal(s, m, "Invalid format. Expect `"+commandPrefix+"/rename <sticker_name> <new_sticker_name>. Try again!`")
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

func handleRandom(s *discordgo.Session, m *discordgo.MessageCreate, sm *sticker.Manager, command []string) {
	sm.RLock()
	defer sm.RUnlock()

	stickers := sm.MatchedStickers(buildPatternGroups(command))

	if len(stickers) == 0 {
		replyNormal(s, m, "Cannot find any matched sticker. Find the sticker names with `"+commandPrefix+"/list` command.")
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

func handleSticker(s *discordgo.Session, m *discordgo.MessageCreate, sm *sticker.Manager, command []string) {
	sm.RLock()
	defer sm.RUnlock()

	pg := buildPatternGroups(command)
	if len(pg) > 1 {
		replyNormal(s, m, "List command should not contain slash ('/').")
		return
	}

	stickers := sm.MatchedStickers(pg)
	if len(stickers) == 0 {
		replyNormal(s, m, "Cannot find the sticker you're looking for. Find the sticker name with `"+commandPrefix+"/list` command.")
		return
	}
	if len(stickers) > 1 {
		matchedStr := sticker.StickerListString(stickers)
		replyNormal(s, m, "Found more than one stickers! Please provide more specific patterns. Matched: "+matchedStr)
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
		"/help",
		"Show this message.",
	}, {
		"/list [<pattern>...[ / <pattern>...]...]",
		"If no pattern is given, list all stickers; Otherwise, list all stickers matching any group of patterns. Groups are separated with slashes.",
	}, {
		"/add <sticker_name> <URL>",
		"Download and save the image at `<URL>` as a new sticker.",
	}, {
		"/rename <sticker_name> <new_sticker_name>",
		"Move the sticker on `<sticker_name>` to `<new_sticker_name>`.",
	}, {
		"/random [<pattern>...[ / <pattern>...]...]",
		"All stickers that match any group of patterns will be collected, and a random one will be post. Groups are separated with slashes.",
	}, {
		"<pattern>...",
		"A command that does not start with slash is considered as patterns. A sticker is posted if it's the only one that matches the patterns. Use `/list` command to view the available stickers.",
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
		resourcePathPtr   = flag.String("resource-path", "resources", "The root directory of the resources. Each directory in it will become the group name.")
		configFilePathPtr = flag.String("config-file", "config.json", "The JSON format configuration file. See config.json.example for the supported configs.")
	)

	flag.Parse()
	if !flag.Parsed() {
		flag.PrintDefaults()
	}

	var config struct {
		Token         string
		CommandPrefix string
		CoolDown      int
	}
	configBtyes, err := ioutil.ReadFile(*configFilePathPtr)
	if err != nil {
		log.Fatalln("Failed to read the config file:", err)
	}
	if err := json.Unmarshal(configBtyes, &config); err != nil {
		log.Fatalln("Failed to unmarshal the config file:", err)
	}

	commandPrefix = config.CommandPrefix
	coolDown = time.Duration(config.CoolDown) * time.Second

	userHelp = buildUserHelp()

	log.SetFlags(log.Ldate | log.Ltime | log.Lmicroseconds | log.Lshortfile | log.Lmsgprefix)

	log.Println("Starting...")
	log.Println("\tresource directory =", *resourcePathPtr)
	log.Println("\tconfig file        =", *configFilePathPtr)
	log.Println("\t\tcommand prefix     =", commandPrefix)
	log.Println("\t\tcool down interval =", coolDown)

	rand.Seed(time.Now().UnixNano())

	sm, err := sticker.NewManager(*resourcePathPtr)
	if err != nil {
		log.Fatalln("Failed to collect the sticker info:", err)
	}

	s, err := discordgo.New("Bot " + strings.TrimSpace(string(config.Token)))
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

		command := strings.Fields(m.Content[len(commandPrefix):])
		if len(command) == 0 {
			replyDM(s, m, userHelp)
			return
		}

		// Non-command case.
		if command[0][0] != '/' {
			if coolDown == 0 || cd.CoolDown(coolDown, m.ChannelID) {
				handleSticker(s, m, sm, command)
			}
			return
		}

		var matchedCommands []string
		for _, comm := range []string{"help", "list", "add", "rename", "random"} {
			if strings.HasPrefix(comm, command[0][1:]) {
				matchedCommands = append(matchedCommands, comm)
			}
		}

		if len(matchedCommands) > 1 {
			for i, comm := range matchedCommands {
				matchedCommands[i] = fmt.Sprintf("`%s/%s`", commandPrefix, comm)
			}
			replyNormal(s, m, "Matched more then 1 commands: "+strings.Join(matchedCommands, ", "))
			return
		}

		if len(matchedCommands) < 1 {
			replyDM(s, m, fmt.Sprintf("Unknown command `%s%s`\n", commandPrefix, command[0])+userHelp)
			return
		}

		command = command[1:]

		switch matchedCommands[0] {
		case "help":
			replyDM(s, m, userHelp)
		case "list":
			handleList(s, m, sm, command)
		case "add":
			handleAdd(s, m, sm, command)
		case "rename":
			handleRename(s, m, sm, command)
		case "random":
			if coolDown == 0 || cd.CoolDown(coolDown, m.ChannelID) {
				handleRandom(s, m, sm, command)
			}
		default:
			panic("Should not go here")
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
