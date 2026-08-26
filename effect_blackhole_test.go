package tuiffects

import (
	"strings"
	"testing"
)

// blackholeTestInput is wide enough that the ring does not swallow the whole
// input: the ring holds three characters per unit of radius, so a handful of
// characters would leave nothing for the starfield.
const blackholeTestInput = "the quick brown fox\njumps over the lazy\ndog and then keeps"

// TestBlackholeReassemblesTheInput runs blackhole to completion and checks the
// text comes back, having first been taken apart.
//
// Negative controls, both run and confirmed failing:
//   - pointing the home path's waypoint at the landing coord instead of
//     ch.InputCoord leaves every character a few cells from where it belongs,
//     and the final frame no longer reads as the input;
//   - dropping the ch.Motion.SetCoordinate call that throws a character out
//     into the starfield leaves it on its input coordinate, and nothing is
//     painted outside the text block on the first frame.
func TestBlackholeReassemblesTheInput(t *testing.T) {
	term := NewTerminalFromText(blackholeTestInput, TerminalConfig{Width: 60, Height: 20})
	engine := NewEngine(term, NewRng(3))

	frames, err := Run(NewBlackhole(DefaultBlackholeConfig()), engine, 40000)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(frames) == 0 {
		t.Fatal("the effect produced no frames")
	}
	if len(frames) >= 40000 {
		t.Fatal("the effect never finished within the frame cap")
	}

	want := strings.Split(blackholeTestInput, "\n")
	if got := nonBlank(frames[len(frames)-1]); !blackholeRowsEqual(got, want) {
		t.Errorf("the final frame reads %q, want %q", got, want)
	}
	// The first frame is the starfield, which is thrown across the whole
	// canvas. Without this an effect that never moved anything would still
	// pass the check above.
	outside := 0
	for _, coord := range blackholePaintedCells(frames[0]) {
		if !term.Canvas.CoordIsInText(coord) {
			outside++
		}
	}
	if outside == 0 {
		t.Error("the first frame paints nothing outside the text block, so nothing was scattered")
	}
}

// TestBlackholeSwallowsEverythingIntoOneCell checks the thing the effect is
// named for. Between the ring collapsing and the singularity going off, every
// single character stands on the middle of the canvas and the whole screen is
// down to that one cell.
//
// The rendered frame alone is not enough here, and an earlier version of this
// test that only counted painted cells passed its own negative control. A
// swallowed character ends its scene on a blank, so it disappears from the
// frame wherever it happens to be standing; the frame going quiet says the
// ring collapsed, not that anything was dragged in. So the coordinates are
// checked as well.
//
// Negative control: pointing the singularity path's waypoint at ch.InputCoord
// instead of canvas.Center means the characters that are not on the ring go
// home instead of being pulled in, and they are never all on the centre. Run
// and confirmed failing.
func TestBlackholeSwallowsEverythingIntoOneCell(t *testing.T) {
	term := NewTerminalFromText(blackholeTestInput, TerminalConfig{Width: 60, Height: 20})
	engine := NewEngine(term, NewRng(3))
	effect := NewBlackhole(DefaultBlackholeConfig())
	if err := effect.Build(engine); err != nil {
		t.Fatalf("Build: %v", err)
	}

	center := term.Canvas.Center
	swallowed := false
	for i := 0; i < 40000 && !effect.phaseIsPast(blackholeComplete); i++ {
		if !effect.Advance(engine) {
			break
		}
		cells := blackholePaintedCells(engine.Frame())
		if len(cells) != 1 || cells[0] != center {
			continue
		}
		onCenter := true
		for _, ch := range term.InputCharacters {
			if ch.Motion.CurrentCoord != center {
				onCenter = false
				break
			}
		}
		if onCenter {
			swallowed = true
			break
		}
	}
	if !swallowed {
		t.Errorf("no frame had every character on the single cell at %v, so nothing was swallowed", center)
	}
}

