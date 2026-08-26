package tuiffects

import "testing"

// ringsProbeText is wide enough to fill the first two rings on the canvas the
// geometry tests use, so ring 0 is full and the assertions are about a ring
// rather than about a handful of stragglers.
const ringsProbeText = "ring around the rosie"

// TestRingsSettlesIntoTheInputText runs rings to completion and checks the
// text comes back where it started.
//
// Negative control: pointing the "home" path's waypoint at canvas.Center
// instead of ch.InputCoord piles every character into the middle of the
// canvas, and the final frame no longer reads as the input.
func TestRingsSettlesIntoTheInputText(t *testing.T) {
	const input = "ring around"
	term := NewTerminalFromText(input, TerminalConfig{Width: 24, Height: 9})
	engine := NewEngine(term, NewRng(5))

	frames, err := Run(NewRings(DefaultRingsConfig()), engine, 20000)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(frames) == 0 {
		t.Fatal("the effect produced no frames")
	}
	if len(frames) >= 20000 {
		t.Fatal("the effect never finished within the frame cap")
	}
	rows := nonBlank(frames[len(frames)-1])
	if len(rows) != 1 || rows[0] != input {
		t.Errorf("the final frame reads %q, want %q", rows, input)
	}
}

// TestRingsTakesTheTextApart checks the effect animates at all: some frame in
// the middle of the run has to differ from the input.
//
// Negative control: never activating the initial disperse path in
// startInitialDisperse leaves every character sitting in the text and every
// frame reads as the input.
func TestRingsTakesTheTextApart(t *testing.T) {
	const input = "ring around"
	term := NewTerminalFromText(input, TerminalConfig{Width: 24, Height: 9})
	engine := NewEngine(term, NewRng(5))

	frames, err := Run(NewRings(DefaultRingsConfig()), engine, 20000)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(frames) < 100 {
		t.Fatalf("the effect ran for %d frames, want a real animation", len(frames))
	}
	moved := false
	for _, frame := range frames {
		rows := nonBlank(frame)
		if len(rows) != 1 || rows[0] != input {
			moved = true
			break
		}
	}
	if !moved {
		t.Error("every frame reads as the input, so nothing was ever taken apart")
	}
}

// TestRingsSpinsCharactersAroundTheCenter checks the thing the effect is named
// for: while it is spinning, a ring's characters ride that ring's circle and
// go all the way round it.
//
// Both halves matter. A character parked on one cell of the circle satisfies
// the first check and never turns; a character wandering the canvas satisfies
// neither. The measured run keeps every ring 0 character on the circle for
// about seventy percent of the spin frames, so half is a floor with headroom.
//
// Negative control: dropping the e.ChainPaths call in addCharacterToRing
// leaves each character on the single ring cell its first path reaches. It
// still sits on the circle, so the on-circle check still passes, and the
// quadrant check fails with one or two quadrants visited instead of four.
func TestRingsSpinsCharactersAroundTheCenter(t *testing.T) {
	term := NewTerminalFromText(ringsProbeText, TerminalConfig{Width: 40, Height: 15})
	engine := NewEngine(term, NewRng(5))
	effect := NewRings(DefaultRingsConfig())
	if err := effect.Build(engine); err != nil {
		t.Fatalf("Build: %v", err)
	}
	if len(effect.rings) == 0 || len(effect.rings[0].characters) == 0 {
		t.Fatal("the build made no populated rings")
	}

	center := term.Canvas.Center
	ring := effect.rings[0]
	onCircle := map[Coord]bool{}
	for _, coord := range ring.counterClockwise {
		onCircle[coord] = true
	}

	spinFrames := 0
	onRing := map[*Character]int{}
	quadrants := map[*Character]map[int]bool{}
	for _, ch := range ring.characters {
		quadrants[ch] = map[int]bool{}
	}
	for i := 0; i < 20000; i++ {
		if !effect.Advance(engine) {
			break
		}
		if effect.phase != ringsPhaseSpin {
			continue
		}
		spinFrames++
		for _, ch := range ring.characters {
			coord := ch.Motion.CurrentCoord
			if onCircle[coord] {
				onRing[ch]++
			}
			quadrants[ch][ringsQuadrant(center, coord)] = true
		}
	}
	if spinFrames < 100 {
		t.Fatalf("the run spent %d frames spinning, want a real spin phase", spinFrames)
	}
	for _, ch := range ring.characters {
		if share := float64(onRing[ch]) / float64(spinFrames); share < 0.5 {
			t.Errorf("%q sat on its ring for %.0f%% of the spin frames, want at least 50%%",
				ch.InputSymbol, share*100)
		}
		if got := len(quadrants[ch]); got != 4 {
			t.Errorf("%q reached %d quadrants around the centre, want all 4", ch.InputSymbol, got)
		}
	}
}

// ringsQuadrant buckets a coordinate into one of the four quadrants around a
// centre. It is only used by the tests, to say whether a character went all
// the way round rather than rocking back and forth on one side.
func ringsQuadrant(center, coord Coord) int {
	quadrant := 0
	if coord.Column-center.Column >= 0 {
		quadrant |= 1
	}
	if coord.Row-center.Row >= 0 {
		quadrant |= 2
	}
	return quadrant
}

