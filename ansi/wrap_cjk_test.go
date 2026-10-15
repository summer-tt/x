package ansi

import (
	"math/rand"
	"strings"
	"testing"
	"unicode"
	"unicode/utf8"
)

func TestWrapCJKGreedy(t *testing.T) {
	for _, tc := range []struct {
		name   string
		input  string
		limit  int
		widths []int
	}{
		// Five two-cell clusters fill every ten-cell line.
		{"even", strings.Repeat("中文", 10), 10, []int{10, 10, 10, 10}},
		// A 29-cell line holds fourteen CJK clusters, leaving one unused cell.
		{"odd", strings.Repeat("甲", 31), 29, []int{28, 28, 6}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, method := range []Method{GraphemeWidth, WcWidth} {
				got := method.Wrap(tc.input, tc.limit, "")
				lines := strings.Split(got, "\n")
				if len(lines) != len(tc.widths) {
					t.Fatalf("%v: expected %d lines, got %q", method, len(tc.widths), got)
				}
				for i, line := range lines {
					if width := method.StringWidth(line); width != tc.widths[i] {
						t.Errorf("%v line %d: width %d, want %d", method, i, width, tc.widths[i])
					}
				}
				if strings.ReplaceAll(got, "\n", "") != tc.input {
					t.Errorf("%v: wrap changed the text: %q", method, got)
				}
			}
		})
	}
}

func TestWrapCJKPunctuation(t *testing.T) {
	for _, tc := range []struct {
		name, input, want string
		limit             int
	}{
		// Carry the third ideograph to the next line with the closing mark.
		{"comma", "甲乙丙，丁", "甲乙\n丙，丁", 6},
		{"full stop", "甲乙丙。丁", "甲乙\n丙。丁", 6},
		// Move the opening mark instead of leaving it at the end of line one.
		{"parenthesis", "甲乙（丙丁", "甲乙\n（丙丁", 6},
		{"bracket", "甲乙【丙丁", "甲乙\n【丙丁", 6},
		// A repair must not create a new forbidden boundary in a mark chain.
		{"closing chain", "甲乙，。丙", "甲\n乙，。\n丙", 6},
		{"opening chain", "甲乙（【丙丁", "甲乙\n（【丙丁", 8},
		// An opening mark also follows an ASCII word when that word wraps.
		{"opening before word", "甲乙（hi丁", "甲乙\n（hi丁", 6},
		// Written whitespace must not detach the opener from the next word.
		{"opening before spaced word", "甲（ a，乙", "甲\n（ a，\n乙", 6},
		// A closing chain after a space still carries a non-closing unit.
		{"closing after space", "甲乙 ，，丙", "甲\n乙 ，，\n丙", 8},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := Wrap(tc.input, tc.limit, "")
			if got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
			for _, line := range strings.Split(got, "\n") {
				first, _ := utf8.DecodeRuneInString(line)
				last, _ := utf8.DecodeLastRuneInString(line)
				if isCJKClosing(first) || isCJKOpening(last) {
					t.Errorf("prohibited line boundary in %q", line)
				}
				if width := StringWidth(line); width > tc.limit {
					t.Errorf("line %q has width %d > %d", line, width, tc.limit)
				}
			}
		})
	}

	// Exercise every agreed mark, including the one-cell marks outside the
	// CJK blocks. The shifted unit is always the complete third ideograph.
	for _, mark := range cjkClosing {
		t.Run("closing/"+string(mark), func(t *testing.T) {
			input := "甲乙丙" + string(mark) + "丁"
			want := "甲乙\n丙" + string(mark) + "丁"
			if got := Wrap(input, 6, ""); got != want {
				t.Errorf("got %q, want %q", got, want)
			}
		})
	}
	for _, mark := range cjkOpening {
		t.Run("opening/"+string(mark), func(t *testing.T) {
			input := "甲乙" + string(mark) + "丙丁"
			want := "甲乙\n" + string(mark) + "丙丁"
			if got := Wrap(input, 4+StringWidth(string(mark)), ""); got != want {
				t.Errorf("got %q, want %q", got, want)
			}
		})
	}
}

