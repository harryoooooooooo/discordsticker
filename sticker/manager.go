package sticker

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
)

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

// countUniqLen finds the unique prefix length among the whole slice for all strings.
// An unique prefix means there is no other string has the same prefix.
func countUniqLen(strs []string) ([]int, error) {
	ret := make([]int, len(strs))
	for i := 0; i < len(strs); i++ {
		for j := i + 1; j < len(strs); j++ {
			s := strs[i]
			t := strs[j]
			if s == t || strings.HasPrefix(s, t) || strings.HasPrefix(t, s) {
				return nil, errors.New("Found contained/same strings: " + s + " vs " + t)
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

type AddStickerResult int

const (
	AddStickerNotYetImplErr AddStickerResult = iota
)

func (m *Manager) AddSticker(path, url string) (retRes AddStickerResult) {
	return AddStickerNotYetImplErr
}

type RenameStickerResult int

const (
	RenameStickerNotYetImplErr RenameStickerResult = iota
)

func (m *Manager) RenameSticker(src, dst string) (retRes RenameStickerResult) {
	return RenameStickerNotYetImplErr
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
