package tuiffects

import "testing"

// TestExpandSettlesIntoTheInputText runs expand to completion and checks the
// screen it leaves behind, and that it did not start there.
//
// Negative control: pointing the ramp at a fixed symbol instead of
// ch.InputSymbol leaves the final frame reading as that symbol.
func TestExpandSettlesIntoTheInputText(t *testing.T) {
	const input = "expanding text"
	term := NewTerminalFromText(input, TerminalConfig{Width: 24, Height: 5})
	engine := NewEngine(term, NewRng(4))

	frames, err := Run(NewExpand(DefaultExpandConfig()), engine, 40000)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(frames) == 0 {
		t.Fatal("the effect produced no frames")
	}
	if len(frames) >= 40000 {
		t.Fatal("the effect never finished within the frame cap")
	}
	rows := nonBlank(frames[len(frames)-1])
	if len(rows) != 1 || rows[0] != input {
		t.Errorf("the final frame reads %q, want %q", rows, input)
	}
	// The first frame is the whole text piled on one cell, so it cannot read
	// as the input. An effect that never animated would pass the check above
	// and fail this one.
	if first := nonBlank(frames[0]); len(first) == 1 && first[0] == input {
		t.Error("the first frame already reads as the input, so nothing expanded")
	}
}

// TestExpandStartsStackedOnTheCentre checks the thing the effect is named for:
// every character begins on the middle cell of the canvas, not where it
// belongs, and the ones that belong elsewhere leave it.
//
// Negative control: dropping the SetCoordinate call in Build leaves every
// character on its input coordinate and the centre check fails on the first
// character that does not belong there.
func TestExpandStartsStackedOnTheCentre(t *testing.T) {
	term := NewTerminalFromText("abcdef", TerminalConfig{Width: 12, Height: 5})
	engine := NewEngine(term, NewRng(4))
	effect := NewExpand(DefaultExpandConfig())
	if err := effect.Build(engine); err != nil {
		t.Fatalf("Build: %v", err)
	}
	center := term.Canvas.Center
	for _, ch := range term.InputCharacters {
		if ch.Motion.CurrentCoord != center {
			t.Fatalf("%q starts at %v, want the canvas centre %v", ch.InputSymbol, ch.Motion.CurrentCoord, center)
		}
		if !ch.IsVisible {
			t.Fatalf("%q is hidden at the start, so the pile at the centre is invisible", ch.InputSymbol)
		}
	}

	for i := 0; i < 40000; i++ {
		if !effect.Advance(engine) {
			break
		}
	}
	moved := 0
	for _, ch := range term.InputCharacters {
		if ch.Motion.CurrentCoord != ch.InputCoord {
			t.Fatalf("%q ended at %v, want its own cell %v", ch.InputSymbol, ch.Motion.CurrentCoord, ch.InputCoord)
		}
		if ch.InputCoord != center {
			moved++
		}
	}
	if moved == 0 {
		t.Fatal("no character had anywhere to expand to")
	}
}

// TestExpandOnlyMovesOutwards checks each character travels the straight line
// from the centre to its own cell and never leaves it. That is what separates
// an expansion from a scatter: the picture grows out of one point.
//
// Negative control: easing the path with InOutBack instead of InOutQuart makes
// the overshoot at both ends carry characters outside the box between the
// centre and their cell, and this fails.
func TestExpandOnlyMovesOutwards(t *testing.T) {
	term := NewTerminalFromText("abcd\nefgh", TerminalConfig{Width: 14, Height: 7})
	engine := NewEngine(term, NewRng(4))
	effect := NewExpand(DefaultExpandConfig())
	if err := effect.Build(engine); err != nil {
		t.Fatalf("Build: %v", err)
	}
	center := term.Canvas.Center
	for i := 0; i < 40000; i++ {
		if !effect.Advance(engine) {
			break
		}
		for _, ch := range term.InputCharacters {
			at := ch.Motion.CurrentCoord
			if !within(center.Column, ch.InputCoord.Column, at.Column) ||
				!within(center.Row, ch.InputCoord.Row, at.Row) {
				t.Fatalf("frame %d put %q at %v, outside the line from the centre %v to %v",
					i, ch.InputSymbol, at, center, ch.InputCoord)
			}
		}
	}
}

// within reports whether v lies between a and b, either way round.
func within(a, b, v int) bool {
	return v >= min(a, b) && v <= max(a, b)
}

// TestExpandCarriesTheInputBackground checks the dynamic colour policy on a
// cell that arrived with a background, which on a captured screen is every
// selection bar and filled panel. The ramp has to move both halves of the
// colour, and the character has to land wearing both again.
//
// Negative control: passing nil for the background gradient in the dynamic
// branch of Build leaves the cell with no background and this fails.
func TestExpandCarriesTheInputBackground(t *testing.T) {
	grid := [][]InputCell{{
		{Symbol: "b", Fg: RGB(255, 255, 255), HasFg: true, Bg: RGB(0, 0, 200), HasBg: true},
	}}
	term := NewTerminalFromCells(grid, TerminalConfig{
		Width: 9, Height: 5, ExistingColorHandling: DynamicExistingColors,
	})
	engine := NewEngine(term, NewRng(4))
	effect := NewExpand(DefaultExpandConfig())
	if err := effect.Build(engine); err != nil {
		t.Fatalf("Build: %v", err)
	}
	ch := term.InputCharacters[0]

	sawBackground := false
	for i := 0; i < 40000; i++ {
		if !effect.Advance(engine) {
			break
		}
		if ch.Animation.CurrentVisual().Colors.HasBg {
			sawBackground = true
		}
	}
	if !sawBackground {
		t.Error("the character never wore a background, so the ramp dropped it")
	}
	final := ch.Animation.CurrentVisual().Colors
	if !final.HasBg || final.Bg != RGB(0, 0, 200) {
		t.Errorf("the character settled on background %v (set: %v), want the input's 0,0,200",
			final.Bg, final.HasBg)
	}
	if !final.HasFg || final.Fg != RGB(255, 255, 255) {
		t.Errorf("the character settled on foreground %v (set: %v), want the input's 255,255,255",
			final.Fg, final.HasFg)
	}
}
