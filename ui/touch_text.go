package ui

import (
	"math"
	"runtime"
	"time"

	"github.com/egoist/mygo/internal/platform"
	"github.com/egoist/mygo/internal/text"
)

// A finger edits and selects text as phones have it: a word chosen by holding
// it (with immediate hold-and-drag expansion), handles at the selection's ends
// that drag them across lines and paragraphs, edge auto-scroll, the system's
// magnifier, and the system's menu of what to do with it. The handles draw over
// everything, and take the fingers near them before what is under them.

// handleReach is how far from a handle, in DIPs, a finger takes it,
// caretTapReachX and caretTapReachY are how far from the caret a tap on an
// already-focused text input opens the system's edit menu, caretHandleFor how
// long Android's caret handle shows after the caret moved, and
// touchSelectScrollEdge how close to a scroll viewport's top or bottom a
// selection drag starts auto-scrolling.
const (
	handleReach           = 28
	caretTapReachX        = 28
	caretTapReachY        = 16
	caretHandleFor        = 4 * time.Second
	touchSelectDragSlop   = 4
	touchSelectScrollEdge = 56
)

// Which handle a finger holds.
const (
	handleNone = iota
	handleStart
	handleEnd
	handleCaret
)

// touchSelKind identifies which text selection backend owns the active touch
// handles.
type touchSelKind uint8

const (
	touchSelNone touchSelKind = iota
	touchSelAdapter
	touchSelScope
	touchSelReadOnly
)

// touchText tracks the active touch selection handles and edit menu.
type touchText struct {
	// id is the element that fingers last placed the selection or caret of, 0
	// for none: keys hide its handles.
	id uint64
	// scope is the Selectable container ID when the selection spans paragraphs
	// in a Selectable container (0 for a single input or standalone text).
	scope uint64
	// caretAt is when the caret handle started showing.
	caretAt time.Time
	// held is the handle a finger holds.
	held int
	// grabY offsets the finger's Y toward the vertical center of the line the
	// grabbed handle sits on so touching a top or bottom knob never jumps lines.
	grabY float32
	// menu is whether the system's edit menu shows, which menuFocus had
	// the focus as it came: it goes as a finger lands, keys type, or the
	// focus leaves what it was for, as Android's TextView has it.
	menu      bool
	menuFocus uint64
}

// touchSelHandles describes the current selection handle geometry in content
// DIPs, across editable inputs, read-only TextInputClient editors, standalone
// Selectable text, and multi-paragraph Selectable containers.
type touchSelHandles struct {
	kind     touchSelKind
	state    *state
	adapter  *textInputAdapter
	scope    uint64
	sel      platform.TextRange
	a, b     textPoint // ordered endpoints for touchSelScope
	rFrom    int       // ordered rune endpoints for touchSelReadOnly
	rTo      int
	start    Rect
	end      Rect
	hasStart bool
	hasEnd   bool
	empty    bool
}

// touchSelecting reports whether a finger is actively dragging a selection
// handle or extending a long-press selection.
func (rt *engine) touchSelecting() bool {
	return rt.touchText.held != handleNone || (rt.touch.down && rt.touch.selecting && rt.touch.selectMoved)
}

func (rt *engine) fromWindowRect(r platform.RectF) Rect {
	z := rt.zoomFactor()
	if z <= 0 {
		z = 1
	}
	return Rect{
		X: float32(r.X) / z,
		Y: float32(r.Y) / z,
		W: float32(r.W) / z,
		H: float32(r.H) / z,
	}
}

