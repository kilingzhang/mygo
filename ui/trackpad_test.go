package ui

import (
	"testing"
	"time"

	"github.com/egoist/mygo/internal/platform"
)

func sendPhasedScroll(t *Tester, x, y, dx, dy float64, phase, momentumPhase platform.ScrollPhase) {
	t.send(platform.SurfaceEvent{
		Kind:          platform.PointerScroll,
		X:             x,
		Y:             y,
		DX:            dx,
		DY:            dy,
		Precise:       true,
		Momentum:      momentumPhase != platform.ScrollPhaseNone,
		Phase:         phase,
		MomentumPhase: momentumPhase,
	})
}

// A phased vertical trackpad swipe stays latched to the container where it
// began even when an inner scroll container moves under the stationary pointer,
// and keeps the ensuing momentum coast on that same container.
func TestTrackpadScrollGestureLatch(t *testing.T) {
	var outer, inner ScrollState
	tt := coreNewTester(func(c *context) {
		coreScroll(c).Fill().TrackScroll(&outer).Children(func() {
			coreBox(c).Height(120).Shrink(0)
			coreScroll(c).Height(120).Shrink(0).TrackScroll(&inner).Children(func() {
				coreBox(c).Height(500).Shrink(0)
			})
			coreBox(c).Height(1200).Shrink(0)
		})
	}, 300, 300)

	// Begin swipe at y=40 (over the top padding box, outside inner scroll).
	tt.Move(150, 40)
	sendPhasedScroll(tt, 150, 40, 0, 0, platform.ScrollPhaseBegan, platform.ScrollPhaseNone)
	sendPhasedScroll(tt, 150, 40, 0, 140, platform.ScrollPhaseChanged, platform.ScrollPhaseNone)
	tt.Frame()
	if outer.Y != 140 || inner.Y != 0 {
		t.Fatalf("after first delta: outer.Y=%v inner.Y=%v, want 140, 0", outer.Y, inner.Y)
	}
	if !outer.Active() {
		t.Fatal("expected outer.Active() true while trackpad fingers remain down")
	}

	// Inner scroll now sits at [120-140=-20 .. 100], covering pointer y=40!
	// A continued ScrollPhaseChanged and subsequent Momentum MUST stay latched to outer.
	sendPhasedScroll(tt, 150, 40, 0, 60, platform.ScrollPhaseChanged, platform.ScrollPhaseNone)
	tt.Frame()
	if outer.Y != 200 || inner.Y != 0 {
		t.Fatalf("inner stole active gesture: outer.Y=%v inner.Y=%v, want 200, 0", outer.Y, inner.Y)
	}
	sendPhasedScroll(tt, 150, 40, 0, 0, platform.ScrollPhaseEnded, platform.ScrollPhaseNone)
	sendPhasedScroll(tt, 150, 40, 0, 45, platform.ScrollPhaseNone, platform.ScrollPhaseBegan)
	tt.Frame()
	if outer.Y != 245 || inner.Y != 0 {
		t.Fatalf("inner stole momentum gesture: outer.Y=%v inner.Y=%v, want 245, 0", outer.Y, inner.Y)
	}
	sendPhasedScroll(tt, 150, 40, 0, 0, platform.ScrollPhaseNone, platform.ScrollPhaseEnded)
	if outer.Active() {
		t.Fatal("expected outer.Active() false after momentum ended")
	}
}

// Swiping primarily vertically with tiny horizontal jitter over a wide
// ScrollHorizontal child scrolls the outer vertical timeline cleanly without
// letting horizontal jitter consume and discard the vertical delta.
func TestTrackpadPrimaryAxisSkipsHorizontalJitter(t *testing.T) {
	var vert, horiz ScrollState
	tt := coreNewTester(func(c *context) {
		coreScroll(c).Fill().TrackScroll(&vert).Children(func() {
			coreScrollHorizontal(c).Height(120).Shrink(0).TrackScroll(&horiz).Children(func() {
				coreBox(c).Width(1000).Height(120).Shrink(0)
			})
			coreBox(c).Height(1000).Shrink(0)
		})
	}, 300, 300)

	tt.Move(150, 60)
	// First check unphased diagonal-vertical scroll over the horizontal scroller.
	tt.Scroll(150, 60, -2, 30)
	tt.Frame()
	if vert.Y != 30 || horiz.X != 0 {
		t.Fatalf("unphased vertical scroll with dx jitter: vert.Y=%v horiz.X=%v, want 30, 0", vert.Y, horiz.X)
	}

	// Next check phased trackpad diagonal-vertical swipe over horizontal scroller.
	sendPhasedScroll(tt, 150, 60, -1.5, 25, platform.ScrollPhaseBegan, platform.ScrollPhaseNone)
	sendPhasedScroll(tt, 150, 60, 0, 0, platform.ScrollPhaseEnded, platform.ScrollPhaseNone)
	sendPhasedScroll(tt, 150, 60, 0, 0, platform.ScrollPhaseNone, platform.ScrollPhaseEnded)
	tt.Frame()
	if vert.Y != 55 || horiz.X != 0 {
		t.Fatalf("phased vertical scroll with dx jitter: vert.Y=%v horiz.X=%v, want 55, 0", vert.Y, horiz.X)
	}

	// Deliberate horizontal scroll still moves the horizontal scroller.
	tt.Scroll(150, 60, 40, 1.5)
	tt.Frame()
	if horiz.X != 40 || vert.Y != 55 {
		t.Fatalf("horizontal scroll over ScrollHorizontal: horiz.X=%v vert.Y=%v, want 40, 55", horiz.X, vert.Y)
	}
}

