// Package camera shows what a phone's camera sees in a window's native UI, and
// takes photos: AVCaptureSession on iOS, CameraX on Android. For a photo in the
// system's own camera screen, use package imagepicker: it needs no permission.
//
//	cam, err := camera.New(win, camera.Options{})
//	...
//	ui.WebView(c, cam).Fill() // in the view; what it draws after shows over the preview
//	path, err := cam.TakePhoto(ctx)
//
// The preview shows under the content, as a web view does. On desktops and in
// browsers New reports errors.ErrUnsupported.
package camera

import (
	"context"
	"encoding/json"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/internal/platform"
	"github.com/egoist/mygo/internal/surface"
)

const pluginID = "camera"

// Options are which camera shows.
type Options struct {
	// Front chooses the camera on the screen's side.
	Front bool
	// ScanCodes emits QR codes seen in the preview through OnCode.
	ScanCodes bool
}

// Camera is a preview of a camera, which takes photos. Show it with ui.WebView.
type Camera struct{ view *mygo.PluginView }

// New asks the user for the camera, the first time, and makes its preview in a
// window showing native UI.
func New(w *mygo.Window, o Options) (*Camera, error) {
	v, err := mygo.NewPluginView(w, "camera", map[string]any{"front": o.Front, "scanCodes": o.ScanCodes})
	if err != nil {
		return nil, err
	}
	return &Camera{v}, nil
}

// OnCode calls fn on the main thread when ScanCodes is enabled and a QR code
// enters the preview. Call the returned function to stop listening.
func (c *Camera) OnCode(fn func(value string)) (off func()) {
	if c == nil || c.view == nil || fn == nil {
		return func() {}
	}
	viewID := c.view.ID
	return mygo.Native(pluginID).On("code", func(data []byte) {
		var ev struct {
			ID    int    `json:"id"`
			Value string `json:"value"`
		}
		if json.Unmarshal(data, &ev) == nil && ev.ID == viewID && ev.Value != "" {
			fn(ev.Value)
		}
	})
}

// SurfaceWebView gives package ui the view to show.
func (c *Camera) SurfaceWebView(conn *surface.Conn) platform.WebView {
	return c.view.SurfaceWebView(conn)
}

// TakePhoto takes a photo and returns the path of its JPEG in the app's
// temporary directory.
func (c *Camera) TakePhoto(ctx context.Context) (string, error) {
	var r struct {
		Path string `json:"path"`
	}
	err := mygo.Native(pluginID).Call(ctx, "takePhoto", map[string]any{"id": c.view.ID}, &r)
	return r.Path, err
}

// Destroy stops the camera and takes its preview away.
func (c *Camera) Destroy() {
	go mygo.Native(pluginID).Call(context.Background(), "dispose", map[string]any{"id": c.view.ID}, nil)
	c.view.Destroy()
}
