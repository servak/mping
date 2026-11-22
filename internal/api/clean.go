package api

import (
	"strings"
	"unicode"
)

// Clean makes s, which may come from elsewhere, safe to show to people and
// programs: it keeps printable characters only, at most limit of them, on one
// line. Control, format and other invisible characters, which could change how
// a terminal shows the rest or hide text from people, are dropped, and so are
// invalid UTF-8 and U+FFFD.
func Clean(s string, limit int) string {
	var b strings.Builder
	n, space := 0, false
	for _, r := range s {
		switch {
		case unicode.IsSpace(r):
			space = n > 0
			continue
		case r == unicode.ReplacementChar, !unicode.IsPrint(r),
			unicode.In(r, unicode.Variation_Selector, unicode.Other_Default_Ignorable_Code_Point):
			continue
		}
		if space {
			b.WriteByte(' ')
			n++
			space = false
		}
		if n >= limit {
			return strings.TrimRight(b.String(), " ") + "…"
		}
		b.WriteRune(r)
		n++
	}
	return b.String()
}
