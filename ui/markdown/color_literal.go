package markdown

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/egoist/mygo/ui"
)

// parseColorLiteral parses a color literal into an sRGB ui.Color.
// Supported formats:
//   - Hex: #RRGGBB, #RRGGBBAA (and #RGB, #RGBA when allowCodeForms is true)
//   - CSS: rgb/rgba, hsl/hsla, hwb, oklch, oklab, lab, lch, color(display-p3 ...), color(srgb ...)
//   - Code-only (when allowCodeForms is true):
//     - MyGo/Kong: ui.Hex("#..."), ui.RGB(...), ui.RGBA(...)
//     - Swift/UIKit/AppKit: Color(red: ..., green: ..., blue: ...[, opacity: ...]), UIColor(...), NSColor(...)
//     - Tailwind palette tokens: emerald-500, bg-indigo-500, text-amber-400/80, etc.
func parseColorLiteral(raw string, allowCodeForms bool) (ui.Color, bool) {
	s := strings.TrimSpace(raw)
	if len(s) < 4 {
		return ui.Color{}, false
	}
	if s[0] == '#' {
		return parseHexColor(s[1:], allowCodeForms)
	}
	if open := strings.IndexByte(s, '('); open >= 3 && s[len(s)-1] == ')' {
		fn := strings.ToLower(strings.TrimSpace(s[:open]))
		body := strings.TrimSpace(s[open+1 : len(s)-1])
		if body == "" {
			return ui.Color{}, false
		}
		switch fn {
		case "rgb", "rgba":
			return parseRGBColor(body)
		case "hsl", "hsla":
			return parseHSLColor(body)
		case "hwb":
			return parseHWBColor(body)
		case "oklch":
			return parseOKLCHColor(body)
		case "oklab":
			return parseOKLabColor(body)
		case "lab":
			return parseCIELabColor(body)
		case "lch":
			return parseCIELCHColor(body)
		case "color":
			if allowCodeForms && strings.TrimSpace(s[:open]) == "Color" && strings.ContainsRune(body, ':') {
				return parseSwiftColor(body)
			}
			return parseCSSColorFunc(body)
		}
		if allowCodeForms {
			switch fn {
			case "ui.hex":
				if len(body) >= 6 && ((body[0] == '"' && body[len(body)-1] == '"') || (body[0] == '\'' && body[len(body)-1] == '\'')) {
					inner := strings.TrimSpace(body[1 : len(body)-1])
					if strings.HasPrefix(inner, "#") {
						return parseHexColor(inner[1:], true)
					}
				}
			case "ui.rgb", "ui.rgba":
				return parseRGBColor(body)
			case "color.init", "uicolor", "nscolor":
				return parseSwiftColor(body)
			}
			// SwiftUI Color(red: ..., green: ..., blue: ...[, opacity: ...])
			if strings.TrimSpace(s[:open]) == "Color" {
				return parseSwiftColor(body)
			}
		}
		return ui.Color{}, false
	}
	if allowCodeForms {
		return parseTailwindColor(s)
	}
	return ui.Color{}, false
}

// parseLinearGradient parses a CSS linear-gradient(...) expression and returns
// its color stops when at least two valid color stops are present.
func parseLinearGradient(raw string) ([]ui.Color, bool) {
	s := strings.TrimSpace(raw)
	if !hasFoldPrefix(s, "linear-gradient(") || !strings.HasSuffix(s, ")") {
		return nil, false
	}
	body := strings.TrimSpace(s[len("linear-gradient(") : len(s)-1])
	if body == "" {
		return nil, false
	}
	parts := splitTopLevelCommas(body)
	if len(parts) < 2 {
		return nil, false
	}
	startIdx := 0
	first := strings.TrimSpace(parts[0])
	if isGradientDirection(first) {
		startIdx = 1
	}
	if len(parts)-startIdx < 2 {
		return nil, false
	}
	stops := make([]ui.Color, 0, len(parts)-startIdx)
	for _, part := range parts[startIdx:] {
		c, ok := parseGradientStopColor(part)
		if !ok {
			return nil, false
		}
		stops = append(stops, c)
	}
	return stops, len(stops) >= 2
}

func splitTopLevelCommas(s string) []string {
	var parts []string
	depth := 0
	start := 0
	for i := range len(s) {
		switch s[i] {
		case '(':
			depth++
		case ')':
			if depth > 0 {
				depth--
			}
		case ',':
			if depth == 0 {
				parts = append(parts, s[start:i])
				start = i + 1
			}
		}
	}
	if depth != 0 {
		return nil
	}
	parts = append(parts, s[start:])
	return parts
}

func isGradientDirection(s string) bool {
	ls := strings.ToLower(strings.TrimSpace(s))
	if strings.HasPrefix(ls, "to ") || strings.HasPrefix(ls, "in ") {
		return true
	}
	for _, suf := range []string{"deg", "rad", "grad", "turn"} {
		if strings.HasSuffix(ls, suf) {
			if _, ok := parseColorFloat(strings.TrimSpace(ls[:len(ls)-len(suf)])); ok {
				return true
			}
		}
	}
	return false
}

func parseGradientStopColor(part string) (ui.Color, bool) {
	p := strings.TrimSpace(part)
	if p == "" {
		return ui.Color{}, false
	}
	// Try full string first (handles functional colors with spaces).
	if c, ok := parseColorLiteral(p, true); ok {
		return c, true
	}
	// If it starts with a functional color followed by a stop position (e.g. "rgba(16, 163, 127, 0.5) 20%").
	if open := strings.IndexByte(p, '('); open > 0 {
		depth := 0
		for i := open; i < len(p); i++ {
			if p[i] == '(' {
				depth++
			} else if p[i] == ')' {
				depth--
				if depth == 0 {
					return parseColorLiteral(p[:i+1], true)
				}
			}
		}
		return ui.Color{}, false
	}
	// Otherwise split off trailing stop position (e.g. "#10a37f 0%" or "emerald-500 50%").
	if sp := strings.IndexAny(p, " \t"); sp > 0 {
		return parseColorLiteral(p[:sp], true)
	}
	return ui.Color{}, false
}

