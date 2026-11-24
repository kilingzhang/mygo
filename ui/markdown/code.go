package markdown

import (
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/egoist/mygo/internal/l10n"
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
)

// CodeBlockView is a code block as Options.CodeBlock and Options.Code build
// it.
type CodeBlockView struct {
	// Language is the language its fence names, as "go", and Info the
	// whole info string after the fence.
	Language, Info string
	// Open tells a block still being written, whose closing fence has not
	// come: heavy renderers, as of diagrams, wait for it to close.
	Open bool
	// Index is the block's in the Document.
	Index int

	st *viewState
}

// Code returns the block's code, its lines ending with newlines.
func (b *CodeBlockView) Code() string { return b.st.doc.Lines(b.Index) }

// SourceLine returns the line of the view's source the block starts on,
// from 1: its opening fence, or indented code's first line. Line k of a
// fenced block's Code, from 1, is line SourceLine()+k of the source, and
// of indented code line SourceLine()+k-1: an app that builds the block as
// a view of its own, as of the Markdown in it, maps that view's lines
// (Options.OnTask's) back to the source so.
func (b *CodeBlockView) SourceLine() int {
	return lineAt(b.st.doc.Source(), b.st.doc.Block(b.Index).Start)
}

// codeState is what a view keeps of a code block: the tokens of its lines
// and the spans of its chunks of lines, to highlight only what changed.
type codeState struct {
	lang          string
	tok           editor.Tokenizer
	lines         []string
	ends          []uint64
	toks          [][]editor.Token
	chunks        []codeChunk
	copiedAt      time.Time
	seen          bool
	colorLiterals bool
	dark          bool
	surface       Color
}

// codeChunk is chunkLines lines of a code block, laid out as one text, so
// that a block growing as it streams lays out only its last chunk anew.
type codeChunk struct {
	spans   []ui.Span
	objects []mathObject
	n       int // lines
	ok      bool
}

const chunkLines = 32

func (st *viewState) codeState(i int) *codeState {
	b := st.doc.Block(i)
	if st.codes == nil {
		st.codes = map[int]*codeState{}
	}
	cs := st.codes[b.Start]
	lang := b.Language()
	if cs == nil || cs.lang != lang {
		cs = &codeState{lang: lang}
		if l := editor.LanguageNamed(lang); l != nil && lang != "" {
			cs.tok = l.Tokenizer
		}
		st.codes[b.Start] = cs
	}
	cs.seen = true
	return cs
}

// codeBlock builds code block i, through the hooks of the options.
func (st *viewState) codeBlock(c *ui.Context, i int) ui.Element {
	b := st.doc.Block(i)
	cb := &CodeBlockView{Language: b.Language(), Info: b.Info(), Open: b.Open, Index: i, st: st}
	if f := st.o.Code[cb.Language]; f != nil {
		box := ui.Column(c)
		return box.Children(func() { f(c, cb) })
	}
	if st.o.CodeBlock != nil {
		box := ui.Column(c)
		return box.Children(func() { st.o.CodeBlock(c, cb) })
	}
	return DefaultCodeBlock(c, cb)
}

// DefaultCodeBlock builds a code block as views do without hooks: a box
// with the language and a copy button above the highlighted code.
func DefaultCodeBlock(c *ui.Context, b *CodeBlockView) ui.Element {
	th := &b.st.th
	box := ui.Column(c).Direction(ui.LeftToRight).Background(th.CodeBlockBackground).Border(1, th.CodeBlockBorder).Radius(th.CodeBlockRadius).AlignSelf(ui.Stretch).Clip()
	box.Role(ui.RoleGroup).Label(codeLabel(b.Language))
	return box.Children(func() {
		head := ui.Row(c).AlignItems(ui.Center).Padding(th.em(0.35), th.em(0.4), 0, th.em(th.CodeBlockPadding))
		head.Children(func() {
			ui.Text(c, b.Language).FontSize(th.em(0.8)).TextColor(th.Muted).Unselectable()
			ui.Spacer(c)
			b.CopyButton(c)
		})
		b.Body(c)
	})
}

func codeLabel(lang string) string {
	if lang == "" {
		return "Code"
	}
	return "Code, " + lang
}

var (
	copyIcon  = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.3"><rect x="5.5" y="5.5" width="8" height="8" rx="1.6"/><path d="M10.5 3.6V3.2A1.7 1.7 0 0 0 8.8 1.5H4.2A1.7 1.7 0 0 0 2.5 3.2v4.6a1.7 1.7 0 0 0 1.7 1.7h.4"/></svg>`))
	checkIcon = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"><path d="M3.5 8.5l3 3 6-7"/></svg>`))
)