func TestWrapCJKMixed(t *testing.T) {
	for _, tc := range []struct {
		name, input, want string
		limit             int
		breakpoints       string
	}{
		// Chinese can break between clusters; each English word stays whole.
		{"words", "甲乙hello丙丁world戊己", "甲乙hello\n丙丁world\n戊己", 10, ""},
		{"spaces", "甲乙 hello 丙丁 world 戊己", "甲乙 hello\n丙丁 world\n戊己", 10, ""},
		// A hyphen/custom breakpoint still ends the ASCII segment, and spaces
		// at an automatically inserted break are discarded as upstream does.
		{"hyphen", "甲乙 ab-cd 丙丁", "甲乙 ab-\ncd 丙丁", 8, ""},
		{"custom breakpoint", "甲乙ab|cd丙丁", "甲乙ab|\ncd丙丁", 7, "|"},
		// NBSP stays within the English word and is never discarded.
		{"nbsp", "甲乙ab\u00a0cd丙", "甲乙\nab\u00a0cd丙", 8, ""},
		// Long ASCII words retain the existing hard-cut path after a CJK unit.
		{"long word", "甲abcdefghijkl乙", "甲\nabcde\nfghij\nkl乙", 5, ""},
		// Existing ideographic-space handling wins over the CJK block class.
		{"wide space", "甲\u3000乙丙丁", "甲\u3000乙\n丙丁", 6, ""},
		// Leading spaces fit with the first unit, or are discarded at the
		// same leading wrap that the upstream whitespace path would insert.
		{"leading space", " 甲乙", " 甲\n乙", 4, ""},
		{"leading space overflow", "   甲乙", "\n甲乙", 4, ""},
		// Explicit line breaks are not shifted to repair user-supplied edges.
		{"explicit newline", "甲（\n乙", "甲（\n乙", 6, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := Wrap(tc.input, tc.limit, tc.breakpoints); got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestWrapCJKANSI(t *testing.T) {
	const (
		red   = "\x1b[31m"
		green = "\x1b[32m"
		reset = "\x1b[0m"
		link  = "\x1b]8;;https://example.com/中文\x1b\\"
		end   = "\x1b]8;;\x1b\\"
	)
	for _, tc := range []struct {
		name, input, want string
	}{
		// SGR state remains active on line two, without inserting resets.
		{"style", red + "甲乙丙丁戊己" + reset, red + "甲乙丙\n丁戊己" + reset},
		// Moving a suffix must not split or reorder the color change before it.
		{"style and punctuation", red + "甲乙" + green + "丙，丁" + reset, red + "甲乙" + green + "\n丙，丁" + reset},
		// OSC8 payloads are opaque, even when their URLs contain CJK text.
		{"hyperlink", link + "甲乙丙，丁" + end, link + "甲乙\n丙，丁" + end},
		{"style and hyperlink", red + link + "甲乙丙，丁" + end + reset, red + link + "甲乙\n丙，丁" + end + reset},
		{"BEL hyperlink", "\x1b]8;;https://example.com\a甲乙丙，丁\x1b]8;;\a", "\x1b]8;;https://example.com\a甲乙\n丙，丁\x1b]8;;\a"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := Wrap(tc.input, 6, "")
			if got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
			if strings.ReplaceAll(got, "\n", "") != tc.input {
				t.Errorf("ANSI bytes or text changed: %q", got)
			}
			// Decode the wrapped stream to verify that every visible cluster
			// still has its expected color/hyperlink, including after a break.
			var color string
			linked := false
			for rest := got; len(rest) > 0; {
				seq, width, n, state := DecodeSequence(rest, NormalState, nil)
				if n == 0 || state != NormalState {
					t.Fatalf("broken ANSI sequence at %q", rest)
				}
				rest = rest[n:]
				switch seq {
				case red, green:
					color = seq
				case reset:
					color = ""
				case link, "\x1b]8;;https://example.com\a":
					linked = true
				case end, "\x1b]8;;\a":
					linked = false
				}
				if width > 0 {
					wantColor := ""
					if strings.HasPrefix(tc.input, red) {
						wantColor = red
						if strings.Contains(tc.input, green) && strings.ContainsAny(seq, "丙，丁") {
							wantColor = green
						}
					}
					if color != wantColor || linked != strings.Contains(tc.name, "hyperlink") {
						t.Errorf("cluster %q: color %q, linked %v", seq, color, linked)
					}
				}
			}
			if color != "" || linked {
				t.Error("style or hyperlink was not closed at the end")
			}
		})
	}
}

func TestWrapCJKClustersAndClasses(t *testing.T) {
	// Combining marks and supplementary variation selectors follow their
	// leading Han rune onto the same line; neither cluster may be split.
	input := "甲\u0301乙\U000e0100𠀀丙"
	want := "甲\u0301乙\U000e0100\n𠀀丙"
	if got := Wrap(input, 4, ""); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	// Fullwidth letters are independent CJK units, not an ASCII-like word.
	if got := Wrap("甲ＡＢＣ乙", 6, ""); got != "甲ＡＢ\nＣ乙" {
		t.Errorf("fullwidth forms: got %q", got)
	}
	// CJK break classification does not override actual display width:
	// halfwidth punctuation stays one cell; halfwidth kana are excluded below.
	if got := Wrap("甲\uff61\uff61乙", 4, ""); got != "甲\uff61\uff61\n乙" {
		t.Errorf("halfwidth punctuation: got %q", got)
	}
	for _, r := range []rune{'甲', '𠀀', '\u3000', '\u303f', 'Ａ', '\uffef'} {
		if !isCJK(r) {
			t.Errorf("%U should provide CJK breaks", r)
		}
	}
	for _, r := range []rune{'a', '\u3042', '\u30a2', '\uac00', '\uff76', '\uff9e', '\uffa1', nbsp} {
		if isCJK(r) {
			t.Errorf("%U should retain ordinary word rules", r)
		}
	}
	// Kana, Hangul and halfwidth kana stay in ordinary words. With a preceding
	// ASCII word they move as a whole instead of filling the first line.
	for _, text := range []string{"\u3042\u3044", "\u30a2\u30a4", "\uac00\ub098", "\uff76\uff85\uff76\uff85"} {
		want := "ab\n" + text
		if got := Wrap("ab "+text, 6, ""); got != want {
			t.Errorf("ordinary script %q: got %q, want %q", text, got, want)
		}
	}
}

func TestWrapCJKGroupOverflow(t *testing.T) {
	for _, tc := range []struct {
		name, input, want string
		limit             int
	}{
		{"reported overflow", "甲（abcde）乙", "甲\n（abc\nde）\n乙", 5},
		{"Chinese parentheses around path", "甲（/usr/local/share/doc/longpath）乙", "", 8},
		{"ASCII parentheses around path", "甲(/usr/local/share/doc/longpath)乙", "", 8},
		{"group exactly limit", "甲（abc）乙", "甲\n（abc）\n乙", 7},
		{"group one cell over limit", "甲（abc）乙", "甲\n（abc\n）乙", 6},
		{"fitting group keeps punctuation together", "甲乙（hi）丙", "甲乙\n（hi）丙", 8},
		{"opening chain", "（【甲乙", "（【\n甲乙", 4},
		{"closing chain", "甲乙，。丙", "甲\n乙，\n。丙", 4},
		{"word with grapheme cluster", "甲（a\u0301bcdef）乙", "", 5},
		{"word with NBSP", "甲（ab\u00a0cdef）乙", "", 5},
		{"hyphenated path", "甲（/usr/long-name/file）乙", "", 5},
		{"consecutive breakpoints", "甲（a--b）乙", "", 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, method := range []Method{GraphemeWidth, WcWidth} {
				got := method.Wrap(tc.input, tc.limit, "")
				if tc.want != "" && got != tc.want {
					t.Errorf("%v: got %q, want %q", method, got, tc.want)
				}
				if strings.ReplaceAll(got, "\n", "") != tc.input {
					t.Errorf("%v: text changed: %q -> %q", method, tc.input, got)
				}
				for _, line := range strings.Split(got, "\n") {
					if width := method.StringWidth(line); width > tc.limit {
						t.Errorf("%v: line %q has width %d > %d (output %q)", method, line, width, tc.limit, got)
					}
				}
			}
		})
	}
}

func TestWrapCJKGroupOverflowANSI(t *testing.T) {
	const (
		red   = "\x1b[31m"
		green = "\x1b[32m"
		reset = "\x1b[0m"
		link  = "\x1b]8;;https://example.com/中文\x1b\\"
		end   = "\x1b]8;;\x1b\\"
	)
	for _, tc := range []struct {
		name, input string
		linked      bool
	}{
		{"SGR", red + "甲（abcdefgh）乙" + reset, false},
		{"SGR and OSC8", red + link + "甲（ab" + green + "cdefgh）乙" + end + reset, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, method := range []Method{GraphemeWidth, WcWidth} {
				got := method.Wrap(tc.input, 5, "")
				if strings.ReplaceAll(got, "\n", "") != tc.input {
					t.Fatalf("%v: text or ANSI bytes changed: %q", method, got)
				}
				for _, line := range strings.Split(got, "\n") {
					if width := method.StringWidth(line); width > 5 {
						t.Errorf("%v: line %q has width %d > 5", method, line, width)
					}
				}
				var color string
				linked := false
				for rest := got; len(rest) > 0; {
					seq, width, n, state := method.DecodeSequenceInString(rest, NormalState, nil)
					if n == 0 || state != NormalState {
						t.Fatalf("%v: broken ANSI sequence at %q", method, rest)
					}
					rest = rest[n:]
					switch seq {
					case red, green:
						color = seq
					case reset:
						color = ""
					case link:
						linked = true
					case end:
						linked = false
					}
					if width > 0 {
						wantColor := red
						if tc.linked && strings.ContainsAny(seq, "cdefgh）乙") {
							wantColor = green
						}
						if color != wantColor || linked != tc.linked {
							t.Errorf("%v cluster %q: color %q, linked %v", method, seq, color, linked)
						}
					}
				}
				if color != "" || linked {
					t.Error("style or hyperlink was not closed at the end")
				}
			}
		})
	}
}

