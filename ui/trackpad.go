package ui

import (
	"math"
	"slices"
	"time"
)

const (
	// tpSpringOmega and tpBounceSpeed match iOS/macOS critically-damped
	// rubber-band springback physics.
	tpSpringOmega = 14.14
	tpBounceSpeed = 4000
	// tpBounceTimeout guards against drivers that omit ScrollPhaseEnded.
	tpBounceTimeout = 250 * time.Millisecond
)

type trackpadScroll struct {
	gestureActive   bool
	fingersDown     bool
	momentumStopped bool
	targetID        uint64
	caughtID        uint64
	momentumID      uint64
	momentumAt      time.Time
	vx, vy          float32
	absorbAxis      uint8 // bit 0 = X, bit 1 = Y
	axisLock        uint8 // 0 = undecided, 1 = X, 2 = Y, 3 = free (both)
	lockSumX        float32
	lockSumY        float32

	bouncing []uint64
	bounceAt time.Time
}

func (tp *trackpadScroll) isActive(s *state) bool {
	if s == nil {
		return false
	}
	if tp.fingersDown && (tp.targetID == s.id || tp.caughtID == s.id) {
		return true
	}
	if tp.momentumID == s.id {
		return true
	}
	if s.over[0] != 0 || s.over[1] != 0 {
		return true
	}
	return slices.Contains(tp.bouncing, s.id)
}

func (tp *trackpadScroll) clearBounce(id uint64) {
	if id == 0 {
		return
	}
	for i := 0; i < len(tp.bouncing); i++ {
		if tp.bouncing[i] == id {
			tp.bouncing = append(tp.bouncing[:i], tp.bouncing[i+1:]...)
			i--
		}
	}
}

func tpResetOver(s *state) {
	if s == nil {
		return
	}
	s.over = [2]float32{}
	s.overRaw = [2]float32{}
	s.overV = [2]float32{}
}

func tpSetOver(s *state, axis int) {
	dim := s.w
	if axis == 1 {
		dim = s.h
	}
	if dim <= 0 {
		s.over[axis] = 0
		return
	}
	s.over[axis] = Rubberband(s.overRaw[axis], 0, 0, dim)
}

func tpRawFor(s *state, axis int, over float32) float32 {
	dim := s.w
	if axis == 1 {
		dim = s.h
	}
	a := abs32(over)
	if dim <= 0 || a >= dim*0.999 {
		return over
	}
	const c = 0.55
	r := dim / c * (1/(1-a/dim) - 1)
	if over < 0 {
		return -r
	}
	return r
}

func (tp *trackpadScroll) startBounce(rt *engine, s *state, vx, vy float32) {
	if s == nil {
		return
	}
	s.overRaw = [2]float32{}
	limit := float32(tpBounceSpeed)
	s.overV = [2]float32{max(-limit, min(vx, limit)), max(-limit, min(vy, limit))}
	if rt.preferences().ReduceMotion {
		s.over, s.overV = [2]float32{}, [2]float32{}
		tp.clearBounce(s.id)
		rt.requestFrame()
		return
	}
	if s.over[0] == 0 && s.over[1] == 0 && s.overV[0] == 0 && s.overV[1] == 0 {
		return
	}
	if !slices.Contains(tp.bouncing, s.id) {
		tp.bouncing = append(tp.bouncing, s.id)
	}
	if tp.bounceAt.IsZero() {
		tp.bounceAt = rt.now()
	}
	rt.requestFrame()
	rt.animating = true
}

func (tp *trackpadScroll) catch(s *state) {
	if s == nil {
		return
	}
	for axis := range s.over {
		s.overRaw[axis] = tpRawFor(s, axis, s.over[axis])
	}
	s.overV = [2]float32{}
	tp.clearBounce(s.id)
	tp.caughtID = s.id
}

