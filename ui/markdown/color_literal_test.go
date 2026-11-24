package markdown

import (
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/markdown/parser"
)

func TestParseColorLiteral(t *testing.T) {
	cases := []struct {
		in        string
		codeForms bool
		want      ui.Color
		ok        bool
	}{
		{"#10a37f", false, ui.Color{R: 0x10, G: 0xa3, B: 0x7f, A: 255}, true},
		{"#6C63FF", true, ui.Color{R: 0x6c, G: 0x63, B: 0xff, A: 255}, true},
		{"#10a37f80", false, ui.Color{R: 0x10, G: 0xa3, B: 0x7f, A: 0x80}, true},
		{"#fff", true, ui.Color{R: 255, G: 255, B: 255, A: 255}, true},
		{"#f0a8", true, ui.Color{R: 255, G: 0, B: 170, A: 136}, true},
		{"#fff", false, ui.Color{}, false},
		{"#123", false, ui.Color{}, false},
		{"#1234", false, ui.Color{}, false},
		{"#10a37f0", true, ui.Color{}, false},
		{"#zzzzzz", true, ui.Color{}, false},
		{"rgb(16, 163, 127)", false, ui.Color{R: 16, G: 163, B: 127, A: 255}, true},
		{"rgba(16, 163, 127, 0.5)", false, ui.Color{R: 16, G: 163, B: 127, A: 128}, true},
		{"rgb(16 163 127 / 50%)", false, ui.Color{R: 16, G: 163, B: 127, A: 128}, true},
		{"rgb(100%, 0%, 50%)", false, ui.Color{R: 255, G: 0, B: 128, A: 255}, true},
		{"rgb(300, 0, 0)", false, ui.Color{}, false},
		{"hsl(165, 82%, 35%)", false, ui.Color{R: 16, G: 162, B: 126, A: 255}, true},
		{"hsla(243deg 100% 69% / 0.8)", false, ui.Color{R: 105, G: 97, B: 255, A: 204}, true},
		{"hsl(120, 50, 50)", false, ui.Color{}, false},
		{"hwb(165 6% 36%)", false, ui.Color{R: 15, G: 163, B: 126, A: 255}, true},
		{"oklch(0.62 0.14 166)", false, oklchToColor(0.62, 0.14, 166), true},
		{"oklab(0.62 -0.11 0.04)", false, oklabToColor(0.62, -0.11, 0.04), true},
		{"lab(58 -45 14)", false, cieLabToColor(58, -45, 14), true},
		{"lch(58 47 162)", false, cieLabToColor(58, 47*0.9510565162951535*-1, 47*0.30901699437494745), true},
		{"color(srgb 0.06 0.64 0.50)", false, ui.Color{R: 15, G: 163, B: 128, A: 255}, true},
		{"color(display-p3 0.2 0.8 0.5 / 50%)", false, func() ui.Color { c, _ := parseCSSColorFunc("display-p3 0.2 0.8 0.5 / 50%"); return c }(), true},
		// Code-only constructors & Tailwind palette
		{`ui.Hex("#10a37f")`, true, ui.Color{R: 0x10, G: 0xa3, B: 0x7f, A: 255}, true},
		{`ui.RGB(16, 163, 127)`, true, ui.Color{R: 16, G: 163, B: 127, A: 255}, true},
		{`ui.RGBA(16, 163, 127, 0.5)`, true, ui.Color{R: 16, G: 163, B: 127, A: 128}, true},
		{`Color(red: 0.06, green: 0.64, blue: 0.50)`, true, ui.Color{R: 15, G: 163, B: 128, A: 255}, true},
		{`UIColor(red: 0.42, green: 0.39, blue: 1.0, alpha: 0.5)`, true, ui.Color{R: 107, G: 99, B: 255, A: 128}, true},
		{"emerald-500", true, ui.Hex("#10b981"), true},
		{"bg-indigo-500/80", true, ui.Hex("#6366f1").Alpha(0.8), true},
		{"emerald-500", false, ui.Color{}, false},
		{"grok", true, ui.Color{}, false},
		{"0x10a37f", true, ui.Color{}, false},
	}
	for _, tc := range cases {
		got, ok := parseColorLiteral(tc.in, tc.codeForms)
		if ok != tc.ok {
			t.Errorf("parseColorLiteral(%q, %v) ok=%v, want %v", tc.in, tc.codeForms, ok, tc.ok)
			continue
		}
		if ok && got != tc.want {
			t.Errorf("parseColorLiteral(%q, %v) = %+v, want %+v", tc.in, tc.codeForms, got, tc.want)
		}
	}
}