// CopyButton builds a button copying the block's code, which shows that
// it did for two seconds, and waits for a block being written to close.
func (b *CodeBlockView) CopyButton(c *ui.Context) ui.Element {
	st := b.st
	th := &st.th
	cs := st.codeState(b.Index)
	copied := !cs.copiedAt.IsZero() && c.Now().Sub(cs.copiedAt) < 2*time.Second
	tx := l10n.In(c.Locale())
	btn := ui.ButtonBase(c).Padding(th.em(0.2), th.em(0.45)).Radius(5).Gap(th.em(0.3)).Label(tx.CopyCode()).Disabled(b.Open)
	if btn.Hovered() && !b.Open {
		btn.Background(th.Text.Alpha(0.08))
	}
	btn.Children(func() {
		icon, label := copyIcon, tx.Copy()
		if copied {
			icon, label = checkIcon, tx.Copied()
		}
		size := th.em(0.95)
		ui.Icon(c, icon).Size(size, size).TextColor(th.Muted)
		ui.Text(c, label).FontSize(th.em(0.8)).TextColor(th.Muted)
	})
	if btn.Clicked() {
		c.WriteClipboard(b.Code())
		cs.copiedAt = c.Now()
		c.After(2 * time.Second)
	}
	return btn
}

// Body builds the block's code, highlighted, selectable with the view's
// text, scrolling sideways unless Options.WrapCode.
func (b *CodeBlockView) Body(c *ui.Context) ui.Element {
	st := b.st
	th := &st.th
	cs := st.codeState(b.Index)
	st.highlight(cs, b.Index)
	pad := th.em(th.CodeBlockPadding)
	var outer ui.Element
	if st.o.WrapCode {
		outer = ui.Column(c)
	} else {
		outer = ui.ScrollHorizontal(c)
	}
	// Code reads left to right in a right-to-left interface too, and
	// indents with tabs of 4 columns on every system, as in editors.
	outer.Direction(ui.LeftToRight).AlignSelf(ui.Stretch).Font(th.CodeFont).FontSize(th.em(th.CodeSize)).LineHeight(th.CodeLineHeight).TabSize(4)
	return outer.Children(func() {
		col := ui.Column(c).Padding(th.em(0.25), pad, pad, pad)
		if !st.o.WrapCode {
			col.Shrink(0)
		}
		col.Children(func() {
			for k := range cs.chunks {
				ch := &cs.chunks[k]
				el := ui.RichText(c, ch.spans...)
				if !st.o.WrapCode {
					el.NoWrap()
				}
				if objects := ch.objects; len(objects) > 0 {
					el.DrawObjects(func(pa *ui.Painter, at int, r ui.Rect) {
						if idx, ok := slices.BinarySearchFunc(objects, at, func(o mathObject, at int) int { return o.rune - at }); ok && objects[idx].draw != nil {
							objects[idx].draw(pa, r)
						}
					})
				}
				st.note(el.ID(), b.Index, k*chunkLines)
			}
		})
	})
}

type codeLineSwatch struct {
	at   int
	col  ui.Color
	grad []ui.Color
}

func findLineColorSwatches(line string) []codeLineSwatch {
	if !strings.ContainsAny(line, "#rRhHoOlLcC") {
		return nil
	}
	var out []codeLineSwatch
	pos := 0
	for pos < len(line) {
		a, z, col, grad, ok := findNextColorLiteral(line, pos)
		if !ok {
			break
		}
		out = append(out, codeLineSwatch{at: a, col: col, grad: grad})
		pos = z
	}
	return out
}