// Phased trackpad pull past top/bottom rubber-bands with progressive resistance,
// unwinds smoothly on reversal, springs back on release, catches mid-bounce on
// MayBegin, and stays strictly clamped when ReduceMotion is enabled.
func TestTrackpadRubberBandAndReduceMotion(t *testing.T) {
	var s ScrollState
	var contentY float32
	tt := coreNewTester(func(c *context) {
		coreScroll(c).Fill().TrackScroll(&s).Children(func() {
			item := coreBox(c).Height(800).Shrink(0)
			contentY = item.Bounds().Y
		})
	}, 200, 300)
	st := func() *state { return tt.rt.states[tt.rt.c.root.first.id] }

	tt.Move(100, 100)
	sendPhasedScroll(tt, 100, 100, 0, -60, platform.ScrollPhaseBegan, platform.ScrollPhaseNone)
	tt.Frame()
	overDown := st().over[1]
	if overDown >= 0 || contentY <= 0 {
		t.Fatalf("pull past top should rubber-band down: over=%v contentY=%v", overDown, contentY)
	}
	if !s.Active() {
		t.Fatal("expected s.Active() true while overscrolled")
	}

	// Unwind half way and ensure smooth recovery.
	sendPhasedScroll(tt, 100, 100, 0, 30, platform.ScrollPhaseChanged, platform.ScrollPhaseNone)
	tt.Frame()
	if st().over[1] <= overDown || st().over[1] >= 0 {
		t.Fatalf("expected partial unwind: got %v, prev %v", st().over[1], overDown)
	}

	// Release: spring starts moving back toward 0.
	sendPhasedScroll(tt, 100, 100, 0, 0, platform.ScrollPhaseEnded, platform.ScrollPhaseNone)
	tt.Advance(40 * time.Millisecond)
	midBounce := st().over[1]
	if midBounce <= overDown || midBounce >= 0 {
		t.Fatalf("expected active springback between %v and 0, got %v", overDown, midBounce)
	}

	// MayBegin catches the bounce in place under fingers.
	sendPhasedScroll(tt, 100, 100, 0, 0, platform.ScrollPhaseMayBegin, platform.ScrollPhaseNone)
	tt.Advance(60 * time.Millisecond)
	if caught := st().over[1]; caught != midBounce {
		t.Fatalf("MayBegin should catch bounce at %v, drifted to %v", midBounce, caught)
	}

	// Complete release settles all the way back to 0.
	sendPhasedScroll(tt, 100, 100, 0, 0, platform.ScrollPhaseBegan, platform.ScrollPhaseNone)
	sendPhasedScroll(tt, 100, 100, 0, 0, platform.ScrollPhaseEnded, platform.ScrollPhaseNone)
	tt.Advance(time.Second)
	tt.Frame()
	if st().over[1] != 0 || contentY != 0 {
		t.Fatalf("expected settled over=0 contentY=0, got %v %v", st().over[1], contentY)
	}

	// ReduceMotion suppresses overscroll cleanly.
	tt.SetPreferences(Preferences{ReduceMotion: true, TextScale: 1})
	sendPhasedScroll(tt, 100, 100, 0, -60, platform.ScrollPhaseBegan, platform.ScrollPhaseNone)
	sendPhasedScroll(tt, 100, 100, 0, 0, platform.ScrollPhaseEnded, platform.ScrollPhaseNone)
	tt.Frame()
	if st().over[1] != 0 || contentY != 0 {
		t.Fatalf("expected ReduceMotion to keep over=0, got %v contentY=%v", st().over[1], contentY)
	}
}