// paraSelectionEdge computes the content-space caret bar at the visual start or
// end of runes [from, to) inside paragraph s, and reports whether that bar lies
// inside s's visible clipped viewport.
func paraSelectionEdge(s *state, from, to int, wantEnd bool) (Rect, bool) {
	if s == nil || s.editor == nil || s.editor.layout == nil || s.vw <= 0 || s.vh <= 0 {
		return Rect{}, false
	}
	ed := s.editor
	l := ed.layout
	n := ed.buf.n
	from = max(0, min(from, n))
	to = max(from, min(to, n))
	var local Rect
	var have bool
	if from < to {
		if rects := l.SelectionVisual(from, to, false); len(rects) > 0 {
			r := rects[0]
			x := r.X
			if wantEnd {
				r = rects[len(rects)-1]
				x = r.X + r.W
			}
			local = Rect{X: s.x + ed.originX + x - ed.scrollX, Y: s.y + ed.originY + r.Y, W: 1, H: max(r.H, 1)}
			have = true
		}
	}
	if !have {
		at := from
		if wantEnd {
			at = to
		}
		at = max(0, min(at, len(l.Runes)))
		cx, cy, ch := l.CaretAt(text.CaretPosition{Index: at})
		local = Rect{X: s.x + ed.originX + cx - ed.scrollX, Y: s.y + ed.originY + cy, W: 1, H: max(ch, 1)}
	}
	wr := transformRect(s.world, local)
	visible := wr.Y+wr.H >= s.vy-1 && wr.Y <= s.vy+s.vh+1 && wr.X >= s.vx-2 && wr.X <= s.vx+s.vw+2
	return wr, visible
}

func stateVisibleBox(s *state) Rect {
	if s.vw > 0 && s.vh > 0 {
		return Rect{X: s.vx, Y: s.vy, W: s.vw, H: s.vh}
	}
	return transformRect(s.world, Rect{X: s.x, Y: s.y, W: s.w, H: s.h})
}

// selectionHandles returns the active touch selection handle geometry in
// content DIPs.
func (rt *engine) selectionHandles() (touchSelHandles, bool) {
	t := &rt.touchText
	// 1. Multi-paragraph Selectable container.
	if t.scope != 0 && rt.selection.scope == t.scope {
		a, b, selected := rt.textSelectionBounds()
		if !selected {
			return touchSelHandles{}, false
		}
		sa, sb := rt.states[a.id], rt.states[b.id]
		if sa == nil || sb == nil || !rt.textSelectionVisible(sa) {
			return touchSelHandles{}, false
		}
		startTo := sa.editor.buf.n
		if a.id == b.id {
			startTo = b.at
		}
		endFrom := 0
		if a.id == b.id {
			endFrom = a.at
		}
		start, hasStart := paraSelectionEdge(sa, a.at, startTo, false)
		end, hasEnd := paraSelectionEdge(sb, endFrom, b.at, true)
		if !hasStart && !hasEnd {
			return touchSelHandles{}, false
		}
		return touchSelHandles{
			kind:     touchSelScope,
			state:    rt.states[rt.focused],
			scope:    t.scope,
			a:        a,
			b:        b,
			start:    start,
			end:      end,
			hasStart: hasStart,
			hasEnd:   hasEnd,
		}, true
	}

	if t.id == 0 || t.id != rt.focused {
		return touchSelHandles{}, false
	}
	s := rt.states[t.id]
	if s == nil || s.flags&flagDisabled != 0 {
		return touchSelHandles{}, false
	}

	// 2. TextInput / TextArea / HandleTextInput (including read-only code editor).
	if s.textAdapter != nil && s.textClient != nil && (s.editor == nil || !s.editor.password) {
		a := s.textAdapter
		sel := a.Selection().Range
		vis := stateVisibleBox(s)
		inVis := func(r Rect) bool {
			return r.Y+r.H >= vis.Y-1 && r.Y <= vis.Y+vis.H+1 && r.X >= vis.X-2 && r.X <= vis.X+vis.W+2
		}
		var start, end Rect
		var hasStart, hasEnd bool
		if rs, _, got := a.BoundsForRange(platform.TextRange{Start: sel.Start, End: sel.Start}); got {
			start = rt.fromWindowRect(rs)
			hasStart = inVis(start)
		}
		if sel.Start == sel.End {
			if !hasStart {
				return touchSelHandles{}, false
			}
			// Read-only TextInputClient (e.g. code preview) shows handles only
			// for a non-empty range selection, never a bare caret.
			if s.textClientReadOnly() {
				return touchSelHandles{}, false
			}
			return touchSelHandles{
				kind:     touchSelAdapter,
				state:    s,
				adapter:  a,
				sel:      sel,
				start:    start,
				end:      start,
				hasStart: true,
				hasEnd:   true,
				empty:    true,
			}, true
		}
		if re, _, got := a.BoundsForRange(platform.TextRange{Start: sel.End, End: sel.End}); got {
			end = rt.fromWindowRect(re)
			hasEnd = inVis(end)
		}
		if !hasStart && !hasEnd {
			return touchSelHandles{}, false
		}
		return touchSelHandles{
			kind:     touchSelAdapter,
			state:    s,
			adapter:  a,
			sel:      sel,
			start:    start,
			end:      end,
			hasStart: hasStart,
			hasEnd:   hasEnd,
		}, true
	}

	// 3. Standalone Selectable text (s.textScope == 0).
	if s.editor != nil && s.flags&flagSelectable != 0 && s.textScope == 0 && rt.textSelectionVisible(s) {
		from, to := s.editor.selection()
		if from == to {
			return touchSelHandles{}, false
		}
		start, hasStart := paraSelectionEdge(s, from, to, false)
		end, hasEnd := paraSelectionEdge(s, from, to, true)
		if !hasStart && !hasEnd {
			return touchSelHandles{}, false
		}
		return touchSelHandles{
			kind:     touchSelReadOnly,
			state:    s,
			rFrom:    from,
			rTo:      to,
			start:    start,
			end:      end,
			hasStart: hasStart,
			hasEnd:   hasEnd,
		}, true
	}

	return touchSelHandles{}, false
}

