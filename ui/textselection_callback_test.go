package ui

import (
	"slices"
	"testing"
)

func TestSelectableOnSelectionChange(t *testing.T) {
	var calls [][]SelectedText
	var rects []Rect
	var bounds Rect
	var hasBounds bool
	var scopeID uint64
	tt := coreNewTester(func(c *context) {
		col := coreColumn(c).Selectable().Padding(12).Gap(8)
		scopeID = col.ID()
		col.OnSelectionChange(func(parts []SelectedText) {
			calls = append(calls, slices.Clone(parts))
			rects = col.SelectionRects()
			bounds, hasBounds = col.SelectionBounds()
		})
		col.Children(func() {
			coreText(c, "Alpha beta")
			coreText(c, "Gamma delta")
		})
	}, 300, 150)
	selectBetween(t, tt, "Alpha beta", 6, "Gamma delta", 5)
	tt.Frame()
	if len(calls) == 0 {
		t.Fatal("selection change callback did not run")
	}
	last := calls[len(calls)-1]
	if len(last) != 2 ||
		last[0].Text != "Alpha beta" || last[0].Start != 6 || last[0].End != len("Alpha beta") ||
		last[1].Text != "Gamma delta" || last[1].Start != 0 || last[1].End != 5 {
		t.Fatalf("callback parts: %#v", last)
	}
	if len(rects) == 0 || !hasBounds || bounds.W <= 0 || bounds.H <= 0 {
		t.Fatalf("selection rects: %#v bounds %#v %v", rects, bounds, hasBounds)
	}
	if len(last[0].Ancestors) == 0 || last[0].Ancestors[len(last[0].Ancestors)-1] != scopeID {
		t.Fatalf("ancestors do not reach the Selectable container: %#v", last[0].Ancestors)
	}
	for _, r := range rects {
		if r.W <= 0 || r.H <= 0 {
			t.Fatalf("empty selection rect: %#v", r)
		}
	}
	tt.ClickAt(5, 5)
	tt.Frame()
	if len(calls) == 0 || calls[len(calls)-1] != nil {
		t.Fatalf("clearing the selection did not report nil: %#v", calls)
	}
}

func TestSelectableOnSelectionChangeNestedScope(t *testing.T) {
	var outerCalls, innerCalls int
	tt := coreNewTester(func(c *context) {
		outer := coreColumn(c).Selectable().Gap(8)
		outer.OnSelectionChange(func(parts []SelectedText) {
			if parts != nil {
				outerCalls++
			}
		})
		outer.Children(func() {
			coreText(c, "Outside")
			inner := coreColumn(c).Selectable()
			inner.OnSelectionChange(func(parts []SelectedText) {
				if parts != nil {
					innerCalls++
				}
			})
			inner.Children(func() {
				coreText(c, "Inside")
			})
		})
	}, 300, 150)
	selectBetween(t, tt, "Inside", 0, "Inside", 6)
	tt.Frame()
	if innerCalls == 0 {
		t.Fatal("inner selection did not notify its container")
	}
	if outerCalls != 0 {
		t.Fatalf("outer container received %d inner selections", outerCalls)
	}
}

func TestSelectableSelectTextInQuiet(t *testing.T) {
	var scopeID uint64
	var calls int
	tt := coreNewTester(func(c *context) {
		col := coreColumn(c).Selectable().Padding(12).Gap(8)
		scopeID = col.ID()
		col.OnSelectionChange(func(parts []SelectedText) {
			if parts != nil {
				calls++
			}
		})
		col.Children(func() {
			coreText(c, "Alpha beta")
			coreText(c, "Gamma delta")
		})
	}, 300, 150)
	tt.Frame()
	if !tt.rt.selectTextIn(scopeID, scopeID, "Gamma", 0) {
		t.Fatal("SelectTextIn did not find the quote")
	}
	tt.Frame()
	parts := tt.rt.selectedTexts(scopeID)
	if len(parts) != 1 || parts[0].Text != "Gamma delta" || parts[0].Start != 0 || parts[0].End != 5 {
		t.Fatalf("selected parts: %#v", parts)
	}
	if calls != 0 {
		t.Fatalf("quiet selection notified %d times", calls)
	}
	if tt.rt.clearTextSelectionIf(scopeID, "Alpha") {
		t.Fatal("cleared a selection that did not match")
	}
	if !tt.rt.clearTextSelectionIf(scopeID, "Gamma") {
		t.Fatal("did not clear the matching selection")
	}
	tt.Frame()
	if parts := tt.rt.selectedTexts(scopeID); len(parts) != 0 {
		t.Fatalf("selection remained after clear: %#v", parts)
	}
}

func TestSelectableSubtreeRangeRects(t *testing.T) {
	var scopeID, msgID uint64
	tt := coreNewTester(func(c *context) {
		col := coreColumn(c).Selectable().Padding(12).Gap(8)
		scopeID = col.ID()
		col.Children(func() {
			coreText(c, "Other message")
			msg := coreColumn(c).Gap(4)
			msgID = msg.ID()
			msg.Children(func() {
				coreText(c, "First line here")
				coreText(c, "Second line here")
			})
		})
	}, 300, 180)
	tt.Frame()
	gotText := tt.rt.subtreeText(scopeID, msgID)
	if gotText != "First line here\nSecond line here" {
		t.Fatalf("subtree text = %q", gotText)
	}
	selectBetween(t, tt, "First line here", 6, "Second line here", 6)
	tt.Frame()
	full, start, end, ok := tt.rt.subtreeSelectionRange(scopeID, msgID)
	if !ok || full != gotText || string([]rune(full)[start:end]) != "line here\nSecond" {
		t.Fatalf("subtreeSelectionRange = %q [%d:%d] ok=%v", full, start, end, ok)
	}
	tt.ClickAt(5, 5)
	tt.Frame()
	rects := tt.rt.subtreeRangeRects(scopeID, msgID, start, end, true)
	if len(rects) != 2 {
		t.Fatalf("expected 2 rects across 2 paragraphs without active selection, got %#v", rects)
	}
	if sel := tt.rt.selectedTexts(scopeID); len(sel) != 0 {
		t.Fatalf("SubtreeRangeRects mutated selection: %#v", sel)
	}
}
