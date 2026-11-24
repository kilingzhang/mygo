package markdown

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/markdown/parser"
)

// TokenKind identifies the semantic type of an enriched inline Markdown token.
type TokenKind uint8

const (
	TokenNone TokenKind = iota
	TokenColor
	TokenGradient
	TokenTimestamp
	TokenCron
)

// InlineToken describes an interactive enriched inline token inside a
// paragraph (color literal, CSS gradient, timestamp, or cron expression).
type InlineToken struct {
	Kind     TokenKind
	Raw      string
	Color    Color
	Gradient []Color
	UnixNano int64
	HasZone  bool
}

// httpBadgeStyle returns a semantic accent color and prefix length for HTTP
// methods (e.g. "GET", "POST /api/users") and HTTP status codes (e.g. "200 OK",
// "404 Not Found", "HTTP 500") inside inline code spans.
func parseHTTPBadge(code string) (accent ui.Color, prefixLen int, ok bool) {
	s := strings.TrimSpace(code)
	if s == "" || len(s) > 96 {
		return ui.Color{}, 0, false
	}
	// Standalone HTTP method or "METHOD /path" / "METHOD http..."
	methodColor := func(m string) (ui.Color, bool) {
		switch m {
		case "GET":
			return ui.Hex("#10b981"), true
		case "POST":
			return ui.Hex("#3b82f6"), true
		case "PUT", "PATCH":
			return ui.Hex("#f59e0b"), true
		case "DELETE":
			return ui.Hex("#ef4444"), true
		case "HEAD", "OPTIONS":
			return ui.Hex("#8b5cf6"), true
		default:
			return ui.Color{}, false
		}
	}
	if c, matched := methodColor(s); matched {
		return c, len(s), true
	}
	if sp := strings.IndexByte(s, ' '); sp > 0 && sp < len(s)-1 {
		head := s[:sp]
		rest := strings.TrimSpace(s[sp+1:])
		if c, matched := methodColor(head); matched && (strings.HasPrefix(rest, "/") || strings.HasPrefix(rest, "http://") || strings.HasPrefix(rest, "https://")) {
			return c, sp, true
		}
		// "HTTP 200" / "HTTP 404" / "HTTP 503"
		if head == "HTTP" {
			if status, err := strconv.Atoi(rest); err == nil && status >= 100 && status <= 599 {
				if c, matched := httpStatusColor(status); matched {
					return c, len(s), true
				}
			}
		}
		// "200 OK", "404 Not Found", "500 Internal Server Error", etc.
		if len(head) == 3 {
			if status, err := strconv.Atoi(head); err == nil && isKnownHTTPStatus(status, rest) {
				if c, matched := httpStatusColor(status); matched {
					return c, len(s), true
				}
			}
		}
	}
	return ui.Color{}, 0, false
}

func httpStatusColor(code int) (ui.Color, bool) {
	switch {
	case code >= 200 && code <= 299:
		return ui.Hex("#10b981"), true
	case code >= 300 && code <= 399:
		return ui.Hex("#0ea5e9"), true
	case code >= 400 && code <= 499:
		return ui.Hex("#f59e0b"), true
	case code >= 500 && code <= 599:
		return ui.Hex("#ef4444"), true
	default:
		return ui.Color{}, false
	}
}

func isKnownHTTPStatus(code int, reason string) bool {
	want, ok := knownHTTPStatuses[code]
	return ok && strings.EqualFold(strings.TrimSpace(reason), want)
}

var knownHTTPStatuses = map[int]string{
	200: "OK",
	201: "Created",
	202: "Accepted",
	204: "No Content",
	206: "Partial Content",
	301: "Moved Permanently",
	302: "Found",
	304: "Not Modified",
	307: "Temporary Redirect",
	308: "Permanent Redirect",
	400: "Bad Request",
	401: "Unauthorized",
	403: "Forbidden",
	404: "Not Found",
	405: "Method Not Allowed",
	408: "Request Timeout",
	409: "Conflict",
	410: "Gone",
	413: "Payload Too Large",
	415: "Unsupported Media Type",
	422: "Unprocessable Entity",
	429: "Too Many Requests",
	500: "Internal Server Error",
	501: "Not Implemented",
	502: "Bad Gateway",
	503: "Service Unavailable",
	504: "Gateway Timeout",
}