// handles returns the input whose handles show and the places of its selection
// or caret, in the window: where it starts and ends.
func (rt *engine) handles() (a *textInputAdapter, start, end platform.RectF, sel platform.TextRange, ok bool) {
	h, valid := rt.selectionHandles()
	if !valid || h.kind != touchSelAdapter || !h.hasStart || !h.hasEnd {
		return nil, start, end, sel, false
	}
	toWin := func(r Rect) platform.RectF {
		return rt.toWindow(platform.RectF{X: float64(r.X), Y: float64(r.Y), W: float64(r.W), H: float64(r.H)})
	}
	return h.adapter, toWin(h.start), toWin(h.end), h.sel, true
}

// caretHandle reports whether a caret, with nothing selected, has a handle:
// Android's do, for a while after it moved; iOS has none.
func (rt *engine) caretHandle(now time.Time) bool {
	t := &rt.touchText
	return overscrollKind == overscrollStretch && !t.caretAt.IsZero() && now.Sub(t.caretAt) < caretHandleFor
}

// paintHandles draws the selection's handles over what the window shows.
func (rt *engine) paintHandles(p *Painter) {
	h, ok := rt.selectionHandles()
	if !ok {
		return
	}
	color := p.Theme().Accent
	cupertino := runtime.GOOS == "ios" || (runtime.GOOS == "darwin" && overscrollKind != overscrollStretch) || overscrollKind == overscrollBounce
	if h.empty {
		if h.hasEnd && (rt.caretHandle(p.Now()) || rt.touchText.held == handleCaret) {
			drawTeardrop(p, h.end.X, h.end.Y+h.end.H, color, 0)
			p.After(time.Second)
		}
		return
	}
	if cupertino {
		// A bar along the line, with a knob at its outer end.
		bar := func(r Rect, top bool) {
			x, y0, y1 := r.X, r.Y, r.Y+r.H
			p.Fill(Rect{x - 1, y0, 2, y1 - y0}, color, 1)
			cy := y1 + 4
			if top {
				cy = y0 - 4
			}
			p.Fill(Rect{x - 5, cy - 5, 10, 10}, color, 5)
		}
		if h.hasStart {
			bar(h.start, true)
		}
		if h.hasEnd {
			bar(h.end, false)
		}
		return
	}
	// Android's teardrops under the line, leaning out of the selection.
	if h.hasStart {
		drawTeardrop(p, h.start.X, h.start.Y+h.start.H, color, -1)
	}
	if h.hasEnd {
		drawTeardrop(p, h.end.X, h.end.Y+h.end.H, color, 1)
	}
}

// drawTeardrop draws Android's text handle with its point at x, y: straight
// down from the point (lean 0), or leaning to a side.
func drawTeardrop(p *Painter, x, y float32, c Color, lean float32) {
	const r = 9
	cx, cy := x+lean*r, y+r
	p.Fill(Rect{cx - r, cy - r + 2, 2 * r, 2 * r}, c, r)
	var path Path
	if lean == 0 {
		path.MoveTo(x, y).LineTo(x+r*0.8, y+r*0.6).LineTo(x-r*0.8, y+r*0.6).Close()
	} else {
		path.MoveTo(x, y).LineTo(x+lean*r*1.6, y+2).LineTo(x, y+r*1.4).Close()
	}
	p.FillPath(&path, c)
}

