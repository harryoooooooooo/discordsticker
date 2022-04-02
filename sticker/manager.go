package sticker

import (
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
)

// StickerListString composes the report of a list of stickers.
func StickerListString(stickers []*Sticker, isFullPath bool) string {
	moreThanTen := false
	if len(stickers) > 10 {
		stickers = stickers[:10]
		moreThanTen = true
	}
	var matchedNames []string
	for _, s := range stickers {
		if isFullPath {
			matchedNames = append(matchedNames, s.StringWithHintFull())
		} else {
			matchedNames = append(matchedNames, s.StringWithHint())
		}
	}
	sb := strings.Builder{}
	sb.WriteString("`")
	sb.WriteString(strings.Join(matchedNames, "`, `"))
	sb.WriteString("`")
	if moreThanTen {
		sb.WriteString("... and more")
	}
	return sb.String()
}

// GroupListString composes the report of a list of group.
func GroupListString(groups []*Group) string {
	moreThanTen := false
	if len(groups) > 10 {
		groups = groups[:10]
		moreThanTen = true
	}
	var matchedNames []string
	for _, s := range groups {
		matchedNames = append(matchedNames, s.StringWithHint())
	}
	sb := strings.Builder{}
	sb.WriteString("`")
	sb.WriteString(strings.Join(matchedNames, "`, `"))
	sb.WriteString("`")
	if moreThanTen {
		sb.WriteString("... and more")
	}
	return sb.String()
}

// withHint returns the string with the optional part is hinted.
// Note that the string is processed as []rune.
// Sample input:
// 	s = "abcde", uniqLen = 3
// Sample output:
// 	"abc[de]"
func withHint(s string, uniqLen int) string {
	rs := []rune(s)
	if len(rs) == uniqLen {
		return s
	}
	return fmt.Sprintf(
		"%s[%s]",
		string(rs[:uniqLen]),
		string(rs[uniqLen:]),
	)
}

type Sticker struct {
	name        string
	ext         string
	uniqLen     int
	uniqLenGlob int

	group *Group
}

func (s *Sticker) StringWithHintFull() string {
	return s.group.StringWithHint() + "/" + withHint(s.name, s.uniqLen)
}

func (s *Sticker) StringWithHint() string {
	return withHint(s.name, s.uniqLenGlob)
}

func (s *Sticker) Path() string {
	return filepath.Join(s.group.Path(), s.name+"."+s.ext)
}

func (s *Sticker) Ext() string {
	return s.ext
}

type Group struct {
	name     string
	uniqLen  int
	stickers []*Sticker

	root string
}

func (g *Group) Stickers() []*Sticker {
	return g.stickers
}

func (g *Group) StringWithHint() string {
	return withHint(g.name, g.uniqLen)
}

func (g *Group) Path() string {
	return filepath.Join(g.root, g.name)
}

// Manager holds the cached sticker info.
// Note that it's caller's responsibility to lock the resource.
type Manager struct {
	root   string
	groups []*Group

	mu sync.RWMutex
}

func NewManager(root string) (*Manager, error) {
	m := &Manager{root: root}
	if err := m.update(); err != nil {
		return nil, err
	}
	return m, nil
}

func countUniqLenTwo(a, b string) int {
	ra := []rune(a)
	rb := []rune(b)
	i := 0
	for i < len(ra) && i < len(rb) && ra[i] == rb[i] {
		i++
	}
	return i
}

type countUniqLenError struct {
	str1 string
	str2 string
}

func (e *countUniqLenError) Error() string {
	return fmt.Sprintf("Found contained strings: %q vs %q", e.str1, e.str2)
}

// countUniqLen finds the unique prefix length among the whole slice for all strings.
// An unique prefix means there is no other string has the same prefix.
func countUniqLen(strs []string) ([]int, error) {
	ret := make([]int, len(strs))
	for i := 0; i < len(strs); i++ {
		for j := i + 1; j < len(strs); j++ {
			s := strs[i]
			t := strs[j]
			if s == t || strings.HasPrefix(s, t) || strings.HasPrefix(t, s) {
				return nil, &countUniqLenError{str1: s, str2: t}
			}
			l := countUniqLenTwo(s, t) + 1
			if ret[i] < l {
				ret[i] = l
			}
			if ret[j] < l {
				ret[j] = l
			}
		}
	}
	return ret, nil
}

