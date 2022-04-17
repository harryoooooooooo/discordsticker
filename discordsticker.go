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

	"github.com/bwmarrin/discordgo"
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

func handleList(s *discordgo.Session, m *discordgo.MessageCreate, sm *sticker.Manager, commandPrefix string, command []string) {
	sm.RLock()
	defer sm.RUnlock()

	if len(command) == 0 {
		sb := strings.Builder{}
		sb.WriteString("```\n")
		for _, g := range sm.Groups() {
			sb.WriteString(g.StringWithHint())
			sb.WriteString("\n")
		}
		sb.WriteString("```\n")
		sb.WriteString("Use `" + commandPrefix + "list <group_name>` to query the available stickers inside a group.")
		replyDM(s, m, sb.String())
		return
	}

	groupName := command[0]
	gs := sm.MatchedGroups(groupName)
	if len(gs) == 0 {
		replyDM(s, m, "No matched group name! Use `"+commandPrefix+"list` to query the group list.")
		return
	}
	if len(gs) > 1 {
		replyDM(s, m, "Matched more than one group names! Please provide more specific prefix. Matched: "+sticker.GroupListString(gs))
		return
	}

	sb := strings.Builder{}
	sb.WriteString("```\n")
	for _, s := range gs[0].Stickers() {
		sb.WriteString(s.StringWithHint())
		sb.WriteString("\n\t\t\t= ")
		sb.WriteString(s.StringWithHintFull())
		sb.WriteString("\n")
	}
	sb.WriteString("```\n")
	sb.WriteString("Each sticker has two valid names -- One includes directory (group) name and one works globally.\n")
	sb.WriteString("Note that the characters inside the brackets are optional. That is, a sticker shown as `巧[克力]` can be specified with `巧`, `巧克`, and `巧克力`.")
	replyDM(s, m, sb.String())
}

func handleAdd(s *discordgo.Session, m *discordgo.MessageCreate, sm *sticker.Manager, commandPrefix string, command []string) {
	sm.Lock()
	defer sm.Unlock()

	if len(command) != 2 {
		replyNormal(s, m, "Invalid format. Expect `"+commandPrefix+"add <full_path> <URL>`. Try again!")
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

func handleRename(s *discordgo.Session, m *discordgo.MessageCreate, sm *sticker.Manager, commandPrefix string, command []string) {
	sm.Lock()
	defer sm.Unlock()

	if len(command) != 2 {
		replyNormal(s, m, "Invalid format. Expect `"+commandPrefix+"rename <sticker_name> <new_full_path>. Try again!`")
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

func handleRandom(s *discordgo.Session, m *discordgo.MessageCreate, sm *sticker.Manager, commandPrefix string, stickerIDs []string) {
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

	if _, err := s.ChannelFileSend(m.ChannelID, "sticker."+stickers[0].Ext(), r); err != nil {
		log.Println("Failed to post sticker:", err)
		replyNormal(s, m, "Failed to post sticker!")
		return
	}
}

func handleSticker(s *discordgo.Session, m *discordgo.MessageCreate, sm *sticker.Manager, commandPrefix, stickerID string) {
	sm.RLock()
	defer sm.RUnlock()

	stickers := sm.MatchedStickers(stickerID)
	if len(stickers) == 0 {
		replyNormal(s, m, "Cannot find the sticker you're looking for. Find the sticker name with `"+commandPrefix+"list` command.")
		return
	}
	if len(stickers) > 1 {
		matchedStr := sticker.StickerListString(stickers, strings.Contains(stickerID, "/"))
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

	if _, err := s.ChannelFileSend(m.ChannelID, "sticker."+stickers[0].Ext(), r); err != nil {
		log.Println("Failed to post sticker:", err)
		replyNormal(s, m, "Failed to post sticker!")
		return
	}
}

func userHelp(commandPrefix string) string {
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
		"list [<group_name>]",
		"If `<group_name>` is not given, list all directory (group) names; Otherwise, list all stickers inside `<group_name>`.",
	}, {
		"add <full_path> <URL>",
		"Download and save the image at `<URL>` as a new sticker. Note that a full path should includes directory name.",
	}, {
		"rename <sticker_name> <new_full_path>",
		"Move the sticker on `<sticker_name>` to `<new_full_path>`. Note that a full path should includes directory name.",
	}, {
		"random [<group_prefix>/]<sticker_prefix>...",
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
	)

	flag.Parse()
	if !flag.Parsed() {
		flag.PrintDefaults()
	}

	var (
		commandPrefix = *commandPrefixPtr
		tokenFilePath = *tokenFilePathPtr
		resourcePath  = *resourcePathPtr
	)

	log.SetFlags(log.Ldate | log.Ltime | log.Lmicroseconds | log.Lshortfile | log.Lmsgprefix)

	log.Println("Starting...")
	log.Println("\tcommand prefix     =", commandPrefix)
	log.Println("\ttoken file         =", tokenFilePath)
	log.Println("\tresource directory =", resourcePath)

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

	userHelpStr := userHelp(commandPrefix)

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
		if command[0] == "" {
			command = []string{"help"}
		}

		switch command[0] {
		case "help":
			replyDM(s, m, userHelpStr)
		case "list":
			handleList(s, m, sm, commandPrefix, command[1:])
		case "add":
			handleAdd(s, m, sm, commandPrefix, command[1:])
		case "rename":
			handleRename(s, m, sm, commandPrefix, command[1:])
		case "random":
			handleRandom(s, m, sm, commandPrefix, command[1:])
		default: // Consider the command name as the sticker ID.
			handleSticker(s, m, sm, commandPrefix, command[0])
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
