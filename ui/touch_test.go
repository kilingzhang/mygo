package ui

import (
	"slices"
	"testing"
	"time"

	"github.com/egoist/mygo/internal/platform"
)

// A finger pans what scrolls under it once it moves, and the press ends
// without a click; lifted as it moves, the content flings on and slows
// down to a stop.
func TestTouchPans(t *testing.T) {
	var s ScrollState
	clicks := 0
	tt := coreNewTester(func(c *context) {
		coreScroll(c).Fill().TrackScroll(&s).Children(func() {
			b := coreBox(c).Height(1000).Shrink(0)
			if b.Clicked() {
				clicks++
			}
		})
	}, 200, 200)
	// A tap clicks.
	tt.FingerDown(100, 150)
	tt.FingerUp(100, 150)
	tt.Frame()
	if clicks != 1 || s.Y != 0 {
		t.Fatalf("a tap: %d clicks, scrolled to %v", clicks, s.Y)
	}
	// Moving less than the slop is still a press.
	tt.Advance(time.Second)
	tt.FingerDown(100, 150)
	tt.FingerMove(100, 146)
	if s.Y != 0 {
		t.Errorf("within the slop: scrolled to %v", s.Y)
	}
	// Past it, the finger drags the content, which follows it from there.
	tt.FingerMove(100, 140)
	tt.Advance(16 * time.Millisecond)
	tt.FingerMove(100, 100)
	if s.Y != 40 {
		t.Errorf("dragged 40 up: scrolled to %v", s.Y)
	}
	tt.Advance(16 * time.Millisecond)
	tt.FingerMove(100, 60)
	tt.Advance(16 * time.Millisecond)
	tt.FingerUp(100, 20)
	tt.Frame()
	if clicks != 1 {
		t.Errorf("a pan clicked: %d clicks", clicks)
	}
	at := s.Y
	if at != 80 {
		t.Errorf("after the pan: at %v", at)
	}
	// It flings on, slower and slower, until it stops.
	tt.Advance(50 * time.Millisecond)
	if s.Y <= at {
		t.Errorf("no fling: at %v, was %v", s.Y, at)
	}
	tt.Advance(10 * time.Second)
	if tt.FramePending() || s.Y <= at || s.Y > 800 {
		t.Errorf("after the fling: at %v, frames pending %v", s.Y, tt.FramePending())
	}
	// A finger stops a fling and taps nothing.
	s.Y = 0
	tt.Frame()
	tt.Advance(time.Second)
	tt.FingerDown(100, 150)
	tt.Advance(16 * time.Millisecond)
	tt.FingerMove(100, 100)
	tt.Advance(16 * time.Millisecond)
	tt.FingerUp(100, 40)
	tt.Advance(16 * time.Millisecond)
	tt.FingerDown(100, 100)
	tt.FingerUp(100, 100)
	stopped := s.Y
	tt.Advance(time.Second)
	if s.Y != stopped || clicks != 1 {
		t.Errorf("a tap on a fling: moved from %v to %v, %d clicks", stopped, s.Y, clicks)
	}
}

// A finger that lands on a fling stops it and taps nothing, but pans once
// it moves past the slop, as UIKit's scroll views follow the finger that
// stops their deceleration: flicking again and again keeps the content
// going (#2003).
func TestTouchPansAfterCatchingFling(t *testing.T) {
	var s ScrollState
	clicks := 0
	tt := coreNewTester(func(c *context) {
		coreScroll(c).Fill().TrackScroll(&s).Children(func() {
			if coreBox(c).Height(5000).Shrink(0).Clicked() {
				clicks++
			}
		})
	}, 200, 200)
	flick := func() {
		tt.FingerDown(100, 150)
		tt.Advance(16 * time.Millisecond)
		tt.FingerMove(100, 100)
		tt.Advance(16 * time.Millisecond)
		tt.FingerUp(100, 40)
	}
	flick()
	// Whole frames (60 Hz), for the frame of the finger's landing not to
	// come too soon after the last, which TestTouchFlingSurvivesFrameTooSoon
	// tests.
	tt.Advance(3 * (time.Second / 60))
	if !tt.rt.flinging.on {
		t.Fatalf("no fling after a flick: at %v", s.Y)
	}
	// The finger stops it.
	tt.FingerDown(100, 150)
	caught := s.Y
	tt.Advance(50 * time.Millisecond)
	if tt.rt.flinging.on || s.Y != caught {
		t.Fatalf("the finger did not stop the fling: at %v, was %v", s.Y, caught)
	}
	// Within the slop it moves nothing; past it, the content follows it
	// from there.
	tt.FingerMove(100, 146)
	tt.FingerMove(100, 140)
	if s.Y != caught {
		t.Errorf("within and at the slop: at %v, was %v", s.Y, caught)
	}
	tt.Advance(16 * time.Millisecond)
	tt.FingerMove(100, 100)
	if s.Y != caught+40 {
		t.Errorf("dragged 40 up after catching the fling: at %v, want %v", s.Y, caught+40)
	}
	// Lifted as it moves, it flings again, and clicked nothing.
	tt.Advance(16 * time.Millisecond)
	tt.FingerUp(100, 40)
	if !tt.rt.flinging.on {
		t.Errorf("no fling after the second flick")
	}
	tt.Advance(10 * time.Second)
	if clicks != 0 {
		t.Errorf("flicks clicked: %d clicks", clicks)
	}
	// A finger that catches a fling and lifts without moving still taps
	// nothing, and stays swallowed within the slop.
	s.Y = 0
	tt.Frame()
	tt.Advance(time.Second)
	flick()
	tt.Advance(16 * time.Millisecond)
	tt.FingerDown(100, 100)
	tt.FingerMove(100, 95)
	stopped := s.Y
	tt.FingerUp(100, 95)
	tt.Advance(time.Second)
	if s.Y != stopped || clicks != 0 {
		t.Errorf("a tap on a fling: moved from %v to %v, %d clicks", stopped, s.Y, clicks)
	}
}