// TestRingsScattersBetweenSpins checks the other half of the cycle: while the
// effect is dispersed, the ring's characters are off the circle.
//
// The measured run keeps every ring 0 character off the circle for at least
// sixty-five percent of the disperse frames, so half is a floor with headroom.
//
// Negative control: making makeDisperseWaypoints put all five waypoints on the
// origin instead of picking them out of FindCoordsInRect leaves the characters
// standing where the scatter began, which is on the circle, and this fails.
func TestRingsScattersBetweenSpins(t *testing.T) {
	term := NewTerminalFromText(ringsProbeText, TerminalConfig{Width: 40, Height: 15})
	engine := NewEngine(term, NewRng(5))
	effect := NewRings(DefaultRingsConfig())
	if err := effect.Build(engine); err != nil {
		t.Fatalf("Build: %v", err)
	}
	if len(effect.rings) == 0 || len(effect.rings[0].characters) == 0 {
		t.Fatal("the build made no populated rings")
	}

	ring := effect.rings[0]
	onCircle := map[Coord]bool{}
	for _, coord := range ring.counterClockwise {
		onCircle[coord] = true
	}

	disperseFrames := 0
	offRing := map[*Character]int{}
	for i := 0; i < 20000; i++ {
		if !effect.Advance(engine) {
			break
		}
		if effect.phase != ringsPhaseDisperse {
			continue
		}
		disperseFrames++
		for _, ch := range ring.characters {
			if !onCircle[ch.Motion.CurrentCoord] {
				offRing[ch]++
			}
		}
	}
	if disperseFrames < 100 {
		t.Fatalf("the run spent %d frames dispersed, want a real disperse phase", disperseFrames)
	}
	for _, ch := range ring.characters {
		if share := float64(offRing[ch]) / float64(disperseFrames); share < 0.5 {
			t.Errorf("%q stayed on its ring for %.0f%% of the disperse frames, want it scattered",
				ch.InputSymbol, (1-share)*100)
		}
	}
}

// TestRingsShowsTheTextFromTheFirstFrame pins the opening.
//
// The rings have to form out of text the viewer has already seen, so the
// picture is on screen and untouched for the first hundred frames. That is
// upstream's behaviour and it is also what DynamicExistingColors needs, so
// this effect takes the screen apart rather than assembling it and no
// deviation is scoped to the colour policy.
//
// Negative control: dropping the e.Terminal.SetCharacterVisibility(ch, true)
// call in Build leaves the canvas blank and the first check fails on an empty
// frame.
func TestRingsShowsTheTextFromTheFirstFrame(t *testing.T) {
	const input = "ring around"
	term := NewTerminalFromText(input, TerminalConfig{Width: 24, Height: 9})
	engine := NewEngine(term, NewRng(5))
	effect := NewRings(DefaultRingsConfig())
	if err := effect.Build(engine); err != nil {
		t.Fatalf("Build: %v", err)
	}
	for i := 0; i < ringsOpeningFrames; i++ {
		if !effect.Advance(engine) {
			t.Fatalf("the effect ended during the opening, at frame %d", i)
		}
		rows := nonBlank(engine.Frame())
		if len(rows) != 1 || rows[0] != input {
			t.Fatalf("frame %d reads %q, want the untouched input %q", i, rows, input)
		}
	}
	// Once the opening is over the characters have to actually leave.
	left := false
	for i := 0; i < 20000 && !left; i++ {
		if !effect.Advance(engine) {
			break
		}
		for _, ch := range term.InputCharacters {
			if ch.Motion.CurrentCoord != ch.InputCoord {
				left = true
				break
			}
		}
	}
	if !left {
		t.Error("no character moved after the opening ended")
	}
}

// TestRingsResolvesToInputColors checks the dynamic colour policy: a character
// that arrived with its own foreground and background settles back into both,
// rather than into the effect's gradient with the background dropped.
//
// The background half is the one that matters on a captured screen. An effect
// that sets only a foreground blanks every selection bar and filled panel for
// the length of the run.
//
// Negative control: dropping the "dynamic && ch.UsesInputColors" branch in
// Build so final is always Fg(mapping.At(...)) makes the settled colour the
// gradient's and leaves no background at all, so both checks fail.
func TestRingsResolvesToInputColors(t *testing.T) {
	fg, bg := RGB(12, 200, 90), RGB(40, 40, 90)
	cells := [][]InputCell{{
		{Symbol: "x", Fg: fg, HasFg: true, Bg: bg, HasBg: true},
	}}
	term := NewTerminalFromCells(cells, TerminalConfig{
		Width: 14, Height: 7, ExistingColorHandling: DynamicExistingColors,
	})
	engine := NewEngine(term, NewRng(5))
	effect := NewRings(DefaultRingsConfig())
	if err := effect.Build(engine); err != nil {
		t.Fatalf("Build: %v", err)
	}
	for i := 0; i < 20000; i++ {
		if !effect.Advance(engine) {
			break
		}
	}
	got := term.InputCharacters[0].Animation.CurrentVisual().Colors
	if !got.HasFg || got.Fg != fg {
		t.Errorf("the settled foreground is %v, want the input's own %v", got, fg)
	}
	if !got.HasBg || got.Bg != bg {
		t.Errorf("the settled background is %v, want the input's own %v", got, bg)
	}
}
