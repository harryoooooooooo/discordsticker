package sticker

import (
	"path/filepath"
)

type Sticker struct {
	name    string
	path    string
	uniqLen int
}

func (s *Sticker) StringWithHint() string {
	return withHint(s.name, s.uniqLen)
}

func (s *Sticker) Path() string {
	return s.path
}

func (s *Sticker) Ext() string {
	return filepath.Ext(s.path)
}