// A frame too soon after the last to move a fling (its decay rounds to
// none in float32) leaves the fling going, its time adding up to the next
// frame, rather than ending it as if the content had reached its end
// (#2003).
func TestTouchFlingSurvivesFrameTooSoon(t *testing.T) {
	var s ScrollState
	tt := coreNewTester(func(c *context) {
		coreScroll(c).Fill().TrackScroll(&s).Children(func() {
			coreBox(c).Height(5000).Shrink(0)
		})
	}, 200, 200)
	tt.FingerDown(100, 150)
	tt.Advance(16 * time.Millisecond)
	tt.FingerMove(100, 100)
	tt.Advance(16 * time.Millisecond)
	tt.FingerUp(100, 40)
	f := &tt.rt.flinging
	if !f.on {
		t.Fatalf("no fling after a flick")
	}
	at, vy, was := f.at, f.vy, s.Y
	tt.rt.flingStep(at.Add(time.Nanosecond))
	if !f.on {
		t.Fatalf("a frame 1ns after the last ended the fling")
	}
	if f.at != at || f.vy != vy || s.Y != was {
		t.Errorf("a frame that moved nothing changed the fling: at %v (was %v), vy %v (was %v), scrolled to %v (was %v)", f.at, at, f.vy, vy, s.Y, was)
	}
	tt.Advance(50 * time.Millisecond)
	if s.Y <= was {
		t.Errorf("the fling did not go on: at %v, was %v", s.Y, was)
	}
}

// A scroll container reaching the bottom of the safe area goes on under
// the bottom inset, as UIKit's under a floating tab bar, and scrolls as
// far again; what does not scroll stays clear of the insets.
func TestScrollUnderBottomInset(t *testing.T) {
	var s ScrollState
	var box *node
	tt := coreNewTester(func(c *context) {
		coreColumn(c).Fill().Children(func() {
			box = coreBox(c).Height(20).Shrink(0)
			coreScroll(c).Grow(1).TrackScroll(&s).Children(func() {
				coreBox(c).Height(1000).Shrink(0)
			})
		})
	}, 200, 400)
	tt.SetInsets(30, 0, 50, 0)
	if box.y != 30 {
		t.Errorf("the box is at %v, under the top inset", box.y)
	}
	s.Y = 10000
	tt.Frame()
	tt.Frame()
	// 400 high: 30 above, 20 of box, the scroll from 50 to the bottom
	// edge, 350 high, whose end comes out 50 above it.
	if want := float32(1000 - (400 - 50 - 50)); s.Y != want {
		t.Errorf("scrolled to the end at %v, want %v", s.Y, want)
	}
}

// A finger on what scrolls presses only once pressDelay passed without
// it moving: a pan that starts sooner presses nothing.
func TestTouchPressDelay(t *testing.T) {
	pressed := false
	tt := coreNewTester(func(c *context) {
		coreScroll(c).Fill().Children(func() {
			b := coreBox(c).Height(1000).Shrink(0)
			b.Clicked()
			pressed = b.Pressed()
		})
	}, 200, 200)
	tt.FingerDown(100, 100)
	tt.Frame()
	if pressed {
		t.Error("pressed before the delay")
	}
	tt.Advance(pressDelay + 20*time.Millisecond)
	if !pressed {
		t.Error("not pressed after the delay")
	}
	tt.FingerUp(100, 100)
	tt.Advance(time.Second)
	tt.FingerDown(100, 150)
	tt.Advance(16 * time.Millisecond)
	tt.FingerMove(100, 120)
	tt.Advance(pressDelay * 2)
	if pressed {
		t.Error("a pan pressed")
	}
	tt.FingerUp(100, 100)
}

// A finger keeps to the way it first went, and to the container that scrolls
// that way: a list across inside a list down takes only what goes across.
func TestTouchLocksAxis(t *testing.T) {
	var outer, inner ScrollState
	tt := coreNewTester(func(c *context) {
		coreScroll(c).Fill().TrackScroll(&outer).Children(func() {
			coreScrollHorizontal(c).Height(100).Shrink(0).TrackScroll(&inner).Children(func() {
				coreBox(c).Width(1000).Height(100).Shrink(0)
			})
			coreBox(c).Height(1000).Shrink(0)
		})
	}, 200, 200)
	tt.FingerDown(100, 50)
	tt.Advance(time.Millisecond)
	tt.FingerMove(70, 52) // across
	tt.Advance(16 * time.Millisecond)
	tt.FingerMove(40, 90) // then diagonally: it keeps going across
	tt.Frame()
	if inner.X != 30 || outer.Y != 0 {
		t.Errorf("across: inner %v, outer %v", inner.X, outer.Y)
	}
	tt.FingerUp(40, 90)
	tt.Advance(5 * time.Second)
	inner.X = 0
	tt.Frame()
	tt.FingerDown(100, 150)
	tt.Advance(time.Millisecond)
	tt.FingerMove(102, 120) // down the list, from outside the inner one
	tt.Advance(16 * time.Millisecond)
	tt.FingerMove(102, 100)
	tt.Frame()
	if inner.X != 0 || outer.Y != 20 {
		t.Errorf("down: inner %v, outer %v", inner.X, outer.Y)
	}
}

// Past the end the content follows the finger less and less far, and springs
// back as it lifts.
func TestTouchOverscrolls(t *testing.T) {
	defer func(k overscroll) { overscrollKind = k }(overscrollKind)
	overscrollKind = overscrollBounce
	var s ScrollState
	var top float32
	tt := coreNewTester(func(c *context) {
		coreScroll(c).Fill().TrackScroll(&s).Children(func() {
			b := coreBox(c).Height(1000).Shrink(0)
			top = b.st.y
		})
	}, 200, 200)
	tt.Frame()
	tt.FingerDown(100, 50)
	tt.Advance(time.Millisecond)
	tt.FingerMove(100, 60)
	tt.Advance(16 * time.Millisecond)
	tt.FingerMove(100, 110) // 50 past the top
	tt.Frame()
	tt.Frame()
	if s.Y != 0 {
		t.Fatalf("scrolled to %v", s.Y)
	}
	if top <= 0 || top >= 50 {
		t.Fatalf("pulled 50 past the top: the content shows %v down", top)
	}
	pulled := top
	tt.Advance(200 * time.Millisecond) // a finger at rest throws nothing
	tt.FingerUp(100, 110)
	tt.Advance(100 * time.Millisecond)
	tt.Frame()
	if top >= pulled {
		t.Errorf("as it lifts, it should spring back: %v from %v", top, pulled)
	}
	for range 200 {
		tt.Advance(16 * time.Millisecond)
	}
	tt.Frame()
	if top != 0 {
		t.Errorf("after the spring, it shows at %v", top)
	}
	// Without overscroll it stops at the end.
	overscrollKind = overscrollNone
	tt.FingerDown(100, 50)
	tt.Advance(time.Millisecond)
	tt.FingerMove(100, 60)
	tt.FingerMove(100, 110)
	tt.Frame()
	tt.Frame()
	if top != 0 {
		t.Errorf("without overscroll it shows at %v", top)
	}
	tt.FingerUp(100, 110)
}