// parseTimestampLiteral parses RFC3339/ISO-8601 timestamps (with explicit zone)
// and, when allowUnix is true (inline code spans), 10-digit Unix seconds or
// 13-digit Unix milliseconds within [2001, 2049].
func parseTimestampLiteral(raw string, allowUnix bool) (unixNano int64, hasZone bool, ok bool) {
	s := strings.TrimSpace(raw)
	if len(s) < 10 || len(s) > 40 {
		return 0, false, false
	}
	if allowUnix && (len(s) == 10 || len(s) == 13) && isAllDigits(s) {
		v, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			return 0, false, false
		}
		if len(s) == 10 && v >= 1_000_000_000 && v <= 2_500_000_000 {
			return time.Unix(v, 0).UnixNano(), false, true
		}
		if len(s) == 13 && v >= 1_000_000_000_000 && v <= 2_500_000_000_000 {
			return time.UnixMilli(v).UnixNano(), false, true
		}
		return 0, false, false
	}
	if len(s) >= 20 && s[4] == '-' && s[7] == '-' && (s[10] == 'T' || s[10] == 't') && s[13] == ':' && s[16] == ':' {
		if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
			return t.UnixNano(), true, true
		}
		if t, err := time.Parse(time.RFC3339, s); err == nil {
			return t.UnixNano(), true, true
		}
	}
	return 0, false, false
}

