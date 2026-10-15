package ansi

import (
	"bytes"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/charmbracelet/x/ansi/parser"
)

// nbsp is a non-breaking space.
const nbsp = 0xA0

// Hardwrap wraps a string or a block of text to a given line length, breaking
// word boundaries. This will preserve ANSI escape codes and will account for
// wide-characters in the string.
// When preserveSpace is true, spaces at the beginning of a line will be
// preserved.
// This treats the text as a sequence of graphemes.
func Hardwrap(s string, limit int, preserveSpace bool) string {
	return hardwrap(GraphemeWidth, s, limit, preserveSpace)
}

// HardwrapWc wraps a string or a block of text to a given line length, breaking
// word boundaries. This will preserve ANSI escape codes and will account for
// wide-characters in the string.
// When preserveSpace is true, spaces at the beginning of a line will be
// preserved.
// This treats the text as a sequence of wide characters and runes.
func HardwrapWc(s string, limit int, preserveSpace bool) string {
	return hardwrap(WcWidth, s, limit, preserveSpace)
}

func hardwrap(m Method, s string, limit int, preserveSpace bool) string {
	if limit < 1 {
		return s
	}

	var (
		cluster      []byte
		buf          bytes.Buffer
		curWidth     int
		forceNewline bool
		pstate       = parser.GroundState // initial state
		b            = []byte(s)
	)

	addNewline := func() {
		buf.WriteByte('\n')
		curWidth = 0
	}

	i := 0
	for i < len(b) {
		state, action := parser.Table.Transition(pstate, b[i])
		if state == parser.Utf8State {
			var width int
			cluster, width = FirstGraphemeCluster(b[i:], m)
			i += len(cluster)

			if curWidth+width > limit {
				addNewline()
			}
			if !preserveSpace && curWidth == 0 && len(cluster) <= 4 {
				// Skip spaces at the beginning of a line
				if r, _ := utf8.DecodeRune(cluster); r != utf8.RuneError && unicode.IsSpace(r) {
					pstate = parser.GroundState
					continue
				}
			}

			buf.Write(cluster)
			curWidth += width
			pstate = parser.GroundState
			continue
		}

		switch action {
		case parser.PrintAction, parser.ExecuteAction:
			if b[i] == '\n' {
				addNewline()
				forceNewline = false
				break
			}

			if curWidth+1 > limit {
				addNewline()
				forceNewline = true
			}

			// Skip spaces at the beginning of a line
			if curWidth == 0 {
				if !preserveSpace && forceNewline && unicode.IsSpace(rune(b[i])) {
					break
				}
				forceNewline = false
			}

			buf.WriteByte(b[i])
			if action == parser.PrintAction {
				curWidth++
			}
		default:
			buf.WriteByte(b[i])
		}

		// We manage the UTF8 state separately manually above.
		if pstate != parser.Utf8State {
			pstate = state
		}
		i++
	}

	return buf.String()
}

// Wordwrap wraps a string or a block of text to a given line length, not
// breaking word boundaries. This will preserve ANSI escape codes and will
// account for wide-characters in the string.
// The breakpoints string is a list of characters that are considered
// breakpoints for word wrapping. A hyphen (-) is always considered a
// breakpoint.
//
// Note: breakpoints must be a string of 1-cell wide rune characters.
//
// This treats the text as a sequence of graphemes.
func Wordwrap(s string, limit int, breakpoints string) string {
	return wordwrap(GraphemeWidth, s, limit, breakpoints)
}

// WordwrapWc wraps a string or a block of text to a given line length, not
// breaking word boundaries. This will preserve ANSI escape codes and will
// account for wide-characters in the string.
// The breakpoints string is a list of characters that are considered
// breakpoints for word wrapping. A hyphen (-) is always considered a
// breakpoint.
//
// Note: breakpoints must be a string of 1-cell wide rune characters.
//
// This treats the text as a sequence of wide characters and runes.
func WordwrapWc(s string, limit int, breakpoints string) string {
	return wordwrap(WcWidth, s, limit, breakpoints)
}

