package tuiffects

import (
	"strings"
	"testing"
)

// sprayPaintedCells counts the cells a frame actually paints, ignoring the blanks
// the canvas pads with.
func sprayPaintedCells(frame string) int {
	count := 0
	for _, r := range plain(frame) {
		if r != ' ' && r != '\n' {
			count++
		}
	}
	return count
}

// TestSpraySettlesIntoTheInputText runs spray to completion.
//
// Negative control: giving the droplet scene a fixed symbol instead of
// ch.InputSymbol leaves the final frame reading as that symbol. Run and
// confirmed failing.
func TestSpraySettlesIntoTheInputText(t *testing.T) {
	const input = "spray paint"
	term := NewTerminalFromText(input, TerminalConfig{Width: 24, Height: 5})
	engine := NewEngine(term, NewRng(7))

	frames, err := Run(NewSpray(DefaultSprayConfig()), engine, 40000)
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
	// The text sits on one row and the nozzle sits on another, so a character
	// in flight puts a glyph on a second row. If no frame ever has two rows,
	// nothing left the nozzle and the effect only revealed the text in place.
	flew := false
	for _, frame := range frames {
		if len(nonBlank(frame)) > 1 {
			flew = true
			break
		}
	}
	if !flew {
		t.Error("no frame ever painted a character off the text row, so nothing flew")
	}
}

// TestSprayFiresEveryCharacterFromTheNozzle is the test for the thing the
// effect is named for: every character starts life at one point and flies out
// of it, and that point moves with the configured position.
//
// Negative control: dropping ch.Motion.SetCoordinate(origin) in Build leaves
// each character sitting on its input coordinate, so every position fails.
// Run and confirmed failing.
func TestSprayFiresEveryCharacterFromTheNozzle(t *testing.T) {
	// Width 20, Height 9: Left 1, Right 20, Bottom 1, Top 9, Center (10,5).
	// floorDiv(Right,2) is 10 and floorDiv(Top,2) is 4, which is a different
	// row from the canvas centre, so a wrong origin cannot pass by accident.
	cases := []struct {
		position SprayPosition
		want     Coord
	}{
		{SprayEast, C(19, 4)},
		{SprayWest, C(1, 4)},
		{SprayNorth, C(10, 9)},
		{SpraySouth, C(10, 1)},
		{SprayNorthEast, C(19, 9)},
		{SprayNorthWest, C(1, 9)},
		{SpraySouthEast, C(19, 1)},
		{SpraySouthWest, C(1, 1)},
		{SprayCenter, C(10, 5)},
	}
	for _, tc := range cases {
		term := NewTerminalFromText("nozzle", TerminalConfig{Width: 20, Height: 9})
		engine := NewEngine(term, NewRng(3))
		config := DefaultSprayConfig()
		config.SprayPosition = tc.position
		effect := NewSpray(config)
		if err := effect.Build(engine); err != nil {
			t.Fatalf("Build: %v", err)
		}
		for _, ch := range term.InputCharacters {
			if got := ch.Motion.CurrentCoord; got != tc.want {
				t.Fatalf("position %d: %q starts at %v, want the nozzle at %v",
					tc.position, ch.InputSymbol, got, tc.want)
			}
		}
	}
}

// TestSprayLiftsCharactersWhileTheyFly checks the layer handoff. A character
// in flight crosses cells that already hold a landed one, so it must draw over
// them until it arrives and then drop back down.
//
// Negative control: removing either SetLayer registration fails this. Without
// the PathActivated one nothing is lifted after Build; without the
// PathComplete one nothing comes back down at the end. Both run and confirmed
// failing.
func TestSprayLiftsCharactersWhileTheyFly(t *testing.T) {
	term := NewTerminalFromText("layers", TerminalConfig{Width: 16, Height: 4})
	engine := NewEngine(term, NewRng(5))
	effect := NewSpray(DefaultSprayConfig())
	if err := effect.Build(engine); err != nil {
		t.Fatalf("Build: %v", err)
	}
	for _, ch := range term.InputCharacters {
		if ch.Layer != 1 {
			t.Fatalf("%q is on layer %d after Build, want 1 for the flight", ch.InputSymbol, ch.Layer)
		}
	}
	for i := 0; i < 40000; i++ {
		if !effect.Advance(engine) {
			break
		}
	}
	for _, ch := range term.InputCharacters {
		if ch.Layer != 0 {
			t.Errorf("%q landed on layer %d, want 0", ch.InputSymbol, ch.Layer)
		}
	}
}

