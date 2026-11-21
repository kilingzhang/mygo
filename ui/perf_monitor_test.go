package ui

import (
	"testing"
	"time"
)

func TestPerfMonitorTracksFramesAndDecaysIdle(t *testing.T) {
	var nowMu = time.Date(2026, 11, 21, 12, 0, 0, 0, time.UTC)
	var builds int
	var lastMetrics PerfMetrics

	tt := coreNewTester(func(c *context) {
		builds++
		pub := makeContext(c)
		pub.SetPerfMonitor(true)
		lastMetrics = pub.PerfMetrics()
		coreText(c, "hello")
	}, 400, 300)
	tt.rt.clock = func() time.Time { return nowMu }

	// Step several active frames at ~60 fps (16ms apart).
	for range 15 {
		nowMu = nowMu.Add(16 * time.Millisecond)
		tt.Frame()
	}
	if !lastMetrics.Enabled {
		t.Fatal("expected PerfMetrics.Enabled to be true")
	}
	if lastMetrics.TotalFrames < 14 {
		t.Fatalf("expected TotalFrames >= 14, got %d", lastMetrics.TotalFrames)
	}
	if lastMetrics.FPS < 55 || lastMetrics.FPS > 70 {
		t.Fatalf("expected FPS near 60, got %d", lastMetrics.FPS)
	}
	if lastMetrics.DrawableWidth != 400 || lastMetrics.DrawableHeight != 300 {
		t.Fatalf("expected drawable 400x300, got %dx%d", lastMetrics.DrawableWidth, lastMetrics.DrawableHeight)
	}

	// Simulate the one-shot idle decay trigger after >1s without active frames.
	nowMu = nowMu.Add(1200 * time.Millisecond)
	tt.rt.perf.timestamps = tt.rt.perf.timestamps[:0]
	tt.rt.perf.metrics.FPS = 0
	tt.rt.perf.metrics.FrameInterval = 0
	tt.rt.perf.idlePending.Store(true)
	totalBeforeIdle := tt.rt.perf.metrics.TotalFrames

	tt.Frame()
	if tt.rt.perf.metrics.FPS != 0 {
		t.Fatalf("expected FPS == 0 after idle decay, got %d", tt.rt.perf.metrics.FPS)
	}
	if tt.rt.perf.metrics.TotalFrames != totalBeforeIdle {
		t.Fatalf("idle decay frame should not increment TotalFrames: before=%d after=%d", totalBeforeIdle, tt.rt.perf.metrics.TotalFrames)
	}

	// Turning off monitoring clears state cleanly.
	tt.rt.public.SetPerfMonitor(false)
	if m := tt.rt.public.PerfMetrics(); m.Enabled {
		t.Fatalf("expected PerfMetrics.Enabled false after disable, got %+v", m)
	}
}
