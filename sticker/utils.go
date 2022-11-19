package sticker

import (
	"strings"
)

// StickerListString composes the report of a list of stickers.
// The names are quoted with "`", and separated by ", ".
// Example: "`pabc`, `pdef`, `qabc`"
func StickerListString(stickers []*Sticker) string {
	moreThanTen := false
	if len(stickers) > 10 {
		stickers = stickers[:10]
		moreThanTen = true
	}
	var names []string
	for _, s := range stickers {
		names = append(names, s.Name())
	}
	sb := strings.Builder{}
	sb.WriteString("`")
	sb.WriteString(strings.Join(names, "`, `"))
	sb.WriteString("`")
	if moreThanTen {
		sb.WriteString("... and more")
	}
	return sb.String()
}