func distSqPointToVSeg(px, py, sx, y0, y1 float32) float32 {
	dx := px - sx
	cy := min(max(py, y0), y1)
	dy := py - cy
	return dx*dx + dy*dy
}

func handleGrabOffset(r Rect, y float32) float32 {
	mid := r.Y + r.H/2
	return max(-12, min(12, mid-y))
}

// handleAt returns the handle a finger going down at x, y takes, and the
// vertical offset from the finger to the line center of that handle.
func (rt *engine) handleAt(x, y float32) (int, float32) {
	h, ok := rt.selectionHandles()
	if !ok {
		return handleNone, 0
	}
	const maxDistSq = float32(handleReach * handleReach)
	if h.empty {
		if h.hasEnd && rt.caretHandle(rt.now()) {
			dx, dy := x-h.end.X, y-(h.end.Y+h.end.H+9)
			if dx*dx+dy*dy < maxDistSq {
				return handleCaret, handleGrabOffset(h.end, y)
			}
		}
		return handleNone, 0
	}
	cupertino := runtime.GOOS == "ios" || (runtime.GOOS == "darwin" && overscrollKind != overscrollStretch) || overscrollKind == overscrollBounce
	best := handleNone
	bestDist := maxDistSq
	var bestGrab float32
	check := func(kind int, r Rect, top bool) {
		var d float32
		if cupertino {
			y0, y1 := r.Y, r.Y+r.H
			if top {
				y0 -= 6
			} else {
				y1 += 6
			}
			d = distSqPointToVSeg(x, y, r.X, y0, y1)
		} else {
			lean := float32(1)
			if top {
				lean = -1
			}
			dx1, dy1 := x-r.X, y-(r.Y+r.H)
			dx2, dy2 := x-(r.X+lean*9), y-(r.Y+r.H+9)
			d = min(dx1*dx1+dy1*dy1, dx2*dx2+dy2*dy2)
		}
		if d < bestDist {
			bestDist = d
			best = kind
			bestGrab = handleGrabOffset(r, y)
		}
	}
	if h.hasStart {
		check(handleStart, h.start, true)
	}
	if h.hasEnd {
		check(handleEnd, h.end, false)
	}
	return best, bestGrab
}

// dragHandle moves the selection's end a finger holds to under it.
func (rt *engine) dragHandle(x, y float32) {
	h, ok := rt.selectionHandles()
	if !ok {
		return
	}
	t := &rt.touchText
	ty := y + t.grabY
	wx, wy := rt.windowPoint(x, ty)
	changed := false

	switch h.kind {
	case touchSelAdapter:
		i, found := h.adapter.IndexForPoint(wx, wy)
		if !found {
			return
		}
		to := h.sel
		switch t.held {
		case handleCaret:
			to = platform.TextRange{Start: i, End: i}
		case handleStart:
			if i > h.sel.End {
				to = platform.TextRange{Start: h.sel.End, End: i}
				t.held = handleEnd
			} else if i < h.sel.End {
				to.Start = i
			}
		case handleEnd:
			if i < h.sel.Start {
				to = platform.TextRange{Start: i, End: h.sel.Start}
				t.held = handleStart
			} else if i > h.sel.Start {
				to.End = i
			}
		}
		if to != h.sel {
			h.adapter.SetTextSelection(to)
			changed = true
		}

	case touchSelScope:
		p := rt.textSelectionAt(h.scope, x, ty)
		if p.id == 0 {
			return
		}
		a, b := h.a, h.b
		var na, nb textPoint
		switch t.held {
		case handleStart:
			cmp := rt.compareTextPoints(p, b)
			switch {
			case cmp < 0:
				na, nb = p, b
			case cmp > 0:
				na, nb = b, p
				t.held = handleEnd
			default:
				na, nb = a, b
			}
		case handleEnd:
			cmp := rt.compareTextPoints(p, a)
			switch {
			case cmp > 0:
				na, nb = a, p
			case cmp < 0:
				na, nb = p, a
				t.held = handleStart
			default:
				na, nb = a, b
			}
		}
		if na != a || nb != b {
			rt.selection.anchor, rt.selection.caret = na, nb
			rt.focused = p.id
			t.id = p.id
			changed = true
		}

	case touchSelReadOnly:
		lx, ly := h.state.local(x, ty)
		i := h.state.editor.hit(lx, ly)
		from, to := h.rFrom, h.rTo
		nf, nt := from, to
		switch t.held {
		case handleStart:
			if i < to {
				nf = i
			} else if i > to {
				nf, nt = to, i
				t.held = handleEnd
			}
		case handleEnd:
			if i > from {
				nt = i
			} else if i < from {
				nf, nt = i, from
				t.held = handleStart
			}
		}
		if nf != from || nt != to {
			h.state.editor.anchor, h.state.editor.caret = nf, nt
			changed = true
		}
	}

	if changed {
		rt.haptic("selection")
	}
	mx, my := rt.windowPoint(x, y)
	rt.touchSurface(func(s platform.TouchSurface) { s.ShowMagnifier(mx, my) })
	t.caretAt = rt.now()
	rt.requestFrame()
}