func (m *Manager) update() error {
	var newGroups []*Group

	dirsPath, err := filepath.Glob(filepath.Join(m.root, "*"))
	if err != nil {
		return err
	}
	dirsBase := make([]string, len(dirsPath))
	for i, d := range dirsPath {
		dirsBase[i] = filepath.Base(d)
	}
	dirsUniqLen, err := countUniqLen(dirsBase)
	if err != nil {
		return err
	}

	var allImgsName []string
	for dirI, dirPath := range dirsPath {
		imgsPath, err := filepath.Glob(filepath.Join(dirPath, "*"))
		if err != nil {
			return err
		}

		newGroup := &Group{
			name:     dirsBase[dirI],
			uniqLen:  dirsUniqLen[dirI],
			stickers: make([]*Sticker, len(imgsPath)),
			root:     m.root,
		}

		imgsName := make([]string, len(imgsPath))
		imgsExt := make([]string, len(imgsPath))
		for i, im := range imgsPath {
			b := filepath.Base(im)
			e := filepath.Ext(im)
			imgsName[i] = b[:len(b)-len(e)]
			imgsExt[i] = e[1:]
		}

		imgsUniqLen, err := countUniqLen(imgsName)
		if err != nil {
			return err
		}

		for i, l := range imgsUniqLen {
			newGroup.stickers[i] = &Sticker{
				name:    imgsName[i],
				ext:     imgsExt[i],
				uniqLen: l,
				group:   newGroup,
			}
		}

		newGroups = append(newGroups, newGroup)
		allImgsName = append(allImgsName, imgsName...)
	}

	allImgsUniqLen, err := countUniqLen(allImgsName)
	if err != nil {
		return err
	}

	i := 0
	for _, g := range newGroups {
		for _, s := range g.stickers {
			s.uniqLenGlob = allImgsUniqLen[i]
			i++
		}
	}

	m.groups = newGroups

	return nil
}

var UninformableErr = errors.New("Error uninformable to user")

const (
	AddStickerSizeLimit = 3500000
)

// AddSticker adds downloads the sticker to local and updates the sticker data.
// UninformableErr is returned when there is an internal error occurs;
// Otherwise there is probably an error caused by user and the error object may cantain advice if any.
func (m *Manager) AddSticker(path, url string) (retErr error) {
	idSplitted := strings.Split(path, "/")
	if len(idSplitted) != 2 || idSplitted[0] == "" || idSplitted[1] == "" {
		return errors.New("Invalid path. Expect `<group_name>/<sticker_name>` but got `" + path + "`")
	}
	groupName := idSplitted[0]
	stickerName := idSplitted[1]

	if ss := m.MatchedStickers(path); len(ss) != 0 {
		matchedStr := StickerListString(ss, true)
		return errors.New("The name has already matched the following stickers: " + matchedStr)
	}

	resp, err := http.Head(url)
	if err != nil {
		log.Printf("Failed to HEAD URL=%q: %v\n", url, err)
		return errors.New("Failed to download the image. Is it a valid URL?")
	}

	ctype := resp.Header.Get("Content-Type")
	switch ctype {
	case "image/png", "image/jpeg", "image/gif":
		// Valid types. Nothing to do.
	default:
		return errors.New("Invalid URL content type. Only png, jpeg, and gif are supported.")
	}

	size, err := strconv.Atoi(resp.Header.Get("Content-Length"))
	if err != nil {
		log.Println("Failed to convert the content length to integer:", err)
		return errors.New("Invalid Content-Length from the URL. Is it a valid URL?")
	}
	if size > AddStickerSizeLimit {
		return errors.New(fmt.Sprintf("Image size too large. Expect < %dB, got %d", AddStickerSizeLimit, size))
	}

	resp, err = http.Get(url)
	if err != nil {
		log.Printf("Failed to GET URL=%q: %v\n", url, err)
		return UninformableErr
	}
	defer resp.Body.Close()

	dirPath := filepath.Join(m.root, groupName)
	if _, err := os.Stat(dirPath); os.IsNotExist(err) {
		if err := os.Mkdir(dirPath, 0755); err != nil {
			log.Println("Failed to create new directory:", err)
			return UninformableErr
		}
		defer func() {
			if retErr != nil {
				os.Remove(dirPath)
			}
		}()
	}

	ext := ctype[len("image/"):]
	imgPath := filepath.Join(dirPath, stickerName+"."+ext)
	w, err := os.Create(imgPath)
	if err != nil {
		log.Println("Failed to create a new file:", err)
		return UninformableErr
	}
	defer func() {
		if retErr != nil {
			os.Remove(imgPath)
		}
	}()
	defer w.Close()

	if _, err := io.Copy(w, resp.Body); err != nil {
		log.Println("Failed to write the image:", err)
		return UninformableErr
	}

	w.Close()

	if err := m.update(); err != nil {
		if _, ok := err.(*countUniqLenError); ok {
			return fmt.Errorf("The new sticker path cover the existing sticker path: %w", err)
		}
		log.Println("Failed to update sticker info:", err)
		return UninformableErr
	}

	return nil
}