func parseHexColor(hex string, allowShort bool) (ui.Color, bool) {
	switch len(hex) {
	case 3:
		if !allowShort {
			return ui.Color{}, false
		}
		r, ok1 := hexNibble(hex[0])
		g, ok2 := hexNibble(hex[1])
		b, ok3 := hexNibble(hex[2])
		if !ok1 || !ok2 || !ok3 {
			return ui.Color{}, false
		}
		return ui.Color{R: r*16 + r, G: g*16 + g, B: b*16 + b, A: 255}, true
	case 4:
		if !allowShort {
			return ui.Color{}, false
		}
		r, ok1 := hexNibble(hex[0])
		g, ok2 := hexNibble(hex[1])
		b, ok3 := hexNibble(hex[2])
		a, ok4 := hexNibble(hex[3])
		if !ok1 || !ok2 || !ok3 || !ok4 {
			return ui.Color{}, false
		}
		return ui.Color{R: r*16 + r, G: g*16 + g, B: b*16 + b, A: a*16 + a}, true
	case 6:
		r, ok1 := hexByte(hex[0], hex[1])
		g, ok2 := hexByte(hex[2], hex[3])
		b, ok3 := hexByte(hex[4], hex[5])
		if !ok1 || !ok2 || !ok3 {
			return ui.Color{}, false
		}
		return ui.Color{R: r, G: g, B: b, A: 255}, true
	case 8:
		r, ok1 := hexByte(hex[0], hex[1])
		g, ok2 := hexByte(hex[2], hex[3])
		b, ok3 := hexByte(hex[4], hex[5])
		a, ok4 := hexByte(hex[6], hex[7])
		if !ok1 || !ok2 || !ok3 || !ok4 {
			return ui.Color{}, false
		}
		return ui.Color{R: r, G: g, B: b, A: a}, true
	default:
		return ui.Color{}, false
	}
}

func hexNibble(b byte) (uint8, bool) {
	switch {
	case b >= '0' && b <= '9':
		return b - '0', true
	case b >= 'a' && b <= 'f':
		return b - 'a' + 10, true
	case b >= 'A' && b <= 'F':
		return b - 'A' + 10, true
	default:
		return 0, false
	}
}

func hexByte(hi, lo byte) (uint8, bool) {
	a, ok1 := hexNibble(hi)
	b, ok2 := hexNibble(lo)
	if !ok1 || !ok2 {
		return 0, false
	}
	return a*16 + b, true
}

func isHexDigit(b byte) bool {
	_, ok := hexNibble(b)
	return ok
}

// splitColorArgs splits either comma-separated ("a, b, c[, d]") or
// space-separated ("a b c [/ d]") CSS color function arguments.
func splitColorArgs(body string) (c1, c2, c3, alpha string, hasAlpha, ok bool) {
	if strings.ContainsRune(body, ',') {
		if strings.ContainsRune(body, '/') {
			return "", "", "", "", false, false
		}
		parts := strings.Split(body, ",")
		if len(parts) != 3 && len(parts) != 4 {
			return "", "", "", "", false, false
		}
		for i := range parts {
			parts[i] = strings.TrimSpace(parts[i])
			if parts[i] == "" {
				return "", "", "", "", false, false
			}
		}
		if len(parts) == 4 {
			return parts[0], parts[1], parts[2], parts[3], true, true
		}
		return parts[0], parts[1], parts[2], "", false, true
	}
	left := body
	if slash := strings.IndexByte(body, '/'); slash >= 0 {
		if strings.IndexByte(body[slash+1:], '/') >= 0 {
			return "", "", "", "", false, false
		}
		left = strings.TrimSpace(body[:slash])
		alpha = strings.TrimSpace(body[slash+1:])
		if alpha == "" {
			return "", "", "", "", false, false
		}
		hasAlpha = true
	}
	fields := strings.Fields(left)
	if len(fields) != 3 {
		return "", "", "", "", false, false
	}
	return fields[0], fields[1], fields[2], alpha, hasAlpha, true
}