func TestTouchLongPressSelectableContextMenu(t *testing.T) {
	chosen := false
	tt := coreNewTester(func(c *context) {
		coreColumn(c).Selectable().Children(func() {
			coreColumn(c).ContextMenu(func(m *Menu) {
				if m.Item("Copy Message").Chosen() {
					chosen = true
				}
			}).Children(func() { coreText(c, "Chat message") })
			coreText(c, "Standalone text")
		})
	}, 300, 200)
	tt.Frame()

	x, y := selectableAt(t, tt, "Chat message", 2)
	tt.FingerDown(x, y)
	tt.Advance(longPressDelay + 100*time.Millisecond)
	tt.FingerUp(x, y)
	tt.Frame()
	if err := tt.ChooseMenuItem("Copy Message"); err != nil {
		t.Fatal(err)
	}
	if !chosen {
		t.Fatal("the custom menu did not receive the chosen item")
	}

	x, y = selectableAt(t, tt, "Standalone text", 2)
	tt.FingerDown(x, y)
	tt.Advance(longPressDelay + 100*time.Millisecond)
	tt.FingerUp(x, y)
	tt.Frame()
	tt.Key(Cmd, KeyC)
	if got := tt.Clipboard(); got != "Standalone" {
		t.Fatalf("long press on text without a custom menu selected %q", got)
	}
}

// A finger held still long-presses, which clicks nothing; moved first, it
// does not.
func TestTouchLongPress(t *testing.T) {
	long, clicks := 0, 0
	tt := coreNewTester(func(c *context) {
		b := coreBox(c).Fill()
		if b.LongPressed() {
			long++
		}
		if b.Clicked() {
			clicks++
		}
	}, 200, 200)
	tt.Frame()
	tt.FingerDown(50, 50)
	tt.Advance(400 * time.Millisecond)
	if long != 0 {
		t.Fatal("too soon")
	}
	tt.Advance(200 * time.Millisecond)
	tt.Frame()
	if long != 1 {
		t.Fatalf("long presses: %d", long)
	}
	tt.FingerUp(50, 50)
	tt.Frame()
	if clicks != 0 {
		t.Errorf("a long press clicked %d times", clicks)
	}
	// A tap clicks, and long-presses nothing.
	tt.Advance(time.Second)
	tt.FingerDown(50, 50)
	tt.Advance(50 * time.Millisecond)
	tt.FingerUp(50, 50)
	tt.Advance(time.Second)
	tt.Frame()
	if long != 1 || clicks != 1 {
		t.Errorf("a tap: %d long, %d clicks", long, clicks)
	}
}

// A sideways Draggable in a list takes what goes sideways, and leaves what
// goes down to the list.
func TestTouchDraggableAxisInScroll(t *testing.T) {
	var s ScrollState
	var off Point
	tt := coreNewTester(func(c *context) {
		coreScroll(c).Fill().TrackScroll(&s).Children(func() {
			coreBox(c).Height(100).Shrink(0).Draggable(&off, DraggableOptions{Axis: Horizontal})
			coreBox(c).Height(1000).Shrink(0)
		})
	}, 200, 200)
	tt.Frame()
	// Down: the list.
	tt.FingerDown(100, 50)
	tt.Advance(time.Millisecond)
	tt.FingerMove(101, 30)
	tt.Advance(16 * time.Millisecond)
	tt.FingerMove(101, 20)
	tt.Frame()
	if off.X != 0 || s.Y != 10 {
		t.Errorf("down: dragged %v, scrolled %v", off.X, s.Y)
	}
	tt.FingerUp(101, 20)
	tt.Advance(5 * time.Second)
	s.Y = 0
	tt.Frame()
	// Across: the row.
	tt.FingerDown(100, 50)
	tt.Advance(time.Millisecond)
	tt.FingerMove(70, 51)
	tt.Frame()
	tt.Advance(16 * time.Millisecond)
	tt.FingerMove(40, 51)
	tt.Frame()
	if off.X >= 0 || s.Y != 0 {
		t.Errorf("across: dragged %v, scrolled %v", off.X, s.Y)
	}
	tt.FingerUp(40, 51)
}

// Pulling a container past its top by enough refreshes it.
func TestPullToRefresh(t *testing.T) {
	defer func(k overscroll) { overscrollKind = k }(overscrollKind)
	overscrollKind = overscrollBounce
	var r RefreshState
	fired := 0
	tt := coreNewTester(func(c *context) {
		e := coreScroll(c).Fill()
		if e.PullToRefresh(&r) {
			fired++
		}
		e.Children(func() { coreBox(c).Height(50).Shrink(0) })
	}, 200, 200)
	tt.Frame()
	tt.FingerDown(100, 20)
	tt.Advance(time.Millisecond)
	tt.FingerMove(100, 30)
	tt.Advance(16 * time.Millisecond)
	tt.FingerMove(100, 60) // not far enough
	tt.FingerUp(100, 60)
	for range 100 {
		tt.Advance(16 * time.Millisecond)
	}
	if fired != 0 || r.Refreshing {
		t.Fatalf("a short pull fired %d times", fired)
	}
	tt.FingerDown(100, 20)
	tt.Advance(time.Millisecond)
	tt.FingerMove(100, 30)
	tt.Advance(16 * time.Millisecond)
	tt.FingerMove(100, 190)
	tt.FingerUp(100, 190)
	tt.Frame()
	if fired != 1 || !r.Refreshing {
		t.Fatalf("a long pull: fired %d, refreshing %v", fired, r.Refreshing)
	}
	// It rests with the indicator showing until the app is done.
	for range 100 {
		tt.Advance(16 * time.Millisecond)
	}
	st := tt.rt.states[tt.rt.lastScrollerForTest()]
	if st == nil || st.over[1] != -refreshPull {
		t.Fatalf("while refreshing: over %+v", st)
	}
	r.Refreshing = false
	tt.Frame()
	for range 100 {
		tt.Advance(16 * time.Millisecond)
	}
	if st.over[1] != 0 {
		t.Errorf("after refreshing: over %v", st.over[1])
	}
}

// What takes clicks inside a sideways Draggable leaves what goes sideways to
// it, and takes no click for it.
func TestTouchDraggableAroundClickable(t *testing.T) {
	var off Point
	clicks := 0
	tt := coreNewTester(func(c *context) {
		coreBox(c).Fill().Draggable(&off, DraggableOptions{Axis: Horizontal}).Children(func() {
			if coreBox(c).Fill().Clicked() {
				clicks++
			}
		})
	}, 200, 200)
	tt.Frame()
	tt.FingerDown(150, 50)
	tt.Advance(time.Millisecond)
	tt.FingerMove(120, 50)
	tt.Frame()
	tt.Advance(16 * time.Millisecond)
	tt.FingerMove(80, 50)
	tt.Frame()
	tt.Advance(200 * time.Millisecond)
	tt.FingerUp(80, 50)
	tt.Frame()
	if clicks != 0 || off.X >= -30 {
		t.Errorf("%d clicks, dragged to %v", clicks, off.X)
	}
}