// initLongPressSelection records the initial word anchor right after a long
// press selects a word, so keeping the finger down can drag-expand it.
func (rt *engine) initLongPressSelection(t *touchPan, target *state, x, y float32) {
	t.selecting = true
	t.selectMoved = false
	t.selectX0, t.selectY0 = x, y
	t.autoScrollAt = rt.now()
	t.selectPts = [2]textPoint{}
	t.selectRange = platform.TextRange{}
	if target == nil {
		return
	}
	if target.textScope != 0 && rt.selection.scope == target.textScope {
		if a, b, ok := rt.textSelectionBounds(); ok {
			t.selectPts = [2]textPoint{a, b}
		}
		return
	}
	if target.textAdapter != nil && target.textClient != nil {
		t.selectRange = target.textAdapter.Selection().Range
		return
	}
	if target.editor != nil {
		a, b := target.editor.selection()
		t.selectRange = platform.TextRange{Start: a, End: b}
	}
}

// dragLongPressSelection extends a long-press word selection as the user keeps
// their finger down and slides across text, without releasing first.
func (rt *engine) dragLongPressSelection(t *touchPan, x, y float32) {
	s := rt.states[rt.focused]
	if s == nil || s.flags&flagDisabled != 0 {
		return
	}
	changed := false
	switch {
	case rt.touchText.scope != 0 && rt.selection.scope == rt.touchText.scope:
		p := rt.textSelectionAt(rt.touchText.scope, x, y)
		if p.id == 0 {
			return
		}
		origA, origB := t.selectPts[0], t.selectPts[1]
		if origA.id == 0 {
			origA, origB = p, p
			t.selectPts = [2]textPoint{origA, origB}
		}
		var na, nb textPoint
		switch {
		case rt.compareTextPoints(p, origA) < 0:
			na, nb = origB, p
		case rt.compareTextPoints(p, origB) > 0:
			na, nb = origA, p
		default:
			na, nb = origA, origB
		}
		if rt.selection.anchor != na || rt.selection.caret != nb {
			rt.selection.anchor, rt.selection.caret = na, nb
			rt.focused = p.id
			rt.touchText.id = p.id
			changed = true
		}

	case s.textAdapter != nil && s.textClient != nil && (s.editor == nil || !s.editor.password):
		wx, wy := rt.windowPoint(x, y)
		i, found := s.textAdapter.IndexForPoint(wx, wy)
		if !found {
			return
		}
		orig := t.selectRange
		to := orig
		switch {
		case i < orig.Start:
			to = platform.TextRange{Start: i, End: orig.End}
		case i > orig.End:
			to = platform.TextRange{Start: orig.Start, End: i}
		}
		cur := s.textAdapter.Selection().Range
		if to != cur {
			s.textAdapter.SetTextSelection(to)
			changed = true
		}

	case s.editor != nil && s.flags&flagSelectable != 0 && s.textScope == 0:
		p := textHit(s, x, y)
		orig := t.selectRange
		na, nb := orig.Start, orig.End
		switch {
		case p.at < orig.Start:
			na, nb = orig.End, p.at
		case p.at > orig.End:
			na, nb = orig.Start, p.at
		}
		if s.editor.anchor != na || s.editor.caret != nb {
			s.editor.anchor, s.editor.caret = na, nb
			changed = true
		}
	default:
		return
	}

	if changed {
		rt.haptic("selection")
	}
	mx, my := rt.windowPoint(x, y)
	rt.touchSurface(func(ts platform.TouchSurface) { ts.ShowMagnifier(mx, my) })
	rt.touchText.caretAt = rt.now()
	rt.requestFrame()
}