// Context.CanScrollUnderPointer reports horizontal scroll capability accurately
// when a horizontal scroller sits inside a vertical scroll view.
func TestContextCanScrollUnderPointer(t *testing.T) {
	var horiz ScrollState
	var ctx *Context
	tt := coreNewTester(func(c *context) {
		ctx = c.rt.public
		coreScroll(c).Fill().Children(func() {
			coreScrollHorizontal(c).Height(100).Shrink(0).TrackScroll(&horiz).Children(func() {
				coreBox(c).Width(800).Height(100).Shrink(0)
			})
			coreBox(c).Height(1000).Shrink(0)
		})
	}, 300, 300)

	tt.Move(100, 50)
	if !ctx.CanScrollUnderPointer(20, 0) {
		t.Fatal("expected CanScrollUnderPointer(20, 0) true at left start of wide ScrollHorizontal")
	}
	if ctx.CanScrollUnderPointer(-20, 0) {
		t.Fatal("expected CanScrollUnderPointer(-20, 0) false at left start (x=0)")
	}
	if ctx.CanScrollUnderPointer(20, 0) && false {
		t.Fatal("unreachable")
	}
	horiz.X = 100
	tt.Frame()
	if !ctx.CanScrollUnderPointer(-20, 0) {
		t.Fatal("expected CanScrollUnderPointer(-20, 0) true once scrolled right to x=100")
	}
	tt.Move(100, 200)
	if ctx.CanScrollUnderPointer(20, 0) {
		t.Fatal("expected CanScrollUnderPointer(20, 0) false outside ScrollHorizontal")
	}
}

// Scrolled paragraphs with inline spans (links / code spans) reuse layout and
// skip painting when scrolled out of view.
func TestInlineParagraphLayoutReuseAndPaintCull(t *testing.T) {
	var scroll ScrollState
	tt := coreNewTester(func(c *context) {
		coreScroll(c).Fill().TrackScroll(&scroll).Children(func() {
			for i := range 60 {
				p := coreText(c, "Message "+itoa(i)+" with ")
				p.Children(func() {
					link := coreText(c, "inline_link.go").TextColor(RGB(0, 100, 220)).Role(RoleLink).Focusable()
					link.flags |= flagClickable
				})
			}
		})
	}, 300, 240)

	tt.Frame()
	beforeCull := tt.rt.culled
	scroll.Y = 300
	tt.Frame()
	if tt.rt.culled <= beforeCull {
		t.Fatalf("expected offscreen paragraphs with inline links to be paint-culled, culled=%d->%d", beforeCull, tt.rt.culled)
	}
	rootCol := tt.rt.c.root.first
	para := rootCol.first
	if para.lay&layReuse == 0 {
		t.Fatalf("expected paragraph with unchanged inline child to reuse layout (layReuse bit), got lay=%v", para.lay)
	}
}

func TestSystemScrollPreferencesAlwaysShowClickJumpsAndDisableAnim(t *testing.T) {
	var s ScrollState
	var hidden ScrollState
	var st, hiddenSt *state
	tt := coreNewTester(func(c *context) {
		coreRow(c).Fill().Children(func() {
			sc := coreScroll(c).Size(150, 200).TrackScroll(&s)
			st = sc.st
			sc.Children(func() { coreBox(c).Size(150, 2000).Shrink(0) })

			hs := coreScroll(c).Size(150, 200).Scrollbars(false).TrackScroll(&hidden)
			hiddenSt = hs.st
			hs.Children(func() { coreBox(c).Size(150, 2000).Shrink(0) })
		})
	}, 320, 220)
	defer tt.rt.close()

	// Move pointer outside both scroll containers.
	tt.Move(310, 210)
	tt.Frame()
	if st.barsShown {
		t.Fatal("default overlay policy should hide scrollbars when pointer is away")
	}

	// System preference AlwaysShowScrollbars keeps default scrollbars visible
	// while still honoring explicit Scrollbars(false).
	tt.SetPreferences(Preferences{AlwaysShowScrollbars: true, TextScale: 1})
	tt.Frame()
	if !st.barsShown {
		t.Fatal("AlwaysShowScrollbars=true should show scrollbars even when pointer is away")
	}
	if hiddenSt.barsShown {
		t.Fatal("explicit Scrollbars(false) must stay hidden even when AlwaysShowScrollbars=true")
	}

	// Track click paging (ScrollbarClickJumps=false) moves by ~90% of viewport height.
	trackX := float32(144)
	tt.Press(trackX, 170)
	tt.Release(trackX, 170)
	tt.Frame()
	if s.Y < 160 || s.Y > 200 {
		t.Fatalf("page-jump track click scrolled to %v, want ~180", s.Y)
	}

	// ScrollbarClickJumps=true jumps directly near the bottom when clicking at y=170.
	s.Y = 0
	tt.SetPreferences(Preferences{AlwaysShowScrollbars: true, ScrollbarClickJumps: true, TextScale: 1})
	tt.Frame()
	tt.Press(trackX, 170)
	tt.Release(trackX, 170)
	tt.Frame()
	if s.Y < 1200 {
		t.Fatalf("ScrollbarClickJumps=true track click at y=170 scrolled only to %v, want >= 1200", s.Y)
	}

	// DisableScrollAnimation=true makes mouse wheel ticks scroll immediately without glide.
	s.Y = 0
	tt.SetPreferences(Preferences{DisableScrollAnimation: true, TextScale: 1})
	tt.Move(50, 100)
	tt.Wheel(50, 100, 0, 80)
	if s.Y != 80 {
		t.Fatalf("DisableScrollAnimation=true should scroll wheel immediately to 80, got %v", s.Y)
	}
}

