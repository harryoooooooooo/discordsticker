package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"math/rand"
	"os"
	"os/signal"
	"path/filepath"
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
)

type handler interface {
	postSticker(poster io.Reader, ext string) error
	replyPrivate(msg string)
	replyPublic(msg string)
}

type messageHandler struct {
	s *discordgo.Session
	m *discordgo.MessageCreate
}

// replyPublic sends message back to the channel from where we got the message.
func (h *messageHandler) replyPublic(msg string) {
	if _, err := h.s.ChannelMessageSend(h.m.ChannelID, msg); err != nil {
		log.Println("Failed to reply:", err)
	}
}

// replyPrivate sends message as DM.
// If the message was sent from a guild, only a done message will be sent to the guild.
func (h *messageHandler) replyPrivate(msg string) {
	if h.m.GuildID == "" {
		// Got command from DM, simple case.
		if _, err := h.s.ChannelMessageSend(h.m.ChannelID, msg); err != nil {
			log.Println("Failed to reply:", err)
		}
		return
	}

	// Got command from guild but we replies with DM. The function informs guild on failure.
	informFailure := func() {
		if _, err := h.s.ChannelMessageSend(h.m.ChannelID, "Failed to send you a DM!"); err != nil {
			log.Println("Failed to inform failure to guild:", err)
		}
	}

	userChannel, err := h.s.UserChannelCreate(h.m.Author.ID)
	if err != nil {
		log.Println("Failed to create a DM channel:", err)
		informFailure()
		return
	}
	if _, err := h.s.ChannelMessageSend(userChannel.ID, msg); err != nil {
		log.Println("Failed to reply:", err)
		informFailure()
		return
	}
	if _, err := h.s.ChannelMessageSend(h.m.ChannelID, "Direct message is sent!"); err != nil {
		log.Println("Failed to send done message to guild:", err)
	}
}

func (h *messageHandler) postSticker(r io.Reader, ext string) error {
	_, err := h.s.ChannelFileSend(h.m.ChannelID, "sticker"+ext, r)
	return err
}

type commandHandler struct {
	s       *discordgo.Session
	i       *discordgo.InteractionCreate
	replied bool
}

func (h *commandHandler) reply(msg string, ephemeral bool, components []discordgo.MessageComponent) {
	var flags discordgo.MessageFlags
	if ephemeral {
		flags = discordgo.MessageFlagsEphemeral
	}
	if h.replied {
		if _, err := h.s.FollowupMessageCreate(
			h.i.Interaction,
			true,
			&discordgo.WebhookParams{Content: msg, Components: components, Flags: flags},
		); err != nil {
			log.Println("Failed to reply a followup message:", err)
		}
		return
	}
	h.replied = true
	if err := h.s.InteractionRespond(h.i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{Content: msg, Components: components, Flags: flags},
	}); err != nil {
		log.Println("Failed to reply:", err)
	}
}

func (h *commandHandler) replyPrivate(msg string) {
	h.reply(msg, true, nil)
}

func (h *commandHandler) replyPublic(msg string) {
	h.reply(msg, false, nil)
}

func (h *commandHandler) postSticker(r io.Reader, ext string) error {
	files := []*discordgo.File{{
		Name:        "sticker" + ext,
		ContentType: "image/" + ext[1:],
		Reader:      r,
	}}
	if h.replied {
		_, err := h.s.FollowupMessageCreate(h.i.Interaction, true, &discordgo.WebhookParams{Files: files})
		return err
	}
	h.replied = true
	return h.s.InteractionRespond(h.i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{Files: files},
	})
}