// TestBlackholeFormsARingAroundTheCentre checks the ring itself: before
// anything is eaten, the characters chosen for the ring stand on a circle of
// blackholeRadius around the middle of the canvas.
//
// Negative control: pointing the formation path's waypoint at ch.InputCoord
// instead of its slot on the circle leaves the ring characters in the text,
// which is nowhere near the circle, and no frame has all of them on it. Run
// and confirmed failing.
func TestBlackholeFormsARingAroundTheCentre(t *testing.T) {
	term := NewTerminalFromText(blackholeTestInput, TerminalConfig{Width: 60, Height: 20})
	engine := NewEngine(term, NewRng(3))
	effect := NewBlackhole(DefaultBlackholeConfig())
	if err := effect.Build(engine); err != nil {
		t.Fatalf("Build: %v", err)
	}
	if len(effect.blackholeChars) == 0 {
		t.Fatal("the effect picked no ring characters")
	}

	slots := map[Coord]bool{}
	for _, coord := range FindCoordsOnCircle(
		term.Canvas.Center, effect.blackholeRadius, len(effect.blackholeChars), true) {
		slots[coord] = true
	}
	// The circle is drawn with the column offset doubled so it looks round in
	// a cell grid, so every slot is within the radius vertically and within
	// twice it horizontally. Checking that here stops a circle helper that
	// quietly changed shape from being accepted by the membership test alone.
	for coord := range slots {
		if abs(coord.Row-term.Canvas.Center.Row) > effect.blackholeRadius ||
			abs(coord.Column-term.Canvas.Center.Column) > 2*effect.blackholeRadius {
			t.Fatalf("ring slot %v is not within radius %d of %v",
				coord, effect.blackholeRadius, term.Canvas.Center)
		}
	}

	formed := false
	for i := 0; i < 40000 && effect.phase == blackholeForming; i++ {
		if !effect.Advance(engine) {
			break
		}
		onRing := 0
		for _, ch := range effect.blackholeChars {
			if slots[ch.Motion.CurrentCoord] {
				onRing++
			}
		}
		if onRing == len(effect.blackholeChars) {
			formed = true
			break
		}
	}
	if !formed {
		t.Errorf("the %d ring characters never all stood on the circle at once",
			len(effect.blackholeChars))
	}
}

// TestBlackholeCoolsBackIntoTheInputColors checks the dynamic colour policy. A
// character that arrived carrying a foreground and a background cools out of
// the explosion into both of them, rather than into the effect's gradient.
//
// The background matters as much as the foreground here: on a captured screen
// it is every selection bar and filled panel, and an effect that restored only
// the foreground would hand the screen back with its chrome missing.
//
// Negative control: dropping the dynamic branch from buildCoolingScene, so the
// ramp always ends at finalColors, leaves the settled foreground at the
// gradient colour and the background unset, and both checks fail. Run and
// confirmed failing.
func TestBlackholeCoolsBackIntoTheInputColors(t *testing.T) {
	wantFg := RGB(12, 200, 90)
	wantBg := RGB(40, 10, 70)
	cells := [][]InputCell{{
		{Symbol: "a", Fg: wantFg, HasFg: true, Bg: wantBg, HasBg: true},
		{Symbol: "b", Fg: wantFg, HasFg: true, Bg: wantBg, HasBg: true},
		{Symbol: "c", Fg: wantFg, HasFg: true, Bg: wantBg, HasBg: true},
	}}
	term := NewTerminalFromCells(cells, TerminalConfig{
		Width: 24, Height: 10, ExistingColorHandling: DynamicExistingColors,
	})
	engine := NewEngine(term, NewRng(5))
	effect := NewBlackhole(DefaultBlackholeConfig())
	if err := effect.Build(engine); err != nil {
		t.Fatalf("Build: %v", err)
	}
	for i := 0; i < 40000; i++ {
		if !effect.Advance(engine) {
			break
		}
	}
	if err := effect.Err(); err != nil {
		t.Fatalf("the effect stopped on an error: %v", err)
	}

	for _, ch := range term.InputCharacters {
		got := ch.Animation.CurrentVisual().Colors
		if !got.HasFg || got.Fg != wantFg {
			t.Errorf("%q settled on foreground %v, want the input's own %v",
				ch.InputSymbol, got, wantFg)
		}
		if !got.HasBg || got.Bg != wantBg {
			t.Errorf("%q settled on background %v, want the input's own %v",
				ch.InputSymbol, got, wantBg)
		}
	}
}

// phaseIsPast reports whether the effect has reached a phase or gone beyond it.
// The phases only ever run forwards, so a test can use this as a stop
// condition. Note that Collapsing hands straight on to Exploding inside one
// Advance, so only Complete is a useful stop for the collapse itself.
func (b *Blackhole) phaseIsPast(phase blackholePhase) bool { return b.phase >= phase }

// blackholePaintedCells lists the coordinates a rendered frame actually put something
// in, in canvas coordinates with row 1 at the bottom.
func blackholePaintedCells(frame string) []Coord {
	var out []Coord
	lines := strings.Split(plain(frame), "\n")
	for index, line := range lines {
		row := len(lines) - index
		for column, r := range []rune(line) {
			if r != ' ' {
				out = append(out, C(column+1, row))
			}
		}
	}
	return out
}

// blackholeRowsEqual compares two sets of frame rows.
func blackholeRowsEqual(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}
