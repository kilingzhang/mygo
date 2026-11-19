package sfsymbols

import (
	"bytes"
	"encoding/json"
	"image"
	"image/png"

	"github.com/egoist/mygo/internal/mobile"
)

const pluginID = "sfsymbols"

// Available reports whether the running app's iOS host includes the symbol renderer.
func Available() bool {
	// Keep the hot rendering path in the bitmap cache. The iOS build
	// includes this plugin whenever the Go package is in the app.
	return true
}

// Names returns no catalog on iOS. UIKit looks up each symbol by name.
func Names() []string { return nil }

func rasterize(name string, px int, natural bool, weight Weight) (*image.RGBA, bool, bool) {
	request := struct {
		Name    string `json:"name"`
		Pixels  int    `json:"pixels"`
		Natural bool   `json:"natural"`
		Weight  Weight `json:"weight"`
	}{name, px, natural, weight}
	args, err := json.Marshal(request)
	if err != nil {
		return nil, false, false
	}
	data, handled := mobile.PluginCallSync(pluginID, "rasterize", string(args))
	if !handled {
		return nil, false, false
	}
	if len(data) == 0 {
		return nil, false, true
	}
	src, err := png.Decode(bytes.NewReader(data))
	if err != nil || src.Bounds() != image.Rect(0, 0, px, px) {
		return nil, false, false
	}
	coverage := image.NewRGBA(src.Bounds())
	for y := range px {
		for x := range px {
			_, _, _, alpha := src.At(x, y).RGBA()
			coverage.Pix[y*coverage.Stride+x*4+3] = uint8(alpha >> 8)
		}
	}
	return coverage, true, false
}