func TestParseLinearGradient(t *testing.T) {
	stops, ok := parseLinearGradient("linear-gradient(135deg, #10a37f 0%, #6c63ff 50%, rgba(224, 122, 95, 0.8) 100%)")
	if !ok || len(stops) != 3 {
		t.Fatalf("expected 3 stops, got ok=%v stops=%+v", ok, stops)
	}
	if stops[0] != ui.Hex("#10a37f") || stops[1] != ui.Hex("#6c63ff") {
		t.Fatalf("unexpected gradient stops: %+v", stops)
	}
	if _, ok := parseLinearGradient("linear-gradient(to right, #10a37f)"); ok {
		t.Fatal("single-stop gradient must not parse")
	}
}

func TestHTTPBadgesTimestampsAndCron(t *testing.T) {
	// HTTP methods & statuses
	for _, good := range []string{"GET", "POST", "DELETE", "GET /api/v1/sessions", "200 OK", "404 Not Found", "503 Service Unavailable", "HTTP 429"} {
		if _, _, ok := parseHTTPBadge(good); !ok {
			t.Errorf("expected HTTP badge match for %q", good)
		}
	}
	for _, bad := range []string{"200", "404", "get", "GETTING", "POST foo"} {
		if _, _, ok := parseHTTPBadge(bad); ok {
			t.Errorf("unexpected HTTP badge match for %q", bad)
		}
	}

	// Timestamps
	if ns, hasZone, ok := parseTimestampLiteral("2026-11-23T01:40:40+08:00", false); !ok || !hasZone || ns == 0 {
		t.Fatalf("expected ISO timestamp match, got ok=%v hasZone=%v ns=%d", ok, hasZone, ns)
	}
	if ns, _, ok := parseTimestampLiteral("1795369240", true); !ok || time.Unix(0, ns).Unix() != 1795369240 {
		t.Fatalf("expected 10-digit unix timestamp match, got ok=%v ns=%d", ok, ns)
	}
	if _, _, ok := parseTimestampLiteral("1795369240", false); ok {
		t.Fatal("bare unix timestamp must not match outside code spans")
	}
	if _, _, ok := parseTimestampLiteral("1234567890", true); !ok {
		t.Fatal("2009 unix timestamp should match in code spans")
	}
	if _, _, ok := parseTimestampLiteral("0000000123", true); ok {
		t.Fatal("small padded int must not match unix timestamp")
	}

	// Cron
	cs, ok := ParseCron("*/5 * * * *")
	if !ok {
		t.Fatal("expected valid cron for */5 * * * *")
	}
	if got := cs.Describe(true); !strings.Contains(got, "5") {
		t.Fatalf("unexpected zh cron description: %q", got)
	}
	if got := cs.Describe(false); !strings.Contains(strings.ToLower(got), "5 minutes") {
		t.Fatalf("unexpected en cron description: %q", got)
	}
	base := time.Date(2026, 11, 23, 1, 41, 0, 0, time.UTC)
	runs := cs.NextRuns(base, 3)
	if len(runs) != 3 || runs[0].Minute() != 45 || runs[1].Minute() != 50 || runs[2].Minute() != 55 {
		t.Fatalf("unexpected next runs: %v", runs)
	}
	if cs2, ok := ParseCron("0 9 * * 1-5"); !ok || !strings.Contains(cs2.Describe(true), "工作日") {
		t.Fatalf("expected weekday cron description, got ok=%v desc=%q", ok, cs2.Describe(true))
	}
	if _, ok := ParseCron("1 2 3 4 5"); ok {
		t.Fatal("plain 5 numbers without cron syntax must not match")
	}
}

func TestEnsureColorContrast(t *testing.T) {
	darkBg := ui.Hex("#161a28")
	lightBg := ui.Hex("#f6f7fb")
	samples := []ui.Color{
		ui.Hex("#10a37f"),
		ui.Hex("#6c63ff"),
		ui.Hex("#7c6df0"),
		ui.Hex("#000000"),
		ui.Hex("#ffffff"),
		ui.Hex("#facc15"),
		ui.Hex("#cc5f43"),
	}
	for _, raw := range samples {
		for _, bg := range []ui.Color{darkBg, lightBg} {
			got := ensureColorContrast(raw, bg, 4.5)
			if cr := contrastRatio(got, bg); cr < 4.4 {
				t.Errorf("contrastRatio(%+v -> %+v, %+v) = %.2f, want >= 4.4", raw, got, bg, cr)
			}
		}
	}
}

