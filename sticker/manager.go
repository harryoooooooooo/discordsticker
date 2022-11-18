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
	"strconv"
	"strings"
	"sync"
)

// Manager holds the cached sticker info.
// Note that it's caller's responsibility to lock the resource.
type Manager struct {
	root     string
	stickers []*Sticker

	mu sync.RWMutex
}

func (m *Manager) Lock()    { m.mu.Lock() }
func (m *Manager) Unlock()  { m.mu.Unlock() }
func (m *Manager) RLock()   { m.mu.RLock() }
func (m *Manager) RUnlock() { m.mu.RUnlock() }

func (m *Manager) Stickers() []*Sticker {
	return m.stickers
}

func NewManager(root string) (*Manager, error) {
	m := &Manager{root: filepath.Clean(root)}
	if err := m.update(); err != nil {
		return nil, err
	}
	return m, nil
}

// update reads the structure of the stickers in the file system.
// Under the root directory, this function walks recursively into every directory,
// and treats all found png, jpeg, and gif files as stickers.
// The filepath separator in the sticker names will be replaced by '-'.
// update may returns countUniqLenError if there are conflicted sticker names.
func (m *Manager) update() error {
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

	names := make([]string, len(paths))
	for i, p := range paths {
		ext := filepath.Ext(p)
		name := p[len(m.root) : len(p)-len(ext)]
		if name[0] == filepath.Separator {
			name = name[1:]
		}
		names[i] = strings.ReplaceAll(filepath.ToSlash(name), "/", "-")
	}

	uniqLen, err := countUniqLen(names)
	if err != nil {
		return err
	}

	stickers := make([]*Sticker, len(paths))
	for i := range stickers {
		stickers[i] = &Sticker{
			name:    names[i],
			path:    paths[i],
			uniqLen: uniqLen[i],
		}
	}

	m.stickers = stickers

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
func (m *Manager) AddSticker(path, url string) (retErr error) {
	if strings.Contains(filepath.ToSlash(path), "/") {
		return errors.New(fmt.Sprintf("Invalid sticker path, filepath separator (%c) or slash is included", filepath.Separator))
	}

	if ss := m.MatchedStickers(path); len(ss) != 0 {
		matchedStr := StickerListString(ss)
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

	ext := ctype[len("image/"):]
	imgPath := filepath.Join(m.root, path+"."+ext)
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

// RenameSticker renames the sticker.
// UninformableErr is returned when there is an internal error occurs;
// Otherwise there is probably an error caused by user and the error object may cantain advice if any.
func (m *Manager) RenameSticker(src, dst string) (retErr error) {
	srcMatched := m.MatchedStickers(src)
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

	dstMatched := m.MatchedStickers(dst)
	if len(dstMatched) > 1 || (len(dstMatched) == 1 && dstMatched[0] != srcMatched[0]) {
		return errors.New("The new path already matched existing stickers: " + StickerListString(dstMatched))
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

	if err := m.update(); err != nil {
		if _, ok := err.(*countUniqLenError); ok {
			return fmt.Errorf("The new sticker path cover the existing sticker path: %w", err)
		}
		log.Println("Failed to update sticker info:", err)
		return UninformableErr
	}

	return nil
}

// MatchedStickers returns the matched stickers.
func (m *Manager) MatchedStickers(name string) []*Sticker {
	var ret []*Sticker
	for _, s := range m.stickers {
		if strings.HasPrefix(s.name, name) {
			ret = append(ret, s)
		}
	}
	return ret
}
