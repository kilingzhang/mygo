package ui

import (
	"math"
	"sync/atomic"
	"time"

	"github.com/egoist/mygo/internal/gpu"
)

// PerfMetrics holds real-time rendering and GPU metrics for a window.
// Metrics are collected only while SetPerfMonitor(true) is active.
type PerfMetrics struct {
	Enabled        bool
	Backend        string        // "Metal", "Metal (Software)", or "Software"
	DeviceName     string        // Active GPU device name when available
	FPS            int           // Rolling presented/drawn frames per second (0 when idle)
	RefreshRate    float64       // Display refresh rate in Hz (0 when unknown)
	DrawableWidth  int           // Physical drawable width in pixels
	DrawableHeight int           // Physical drawable height in pixels
	GPUTime        time.Duration // Latest completed GPU command buffer duration
	AvgGPUTime     time.Duration // Smoothed average GPU command buffer duration
	CPUTime        time.Duration // Latest frame total CPU duration
	BuildTime      time.Duration // UI tree build duration
	LayoutTime     time.Duration // Layout pass duration
	PaintTime      time.Duration // Scene paint duration
	PresentTime    time.Duration // GPU/surface submit duration
	FrameInterval  time.Duration // Time between recent active frames (0 when idle)
	TotalFrames    uint64        // Active frames measured since monitoring started
	SlowFrames     uint64        // Active frames exceeding the display frame budget
	DroppedFrames  uint64        // Drawables skipped or dropped by the GPU presenter
}

type metricsRenderer interface {
	SetMetrics(enabled bool)
	Metrics() gpu.MetricsSnapshot
}

type perfHost interface {
	setGPUMetrics(enabled bool)
	perfGPU() (backend string, snap gpu.MetricsSnapshot, ok bool)
}

type perfMonitor struct {
	enabled         bool
	timestamps      []time.Time
	lastFrameAt     time.Time
	lastViewRebuild time.Time
	lastGPUFrames   uint64
	metrics         PerfMetrics

	idleTimer   *time.Timer
	idlePending atomic.Bool
}

const perfWindow = time.Second

// SetPerfMonitor enables or disables per-window rendering and GPU performance
// monitoring. Disabling stops all background decay timers and GPU completion
// callbacks.
func (c *Context) SetPerfMonitor(enabled bool) {
	rt := c.runtime()
	if rt == nil {
		return
	}
	if !enabled {
		if rt.perf != nil && rt.perf.enabled {
			rt.perf.setEnabled(rt, false)
		}
		return
	}
	if rt.perf == nil {
		rt.perf = &perfMonitor{}
	}
	if !rt.perf.enabled {
		rt.perf.setEnabled(rt, true)
	}
}

// PerfMetrics returns the latest rendering and GPU metrics snapshot for the
// window. When monitoring is disabled, Enabled is false.
func (c *Context) PerfMetrics() PerfMetrics {
	rt := c.runtime()
	if rt == nil || rt.perf == nil || !rt.perf.enabled {
		return PerfMetrics{}
	}
	rt.perf.syncGPU(rt)
	return rt.perf.metrics
}

func (p *perfMonitor) setEnabled(rt *engine, enabled bool) {
	p.enabled = enabled
	p.stopTimer()
	p.idlePending.Store(false)
	p.timestamps = p.timestamps[:0]
	p.lastFrameAt = time.Time{}
	p.lastViewRebuild = time.Time{}
	p.lastGPUFrames = 0
	p.metrics = PerfMetrics{Enabled: enabled}
	if ph, ok := rt.host.(perfHost); ok {
		ph.setGPUMetrics(enabled)
	}
	if enabled {
		p.syncGPU(rt)
	}
}

func (p *perfMonitor) stop() {
	p.enabled = false
	p.stopTimer()
	p.idlePending.Store(false)
}

func (p *perfMonitor) stopTimer() {
	if p.idleTimer != nil {
		p.idleTimer.Stop()
	}
}

func (p *perfMonitor) syncGPU(rt *engine) {
	if !p.enabled {
		return
	}
	w, h, scale := rt.size()
	if scale <= 0 {
		scale = 1
	}
	p.metrics.RefreshRate = float64(rt.host.refreshRate())
	p.metrics.DrawableWidth = int(math.Round(float64(w * scale)))
	p.metrics.DrawableHeight = int(math.Round(float64(h * scale)))
	if ph, ok := rt.host.(perfHost); ok {
		backend, snap, hasGPU := ph.perfGPU()
		p.metrics.Backend = backend
		if hasGPU {
			p.metrics.DeviceName = snap.DeviceName
			if snap.DrawableWidth > 0 && snap.DrawableHeight > 0 {
				p.metrics.DrawableWidth = snap.DrawableWidth
				p.metrics.DrawableHeight = snap.DrawableHeight
			}
			p.metrics.GPUTime = snap.LastGPUTime
			p.metrics.AvgGPUTime = snap.AvgGPUTime
			p.metrics.DroppedFrames = snap.DroppedFrames
		}
	}
	if p.metrics.Backend == "" {
		p.metrics.Backend = "Software"
	}
}

