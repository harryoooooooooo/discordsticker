package sticker

import (
	"fmt"
	"strings"
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
