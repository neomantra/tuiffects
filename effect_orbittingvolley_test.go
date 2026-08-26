package tuiffects

import "testing"

// TestOrbittingVolleyAssemblesTheInputText runs the effect to completion and
// checks the text is left on screen with the launchers gone.
//
// Negative control: dropping the final branch that hides the launchers leaves
// a block glyph in the frame and the row comparison fails.
func TestOrbittingVolleyAssemblesTheInputText(t *testing.T) {
	const input = "orbit"
	term := NewTerminalFromText(input, TerminalConfig{Width: 20, Height: 5})
	engine := NewEngine(term, NewRng(7))

	frames, err := Run(NewOrbittingVolley(DefaultOrbittingVolleyConfig()), engine, 40000)
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
}

// TestOrbittingVolleyFiresInVolleys is the animation check. The text must
// arrive a volley at a time rather than all at once, so at the halfway frame
// only some of the characters are on screen and the frame does not yet read as
// the finished text.
//
// A frame-to-frame comparison alone would not catch this: the launchers move
// every frame, so even an effect that revealed the whole text immediately
// would still produce a changing picture. The count of visible characters is
// what makes the assertion about the text.
//
// Negative control: setting the delay back to zero after a volley, so one
// fires every frame, puts every character on screen within four frames and the
// halfway count then matches the final one.
func TestOrbittingVolleyFiresInVolleys(t *testing.T) {
	term := NewTerminalFromText("orbitting volley", TerminalConfig{Width: 30, Height: 6})
	engine := NewEngine(term, NewRng(7))
	effect := NewOrbittingVolley(DefaultOrbittingVolleyConfig())
	if err := effect.Build(engine); err != nil {
		t.Fatalf("Build: %v", err)
	}
	var visiblePerFrame []int
	var frames []string
	for i := 0; i < 40000; i++ {
		if !effect.Advance(engine) {
			break
		}
		count := 0
		for _, ch := range term.InputCharacters {
			if ch.IsVisible {
				count++
			}
		}
		visiblePerFrame = append(visiblePerFrame, count)
		frames = append(frames, engine.Frame())
	}
	if len(frames) < 20 {
		t.Fatalf("the effect ran for %d frames, want a real animation", len(frames))
	}
	total := len(term.InputCharacters)
	middle := len(frames) / 2
	if visiblePerFrame[middle] >= total {
		t.Errorf("frame %d of %d already shows all %d characters, so nothing arrives in volleys",
			middle, len(frames), total)
	}
	if visiblePerFrame[len(visiblePerFrame)-1] != total {
		t.Errorf("the last frame shows %d of %d characters",
			visiblePerFrame[len(visiblePerFrame)-1], total)
	}
	if frames[middle] == frames[len(frames)-1] {
		t.Error("the middle frame already reads as the finished text")
	}
}

// TestOrbittingVolleyOrbitsFourLaunchers is the test named for the effect. The
// four launchers must ride the edge of the canvas a quarter turn apart: the
// top one walks left to right along the top row while the other three hold the
// right column, the bottom row and the left column, and every one of them
// stays on the perimeter.
//
// Negative control: placing the three trailing launchers from the top
// launcher's coordinate rather than from its progress, by deleting the body of
// setLauncherCoordinates, leaves all four stacked in the corners they were
// added at. Run that way the right launcher never leaves row Top and the
// bottom launcher never leaves column Right, so both spread checks fail.
func TestOrbittingVolleyOrbitsFourLaunchers(t *testing.T) {
	term := NewTerminalFromText("orbit", TerminalConfig{Width: 21, Height: 9})
	engine := NewEngine(term, NewRng(7))
	effect := NewOrbittingVolley(DefaultOrbittingVolleyConfig())
	if err := effect.Build(engine); err != nil {
		t.Fatalf("Build: %v", err)
	}
	if len(effect.launchers) != 4 {
		t.Fatalf("the effect built %d launchers, want 4", len(effect.launchers))
	}
	canvas := term.Canvas

	topColumns := map[int]bool{}
	rightRows := map[int]bool{}
	bottomColumns := map[int]bool{}
	leftRows := map[int]bool{}

	for i := 0; i < 40000; i++ {
		if !effect.Advance(engine) {
			break
		}
		coords := make([]Coord, 4)
		for j, launcher := range effect.launchers {
			coords[j] = launcher.character.Motion.CurrentCoord
		}
		for j, coord := range coords {
			onEdge := coord.Row == canvas.Top || coord.Row == canvas.Bottom ||
				coord.Column == canvas.Left || coord.Column == canvas.Right
			if !onEdge {
				t.Fatalf("frame %d put launcher %d at %v, which is off the perimeter", i, j, coord)
			}
		}
		if coords[0].Row != canvas.Top {
			t.Fatalf("frame %d took the top launcher off the top row, to %v", i, coords[0])
		}
		if coords[1].Column != canvas.Right {
			t.Fatalf("frame %d took the right launcher off the right column, to %v", i, coords[1])
		}
		if coords[2].Row != canvas.Bottom {
			t.Fatalf("frame %d took the bottom launcher off the bottom row, to %v", i, coords[2])
		}
		if coords[3].Column != canvas.Left {
			t.Fatalf("frame %d took the left launcher off the left column, to %v", i, coords[3])
		}
		topColumns[coords[0].Column] = true
		rightRows[coords[1].Row] = true
		bottomColumns[coords[2].Column] = true
		leftRows[coords[3].Row] = true
	}

	if len(topColumns) < canvas.Right/2 {
		t.Errorf("the top launcher visited %d columns of %d, want it to cross the canvas",
			len(topColumns), canvas.Right)
	}
	if len(rightRows) < 3 {
		t.Errorf("the right launcher visited %d rows, want it to travel down the column", len(rightRows))
	}
	if len(bottomColumns) < 3 {
		t.Errorf("the bottom launcher visited %d columns, want it to travel along the row",
			len(bottomColumns))
	}
	if len(leftRows) < 3 {
		t.Errorf("the left launcher visited %d rows, want it to travel up the column", len(leftRows))
	}
}