func (tp *trackpadScroll) step(rt *engine, now time.Time) {
	if len(tp.bouncing) == 0 {
		tp.bounceAt = now
		return
	}
	dt := float32(now.Sub(tp.bounceAt).Seconds())
	tp.bounceAt = now
	if dt <= 0 {
		rt.animating = true
		return
	}
	if dt > 0.05 {
		dt = 0.05
	}
	if rt.preferences().ReduceMotion {
		for _, id := range tp.bouncing {
			if s := rt.states[id]; s != nil {
				tpResetOver(s)
			}
		}
		tp.bouncing = tp.bouncing[:0]
		return
	}
	kept := tp.bouncing[:0]
	for _, id := range tp.bouncing {
		s := rt.states[id]
		if s == nil {
			continue
		}
		going := false
		for axis := range s.over {
			x := s.over[axis]
			v := s.overV[axis]
			if x == 0 && v == 0 {
				continue
			}
			const w = tpSpringOmega
			b := v + w*x
			e := float32(math.Exp(float64(-w * dt)))
			nx := (x + b*dt) * e
			nv := (b - w*(x+b*dt)) * e
			if abs32(nx) < 0.1 && abs32(nv) < 5 {
				s.over[axis], s.overV[axis] = 0, 0
				continue
			}
			s.over[axis], s.overV[axis] = nx, nv
			going = true
		}
		if going {
			kept = append(kept, id)
		}
	}
	tp.bouncing = kept
	if len(kept) > 0 {
		rt.animating = true
	}
}

// chainScrollAxes returns whether any container in chain scrolls X or Y.
func (rt *engine) chainScrollAxes(chain []uint64) (hasX, hasY bool) {
	for _, id := range chain {
		s := rt.states[id]
		if s == nil {
			continue
		}
		if s.flags&flagScrollX != 0 && s.contentW > float64(s.w)+0.5 {
			hasX = true
		}
		if s.flags&flagScrollY != 0 && s.contentH > float64(s.h)+0.5 {
			hasY = true
		}
		if hasX && hasY {
			return true, true
		}
	}
	return hasX, hasY
}

// filterScrollAxes keeps only the deltas s should consider along its enabled
// axes, skipping orthogonal single-axis containers when the opposite axis
// dominates the gesture.
func filterScrollAxes(s *state, sx, sy float32, hasAncX, hasAncY bool) (fx, fy float32, ok bool) {
	if s == nil || s.flags&(flagScrollX|flagScrollY) == 0 {
		return 0, 0, false
	}
	ax, ay := abs32(sx), abs32(sy)
	if ay > ax && s.flags&flagScrollY == 0 && (hasAncY || ay > ax*1.25) {
		return 0, 0, false
	}
	if ax > ay && s.flags&flagScrollX == 0 && (hasAncX || ax > ay*1.25) {
		return 0, 0, false
	}
	if s.flags&flagScrollX != 0 {
		fx = sx
	}
	if s.flags&flagScrollY != 0 {
		fy = sy
	}
	return fx, fy, fx != 0 || fy != 0
}

func canMoveScrollOrUnwind(s *state, fx, fy float32) bool {
	if s == nil {
		return false
	}
	mx, my := max(0, s.contentW-float64(s.w)), max(0, s.contentH-float64(s.h))
	if fy != 0 && s.flags&flagScrollY != 0 {
		if s.overRaw[1] < 0 && fy > 0 || s.overRaw[1] > 0 && fy < 0 {
			return true
		}
		if fy > 0 && s.scrollY < my-0.01 || fy < 0 && s.scrollY > 0.01 {
			return true
		}
	}
	if fx != 0 && s.flags&flagScrollX != 0 {
		if s.overRaw[0] < 0 && fx > 0 || s.overRaw[0] > 0 && fx < 0 {
			return true
		}
		dx := fx
		if s.rtl {
			dx = -dx
		}
		if dx > 0 && s.scrollX < mx-0.01 || dx < 0 && s.scrollX > 0.01 {
			return true
		}
	}
	return false
}

func canTrackpadOverscroll(s *state, fx, fy float32) bool {
	if s == nil {
		return false
	}
	if fy != 0 && s.flags&flagScrollY != 0 && s.contentH > float64(s.h)+0.5 {
		return true
	}
	if fx != 0 && s.flags&flagScrollX != 0 && s.contentW > float64(s.w)+0.5 {
		return true
	}
	return false
}

func (tp *trackpadScroll) mayBegin(rt *engine, chain []uint64) {
	tp.momentumID = 0
	tp.momentumStopped = true
	tp.absorbAxis = 0
	tp.caughtID = 0
	tp.axisLock, tp.lockSumX, tp.lockSumY = 0, 0, 0
	for _, id := range chain {
		s := rt.states[id]
		if s != nil && (s.over[0] != 0 || s.over[1] != 0 || slices.Contains(tp.bouncing, id)) {
			tp.catch(s)
			return
		}
	}
}