func wordwrap(m Method, s string, limit int, breakpoints string) string {
	if limit < 1 {
		return s
	}

	var (
		cluster  []byte
		buf      bytes.Buffer
		word     bytes.Buffer
		space    bytes.Buffer
		curWidth int
		wordLen  int
		pstate   = parser.GroundState // initial state
		b        = []byte(s)
	)

	addSpace := func() {
		curWidth += space.Len()
		buf.Write(space.Bytes())
		space.Reset()
	}

	addWord := func() {
		if word.Len() == 0 {
			return
		}

		addSpace()
		curWidth += wordLen
		buf.Write(word.Bytes())
		word.Reset()
		wordLen = 0
	}

	addNewline := func() {
		buf.WriteByte('\n')
		curWidth = 0
		space.Reset()
	}

	i := 0
	for i < len(b) {
		state, action := parser.Table.Transition(pstate, b[i])
		if state == parser.Utf8State { //nolint:nestif
			var width int
			cluster, width = FirstGraphemeCluster(b[i:], m)
			i += len(cluster)

			r, _ := utf8.DecodeRune(cluster)
			if r != utf8.RuneError && unicode.IsSpace(r) && r != nbsp {
				addWord()
				space.WriteRune(r)
			} else if bytes.ContainsAny(cluster, breakpoints) {
				addSpace()
				addWord()
				buf.Write(cluster)
				curWidth++
			} else {
				word.Write(cluster)
				wordLen += width
				if curWidth+space.Len()+wordLen > limit &&
					wordLen < limit {
					addNewline()
				}
			}

			pstate = parser.GroundState
			continue
		}

		switch action {
		case parser.PrintAction, parser.ExecuteAction:
			r := rune(b[i])
			switch {
			case r == '\n':
				if wordLen == 0 {
					if curWidth+space.Len() > limit {
						curWidth = 0
					} else {
						buf.Write(space.Bytes())
					}
					space.Reset()
				}

				addWord()
				addNewline()
			case unicode.IsSpace(r):
				addWord()
				space.WriteByte(b[i])
			case r == '-':
				fallthrough
			case runeContainsAny(r, breakpoints):
				addSpace()
				addWord()
				buf.WriteByte(b[i])
				curWidth++
			default:
				word.WriteByte(b[i])
				wordLen++
				if curWidth+space.Len()+wordLen > limit &&
					wordLen < limit {
					addNewline()
				}
			}

		default:
			word.WriteByte(b[i])
		}

		// We manage the UTF8 state separately manually above.
		if pstate != parser.Utf8State {
			pstate = state
		}
		i++
	}

	addWord()

	return buf.String()
}

// Wrap wraps a string or a block of text to a given line length, breaking word
// boundaries if necessary. This will preserve ANSI escape codes and will
// account for wide-characters in the string. The breakpoints string is a list
// of characters that are considered breakpoints for word wrapping. A hyphen
// (-) is always considered a breakpoint.
//
// Note: breakpoints must be a string of 1-cell wide rune characters.
//
// This treats the text as a sequence of graphemes.
func Wrap(s string, limit int, breakpoints string) string {
	return wrap(GraphemeWidth, s, limit, breakpoints)
}

// WrapWc wraps a string or a block of text to a given line length, breaking word
// boundaries if necessary. This will preserve ANSI escape codes and will
// account for wide-characters in the string. The breakpoints string is a list
// of characters that are considered breakpoints for word wrapping. A hyphen
// (-) is always considered a breakpoint.
//
// Note: breakpoints must be a string of 1-cell wide rune characters.
//
// This treats the text as a sequence of wide characters and runes.
func WrapWc(s string, limit int, breakpoints string) string {
	return wrap(WcWidth, s, limit, breakpoints)
}

