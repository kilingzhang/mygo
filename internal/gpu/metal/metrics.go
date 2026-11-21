//go:build darwin

package metal

import (
	"time"
	"weak"

	"github.com/ebitengine/purego"
	"github.com/ebitengine/purego/objc"

	"github.com/egoist/mygo/internal/gpu"
)

// SetMetrics enables or disables per-frame Metal command buffer timing and
// presentation counters on this renderer.
func (r *Renderer) SetMetrics(enabled bool) {
	if r == nil {
		return
	}
	r.metricsEnabled.Store(enabled)
	if enabled {
		r.metricsMu.Lock()
		if r.deviceName == "" && r.device != 0 {
			r.readDeviceNameLocked()
		}
		r.metricsMu.Unlock()
	}
}

// Metrics returns a point-in-time snapshot of Metal GPU timing and presentation
// counters for this renderer.
func (r *Renderer) Metrics() gpu.MetricsSnapshot {
	if r == nil {
		return gpu.MetricsSnapshot{}
	}
	r.metricsMu.Lock()
	if r.deviceName == "" && r.device != 0 {
		r.readDeviceNameLocked()
	}
	snap := gpu.MetricsSnapshot{
		DeviceName:      r.deviceName,
		DrawableWidth:   r.w,
		DrawableHeight:  r.h,
		PresentedFrames: r.presentedFrames,
		DroppedFrames:   r.droppedFrames,
		LastGPUTime:     r.lastGPUTime,
		AvgGPUTime:      r.avgGPUTime,
		GPUFrames:       r.gpuFrames,
	}
	r.metricsMu.Unlock()
	return snap
}

func (r *Renderer) readDeviceNameLocked() {
	if r.device == 0 {
		return
	}
	pool(func() {
		nameObj := send(r.device, "name")
		if nameObj == 0 {
			return
		}
		p := send(nameObj, "UTF8String")
		if p == 0 {
			return
		}
		var b []byte
		for i := uintptr(0); ; i++ {
			c := *(*byte)(ptr(p + i))
			if c == 0 {
				r.deviceName = string(b)
				return
			}
			b = append(b, c)
		}
	})
}

func (r *Renderer) ensureCompletedBlockLocked(cb id) bool {
	if r.completedBlock != 0 {
		return r.canCompleteCB
	}
	if cb == 0 || !respondsTo(cb, "addCompletedHandler:") || !respondsTo(cb, "GPUStartTime") || !respondsTo(cb, "GPUEndTime") {
		r.canCompleteCB = false
		return false
	}
	r.selStatus = sel("status")
	r.selGPUStart = sel("GPUStartTime")
	r.selGPUEnd = sel("GPUEndTime")
	weakR := weak.Make(r)
	r.completedBlock = objc.NewBlock(func(_ objc.Block, completedCB id) {
		target := weakR.Value()
		if target == nil || completedCB == 0 || !target.metricsEnabled.Load() {
			return
		}
		if int(sendWithSEL(completedCB, target.selStatus)) == statusError {
			return
		}
		start := msgDouble(completedCB, uintptr(target.selGPUStart))
		end := msgDouble(completedCB, uintptr(target.selGPUEnd))
		if end <= start || start <= 0 {
			return
		}
		dur := time.Duration((end - start) * float64(time.Second))
		target.recordGPUTime(dur)
	})
	r.canCompleteCB = r.completedBlock != 0
	return r.canCompleteCB
}

func sendWithSEL(obj id, s objc.SEL) id {
	r, _, _ := purego.SyscallN(msgSend, obj, uintptr(s))
	return r
}

func (r *Renderer) attachMetrics(cb id) {
	if !r.metricsEnabled.Load() || cb == 0 {
		return
	}
	r.metricsMu.Lock()
	ok := r.ensureCompletedBlockLocked(cb)
	block := r.completedBlock
	if r.deviceName == "" && r.device != 0 {
		r.readDeviceNameLocked()
	}
	r.metricsMu.Unlock()
	if ok && block != 0 {
		send(cb, "addCompletedHandler:", uintptr(block))
	}
}

func (r *Renderer) recordPresent(presented bool) {
	if !r.metricsEnabled.Load() {
		return
	}
	r.metricsMu.Lock()
	if presented {
		r.presentedFrames++
	} else {
		r.droppedFrames++
	}
	r.metricsMu.Unlock()
}

func (r *Renderer) recordGPUTime(dur time.Duration) {
	r.metricsMu.Lock()
	r.lastGPUTime = dur
	r.gpuFrames++
	if r.avgGPUTime == 0 {
		r.avgGPUTime = dur
	} else {
		// Smooth exponential moving average (alpha = 0.2).
		r.avgGPUTime = (r.avgGPUTime*4 + dur) / 5
	}
	r.metricsMu.Unlock()
}

func (r *Renderer) releaseMetrics() {
	r.metricsMu.Lock()
	block := r.completedBlock
	r.completedBlock = 0
	r.canCompleteCB = false
	r.metricsMu.Unlock()
	if block != 0 {
		block.Release()
	}
}