// TestSprayAssemblesRatherThanSweeps pins the behaviour under the colour
// policy a screen saver runs in. Spray builds the picture out of nothing, so
// unlike a sweep it must NOT show every character from the first frame: the
// screen has to fill up as the nozzle reaches it.
//
// Negative control: applying the waves deviation to Build, that is leaving
// every character on its input coordinate and showing it under
// DynamicExistingColors, makes the first frame carry all twelve characters and
// paint all twelve cells. Run and confirmed failing on both assertions.
func TestSprayAssemblesRatherThanSweeps(t *testing.T) {
	red := RGB(255, 0, 0)
	const width = 12
	grid := make([][]InputCell, 1)
	grid[0] = make([]InputCell, width)
	for x := range grid[0] {
		grid[0][x] = InputCell{Symbol: "x", Fg: red, HasFg: true}
	}
	term := NewTerminalFromCells(grid, TerminalConfig{
		Width: width, Height: 1, ExistingColorHandling: DynamicExistingColors,
	})
	engine := NewEngine(term, NewRng(11))
	effect := NewSpray(DefaultSprayConfig())
	if err := effect.Build(engine); err != nil {
		t.Fatalf("Build: %v", err)
	}

	var frames []string
	for i := 0; i < 40000; i++ {
		if !effect.Advance(engine) {
			break
		}
		frames = append(frames, engine.Frame())
		if i > 0 {
			continue
		}
		// The visibility count is the direct statement of the rule, and the
		// painted-cell count catches the other way of getting it wrong:
		// showing everything at once but piled on the nozzle.
		shown := 0
		for _, ch := range term.InputCharacters {
			if ch.IsVisible {
				shown++
			}
		}
		if shown == 0 || shown >= width {
			t.Errorf("the first frame shows %d of %d characters; spray assembles, it does not sweep", shown, width)
		}
		if got := sprayPaintedCells(frames[0]); got >= width {
			t.Errorf("the first frame paints %d of %d cells; spray assembles, it does not sweep", got, width)
		}
	}
	if len(frames) < 2 {
		t.Fatalf("the effect ran for %d frames, want a real animation", len(frames))
	}

	last := frames[len(frames)-1]
	if got := plain(last); got != strings.Repeat("x", width) {
		t.Fatalf("the final frame reads %q, want %q", got, strings.Repeat("x", width))
	}
	if !strings.Contains(last, "\x1b[38;2;255;0;0m") {
		t.Errorf("the final frame does not carry the input's red: %q", last)
	}
}

// TestSprayKeepsTheInputBackground checks a captured cell's background
// survives the flight. On a screen that is every selection bar and filled
// panel, and losing it makes them blink out for the length of the effect.
//
// Negative control: colouring the dynamic droplet frames with Fg(final.Fg)
// instead of the whole input pair drops the background from every frame. Run
// and confirmed failing.
func TestSprayKeepsTheInputBackground(t *testing.T) {
	blue := RGB(0, 0, 255)
	grid := [][]InputCell{{
		{Symbol: "a", Fg: RGB(255, 255, 255), HasFg: true, Bg: blue, HasBg: true},
		{Symbol: "b", Fg: RGB(255, 255, 255), HasFg: true, Bg: blue, HasBg: true},
	}}
	term := NewTerminalFromCells(grid, TerminalConfig{
		Width: 2, Height: 1, ExistingColorHandling: DynamicExistingColors,
	})
	engine := NewEngine(term, NewRng(2))

	frames, err := Run(NewSpray(DefaultSprayConfig()), engine, 40000)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	for i, frame := range frames {
		if sprayPaintedCells(frame) == 0 {
			continue
		}
		if !strings.Contains(frame, "\x1b[48;2;0;0;255m") {
			t.Fatalf("frame %d paints a character without the input's background: %q", i, frame)
		}
	}
}