func TestWrapCJKFittingGroupProperties(t *testing.T) {
	// Each punctuation group fits every tested limit; none of these inputs
	// may use the overflow fallback to excuse a prohibited line boundary.
	rng := rand.New(rand.NewSource(122))
	groups := []string{"甲", "乙", "abc", "（a）", "【ab】", "丙，", "“ab”"}
	for i := 0; i < 2000; i++ {
		var b strings.Builder
		b.WriteString("甲")
		for n := rng.Intn(40); n > 0; n-- {
			b.WriteString(groups[rng.Intn(len(groups))])
			if rng.Intn(4) == 0 {
				b.WriteByte(' ')
			}
		}
		b.WriteString("乙")
		input, limit := b.String(), rng.Intn(7)+6
		for _, method := range []Method{GraphemeWidth, WcWidth} {
			got := method.Wrap(input, limit, "")
			if strings.Join(strings.Fields(got), "") != strings.Join(strings.Fields(input), "") {
				t.Fatalf("%v: text changed: %q -> %q", method, input, got)
			}
			for _, line := range strings.Split(got, "\n") {
				if width := method.StringWidth(line); width > limit {
					t.Fatalf("%v: line width %d > %d: %q -> %q", method, width, limit, input, got)
				}
				line = strings.TrimSpace(line)
				first, _ := utf8.DecodeRuneInString(line)
				last, _ := utf8.DecodeLastRuneInString(line)
				if isCJKClosing(first) || isCJKOpening(last) {
					t.Fatalf("%v: prohibited boundary at limit %d: %q -> %q", method, limit, input, got)
				}
			}
		}
	}
}

