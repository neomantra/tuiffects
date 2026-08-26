package tuiffects

import (
	"strings"
	"testing"
)

// middleoutCoords snapshots where every input character is standing.
func middleoutCoords(term *Terminal) map[*Character]Coord {
	out := make(map[*Character]Coord, len(term.InputCharacters))
	for _, ch := range term.InputCharacters {
		out[ch] = ch.Motion.CurrentCoord
	}
	return out
}

// middleoutRun advances an effect to completion, capped, and returns the
// character positions from the last frame of the centre expansion, which is
// the frame before the second expansion starts.
func middleoutRun(t *testing.T, term *Terminal, engine *Engine, effect *Middleout) map[*Character]Coord {
	t.Helper()
	if err := effect.Build(engine); err != nil {
		t.Fatalf("Build: %v", err)
	}
	var collapsed map[*Character]Coord
	frames := 0
	for ; frames < 40000; frames++ {
		if effect.phase == middleoutPhaseCenter {
			collapsed = middleoutCoords(term)
		}
		if !effect.Advance(engine) {
			break
		}
	}
	if frames >= 40000 {
		t.Fatal("the effect never finished within the frame cap")
	}
	if collapsed == nil {
		t.Fatal("the centre expansion never ran")
	}
	return collapsed
}

// TestMiddleoutResolvesTheInputText runs middleout to completion and checks
// every character got home.
//
// Negative control: pointing the full path's waypoint at the canvas centre
// instead of ch.InputCoord leaves the text stacked in one place and the final
// frame does not read as the input.
func TestMiddleoutResolvesTheInputText(t *testing.T) {
	const input = "middle out"
	term := NewTerminalFromText(input, TerminalConfig{Width: 20, Height: 5})
	engine := NewEngine(term, NewRng(7))

	frames, err := Run(NewMiddleout(DefaultMiddleoutConfig()), engine, 40000)
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
	// It has to move as well as arrive: a first frame that already reads as
	// the input is not an animation.
	if plain(frames[0]) == plain(frames[len(frames)-1]) {
		t.Error("the first frame already matches the last, so nothing animated")
	}
}

// TestMiddleoutStartsStackedOnTheCentre is the test for the thing the effect
// is named for. Every character is teleported onto the middle of the canvas
// before it moves anywhere, so the whole picture starts as one cell.
//
// Negative control: dropping ch.Motion.SetCoordinate(canvas.Center) from Build
// leaves every character at its input coordinate, and both halves of this
// fail: the coordinates are wrong and the opening frame reads as the input.
func TestMiddleoutStartsStackedOnTheCentre(t *testing.T) {
	term := NewTerminalFromText("abc\ndef", TerminalConfig{Width: 9, Height: 5})
	engine := NewEngine(term, NewRng(7))
	effect := NewMiddleout(DefaultMiddleoutConfig())
	if err := effect.Build(engine); err != nil {
		t.Fatalf("Build: %v", err)
	}
	center := term.Canvas.Center
	for _, ch := range term.InputCharacters {
		if ch.Motion.CurrentCoord != center {
			t.Fatalf("%q starts at %v, want the canvas centre %v",
				ch.InputSymbol, ch.Motion.CurrentCoord, center)
		}
	}
	rows := nonBlank(engine.Frame())
	if len(rows) != 1 || len([]rune(rows[0])) != center.Column {
		t.Errorf("the opening frame reads %q, want a single character on column %d",
			rows, center.Column)
	}
}

// TestMiddleoutCollapsesToTheCentreRowFirst checks the first expansion, which
// is the half of the effect that gives it its shape: the text spreads out
// along the centre row and only then opens up and down.
//
// Negative control: giving the centre waypoint ch.InputCoord.Row instead of
// canvas.CenterRow makes the characters spread straight to their own rows and
// the row check fails for every character that did not start on the centre
// row.
func TestMiddleoutCollapsesToTheCentreRowFirst(t *testing.T) {
	term := NewTerminalFromText("abcd\nefgh\nijkl", TerminalConfig{Width: 10, Height: 7})
	engine := NewEngine(term, NewRng(7))
	collapsed := middleoutRun(t, term, engine, NewMiddleout(DefaultMiddleoutConfig()))

	center := term.Canvas.CenterRow
	for ch, coord := range collapsed {
		if coord.Row != center {
			t.Errorf("%q ended the centre expansion on row %d, want the centre row %d",
				ch.InputSymbol, coord.Row, center)
		}
		if coord.Column != ch.InputCoord.Column {
			t.Errorf("%q ended the centre expansion on column %d, want its own column %d",
				ch.InputSymbol, coord.Column, ch.InputCoord.Column)
		}
	}
	for _, ch := range term.InputCharacters {
		if ch.Motion.CurrentCoord != ch.InputCoord {
			t.Errorf("%q finished at %v, want its input coordinate %v",
				ch.InputSymbol, ch.Motion.CurrentCoord, ch.InputCoord)
		}
	}
}