// touchAutoScroll smoothly scrolls an enclosing scroll container (or custom
// scrolling text editor) when a finger drags a selection handle or hold-drags
// near its top or bottom edge.
func (rt *engine) touchAutoScroll(now time.Time) {
	t := &rt.touch
	if !t.down || !rt.touchSelecting() {
		return
	}
	dt := float32(now.Sub(t.autoScrollAt).Seconds())
	t.autoScrollAt = now
	if dt <= 0 {
		rt.animating = true
		return
	}
	dt = min(dt, 0.05)

	start := rt.states[rt.focused]
	if start == nil && rt.touchText.scope != 0 {
		start = rt.states[rt.touchText.scope]
	}
	for sc := start; sc != nil; sc = rt.states[sc.parent] {
		isScroll := sc.flags&flagScrollY != 0 && sc.contentH > float64(sc.h)
		isCustom := !isScroll && sc.input != nil && sc.textClient != nil
		if !isScroll && !isCustom {
			continue
		}
		_, ly := sc.local(t.lx, t.ly)
		var dir, frac float32
		switch {
		case ly < touchSelectScrollEdge:
			dir = -1
			frac = min(1, max(0, (touchSelectScrollEdge-ly)/touchSelectScrollEdge))
		case sc.h-ly < touchSelectScrollEdge:
			dir = 1
			frac = min(1, max(0, (touchSelectScrollEdge-(sc.h-ly))/touchSelectScrollEdge))
		}
		if dir == 0 {
			return
		}
		// Quadratic ramp from 80 to 420 DIPs/s as the finger nears the edge.
		dy := dir * (80 + 340*frac*frac) * dt
		moved := false
		if isScroll {
			moved = scrollBy(sc, 0, dy)
		} else {
			moved = rt.deliver(sc, InputEvent{Kind: InputScroll, DY: dy, Precise: true})
		}
		if moved {
			t.scrollSelection = true
			if rt.touchText.held != handleNone {
				rt.dragHandle(t.lx, t.ly)
			} else if t.selecting && t.selectMoved {
				rt.dragLongPressSelection(t, t.lx, t.ly)
			}
			rt.animating = true
		}
		return
	}
}

// windowPoint returns the content's x, y in the window.
func (rt *engine) windowPoint(x, y float32) (float64, float64) {
	r := rt.toWindow(platform.RectF{X: float64(x), Y: float64(y)})
	return r.X, r.Y
}

// touchSurface calls fn with the window's touch surface, where it has one.
func (rt *engine) touchSurface(fn func(platform.TouchSurface)) {
	if h, ok := rt.host.(interface{ touchSurface() platform.TouchSurface }); ok {
		if s := h.touchSurface(); s != nil {
			fn(s)
		}
	}
}

// touchSurfaceOf returns the window's touch surface, nil without one.
func (rt *engine) touchSurfaceOf() platform.TouchSurface {
	if h, ok := rt.host.(interface{ touchSurface() platform.TouchSurface }); ok {
		return h.touchSurface()
	}
	return nil
}

// hideEditMenu takes the system's edit menu away, where it shows.
func (rt *engine) hideEditMenu() {
	if !rt.touchText.menu {
		return
	}
	rt.touchText.menu = false
	rt.touchSurface(func(ts platform.TouchSurface) { ts.HideEditMenu() })
}