func buildPatternGroups(arg string) [][]string {
	var toks []string
	for _, f := range strings.Fields(arg) {
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

func handleList(h handler, sm *sticker.Manager, patterns string) {
	sm.RLock()
	defer sm.RUnlock()

	var ss []*sticker.Sticker
	if patterns == "" {
		ss = sm.Stickers()
	} else {
		ss = sm.MatchedStickers(buildPatternGroups(patterns))
	}

	if len(ss) == 0 {
		h.replyPrivate("No matched stickers found!")
		return
	}

	msgs := make([]string, len(ss))
	for i, s := range ss {
		msgs[i] = s.Name()
	}

	for _, msg := range quotedMessagesToTrunks(msgs) {
		h.replyPrivate(msg)
	}
}

func handleAdd(h handler, sm *sticker.Manager, name, url string) {
	sm.Lock()
	defer sm.Unlock()

	if err := sm.AddSticker(name, url); err != nil {
		if err != sticker.UninformableErr {
			h.replyPublic(err.Error())
		} else {
			h.replyPublic("Something goes wrong here! Please contact the admin.")
		}
		return
	}
	h.replyPublic(fmt.Sprintf("Done. Added sticker: `%s`", name))
}

func handleRename(h handler, sm *sticker.Manager, name, newName string) {
	sm.Lock()
	defer sm.Unlock()

	if err := sm.RenameSticker(name, newName); err != nil {
		if err != sticker.UninformableErr {
			h.replyPublic(err.Error())
		} else {
			h.replyPublic("Something goes wrong here! Please contact the admin.")
		}
		return
	}
	h.replyPublic(fmt.Sprintf("Done. Renamed sticker: `%s` -> `%s`", name, newName))
}

func handleRandom(h handler, sm *sticker.Manager, patterns string) {
	sm.RLock()
	defer sm.RUnlock()

	stickers := sm.MatchedStickers(buildPatternGroups(patterns))

	if len(stickers) == 0 {
		h.replyPublic("Cannot find any matched sticker. Find the sticker names with `list` command.")
		return
	}

	sticker := stickers[rand.Intn(len(stickers))]

	r, err := os.Open(sticker.Path())
	if err != nil {
		log.Println("Failed to open the image:", err)
		h.replyPublic("Something goes wrong here! Please contact the admin.")
		return
	}
	defer r.Close()

	if err := h.postSticker(r, sticker.Ext()); err != nil {
		log.Println("Failed to post sticker:", err)
		h.replyPublic("Something goes wrong here! Please contact the admin.")
		return
	}
}

func handlePost(h handler, sm *sticker.Manager, pattern string, handleMulti func([]*sticker.Sticker)) {
	sm.RLock()
	defer sm.RUnlock()

	pg := buildPatternGroups(pattern)
	if len(pg) > 1 {
		h.replyPublic("Post command should not contain slash (`/`).")
		return
	}

	stickers := sm.MatchedStickers(pg)
	if len(stickers) == 0 {
		h.replyPublic("Cannot find the sticker you're looking for. Find the sticker name with `list` command.")
		return
	}
	if len(stickers) > 1 {
		if handleMulti != nil {
			handleMulti(stickers)
		} else {
			matchedStr := sticker.StickerListString(stickers)
			h.replyPublic("Found more than one stickers! Please provide more specific patterns. Matched: " + matchedStr)
		}
		return
	}

	r, err := os.Open(stickers[0].Path())
	if err != nil {
		log.Println("Failed to open the image:", err)
		h.replyPublic("Something goes wrong here! Please contact the admin.")
		return
	}
	defer r.Close()

	if err := h.postSticker(r, stickers[0].Ext()); err != nil {
		log.Println("Failed to post sticker:", err)
		h.replyPublic("Something goes wrong here! Please contact the admin.")
		return
	}
}

func handleHelp(h handler, appCommand bool) {
	sb := strings.Builder{}
	if !appCommand {
		sb.WriteString("Please send commands with the prefix `")
		sb.WriteString(commandPrefix)
		sb.WriteString("`.\n")
	}
	sb.WriteString("Available commands:\n\n")
	for _, t := range []struct {
		command string
		args    string
		desc    string
	}{{
		"help", "",
		"Show this message.",
	}, {
		"list", "[<pattern>...[ / <pattern>...]...]",
		"If no pattern is given, list all stickers; Otherwise, list all stickers matching any group of patterns. Groups are separated with slashes.",
	}, {
		"add", "<sticker_name> <URL>",
		"Download and save the image at `<URL>` as a new sticker.",
	}, {
		"rename", "<sticker_name> <new_sticker_name>",
		"Move the sticker on `<sticker_name>` to `<new_sticker_name>`.",
	}, {
		"random", "[<pattern>...[ / <pattern>...]...]",
		"All stickers that match any group of patterns will be collected, and a random one will be post. Groups are separated with slashes.",
	}, {
		"post", "<pattern>...",
		"A command that does not start with slash is considered as patterns. A sticker is posted if it's the only one that matches the patterns. Use `list` command to view the available stickers.",
	}} {
		sb.WriteString("`")
		if appCommand {
			sb.WriteString("/sticker ")
			sb.WriteString(t.command)
			sb.WriteString(" ")
			sb.WriteString(t.args)
		} else {
			sb.WriteString(commandPrefix)
			if t.command != "post" {
				sb.WriteString("/")
				sb.WriteString(t.command)
				sb.WriteString(" ")
			}
			sb.WriteString(t.args)
		}
		sb.WriteString("`\n")
		sb.WriteString(t.desc)
		sb.WriteString("\n\n")
	}
	h.replyPrivate(sb.String())
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
		AppID         string
		CommandPrefix string
		CoolDown      int
		CaseSensitive bool
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

	log.SetFlags(log.Ldate | log.Ltime | log.Lmicroseconds | log.Lshortfile | log.Lmsgprefix)

	log.Println("Starting...")
	log.Println("\tresource directory =", *resourcePathPtr)
	log.Println("\tconfig file        =", *configFilePathPtr)
	log.Println("\t\tcommand prefix     =", commandPrefix)
	log.Println("\t\tcool down interval =", coolDown)
	log.Println("\t\tcase sensitive     =", config.CaseSensitive)

	rand.Seed(time.Now().UnixNano())

	sm, err := sticker.NewManager(*resourcePathPtr, sticker.CaseSensitive(config.CaseSensitive))
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

		h := &messageHandler{s: s, m: m}

		command := strings.TrimSpace(m.Content[len(commandPrefix):])
		if command == "" {
			command = "/help"
		}

		// Non-command case.
		if command[0] != '/' {
			if coolDown == 0 || cd.CoolDown(coolDown, m.ChannelID) {
				handlePost(h, sm, command, nil)
			} else {
				h.replyPublic("Cooling down.")
			}
			return
		}

		command, arg, _ := strings.Cut(command[1:], " ")

		var matchedCommands []string
		for _, comm := range []string{"help", "list", "add", "rename", "random"} {
			if strings.HasPrefix(comm, command) {
				matchedCommands = append(matchedCommands, comm)
			}
		}
		if len(matchedCommands) > 1 {
			for i, comm := range matchedCommands {
				matchedCommands[i] = fmt.Sprintf("`%s/%s`", commandPrefix, comm)
			}
			h.replyPublic("Matched more then 1 commands: " + strings.Join(matchedCommands, ", "))
			return
		}
		if len(matchedCommands) < 1 {
			h.replyPrivate(fmt.Sprintf("Unknown command `%s/%s`. Run `%s/help` to see the supported commands.\n", commandPrefix, command, commandPrefix))
			return
		}

		switch matchedCommands[0] {
		case "help":
			handleHelp(h, false)
		case "list":
			handleList(h, sm, arg)
		case "add":
			args := strings.Fields(arg)
			if len(args) != 2 {
				h.replyPublic("Invalid format. Expect `" + commandPrefix + "/add <sticker_name> <URL>`.")
				return
			}
			handleAdd(h, sm, args[0], args[1])
		case "rename":
			args := strings.Fields(arg)
			if len(args) != 2 {
				h.replyPublic("Invalid format. Expect `" + commandPrefix + "/rename <sticker_name> <new_sticker_name>.`")
				return
			}
			handleRename(h, sm, args[0], args[1])
		case "random":
			if coolDown == 0 || cd.CoolDown(coolDown, m.ChannelID) {
				handleRandom(h, sm, arg)
			} else {
				h.replyPublic("Cooling down.")
			}
		default:
			panic("Should not go here")
		}
	})

	s.AddHandler(func(s *discordgo.Session, i *discordgo.InteractionCreate) {
		h := &commandHandler{s: s, i: i}
		switch i.Type {
		case discordgo.InteractionApplicationCommand:
			if i.ApplicationCommandData().Name != "sticker" {
				h.replyPrivate("Unsupported command, please contact the admin")
				return
			}
			if len(i.ApplicationCommandData().Options) != 1 {
				h.replyPrivate("Invalid command format, please contact the admin")
				return
			}

			data := i.ApplicationCommandData().Options[0]

			getOptionString := func(name string) string {
				for _, o := range data.Options {
					if o.Name == name {
						return o.StringValue()
					}
				}
				return ""
			}

			switch data.Name {
			case "help":
				handleHelp(h, true)
			case "list":
				handleList(h, sm, getOptionString("patterns"))
			case "add":
				handleAdd(h, sm, getOptionString("name"), getOptionString("url"))
			case "rename":
				handleRename(h, sm, getOptionString("name"), getOptionString("new_name"))
			case "random":
				if coolDown == 0 || cd.CoolDown(coolDown, i.ChannelID) {
					handleRandom(h, sm, getOptionString("patterns"))
				} else {
					h.replyPublic("Cooling down.")
				}
			case "post":
				if coolDown != 0 && !cd.CoolDown(coolDown, i.ChannelID) {
					h.replyPublic("Cooling down.")
					return
				}
				handlePost(h, sm, getOptionString("pattern"), func(ss []*sticker.Sticker) {
					const (
						maxRowCount    = 5
						maxColumnCount = 5
					)
					var buttons []discordgo.MessageComponent
					for i, s := range ss {
						if i%2 == 0 {
							buttons = append(buttons, discordgo.Button{
								Label:    s.Name(),
								Style:    discordgo.PrimaryButton,
								Disabled: false,
								CustomID: s.Path(),
							})
						} else {
							buttons = append(buttons, discordgo.Button{
								Label:    s.Name(),
								Style:    discordgo.SecondaryButton,
								Disabled: false,
								CustomID: s.Path(),
							})
						}
					}
					for compBegin := 0; compBegin < len(buttons); compBegin += maxRowCount * maxColumnCount {
						compEnd := compBegin + maxRowCount*maxColumnCount
						if compEnd > len(buttons) {
							compEnd = len(buttons)
						}
						content := fmt.Sprintf("Showing %d ~ %d matched stickers:", compBegin+1, compEnd)
						var components []discordgo.MessageComponent
						for rowBegin := compBegin; rowBegin < compEnd; rowBegin += maxColumnCount {
							rowEnd := rowBegin + maxColumnCount
							if rowEnd > compEnd {
								rowEnd = compEnd
							}
							components = append(components, discordgo.ActionsRow{Components: buttons[rowBegin:rowEnd]})
						}
						h.reply(content, true, components)
					}
					cd.RemoveCoolDown(i.ChannelID)
				})
			default:
				panic("Should not go here")
			}
		case discordgo.InteractionMessageComponent:
			if coolDown != 0 && !cd.CoolDown(coolDown, i.ChannelID) {
				h.replyPrivate("Cooling down.")
				return
			}
			path := i.MessageComponentData().CustomID
			r, err := os.Open(path)
			if err != nil {
				log.Println("Failed to open the image:", err)
				h.replyPrivate("Something goes wrong here! Please contact the admin.")
				return
			}
			defer r.Close()

			if err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
				Type: discordgo.InteractionResponseDeferredChannelMessageWithSource,
				Data: &discordgo.InteractionResponseData{Flags: discordgo.MessageFlagsEphemeral},
			}); err != nil {
				log.Println("Failed to send response:", err)
				h.replyPrivate("Something goes wrong here! Please contact the admin.")
				return
			}
			defer func() {
				if err := s.InteractionResponseDelete(i.Interaction); err != nil {
					log.Println("Failed to delete the deferred message:", err)
				}
			}()

			ext := filepath.Ext(path)
			files := []*discordgo.File{{
				Name:        "sticker" + ext,
				ContentType: "image/" + ext[1:],
				Reader:      r,
			}}
			content := ""
			if i.Member != nil {
				content = i.Member.Mention() + " posted:"
			}
			if _, err := s.ChannelMessageSendComplex(i.ChannelID, &discordgo.MessageSend{
				Content:         content,
				Files:           files,
				AllowedMentions: &discordgo.MessageAllowedMentions{},
			}); err != nil {
				log.Println("Failed to post sticker:", err)
				h.replyPrivate("Something goes wrong here! Please contact the admin.")
				return
			}
		}
	})

	if _, err := s.ApplicationCommandCreate(config.AppID, "", &discordgo.ApplicationCommand{
		Name:        "sticker",
		Description: "Discord sticker command",
		Options: []*discordgo.ApplicationCommandOption{{
			Type:        discordgo.ApplicationCommandOptionSubCommand,
			Name:        "post",
			Description: "Post a sticker",
			Options: []*discordgo.ApplicationCommandOption{{
				Type:        discordgo.ApplicationCommandOptionString,
				Name:        "pattern",
				Required:    true,
				Description: "The search pattern of the sticker",
			}},
		}, {
			Type:        discordgo.ApplicationCommandOptionSubCommand,
			Name:        "help",
			Description: "Print the help info",
		}, {
			Type:        discordgo.ApplicationCommandOptionSubCommand,
			Name:        "list",
			Description: "Search and show the stickers",
			Options: []*discordgo.ApplicationCommandOption{{
				Type:        discordgo.ApplicationCommandOptionString,
				Name:        "patterns",
				Required:    false,
				Description: "The search patterns separated by slashes",
			}},
		}, {
			Type:        discordgo.ApplicationCommandOptionSubCommand,
			Name:        "add",
			Description: "Add a new sticker",
			Options: []*discordgo.ApplicationCommandOption{{
				Type:        discordgo.ApplicationCommandOptionString,
				Name:        "name",
				Required:    true,
				Description: "Sticker name",
			}, {
				Type:        discordgo.ApplicationCommandOptionString,
				Name:        "url",
				Required:    true,
				Description: "A link to download the sticker",
			}},
		}, {
			Type:        discordgo.ApplicationCommandOptionSubCommand,
			Name:        "rename",
			Description: "Rename a sticker",
			Options: []*discordgo.ApplicationCommandOption{{
				Type:        discordgo.ApplicationCommandOptionString,
				Name:        "name",
				Required:    true,
				Description: "Sticker name",
			}, {
				Type:        discordgo.ApplicationCommandOptionString,
				Name:        "new_name",
				Required:    true,
				Description: "A link to download the sticker",
			}},
		}, {
			Type:        discordgo.ApplicationCommandOptionSubCommand,
			Name:        "random",
			Description: "Randomly post a sticker form the search results",
			Options: []*discordgo.ApplicationCommandOption{{
				Type:        discordgo.ApplicationCommandOptionString,
				Name:        "patterns",
				Required:    true,
				Description: "The search patterns separated by slashes",
			}},
		}},
	}); err != nil {
		log.Fatalln("Failed to create app command, err:", err)
	}

	readyCh := make(chan struct{}, 1)
	s.AddHandler(func(s *discordgo.Session, r *discordgo.Ready) {
		readyCh <- struct{}{}
	})

	s.Identify.Intents = discordgo.IntentsGuildMessages | discordgo.IntentsDirectMessages

	if err := s.Open(); err != nil {
		log.Fatalln("Failed to open a connection:", err)
	}
	defer s.Close()

	select {
	case <-readyCh:
	case <-time.After(10 * time.Second):
		log.Println("Failed to wait for the BOT ready after 10sec timeout")
		return
	}
	log.Println("Bot is running now. Press CTRL-C to exit.")
	shutdownCh := make(chan os.Signal, 1)
	signal.Notify(shutdownCh, syscall.SIGINT, syscall.SIGTERM, os.Interrupt, os.Kill)

	<-shutdownCh
	log.Println("Bye~")
}