func (p *perfMonitor) record(rt *engine, buildDur, layoutDur, paintDur, presentDur, totalCPU time.Duration, isRepaint bool) {
	if p == nil || !p.enabled {
		return
	}
	now := rt.now()
	if !isRepaint {
		p.lastViewRebuild = now
	}
	// If this single frame was triggered purely by the one-shot idle decay
	// timer to reset FPS -> 0 and read the final GPU timestamp, refresh GPU
	// metadata and return without counting it as an active frame.
	if p.idlePending.Swap(false) {
		p.syncGPU(rt)
		p.metrics.FPS = 0
		p.metrics.FrameInterval = 0
		return
	}

	p.metrics.TotalFrames++
	p.metrics.BuildTime = buildDur
	p.metrics.LayoutTime = layoutDur
	p.metrics.PaintTime = paintDur
	p.metrics.PresentTime = presentDur
	p.metrics.CPUTime = totalCPU

	if !p.lastFrameAt.IsZero() {
		dt := now.Sub(p.lastFrameAt)
		if dt > 0 && dt < 500*time.Millisecond {
			p.metrics.FrameInterval = dt
		} else {
			p.metrics.FrameInterval = 0
		}
	}
	p.lastFrameAt = now

	cutoff := now.Add(-perfWindow)
	keep := 0
	for _, ts := range p.timestamps {
		if !ts.Before(cutoff) {
			p.timestamps[keep] = ts
			keep++
		}
	}
	p.timestamps = append(p.timestamps[:keep], now)
	n := len(p.timestamps)
	switch {
	case n == 0:
		p.metrics.FPS = 0
	case n >= 2:
		span := now.Sub(p.timestamps[0])
		if span >= 150*time.Millisecond {
			p.metrics.FPS = int(float64(n-1)/span.Seconds() + 0.5)
		} else {
			p.metrics.FPS = n
		}
	default:
		p.metrics.FPS = 1
	}

	prevGPUFrames := p.lastGPUFrames
	p.syncGPU(rt)
	if ph, ok := rt.host.(perfHost); ok {
		_, snap, hasGPU := ph.perfGPU()
		if hasGPU {
			p.lastGPUFrames = snap.GPUFrames
		}
	}

	hz := p.metrics.RefreshRate
	if hz <= 0 {
		hz = 60
	}
	budget := time.Duration(float64(time.Second) / hz)
	gpuSlow := p.lastGPUFrames > prevGPUFrames && p.metrics.GPUTime > budget
	if totalCPU > budget || gpuSlow {
		p.metrics.SlowFrames++
	}

	// During continuous repaint-only animations (Painter.After), periodically ask
	// for one view rebuild (~4 Hz) so the HUD overlay displays live FPS/GPU ms.
	if isRepaint && (rt.repainting || rt.animating) && now.Sub(p.lastViewRebuild) >= 250*time.Millisecond {
		rt.host.invalidate()
	}

	p.armIdleDecay(rt)
}

func (p *perfMonitor) armIdleDecay(rt *engine) {
	if p.metrics.FPS <= 0 || rt.closed || rt.closing {
		return
	}
	const idleDelay = 1100 * time.Millisecond
	if p.idleTimer == nil {
		owner := rt.owner
		p.idleTimer = time.AfterFunc(idleDelay, func() {
			e := owner.Value()
			if e == nil {
				return
			}
			e.host.post(func() {
				if e.closed || e.closing || e.perf == nil || !e.perf.enabled {
					return
				}
				if e.perf.metrics.FPS == 0 {
					return
				}
				if e.now().Sub(e.perf.lastFrameAt) < time.Second {
					return
				}
				e.perf.timestamps = e.perf.timestamps[:0]
				e.perf.metrics.FPS = 0
				e.perf.metrics.FrameInterval = 0
				e.perf.idlePending.Store(true)
				e.host.invalidate()
			})
		})
		return
	}
	p.idleTimer.Reset(idleDelay)
}