func wrap(m Method, s string, limit int, breakpoints string) string {
	if limit < 1 {
		return s
	}

	var (
		cluster             string
		buf                 bytes.Buffer
		word                bytes.Buffer
		space               bytes.Buffer
		cjk                 cjkLine
		cjkEnabled          = hasCJKWrap(s)
		wordFirst, wordLast rune                 // boundary clusters of the buffered segment
		wordHasNBSP         bool                 // a fitting connected segment cannot be hard-cut
		spaceWidth          int                  // width of the space buffer
		curWidth            int                  // written width of the line
		wordLen             int                  // word buffer len without ANSI escape codes
		pstate              = parser.GroundState // initial state
	)

	addSpace := func() {
		if spaceWidth == 0 && space.Len() == 0 {
			return
		}
		if cjkEnabled {
			// An ANSI-only word can be flushed by another space before a
			// visible cluster makes the fit decision. As with trailing
			// whitespace, discard spaces that cannot fit, not the ANSI bytes.
			if curWidth+spaceWidth > limit {
				space.Reset()
				spaceWidth = 0
				return
			}
			cjk.space(spaceWidth)
		}
		curWidth += spaceWidth
		buf.Write(space.Bytes())
		space.Reset()
		spaceWidth = 0
	}

	addWord := func() {
		if word.Len() == 0 {
			return
		}

		addSpace()
		if cjkEnabled {
			cjk.add(buf.Len(), wordLen, wordFirst, wordLast)
		}
		curWidth += wordLen
		buf.Write(word.Bytes())
		word.Reset()
		wordLen = 0
		wordFirst, wordLast = 0, 0
		wordHasNBSP = false
	}

	addNewline := func() {
		buf.WriteByte('\n')
		cjk.reset()
		curWidth = 0
		space.Reset()
		spaceWidth = 0
	}

	i := 0
	wrapNewline := func(next rune, nextWidth int) {
		if !cjkEnabled {
			addNewline()
			return
		}
		if next == 0 {
			next = wordFirst
		}
		var wrapped bool
		if curWidth > 0 || spaceWidth > 0 {
			// An empty line has nothing to shift left and must not gain a
			// newline: the fallback below emits an oversized word, keeping
			// graphemes whole, instead of prepending a blank line.
			curWidth, wrapped = cjk.newline(&buf, curWidth, next)
			if wrapped {
				space.Reset()
				spaceWidth = 0
			}
		}

		if curWidth+spaceWidth+wordLen+nextWidth > limit {
			if wordLen > 0 && wordLen <= limit && nextWidth == 0 {
				wordHasNBSP = wordHasNBSP || hasNBSPBeforeBreak(s[i:], breakpoints)
			}
			if wordHasNBSP && wordLen <= limit && nextWidth == 0 {
				// Relax an oversized punctuation group before splitting an
				// NBSP-connected segment that can still fit a line by itself.
				addNewline()
				return
			}
			// Moving the whole punctuation group could not make it fit.
			// Only now relax the prohibitions and hardwrap the buffered word
			// into the remaining cells, keeping escape sequences intact.
			if curWidth+spaceWidth > limit {
				addNewline()
			} else {
				addSpace()
			}
			at, width := 0, 0
			wordFirst, wordLast = 0, 0
			for at < word.Len() {
				seq, w, n, _ := m.DecodeSequence(word.Bytes()[at:], NormalState, nil)
				state, action := parser.Table.Transition(parser.GroundState, word.Bytes()[at])
				if state == parser.Utf8State || action == parser.PrintAction {
					// DecodeSequence stops at ANSI, not at the end of an
					// ANSI-transparent grapheme. Complete the same unit used by
					// the CJK path before testing its fit or recording its width.
					tail, joinedWidth := cjkClusterTail(string(word.Bytes()[at+n:]), string(seq), m)
					n += tail
					w = joinedWidth
				}
				// Keep raw escape/control tokens in order; only complete
				// graphemes can wrap, and one may overflow a narrow line.
				if w > 0 && curWidth+width > 0 && curWidth+width+w > limit {
					buf.Write(word.Next(at))
					addNewline()
					at, width = 0, 0
					wordFirst, wordLast = 0, 0
				}
				if w > 0 {
					r, _ := utf8.DecodeRune(seq)
					if width == 0 {
						wordFirst = r
					}
					wordLast = r
				}
				at += n
				width += w
			}
			// Keep the final fragment buffered so later letters and a
			// fitting closing mark still move with the whole fragment.
			wordLen = width
			if curWidth > 0 && curWidth+wordLen+nextWidth > limit {
				addNewline()
			}
		}
	}

	for i < len(s) {
		state, action := parser.Table.Transition(pstate, s[i])
		if state == parser.Utf8State { //nolint:nestif
			var width int
			cluster, width = FirstGraphemeCluster(s[i:], m)
			i += len(cluster)

			r, _ := utf8.DecodeRuneInString(cluster)
			switch {
			case r != utf8.RuneError && unicode.IsSpace(r) && r != nbsp: // nbsp is a non-breaking space
				addWord()
				space.WriteRune(r)
				spaceWidth += width
			case isCJK(r) || isCJKOpening(r) || isCJKClosing(r):
				tail, joinedWidth := cjkClusterTail(s[i:], cluster, m)
				cluster = s[i-len(cluster) : i+tail]
				i += tail
				width = joinedWidth
				scanState := parser.GroundState
				nextCluster, _ := nextWrapCluster(s[i:], &scanState)
				next, _ := utf8.DecodeRuneInString(nextCluster)
				if wordLast == nbsp || next == nbsp {
					// Keep an NBSP-connected segment in the ordinary word
					// buffer. It moves whole while it fits an empty line;
					// only an oversized segment uses the hard-cut fallback.
					if (wordLast != nbsp && wordLen > 0) || wordLen+width > limit {
						addWord()
					}
					if wordLen == 0 {
						wordFirst = r
					}
					wordLast = r
					word.WriteString(cluster)
					wordLen += width
					if curWidth+spaceWidth+wordLen > limit {
						wrapNewline(0, 0)
					}
					if wordLen == limit {
						addWord()
					}
					break
				}
				if wordLen > 0 || spaceWidth == 0 {
					addWord()
				}
				if (curWidth > 0 || spaceWidth > 0) && curWidth+spaceWidth+width > limit {
					wrapNewline(r, width)
				}
				// ANSI-only words must not commit pending whitespace until
				// the CJK fit decision has discarded it or kept it in place.
				addWord()
				addSpace()
				cjk.add(buf.Len(), width, r, r)
				buf.WriteString(cluster)
				curWidth += width
			case strings.ContainsAny(cluster, breakpoints):
				if !cjkEnabled {
					addSpace()
				}
				if curWidth+spaceWidth+wordLen+width > limit {
					if wordLen == 0 {
						wordFirst = r
					}
					wordLast = r
					word.WriteString(cluster)
					wordLen += width
					if cjkEnabled {
						wrapNewline(0, 0)
					}
				} else {
					addSpace()
					addWord()
					if cjkEnabled {
						cjk.add(buf.Len(), width, r, r)
					}
					buf.WriteString(cluster)
					curWidth += width
				}
			default:
				if wordLen+width > limit {
					// Hardwrap the word if it's too long
					addWord()
				}

				if wordLen == 0 {
					wordFirst = r
				}
				wordLast = r
				wordHasNBSP = wordHasNBSP || r == nbsp
				word.WriteString(cluster)
				wordLen += width

				if curWidth+wordLen+spaceWidth > limit {
					wrapNewline(0, 0)
				}

				if wordLen == limit {
					// Hardwrap the word if it's too long
					addWord()
				}
			}

			pstate = parser.GroundState
			continue
		}

		switch action {
		case parser.PrintAction, parser.ExecuteAction:
			switch r := rune(s[i]); {
			case r == '\n':
				if wordLen == 0 {
					if curWidth+spaceWidth > limit {
						curWidth = 0
					} else {
						// preserve whitespaces
						buf.Write(space.Bytes())
					}
					space.Reset()
					spaceWidth = 0
				}

				addWord()
				addNewline()
			case unicode.IsSpace(r):
				addWord()
				space.WriteRune(r)
				spaceWidth++
			case r == '-':
				fallthrough
			case runeContainsAny(r, breakpoints):
				if !cjkEnabled {
					addSpace()
				}
				if curWidth+spaceWidth+wordLen >= limit {
					// We can't fit the breakpoint in the current line, treat
					// it as part of the word.
					if wordLen == 0 {
						wordFirst = r
					}
					wordLast = r
					word.WriteRune(r)
					wordLen++
					if cjkEnabled {
						wrapNewline(0, 0)
					}
				} else {
					addSpace()
					addWord()
					if cjkEnabled {
						cjk.add(buf.Len(), 1, r, r)
					}
					buf.WriteRune(r)
					curWidth++
				}
			default:
				if curWidth == limit {
					wrapNewline(0, 0)
				}

				if wordLen == 0 {
					wordFirst = r
				}
				wordLast = r
				word.WriteRune(r)
				wordLen++

				// CJK groups resolve overflow before committing a full word.
				// Without CJK, retain upstream's commit-before-check ordering,
				// including its existing overwide breakpoint/word behavior.
				if cjkEnabled && curWidth+wordLen+spaceWidth > limit {
					wrapNewline(0, 0)
				}

				if wordLen == limit {
					// Hardwrap the word if it's too long
					addWord()
				}
				if !cjkEnabled && curWidth+wordLen+spaceWidth > limit {
					addNewline()
				}
			}

		default:
			word.WriteByte(s[i])
		}

		// We manage the UTF8 state separately manually above.
		if pstate != parser.Utf8State {
			pstate = state
		}
		i++
	}

	if wordLen == 0 {
		if curWidth+spaceWidth > limit {
			curWidth = 0
		} else {
			// preserve whitespaces
			buf.Write(space.Bytes())
		}
		space.Reset()
		spaceWidth = 0
	}

	addWord()

	return buf.String()
}

func runeContainsAny(r rune, s string) bool {
	for _, c := range s {
		if c == r {
			return true
		}
	}
	return false
}
