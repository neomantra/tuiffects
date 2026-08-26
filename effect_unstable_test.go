package tuiffects

import (
	"strings"
	"testing"
)

// buildUnstable builds the effect over a terminal and hands both back, so the
// tests below can watch the characters rather than only the frames.
func buildUnstable(t *testing.T, term *Terminal, seed uint64) (*Unstable, *Engine) {
	t.Helper()
	engine := NewEngine(term, NewRng(seed))
	effect := NewUnstable(DefaultUnstableConfig())
	if err := effect.Build(engine); err != nil {
		t.Fatalf("Build: %v", err)
	}
	return effect, engine
}

// TestUnstableReassemblesTheInputText runs unstable to completion.
//
// Negative control: pointing the reassembly waypoint at the canvas centre
// instead of ch.InputCoord piles the text into one column and the final frame
// stops reading as the input.
func TestUnstableReassemblesTheInputText(t *testing.T) {
	const input = "unstable"
	term := NewTerminalFromText(input, TerminalConfig{Width: 20, Height: 5})
	engine := NewEngine(term, NewRng(7))

	frames, err := Run(NewUnstable(DefaultUnstableConfig()), engine, 40000)
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
	// It has to animate, not just resolve. Halfway through, the characters are
	// somewhere between the edges of the canvas and home.
	if middle := nonBlank(frames[len(frames)/2]); len(middle) == 1 && middle[0] == input {
		t.Error("the middle frame already reads as the input, so nothing moved")
	}
}

// TestUnstableStartsScrambled checks the jumble, which is the state the whole
// effect starts from: every character stands on some other character's
// coordinate, so the set of occupied cells is the input's set rearranged.
//
// Negative control: dropping the ch.Motion.SetCoordinate(jumbled) call in Build
// leaves every character at home and the displaced count falls to zero.
func TestUnstableStartsScrambled(t *testing.T) {
	term := NewTerminalFromText("scramble me\nplease do", TerminalConfig{Width: 20, Height: 6})
	_, _ = buildUnstable(t, term, 11)

	home := map[Coord]bool{}
	for _, ch := range term.InputCharacters {
		home[ch.InputCoord] = true
	}
	occupied := map[Coord]bool{}
	displaced := 0
	for _, ch := range term.InputCharacters {
		coord := ch.Motion.CurrentCoord
		if !home[coord] {
			t.Fatalf("%q was jumbled to %v, which no character came from", ch.InputSymbol, coord)
		}
		if occupied[coord] {
			t.Fatalf("two characters were jumbled onto %v", coord)
		}
		occupied[coord] = true
		if coord != ch.InputCoord {
			displaced++
		}
	}
	if len(occupied) != len(home) {
		t.Errorf("the jumble fills %d cells, want the input's %d", len(occupied), len(home))
	}
	// A permutation may leave a few characters where they were, but not most.
	if displaced < len(term.InputCharacters)/2 {
		t.Errorf("only %d of %d characters moved, so the input was barely scrambled",
			displaced, len(term.InputCharacters))
	}
}

// TestUnstableShakesTheWholeScreenAsOne is the test unstable is named for. The
// rumble shoves every character by the same one cell at once, so the screen
// reads as a single unstable slab rather than as characters jittering
// independently, and it has to still be shoved when the host reads the frame.
//
// Negative control: putting the characters back at the end of Advance, where
// ttfx puts them back at the end of next_frame, makes every rumble frame render
// on the jumbled coordinates and no shoved frame is ever seen. Shoving each
// character with its own offset fails the same test on the other assertion.
func TestUnstableShakesTheWholeScreenAsOne(t *testing.T) {
	term := NewTerminalFromText("shake\nrattle\nroll", TerminalConfig{Width: 20, Height: 8})
	effect, engine := buildUnstable(t, term, 5)

	jumbled := map[*Character]Coord{}
	for _, ch := range term.InputCharacters {
		jumbled[ch] = ch.Motion.CurrentCoord
	}

	shovedFrames := 0
	for frame := 0; frame < unstableMaxRumbleSteps; frame++ {
		if !effect.Advance(engine) {
			t.Fatalf("the effect stopped at frame %d, before the rumble ended", frame)
		}
		if effect.phase != unstableRumble {
			t.Fatalf("the effect left the rumble at frame %d, before its %d steps ran",
				frame, unstableMaxRumbleSteps)
		}
		first := term.InputCharacters[0]
		offset := C(first.Motion.CurrentCoord.Column-jumbled[first].Column,
			first.Motion.CurrentCoord.Row-jumbled[first].Row)
		for _, ch := range term.InputCharacters {
			got := C(ch.Motion.CurrentCoord.Column-jumbled[ch].Column,
				ch.Motion.CurrentCoord.Row-jumbled[ch].Row)
			if got != offset {
				t.Fatalf("frame %d shoved %q by %v and %q by %v, want one offset for the whole screen",
					frame, first.InputSymbol, offset, ch.InputSymbol, got)
			}
		}
		if offset != C(0, 0) {
			shovedFrames++
		}
	}
	if shovedFrames == 0 {
		t.Error("no frame of the rumble was shoved off the jumbled coordinates")
	}
}

