package editor_test

import (
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
)

func TestEditorReadOnlyTouchSelection(t *testing.T) {
	st := editor.State{
		Buffer:   editor.NewBuffer("package main\n\nfunc helloWorld() {\n\tprintln(\"mobile select\")\n}\n"),
		ReadOnly: true,
	}
	tt := ui.NewTesterWith(func(c *ui.Context) {
		editor.View(c, &st).Fill()
	}, 360, 220, ui.TesterOptions{
		Preferences: ui.Preferences{Pointer: ui.PointerCoarse, NoHover: true},
	})
	tt.Frame()
	tt.Frame()

	// Long-press on "package" and hold-drag down to line 3 without releasing.
	tt.FingerDown(76, 12)
	tt.Advance(600 * time.Millisecond)
	tt.Frame()
	tt.FingerMove(180, 52)
	tt.Frame()
	tt.FingerUp(180, 52)
	tt.Frame()

	sel := st.Selection()
	if sel.Empty() {
		t.Fatalf("expected non-empty selection after long-press hold-drag on read-only editor")
	}
	tt.Key(ui.Cmd, ui.KeyC)
	copied := tt.Clipboard()
	if !strings.Contains(copied, "package") || !strings.Contains(copied, "func") {
		t.Fatalf("expected multi-line code selection copied, got %q", copied)
	}
	// Editing commands must be ignored on read-only editor.
	before := st.Buffer.String()
	tt.Key(ui.Cmd, ui.KeyX)
	tt.Frame()
	if st.Buffer.String() != before {
		t.Fatalf("read-only editor buffer mutated by Cmd+X")
	}
}