// A finger holding a word chooses it; the handles at its ends drag them.
func TestTouchTextHandles(t *testing.T) {
	defer func(k overscroll) { overscrollKind = k }(overscrollKind)
	overscrollKind = overscrollBounce
	v := "hello big world"
	var in *node
	tt := coreNewTester(func(c *context) {
		in = coreTextInput(c, &v).Fill().AutoFocus()
	}, 300, 60)
	tt.Frame()
	tt.Frame()
	tt.FingerDown(20, 30)
	tt.Advance(600 * time.Millisecond)
	tt.FingerUp(20, 30)
	tt.Frame()
	if got := in.st.editor.selectedText(); got != "hello" {
		t.Fatalf("held: chose %q", got)
	}
	_, _, end, sel, ok := tt.rt.handles()
	if !ok || sel.End != 5 {
		t.Fatalf("handles: %v %v", ok, sel)
	}
	// The end handle, dragged to the end of the text.
	x, y := float32(end.X), float32(end.Y+end.H)
	tt.FingerDown(x, y)
	if tt.rt.touchText.held != handleEnd {
		t.Fatalf("near the end handle, it holds %d", tt.rt.touchText.held)
	}
	tt.FingerMove(x+200, y)
	tt.Frame()
	tt.FingerUp(x+200, y)
	tt.Frame()
	if got := in.st.editor.selectedText(); got != "hello big world" {
		t.Errorf("dragged to the end: chose %q", got)
	}
}

// The keyboard coming up shows the input in use.
func TestKeyboardRevealsFocus(t *testing.T) {
	v := ""
	var s ScrollState
	tt := coreNewTester(func(c *context) {
		coreScroll(c).Fill().TrackScroll(&s).Children(func() {
			coreBox(c).Height(500).Shrink(0)
			coreTextInput(c, &v).AutoFocus()
		})
	}, 300, 600)
	tt.Frame()
	tt.Frame()
	y0 := s.Y
	tt.SetInsets(0, 0, 300, 0)
	tt.Frame()
	tt.Frame()
	if s.Y <= y0 {
		t.Errorf("under the keyboard: scrolled from %v to %v", y0, s.Y)
	}
}

// A finger leaves no hover behind: what scrolls does not keep its bars.
func TestTouchLeavesNoHover(t *testing.T) {
	tt := coreNewTester(func(c *context) {
		coreScroll(c).Fill().Children(func() { coreBox(c).Height(1000).Shrink(0) })
	}, 200, 200)
	tt.Frame()
	tt.FingerDown(100, 150)
	tt.Advance(time.Millisecond)
	tt.FingerMove(100, 130)
	tt.Advance(16 * time.Millisecond)
	tt.FingerMove(100, 100)
	tt.Advance(200 * time.Millisecond)
	tt.FingerUp(100, 100)
	tt.Advance(5 * time.Second)
	tt.Frame()
	if len(tt.rt.hover) != 0 {
		t.Errorf("hovering %v after the finger lifted", tt.rt.hover)
	}
}

// A finger from the left edge takes a Router's page back, following it; let
// go short of half the way, the page comes back.
func TestTouchEdgeSwipeBack(t *testing.T) {
	defer func(k overscroll) { overscrollKind = k }(overscrollKind)
	overscrollKind = overscrollBounce
	r := NewRouter("/a")
	var c2 *context
	tt := coreNewTester(func(c *context) {
		c2 = c
		r.coreView(c, func(rt *Route) { coreBox(c).Fill() })
	}, 400, 400)
	_ = c2
	r.Push("/a/b")
	tt.Frame()
	tt.Advance(time.Second)
	// Short of half the way: it stays.
	tt.FingerDown(10, 200)
	tt.Advance(time.Millisecond)
	tt.FingerMove(30, 200)
	tt.Frame()
	tt.Advance(16 * time.Millisecond)
	tt.FingerMove(100, 200)
	tt.Frame()
	if !r.swiping || r.swipe <= 0 {
		t.Fatalf("following: swiping %v, %v", r.swiping, r.swipe)
	}
	tt.Advance(200 * time.Millisecond)
	tt.FingerUp(100, 200)
	for range 60 {
		tt.Advance(16 * time.Millisecond)
	}
	if r.Path() != "/a/b" || r.swipe != 0 || r.settling {
		t.Fatalf("given up: at %s, swipe %v, settling %v", r.Path(), r.swipe, r.settling)
	}
	// Past it: back.
	tt.FingerDown(10, 200)
	tt.Advance(time.Millisecond)
	tt.FingerMove(30, 200)
	tt.Frame()
	tt.Advance(16 * time.Millisecond)
	tt.FingerMove(300, 200)
	tt.Frame()
	tt.Advance(200 * time.Millisecond)
	tt.FingerUp(300, 200)
	tt.Frame()
	if r.Path() != "/a" {
		t.Errorf("past half the way: at %s", r.Path())
	}
}

// Without a page to go back to, an edge swipe is any other finger.
func TestTouchEdgeSwipeNowhere(t *testing.T) {
	defer func(k overscroll) { overscrollKind = k }(overscrollKind)
	overscrollKind = overscrollBounce
	r := NewRouter("/a")
	tt := coreNewTester(func(c *context) {
		r.coreView(c, func(rt *Route) { coreBox(c).Fill() })
	}, 400, 400)
	tt.Frame()
	tt.FingerDown(10, 200)
	tt.Advance(time.Millisecond)
	tt.FingerMove(30, 200)
	tt.Frame()
	if r.swiping {
		t.Error("swiping with nowhere to go")
	}
	tt.FingerUp(30, 200)
}

// A finger nearly on a small button takes it; one on nothing does nothing, and
// one between two buttons takes the one under it.
func TestTouchReach(t *testing.T) {
	clicks := 0
	tt := coreNewTester(func(c *context) {
		coreBox(c).Fill().Children(func() {
			if coreBox(c).Absolute().Left(100).Top(100).Size(20, 20).Clicked() {
				clicks++
			}
		})
	}, 300, 300)
	tt.Frame()
	tt.FingerDown(95, 110)
	tt.FingerUp(95, 110)
	tt.Frame()
	if clicks != 1 {
		t.Errorf("nearly on it: %d clicks", clicks)
	}
	tt.Advance(time.Second)
	tt.FingerDown(50, 50)
	tt.FingerUp(50, 50)
	tt.Frame()
	if clicks != 1 {
		t.Errorf("far from it: %d clicks", clicks)
	}
}

