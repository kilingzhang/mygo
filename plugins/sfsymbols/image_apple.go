//go:build darwin || ios

package sfsymbols

import (
	"image"
	"sync"

	"github.com/egoist/mygo/ui"
)

// maxPx is the most pixels square Image draws a symbol.
const maxPx = 512

// shapeKey names a symbol's shape: its name, its pixels square and its
// options. tintKey adds the color it is drawn in.
type shapeKey struct {
	name    string
	px      int
	natural bool
	weight  Weight
}

type tintKey struct {
	shapeKey
	r, g, b, a uint8
}

// The caches hold the shapes the system drew and the bitmaps tinted from them,
// each up to a limit, past which the least recently used goes: an app
// that animates a symbol's color or size does not grow without end.
const (
	shapeLimit = 256
	tintLimit  = 512
)

var (
	mu     sync.Mutex
	shapes = newLRU[shapeKey, *image.RGBA](shapeLimit)
	// missing holds the names the system has no symbol for, so that a
	// Symbol trying a variant (Fill) does not ask the system every frame.
	missing = newLRU[string, struct{}](shapeLimit)
	tinted  = newLRU[tintKey, *ui.Bitmap](tintLimit)
)

// Image returns the SF Symbol named name (as "gearshape"), drawn px pixels
// square in col, or false where the system has no such symbol: on another
// OS, or a system version without it. px is limited to 8 through 512. The result is
// cached, and Image is safe to call from any goroutine.
func Image(name string, px int, col ui.Color, o Options) (ui.ImageSource, bool) {
	if !Available() {
		return nil, false
	}
	px = min(max(px, 8), maxPx)
	sk := shapeKey{name: name, px: px, natural: o.Natural, weight: o.Weight}
	tk := tintKey{shapeKey: sk, r: col.R, g: col.G, b: col.B, a: col.A}
	mu.Lock()
	b, ok := tinted.get(tk)
	coverage, drawn := shapes.get(sk)
	_, absent := missing.get(name)
	mu.Unlock()
	if ok {
		return b, true
	}
	if absent {
		return nil, false
	}
	if !drawn {
		var absent bool
		if coverage, drawn, absent = rasterize(name, px, o.Natural, o.Weight); !drawn {
			// Only a name the system has no symbol for is remembered: another
			// failure may not happen at another size or weight.
			if absent {
				mu.Lock()
				missing.put(name, struct{}{})
				mu.Unlock()
			}
			return nil, false
		}
	}
	b = ui.NewBitmap(tint(coverage, col))
	mu.Lock()
	shapes.put(sk, coverage)
	tinted.put(tk, b)
	mu.Unlock()
	return b, true
}

// tint colors coverage, whose alpha holds the shape, in col.
func tint(coverage *image.RGBA, col ui.Color) *image.RGBA {
	out := image.NewRGBA(coverage.Rect)
	for i := range len(coverage.Pix) / 4 {
		a := uint16(coverage.Pix[i*4+3]) * uint16(col.A) / 255
		out.Pix[i*4+0] = uint8(uint16(col.R) * a / 255)
		out.Pix[i*4+1] = uint8(uint16(col.G) * a / 255)
		out.Pix[i*4+2] = uint8(uint16(col.B) * a / 255)
		out.Pix[i*4+3] = uint8(a)
	}
	return out
}
