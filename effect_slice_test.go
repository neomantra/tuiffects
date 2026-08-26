package tuiffects

import (
	"strings"
	"testing"
)

// sliceRunsHome drives a built effect to the end and checks the picture
// arrived.
func sliceRunsHome(t *testing.T, effect Effect, engine *Engine, input string) {
	t.Helper()
	frames := 0
	for effect.Advance(engine) {
		frames++
		if frames >= 40000 {
			t.Fatal("the effect never finished within the frame cap")
		}
	}
	if frames == 0 {
		t.Fatal("the effect produced no frames")
	}
	if got := strings.Join(nonBlank(engine.Frame()), "\n"); got != input {
		t.Errorf("the final frame reads %q, want %q", got, input)
	}
}

// TestSliceSettlesIntoTheInputText runs slice to completion and checks it both
// animates and lands on the input.
//
// Negative control: pointing the waypoint in sendTo at a fixed coordinate
// instead of ch.InputCoord piles the whole picture onto one cell and the final
// frame no longer reads as the input.
func TestSliceSettlesIntoTheInputText(t *testing.T) {
	const input = "slice me in two"
	term := NewTerminalFromText(input, TerminalConfig{
		Width: 20, Height: 5, MakeFillCharacters: true,
	})
	engine := NewEngine(term, NewRng(5))

	frames, err := Run(NewSlice(DefaultSliceConfig()), engine, 40000)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(frames) == 0 {
		t.Fatal("the effect produced no frames")
	}
	if len(frames) >= 40000 {
		t.Fatal("the effect never finished within the frame cap")
	}
	last := frames[len(frames)-1]
	if rows := nonBlank(last); len(rows) != 1 || rows[0] != input {
		t.Errorf("the final frame reads %q, want %q", rows, input)
	}
	// It has to be an animation and not a single jump to the answer.
	moved := false
	for _, frame := range frames[:len(frames)/2] {
		if plain(frame) != plain(last) {
			moved = true
			break
		}
	}
	if !moved {
		t.Error("every frame in the first half already reads as the final frame")
	}
}

// TestSliceCutsTheScreenInTwo is the test for the thing the effect is named
// for. A slice splits the picture down the middle column and throws the two
// halves off opposite edges: everything left of centre waits above the canvas,
// everything right of it waits below. A slide would send every character out
// by the same edge, and an effect that only looked correct at the end would
// send nothing anywhere.
//
// Negative control: sending both halves to canvas.Top+1 leaves the right half
// above the canvas and the below-canvas half of this test fails.
func TestSliceCutsTheScreenInTwo(t *testing.T) {
	term := NewTerminalFromText("abcdef\nghijkl\nmnopqr", TerminalConfig{
		Width: 8, Height: 5, MakeFillCharacters: true,
	})
	engine := NewEngine(term, NewRng(5))
	if err := NewSlice(DefaultSliceConfig()).Build(engine); err != nil {
		t.Fatalf("Build: %v", err)
	}
	canvas := term.Canvas
	above, below := 0, 0
	for _, ch := range term.InputCharacters {
		want := C(ch.InputCoord.Column, canvas.Bottom-1)
		if ch.InputCoord.Column <= canvas.TextCenterColumn {
			want = C(ch.InputCoord.Column, canvas.Top+1)
			above++
		} else {
			below++
		}
		if got := ch.Motion.CurrentCoord; got != want {
			t.Errorf("%q at %v waits at %v, want %v",
				ch.InputSymbol, ch.InputCoord, got, want)
		}
		// And the path has to lead back to where it came from, or the cut
		// never closes.
		path := ch.Motion.Path(slicePathID)
		if path == nil {
			t.Fatalf("%q has no %s", ch.InputSymbol, slicePathID)
		}
		if len(path.Waypoints) != 1 || path.Waypoints[0].Coord != ch.InputCoord {
			t.Errorf("%q travels to %v, want its input coordinate %v",
				ch.InputSymbol, path.Waypoints, ch.InputCoord)
		}
	}
	if above == 0 || below == 0 {
		t.Errorf("the cut sent %d characters up and %d down, want both sides in use", above, below)
	}
}

