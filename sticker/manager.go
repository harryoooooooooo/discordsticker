package sticker

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
)

// Manager holds the cached sticker info.
// Note that it's caller's responsibility to lock the resource.
type Manager struct {
	root          string
	stickers      []*Sticker
	caseSensitive bool

	mu sync.RWMutex
}

func (m *Manager) Lock()    { m.mu.Lock() }
func (m *Manager) Unlock()  { m.mu.Unlock() }
func (m *Manager) RLock()   { m.mu.RLock() }
func (m *Manager) RUnlock() { m.mu.RUnlock() }

func (m *Manager) Stickers() []*Sticker {
	return m.stickers
}

type ManagerOption func(m *Manager)

func CaseSensitive(v bool) ManagerOption {
	return func(m *Manager) {
		m.caseSensitive = v
	}
}

func NewManager(root string, opts ...ManagerOption) (*Manager, error) {
	m := &Manager{root: filepath.Clean(root)}
	for _, o := range opts {
		o(m)
	}
	if err := m.loadStickers(); err != nil {
		return nil, err
	}
	return m, nil
}

type stickerSliceSorter []*Sticker

func (s stickerSliceSorter) Len() int           { return len(s) }
func (s stickerSliceSorter) Swap(i, j int)      { s[i], s[j] = s[j], s[i] }
func (s stickerSliceSorter) Less(i, j int) bool { return s[i].Name() < s[j].Name() }

func (m *Manager) sortStickers() {
	sort.Sort(stickerSliceSorter(m.stickers))
}

// insertSticker inserts the stickers while assuming m.stickers is sorted.
func (m *Manager) insertSticker(s *Sticker) {
	i, found := sort.Find(len(m.stickers), func(i int) int {
		return strings.Compare(s.Name(), m.stickers[i].Name())
	})
	if found {
		log.Printf("Tried to insert an already existing sticker %q, skipped", s.Name())
		return
	}
	m.stickers = append(m.stickers, nil)
	copy(m.stickers[i+1:], m.stickers[i:])
	m.stickers[i] = s
}

// loadStickers reads the structure of the stickers in the file system.
// Under the root directory, this function walks recursively into every directory,
// and treats all found png, jpeg, and gif files as stickers.
// The filepath separator in the sticker names will be replaced by '-'.
func (m *Manager) loadStickers() error {
	var paths []string

	if err := filepath.WalkDir(m.root, func(path string, d fs.DirEntry, err error) error {
		if path == m.root {
			return nil
		}
		if err != nil {
			log.Printf("WalkDir failed, path=%s err=%v\n", path, err)
			return fs.SkipDir
		}
		if d.IsDir() {
			log.Printf("Directory found, support of directories could be deprecated in the future, path=%s\n", path)
		} else {
			if filepath.Ext(path) == "" {
				log.Printf("Found a file without extension, skipped, path=%s\n", path)
			} else {
				paths = append(paths, path)
			}
		}
		return nil
	}); err != nil {
		return err
	}

	stickers := make([]*Sticker, len(paths))
	for i, p := range paths {
		ext := filepath.Ext(p)
		name := p[len(m.root) : len(p)-len(ext)]
		if name[0] == filepath.Separator {
			name = name[1:]
		}
		name = strings.ReplaceAll(filepath.ToSlash(name), "/", "-")
		if !m.caseSensitive {
			name = strings.ToLower(name)
		}

		stickers[i] = &Sticker{
			name: name,
			path: p,
		}
	}

	for i, s1 := range stickers {
		for _, s2 := range stickers[i+1:] {
			if strings.Contains(s1.Name(), s2.Name()) {
				log.Printf("Found sticker %q contains %q", s1.Path(), s2.Path())
			}
			if strings.Contains(s2.Name(), s1.Name()) {
				log.Printf("Found sticker %q contains %q", s2.Path(), s1.Path())
			}
		}
	}

	m.stickers = stickers
	m.sortStickers()

	return nil
}

// UninformableErr indicates an internal error.
// Functions should log the info before returning UninformableErr.
var UninformableErr = errors.New("Error uninformable to user")

const (
	AddStickerSizeLimit = 3500000
)

