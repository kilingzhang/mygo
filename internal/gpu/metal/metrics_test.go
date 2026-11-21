//go:build darwin && !ios

package metal

import (
	"testing"
	"time"

	"github.com/egoist/mygo/internal/scene"
)

func TestMetalRendererMetrics(t *testing.T) {
	if err := load(); err != nil {
		t.Skip("no Metal:", err)
	}
	var layer id
	pool(func() { layer = send(class("CALayer"), "new") })
	if layer == 0 {
		t.Skip("no Core Animation")
	}
	defer pool(func() { release(&layer) })

	r, err := New(uintptr(layer))
	if err != nil {
		unavailable(t, err)
	}
	defer r.Release()

	r.SetMetrics(true)
	snap := r.Metrics()
	if snap.DeviceName == "" {
		t.Fatal("expected non-empty Metal device name")
	}

	s := &scene.Scene{
		Width:  320,
		Height: 200,
		Scale:  2,
		Clear:  scene.Color{A: 255},
	}
	if err := r.Render(s); err != nil {
		t.Fatalf("Render: %v", err)
	}
	pool(r.waitLast)

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		snap = r.Metrics()
		if snap.PresentedFrames >= 1 && snap.GPUFrames >= 1 && snap.LastGPUTime > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if snap.PresentedFrames < 1 {
		t.Fatalf("expected PresentedFrames >= 1, got %+v", snap)
	}
	if snap.GPUFrames < 1 || snap.LastGPUTime <= 0 {
		t.Fatalf("expected GPU completion timing > 0, got %+v", snap)
	}
	if snap.DrawableWidth != 320 || snap.DrawableHeight != 200 {
		t.Fatalf("expected DrawableWidth/Height 320x200, got %dx%d", snap.DrawableWidth, snap.DrawableHeight)
	}
}