// TestOrbittingVolleyFliesCharactersInFromALauncher checks a character does
// not simply appear at home. Every character is teleported to a launcher on
// the frame it is fired, so the origin of the flight its path records must sit
// on the perimeter of the canvas, and it must end up back at its own cell.
//
// Negative control: dropping the SetCoordinate in launch, so a fired character
// starts where it already stands, leaves every origin at the character's input
// coordinate. The text is anchored in the middle of the canvas so that none of
// those coordinates touch an edge, and the perimeter check then fails.
func TestOrbittingVolleyFliesCharactersInFromALauncher(t *testing.T) {
	term := NewTerminalFromText("orbitting volley", TerminalConfig{
		Width: 30, Height: 7, AnchorText: AnchorC,
	})
	engine := NewEngine(term, NewRng(7))
	effect := NewOrbittingVolley(DefaultOrbittingVolleyConfig())
	if err := effect.Build(engine); err != nil {
		t.Fatalf("Build: %v", err)
	}
	canvas := term.Canvas
	onEdge := func(coord Coord) bool {
		return coord.Row == canvas.Top || coord.Row == canvas.Bottom ||
			coord.Column == canvas.Left || coord.Column == canvas.Right
	}

	shown := map[*Character]bool{}
	for i := 0; i < 40000; i++ {
		if !effect.Advance(engine) {
			break
		}
		for _, ch := range term.InputCharacters {
			if ch.IsVisible {
				shown[ch] = true
			}
		}
	}

	if len(shown) != len(term.InputCharacters) {
		t.Errorf("%d of %d characters were ever shown", len(shown), len(term.InputCharacters))
	}
	for _, ch := range term.InputCharacters {
		path := ch.Motion.Path(orbittingVolleyInputPath)
		if path == nil || len(path.Segments) == 0 {
			t.Fatalf("%q never flew anywhere", ch.InputSymbol)
		}
		origin := path.Segments[0].Start.Coord
		if !onEdge(origin) {
			t.Errorf("%q flew in from %v, which is not on the perimeter", ch.InputSymbol, origin)
		}
		if ch.Motion.CurrentCoord != ch.InputCoord {
			t.Errorf("%q ended at %v, want its home %v",
				ch.InputSymbol, ch.Motion.CurrentCoord, ch.InputCoord)
		}
	}
}

// TestOrbittingVolleyKeepsInputColorsWhenDynamic checks the dynamic branch. The
// effect assembles the screen rather than passing over it, so a character must
// stay hidden until a launcher fires it, and must land wearing the colour and
// background it arrived with.
//
// Negative control: replacing the dynamic branch with the gradient colour, so
// final is always Fg(mapping.At(...)), loses the background and the colour
// check fails. Showing every character at build time, the deviation a sweep
// needs, makes the hidden check fail.
func TestOrbittingVolleyKeepsInputColorsWhenDynamic(t *testing.T) {
	grid := [][]InputCell{{
		{Symbol: "a", Fg: RGB(10, 20, 30), HasFg: true, Bg: RGB(40, 50, 60), HasBg: true},
		{Symbol: "b", Fg: RGB(70, 80, 90), HasFg: true, Bg: RGB(11, 22, 33), HasBg: true},
	}}
	term := NewTerminalFromCells(grid, TerminalConfig{
		Width: 12, Height: 4, ExistingColorHandling: DynamicExistingColors,
	})
	engine := NewEngine(term, NewRng(7))
	effect := NewOrbittingVolley(DefaultOrbittingVolleyConfig())
	if err := effect.Build(engine); err != nil {
		t.Fatalf("Build: %v", err)
	}
	for _, ch := range term.InputCharacters {
		if ch.IsVisible {
			t.Errorf("%q is visible before it was fired; this effect assembles the screen",
				ch.InputSymbol)
		}
	}
	for i := 0; i < 40000; i++ {
		if !effect.Advance(engine) {
			break
		}
	}
	for _, ch := range term.InputCharacters {
		got := ch.Animation.CurrentVisual().Colors
		want := ch.Animation.InputColors
		if got != want {
			t.Errorf("%q settled wearing %+v, want its input colours %+v",
				ch.InputSymbol, got, want)
		}
	}
}