type editMenuCall struct {
	x, y    float64
	actions []string
}

type recordingTouchSurface struct {
	menus []editMenuCall
}

func (r *recordingTouchSurface) Haptic(string)             {}
func (r *recordingTouchSurface) ShowMagnifier(x, y float64) {}
func (r *recordingTouchSurface) HideMagnifier()            {}
func (r *recordingTouchSurface) SetBackAvailable(bool)     {}
func (r *recordingTouchSurface) ShowEditMenu(x, y float64, actions []string) {
	r.menus = append(r.menus, editMenuCall{x: x, y: y, actions: slices.Clone(actions)})
}

func (r *recordingTouchSurface) open() bool {
	return len(r.menus) > 0 && len(r.menus[len(r.menus)-1].actions) > 0
}

type touchMenuTestHost struct {
	*headless
	surface *recordingTouchSurface
}

func (h *touchMenuTestHost) touchSurface() platform.TouchSurface { return h.surface }

// Tapping a KeepFocus button (such as a composer toolbar button or wrapper)
// while a text input has focus must not open the text input's edit menu.
// Tapping an already-focused text input opens the edit menu only near the
// caret, toggles it closed on the next tap, and typing or long-press behaves
// as expected.
func TestTouchEditMenuLifecycle(t *testing.T) {
	defer func(k overscroll) { overscrollKind = k }(overscrollKind)
	overscrollKind = overscrollBounce
	v := ""
	sent := 0
	var in, send *node
	tt := coreNewTester(func(c *context) {
		body := coreColumn(c).Fill().Padding(10).Gap(8)
		body.Children(func() {
			in = coreTextArea(c, &v).FillWidth().MinHeight(48).Height(48).Shrink(0)
			if in.Focused() {
				body.KeepFocus()
			}
			send = coreBox(c).Size(44, 44).Shrink(0)
			if send.Clicked() {
				sent++
				v = ""
			}
		})
	}, 360, 180)
	rec := &recordingTouchSurface{}
	tt.rt.host = &touchMenuTestHost{headless: tt.h, surface: rec}
	tt.Frame()

	sendX, sendY := send.st.x+send.st.w/2, send.st.y+send.st.h/2

	// 1. First tap focuses the empty input without opening the edit menu.
	tt.FingerDown(30, 30)
	tt.FingerUp(30, 30)
	tt.Frame()
	if !in.Focused() {
		t.Fatalf("input not focused after first tap")
	}
	if len(rec.menus) != 0 {
		t.Fatalf("first tap opened edit menu: %+v", rec.menus)
	}

	// 2. Typing text and tapping a KeepFocus button (Send) inside the KeepFocus
	// composer body must NOT pop up the Paste menu.
	in.st.textAdapter.ReplaceText(nil, "hello world")
	tt.Frame()
	tt.Advance(time.Second)
	tt.FingerDown(sendX, sendY)
	tt.FingerUp(sendX, sendY)
	tt.Frame()
	if sent != 1 || !in.Focused() || v != "" {
		t.Fatalf("send tap: sent=%d focused=%v draft=%q", sent, in.Focused(), v)
	}
	if len(rec.menus) != 0 {
		t.Fatalf("KeepFocus button tap opened edit menu: %+v", rec.menus)
	}

	// 3. Tapping the empty input far to the right of the caret must NOT pop up
	// the edit menu.
	tt.Advance(time.Second)
	tt.FingerDown(260, 30)
	tt.FingerUp(260, 30)
	tt.Frame()
	if len(rec.menus) != 0 {
		t.Fatalf("tap far from caret opened edit menu: %+v", rec.menus)
	}

	// 4. Tapping near the caret opens the edit menu ("paste").
	caret, _, ok := in.st.textAdapter.BoundsForRange(platform.TextRange{})
	if !ok {
		t.Fatalf("no caret bounds for empty input")
	}
	cx, cy := float32(caret.X+4), float32(caret.Y+caret.H/2)
	tt.Advance(time.Second)
	tt.FingerDown(cx, cy)
	tt.FingerUp(cx, cy)
	tt.Frame()
	if !rec.open() {
		t.Fatalf("tap on caret did not open edit menu: %+v", rec.menus)
	}

	// 5. Tapping the caret again while the edit menu is open dismisses it and
	// does NOT immediately re-open it.
	tt.Advance(time.Second)
	tt.FingerDown(cx, cy)
	tt.FingerUp(cx, cy)
	tt.Frame()
	if rec.open() {
		t.Fatalf("second tap on caret re-opened edit menu instead of dismissing: %+v", rec.menus)
	}

	// 6. Opening again and starting IME composition (SetMarkedText) dismisses
	// the edit menu.
	tt.Advance(time.Second)
	tt.FingerDown(cx, cy)
	tt.FingerUp(cx, cy)
	tt.Frame()
	if !rec.open() {
		t.Fatalf("expected edit menu open before IME composition: %+v", rec.menus)
	}
	in.st.textAdapter.SetMarkedText(nil, "ni", platform.TextRange{Start: 2, End: 2})
	if rec.open() {
		t.Fatalf("SetMarkedText did not dismiss edit menu: %+v", rec.menus)
	}

	// 7. Long-pressing anywhere in the empty input still opens the edit menu,
	// and tapping the KeepFocus button dismisses it without re-opening.
	in.st.textAdapter.UnmarkText()
	v = ""
	tt.Frame()
	tt.Advance(time.Second)
	tt.FingerDown(240, 30)
	tt.Advance(longPressDelay + 100*time.Millisecond)
	tt.FingerUp(240, 30)
	tt.Frame()
	if !rec.open() {
		t.Fatalf("long press in empty input did not open edit menu: %+v", rec.menus)
	}
	tt.Advance(time.Second)
	tt.FingerDown(sendX, sendY)
	tt.FingerUp(sendX, sendY)
	tt.Frame()
	if rec.open() {
		t.Fatalf("tapping KeepFocus button kept or re-opened edit menu: %+v", rec.menus)
	}

	// 8. Tapping the focused text input inside a KeepFocus container still
	// increments ShowKeyboard so the soft keyboard can be brought back up,
	// while tapping a KeepFocus sibling button does not.
	showBefore := tt.h.ime.ShowKeyboard
	tt.Advance(time.Second)
	tt.FingerDown(sendX, sendY)
	tt.FingerUp(sendX, sendY)
	tt.Frame()
	if tt.h.ime.ShowKeyboard != showBefore {
		t.Fatalf("KeepFocus button tap incremented ShowKeyboard from %d to %d", showBefore, tt.h.ime.ShowKeyboard)
	}
	tt.Advance(time.Second)
	tt.FingerDown(200, 30)
	tt.FingerUp(200, 30)
	tt.Frame()
	if tt.h.ime.ShowKeyboard != showBefore+1 {
		t.Fatalf("tapping focused text input inside KeepFocus did not increment ShowKeyboard: got %d want %d", tt.h.ime.ShowKeyboard, showBefore+1)
	}

	// 9. System keyboard dismissal (SurfaceTextInputEnded) blurs the text
	// input, hides any open edit menu, deactivates TextInputState, and preserves
	// the draft so tapping again focuses cleanly without bouncing.
	in.st.textAdapter.ReplaceText(nil, "draft kept")
	tt.Frame()
	tt.Advance(time.Second)
	tt.FingerDown(240, 30)
	tt.Advance(longPressDelay + 100*time.Millisecond)
	tt.FingerUp(240, 30)
	tt.Frame()
	if !rec.open() || !tt.h.ime.Active {
		t.Fatalf("expected active input and open edit menu before SurfaceTextInputEnded: open=%v active=%v", rec.open(), tt.h.ime.Active)
	}
	tt.rt.event(platform.SurfaceEvent{Kind: platform.SurfaceTextInputEnded})
	tt.Frame()
	if in.Focused() || tt.h.ime.Active || rec.open() || v != "draft kept" {
		t.Fatalf("after SurfaceTextInputEnded: focused=%v active=%v menuOpen=%v draft=%q", in.Focused(), tt.h.ime.Active, rec.open(), v)
	}
}