// TestSliceHorizontalCutsAcrossTheMiddleRow checks the second direction leaves
// by the side edges rather than the top and bottom ones, split at the middle
// row.
//
// Negative control: comparing against canvas.CenterRow instead of
// TextCenterRow moves the split off the text and the characters between the
// two rows go the wrong way.
func TestSliceHorizontalCutsAcrossTheMiddleRow(t *testing.T) {
	const input = "abcdef\nghijkl\nmnopqr\nstuvwx"
	term := NewTerminalFromText(input, TerminalConfig{
		Width: 8, Height: 6, MakeFillCharacters: true,
	})
	engine := NewEngine(term, NewRng(5))
	config := DefaultSliceConfig()
	config.Direction = SliceHorizontal
	effect := NewSlice(config)
	if err := effect.Build(engine); err != nil {
		t.Fatalf("Build: %v", err)
	}
	canvas := term.Canvas
	left, right := 0, 0
	for _, ch := range term.InputCharacters {
		want := C(canvas.Right+1, ch.InputCoord.Row)
		if ch.InputCoord.Row <= canvas.TextCenterRow {
			want = C(canvas.Left-1, ch.InputCoord.Row)
			left++
		} else {
			right++
		}
		if got := ch.Motion.CurrentCoord; got != want {
			t.Errorf("%q at %v waits at %v, want %v",
				ch.InputSymbol, ch.InputCoord, got, want)
		}
	}
	if left == 0 || right == 0 {
		t.Errorf("the cut sent %d characters left and %d right, want both sides in use", left, right)
	}
	sliceRunsHome(t, effect, engine, input)
}

// TestSliceHorizontalMovesTheFillCharacters is why the descriptor declares
// NeedsFillCharacters. The horizontal cut takes the whole rectangle the text
// sits in, blanks included, so the empty cells between words travel with it.
// Without the declaration the terminal holds no fill characters, the query
// comes back short and the cut animates only the glyphs.
//
// Negative control: querying InputOnly in buildHorizontal leaves every fill
// character standing on its input coordinate with no path, and this fails.
func TestSliceHorizontalMovesTheFillCharacters(t *testing.T) {
	descriptor, ok := Lookup("slice")
	if !ok {
		t.Fatal("slice is not registered")
	}
	if !descriptor.NeedsFillCharacters {
		t.Error("the descriptor does not ask for fill characters, so the horizontal cut gets none")
	}

	term := NewTerminalFromText("ab ba\ncd dc", TerminalConfig{
		Width: 7, Height: 4, MakeFillCharacters: true,
	})
	engine := NewEngine(term, NewRng(5))
	config := DefaultSliceConfig()
	config.Direction = SliceHorizontal
	if err := NewSlice(config).Build(engine); err != nil {
		t.Fatalf("Build: %v", err)
	}
	canvas := term.Canvas
	moved, inText := 0, 0
	for _, ch := range term.InnerFillCharacters {
		if !canvas.CoordIsInText(ch.InputCoord) {
			continue
		}
		inText++
		if ch.Motion.Path(slicePathID) != nil && ch.Motion.CurrentCoord != ch.InputCoord {
			moved++
		}
	}
	if inText == 0 {
		t.Fatal("the input made no fill characters inside the text block")
	}
	if moved != inText {
		t.Errorf("%d of %d fill characters inside the text block travel, want all of them", moved, inText)
	}
}

// TestSliceDiagonalStacksEachGroupOnOnePoint checks the third direction. A
// diagonal group leaves through a single cell rather than as a line, so every
// character in it shares one origin. The first half of the diagonals drop out
// of the bottom by the column their lowest character stands in, the second
// half out of the top by the column their highest character stands in.
//
// Negative control: taking the first character of a group rather than its last
// for the groups that leave over the top puts the origin on the far end of the
// diagonal and the column check fails.
func TestSliceDiagonalStacksEachGroupOnOnePoint(t *testing.T) {
	const input = "abcdef\nghijkl\nmnopqr"
	term := NewTerminalFromText(input, TerminalConfig{
		Width: 8, Height: 5, MakeFillCharacters: true,
	})
	engine := NewEngine(term, NewRng(5))
	config := DefaultSliceConfig()
	config.Direction = SliceDiagonal
	effect := NewSlice(config)
	if err := effect.Build(engine); err != nil {
		t.Fatalf("Build: %v", err)
	}
	canvas := term.Canvas
	groups := term.GetCharactersGrouped(InputOnly(), GroupDiagonalBottomLeftToTopRight)
	if len(groups) < 4 {
		t.Fatalf("the input made %d diagonal groups, want several", len(groups))
	}
	half := len(groups) / 2
	for index, group := range groups {
		want := C(group[0].InputCoord.Column, canvas.Bottom-1)
		if index >= half {
			want = C(group[len(group)-1].InputCoord.Column, canvas.Top+1)
		}
		for _, ch := range group {
			if got := ch.Motion.CurrentCoord; got != want {
				t.Errorf("diagonal %d holds %q at %v, want the whole group on %v",
					index, ch.InputSymbol, got, want)
			}
		}
	}
	// Both edges have to be in use, or the picture is not being cut at all.
	seen := map[int]bool{}
	for _, ch := range term.InputCharacters {
		seen[ch.Motion.CurrentCoord.Row] = true
	}
	if !seen[canvas.Bottom-1] || !seen[canvas.Top+1] {
		t.Errorf("the diagonals left by the rows %v, want both %d and %d",
			seen, canvas.Bottom-1, canvas.Top+1)
	}
	sliceRunsHome(t, effect, engine, input)
}