// TestUnstableThrowsEveryCharacterOffAnEdge checks the explosion: each
// character flies to a point on the canvas perimeter and is still there when
// the hold starts, so the screen really does empty out.
//
// Negative control: aiming the explosion waypoint at canvas.Center instead of
// an edge leaves every character in the middle of the canvas and the perimeter
// check fails for all of them.
func TestUnstableThrowsEveryCharacterOffAnEdge(t *testing.T) {
	term := NewTerminalFromText("boom boom", TerminalConfig{Width: 24, Height: 7})
	effect, engine := buildUnstable(t, term, 21)
	canvas := term.Canvas

	onEdge := func(c Coord) bool {
		return c.Column == canvas.Left || c.Column == canvas.Right ||
			c.Row == canvas.Bottom || c.Row == canvas.Top
	}
	for _, ch := range term.InputCharacters {
		target := ch.Motion.Path("explosion").Waypoints[0].Coord
		if !onEdge(target) {
			t.Fatalf("%q is aimed at %v, which is not on the canvas edge", ch.InputSymbol, target)
		}
	}

	// Run until the explosion is over and the hold has started.
	reached := false
	for frame := 0; frame < 40000; frame++ {
		if !effect.Advance(engine) {
			break
		}
		if effect.phase == unstableExplosion && effect.explosionHoldTime < unstableHoldTime {
			reached = true
			break
		}
	}
	if !reached {
		t.Fatal("the explosion never finished")
	}
	for _, ch := range term.InputCharacters {
		if !onEdge(ch.Motion.CurrentCoord) {
			t.Errorf("%q held at %v, want a cell on the canvas edge", ch.InputSymbol, ch.Motion.CurrentCoord)
		}
	}
}

// TestUnstableResolvesTheInputColours runs unstable under the colour policy a
// screen saver uses. A captured cell's background has to come back with it: a
// character that only ever carried a foreground would blank out every filled
// panel on the screen for the length of the effect.
//
// Negative control: dropping the background gradient from the rumble scene, or
// the background half of the final scene, leaves the blue background missing
// from the frames it is asserted on.
func TestUnstableResolvesTheInputColours(t *testing.T) {
	red, blue := RGB(255, 0, 0), RGB(0, 0, 255)
	grid := [][]InputCell{{
		{Symbol: "a", Fg: red, HasFg: true},
		{Symbol: "b", Fg: red, HasFg: true, Bg: blue, HasBg: true},
	}}
	term := NewTerminalFromCells(grid, TerminalConfig{
		Width: 2, Height: 1, ExistingColorHandling: DynamicExistingColors,
	})
	engine := NewEngine(term, NewRng(12))

	frames, err := Run(NewUnstable(DefaultUnstableConfig()), engine, 40000)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(frames) == 0 {
		t.Fatal("the effect produced no frames")
	}
	const redFg, blueBg = "\x1b[38;2;255;0;0m", "\x1b[48;2;0;0;255m"

	last := frames[len(frames)-1]
	if got := plain(last); got != "ab" {
		t.Fatalf("the final frame reads %q, want %q", got, "ab")
	}
	if !strings.Contains(last, redFg) {
		t.Errorf("the final frame does not carry the input's red: %q", last)
	}
	if !strings.Contains(last, blueBg) {
		t.Errorf("the final frame does not carry the input's blue background: %q", last)
	}
	// The picture is on screen from the first frame, wearing its own colours,
	// because unstable rearranges a screen that was already there.
	if !strings.Contains(frames[0], redFg) || !strings.Contains(frames[0], blueBg) {
		t.Errorf("the first frame does not carry the input's colours: %q", frames[0])
	}
	// And the cell that arrived with a background keeps one for the whole run,
	// whatever colour the effect has heated it to.
	for i, frame := range frames {
		if !strings.Contains(plain(frame), "b") {
			continue // b is off the canvas edge on this frame
		}
		if !strings.Contains(frame, "\x1b[48;2;") {
			t.Fatalf("frame %d draws b with no background at all: %q", i, frame)
		}
	}
}