func (tp *trackpadScroll) pickTarget(rt *engine, chain []uint64, dx, dy float32, sideways bool) uint64 {
	hasX, hasY := rt.chainScrollAxes(chain)
	if tp.caughtID != 0 && slices.Contains(chain, tp.caughtID) {
		if s := rt.states[tp.caughtID]; s != nil {
			sx, sy := s.vector(rt.pointerX, rt.pointerY, dx, dy)
			if sideways && s.rtl {
				sx = -sx
			}
			if _, _, ok := filterScrollAxes(s, sx, sy, hasX, hasY); ok {
				return tp.caughtID
			}
		}
	}
	// Pass 1: innermost container that has remaining travel along the gesture.
	for _, id := range chain {
		s := rt.states[id]
		if s == nil {
			continue
		}
		sx, sy := s.vector(rt.pointerX, rt.pointerY, dx, dy)
		if sideways && s.rtl {
			sx = -sx
		}
		fx, fy, ok := filterScrollAxes(s, sx, sy, hasX, hasY)
		if ok && canMoveScrollOrUnwind(s, fx, fy) {
			return id
		}
	}
	// Pass 2: all matching scrollers are at an edge; pick the outermost
	// overflowing container so pulling at a limit bounces the main view.
	for i := len(chain) - 1; i >= 0; i-- {
		id := chain[i]
		s := rt.states[id]
		if s == nil {
			continue
		}
		sx, sy := s.vector(rt.pointerX, rt.pointerY, dx, dy)
		if sideways && s.rtl {
			sx = -sx
		}
		fx, fy, ok := filterScrollAxes(s, sx, sy, hasX, hasY)
		if ok && canTrackpadOverscroll(s, fx, fy) {
			return id
		}
	}
	return 0
}

func tpPanAxis(pos *float64, maxPos float64, scrollDelta, screenDelta float32, overRaw *float32, allowOver bool) (movedPos, changedOver bool) {
	if scrollDelta == 0 && screenDelta == 0 {
		return false, false
	}
	remScreen := float64(screenDelta)
	if *overRaw < 0 && remScreen > 0 {
		unwind := min(float64(-*overRaw), remScreen)
		*overRaw += float32(unwind)
		if *overRaw > -0.01 {
			*overRaw = 0
		}
		remScreen -= unwind
		changedOver = true
	} else if *overRaw > 0 && remScreen < 0 {
		unwind := min(float64(*overRaw), -remScreen)
		*overRaw -= float32(unwind)
		if *overRaw < 0.01 {
			*overRaw = 0
		}
		remScreen += unwind
		changedOver = true
	}
	if *overRaw != 0 {
		if allowOver && remScreen != 0 {
			*overRaw += float32(remScreen)
			changedOver = true
		}
		return false, changedOver
	}
	remPos := remScreen
	if (scrollDelta < 0) != (screenDelta < 0) {
		remPos = -remScreen
	}
	cur := *pos
	next := max(0, min(cur+remPos, maxPos))
	if next != cur {
		*pos = next
		movedPos = true
	}
	excessPos := (cur + remPos) - next
	if allowOver && excessPos != 0 {
		excessScreen := excessPos
		if (scrollDelta < 0) != (screenDelta < 0) {
			excessScreen = -excessScreen
		}
		*overRaw += float32(excessScreen)
		changedOver = true
	}
	return movedPos, changedOver
}

func (tp *trackpadScroll) applyFingerPan(rt *engine, s *state, fx, fy float32) bool {
	s.wheel = nil
	if s.track != nil {
		s.track.glide = nil
	}
	s.haltSnap()
	tp.clearBounce(s.id)
	s.overV = [2]float32{}

	allowOver := !rt.preferences().ReduceMotion
	mx, my := max(0, s.contentW-float64(s.w)), max(0, s.contentH-float64(s.h))
	movedX, overX := false, false
	if s.flags&flagScrollX != 0 && (fx != 0 || s.overRaw[0] != 0) {
		sxDelta := fx
		if s.rtl {
			sxDelta = -sxDelta
		}
		movedX, overX = tpPanAxis(&s.scrollX, mx, sxDelta, fx, &s.overRaw[0], allowOver && s.contentW > float64(s.w)+0.5)
		if overX {
			tpSetOver(s, 0)
		}
	}
	movedY, overY := false, false
	if s.flags&flagScrollY != 0 && (fy != 0 || s.overRaw[1] != 0) {
		movedY, overY = tpPanAxis(&s.scrollY, my, fy, fy, &s.overRaw[1], allowOver && s.contentH > float64(s.h)+0.5)
		if overY {
			tpSetOver(s, 1)
		}
	}
	if movedX || movedY {
		s.scrollTo(s.scrollX, s.scrollY)
	}
	if movedX || movedY || overX || overY {
		if s.snap != nil {
			s.snap.scrolled(rt, s, snapSettle)
		}
		rt.requestFrame()
		return true
	}
	return false
}

