package sticker

import (
	"path/filepath"
)

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