// highlight tokenizes the lines of code block i that changed, and makes
// the spans of the chunks they are in anew.
func (st *viewState) highlight(cs *codeState, i int) {
	d := st.doc
	n := d.LineCount(i)
	if cs.colorLiterals != st.o.ColorLiterals || (st.o.ColorLiterals && (cs.dark != st.uiTh.Dark || cs.surface != st.uiTh.Background)) {
		cs.chunks = cs.chunks[:0]
		cs.colorLiterals = st.o.ColorLiterals
		cs.dark = st.uiTh.Dark
		cs.surface = st.uiTh.Background
	}
	// The first line that differs from those tokenized.
	k0 := 0
	for k0 < n && k0 < len(cs.lines) {
		line, _ := d.Line(i, k0)
		if line != cs.lines[k0] {
			break
		}
		k0++
	}
	if k0 == n && len(cs.lines) == n && len(cs.chunks) == (n+chunkLines-1)/chunkLines {
		return
	}
	cs.lines, cs.ends, cs.toks = cs.lines[:k0], cs.ends[:k0], cs.toks[:k0]
	state := uint64(0)
	if k0 > 0 {
		state = cs.ends[k0-1]
	}
	for k := k0; k < n; k++ {
		line, _ := d.Line(i, k)
		var toks []editor.Token
		if cs.tok != nil {
			toks, state = cs.tok.Tokenize(line, state, nil)
		}
		cs.lines = append(cs.lines, line)
		cs.ends = append(cs.ends, state)
		cs.toks = append(cs.toks, toks)
	}
	chunks := (n + chunkLines - 1) / chunkLines
	if n == 0 {
		chunks = 1
	}
	for len(cs.chunks) < chunks {
		cs.chunks = append(cs.chunks, codeChunk{})
	}
	cs.chunks = cs.chunks[:chunks]
	for k := k0 / chunkLines; k < chunks; k++ {
		cs.chunks[k].ok = false
	}
	ct := st.th.CodeColors
	codeSize := st.th.em(st.th.CodeSize)
	dark := st.uiTh.Dark
	borderBase := st.th.Text
	for k := range cs.chunks {
		ch := &cs.chunks[k]
		if ch.ok {
			continue
		}
		ch.spans = ch.spans[:0]
		ch.objects = ch.objects[:0]
		runePos := 0
		emitSwatch := func(sw codeLineSwatch) {
			h := max(float32(7.0), codeSize*0.64)
			w := h
			var drawFn func(*ui.Painter, ui.Rect)
			if len(sw.grad) >= 2 {
				w = h * 2.0
				drawFn = makeGradientSwatchDraw(sw.grad, w, h, 0.5, dark, borderBase, 1)
			} else {
				drawFn = makeColorSwatchDraw(sw.col, h, 0.5, dark, borderBase, 1)
			}
			gap := max(float32(2.5), codeSize*0.22)
			ch.objects = append(ch.objects, mathObject{rune: runePos, draw: drawFn})
			ch.spans = append(ch.spans,
				ui.Span{Text: "\uFFFC", Size: codeSize, ObjectWidth: 0.5 + w + gap, ObjectHeight: codeSize * 0.74, ObjectDescent: codeSize * 0.14},
				ui.Span{Text: string(linkIconJoiner)},
			)
			runePos += 2
		}
		first := k * chunkLines
		last := min(first+chunkLines, n)
		for l := first; l < last; l++ {
			if l > first {
				ch.spans = append(ch.spans, ui.Span{Text: "\n"})
				runePos++
			}
			line := cs.lines[l]
			var swatches []codeLineSwatch
			if st.o.ColorLiterals {
				swatches = findLineColorSwatches(line)
			}
			swIdx := 0
			emitSegment := func(segStart, segEnd int, sp ui.Span) {
				cur := segStart
				for swIdx < len(swatches) && swatches[swIdx].at < segEnd {
					at := swatches[swIdx].at
					if at > cur {
						part := sp
						part.Text = line[cur:at]
						ch.spans = append(ch.spans, part)
						runePos += utf8.RuneCountInString(part.Text)
						cur = at
					}
					if at >= cur {
						emitSwatch(swatches[swIdx])
					}
					swIdx++
				}
				if cur < segEnd {
					part := sp
					part.Text = line[cur:segEnd]
					ch.spans = append(ch.spans, part)
					runePos += utf8.RuneCountInString(part.Text)
				}
			}
			start := 0
			for _, t := range cs.toks[l] {
				end := min(t.End, len(line))
				if end > start {
					sp := ui.Span{Color: ct.TokenColor(t.Kind)}
					if s := ct.Tokens[t.Kind]; s.Bold {
						sp.Weight = 600
					} else if s.Italic {
						sp.Italic = true
					}
					emitSegment(start, end, sp)
				}
				start = max(start, end)
			}
			if start < len(line) {
				emitSegment(start, len(line), ui.Span{Color: ct.Text})
			}
		}
		if len(ch.spans) == 0 {
			ch.spans = append(ch.spans, ui.Span{Text: " "})
		}
		ch.n = last - first
		ch.ok = true
	}
}