func (tp *trackpadScroll) applyMomentum(rt *engine, s *state, fx, fy float32, now time.Time) bool {
	if s == nil {
		return false
	}
	dt := float32(now.Sub(tp.momentumAt).Seconds())
	tp.momentumAt = now
	if dt <= 0 || dt > 0.05 {
		// Use a conservative 60Hz frame interval when timestamps coincide
		// (e.g., first momentum event right after ScrollPhaseEnded) so boundary
		// bounce velocity isn't doubled on 60Hz displays.
		dt = 1.0 / 60.0
	} else if dt < 1.0/240.0 {
		dt = 1.0 / 240.0
	}
	if tp.absorbAxis&(1<<0) != 0 {
		fx = 0
	}
	if tp.absorbAxis&(1<<1) != 0 {
		fy = 0
	}
	if fx == 0 && fy == 0 {
		return false
	}
	// Track smoothed velocity in DIPs/s for boundary impact bounce.
	instVX, instVY := fx/dt, fy/dt
	if tp.vx == 0 {
		tp.vx = instVX
	} else {
		tp.vx = tp.vx*0.5 + instVX*0.5
	}
	if tp.vy == 0 {
		tp.vy = instVY
	} else {
		tp.vy = tp.vy*0.5 + instVY*0.5
	}

	s.wheel = nil
	mx, my := max(0, s.contentW-float64(s.w)), max(0, s.contentH-float64(s.h))
	curX, curY := s.scrollX, s.scrollY
	var hitX, hitY float32
	if fx != 0 && s.flags&flagScrollX != 0 {
		dx := float64(fx)
		if s.rtl {
			dx = -dx
		}
		nextX := max(0, min(curX+dx, mx))
		if nextX != curX+dx && s.contentW > float64(s.w)+0.5 {
			hitX = max(-2200, min(tp.vx, 2200))
			tp.absorbAxis |= 1 << 0
		}
		curX = nextX
	}
	if fy != 0 && s.flags&flagScrollY != 0 {
		dy := float64(fy)
		nextY := max(0, min(curY+dy, my))
		if nextY != curY+dy && s.contentH > float64(s.h)+0.5 {
			hitY = max(-2200, min(tp.vy, 2200))
			tp.absorbAxis |= 1 << 1
		}
		curY = nextY
	}
	moved := curX != s.scrollX || curY != s.scrollY
	if moved {
		if s.track != nil {
			s.track.glide = nil
		}
		s.haltSnap()
		s.scrollTo(curX, curY)
		if s.snap != nil {
			s.snap.scrolled(rt, s, snapSettle)
		}
		rt.requestFrame()
	}
	if (hitX != 0 || hitY != 0) && !rt.preferences().ReduceMotion {
		tp.startBounce(rt, s, hitX, hitY)
		return true
	}
	return moved
}

// lockAxes applies gentle dominant-axis hysteresis on two-axis (ScrollBoth)
// containers so cleanly vertical or horizontal trackpad swipes do not wobble
// sideways, matching macOS NSScrollView directional locking.
func (tp *trackpadScroll) lockAxes(s *state, sx, sy float32) (fx, fy float32) {
	hasX := s.flags&flagScrollX != 0
	hasY := s.flags&flagScrollY != 0
	if hasX {
		fx = sx
	}
	if hasY {
		fy = sy
	}
	if !hasX || !hasY {
		return fx, fy
	}
	switch tp.axisLock {
	case 1:
		return fx, 0
	case 2:
		return 0, fy
	case 3:
		return fx, fy
	}
	tp.lockSumX += abs32(fx)
	tp.lockSumY += abs32(fy)
	if tp.lockSumX+tp.lockSumY >= 6 {
		switch {
		case tp.lockSumY > tp.lockSumX*2.0:
			tp.axisLock = 2
			return 0, fy
		case tp.lockSumX > tp.lockSumY*2.0:
			tp.axisLock = 1
			return fx, 0
		default:
			tp.axisLock = 3
		}
	}
	return fx, fy
}