// TestSliceAssemblesRatherThanPassesOver is the rule that decides how an
// effect behaves under DynamicExistingColors. Slice throws the picture off the
// canvas and brings it back, so nothing may be standing on its own coordinate
// when the first frame is drawn. An effect that sweeps over the screen, waves
// for one, has to do the opposite and show everything from the first frame.
//
// Negative control: dropping the Motion.SetCoordinate call in sendTo, so no
// character is ever parked off the canvas, puts the whole picture on the first
// frame and this fails.
//
// Moving every character back to its input coordinate at the end of Build,
// which is the obvious way to write this control, does not fail: the frame
// painter never sees that state, because activating a path records where the
// character stood at the time and the first step of the path puts it straight
// back there. Checked that way round rather than leaving a control that
// passes.
func TestSliceAssemblesRatherThanPassesOver(t *testing.T) {
	red := RGB(255, 0, 0)
	grid := [][]InputCell{
		{{Symbol: "a", Fg: red, HasFg: true}, {Symbol: "b", Fg: red, HasFg: true}},
		{{Symbol: "c", Fg: red, HasFg: true}, {Symbol: "d", Fg: red, HasFg: true}},
	}
	term := NewTerminalFromCells(grid, TerminalConfig{
		Width: 2, Height: 2, ExistingColorHandling: DynamicExistingColors,
		MakeFillCharacters: true,
	})
	engine := NewEngine(term, NewRng(5))
	frames, err := Run(NewSlice(DefaultSliceConfig()), engine, 40000)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(frames) == 0 || len(frames) >= 40000 {
		t.Fatalf("the effect produced %d frames", len(frames))
	}
	first := strings.TrimSpace(plain(frames[0]))
	if first != "" {
		t.Errorf("the first frame already shows %q, so the effect is not assembling the screen", first)
	}
	last := frames[len(frames)-1]
	if got := strings.Join(nonBlank(last), "\n"); got != "ab\ncd" {
		t.Fatalf("the final frame reads %q, want %q", got, "ab\ncd")
	}
	if !strings.Contains(last, "\x1b[38;2;255;0;0m") {
		t.Errorf("the final frame does not carry the input's red: %q", last)
	}
}

// TestSliceResolvesTheInputColours checks the colour policy a screen saver
// runs in, where the input is a picture that has to come back as itself.
//
// Negative control: dropping the DynamicExistingColors branch in Build makes
// the final frame carry the effect's own gradient instead of the input's
// green, and lose the background entirely.
func TestSliceResolvesTheInputColours(t *testing.T) {
	green := RGB(0, 255, 0)
	blue := RGB(0, 0, 255)
	grid := [][]InputCell{{
		{Symbol: "x", Fg: green, HasFg: true, Bg: blue, HasBg: true},
		{Symbol: "y", Fg: green, HasFg: true, Bg: blue, HasBg: true},
	}}
	term := NewTerminalFromCells(grid, TerminalConfig{
		Width: 2, Height: 1, ExistingColorHandling: DynamicExistingColors,
		MakeFillCharacters: true,
	})
	engine := NewEngine(term, NewRng(5))
	frames, err := Run(NewSlice(DefaultSliceConfig()), engine, 40000)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(frames) == 0 {
		t.Fatal("the effect produced no frames")
	}
	last := frames[len(frames)-1]
	if !strings.Contains(last, "\x1b[38;2;0;255;0m") {
		t.Errorf("the final frame lost the input's foreground: %q", last)
	}
	// A captured cell's background is half of what it looked like, and an
	// effect that sets only a foreground blanks every filled panel on screen.
	if !strings.Contains(last, "\x1b[48;2;0;0;255m") {
		t.Errorf("the final frame lost the input's background: %q", last)
	}
}