// AddSticker downloads the sticker to local and updates the sticker data.
// UninformableErr is returned when there is an internal error occurs;
// Otherwise there is probably an error caused by user and the error object may cantain advice if any.
func (m *Manager) AddSticker(name, url string) (retErr error) {
	if strings.Contains(filepath.ToSlash(name), "/") {
		return errors.New(fmt.Sprintf("Invalid sticker name, filepath separator (%c) or slash is included", filepath.Separator))
	}

	if ss := m.MatchedStickers([][]string{{name}}); len(ss) != 0 {
		matchedStr := StickerListString(ss)
		return errors.New("The name is contained by the following sticker(s): " + matchedStr)
	}
	if ss := m.containedStickers(name); len(ss) != 0 {
		matchedStr := StickerListString(ss)
		return errors.New("The name contains the following sticker(s): " + matchedStr)
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

	ext := ctype[len("image/"):]
	path := filepath.Join(m.root, name+"."+ext)
	w, err := os.Create(path)
	if err != nil {
		log.Println("Failed to create a new file:", err)
		return UninformableErr
	}
	defer func() {
		if retErr != nil {
			os.Remove(path)
		}
	}()
	defer w.Close()

	if _, err := io.Copy(w, resp.Body); err != nil {
		log.Println("Failed to write the image:", err)
		return UninformableErr
	}

	if !m.caseSensitive {
		name = strings.ToLower(name)
	}
	m.insertSticker(&Sticker{
		name: name,
		path: path,
	})

	return nil
}

// RenameSticker renames the sticker.
// UninformableErr is returned when there is an internal error occurs;
// Otherwise there is probably an error caused by user and the error object may cantain advice if any.
func (m *Manager) RenameSticker(src, dst string) (retErr error) {
	srcMatched := m.MatchedStickers([][]string{{src}})
	if len(srcMatched) < 1 {
		return errors.New("Sticker not found.")
	}
	if len(srcMatched) > 1 {
		matchedStr := StickerListString(srcMatched)
		return errors.New("Found more than one stickers. Matched: " + matchedStr)
	}

	if strings.Contains(filepath.ToSlash(dst), "/") {
		return errors.New(fmt.Sprintf("Invalid dst path, filepath separator (%c) or slash is included", filepath.Separator))
	}

	dstMatched := m.MatchedStickers([][]string{{dst}})
	if len(dstMatched) > 1 || (len(dstMatched) == 1 && dstMatched[0] != srcMatched[0]) {
		return errors.New("The new name is contained by existing sticker(s): " + StickerListString(dstMatched))
	}
	if ss := m.containedStickers(dst); len(ss) != 0 {
		for i, s := range ss {
			if s == srcMatched[0] {
				copy(ss[i:], ss[i+1:])
				ss = ss[:len(ss)-1]
				break
			}
		}
		if len(ss) != 0 {
			matchedStr := StickerListString(ss)
			return errors.New("The name contains the following sticker(s): " + matchedStr)
		}
	}

	srcPath := srcMatched[0].Path()
	dstPath := filepath.Join(m.root, dst+srcMatched[0].Ext())
	if err := os.Rename(srcPath, dstPath); err != nil {
		log.Println("Failed to move the image:", err)
		return UninformableErr
	}
	defer func() {
		if retErr != nil {
			if err := os.Rename(dstPath, srcPath); err != nil {
				log.Println("Failed to move the image back:", err)
			}
		}
	}()

	if !m.caseSensitive {
		dst = strings.ToLower(dst)
	}
	srcMatched[0].name = dst
	srcMatched[0].path = dstPath

	for i, s := range m.stickers {
		if s == srcMatched[0] {
			copy(m.stickers[i:], m.stickers[i+1:])
			m.stickers = m.stickers[:len(m.stickers)-1]
			break
		}
	}
	m.insertSticker(srcMatched[0])

	return nil
}

// MatchedStickers returns the matched stickers.
// patternGroups indicates some pattern groups; A pattern group contains some patterns.
// A sticker is considered matched if "any" of the pattern groups has
// "all" patterns that are substrings of the name of the sticker.
// Note that a pattern group is ignored if it is empty;
// However, if all pattern groups are empty or no pattern group is passed,
// then the function returns all stickers.
func (m *Manager) MatchedStickers(patternGroups [][]string) []*Sticker {
	// Filter out empty pattern groups.
	var pgs [][]string
	for _, pg := range patternGroups {
		if len(pg) != 0 {
			pgs = append(pgs, pg)
		}
	}
	if len(pgs) == 0 {
		return m.stickers
	}

	if !m.caseSensitive {
		for _, pg := range pgs {
			for i, p := range pg {
				pg[i] = strings.ToLower(p)
			}
		}
	}

	var ret []*Sticker
	for _, s := range m.stickers {
		for _, pg := range pgs {
			matched := true
			for _, p := range pg {
				if !strings.Contains(s.name, p) {
					matched = false
					break
				}
			}
			if matched {
				ret = append(ret, s)
				break
			}
		}
	}
	return ret
}

func (m *Manager) containedStickers(name string) []*Sticker {
	var ret []*Sticker
	if !m.caseSensitive {
		name = strings.ToLower(name)
	}
	for _, s := range m.stickers {
		if strings.Contains(name, s.Name()) {
			ret = append(ret, s)
		}
	}
	return ret
}