// handle attempts to process phased touchpad and latched momentum scroll events.
// It returns handled=true when the phased gesture machinery owned the event.
func (tp *trackpadScroll) handle(rt *engine, chain []uint64, dx, dy float32, sideways, precise, momentum bool, phase, momentumPhase ScrollPhase) bool {
	if !precise || (phase == ScrollPhaseNone && momentumPhase == ScrollPhaseNone && !(momentum && tp.momentumID != 0)) {
		return false
	}
	now := rt.now()
	if momentum || momentumPhase != ScrollPhaseNone {
		switch momentumPhase {
		case ScrollPhaseEnded, ScrollPhaseCanceled:
			tp.momentumID = 0
			tp.momentumStopped = false
			tp.absorbAxis = 0
			tp.vx, tp.vy = 0, 0
			return true
		case ScrollPhaseBegan:
			if tp.momentumStopped {
				return true
			}
			tp.momentumAt = now
			tp.vx, tp.vy = 0, 0
		}
		if tp.momentumStopped {
			return true
		}
		if tp.momentumID == 0 {
			tp.momentumID = tp.pickTarget(rt, chain, dx, dy, sideways)
			tp.momentumAt = now
		}
		if s := rt.states[tp.momentumID]; s != nil {
			sx, sy := s.vector(rt.pointerX, rt.pointerY, dx, dy)
			if sideways && s.rtl {
				sx = -sx
			}
			fx, fy := tp.lockAxes(s, sx, sy)
			tp.applyMomentum(rt, s, fx, fy, now)
		}
		return true
	}

	switch phase {
	case ScrollPhaseMayBegin:
		tp.mayBegin(rt, chain)
		return true
	case ScrollPhaseBegan:
		tp.momentumID = 0
		tp.momentumStopped = true
		tp.absorbAxis = 0
		tp.vx, tp.vy = 0, 0
		tp.axisLock, tp.lockSumX, tp.lockSumY = 0, 0, 0
		tp.gestureActive = true
		tp.fingersDown = true
		tp.targetID = 0
		if s := rt.states[tp.caughtID]; s == nil || !slices.Contains(chain, tp.caughtID) {
			tp.mayBegin(rt, chain)
		}
		if dx == 0 && dy == 0 {
			return true
		}
	case ScrollPhaseChanged:
		if !tp.gestureActive {
			tp.momentumID = 0
			tp.momentumStopped = true
			tp.absorbAxis = 0
			tp.axisLock, tp.lockSumX, tp.lockSumY = 0, 0, 0
			tp.gestureActive = true
			tp.fingersDown = true
			tp.targetID = 0
		}
		if dx == 0 && dy == 0 {
			return true
		}
	case ScrollPhaseEnded, ScrollPhaseCanceled:
		tp.fingersDown = false
		tp.gestureActive = false
		tp.momentumStopped = false
		target := rt.states[tp.targetID]
		if target == nil && tp.caughtID != 0 {
			target = rt.states[tp.caughtID]
		}
		if target != nil && (dx != 0 || dy != 0) {
			sx, sy := target.vector(rt.pointerX, rt.pointerY, dx, dy)
			if sideways && target.rtl {
				sx = -sx
			}
			fx, fy := tp.lockAxes(target, sx, sy)
			tp.applyFingerPan(rt, target, fx, fy)
		}
		if target != nil {
			tp.momentumID = target.id
			tp.momentumAt = now
			tp.vx, tp.vy = 0, 0
			if target.over[0] != 0 || target.over[1] != 0 {
				if target.over[0] != 0 {
					tp.absorbAxis |= 1 << 0
				}
				if target.over[1] != 0 {
					tp.absorbAxis |= 1 << 1
				}
				tp.startBounce(rt, target, 0, 0)
			}
		} else {
			tp.momentumID = 0
		}
		tp.targetID = 0
		tp.caughtID = 0
		return true
	}

	if tp.targetID == 0 {
		tp.targetID = tp.pickTarget(rt, chain, dx, dy, sideways)
	}
	if s := rt.states[tp.targetID]; s != nil {
		sx, sy := s.vector(rt.pointerX, rt.pointerY, dx, dy)
		if sideways && s.rtl {
			sx = -sx
		}
		fx, fy := tp.lockAxes(s, sx, sy)
		tp.applyFingerPan(rt, s, fx, fy)
	}
	return true
}