func TestWrapCJKDegenerate(t *testing.T) {
	for _, tc := range []struct {
		name, input, want string
		limit             int
	}{
		{"empty zero", "", "", 0},
		{"empty one", "", "", 1},
		{"empty normal", "", "", 8},
		{"zero", "甲乙，", "甲乙，", 0},
		{"negative", "甲乙，", "甲乙，", -1},
		// A wide cluster on an empty one-cell line is emitted once, not
		// preceded by a blank line or retried indefinitely.
		{"one", "甲乙", "甲\n乙", 1},
		{"one ASCII", "abcd", "a\nb\nc\nd", 1},
		// Oversized groups relax punctuation rules, even at the smallest
		// limits. A single wide cluster retains the upstream behavior above.
		{"one punctuation", "甲，乙", "甲\n，\n乙", 1},
		{"one ASCII in punctuation", "（a。乙", "（\na\n。\n乙", 1},
		{"one styled ASCII in punctuation", "（\x1b[31ma\x1b[0m。乙", "（\n\x1b[31ma\x1b[0m\n。\n乙", 1},
		{"leading closing marks", "，，甲乙", "，\n，\n甲\n乙", 2},
		{"small punctuation group", "（甲）乙", "（\n甲\n）\n乙", 2},
		// A limit-sized word cannot extend the line past an opening mark.
		{"full word in punctuation group", "甲（abcde）乙", "甲\n（abc\nde）\n乙", 5},
		{"space in punctuation group", "甲（abcde ）乙", "甲\n（abc\nde ）\n乙", 5},
		{"long word in punctuation group", "甲（abcdefg）乙", "甲\n（abc\ndefg\n）乙", 5},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := Wrap(tc.input, tc.limit, ""); got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestWrapCJKBoundaryProperties(t *testing.T) {
	// Fixed-seed combinations exercise hard cuts inside oversized punctuation
	// groups. Width and text preservation take priority over prohibited edges.
	rng := rand.New(rand.NewSource(121))
	atoms := []string{"甲", "乙", "丙", "a", "word", " ", "-", "，", "。", "）", "（", "【", "】", "“", "”"}
	withoutSpace := func(s string) string {
		return strings.Map(func(r rune) rune {
			if unicode.IsSpace(r) {
				return -1
			}
			return r
		}, s)
	}
	for i := 0; i < 10000; i++ {
		var b strings.Builder
		b.WriteString("甲")
		for n := rng.Intn(40); n > 0; n-- {
			b.WriteString(atoms[rng.Intn(len(atoms))])
		}
		b.WriteString("乙")
		// Every individual cluster fits; a two-cell cluster cannot fit in
		// limit=1 without changing the text or splitting that cluster.
		input, limit := b.String(), rng.Intn(11)+2
		got := Wrap(input, limit, "")
		if withoutSpace(got) != withoutSpace(input) {
			t.Fatalf("text changed: %q -> %q", input, got)
		}
		for _, line := range strings.Split(got, "\n") {
			if width := StringWidth(line); width > limit {
				t.Fatalf("line width %d > %d: %q -> %q", width, limit, input, got)
			}
		}
	}
}

func TestWrapCJKPendingANSIWhitespace(t *testing.T) {
	for _, escape := range []struct{ name, open, close string }{
		{"SGR", "\x1b[31m", "\x1b[0m"},
		{"OSC8 ST", "\x1b]8;;https://example.com/中文\x1b\\", "\x1b]8;;\x1b\\"},
		{"OSC8 BEL", "\x1b]8;;https://example.com/中文\a", "\x1b]8;;\a"},
	} {
		for _, space := range []string{" ", "\u3000"} {
			t.Run(escape.name+"/"+space, func(t *testing.T) {
				for _, tc := range []struct{ input, want string }{
					{"甲" + space + escape.open + "乙丙" + escape.close, "甲\n" + escape.open + "乙\n丙" + escape.close},
					// Another space can flush the ANSI-only word before the
					// next cluster. Unplaceable whitespace must not widen a line.
					{"甲" + space + escape.open + space + "乙丙" + escape.close, "甲" + escape.open + "\n乙\n丙" + escape.close},
				} {
					for _, method := range []Method{GraphemeWidth, WcWidth} {
						got := method.Wrap(tc.input, 2, "")
						if got != tc.want {
							t.Errorf("%v: got %q, want %q", method, got, tc.want)
						}
						checkCJKStream(t, method, tc.input, got, 2)
					}
				}
			})
		}
	}
}

func TestWrapCJKNonTargetCompatibility(t *testing.T) {
	// Exact upstream v0.11.8 bytes, including its trailing spaces and its
	// pre-existing overwide breakpoint output. CJK width rules do not apply.
	for _, tc := range []struct {
		name, input, want string
		limit             int
		breakpoints       string
	}{
		{"hyphen trailing space", "foo bar-baz", "foo \nbar-baz", 7, ""},
		{"hyphen upstream overflow", "abcde f-", "abcde f-", 7, ""},
		{"kana", "あ い-う", "あ \nい-う", 5, ""},
		{"non-target unicode", "é é-é", "é \né-é", 3, ""},
		{"SGR", "foo \x1b[31mbar-baz\x1b[0m", "foo \n\x1b[31mbar-baz\x1b[0m", 7, ""},
		{"custom trailing space", "foo bar|baz", "foo \nbar|baz", 7, "|"},
		{"custom upstream overflow", "abcde f|", "abcde f|", 7, "|"},
		{"CJK only inside URL", "foo \x1b]8;;https://example.com/中文\x1b\\bar-baz\x1b]8;;\x1b\\", "foo \n\x1b]8;;https://example.com/中文\x1b\\bar-baz\x1b]8;;\x1b\\", 7, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, method := range []Method{GraphemeWidth, WcWidth} {
				if got := method.Wrap(tc.input, tc.limit, tc.breakpoints); got != tc.want {
					t.Errorf("%v: got %q, want %q", method, got, tc.want)
				}
			}
		})
	}
}

func TestWrapCJKActualMethodWidth(t *testing.T) {
	for _, tc := range []struct {
		input, want string
		limit       int
	}{
		{"｡｡｡", "｡｡｡", 3},
		{"｡｡｡", "｡\n｡\n｡", 1},
		{"甲｡｡乙", "甲｡｡\n乙", 4},
		{"甲\u0301｡｡乙", "甲\u0301｡｡\n乙", 4},
	} {
		for _, method := range []Method{GraphemeWidth, WcWidth} {
			got := method.Wrap(tc.input, tc.limit, "")
			if got != tc.want {
				t.Errorf("%v: input %q: got %q, want %q", method, tc.input, got, tc.want)
			}
			checkCJKStream(t, method, tc.input, got, tc.limit)
		}
	}
}

func TestWrapCJKNBSPConnections(t *testing.T) {
	for _, tc := range []struct {
		name, input, want string
		limit             int
	}{
		{"CJK on both sides", "a 甲\u00a0乙", "a\n甲\u00a0乙", 6},
		{"ordinary word after", "a 甲\u00a0abc", "a\n甲\u00a0abc", 7},
		{"ordinary word before", "a abc\u00a0甲", "a\nabc\u00a0甲", 7},
		{"multiple connections", "a 甲\u00a0ab\u00a0乙", "a\n甲\u00a0ab\u00a0乙", 9},
		{"connected closing mark", "甲乙，\u00a0丙", "甲\n乙，\u00a0丙", 8},
		{"connected opening mark", "甲乙\u00a0（丙", "甲\n乙\u00a0（丙", 8},
		{"connected closing chain", "甲乙，\u00a0。丙", "甲\n乙，\u00a0。\n丙", 8},
		// An oversized punctuation group may relax its opener, but not
		// hard-cut an NBSP segment that still fits an empty line.
		{"NBSP outranks opener", "甲（甲\u00a0乙", "甲\n（\n甲\u00a0乙", 6},
		{"mixed NBSP outranks opener", "（abc\u00a0甲", "（\nabc\u00a0甲", 6},
		{"NBSP after word prefix overflow", "甲（【abc\u00a0乙", "甲\n（【\nabc\u00a0乙", 6},
		{"later CJK boundary", "a 甲\u00a0word乙", "a\n甲\u00a0word\n乙", 8},
		{"SGR between connections", "a 甲\x1b[31m\u00a0乙\x1b[0m", "a\n甲\x1b[31m\u00a0乙\x1b[0m", 6},
		{"OSC8 between connections", "a 甲\u00a0\x1b]8;;https://example.com/中\x1b\\乙\x1b]8;;\x1b\\", "a\n甲\u00a0\x1b]8;;https://example.com/中\x1b\\乙\x1b]8;;\x1b\\", 6},
		{"overwide segment", "甲\u00a0乙\u00a0丙", "", 6},
		{"overwide mixed segment", "a 甲\u00a0abcdef\u00a0乙", "", 6},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, method := range []Method{GraphemeWidth, WcWidth} {
				got := method.Wrap(tc.input, tc.limit, "")
				if tc.want != "" && got != tc.want {
					t.Errorf("%v: got %q, want %q", method, got, tc.want)
				}
				checkCJKStream(t, method, tc.input, got, tc.limit)
			}
		})
	}
}