func TestCodeBlockColorSwatchesAndCopy(t *testing.T) {
	src := "```css\n.btn {\n  color: #10a37f;\n  background: rgba(108, 99, 255, 0.25);\n  border-image: linear-gradient(90deg, #10a37f, #6c63ff);\n}\n```\n"
	o := &Options{ColorLiterals: true}
	var st *viewState
	tt := ui.NewTesterWith(func(c *ui.Context) {
		st = *ui.Local(View(c, src, o), stateKey{}, newState)
	}, 720, 260, ui.TesterOptions{Dark: true})

	if len(st.codes) == 0 {
		t.Fatal("expected code block state")
	}
	var objs int
	for _, cs := range st.codes {
		for _, ch := range cs.chunks {
			objs += len(ch.objects)
		}
	}
	if objs != 3 {
		t.Fatalf("expected 3 swatch objects inside CSS code block, got %d", objs)
	}

	// Select all and copy must strip swatch objects and joiners cleanly.
	var codeRect ui.Rect
	found := false
	for _, txt := range tt.Texts() {
		if strings.Contains(txt, "#10a37f") {
			codeRect, found = tt.Find(txt)
			break
		}
	}
	if !found {
		t.Fatalf("expected code block text in %q", tt.Texts())
	}
	tt.ClickAt(codeRect.X+4, codeRect.Y+codeRect.H/2)
	tt.Command("selectAll")
	tt.Command("copy")
	copied := tt.Clipboard()
	if strings.ContainsRune(copied, '\uFFFC') || strings.ContainsRune(copied, linkIconJoiner) {
		t.Fatalf("code block clipboard leaked inline object or joiner: %q", copied)
	}
	if !strings.Contains(copied, "color: #10a37f;") || !strings.Contains(copied, "background: rgba(108, 99, 255, 0.25);") {
		t.Fatalf("code block clipboard content unexpected: %q", copied)
	}
}

func TestStandaloneImageAndCustomMediaBlockCentered(t *testing.T) {
	// 1. Standalone image paragraph centers horizontally within the markdown column.
	srcImg := "Hello world\n\n![preview](demo.png)\n"
	oImg := &Options{}
	tt := ui.NewTester(func(c *ui.Context) {
		ui.Column(c).Width(720).Children(func() { View(c, srcImg, oImg) })
	}, 720, 200)
	imgRect, ok := tt.Find("preview")
	if !ok {
		t.Fatalf("expected image placeholder in %q", tt.Texts())
	}
	imgMid := imgRect.X + imgRect.W*0.5
	if diff := imgMid - 360; diff < -8 || diff > 8 {
		t.Fatalf("expected standalone image centered at x=360, got mid=%.1f rect=%+v", imgMid, imgRect)
	}

	// 2. Custom Options.Block container stretches to full column width so centered media cards align to column center.
	srcBlock := "Intro line\n\n![video](demo.mp4)\n"
	oBlock := &Options{
		Block: func(c *ui.Context, d *parser.Document, i int) bool {
			if d.Block(i).Kind != parser.Paragraph || !strings.Contains(d.Source()[d.Block(i).Start:d.Block(i).End], "demo.mp4") {
				return false
			}
			ui.Column(c).FillWidth().AlignItems(ui.Center).Children(func() {
				ui.Box(c).Width(320).Height(120).Center().Children(func() {
					ui.Text(c, "VIDEO-CARD")
				})
			})
			return true
		},
	}
	tt2 := ui.NewTester(func(c *ui.Context) {
		ui.Column(c).Width(720).Children(func() { View(c, srcBlock, oBlock) })
	}, 720, 240)
	cardRect, ok := tt2.Find("VIDEO-CARD")
	if !ok {
		t.Fatalf("expected custom media card in %q", tt2.Texts())
	}
	cardMid := cardRect.X + cardRect.W*0.5
	if diff := cardMid - 360; diff < -8 || diff > 8 {
		t.Fatalf("expected custom media block centered at x=360, got mid=%.1f rect=%+v", cardMid, cardRect)
	}
}

