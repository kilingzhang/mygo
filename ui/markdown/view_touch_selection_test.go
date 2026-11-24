package markdown_test

import (
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/markdown"
)

func TestMarkdownCoarsePointerLongPressSelectsText(t *testing.T) {
	doc := "First paragraph alpha beta gamma.\n\nSecond paragraph delta epsilon zeta."
	opts := &markdown.Options{ShareSelection: false}
	tt := ui.NewTesterWith(func(c *ui.Context) {
		ui.Scroll(c).Fill().Children(func() {
			markdown.View(c, doc, opts).FillWidth().Padding(12)
		})
	}, 360, 260, ui.TesterOptions{
		Preferences: ui.Preferences{Pointer: ui.PointerCoarse, NoHover: true},
	})
	tt.Frame()
	tt.Frame()

	var firstBox, secondBox ui.Rect
	for _, n := range tt.AccessAll() {
		if strings.Contains(n.Name, "First paragraph") {
			firstBox = n.Bounds
		}
		if strings.Contains(n.Name, "Second paragraph") {
			secondBox = n.Bounds
		}
	}
	if firstBox.W <= 0 || secondBox.W <= 0 {
		t.Fatalf("missing markdown paragraph bounds: first=%+v second=%+v", firstBox, secondBox)
	}

	// Long-press on first paragraph and hold-drag into second paragraph.
	// Must select text (not be hijacked by desktop ContextMenu).
	tt.FingerDown(firstBox.X+24, firstBox.Y+firstBox.H/2)
	tt.Advance(600 * time.Millisecond)
	tt.Frame()
	tt.FingerMove(secondBox.X+120, secondBox.Y+secondBox.H/2)
	tt.Frame()
	tt.FingerUp(secondBox.X+120, secondBox.Y+secondBox.H/2)
	tt.Frame()

	tt.Key(ui.Cmd, ui.KeyC)
	got := tt.Clipboard()
	if !strings.Contains(got, "First") || !strings.Contains(got, "Second") {
		t.Fatalf("expected coarse-pointer hold-drag on standalone markdown to copy across paragraphs, got %q", got)
	}
}
