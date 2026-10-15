package ansi

import (
	"bytes"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/charmbracelet/x/ansi/parser"
)

// isCJK reports whether the first rune of a cluster provides CJK wrap
// opportunities. Kana and Hangul, including their halfwidth forms, retain the
// ordinary word-wrapping rules.
func isCJK(r rune) bool {
	if unicode.Is(unicode.Katakana, r) || unicode.Is(unicode.Hangul, r) ||
		r >= 0xFF9E && r <= 0xFF9F {
		return false
	}
	return unicode.Is(unicode.Han, r) ||
		r >= 0x3000 && r <= 0x303F ||
		r >= 0xFF00 && r <= 0xFFEF
}

// These are the common Simplified Chinese line-start/line-end prohibitions
// from the CJK line-breaking conventions described by W3C CLReq, section
// "Line Composition Rules for Punctuation Marks". This deliberately bounded
// subset is not a full implementation of Unicode UAX #14. The curly quotes,
// ellipsis, dash and middle dot also apply outside the CJK Unicode blocks.
const (
	cjkClosing = "，。、；：？！）］】》」』”’…—·～％"
	cjkOpening = "（［【《「『“‘"
)

func isCJKClosing(r rune) bool { return strings.ContainsRune(cjkClosing, r) }
func isCJKOpening(r rune) bool { return strings.ContainsRune(cjkOpening, r) }

// nextWrapCluster skips escape payloads, not visible whitespace or controls.
// Preserve parser state between tokens: ExecuteAction can return a control
// without leaving Escape/CSI state, so the following payload is still opaque.
// Width calculation is left to the caller's Method, not this classification scan.
func nextWrapCluster(s string, pstate *parser.State) (cluster string, n int) {
	for i := 0; i < len(s); i++ {
		state, action := parser.Table.Transition(*pstate, s[i])
		*pstate = state
		if state == parser.Utf8State {
			cluster, _ = FirstGraphemeCluster(s[i:], GraphemeWidth)
			*pstate = parser.GroundState
			return cluster, i + len(cluster)
		}
		if action == parser.PrintAction || action == parser.ExecuteAction {
			return s[i : i+1], i + 1
		}
	}
	return "", len(s)
}

// cjkClusterTail includes only ANSI-separated continuations of this visible
// grapheme, not trailing escapes or arbitrary zero-width controls. This keeps
// its bytes together before the NBSP/line-break decision. Use the existing
// grapheme segmenter and Method width on the joined visible cluster.
func cjkClusterTail(s, cluster string, m Method) (n, width int) {
	_, width = FirstGraphemeCluster(cluster, m)
	pstate := parser.GroundState
	for at := 0; at < len(s); {
		tail, size := nextWrapCluster(s[at:], &pstate)
		if len(tail) == 0 {
			break
		}
		joined := cluster + tail
		first, w := FirstGraphemeCluster(joined, m)
		if len(first) != len(joined) {
			break
		}
		cluster, width = joined, w
		at += size
		n = at
	}
	return n, width
}

func hasCJKWrap(s string) bool {
	pstate := parser.GroundState
	for len(s) > 0 {
		cluster, n := nextWrapCluster(s, &pstate)
		r, _ := utf8.DecodeRuneInString(cluster)
		if !unicode.IsSpace(r) && (isCJK(r) || isCJKOpening(r) || isCJKClosing(r)) {
			return true
		}
		s = s[n:]
	}
	return false
}

// hasNBSPBeforeBreak checks the unbuffered rest of an ordinary word before
// the punctuation fallback cuts it. An NBSP later in that word still forbids
// cutting its prefix while the connected segment fits an empty line.
func hasNBSPBeforeBreak(s, breakpoints string) bool {
	pstate := parser.GroundState
	for len(s) > 0 {
		cluster, n := nextWrapCluster(s, &pstate)
		r, _ := utf8.DecodeRuneInString(cluster)
		if r == nbsp {
			return true
		}
		if unicode.IsSpace(r) || r == '-' || strings.ContainsRune(breakpoints, r) ||
			isCJK(r) || isCJKOpening(r) || isCJKClosing(r) {
			return false
		}
		s = s[n:]
	}
	return false
}

// cjkLine remembers legal unit boundaries in the written line. Ordinary words
// remain whole units; CJK clusters are individual units. Byte offsets always
// point outside escape sequences, so moving a suffix preserves CSI/OSC bytes
// and the terminal's active style/hyperlink across the inserted newline.
type cjkLine struct {
	units []cjkUnit
}

type cjkUnit struct {
	start       int
	width       int
	first, last rune // boundary clusters, including NBSP-connected segments
}

func (l *cjkLine) reset() { l.units = l.units[:0] }

func (l *cjkLine) space(width int) {
	if n := len(l.units); n > 0 {
		// Include written whitespace when moving its preceding unit. Keep
		// earlier boundaries available: a closing-mark chain may require
		// walking left past whitespace to find a non-closing unit.
		l.units[n-1].width += width
	}
}

func (l *cjkLine) add(start, width int, first, last rune) {
	if width > 0 {
		l.units = append(l.units, cjkUnit{start: start, width: width, first: first, last: last})
	}
}

// newline shifts the break left past a closing mark's preceding unit, or past
// trailing opening marks. It walks farther left for consecutive punctuation,
// rather than creating a new prohibited boundary while repairing the old one.
func (l *cjkLine) newline(buf *bytes.Buffer, curWidth int, next rune) (int, bool) {
	at := len(l.units)
	if isCJKClosing(next) && at > 0 {
		at--
	}
	for at > 0 && (at < len(l.units) && isCJKClosing(l.units[at].first) ||
		isCJKOpening(l.units[at-1].last)) {
		at--
	}

	if at < len(l.units) {
		width := 0
		for _, unit := range l.units[at:] {
			width += unit.width
		}
		if width == curWidth {
			// The group is already at the start of the line. Let the caller
			// hardwrap it if the pending content still cannot fit, rather
			// than emitting an empty line or allowing an overwide group.
			// An input-supplied closing mark may break without moving left.
			if !isCJKClosing(l.units[0].first) {
				return curWidth, false
			}
		} else {
			start := l.units[at].start
			tail := append([]byte(nil), buf.Bytes()[start:]...)
			buf.Truncate(start)
			buf.WriteByte('\n')
			buf.Write(tail)
			copy(l.units, l.units[at:])
			l.units = l.units[:len(l.units)-at]
			for i := range l.units {
				l.units[i].start++
			}
			return width, true
		}
	}

	buf.WriteByte('\n')
	l.reset()
	return 0, true
}