// Compare complete decoded clusters/escape sequences, excluding only ordinary
// wrap whitespace. This also checks SGR/OSC8 ordering and therefore state at
// each visible cluster; NBSP and grapheme contents must remain identical.
func checkCJKStream(t *testing.T, method Method, input, got string, limit int) {
	t.Helper()
	stream := func(s string) string {
		var b strings.Builder
		for len(s) > 0 {
			seq, _, n, state := method.DecodeSequenceInString(s, NormalState, nil)
			if n == 0 || state != NormalState {
				t.Fatalf("%v: broken ANSI sequence at %q", method, s)
			}
			r, _ := utf8.DecodeRuneInString(seq)
			if r == nbsp || !unicode.IsSpace(r) {
				b.WriteString(seq)
			}
			s = s[n:]
		}
		return b.String()
	}
	if stream(got) != stream(input) {
		t.Errorf("%v: cluster/ANSI stream changed: %q -> %q", method, input, got)
	}
	for _, line := range strings.Split(got, "\n") {
		if width := method.StringWidth(line); width > limit {
			t.Errorf("%v: width %d > %d in %q (output %q)", method, width, limit, line, got)
		}
	}
}

func TestWrapCJKVisibleTargets(t *testing.T) {
	for _, tc := range []struct {
		input string
		want  bool
	}{
		{"foo bar-baz", false},
		{"あいう ア 가나 ｶﾅ é a\u0301\u00a0", false},
		{" \u3000 ", false},
		{"\x1b]8;;https://example.com/中文\x1b\\foo-bar\x1b]8;;\x1b\\", false},
		{"\x1b]8;;https://example.com/甲\afoo-bar\x1b]8;;\a", false},
		{"\x1b[31m甲\u0301\x1b[0m", true},
		{"foo｡bar", true},
		{"a“b", true},
	} {
		if got := hasCJKWrap(tc.input); got != tc.want {
			t.Errorf("hasCJKWrap(%q) = %v, want %v", tc.input, got, tc.want)
		}
	}
}