func parseColorFloat(s string) (float64, bool) {
	if s == "" || strings.ContainsAny(s, "eE_") {
		return 0, false
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil || math.IsNaN(v) || math.IsInf(v, 0) {
		return 0, false
	}
	return v, true
}

func parseAlphaArg(s string) (uint8, bool) {
	if strings.HasSuffix(s, "%") {
		v, ok := parseColorFloat(s[:len(s)-1])
		if !ok || v < 0 || v > 100 {
			return 0, false
		}
		return clampByte(v / 100 * 255), true
	}
	v, ok := parseColorFloat(s)
	if !ok || v < 0 || v > 1 {
		return 0, false
	}
	return clampByte(v * 255), true
}

func parseRGBChannel(s string) (uint8, bool) {
	if strings.HasSuffix(s, "%") {
		v, ok := parseColorFloat(s[:len(s)-1])
		if !ok || v < 0 || v > 100 {
			return 0, false
		}
		return clampByte(v / 100 * 255), true
	}
	v, ok := parseColorFloat(s)
	if !ok || v < 0 || v > 255 {
		return 0, false
	}
	return clampByte(v), true
}

func parseUnitOrPercent(s string, maxVal float64) (float64, bool) {
	if strings.HasSuffix(s, "%") {
		v, ok := parseColorFloat(s[:len(s)-1])
		if !ok || v < 0 || v > 100 {
			return 0, false
		}
		return v / 100 * maxVal, true
	}
	v, ok := parseColorFloat(s)
	if !ok || v < 0 || v > maxVal {
		return 0, false
	}
	return v, true
}

func parseSignedOrPercent(s string, pctScale, absMax float64) (float64, bool) {
	if strings.HasSuffix(s, "%") {
		v, ok := parseColorFloat(s[:len(s)-1])
		if !ok || v < -100 || v > 100 {
			return 0, false
		}
		return v / 100 * pctScale, true
	}
	v, ok := parseColorFloat(s)
	if !ok || v < -absMax || v > absMax {
		return 0, false
	}
	return v, true
}

func parseRGBColor(body string) (ui.Color, bool) {
	c1, c2, c3, alpha, hasAlpha, ok := splitColorArgs(body)
	if !ok {
		return ui.Color{}, false
	}
	r, ok1 := parseRGBChannel(c1)
	g, ok2 := parseRGBChannel(c2)
	b, ok3 := parseRGBChannel(c3)
	if !ok1 || !ok2 || !ok3 {
		return ui.Color{}, false
	}
	a := uint8(255)
	if hasAlpha {
		var ok4 bool
		a, ok4 = parseAlphaArg(alpha)
		if !ok4 {
			return ui.Color{}, false
		}
	}
	return ui.Color{R: r, G: g, B: b, A: a}, true
}

func parseAngleDeg(s string) (float64, bool) {
	ls := strings.ToLower(s)
	scale := 1.0
	switch {
	case strings.HasSuffix(ls, "deg"):
		ls = ls[:len(ls)-3]
	case strings.HasSuffix(ls, "grad"):
		ls = ls[:len(ls)-4]
		scale = 0.9
	case strings.HasSuffix(ls, "rad"):
		ls = ls[:len(ls)-3]
		scale = 180.0 / math.Pi
	case strings.HasSuffix(ls, "turn"):
		ls = ls[:len(ls)-4]
		scale = 360.0
	}
	v, ok := parseColorFloat(ls)
	if !ok {
		return 0, false
	}
	deg := math.Mod(v*scale, 360)
	if deg < 0 {
		deg += 360
	}
	return deg, true
}

func parsePercentUnit(s string) (float64, bool) {
	if !strings.HasSuffix(s, "%") {
		return 0, false
	}
	v, ok := parseColorFloat(s[:len(s)-1])
	if !ok || v < 0 || v > 100 {
		return 0, false
	}
	return v / 100, true
}

func parseHSLColor(body string) (ui.Color, bool) {
	c1, c2, c3, alpha, hasAlpha, ok := splitColorArgs(body)
	if !ok {
		return ui.Color{}, false
	}
	h, ok1 := parseAngleDeg(c1)
	s, ok2 := parsePercentUnit(c2)
	l, ok3 := parsePercentUnit(c3)
	if !ok1 || !ok2 || !ok3 {
		return ui.Color{}, false
	}
	a := uint8(255)
	if hasAlpha {
		var ok4 bool
		a, ok4 = parseAlphaArg(alpha)
		if !ok4 {
			return ui.Color{}, false
		}
	}
	col := hslToColor(h, s, l)
	col.A = a
	return col, true
}

func parseHWBColor(body string) (ui.Color, bool) {
	if strings.ContainsRune(body, ',') {
		return ui.Color{}, false
	}
	c1, c2, c3, alpha, hasAlpha, ok := splitColorArgs(body)
	if !ok {
		return ui.Color{}, false
	}
	h, ok1 := parseAngleDeg(c1)
	w, ok2 := parsePercentUnit(c2)
	bl, ok3 := parsePercentUnit(c3)
	if !ok1 || !ok2 || !ok3 {
		return ui.Color{}, false
	}
	a := uint8(255)
	if hasAlpha {
		var ok4 bool
		a, ok4 = parseAlphaArg(alpha)
		if !ok4 {
			return ui.Color{}, false
		}
	}
	if w+bl >= 1 {
		v := clampByte((w / (w + bl)) * 255)
		return ui.Color{R: v, G: v, B: v, A: a}, true
	}
	base := hslToColor(h, 1, 0.5)
	scale := 1 - w - bl
	return ui.Color{
		R: clampByte((float64(base.R)/255*scale + w) * 255),
		G: clampByte((float64(base.G)/255*scale + w) * 255),
		B: clampByte((float64(base.B)/255*scale + w) * 255),
		A: a,
	}, true
}

func parseOKLCHColor(body string) (ui.Color, bool) {
	if strings.ContainsRune(body, ',') {
		return ui.Color{}, false
	}
	c1, c2, c3, alpha, hasAlpha, ok := splitColorArgs(body)
	if !ok {
		return ui.Color{}, false
	}
	L, ok1 := parseUnitOrPercent(c1, 1)
	C, ok2 := parseUnitOrPercent(c2, 0.4)
	if !ok2 {
		// Allow raw chroma up to 1.0 (wide gamut values > 0.4).
		var rawC float64
		rawC, ok2 = parseColorFloat(c2)
		if ok2 && rawC >= 0 && rawC <= 1 {
			C = rawC
		} else {
			ok2 = false
		}
	}
	H, ok3 := parseAngleDeg(c3)
	if !ok1 || !ok2 || !ok3 {
		return ui.Color{}, false
	}
	a := uint8(255)
	if hasAlpha {
		var ok4 bool
		a, ok4 = parseAlphaArg(alpha)
		if !ok4 {
			return ui.Color{}, false
		}
	}
	col := oklchToColor(L, C, H)
	col.A = a
	return col, true
}

func parseOKLabColor(body string) (ui.Color, bool) {
	if strings.ContainsRune(body, ',') {
		return ui.Color{}, false
	}
	c1, c2, c3, alpha, hasAlpha, ok := splitColorArgs(body)
	if !ok {
		return ui.Color{}, false
	}
	L, ok1 := parseUnitOrPercent(c1, 1)
	av, ok2 := parseSignedOrPercent(c2, 0.4, 0.5)
	bv, ok3 := parseSignedOrPercent(c3, 0.4, 0.5)
	if !ok1 || !ok2 || !ok3 {
		return ui.Color{}, false
	}
	a := uint8(255)
	if hasAlpha {
		var ok4 bool
		a, ok4 = parseAlphaArg(alpha)
		if !ok4 {
			return ui.Color{}, false
		}
	}
	col := oklabToColor(L, av, bv)
	col.A = a
	return col, true
}

func parseCIELabColor(body string) (ui.Color, bool) {
	if strings.ContainsRune(body, ',') {
		return ui.Color{}, false
	}
	c1, c2, c3, alpha, hasAlpha, ok := splitColorArgs(body)
	if !ok {
		return ui.Color{}, false
	}
	L, ok1 := parseUnitOrPercent(c1, 100)
	av, ok2 := parseSignedOrPercent(c2, 125, 160)
	bv, ok3 := parseSignedOrPercent(c3, 125, 160)
	if !ok1 || !ok2 || !ok3 {
		return ui.Color{}, false
	}
	a := uint8(255)
	if hasAlpha {
		var ok4 bool
		a, ok4 = parseAlphaArg(alpha)
		if !ok4 {
			return ui.Color{}, false
		}
	}
	col := cieLabToColor(L, av, bv)
	col.A = a
	return col, true
}

func parseCIELCHColor(body string) (ui.Color, bool) {
	if strings.ContainsRune(body, ',') {
		return ui.Color{}, false
	}
	c1, c2, c3, alpha, hasAlpha, ok := splitColorArgs(body)
	if !ok {
		return ui.Color{}, false
	}
	L, ok1 := parseUnitOrPercent(c1, 100)
	C, ok2 := parseUnitOrPercent(c2, 150)
	if !ok2 {
		var rawC float64
		rawC, ok2 = parseColorFloat(c2)
		if ok2 && rawC >= 0 && rawC <= 230 {
			C = rawC
		} else {
			ok2 = false
		}
	}
	H, ok3 := parseAngleDeg(c3)
	if !ok1 || !ok2 || !ok3 {
		return ui.Color{}, false
	}
	a := uint8(255)
	if hasAlpha {
		var ok4 bool
		a, ok4 = parseAlphaArg(alpha)
		if !ok4 {
			return ui.Color{}, false
		}
	}
	hrad := H * math.Pi / 180
	col := cieLabToColor(L, C*math.Cos(hrad), C*math.Sin(hrad))
	col.A = a
	return col, true
}

func parseCSSColorFunc(body string) (ui.Color, bool) {
	if strings.ContainsRune(body, ',') {
		return ui.Color{}, false
	}
	left := body
	alphaStr := ""
	hasAlpha := false
	if slash := strings.IndexByte(body, '/'); slash >= 0 {
		if strings.IndexByte(body[slash+1:], '/') >= 0 {
			return ui.Color{}, false
		}
		left = strings.TrimSpace(body[:slash])
		alphaStr = strings.TrimSpace(body[slash+1:])
		if alphaStr == "" {
			return ui.Color{}, false
		}
		hasAlpha = true
	}
	fields := strings.Fields(left)
	if len(fields) != 4 {
		return ui.Color{}, false
	}
	space := strings.ToLower(fields[0])
	r, ok1 := parseUnitOrPercent(fields[1], 1)
	g, ok2 := parseUnitOrPercent(fields[2], 1)
	b, ok3 := parseUnitOrPercent(fields[3], 1)
	if !ok1 || !ok2 || !ok3 {
		return ui.Color{}, false
	}
	a := uint8(255)
	if hasAlpha {
		var ok4 bool
		a, ok4 = parseAlphaArg(alphaStr)
		if !ok4 {
			return ui.Color{}, false
		}
	}
	switch space {
	case "srgb":
		return ui.Color{R: clampByte(r * 255), G: clampByte(g * 255), B: clampByte(b * 255), A: a}, true
	case "display-p3":
		lr := gammaToLinear(r)
		lg := gammaToLinear(g)
		lb := gammaToLinear(b)
		// Display P3 linear -> sRGB linear (W3C CSS Color 4 matrix).
		sr := +1.2249401762805588*lr - 0.2249401762805588*lg
		sg := -0.0420569547096883*lr + 1.0420569547096883*lg
		sb := -0.0196375545903343*lr - 0.0786360455506324*lg + 1.0982736001409667*lb
		return ui.Color{
			R: clampByte(linearToSRGB(sr) * 255),
			G: clampByte(linearToSRGB(sg) * 255),
			B: clampByte(linearToSRGB(sb) * 255),
			A: a,
		}, true
	default:
		return ui.Color{}, false
	}
}

// parseSwiftColor parses SwiftUI/UIKit/AppKit constructors such as:
// Color(red: 0.06, green: 0.64, blue: 0.50) or UIColor(red: 0.42, green: 0.39, blue: 1, alpha: 0.8).
func parseSwiftColor(body string) (ui.Color, bool) {
	parts := strings.Split(body, ",")
	if len(parts) < 3 || len(parts) > 4 {
		return ui.Color{}, false
	}
	var r, g, b float64
	a := 1.0
	hasR, hasG, hasB := false, false, false
	for _, part := range parts {
		colon := strings.IndexByte(part, ':')
		if colon <= 0 {
			return ui.Color{}, false
		}
		k := strings.TrimSpace(part[:colon])
		vStr := strings.TrimSpace(part[colon+1:])
		v, ok := parseColorFloat(vStr)
		if !ok || v < 0 || v > 1 {
			return ui.Color{}, false
		}
		switch k {
		case "red":
			r, hasR = v, true
		case "green":
			g, hasG = v, true
		case "blue":
			b, hasB = v, true
		case "opacity", "alpha":
			a = v
		default:
			return ui.Color{}, false
		}
	}
	if !hasR || !hasG || !hasB {
		return ui.Color{}, false
	}
	return ui.Color{
		R: clampByte(r * 255),
		G: clampByte(g * 255),
		B: clampByte(b * 255),
		A: clampByte(a * 255),
	}, true
}

// Official Tailwind CSS palette (v3/v4 hex values).
var tailwindPalette = map[string][11]uint32{
	"slate":   {0xf8fafc, 0xf1f5f9, 0xe2e8f0, 0xcbd5e1, 0x94a3b8, 0x64748b, 0x475569, 0x334155, 0x1e293b, 0x0f172a, 0x020617},
	"gray":    {0xf9fafb, 0xf3f4f6, 0xe5e7eb, 0xd1d5db, 0x9ca3af, 0x6b7280, 0x4b5563, 0x374151, 0x1f2937, 0x111827, 0x030712},
	"zinc":    {0xfafafa, 0xf4f4f5, 0xe4e4e7, 0xd4d4d8, 0xa1a1aa, 0x71717a, 0x52525b, 0x3f3f46, 0x27272a, 0x18181b, 0x09090b},
	"neutral": {0xfafafa, 0xf5f5f5, 0xe5e5e5, 0xd4d4d4, 0xa3a3a3, 0x737373, 0x525252, 0x404040, 0x262626, 0x171717, 0x0a0a0a},
	"stone":   {0xfafaf9, 0xf5f5f4, 0xe7e5e4, 0xd6d3d1, 0xa8a29e, 0x78716c, 0x57534e, 0x44403c, 0x292524, 0x1c1917, 0x0c0a09},
	"red":     {0xfef2f2, 0xfee2e2, 0xfecaca, 0xfca5a5, 0xf87171, 0xef4444, 0xdc2626, 0xb91c1c, 0x991b1b, 0x7f1d1d, 0x450a0a},
	"orange":  {0xfff7ed, 0xffedd5, 0xfed7aa, 0xfdba74, 0xfb923c, 0xf97316, 0xea580c, 0xc2410c, 0x9a3412, 0x7c2d12, 0x431407},
	"amber":   {0xfffbeb, 0xfef3c7, 0xfde68a, 0xfcd34d, 0xfbbf24, 0xf59e0b, 0xd97706, 0xb45309, 0x92400e, 0x78350f, 0x451a03},
	"yellow":  {0xfefce8, 0xfef9c3, 0xfef08a, 0xfde047, 0xfacc15, 0xeab308, 0xca8a04, 0xa16207, 0x854d0e, 0x713f12, 0x422006},
	"lime":    {0xf7fee7, 0xecfccb, 0xd9f99d, 0xbef264, 0xa3e635, 0x84cc16, 0x65a30d, 0x4d7c0f, 0x3f6212, 0x365314, 0x1a2e05},
	"green":   {0xf0fdf4, 0xdcfce7, 0xbbf7d0, 0x86efac, 0x4ade80, 0x22c55e, 0x16a34a, 0x15803d, 0x166534, 0x14532d, 0x052e16},
	"emerald": {0xecfdf5, 0xd1fae5, 0xa7f3d0, 0x6ee7b7, 0x34d399, 0x10b981, 0x059669, 0x047857, 0x065f46, 0x064e3b, 0x022c22},
	"teal":    {0xf0fdfa, 0xccfbf1, 0x99f6e4, 0x5eead4, 0x2dd4bf, 0x14b8a6, 0x0d9488, 0x0f766e, 0x115e59, 0x134e4a, 0x042f2e},
	"cyan":    {0xecfeff, 0xcffafe, 0xa5f3fc, 0x67e8f9, 0x22d3ee, 0x06b6d4, 0x0891b2, 0x0e7490, 0x155e75, 0x164e63, 0x083344},
	"sky":     {0xf0f9ff, 0xe0f2fe, 0xbae6fd, 0x7dd3fc, 0x38bdf8, 0x0ea5e9, 0x0284c7, 0x0369a1, 0x075985, 0x0c4a6e, 0x082f49},
	"blue":    {0xeff6ff, 0xdbeafe, 0xbfdbfe, 0x93c5fd, 0x60a5fa, 0x3b82f6, 0x2563eb, 0x1d4ed8, 0x1e40af, 0x1e3a8a, 0x172554},
	"indigo":  {0xeef2ff, 0xe0e7ff, 0xc7d2fe, 0xa5b4fc, 0x818cf8, 0x6366f1, 0x4f46e5, 0x4338ca, 0x3730a3, 0x312e81, 0x1e1b4b},
	"violet":  {0xf5f3ff, 0xede9fe, 0xddd6fe, 0xc4b5fd, 0xa78bfa, 0x8b5cf6, 0x7c3aed, 0x6d28d9, 0x5b21b6, 0x4c1d95, 0x2e1065},
	"purple":  {0xfaf5ff, 0xf3e8ff, 0xe9d5ff, 0xd8b4fe, 0xc084fc, 0xa855f7, 0x9333ea, 0x7e22ce, 0x6b21a8, 0x581c87, 0x3b0764},
	"fuchsia": {0xfdf4ff, 0xfae8ff, 0xf5d0fe, 0xf0abfc, 0xe879f9, 0xd946ef, 0xc026d3, 0xa21caf, 0x86198f, 0x701a75, 0x4a044e},
	"pink":    {0xfdf2f8, 0xfce7f3, 0xfbcfe8, 0xf9a8d4, 0xf472b6, 0xec4899, 0xdb2777, 0xbe185d, 0x9d174d, 0x831843, 0x500724},
	"rose":    {0xfff1f2, 0xffe4e6, 0xfecdd3, 0xfda4af, 0xfb7185, 0xf43f5e, 0xe11d48, 0xbe123c, 0x9f1239, 0x881337, 0x4c0519},
}

var tailwindPrefixes = []string{
	"bg-", "text-", "border-", "ring-", "fill-", "stroke-",
	"from-", "via-", "to-", "accent-", "decoration-", "outline-",
	"shadow-", "divide-", "placeholder-",
}

func tailwindShadeIndex(s string) (int, bool) {
	switch s {
	case "50":
		return 0, true
	case "100":
		return 1, true
	case "200":
		return 2, true
	case "300":
		return 3, true
	case "400":
		return 4, true
	case "500":
		return 5, true
	case "600":
		return 6, true
	case "700":
		return 7, true
	case "800":
		return 8, true
	case "900":
		return 9, true
	case "950":
		return 10, true
	default:
		return 0, false
	}
}

func parseTailwindColor(s string) (ui.Color, bool) {
	if len(s) < 5 || len(s) > 36 || strings.ContainsAny(s, " \t\n") {
		return ui.Color{}, false
	}
	alpha := uint8(255)
	if slash := strings.IndexByte(s, '/'); slash >= 0 {
		opStr := s[slash+1:]
		s = s[:slash]
		op, err := strconv.Atoi(opStr)
		if err != nil || op < 0 || op > 100 {
			return ui.Color{}, false
		}
		alpha = clampByte(float64(op) / 100 * 255)
	}
	for _, pfx := range tailwindPrefixes {
		if strings.HasPrefix(s, pfx) {
			s = s[len(pfx):]
			break
		}
	}
	dash := strings.IndexByte(s, '-')
	if dash <= 0 || dash >= len(s)-1 {
		return ui.Color{}, false
	}
	hue := s[:dash]
	shadeIdx, ok := tailwindShadeIndex(s[dash+1:])
	if !ok {
		return ui.Color{}, false
	}
	row, ok := tailwindPalette[hue]
	if !ok {
		return ui.Color{}, false
	}
	hex := row[shadeIdx]
	return ui.Color{
		R: uint8(hex >> 16),
		G: uint8(hex >> 8),
		B: uint8(hex),
		A: alpha,
	}, true
}

func oklchToColor(L, C, Hdeg float64) ui.Color {
	hrad := Hdeg * math.Pi / 180
	return oklabToColor(L, C*math.Cos(hrad), C*math.Sin(hrad))
}

func oklabToColor(L, a, b float64) ui.Color {
	l_ := L + 0.3963377774*a + 0.2158037573*b
	m_ := L - 0.1055613458*a - 0.0638541728*b
	s_ := L - 0.0894841775*a - 1.2914855480*b

	l := l_ * l_ * l_
	m := m_ * m_ * m_
	s := s_ * s_ * s_

	rl := +4.0767416621*l - 3.3077115913*m + 0.2309699292*s
	gl := -1.2684380046*l + 2.6097574011*m - 0.3413193965*s
	bl := -0.0041960863*l - 0.7034186147*m + 1.7076147010*s

	return ui.Color{
		R: clampByte(linearToSRGB(rl) * 255),
		G: clampByte(linearToSRGB(gl) * 255),
		B: clampByte(linearToSRGB(bl) * 255),
		A: 255,
	}
}

func cieLabToColor(L, a, b float64) ui.Color {
	// CIE Lab (D50) -> XYZ D50.
	fy := (L + 16) / 116
	fx := a/500 + fy
	fz := fy - b/200

	const eps = 216.0 / 24389.0
	const kappa = 24389.0 / 27.0
	fInv := func(f float64) float64 {
		f3 := f * f * f
		if f3 > eps {
			return f3
		}
		return (116*f - 16) / kappa
	}
	// D50 reference white.
	x50 := fInv(fx) * 0.96422
	y50 := fInv(fy) * 1.00000
	z50 := fInv(fz) * 0.82521

	// Bradford chromatic adaptation D50 -> D65.
	x65 := +0.9555766*x50 - 0.0230393*y50 + 0.0631636*z50
	y65 := -0.0282895*x50 + 1.0099416*y50 + 0.0210077*z50
	z65 := +0.0122982*x50 - 0.0204830*y50 + 1.3299098*z50

	// XYZ D65 -> linear sRGB.
	rl := +3.2404542*x65 - 1.5371385*y65 - 0.4985314*z65
	gl := -0.9692660*x65 + 1.8760108*y65 + 0.0415560*z65
	bl := +0.0556434*x65 - 0.2040259*y65 + 1.0572252*z65

	return ui.Color{
		R: clampByte(linearToSRGB(rl) * 255),
		G: clampByte(linearToSRGB(gl) * 255),
		B: clampByte(linearToSRGB(bl) * 255),
		A: 255,
	}
}

func linearToSRGB(v float64) float64 {
	if v <= 0 {
		return 0
	}
	if v >= 1 {
		return 1
	}
	if v <= 0.0031308 {
		return 12.92 * v
	}
	return 1.055*math.Pow(v, 1.0/2.4) - 0.055
}

func gammaToLinear(v float64) float64 {
	if v <= 0.04045 {
		return v / 12.92
	}
	return math.Pow((v+0.055)/1.055, 2.4)
}

func srgbToLinear(u uint8) float64 {
	return gammaToLinear(float64(u) / 255.0)
}

func relativeLuminance(c ui.Color) float64 {
	return 0.2126*srgbToLinear(c.R) + 0.7152*srgbToLinear(c.G) + 0.0722*srgbToLinear(c.B)
}

func contrastRatio(a, b ui.Color) float64 {
	la := relativeLuminance(a)
	lb := relativeLuminance(b)
	if la < lb {
		la, lb = lb, la
	}
	return (la + 0.05) / (lb + 0.05)
}

// ColorContrastRatio returns the WCAG 2.1 contrast ratio between two opaque colors.
func ColorContrastRatio(a, b Color) float64 { return contrastRatio(a, b) }

// FormatColorHex formats c as #rrggbb (or #rrggbbaa when translucent).
func FormatColorHex(c Color) string {
	if c.A < 255 {
		return fmt.Sprintf("#%02x%02x%02x%02x", c.R, c.G, c.B, c.A)
	}
	return fmt.Sprintf("#%02x%02x%02x", c.R, c.G, c.B)
}

// FormatColorRGB formats c as rgb(r, g, b) or rgba(r, g, b, a).
func FormatColorRGB(c Color) string {
	if c.A < 255 {
		return fmt.Sprintf("rgba(%d, %d, %d, %.2f)", c.R, c.G, c.B, float64(c.A)/255)
	}
	return fmt.Sprintf("rgb(%d, %d, %d)", c.R, c.G, c.B)
}

// FormatColorHSL formats c as hsl(h, s%, l%) or hsla(h, s%, l%, a).
func FormatColorHSL(c Color) string {
	h, s, l := rgbToHSL(c)
	hi := int(math.Round(h)) % 360
	si := int(math.Round(s * 100))
	li := int(math.Round(l * 100))
	if c.A < 255 {
		return fmt.Sprintf("hsla(%d, %d%%, %d%%, %.2f)", hi, si, li, float64(c.A)/255)
	}
	return fmt.Sprintf("hsl(%d, %d%%, %d%%)", hi, si, li)
}

// FormatColorOKLCH formats c as oklch(L C H) or oklch(L C H / a).
func FormatColorOKLCH(c Color) string {
	rl, gl, bl := srgbToLinear(c.R), srgbToLinear(c.G), srgbToLinear(c.B)
	l := math.Cbrt(0.4122214708*rl + 0.5363325363*gl + 0.0514459929*bl)
	m := math.Cbrt(0.2119034982*rl + 0.6806995451*gl + 0.1073969566*bl)
	s := math.Cbrt(0.0883024619*rl + 0.2817188376*gl + 0.6299787005*bl)
	L := 0.2104542553*l + 0.7936177850*m - 0.0040720468*s
	av := 1.9779984951*l - 2.4285922050*m + 0.4505937099*s
	bv := 0.0259040371*l + 0.7827717662*m - 0.8086757660*s
	C := math.Hypot(av, bv)
	H := math.Atan2(bv, av) * 180 / math.Pi
	if H < 0 {
		H += 360
	}
	if C < 0.0005 {
		C, H = 0, 0
	}
	if c.A < 255 {
		return fmt.Sprintf("oklch(%.2f %.3f %.0f / %.2f)", L, C, H, float64(c.A)/255)
	}
	return fmt.Sprintf("oklch(%.2f %.3f %.0f)", L, C, H)
}

func rgbToHSL(c ui.Color) (h, s, l float64) {
	r := float64(c.R) / 255
	g := float64(c.G) / 255
	b := float64(c.B) / 255
	hi := max(r, max(g, b))
	lo := min(r, min(g, b))
	l = (hi + lo) / 2
	d := hi - lo
	if d == 0 {
		return 0, 0, l
	}
	if l < 0.5 {
		s = d / (hi + lo)
	} else {
		s = d / (2 - hi - lo)
	}
	switch hi {
	case r:
		h = (g - b) / d
		if g < b {
			h += 6
		}
	case g:
		h = (b-r)/d + 2
	default:
		h = (r-g)/d + 4
	}
	h *= 60
	return h, s, l
}

func hslToColor(h, s, l float64) ui.Color {
	if s <= 0 {
		v := clampByte(l * 255)
		return ui.Color{R: v, G: v, B: v, A: 255}
	}
	var q float64
	if l < 0.5 {
		q = l * (1 + s)
	} else {
		q = l + s - l*s
	}
	p := 2*l - q
	hk := math.Mod(h/360, 1)
	if hk < 0 {
		hk += 1
	}
	hue := func(t float64) float64 {
		if t < 0 {
			t += 1
		}
		if t > 1 {
			t -= 1
		}
		switch {
		case t < 1.0/6.0:
			return p + (q-p)*6*t
		case t < 0.5:
			return q
		case t < 2.0/3.0:
			return p + (q-p)*(2.0/3.0-t)*6
		default:
			return p
		}
	}
	return ui.Color{
		R: clampByte(hue(hk+1.0/3.0) * 255),
		G: clampByte(hue(hk) * 255),
		B: clampByte(hue(hk-1.0/3.0) * 255),
		A: 255,
	}
}

func clampByte(v float64) uint8 {
	if v <= 0 {
		return 0
	}
	if v >= 255 {
		return 255
	}
	return uint8(v + 0.5)
}

// ensureColorContrast preserves hue and saturation while adjusting lightness
// only as much as needed to meet minRatio against bg.
func ensureColorContrast(fg, bg ui.Color, minRatio float64) ui.Color {
	if contrastRatio(fg, bg) >= minRatio {
		return fg
	}
	h, s, l := rgbToHSL(fg)
	bgLum := relativeLuminance(bg)
	lighten := bgLum < 0.5

	search := func(up bool) (ui.Color, bool) {
		var lo, hi float64
		if up {
			lo, hi = l, 1.0
			if contrastRatio(hslToColor(h, s, hi), bg) < minRatio {
				return hslToColor(h, s, hi), false
			}
			for range 16 {
				mid := (lo + hi) * 0.5
				if contrastRatio(hslToColor(h, s, mid), bg) >= minRatio {
					hi = mid
				} else {
					lo = mid
				}
			}
			return hslToColor(h, s, hi), true
		}
		lo, hi = 0.0, l
		if contrastRatio(hslToColor(h, s, lo), bg) < minRatio {
			return hslToColor(h, s, lo), false
		}
		for range 16 {
			mid := (lo + hi) * 0.5
			if contrastRatio(hslToColor(h, s, mid), bg) >= minRatio {
				lo = mid
			} else {
				hi = mid
			}
		}
		return hslToColor(h, s, lo), true
	}

	if c, ok := search(lighten); ok {
		return c
	}
	alt, ok := search(!lighten)
	if ok {
		return alt
	}
	lightEnd := hslToColor(h, s, 1)
	darkEnd := hslToColor(h, s, 0)
	if contrastRatio(lightEnd, bg) >= contrastRatio(darkEnd, bg) {
		return lightEnd
	}
	return darkEnd
}

// colorLiteralPalette derives a readable foreground color and (for inline code)
// a subtle tinted pill background from raw over the active theme surface.
func colorLiteralPalette(raw, surface, codeBg ui.Color, dark, isCode bool) (fg, bg ui.Color) {
	if surface.A == 0 {
		if dark {
			surface = ui.Hex("#161a28")
		} else {
			surface = ui.Hex("#ffffff")
		}
	}
	vis := raw.Over(surface)
	minContrast := 4.5
	if dark {
		minContrast = 5.0
	}
	if !isCode {
		return ensureColorContrast(vis, surface, minContrast), ui.Color{}
	}
	tintAlpha := float32(0.14)
	if dark {
		tintAlpha = 0.18
	}
	if contrastRatio(vis, surface) < 1.35 {
		bg = codeBg
	} else {
		bg = vis.Alpha(tintAlpha)
	}
	effBg := bg.Over(surface)
	fg = ensureColorContrast(vis, effBg, minContrast)
	return fg, bg
}

func isASCIIAlphaNum(b byte) bool {
	return (b >= '0' && b <= '9') || (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z')
}

func isHexPrevDisallowed(b byte) bool {
	return isASCIIAlphaNum(b) || b == '_' || b == '/' || b == '&' || b == '#'
}

func isFuncPrevDisallowed(b byte) bool {
	return isASCIIAlphaNum(b) || b == '_' || b == '.' || b == '-'
}

func hasFoldPrefix(s, prefix string) bool {
	if len(s) < len(prefix) {
		return false
	}
	for i := range len(prefix) {
		c := s[i]
		if c >= 'A' && c <= 'Z' {
			c += 'a' - 'A'
		}
		if c != prefix[i] {
			return false
		}
	}
	return true
}

func matchBalancedParen(s string, openIdx, maxLen int) int {
	limit := min(len(s), openIdx+maxLen)
	depth := 0
	for i := openIdx; i < limit; i++ {
		switch s[i] {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return i
			}
		case '\n':
			return -1
		}
	}
	return -1
}

// findNextColorLiteral finds the next color literal (or linear-gradient) in
// plain prose or code block text starting at from.
func findNextColorLiteral(text string, from int) (start, end int, col ui.Color, grad []ui.Color, ok bool) {
	for i := from; i < len(text); i++ {
		b := text[i]
		switch b {
		case '#':
			if i > 0 && isHexPrevDisallowed(text[i-1]) {
				continue
			}
			j := i + 1
			for j < len(text) && isHexDigit(text[j]) {
				j++
			}
			n := j - (i + 1)
			if (n == 6 || n == 8) && (j == len(text) || (!isASCIIAlphaNum(text[j]) && text[j] != '_')) {
				if c, parsed := parseHexColor(text[i+1:j], false); parsed {
					return i, j, c, nil, true
				}
			}
		case 'r', 'R', 'h', 'H', 'o', 'O', 'l', 'L', 'c', 'C':
			if i > 0 && isFuncPrevDisallowed(text[i-1]) {
				continue
			}
			rest := text[i:]
			if hasFoldPrefix(rest, "linear-gradient(") {
				if closeIdx := matchBalancedParen(rest, len("linear-gradient(")-1, 180); closeIdx > 0 {
					if stops, parsed := parseLinearGradient(rest[:closeIdx+1]); parsed {
						return i, i + closeIdx + 1, stops[0], stops, true
					}
				}
				continue
			}
			var openAt int
			switch {
			case hasFoldPrefix(rest, "rgba("):
				openAt = 4
			case hasFoldPrefix(rest, "rgb("):
				openAt = 3
			case hasFoldPrefix(rest, "hsla("):
				openAt = 4
			case hasFoldPrefix(rest, "hsl("):
				openAt = 3
			case hasFoldPrefix(rest, "hwb("):
				openAt = 3
			case hasFoldPrefix(rest, "oklch("):
				openAt = 5
			case hasFoldPrefix(rest, "oklab("):
				openAt = 5
			case hasFoldPrefix(rest, "lab("):
				openAt = 3
			case hasFoldPrefix(rest, "lch("):
				openAt = 3
			case hasFoldPrefix(rest, "color("):
				openAt = 5
			default:
				continue
			}
			if closeIdx := matchBalancedParen(rest, openAt, 64); closeIdx > openAt {
				if c, parsed := parseColorLiteral(rest[:closeIdx+1], false); parsed {
					return i, i + closeIdx + 1, c, nil, true
				}
			}
		}
	}
	return 0, 0, ui.Color{}, nil, false
}

func drawCheckerboard(pt *ui.Painter, sq ui.Rect, rad float32, dark bool, fadeAlpha float32) {
	cLight := ui.RGB(240, 242, 246).Alpha(fadeAlpha)
	cDark := ui.RGB(178, 184, 196).Alpha(fadeAlpha)
	if dark {
		cLight = ui.RGB(148, 156, 170).Alpha(fadeAlpha)
		cDark = ui.RGB(68, 74, 88).Alpha(fadeAlpha)
	}
	cell := max(float32(3.5), sq.H*0.5)
	pt.Clip(sq, rad, func() {
		pt.Fill(sq, cLight, 0)
		for row := 0; float32(row)*cell < sq.H; row++ {
			for col := 0; float32(col)*cell < sq.W; col++ {
				if (row+col)%2 == 1 {
					x := sq.X + float32(col)*cell
					y := sq.Y + float32(row)*cell
					pt.Fill(ui.Rect{X: x, Y: y, W: min(cell, sq.X+sq.W-x), H: min(cell, sq.Y+sq.H-y)}, cDark, 0)
				}
			}
		}
	})
}

func makeColorSwatchDraw(raw ui.Color, d, lead float32, dark bool, borderBase ui.Color, fadeAlpha float32) func(*ui.Painter, ui.Rect) {
	return func(pt *ui.Painter, r ui.Rect) {
		sq := ui.Rect{
			X: r.X + lead,
			Y: r.Y + (r.H-d)*0.5 - 0.5,
			W: d,
			H: d,
		}
		rad := max(float32(2.0), d*0.28)
		if raw.A < 255 {
			drawCheckerboard(pt, sq, rad, dark, fadeAlpha)
		}
		pt.Fill(sq, raw.Alpha(fadeAlpha), rad)
		borderAlpha := float32(0.22)
		if dark {
			borderAlpha = 0.28
		}
		pt.Stroke(sq, borderBase.Alpha(borderAlpha*fadeAlpha), rad, 0.75)
	}
}

func makeGradientSwatchDraw(stops []ui.Color, w, h, lead float32, dark bool, borderBase ui.Color, fadeAlpha float32) func(*ui.Painter, ui.Rect) {
	hasAlpha := false
	gStops := make([]ui.GradientStop, len(stops))
	for i, c := range stops {
		if c.A < 255 {
			hasAlpha = true
		}
		pos := float32(0)
		if len(stops) > 1 {
			pos = float32(i) / float32(len(stops)-1)
		}
		gStops[i] = ui.GradientStop{At: pos, Color: c.Alpha(fadeAlpha)}
	}
	grad := ui.LinearGradient{Angle: 90, Stops: gStops}
	return func(pt *ui.Painter, r ui.Rect) {
		sq := ui.Rect{
			X: r.X + lead,
			Y: r.Y + (r.H-h)*0.5 - 0.5,
			W: w,
			H: h,
		}
		rad := max(float32(2.0), h*0.28)
		if hasAlpha {
			drawCheckerboard(pt, sq, rad, dark, fadeAlpha)
		}
		pt.FillGradient(sq, grad, rad)
		borderAlpha := float32(0.22)
		if dark {
			borderAlpha = 0.28
		}
		pt.Stroke(sq, borderBase.Alpha(borderAlpha*fadeAlpha), rad, 0.75)
	}
}