func TestTouchSelectableContainerHandlesAndHoldDrag(t *testing.T) {
	defer func(k overscroll) { overscrollKind = k }(overscrollKind)
	overscrollKind = overscrollBounce

	var scroll ScrollState
	var notifyCount int
	var lastParts []SelectedText
	tt := coreNewTester(func(c *context) {
		coreScroll(c).Fill().TrackScroll(&scroll).Children(func() {
			col := coreColumn(c).FillWidth().Padding(12).Gap(10).Selectable()
			col.OnSelectionChange(func(parts []SelectedText) {
				notifyCount++
				lastParts = slices.Clone(parts)
			})
			col.Children(func() {
				coreText(c, "Alpha beta gamma delta").FillWidth()
				coreText(c, "Second line epsilon zeta").FillWidth()
				coreText(c, "Third paragraph omega theta").FillWidth()
				for i := range 20 {
					coreText(c, "Scrollable filler line "+string(rune('A'+i%26))+" tail").FillWidth()
				}
			})
		})
	}, 320, 200)
	rec := &recordingTouchSurface{}
	tt.rt.host = &touchMenuTestHost{headless: tt.h, surface: rec}
	tt.Frame()
	tt.Frame()

	// 1. Long-press on "Alpha" selects the word, shows both handles, and opens
	// the edit menu with copy + selectAll (no cut/paste) on lift.
	x1, y1 := selectableAt(t, tt, "Alpha beta gamma delta", 2)
	tt.FingerDown(x1, y1)
	tt.Advance(longPressDelay + 50*time.Millisecond)
	tt.Frame()
	h, ok := tt.rt.selectionHandles()
	if !ok || h.kind != touchSelScope || !h.hasStart || !h.hasEnd {
		t.Fatalf("expected scope selection handles immediately after long press, ok=%v h=%+v", ok, h)
	}
	tt.FingerUp(x1, y1)
	tt.Frame()
	if !rec.open() {
		t.Fatalf("expected edit menu open after long press release: %+v", rec.menus)
	}
	gotActions := rec.menus[len(rec.menus)-1].actions
	if !slices.Contains(gotActions, "copy") || !slices.Contains(gotActions, "selectAll") || slices.Contains(gotActions, "cut") || slices.Contains(gotActions, "paste") {
		t.Fatalf("unexpected read-only edit menu actions: %v", gotActions)
	}
	tt.Key(Cmd, KeyC)
	if got := tt.Clipboard(); got != "Alpha" {
		t.Fatalf("long press selected %q, want %q", got, "Alpha")
	}

	// 2. Drag the end handle down into the second paragraph.
	h, ok = tt.rt.selectionHandles()
	if !ok || !h.hasEnd {
		t.Fatalf("missing end handle before drag")
	}
	endX, endY := h.end.X, h.end.Y+h.end.H+4
	x2, y2 := selectableAt(t, tt, "Second line epsilon zeta", 15)
	dragY2 := y2 + 12 // bottom knob position for paragraph 2
	beforeNotify := notifyCount
	tt.FingerDown(endX, endY)
	if tt.rt.touchText.held != handleEnd {
		t.Fatalf("expected handleEnd grabbed near bottom knob, got %d", tt.rt.touchText.held)
	}
	tt.FingerMove(x2, dragY2)
	tt.Frame()
	if notifyCount != beforeNotify {
		t.Fatalf("OnSelectionChange fired mid-handle-drag (%d -> %d)", beforeNotify, notifyCount)
	}
	tt.FingerUp(x2, dragY2)
	tt.Frame()
	if notifyCount <= beforeNotify || len(lastParts) < 2 {
		t.Fatalf("expected cross-paragraph OnSelectionChange on release: notify=%d parts=%+v", notifyCount, lastParts)
	}
	tt.Key(Cmd, KeyC)
	if got := tt.Clipboard(); !containsSubstr(got, "Alpha") || !containsSubstr(got, "Second line") {
		t.Fatalf("cross-paragraph handle drag selected %q", got)
	}

	// 3. Drag the start handle past the end handle (smooth crossing -> flips to handleEnd).
	h, ok = tt.rt.selectionHandles()
	if !ok || !h.hasStart {
		t.Fatalf("missing start handle before cross drag")
	}
	startX, startY := h.start.X, h.start.Y-4
	x3, y3 := selectableAt(t, tt, "Third paragraph omega theta", 18)
	tt.FingerDown(startX, startY)
	if tt.rt.touchText.held != handleStart {
		t.Fatalf("expected handleStart grabbed near top knob, got %d", tt.rt.touchText.held)
	}
	tt.FingerMove(x3, y3)
	tt.Frame()
	if tt.rt.touchText.held != handleEnd {
		t.Fatalf("expected start handle crossing past end to flip to handleEnd, got %d", tt.rt.touchText.held)
	}
	tt.FingerUp(x3, y3)
	tt.Frame()
	tt.Key(Cmd, KeyC)
	if got := tt.Clipboard(); len(got) == 0 || !containsSubstr(got, "Third") {
		t.Fatalf("crossing handle drag failed to extend into third paragraph: %q", got)
	}

	// 4. Edge auto-scroll while dragging end handle to the bottom of the scroll viewport.
	h, ok = tt.rt.selectionHandles()
	if !ok || !h.hasEnd {
		t.Fatalf("missing end handle before auto-scroll drag")
	}
	scrollBefore := scroll.Y
	tt.FingerDown(h.end.X, h.end.Y+h.end.H+4)
	tt.FingerMove(120, 192)
	for range 12 {
		tt.Advance(24 * time.Millisecond)
		tt.Frame()
	}
	tt.FingerUp(120, 192)
	tt.Frame()
	if scroll.Y <= scrollBefore+20 {
		t.Fatalf("expected edge auto-scroll during handle drag: before=%.1f after=%.1f", scrollBefore, scroll.Y)
	}
	tt.Key(Cmd, KeyC)
	if got := tt.Clipboard(); !containsSubstr(got, "Scrollable filler line") {
		t.Fatalf("expected auto-scroll handle drag to extend into scrolled paragraphs, got %q", got)
	}

	// 5. Reset scroll to top, tap to clear selection, then long-press + hold-and-drag
	// WITHOUT lifting to immediately expand selection across paragraphs.
	scroll.Y = 0
	tt.Frame()
	tt.Frame()
	tt.Advance(time.Second)
	tt.FingerDown(10, 10)
	tt.FingerUp(10, 10)
	tt.Frame()
	if _, ok := tt.rt.selectionHandles(); ok {
		t.Fatalf("expected tap outside handles to clear selection")
	}

	x1, y1 = selectableAt(t, tt, "Alpha beta gamma delta", 2)
	x2, y2 = selectableAt(t, tt, "Second line epsilon zeta", 12)
	tt.Advance(time.Second)
	tt.FingerDown(x1, y1)
	tt.Advance(longPressDelay + 50*time.Millisecond)
	tt.Frame()
	tt.FingerMove(x2, y2)
	tt.Frame()
	tt.FingerUp(x2, y2)
	tt.Frame()
	tt.Key(Cmd, KeyC)
	if got := tt.Clipboard(); !containsSubstr(got, "Alpha") || !containsSubstr(got, "Second") {
		t.Fatalf("hold-and-drag after long press failed: got %q", got)
	}

	// 6. Sub-slop thumb jitter after pressDelay followed by vertical pan must scroll
	// cleanly without leaving an accidental text selection.
	tt.Advance(time.Second)
	tt.FingerDown(10, 10)
	tt.FingerUp(10, 10)
	tt.Frame()
	tt.Advance(time.Second)
	x1, y1 = selectableAt(t, tt, "Second line epsilon zeta", 5)
	tt.FingerDown(x1, y1)
	tt.Advance(pressDelay + 20*time.Millisecond)
	tt.Frame()
	tt.FingerMove(x1+4, y1-2) // within touchSlop
	tt.Frame()
	tt.Advance(16 * time.Millisecond)
	tt.FingerMove(x1+4, y1-20) // crosses touchSlop -> starts pan
	tt.Frame()
	tt.Advance(16 * time.Millisecond)
	tt.FingerMove(x1+4, y1-80) // vertical pan
	tt.Frame()
	tt.FingerUp(x1+4, y1-80)
	tt.Frame()
	if scroll.Y <= 10 {
		t.Fatalf("expected vertical pan to scroll, got Y=%.1f", scroll.Y)
	}
	if _, _, selected := tt.rt.textSelectionBounds(); selected {
		t.Fatalf("sub-slop jitter before pan left accidental text selection")
	}
}