func TestColorLiteralsRenderAndCopy(t *testing.T) {
	src := "1. OpenAI 翡翠绿（`#10a37f` — 区分度最高）\n2. Codex 紫蓝（`#6c63ff`/`#7c6df0`）与纯黑 #000000 和 #10a37f，不是 #123 或 foo#10a37f 或 `#10a37f0`。\n"
	var clickedTok InlineToken
	o := &Options{
		ColorLiterals: true,
		RichTokens:    true,
		OnTokenClick: func(_ *ui.Context, tok InlineToken) {
			clickedTok = tok
		},
	}
	var st *viewState
	tt := ui.NewTesterWith(func(c *ui.Context) {
		st = *ui.Local(View(c, src, o), stateKey{}, newState)
	}, 720, 260, ui.TesterOptions{Dark: true})

	// Inspect built spans across list item paragraphs.
	coloredCodeBg := map[string]ui.Color{}
	coloredCodeFg := map[string]ui.Color{}
	var coloredProse, uncolored []string
	for i := range st.paras {
		if !st.paras[i].ok {
			continue
		}
		for _, pc := range st.paras[i].p.pieces {
			for _, sp := range pc.spans {
				if sp.Text == "\uFFFC" || sp.Text == string(linkIconJoiner) {
					continue
				}
				if sp.Color != (ui.Color{}) && sp.Color != st.th.Code && sp.Color != st.th.Text {
					if sp.Background.A > 0 {
						coloredCodeBg[sp.Text] = sp.Background
						coloredCodeFg[sp.Text] = sp.Color
					} else {
						coloredProse = append(coloredProse, sp.Text)
					}
				} else if strings.Contains(sp.Text, "#123") || strings.Contains(sp.Text, "foo#10a37f") || sp.Text == "#10a37f0" {
					uncolored = append(uncolored, sp.Text)
				}
			}
		}
	}
	for _, want := range []string{"#10a37f", "#6c63ff", "#7c6df0"} {
		if _, ok := coloredCodeFg[want]; !ok {
			t.Fatalf("expected inline code %q to receive custom color", want)
		}
		if coloredCodeBg[want] == st.th.CodeBackground {
			t.Fatalf("expected inline code %q to receive tinted background, still default %+v", want, st.th.CodeBackground)
		}
	}
	foundBlack, foundGreen := false, false
	for _, s := range coloredProse {
		if s == "#000000" {
			foundBlack = true
		}
		if s == "#10a37f" {
			foundGreen = true
		}
	}
	if !foundBlack || !foundGreen {
		t.Fatalf("expected plain-text #000000 and #10a37f to be styled; got coloredProse=%v", coloredProse)
	}
	if len(uncolored) == 0 {
		t.Fatalf("expected non-colors (#123, foo#10a37f, #10a37f0) to remain uncolored")
	}

	// Clicking an interactive color token fires OnTokenClick.
	if tokRect, ok := tt.Find("\uFFFC\u2060#10a37f"); ok {
		tt.ClickAt(tokRect.X+tokRect.W/2, tokRect.Y+tokRect.H/2)
		if clickedTok.Kind != TokenColor || clickedTok.Raw != "#10a37f" {
			t.Fatalf("expected OnTokenClick for #10a37f, got %+v", clickedTok)
		}
	}

	// Select all and copy must strip swatch objects and word joiners cleanly.
	r, ok := tt.Find("OpenAI 翡翠绿（\uFFFC\u2060#10a37f — 区分度最高）")
	if !ok {
		t.Fatalf("expected rendered text in %q", tt.Texts())
	}
	tt.ClickAt(r.X+2, r.Y+r.H/2)
	tt.Command("selectAll")
	tt.Command("copy")
	copied := tt.Clipboard()
	if strings.ContainsRune(copied, '\uFFFC') || strings.ContainsRune(copied, linkIconJoiner) {
		t.Fatalf("clipboard leaked inline object or joiner: %q", copied)
	}
	if !strings.Contains(copied, "#10a37f") || !strings.Contains(copied, "#6c63ff/#7c6df0") {
		t.Fatalf("clipboard missing expected color tokens: %q", copied)
	}
	if md := st.copyMarkdown(nil); md != src {
		t.Fatalf("copyMarkdown changed source:\ngot:  %q\nwant: %q", md, src)
	}

	// Pixel check: emerald green pixels should be rendered for #10a37f.
	emerald := ui.Hex("#10a37f")
	img := tt.Image()
	if n := colorPixels(img, ui.Rect{X: 0, Y: 0, W: 720, H: 260}, emerald); n == 0 {
		t.Fatalf("expected emerald pixels near %+v in rendered image", emerald)
	}

	// Toggling ColorLiterals off restores default styling cleanly.
	o.ColorLiterals = false
	o.RichTokens = false
	tt.Frame()
	for i := range st.paras {
		if !st.paras[i].ok {
			continue
		}
		if len(st.paras[i].p.objects) != 0 {
			t.Fatalf("expected 0 swatch objects after disabling ColorLiterals, got %d", len(st.paras[i].p.objects))
		}
	}

	if dir := os.Getenv("MYGO_MD_RENDER_DIR"); dir != "" {
		sample := "### 1. 颜色与渐变（悬停看色卡，点击直接复制）\n\n" +
			"1. **OpenAI 翡翠绿**（`#10a37f` — 和 Antigravity 蓝、Claude 橙区分度最高）\n" +
			"2. **Codex 应用紫蓝**（`#6c63ff`/`#7c6df0` — 贴近 Codex App 图标）\n" +
			"3. **广色域与框架色**：`color(display-p3 0.15 0.72 0.56)`、`oklab(0.62 -0.11 0.04)`、`hwb(165 6% 36%)`、`ui.Hex(\"#e07a5f\")`、`Color(red: 0.42, green: 0.39, blue: 1.0)`、`bg-emerald-500`、`indigo-500/80`\n" +
			"4. **渐变条**：`linear-gradient(135deg, #10a37f, #6c63ff, #e07a5f)`\n\n" +
			"### 2. 按键帽、HTTP 语义标、时间与 Cron\n\n" +
			"- 快捷键：按 <kbd>⌘K</kbd> 打开命令面板，按 <kbd>⌘</kbd>+<kbd>⇧</kbd>+<kbd>P</kbd> 切换工作区\n" +
			"- 接口：`GET /api/v1/sessions` 返回 `200 OK`，`POST /api/v1/turns` 触发 `429 Too Many Requests`，`DELETE /api/v1/cache` 遇 `503 Service Unavailable`\n" +
			"- 时间与定时：构建于 2026-11-23T01:40:40+08:00（时间戳 `1795369240`），定时巡检 `*/5 * * * *` 与工作日 `0 9 * * 1-5`\n\n" +
			"```css\n.kong-badge {\n  color: #10a37f;\n  background: rgba(108, 99, 255, 0.22);\n  border-color: oklch(0.68 0.16 42);\n  background-image: linear-gradient(90deg, #10a37f, #6c63ff);\n}\n```\n"

		for _, mode := range []struct {
			name string
			dark bool
			bg   ui.Color
			text ui.Color
			cbg  ui.Color
		}{
			{"dark", true, ui.Hex("#1c1d22"), ui.Hex("#eef0f8"), ui.Hex("#eef0f8").Alpha(0.095)},
			{"light", false, ui.Hex("#f6f7fb"), ui.Hex("#171926"), ui.Hex("#171926").Alpha(0.052)},
		} {
			opts := &Options{
				ColorLiterals: true,
				RichTokens:    true,
				Theme: &Theme{
					Font:           "PingFang SC",
					CodeFont:       "Menlo",
					FontSize:       14,
					LineHeight:     1.62,
					CodeSize:       0.87,
					Text:           mode.text,
					Strong:         mode.text,
					StrongWeight:   600,
					Code:           mode.text.Alpha(0.88),
					CodeBackground: mode.cbg,
				},
			}
			rt := ui.NewTesterWith(func(c *ui.Context) {
				ui.Column(c).Fill().Background(mode.bg).Padding(24).Children(func() {
					View(c, sample, opts)
				})
			}, 840, 560, ui.TesterOptions{Dark: mode.dark})
			rt.SetScale(2)
			rt.Frame()
			f, err := os.Create(filepath.Join(dir, "md_rich_tokens_"+mode.name+".png"))
			if err != nil {
				t.Fatal(err)
			}
			if err := png.Encode(f, rt.Image()); err != nil {
				_ = f.Close()
				t.Fatal(err)
			}
			_ = f.Close()
		}
	}
}