func TestWrapCJKParserBoundaryCompatibility(t *testing.T) {
	for _, tc := range []struct{ name, input, want string }{
		{"ESC NUL OSC8", "a \x1b\x00]8;;甲\ab-", "a \x1b\x00]8;;甲\ab-"},
		{"ESC NUL OSC8 BEL close", "a \x1b\x00]8;;甲\ab-\x1b]8;;\a", "a \x1b\x00]8;;甲\ab-\x1b]8;;\a"},
		{"ESC NUL OSC8 ST close", "a \x1b\x00]8;;甲\x1b\\b-\x1b]8;;\x1b\\", "a \x1b\x00]8;;甲\x1b\\b-\x1b]8;;\x1b\\"},
		{"multiple execute actions", "a \x1b\x00\x01]8;;甲\ab-\x1b]8;;\a", "a\n\x1b\x00\x01]8;;甲\ab-\x1b]8;;\a"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if hasCJKWrap(tc.input) {
				t.Errorf("hidden OSC8 payload enabled CJK wrapping: %q", tc.input)
			}
			for _, method := range []Method{GraphemeWidth, WcWidth} {
				// These are the complete v0.11.8 bytes, including NUL and
				// upstream's control-character width/overflow behavior.
				if got := method.Wrap(tc.input, 4, ""); got != tc.want {
					t.Errorf("%v: got %q, want upstream bytes %q", method, got, tc.want)
				}
			}
		})
	}
}

func TestWrapCJKParserBoundaryNBSP(t *testing.T) {
	for _, tc := range []struct {
		input string
		want  bool
	}{
		{"ab\x1b\x00]8;;\u00a0\acd", false},
		{"ab\x1b\x00]8;;甲\acd\u00a0ef", true},
		{"ab\x1b\x00]8;;\u00a0\acd|ef", false},
		{"ab\x1b\x00]8;;甲\acd-ef\u00a0gh", false},
	} {
		if got := hasNBSPBeforeBreak(tc.input, "|"); got != tc.want {
			t.Errorf("hasNBSPBeforeBreak(%q) = %v, want %v", tc.input, got, tc.want)
		}
	}
}

func TestWrapCJKANSIClusterNBSP(t *testing.T) {
	for _, escape := range []struct{ name, open, close string }{
		{"SGR unclosed", "\x1b[31m", ""},
		{"SGR", "\x1b[31m", "\x1b[0m"},
		{"OSC8 ST", "\x1b]8;;https://example.com/甲\x1b\\", "\x1b]8;;\x1b\\"},
		{"OSC8 BEL", "\x1b]8;;https://example.com/甲\a", "\x1b]8;;\a"},
	} {
		for _, tail := range []string{"\u0301", "\ufe00", "\ufe0f", "\U000e0100", "\u0301\ufe00"} {
			t.Run(escape.name+"/"+tail, func(t *testing.T) {
				input := "a甲" + escape.open + tail + "\u00a0乙" + escape.close
				want := "a\n甲" + escape.open + tail + "\u00a0乙" + escape.close
				for _, method := range []Method{GraphemeWidth, WcWidth} {
					got := method.Wrap(input, 5, "")
					if got != want {
						t.Errorf("%v: got %q, want %q", method, got, want)
					}
					// Do not remove newlines: stream equality alone would miss
					// an illegal break between the base and its attached tail.
					if !strings.Contains(Strip(got), "甲"+tail+"\u00a0乙") {
						t.Errorf("%v: break inside fitting grapheme/NBSP segment: %q", method, got)
					}
					checkCJKStream(t, method, input, got, 5)
				}
			})
		}
	}
}

func TestWrapCJKANSIClusterBoundaries(t *testing.T) {
	for _, tc := range []struct {
		name, input, want, attached string
		limit                       int
	}{
		{"without NBSP", "ab甲\x1b[31m\u0301乙", "ab甲\x1b[31m\u0301\n乙", "甲\u0301", 4},
		{"punctuation left shift", "ab甲\x1b[31m\u0301，乙", "ab\n甲\x1b[31m\u0301，\n乙", "甲\u0301", 4},
		{"multiple ANSI tails", "a甲\x1b[31m\u0301\x1b[32m\ufe00\u00a0乙\x1b[0m", "a\n甲\x1b[31m\u0301\x1b[32m\ufe00\u00a0乙\x1b[0m", "甲\u0301\ufe00", 5},
		{"overwide NBSP segment", "a甲\x1b[31m\u0301\u00a0乙", "a甲\x1b[31m\u0301\u00a0\n乙", "甲\u0301", 4},
		{"halfwidth attached tail", "ab｡\x1b[31m\u0301\u00a0乙", "ab\n｡\x1b[31m\u0301\u00a0乙", "｡\u0301", 4},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, method := range []Method{GraphemeWidth, WcWidth} {
				got := method.Wrap(tc.input, tc.limit, "")
				if got != tc.want {
					t.Errorf("%v: got %q, want %q", method, got, tc.want)
				}
				if !strings.Contains(Strip(got), tc.attached) {
					t.Errorf("%v: newline detached grapheme tail: %q", method, got)
				}
				checkCJKStream(t, method, tc.input, got, tc.limit)
			}
		})
	}
}