func TestTouchStandaloneSelectableHandles(t *testing.T) {
	defer func(k overscroll) { overscrollKind = k }(overscrollKind)
	overscrollKind = overscrollBounce

	tt := coreNewTester(func(c *context) {
		coreColumn(c).Fill().Padding(16).Children(func() {
			coreText(c, "Standalone alpha beta gamma").Selectable()
		})
	}, 320, 120)
	tt.Frame()
	tt.Frame()

	x1, y1 := selectableAt(t, tt, "Standalone alpha beta gamma", 2)
	x2, y2 := selectableAt(t, tt, "Standalone alpha beta gamma", 22)
	tt.FingerDown(x1, y1)
	tt.Advance(longPressDelay + 50*time.Millisecond)
	tt.FingerUp(x1, y1)
	tt.Frame()
	h, ok := tt.rt.selectionHandles()
	if !ok || h.kind != touchSelReadOnly || !h.hasStart || !h.hasEnd {
		t.Fatalf("expected standalone read-only selection handles, ok=%v h=%+v", ok, h)
	}
	tt.FingerDown(h.end.X, h.end.Y+h.end.H+4)
	if tt.rt.touchText.held != handleEnd {
		t.Fatalf("expected handleEnd on standalone text, got %d", tt.rt.touchText.held)
	}
	tt.FingerMove(x2, y2)
	tt.Frame()
	tt.FingerUp(x2, y2)
	tt.Frame()
	tt.Key(Cmd, KeyC)
	if got := tt.Clipboard(); !containsSubstr(got, "Standalone alpha") {
		t.Fatalf("standalone handle drag copied %q", got)
	}
}

func TestTouchDraggableStartBoundsPreservesSelectionAndScroll(t *testing.T) {
	defer func(k overscroll) { overscrollKind = k }(overscrollKind)
	overscrollKind = overscrollBounce

	var off Point
	var scroll ScrollState
	tt := coreNewTester(func(c *context) {
		host := coreBox(c).Fill()
		host.Draggable(&off, DraggableOptions{
			Axis:        Horizontal,
			Bounds:      &Rect{W: 320},
			Rigid:       true,
			StartBounds: &Rect{X: 0, Y: 0, W: 28, H: 400},
		})
		host.Children(func() {
			coreScroll(c).Fill().TrackScroll(&scroll).Children(func() {
				coreColumn(c).FillWidth().Padding(14).Gap(8).Selectable().Children(func() {
					coreText(c, "First word at left edge").FillWidth()
					coreBox(c).Height(600).Shrink(0)
				})
			})
		})
	}, 320, 240)
	tt.Frame()
	tt.Frame()

	// 1. Long-press inside the left-edge StartBounds (x=20) still selects text.
	x1, y1 := selectableAt(t, tt, "First word at left edge", 1)
	if x1 > 28 {
		x1 = 20
	}
	tt.FingerDown(x1, y1)
	tt.Advance(longPressDelay + 50*time.Millisecond)
	tt.FingerUp(x1, y1)
	tt.Frame()
	tt.Key(Cmd, KeyC)
	if got := tt.Clipboard(); got != "First" {
		t.Fatalf("long press inside StartBounds selected %q, want %q", got, "First")
	}

	// 2. Horizontal swipe starting outside StartBounds (x=100) must NOT drag host.
	tt.Advance(time.Second)
	tt.FingerDown(100, 120)
	tt.Advance(16 * time.Millisecond)
	tt.FingerMove(130, 120)
	tt.Frame()
	tt.Advance(16 * time.Millisecond)
	tt.FingerMove(220, 120)
	tt.Frame()
	tt.FingerUp(220, 120)
	tt.Frame()
	if off.X != 0 {
		t.Fatalf("horizontal swipe outside StartBounds dragged to %v", off.X)
	}

	// 3. Horizontal swipe starting inside StartBounds (x=20) drags host 1:1.
	tt.Advance(time.Second)
	tt.FingerDown(20, 120)
	tt.Advance(16 * time.Millisecond)
	tt.FingerMove(50, 120)
	tt.Frame()
	tt.Advance(16 * time.Millisecond)
	tt.FingerMove(160, 120)
	tt.Frame()
	if off.X <= 80 {
		t.Fatalf("horizontal swipe inside StartBounds failed to track 1:1, off=%+v", off)
	}
	tt.FingerUp(160, 120)
	tt.Frame()
}