// showEditMenu shows the system's menu of editing commands over the
// selection of the input or selectable text with the focus.
func (rt *engine) showEditMenu(s *state) {
	if s == nil {
		s = rt.states[rt.focused]
	}
	if s == nil {
		rt.hideEditMenu()
		return
	}
	var actions []string
	for _, name := range [...]string{"cut", "copy", "paste", "selectAll"} {
		if rt.commandEnabled(name) {
			actions = append(actions, name)
		}
	}
	if len(actions) == 0 {
		rt.hideEditMenu()
		return
	}
	// Sync focus, text, and selection to the host before presenting the menu
	// so the host's next SetTextInput update does not dismiss it.
	rt.updateTextInput()
	var cx, cy float32
	placed := false
	if h, ok := rt.selectionHandles(); ok {
		switch {
		case h.kind == touchSelScope:
			if rects := rt.selectionRects(h.scope); len(rects) > 0 {
				r := rects[0]
				cx, cy, placed = r.X+r.W/2, r.Y, true
			}
		case h.hasStart && h.hasEnd:
			if math.Abs(float64(h.start.Y-h.end.Y)) < 2 {
				cx, cy, placed = (h.start.X+h.end.X)/2, h.start.Y, true
			} else {
				cx, cy, placed = h.start.X, h.start.Y, true
			}
		case h.hasStart:
			cx, cy, placed = h.start.X, h.start.Y, true
		case h.hasEnd:
			cx, cy, placed = h.end.X, h.end.Y, true
		}
	}
	if !placed {
		r := transformRect(s.world, Rect{s.x, s.y, s.w, s.h})
		cx, cy = r.X+r.W/2, r.Y
	}
	wx, wy := rt.windowPoint(cx, cy)
	rt.touchSurface(func(ts platform.TouchSurface) {
		ts.ShowEditMenu(wx, wy, actions)
		rt.touchText.menu, rt.touchText.menuFocus = true, rt.focused
	})
}

// editMenuLeft hides the system's edit menu once what it was for has the
// focus no more, or the frame built it no more, as a dialog holding it
// closed (#2249).
func (rt *engine) editMenuLeft() {
	t := &rt.touchText
	if !t.menu {
		return
	}
	if s := rt.states[t.id]; s == nil || s.seen != rt.frame || s.pass != rt.pass || rt.focused != t.menuFocus {
		rt.hideEditMenu()
	}
}

// textTapped notes a finger's tap on the text input s, which it placed the
// caret of: tapped again on the caret while the menu is closed, the menu
// shows what can be done there; tapped while it is open, it stays closed.
func (rt *engine) textTapped(s *state, focused bool, sel platform.TextRange, menu bool, x, y float32) {
	t := &rt.touchText
	t.id = s.id
	t.scope = 0
	t.caretAt = rt.now()
	if menu || !focused || s.textAdapter == nil || s.textClientReadOnly() {
		return
	}
	cur := s.textAdapter.Selection().Range
	if cur != sel || cur.Start != cur.End {
		return
	}
	caret, _, ok := s.textAdapter.BoundsForRange(platform.TextRange{Start: cur.Start, End: cur.Start})
	if !ok {
		return
	}
	wx, wy := rt.windowPoint(x, y)
	if wx < caret.X-caretTapReachX || wx > caret.X+caret.W+caretTapReachX ||
		wy < caret.Y-caretTapReachY || wy > caret.Y+caret.H+caretTapReachY {
		return
	}
	rt.showEditMenu(s)
}

// textKeyed hides the handles and the edit menu as keys type: what they
// point at moved.
func (rt *engine) textKeyed() {
	rt.touchText.caretAt = time.Time{}
	rt.hideEditMenu()
}

// textInputEnded clears text focus when the system ends editing (such as the
// user dismissing the iOS keyboard or a native view becoming first responder).
func (rt *engine) textInputEnded() {
	rt.hideEditMenu()
	s := rt.states[rt.focused]
	if s == nil || (s.textClient == nil && !s.takesText) {
		return
	}
	rt.focused = 0
	rt.focusVisible = false
	rt.touchText.id = 0
	rt.touchText.scope = 0
	rt.requestFrame()
}

// revealFocus scrolls the input with the focus into view, as the keyboard
// covers part of the window.
func (rt *engine) revealFocus() {
	if s := rt.states[rt.focused]; s != nil && s.textClient != nil && !s.textClientReadOnly() {
		rt.reveal(rt.focused)
	}
}
