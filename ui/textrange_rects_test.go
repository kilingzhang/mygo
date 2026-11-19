package ui

import "testing"

func TestTextAreaRangeRects(t *testing.T) {
	text := "Alpha beta gamma"
	var area *node
	tt := coreNewTester(func(c *context) {
		area = coreTextAreaBase(c, &text).Width(220).Lines(2, 2)
	}, 300, 100)
	tt.Frame()
	rects := area.TextRangeRects(0, 5)
	if len(rects) == 0 {
		t.Fatal("no rectangles for a laid out range")
	}
	for _, r := range rects {
		if r.W <= 0 || r.H <= 0 {
			t.Fatalf("empty range rectangle: %#v", r)
		}
	}
	if got := area.TextRangeRects(4, 4); got != nil {
		t.Fatalf("empty range returned rectangles: %#v", got)
	}
}

func TestTextRangeHiddenMerges(t *testing.T) {
	text := "visible hidden visible"
	var area *node
	tt := coreNewTester(func(c *context) {
		area = coreTextAreaBase(c, &text).Width(300).Lines(2, 2)
		area.TextRanges(TextRange{Start: 8, End: 14, Hidden: true})
	}, 400, 100)
	tt.Frame()
	tt.Frame()
	found := false
	for _, r := range area.st.editor.ranges {
		if r.Start == 8 && r.End == 14 && r.Hidden {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("hidden range not merged: %#v", area.st.editor.ranges)
	}
	_, spans := area.st.editor.rangeSpans(8, 14)
	if spans == nil {
		t.Fatal("hidden range has no paint spans")
	}
	hidden := false
	for _, span := range spans.spans {
		if span.colorSet && span.Color.A == 0 {
			hidden = true
			break
		}
	}
	if !hidden {
		t.Fatalf("hidden range has no transparent paint span: %#v", spans.spans)
	}
}

func TestTextAreaRangeObjectWidth(t *testing.T) {
	text := "Alpha beta gamma"
	var area *node
	tt := coreNewTester(func(c *context) {
		area = coreTextAreaBase(c, &text).Width(300).Lines(2, 2)
		area.TextRanges(
			TextRange{Start: 0, End: 1, ObjectWidth: 80, ObjectHeight: 16},
			TextRange{Start: 1, End: 5, Size: 0.01, Hidden: true},
		)
	}, 400, 100)
	tt.Frame()
	tt.Frame()
	rects := area.TextRangeRects(0, 5)
	if len(rects) == 0 {
		t.Fatal("object range has no rectangles")
	}
	total := float32(0)
	for _, r := range rects {
		total += r.W
	}
	if total < 78 || total > 82 {
		t.Fatalf("object range is %.1f wide, want about 80", total)
	}
}