// RenameSticker renames the sticker
// UninformableErr is returned when there is an internal error occurs;
// Otherwise there is probably an error caused by user and the error object may cantain advice if any.
func (m *Manager) RenameSticker(src, dst string) (retErr error) {
	srcMatched := m.MatchedStickers(src)
	if len(srcMatched) < 1 {
		return errors.New("Sticker not found.")
	}
	if len(srcMatched) > 1 {
		matchedStr := StickerListString(srcMatched, strings.Contains(src, "/"))
		return errors.New("Found more than one stickers. Matched: " + matchedStr)
	}

	dstSplitted := strings.Split(dst, "/")
	if len(dstSplitted) != 2 || dstSplitted[0] == "" || dstSplitted[1] == "" {
		return errors.New("Invalid path. Expect `<group_name>/<sticker_name>` but got `" + dst + "`")
	}

	dstMatched := m.MatchedStickers(dst)
	if len(dstMatched) > 1 || (len(dstMatched) == 1 && dstMatched[0] != srcMatched[0]) {
		return errors.New("The new path already matched existing stickers: " + StickerListString(dstMatched, true))
	}

	dstDir := filepath.Join(m.root, dstSplitted[0])
	if _, err := os.Stat(dstDir); os.IsNotExist(err) {
		if err := os.Mkdir(dstDir, 0755); err != nil {
			log.Println("Failed to create new directory:", err)
			return UninformableErr
		}
		defer func() {
			if retErr != nil {
				if err := os.Remove(dstDir); err != nil {
					log.Println("Failed to remove the newly created directory:", err)
				}
			}
		}()
	}

	srcPath := srcMatched[0].Path()
	dstPath := filepath.Join(m.root, dst+"."+srcMatched[0].Ext())
	if err := os.Rename(srcPath, dstPath); err != nil {
		log.Println("Failed to move the image:", err)
		return UninformableErr
	}
	defer func() {
		if retErr != nil {
			if err := os.Rename(dstPath, srcPath); err != nil {
				log.Println("Failed to remove the image back:", err)
			}
		}
	}()

	srcDir := srcMatched[0].group.Path()
	dirsPath, err := filepath.Glob(filepath.Join(srcDir, "*"))
	if err != nil {
		log.Println("Failed to count the file number in the source directory:", err)
		return UninformableErr
	}
	if len(dirsPath) == 0 {
		// Remove the directory if the group becomes empty.
		if err := os.Remove(srcDir); err != nil {
			log.Println("Failed to remove the directory:", err)
			return UninformableErr
		}
		defer func() {
			if retErr != nil {
				if err := os.Mkdir(srcDir, 0755); err != nil {
					log.Println("Failed to create the source directory back:", err)
				}
			}
		}()
	}

	if err := m.update(); err != nil {
		if _, ok := err.(*countUniqLenError); ok {
			return fmt.Errorf("The new sticker path cover the existing sticker path: %w", err)
		}
		log.Println("Failed to update sticker info:", err)
		return UninformableErr
	}

	return nil
}

func (m *Manager) MatchedStickers(id string) []*Sticker {
	tok := strings.Split(id, "/")
	if len(tok) > 2 {
		return nil
	}
	groups, name := m.groups, tok[0]
	if len(tok) > 1 {
		groups, name = m.MatchedGroups(tok[0]), tok[1]
	}
	var ret []*Sticker
	for _, g := range groups {
		for _, s := range g.stickers {
			if strings.HasPrefix(s.name, name) {
				ret = append(ret, s)
			}
		}
	}
	return ret
}

func (m *Manager) MatchedGroups(groupName string) []*Group {
	var ret []*Group
	for _, g := range m.groups {
		if strings.HasPrefix(g.name, groupName) {
			ret = append(ret, g)
		}
	}
	return ret
}

func (m *Manager) Groups() []*Group {
	return m.groups
}

func (m *Manager) Lock()    { m.mu.Lock() }
func (m *Manager) Unlock()  { m.mu.Unlock() }
func (m *Manager) RLock()   { m.mu.RLock() }
func (m *Manager) RUnlock() { m.mu.RUnlock() }