// TestMiddleoutHorizontalCollapsesToTheCentreColumn is the same check for the
// other axis, which is the only thing ExpandDirection changes.
//
// Negative control: ignoring config.ExpandDirection and always building the
// vertical waypoint puts every character on the centre row instead, and the
// column check fails.
func TestMiddleoutHorizontalCollapsesToTheCentreColumn(t *testing.T) {
	term := NewTerminalFromText("abcd\nefgh\nijkl", TerminalConfig{Width: 10, Height: 7})
	engine := NewEngine(term, NewRng(7))
	config := DefaultMiddleoutConfig()
	config.ExpandDirection = ExpandHorizontal
	collapsed := middleoutRun(t, term, engine, NewMiddleout(config))

	center := term.Canvas.CenterColumn
	for ch, coord := range collapsed {
		if coord.Column != center {
			t.Errorf("%q ended the centre expansion on column %d, want the centre column %d",
				ch.InputSymbol, coord.Column, center)
		}
		if coord.Row != ch.InputCoord.Row {
			t.Errorf("%q ended the centre expansion on row %d, want its own row %d",
				ch.InputSymbol, coord.Row, ch.InputCoord.Row)
		}
	}
}

// TestMiddleoutRampsFromTheStartingColour checks the closing scene is a ramp
// and not a switch. A character wears the starting colour for the whole of the
// first expansion and then walks a gradient to its final colour.
//
// Negative control: replacing the ApplyGradientToSymbols call with one
// AddFrame at the final colour leaves two distinct colours on the character,
// the starting one and the final one, and the count check fails.
func TestMiddleoutRampsFromTheStartingColour(t *testing.T) {
	term := NewTerminalFromText("abcd\nefgh\nijkl", TerminalConfig{Width: 10, Height: 7})
	engine := NewEngine(term, NewRng(7))
	config := DefaultMiddleoutConfig()
	effect := NewMiddleout(config)
	if err := effect.Build(engine); err != nil {
		t.Fatalf("Build: %v", err)
	}
	// The bottom left character sits at the far end of the vertical gradient,
	// so it has the longest way to travel from the starting colour.
	var watched *Character
	for _, ch := range term.InputCharacters {
		if watched == nil || ch.InputCoord.Row < watched.InputCoord.Row ||
			(ch.InputCoord.Row == watched.InputCoord.Row && ch.InputCoord.Column < watched.InputCoord.Column) {
			watched = ch
		}
	}
	if watched == nil {
		t.Fatal("the terminal holds no input characters")
	}
	if got := watched.Animation.CurrentVisual().Colors; !got.HasFg || got.Fg != config.StartingColor {
		t.Fatalf("%q opens on %v, want the starting colour %v",
			watched.InputSymbol, got, config.StartingColor)
	}

	seen := []Color{config.StartingColor}
	for i := 0; i < 40000; i++ {
		if !effect.Advance(engine) {
			break
		}
		colors := watched.Animation.CurrentVisual().Colors
		if !colors.HasFg {
			t.Fatalf("frame %d left %q with no foreground colour", i, watched.InputSymbol)
		}
		if colors.Fg != seen[len(seen)-1] {
			seen = append(seen, colors.Fg)
		}
	}
	if len(seen) < 5 {
		t.Errorf("%q wore %d colours over the run (%v), want a ramp of several",
			watched.InputSymbol, len(seen), seen)
	}
	if seen[len(seen)-1] == config.StartingColor {
		t.Errorf("%q finished on the starting colour, so the ramp never arrived", watched.InputSymbol)
	}
}

// TestMiddleoutResolvesTheInputColours checks the effect under the colour
// policy a screen saver runs in, where the picture must come back in the
// colours it arrived with rather than in the effect's own gradient.
//
// This effect assembles the screen rather than passing over it, so it does not
// take the deviation waves takes: the characters are stacked on the centre of
// the canvas from the first frame, and showing them at their home cells early
// would draw the picture before the effect had assembled any of it.
//
// Negative control: dropping the dynamic branch in Build makes the final frame
// carry the gradient's colour instead of the input's red on blue.
func TestMiddleoutResolvesTheInputColours(t *testing.T) {
	red, blue := RGB(255, 0, 0), RGB(0, 0, 255)
	grid := [][]InputCell{{
		{Symbol: "a", Fg: red, HasFg: true, Bg: blue, HasBg: true},
		{Symbol: "b", Fg: red, HasFg: true, Bg: blue, HasBg: true},
	}}
	term := NewTerminalFromCells(grid, TerminalConfig{
		Width: 4, Height: 3, ExistingColorHandling: DynamicExistingColors,
	})
	engine := NewEngine(term, NewRng(12))
	d, ok := Lookup("middleout")
	if !ok {
		t.Fatal("middleout is not registered")
	}
	frames, err := Run(d.New(), engine, 40000)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(frames) == 0 {
		t.Fatal("the effect produced no frames")
	}
	last := frames[len(frames)-1]
	rows := nonBlank(last)
	if len(rows) != 1 || rows[0] != "ab" {
		t.Fatalf("the final frame reads %q, want %q", rows, "ab")
	}
	for _, want := range []string{"\x1b[38;2;255;0;0m", "\x1b[48;2;0;0;255m"} {
		if !strings.Contains(last, want) {
			t.Errorf("the final frame does not carry %q: %q", want, last)
		}
	}
	// The picture must not already be assembled on the first frame. This
	// effect builds the screen up, so frame one is one cell, not the input.
	if plain(frames[0]) == plain(last) {
		t.Error("the first frame already shows the finished picture")
	}
}
