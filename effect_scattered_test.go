package tuiffects

import "testing"

// TestScatteredGathersIntoTheInputText runs scattered to completion and checks
// every character arrives where it belongs.
//
// Negative control: pointing the path's waypoint at the start coordinate
// instead of ch.InputCoord leaves the characters where they were scattered and
// the final frame does not read as the input.
func TestScatteredGathersIntoTheInputText(t *testing.T) {
	const input = "gather up"
	term := NewTerminalFromText(input, TerminalConfig{Width: 20, Height: 5})
	engine := NewEngine(term, NewRng(4))

	frames, err := Run(NewScattered(DefaultScatteredConfig()), engine, 40000)
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

// TestScatteredStartsScattered checks the thing the effect is named for: the
// characters are somewhere else when the run begins.
//
// It reads the rendered frame rather than the motion state, because a
// character can hold a scattered coordinate and still be painted in place.
//
// Negative control: dropping the ch.Motion.SetCoordinate(start) call in Build
// leaves every character on its input coordinate, so the first frame reads as
// the input and both checks here fail.
func TestScatteredStartsScattered(t *testing.T) {
	const input = "scatter me"
	term := NewTerminalFromText(input, TerminalConfig{Width: 30, Height: 9})
	engine := NewEngine(term, NewRng(4))

	frames, err := Run(NewScattered(DefaultScatteredConfig()), engine, 40000)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	first := nonBlank(frames[0])
	if len(first) == 1 && first[0] == input {
		t.Fatal("the first frame already reads as the input, so nothing was scattered")
	}
	// The text is one row, so a scatter across a nine row canvas has to put
	// characters on rows the text never occupies.
	if len(first) < 2 {
		t.Errorf("the first frame occupies %d rows, want the characters spread over several", len(first))
	}
}

// TestScatteredHoldsStillBeforeMoving checks the opening hold. Upstream shows
// the scattered picture for 25 frames without stepping anything, and an effect
// that starts moving at once loses the moment the scatter is readable.
//
// Negative control: removing the holdFrames branch from Advance makes a
// character move within the first 25 frames and the first check fails.
func TestScatteredHoldsStillBeforeMoving(t *testing.T) {
	term := NewTerminalFromText("hold", TerminalConfig{Width: 24, Height: 8})
	engine := NewEngine(term, NewRng(4))
	effect := NewScattered(DefaultScatteredConfig())
	if err := effect.Build(engine); err != nil {
		t.Fatalf("Build: %v", err)
	}

	start := map[*Character]Coord{}
	for _, ch := range term.InputCharacters {
		start[ch] = ch.Motion.CurrentCoord
	}
	for i := 0; i < scatteredHoldFrames; i++ {
		if !effect.Advance(engine) {
			t.Fatalf("the effect ended during the hold, at frame %d", i)
		}
		for ch, want := range start {
			if ch.Motion.CurrentCoord != want {
				t.Fatalf("frame %d moved %q from %v to %v during the hold",
					i, ch.InputSymbol, want, ch.Motion.CurrentCoord)
			}
		}
	}

	// Once the hold is over the characters have to actually travel.
	moved := false
	for i := 0; i < 40000 && !moved; i++ {
		if !effect.Advance(engine) {
			break
		}
		for ch, was := range start {
			if ch.Motion.CurrentCoord != was {
				moved = true
				break
			}
		}
	}
	if !moved {
		t.Error("no character moved after the hold ended")
	}
}

// TestScatteredResolvesToInputColors checks the dynamic colour policy: a
// character that arrived with its own colours settles back into them rather
// than into the effect's gradient.
//
// Negative control: dropping the useInput branch in Build so final is always
// the gradient colour makes the settled foreground the gradient's and this
// fails.
func TestScatteredResolvesToInputColors(t *testing.T) {
	want := RGB(12, 200, 90)
	cells := [][]InputCell{{
		{Symbol: "x", Fg: want, HasFg: true},
	}}
	term := NewTerminalFromCells(cells, TerminalConfig{
		Width: 12, Height: 5, ExistingColorHandling: DynamicExistingColors,
	})
	engine := NewEngine(term, NewRng(4))
	effect := NewScattered(DefaultScatteredConfig())
	if err := effect.Build(engine); err != nil {
		t.Fatalf("Build: %v", err)
	}
	for i := 0; i < 40000; i++ {
		if !effect.Advance(engine) {
			break
		}
	}
	ch := term.InputCharacters[0]
	got := ch.Animation.CurrentVisual().Colors
	if !got.HasFg || got.Fg != want {
		t.Errorf("the settled colour is %v, want the input's own %v", got, want)
	}
}