// A left-edge StartBounds Draggable over non-scrollable content (or after
// pressDelay expires on scrollable content) presses the host directly on
// PointerDown. As the host translates right frame-by-frame, subsequent moves
// must stay locked to the drag instead of re-checking StartBounds against the
// shifted local coordinates and canceling the press after ~x0 DIPs.
func TestTouchDraggableStartBoundsDirectPressFrameByFrame(t *testing.T) {
	defer func(k overscroll) { overscrollKind = k }(overscrollKind)
	overscrollKind = overscrollBounce

	const w = float32(393)
	var off Point
	var scroll ScrollState
	tt := coreNewTester(func(c *context) {
		host := coreBox(c).Fill()
		host.Draggable(&off, DraggableOptions{
			Axis:        Horizontal,
			Bounds:      &Rect{W: w},
			Rigid:       true,
			StartBounds: &Rect{X: 0, Y: 44, W: 28, H: 1000},
			Snap:        Nearest(Point{}, Point{X: w}),
		})
		host.Children(func() {
			// Short non-overflowing scroll container (contentH < viewportH),
			// matching a chat with only 1-2 messages.
			coreScroll(c).Fill().TrackScroll(&scroll).Children(func() {
				coreColumn(c).FillWidth().Padding(14).Gap(8).Children(func() {
					coreBox(c).FillWidth().Height(80).Shrink(0)
				})
			})
		})
	}, int(w), 600)
	tt.Frame()
	tt.Frame()

	// 1. Frame-by-frame left-edge swipe right (starting at x=14) must track 1:1
	//    past x=14..28 all the way across the viewport and snap to w on release.
	tt.FingerDown(14, 300)
	tt.Frame()
	for _, pt := range [][2]float32{
		{24, 300},
		{36, 301},
		{52, 304},
		{80, 312},
		{140, 325},
		{220, 340},
		{260, 345},
	} {
		tt.Advance(16 * time.Millisecond)
		tt.FingerMove(pt[0], pt[1])
		tt.Frame()
	}
	if off.X < 200 {
		t.Fatalf("frame-by-frame left-edge drag snapped back or stalled: off=%+v, want X >= 200", off)
	}
	tt.FingerUp(260, 345)
	tt.Frame()
	for range 30 {
		tt.Advance(16 * time.Millisecond)
	}
	tt.Frame()
	if off.X < w*0.95 {
		t.Fatalf("left-edge drag failed to snap to %v on release: off=%+v", w, off)
	}

	// Reset and verify starting outside StartBounds (x=100) still does NOT drag.
	off = Point{}
	tt.Frame()
	tt.Advance(time.Second)
	tt.FingerDown(100, 300)
	tt.Frame()
	for _, x := range []float32{120, 160, 220, 280} {
		tt.Advance(16 * time.Millisecond)
		tt.FingerMove(x, 300)
		tt.Frame()
	}
	if off.X != 0 {
		t.Fatalf("drag outside StartBounds unexpectedly moved host: off=%+v", off)
	}
	tt.FingerUp(280, 300)
	tt.Frame()

	// 3. Starting very close to the edge (x=3) where a small pre-slop move (<8 DIPs
	//    but >= dragStart=4) translates the host before touchSlop is crossed.
	tt.Advance(time.Second)
	tt.FingerDown(3, 300)
	tt.Frame()
	for _, x := range []float32{8, 16, 40, 120, 240} {
		tt.Advance(16 * time.Millisecond)
		tt.FingerMove(x, 300)
		tt.Frame()
	}
	if off.X < 200 {
		t.Fatalf("near-edge (x=3) drag failed to track 1:1: off=%+v", off)
	}
	tt.FingerUp(240, 300)
	tt.Frame()

	// 4. Scrollable content where the finger pauses past pressDelay (>100ms)
	//    before swiping right (so pressNow presses host directly): frame-by-frame
	//    drag must still track 1:1 all the way across.
	var scrollOff Point
	var longScroll ScrollState
	ttLong := coreNewTester(func(c *context) {
		host := coreBox(c).Fill()
		host.Draggable(&scrollOff, DraggableOptions{
			Axis:        Horizontal,
			Bounds:      &Rect{W: w},
			Rigid:       true,
			StartBounds: &Rect{X: 0, Y: 44, W: 28, H: 1000},
			Snap:        Nearest(Point{}, Point{X: w}),
		})
		host.Children(func() {
			coreScroll(c).Fill().TrackScroll(&longScroll).Children(func() {
				coreBox(c).FillWidth().Height(1400).Shrink(0)
			})
		})
	}, int(w), 600)
	ttLong.Frame()
	ttLong.Frame()
	ttLong.FingerDown(14, 300)
	ttLong.Advance(pressDelay + 20*time.Millisecond)
	ttLong.Frame()
	for _, x := range []float32{24, 40, 70, 140, 240} {
		ttLong.Advance(16 * time.Millisecond)
		ttLong.FingerMove(x, 300)
		ttLong.Frame()
	}
	if scrollOff.X < 200 {
		t.Fatalf("post-pressDelay left-edge drag on scrollable content snapped back: off=%+v", scrollOff)
	}
	ttLong.FingerUp(240, 300)
	ttLong.Frame()
}

func containsSubstr(s, sub string) bool {
	if len(sub) == 0 {
		return true
	}
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