func TestWrapCJKClusterTailControls(t *testing.T) {
	// These zero-width/control tokens are not ANSI-transparent grapheme tails.
	// In particular, ExecuteAction inside ESC must not be swallowed with OSC8.
	for _, rest := range []string{
		"\x00\u0301\u00a0乙", "\r\u0301\u00a0乙", "\n\u0301\u00a0乙",
		"\x1b\x00]8;;甲\a\u0301\u00a0乙", "\x1b[31m\u200b\u0301\u00a0乙",
	} {
		for _, method := range []Method{GraphemeWidth, WcWidth} {
			if n, width := cjkClusterTail(rest, "甲", method); n != 0 || width != 2 {
				t.Errorf("%v: tail of %q consumed %d bytes, width %d; want 0, 2", method, rest, n, width)
			}
		}
	}
}
func TestWrapCJKANSIClusterNarrowFallback(t *testing.T) {
	// The overflow fallback may let a single unbreakable grapheme overflow a
	// very narrow line, but it must not reopen the already-aggregated raw
	// cluster: no break between the base and its ANSI-separated combining
	// tail, and no spurious first empty line. Adjacent wider limits and the
	// fitting limit=5 case must stay unchanged.
	for _, escape := range []struct{ name, open, close string }{
		{"SGR", "\x1b[31m", ""},
		{"SGR reset", "\x1b[31m", "\x1b[0m"},
		{"OSC8 ST", "\x1b]8;;https://example.com/中文\x1b\\", "\x1b]8;;\x1b\\"},
		{"OSC8 BEL", "\x1b]8;;https://example.com/中文\a", "\x1b]8;;\a"},
	} {
		for _, tail := range []string{"\u0301", "\ufe00", "\U000e0100", "\u0301\ufe00"} {
			for _, tc := range []struct {
				name, input, want string
				limit             int
			}{
				{"minimal one", "甲" + escape.open + tail + "\u00a0", "甲" + escape.open + tail + "\n\u00a0", 1},
				{"minimal two", "甲" + escape.open + tail + "\u00a0", "甲" + escape.open + tail + "\n\u00a0", 2},
				{"extended one", "a甲" + escape.open + tail + "\u00a0乙" + escape.close, "a\n甲" + escape.open + tail + "\n\u00a0\n乙" + escape.close, 1},
				{"extended two", "a甲" + escape.open + tail + "\u00a0乙" + escape.close, "a\n甲" + escape.open + tail + "\n\u00a0\n乙" + escape.close, 2},
				{"extended five", "a甲" + escape.open + tail + "\u00a0乙" + escape.close, "a\n甲" + escape.open + tail + "\u00a0乙" + escape.close, 5},
			} {
				t.Run(escape.name+"/"+tail+"/"+tc.name, func(t *testing.T) {
					for _, method := range []Method{GraphemeWidth, WcWidth} {
						got := method.Wrap(tc.input, tc.limit, "")
						if got != tc.want {
							t.Errorf("%v: got %q, want %q", method, got, tc.want)
						}
						if strings.HasPrefix(got, "\n") {
							t.Errorf("%v: spurious first empty line: %q", method, got)
						}
						if strings.ReplaceAll(got, "\n", "") != tc.input {
							t.Errorf("%v: byte order changed: %q -> %q", method, tc.input, got)
						}
						// Keep the newlines: the visible base and its attached
						// tail must stay on one line; re-joining them after
						// deleting the newlines would hide an illegal break.
						if !strings.Contains(Strip(got), "甲"+tail) {
							t.Errorf("%v: break inside the grapheme cluster: %q", method, got)
						}
						for _, line := range strings.Split(got, "\n") {
							width := method.StringWidth(line)
							if width <= tc.limit {
								continue
							}
							// Only a single unbreakable grapheme may overflow.
							visible := Strip(line)
							cluster, clusterWidth := FirstGraphemeCluster(visible, method)
							if len(cluster) != len(visible) || clusterWidth != width {
								t.Errorf("%v: line %q width %d > %d is not a single grapheme", method, line, width, tc.limit)
							}
						}
					}
				})
			}
		}
	}
}

func TestWrapCJKSpacingMarkMethodWidth(t *testing.T) {
	for _, tc := range []struct {
		method Method
		width  int
	}{
		{GraphemeWidth, 2},
		{WcWidth, 3},
	} {
		const visible = "甲\u0903"
		cluster, width := FirstGraphemeCluster(visible, tc.method)
		if cluster != visible || width != tc.width || tc.method.StringWidth(visible) != tc.width {
			t.Errorf("%v: complete spacing-mark grapheme %q width %d, want %q width %d", tc.method, cluster, width, visible, tc.width)
		}
	}
}

