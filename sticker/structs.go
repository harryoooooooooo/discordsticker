package sticker

import (
	"path/filepath"
)

type Sticker struct {
	name string
	path string
}

func (s *Sticker) Name() string {
	return s.name
}

func (s *Sticker) Path() string {
	return s.path
}

func (s *Sticker) Ext() string {
	return filepath.Ext(s.path)
}