func TestTrackpadMayBeginHaltsTrailingMomentumAndScrollBothAxisLock(t *testing.T) {
	var both ScrollState
	tt := coreNewTester(func(c *context) {
		coreScrollBoth(c).Fill().TrackScroll(&both).Children(func() {
			coreBox(c).Size(1200, 1200).Shrink(0)
		})
	}, 300, 300)
	defer tt.rt.close()

	// Dominant vertical trackpad gesture on ScrollBoth locks out tiny horizontal jitter.
	tt.Move(150, 150)
	sendPhasedScroll(tt, 150, 150, 0.5, 10, platform.ScrollPhaseBegan, platform.ScrollPhaseNone)
	sendPhasedScroll(tt, 150, 150, 1.0, 20, platform.ScrollPhaseChanged, platform.ScrollPhaseNone)
	sendPhasedScroll(tt, 150, 150, 1.5, 25, platform.ScrollPhaseChanged, platform.ScrollPhaseNone)
	tt.Frame()
	if both.Y < 50 || both.X > 2 {
		t.Fatalf("vertical gesture on ScrollBoth should lock Y and suppress X jitter: X=%v Y=%v", both.X, both.Y)
	}
	sendPhasedScroll(tt, 150, 150, 0, 0, platform.ScrollPhaseEnded, platform.ScrollPhaseNone)
	sendPhasedScroll(tt, 150, 150, 0, 30, platform.ScrollPhaseNone, platform.ScrollPhaseBegan)
	tt.Frame()
	yBeforeStop := both.Y
	// Touching fingers down (MayBegin) halts coasting; trailing momentum must not resume it.
	sendPhasedScroll(tt, 150, 150, 0, 0, platform.ScrollPhaseMayBegin, platform.ScrollPhaseNone)
	sendPhasedScroll(tt, 150, 150, 0, 40, platform.ScrollPhaseNone, platform.ScrollPhaseChanged)
	tt.Frame()
	if both.Y != yBeforeStop {
		t.Fatalf("trailing momentum after MayBegin moved Y from %v to %v", yBeforeStop, both.Y)
	}
	sendPhasedScroll(tt, 150, 150, 0, 0, platform.ScrollPhaseNone, platform.ScrollPhaseEnded)
}

func TestInputScrollDirectionPropagation(t *testing.T) {
	var got []ScrollDirection
	tt := coreNewTester(func(c *context) {
		coreBox(c).Fill().HandleInput(func(ev InputEvent) bool {
			if ev.Kind == InputScroll {
				got = append(got, ev.ScrollDirection)
				return true
			}
			return false
		})
	}, 200, 200)
	defer tt.rt.close()

	tt.Move(100, 100)
	tt.ScrollPhaseDirection(100, 100, 20, 0, ScrollPhaseBegan, ScrollPhaseNone, ScrollDirectionTraditional)
	tt.ScrollPhaseDirection(100, 100, -20, 0, ScrollPhaseChanged, ScrollPhaseNone, ScrollDirectionNatural)
	if len(got) != 2 || got[0] != ScrollDirectionTraditional || got[1] != ScrollDirectionNatural {
		t.Fatalf("got ScrollDirections=%v, want [Traditional Natural]", got)
	}
}