func isAllDigits(s string) bool {
	if s == "" {
		return false
	}
	for i := range len(s) {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

// findNextISOTimestamp finds an RFC3339/ISO-8601 timestamp with explicit zone
// in plain prose starting at from.
func findNextISOTimestamp(text string, from int) (start, end int, unixNano int64, ok bool) {
	for i := from; i+20 <= len(text); i++ {
		if text[i] < '1' || text[i] > '2' {
			continue
		}
		if i > 0 && isASCIIAlphaNum(text[i-1]) {
			continue
		}
		if text[i+4] != '-' || text[i+7] != '-' || (text[i+10] != 'T' && text[i+10] != 't') || text[i+13] != ':' || text[i+16] != ':' {
			continue
		}
		j := i + 19
		if j < len(text) && text[j] == '.' {
			j++
			for j < len(text) && text[j] >= '0' && text[j] <= '9' {
				j++
			}
		}
		if j >= len(text) {
			continue
		}
		switch text[j] {
		case 'Z', 'z':
			j++
		case '+', '-':
			if j+6 <= len(text) && text[j+3] == ':' {
				j += 6
			} else if j+5 <= len(text) && isAllDigits(text[j+1:j+5]) {
				j += 5
			} else {
				continue
			}
		default:
			continue
		}
		if j < len(text) && isASCIIAlphaNum(text[j]) {
			continue
		}
		if ns, _, parsed := parseTimestampLiteral(text[i:j], false); parsed {
			return i, j, ns, true
		}
	}
	return 0, 0, 0, false
}

// CronSchedule represents a validated 5-field standard cron schedule.
type CronSchedule struct {
	Raw     string
	Min     [60]bool
	Hour    [24]bool
	Dom     [32]bool
	Month   [13]bool
	Dow     [7]bool
	DomWild bool
	DowWild bool
	Fields  [5]string
}

var cronMonthNames = map[string]int{
	"JAN": 1, "FEB": 2, "MAR": 3, "APR": 4, "MAY": 5, "JUN": 6,
	"JUL": 7, "AUG": 8, "SEP": 9, "OCT": 10, "NOV": 11, "DEC": 12,
}

var cronDowNames = map[string]int{
	"SUN": 0, "MON": 1, "TUE": 2, "WED": 3, "THU": 4, "FRI": 5, "SAT": 6,
}

// ParseCron parses a strict 5-field cron expression (min hour dom month dow).
func ParseCron(expr string) (*CronSchedule, bool) {
	s := strings.TrimSpace(expr)
	if len(s) < 9 || len(s) > 64 {
		return nil, false
	}
	fields := strings.Fields(s)
	if len(fields) != 5 {
		return nil, false
	}
	// Require at least one wildcard/step/range so normal 5-number strings don't match.
	hasCronSyntax := false
	for _, f := range fields {
		if strings.ContainsAny(f, "*/-,") {
			hasCronSyntax = true
			break
		}
	}
	if !hasCronSyntax {
		return nil, false
	}
	cs := &CronSchedule{Raw: s}
	copy(cs.Fields[:], fields)
	if !parseCronField(fields[0], 0, 59, nil, cs.Min[:]) {
		return nil, false
	}
	if !parseCronField(fields[1], 0, 23, nil, cs.Hour[:]) {
		return nil, false
	}
	if !parseCronField(fields[2], 1, 31, nil, cs.Dom[:]) {
		return nil, false
	}
	if !parseCronField(fields[3], 1, 12, cronMonthNames, cs.Month[:]) {
		return nil, false
	}
	var dowTmp [8]bool
	if !parseCronField(fields[4], 0, 7, cronDowNames, dowTmp[:]) {
		return nil, false
	}
	for i := range 7 {
		cs.Dow[i] = dowTmp[i]
	}
	if dowTmp[7] {
		cs.Dow[0] = true
	}
	cs.DomWild = fields[2] == "*"
	cs.DowWild = fields[4] == "*"
	return cs, true
}

func parseCronField(field string, minV, maxV int, names map[string]int, out []bool) bool {
	if field == "" {
		return false
	}
	parts := strings.Split(field, ",")
	for _, p := range parts {
		if p == "" {
			return false
		}
		step := 1
		rangePart := p
		if slash := strings.IndexByte(p, '/'); slash >= 0 {
			if strings.IndexByte(p[slash+1:], '/') >= 0 {
				return false
			}
			st, err := strconv.Atoi(p[slash+1:])
			if err != nil || st <= 0 || st > maxV-minV+1 {
				return false
			}
			step = st
			rangePart = p[:slash]
		}
		var lo, hi int
		if rangePart == "*" {
			lo, hi = minV, maxV
		} else if dash := strings.IndexByte(rangePart, '-'); dash >= 0 {
			if strings.IndexByte(rangePart[dash+1:], '-') >= 0 {
				return false
			}
			var ok1, ok2 bool
			lo, ok1 = parseCronAtom(rangePart[:dash], minV, maxV, names)
			hi, ok2 = parseCronAtom(rangePart[dash+1:], minV, maxV, names)
			if !ok1 || !ok2 || lo > hi {
				return false
			}
		} else {
			var ok bool
			lo, ok = parseCronAtom(rangePart, minV, maxV, names)
			if !ok {
				return false
			}
			if strings.ContainsRune(p, '/') {
				hi = maxV
			} else {
				hi = lo
			}
		}
		for v := lo; v <= hi; v += step {
			out[v] = true
		}
	}
	return true
}

func parseCronAtom(s string, minV, maxV int, names map[string]int) (int, bool) {
	if s == "" {
		return 0, false
	}
	if names != nil {
		if v, ok := names[strings.ToUpper(s)]; ok {
			return v, true
		}
	}
	if !isAllDigits(s) {
		return 0, false
	}
	v, err := strconv.Atoi(s)
	if err != nil || v < minV || v > maxV {
		return 0, false
	}
	return v, true
}

func (cs *CronSchedule) matchesDay(t time.Time) bool {
	if !cs.Month[int(t.Month())] {
		return false
	}
	domOK := cs.Dom[t.Day()]
	dowOK := cs.Dow[int(t.Weekday())]
	if cs.DomWild && cs.DowWild {
		return true
	}
	if cs.DomWild {
		return dowOK
	}
	if cs.DowWild {
		return domOK
	}
	return domOK || dowOK
}

// NextRuns computes up to n upcoming run times strictly after from (in from's location).
func (cs *CronSchedule) NextRuns(from time.Time, n int) []time.Time {
	if cs == nil || n <= 0 {
		return nil
	}
	cur := from.Truncate(time.Minute).Add(time.Minute)
	limit := cur.AddDate(4, 0, 0)
	runs := make([]time.Time, 0, n)
	for len(runs) < n && cur.Before(limit) {
		if !cs.Month[int(cur.Month())] {
			cur = time.Date(cur.Year(), cur.Month()+1, 1, 0, 0, 0, 0, cur.Location())
			continue
		}
		if !cs.matchesDay(cur) {
			cur = time.Date(cur.Year(), cur.Month(), cur.Day()+1, 0, 0, 0, 0, cur.Location())
			continue
		}
		if !cs.Hour[cur.Hour()] {
			cur = time.Date(cur.Year(), cur.Month(), cur.Day(), cur.Hour()+1, 0, 0, 0, cur.Location())
			continue
		}
		if !cs.Min[cur.Minute()] {
			cur = cur.Add(time.Minute)
			continue
		}
		runs = append(runs, cur)
		cur = cur.Add(time.Minute)
	}
	return runs
}

// Describe returns a concise human-readable summary in Chinese (zh=true) or English.
func (cs *CronSchedule) Describe(zh bool) string {
	f := cs.Fields
	minF, hourF, domF, monF, dowF := f[0], f[1], f[2], f[3], f[4]

	if minF == "*" && hourF == "*" && domF == "*" && monF == "*" && dowF == "*" {
		if zh {
			return "每分钟执行"
		}
		return "Every minute"
	}
	if strings.HasPrefix(minF, "*/") && hourF == "*" && domF == "*" && monF == "*" && dowF == "*" {
		n := minF[2:]
		if zh {
			return fmt.Sprintf("每 %s 分钟执行", n)
		}
		return fmt.Sprintf("Every %s minutes", n)
	}
	if isAllDigits(minF) && strings.HasPrefix(hourF, "*/") && domF == "*" && monF == "*" && dowF == "*" {
		m, _ := strconv.Atoi(minF)
		h := hourF[2:]
		if zh {
			if m == 0 {
				return fmt.Sprintf("每 %s 小时整点执行", h)
			}
			return fmt.Sprintf("每 %s 小时的第 %02d 分执行", h, m)
		}
		if m == 0 {
			return fmt.Sprintf("Every %s hours at :00", h)
		}
		return fmt.Sprintf("Every %s hours at :%02d", h, m)
	}
	if isAllDigits(minF) && hourF == "*" && domF == "*" && monF == "*" && dowF == "*" {
		m, _ := strconv.Atoi(minF)
		if zh {
			if m == 0 {
				return "每小时整点执行"
			}
			return fmt.Sprintf("每小时第 %02d 分执行", m)
		}
		if m == 0 {
			return "Every hour on the hour"
		}
		return fmt.Sprintf("Every hour at :%02d", m)
	}
	if isAllDigits(minF) && isAllDigits(hourF) && monF == "*" {
		m, _ := strconv.Atoi(minF)
		h, _ := strconv.Atoi(hourF)
		hm := fmt.Sprintf("%02d:%02d", h, m)
		if domF == "*" && dowF == "*" {
			if zh {
				return "每天 " + hm
			}
			return "Every day at " + hm
		}
		if domF == "*" && (dowF == "1-5" || strings.EqualFold(dowF, "MON-FRI")) {
			if zh {
				return "每个工作日 " + hm
			}
			return "Weekdays (Mon–Fri) at " + hm
		}
		if domF == "*" && (dowF == "0,6" || dowF == "6,0" || strings.EqualFold(dowF, "SAT,SUN")) {
			if zh {
				return "每周末 " + hm
			}
			return "Weekends (Sat–Sun) at " + hm
		}
		if domF == "*" {
			if d, ok := parseCronAtom(dowF, 0, 7, cronDowNames); ok {
				if d == 7 {
					d = 0
				}
				if zh {
					names := [...]string{"周日", "周一", "周二", "周三", "周四", "周五", "周六"}
					return "每" + names[d] + " " + hm
				}
				names := [...]string{"Sunday", "Monday", "Tuesday", "Wednesday", "Thursday", "Friday", "Saturday"}
				return "Every " + names[d] + " at " + hm
			}
		}
		if dowF == "*" && isAllDigits(domF) {
			if zh {
				return fmt.Sprintf("每月 %s 日 %s", domF, hm)
			}
			return fmt.Sprintf("Day %s of every month at %s", domF, hm)
		}
	}
	if zh {
		return fmt.Sprintf("分 %s · 时 %s · 日 %s · 月 %s · 周 %s", minF, hourF, domF, monF, dowF)
	}
	return fmt.Sprintf("min %s · hour %s · dom %s · mon %s · dow %s", minF, hourF, domF, monF, dowF)
}

func (sb *spanBuilder) startTokenPiece(tok InlineToken) {
	if !sb.interactiveTokens {
		return
	}
	t := tok
	sb.p.pieces = append(sb.p.pieces, piece{link: -1, token: &t})
	sb.cur = &sb.p.pieces[len(sb.p.pieces)-1]
}

func (sb *spanBuilder) endTokenPiece() {
	if !sb.interactiveTokens {
		return
	}
	sb.cur = nil
}

func (sb *spanBuilder) addSwatchObject(src int, s spanStyle, link int, widthRatio float32, makeDraw func(w, h, lead, fadeAlpha float32) func(*ui.Painter, ui.Rect)) {
	size := sb.th.FontSize
	if s.size != 0 {
		size = s.size
	}
	if s.sub {
		size *= 0.75
	}
	fontSize := size
	if s.code {
		fontSize = size * sb.th.CodeSize
	}
	h := max(float32(7.5), fontSize*0.66)
	w := h * widthRatio
	lead := max(float32(1.0), h*0.14)
	var gap float32
	if s.code {
		rad := max(float32(3), fontSize/4)
		px := min(fontSize*0.22, rad)
		gap = px + max(float32(2.5), fontSize*0.20)
	} else {
		gap = max(float32(3.0), fontSize*0.24)
	}
	ascent := fontSize * 0.76
	descent := fontSize * 0.14

	fadeAlpha := float32(1)
	if sb.fade != nil {
		fadeAlpha = sb.fade(src)
		if fadeAlpha < 1 {
			sb.p.fading = true
			fadeAlpha = max(fadeAlpha, 0.02)
		}
	}
	draw := makeDraw(w, h, lead, fadeAlpha)
	p := sb.p
	p.objects = append(p.objects, mathObject{rune: p.runes, src: src, end: src, draw: draw})
	sp := ui.Span{Text: "\uFFFC", Size: fontSize, ObjectWidth: lead + w + gap, ObjectHeight: ascent, ObjectDescent: descent}
	pc := sb.piece(link)
	pc.spans = append(pc.spans, sp)
	p.marks = append(p.marks, runMark{rune: p.runes, src: src, end: src})
	p.runes++

	joinStyle := s
	joinStyle.code = false
	joinStyle.kbd = false
	joinStyle.mark = false
	joinStyle.bg = ui.Color{}
	joinStyle.borderW = 0
	joinStyle.borderColor = ui.Color{}
	sb.addSpan(string(linkIconJoiner), src, src, false, joinStyle, link, 1)
}

func (sb *spanBuilder) addColorToken(text string, src, end int, exact bool, raw ui.Color, s spanStyle, link int) {
	if link == -1 {
		sb.startTokenPiece(InlineToken{Kind: TokenColor, Raw: strings.TrimSpace(text), Color: raw})
		defer sb.endTokenPiece()
	}
	dark, borderBase := sb.dark, sb.th.Text
	sb.addSwatchObject(src, s, link, 1.0, func(w, h, lead, fadeAlpha float32) func(*ui.Painter, ui.Rect) {
		return makeColorSwatchDraw(raw, h, lead, dark, borderBase, fadeAlpha)
	})
	cs := s
	fg, bg := colorLiteralPalette(raw, sb.surface, sb.th.CodeBackground, sb.dark, s.code)
	cs.color = fg
	if bg.A > 0 {
		cs.bg = bg
	}
	sb.add(text, src, end, exact, cs, link)
}

func (sb *spanBuilder) addGradientToken(text string, src, end int, exact bool, stops []ui.Color, s spanStyle, link int) {
	if link == -1 {
		cp := append([]ui.Color(nil), stops...)
		sb.startTokenPiece(InlineToken{Kind: TokenGradient, Raw: strings.TrimSpace(text), Color: stops[0], Gradient: cp})
		defer sb.endTokenPiece()
	}
	dark, borderBase := sb.dark, sb.th.Text
	sb.addSwatchObject(src, s, link, 2.2, func(w, h, lead, fadeAlpha float32) func(*ui.Painter, ui.Rect) {
		return makeGradientSwatchDraw(stops, w, h, lead, dark, borderBase, fadeAlpha)
	})
	cs := s
	fg, bg := colorLiteralPalette(stops[0], sb.surface, sb.th.CodeBackground, sb.dark, s.code)
	cs.color = fg
	if bg.A > 0 {
		cs.bg = bg
	}
	sb.add(text, src, end, exact, cs, link)
}

func (sb *spanBuilder) addTimestampToken(text string, src, end int, exact bool, unixNano int64, hasZone bool, s spanStyle, link int) {
	if link == -1 {
		sb.startTokenPiece(InlineToken{Kind: TokenTimestamp, Raw: strings.TrimSpace(text), UnixNano: unixNano, HasZone: hasZone})
		defer sb.endTokenPiece()
	}
	cs := s
	cs.dottedUnderline = true
	cs.decoColor = sb.th.Muted.Alpha(0.55)
	sb.add(text, src, end, exact, cs, link)
}

func (sb *spanBuilder) addCronToken(text string, src, end int, exact bool, s spanStyle, link int) {
	if link == -1 {
		sb.startTokenPiece(InlineToken{Kind: TokenCron, Raw: strings.TrimSpace(text)})
		defer sb.endTokenPiece()
	}
	cs := s
	cs.dottedUnderline = true
	cs.decoColor = sb.th.Muted.Alpha(0.55)
	sb.add(text, src, end, exact, cs, link)
}

func (sb *spanBuilder) addHTTPBadgeToken(text string, src, end int, exact bool, accent ui.Color, prefixLen int, s spanStyle, link int) {
	fg, bg := colorLiteralPalette(accent, sb.surface, sb.th.CodeBackground, sb.dark, true)
	if prefixLen >= len(text) {
		cs := s
		cs.color = fg
		if bg.A > 0 {
			cs.bg = bg
		}
		if sb.th.StrongWeight > cs.weight {
			cs.weight = sb.th.StrongWeight
		}
		sb.add(text, src, end, exact, cs, link)
		return
	}
	// "GET /api/v1/users": highlight the HTTP verb, keep the route path calm.
	lead := s
	lead.color = fg
	if bg.A > 0 {
		lead.bg = bg
	}
	if sb.th.StrongWeight > lead.weight {
		lead.weight = sb.th.StrongWeight
	}
	splitEnd := min(src+prefixLen, end)
	sb.add(text[:prefixLen], src, splitEnd, exact, lead, link)
	sb.add(text[prefixLen:], splitEnd, end, exact, s, link)
}

// addCodeEnriched handles enriched inline code spans (colors, gradients, HTTP
// badges, timestamps, cron expressions). Returns true if handled.
func (sb *spanBuilder) addCodeEnriched(text string, src, end int, exact bool, s spanStyle, link int) bool {
	if s.inLink || link != -1 {
		return false
	}
	if sb.colorLiterals {
		if stops, ok := parseLinearGradient(text); ok {
			sb.addGradientToken(text, src, end, exact, stops, s, link)
			return true
		}
		if col, ok := parseColorLiteral(text, true); ok {
			sb.addColorToken(text, src, end, exact, col, s, link)
			return true
		}
	}
	if sb.richTokens {
		if accent, prefixLen, ok := parseHTTPBadge(text); ok {
			sb.addHTTPBadgeToken(text, src, end, exact, accent, prefixLen, s, link)
			return true
		}
		if ns, hasZone, ok := parseTimestampLiteral(text, true); ok {
			sb.addTimestampToken(text, src, end, exact, ns, hasZone, s, link)
			return true
		}
		if _, ok := ParseCron(text); ok {
			sb.addCronToken(text, src, end, exact, s, link)
			return true
		}
	}
	return false
}

// addTextEnriched scans a plain-text inline node for color literals,
// linear-gradients, and (when RichTokens is enabled) ISO-8601 timestamps.
func (sb *spanBuilder) addTextEnriched(n *parser.Inline, exact bool, s spanStyle, link int) {
	if !exact || s.inLink {
		sb.addText(n, exact, s, link)
		return
	}
	if s.code {
		if sb.addCodeEnriched(n.Text, n.Start, n.End, true, s, link) {
			return
		}
		sb.add(n.Text, n.Start, n.End, true, s, link)
		return
	}
	if s.kbd {
		sb.add(n.Text, n.Start, n.End, true, s, link)
		return
	}
	text := n.Text
	checkColor := sb.colorLiterals && strings.ContainsAny(text, "#rRhHoOlLcC")
	checkTime := sb.richTokens && len(text) >= 20 && strings.ContainsRune(text, '-') && strings.ContainsAny(text, "Tt")
	if !checkColor && !checkTime {
		sb.add(text, n.Start, n.End, true, s, link)
		return
	}
	pos := 0
	for pos < len(text) {
		bestA := len(text) + 1
		bestZ := 0
		bestKind := TokenNone
		var bestCol ui.Color
		var bestGrad []ui.Color
		var bestNS int64

		if checkColor {
			if a, z, col, grad, ok := findNextColorLiteral(text, pos); ok && a < bestA {
				bestA, bestZ, bestCol, bestGrad = a, z, col, grad
				if len(grad) >= 2 {
					bestKind = TokenGradient
				} else {
					bestKind = TokenColor
				}
			}
		}
		if checkTime {
			if a, z, ns, ok := findNextISOTimestamp(text, pos); ok && a < bestA {
				bestA, bestZ, bestNS, bestKind = a, z, ns, TokenTimestamp
			}
		}
		if bestKind == TokenNone {
			sb.add(text[pos:], n.Start+pos, n.End, true, s, link)
			return
		}
		if bestA > pos {
			sb.add(text[pos:bestA], n.Start+pos, n.Start+bestA, true, s, link)
		}
		switch bestKind {
		case TokenColor:
			sb.addColorToken(text[bestA:bestZ], n.Start+bestA, n.Start+bestZ, true, bestCol, s, link)
		case TokenGradient:
			sb.addGradientToken(text[bestA:bestZ], n.Start+bestA, n.Start+bestZ, true, bestGrad, s, link)
		case TokenTimestamp:
			sb.addTimestampToken(text[bestA:bestZ], n.Start+bestA, n.Start+bestZ, true, bestNS, true, s, link)
		}
		pos = bestZ
	}
}