func TestWrapCJKANSIClusterCompleteFallback(t *testing.T) {
	// Token width is not a grapheme-boundary oracle: Mc may have positive
	// width on its own, but the existing segmenter attaches it to the base.
	// Interleave ANSI between every continuation to exercise repeated joins.
	for _, escape := range []struct{ name, open, between, close string }{
		{"SGR unclosed", "\x1b[31m", "\x1b[32m", ""},
		{"SGR reset", "\x1b[31m", "\x1b[0m\x1b[32m", "\x1b[0m"},
		{"OSC8 ST", "\x1b]8;;https://example.com/甲\x1b\\", "\x1b]8;;\x1b\\\x1b]8;;https://example.com/乙\x1b\\", "\x1b]8;;\x1b\\"},
		{"OSC8 BEL", "\x1b]8;;https://example.com/甲\a", "\x1b]8;;\a\x1b]8;;https://example.com/乙\a", "\x1b]8;;\a"},
		{"multiple ANSI fragments", "\x1b[31m\x1b]8;;https://example.com/甲\x1b\\\x1b[1m", "\x1b[0m\x1b]8;;\x1b\\\x1b[32m\x1b]8;;https://example.com/乙\a", "\x1b]8;;\a\x1b[0m"},
	} {
		for _, tail := range []struct{ name, text string }{
			{"Mn", "\u0301"},
			{"Me", "\u20dd"},
			{"Mc", "\u0903"},
			{"variation selector", "\ufe00"},
			{"supplementary variation selector", "\U000e0100"},
			{"Mc Mn Me and selectors", "\u0903\u0301\u20dd\ufe00\U000e0100"},
		} {
			raw := "甲" + escape.open
			for i, r := range tail.text {
				if i > 0 {
					raw += escape.between
				}
				raw += string(r)
			}
			visible := "甲" + tail.text
			for _, method := range []struct {
				name string
				m    Method
			}{
				{"GraphemeWidth", GraphemeWidth},
				{"WcWidth", WcWidth},
			} {
				cluster, clusterWidth := FirstGraphemeCluster(visible, method.m)
				if cluster != visible {
					t.Fatalf("fixture is not one grapheme: %q", visible)
				}
				for _, limit := range []struct {
					name  string
					width int
				}{
					{"one", 1}, {"two", 2}, {"exact cluster", clusterWidth}, {"five", 5},
				} {
					// Narrow fixtures leave the complete base+tail on its own
					// line. At width 3, NBSP+乙 can share the following line.
					after := "\u00a0\n乙"
					if method.m.StringWidth("\u00a0乙") <= limit.width {
						after = "\u00a0乙"
					}
					// The five-cell connection moves whole only when it fits.
					// WcWidth's Mc makes it six cells instead.
					suffixFive := raw + "\u00a0乙" + escape.close
					bothFive := "a\n" + suffixFive
					if method.m.StringWidth(visible+"\u00a0乙") > 5 {
						suffixFive = raw + "\u00a0\n乙" + escape.close
						bothFive = "a" + suffixFive
					}
					for _, tc := range []struct{ name, input, narrow, five string }{
						{"minimal", raw + "\u00a0" + escape.close, raw + "\n\u00a0" + escape.close, raw + "\u00a0" + escape.close},
						{"prefix", "a" + raw + "\u00a0" + escape.close, "a\n" + raw + "\n\u00a0" + escape.close, "a" + raw + "\u00a0" + escape.close},
						{"suffix", raw + "\u00a0乙" + escape.close, raw + "\n" + after + escape.close, suffixFive},
						{"prefix and suffix", "a" + raw + "\u00a0乙" + escape.close, "a\n" + raw + "\n" + after + escape.close, bothFive},
					} {
						t.Run(escape.name+"/"+tail.name+"/"+method.name+"/"+limit.name+"/"+tc.name, func(t *testing.T) {
							want := tc.narrow
							if limit.width == 5 {
								want = tc.five
							}
							got := method.m.Wrap(tc.input, limit.width, "")
							if got != want {
								t.Errorf("got %q, want %q", got, want)
							}
							if strings.HasPrefix(got, "\n") || strings.ReplaceAll(got, "\n", "") != tc.input {
								t.Errorf("spurious first line or changed raw byte order: %q -> %q", tc.input, got)
							}
							// Strip ANSI only, never the inserted newlines: the
							// segmenter-confirmed base+tail must remain attached.
							if !strings.Contains(Strip(got), visible) {
								t.Errorf("newline detached a grapheme continuation: %q", got)
							}
							for _, line := range strings.Split(got, "\n") {
								// Measure the complete visible graphemes, not tokens
								// newly separated by ANSI; Method itself is unchanged.
								plain := Strip(line)
								width := method.m.StringWidth(plain)
								if width > limit.width {
									first, w := FirstGraphemeCluster(plain, method.m)
									if first != plain || w != width {
										t.Errorf("line %q width %d > %d is not one complete grapheme", line, width, limit.width)
									}
								}
								// Together with exact raw bytes above, this verifies
								// that no break split a sequence or changed ANSI state.
								for rest := line; len(rest) > 0; {
									_, _, n, state := method.m.DecodeSequenceInString(rest, NormalState, nil)
									if n == 0 || state != NormalState {
										t.Fatalf("broken ANSI sequence at %q", rest)
									}
									rest = rest[n:]
								}
							}
						})
					}
				}
			}
		}
	}
}
