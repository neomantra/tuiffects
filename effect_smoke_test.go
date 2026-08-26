package tuiffects

import (
	"strings"
	"testing"
)

// smokeTerminal builds the terminal smoke expects. The effect declares
// NeedsFillCharacters because its spanning tree runs over canvas cells rather
// than over the text, so a test that leaves them out is running a different
// effect from the one a host runs.
func smokeTerminal(input string, width, height int) *Terminal {
	return NewTerminalFromText(input, TerminalConfig{
		Width: width, Height: height, MakeFillCharacters: true,
	})
}

// smokeSymbolSet is the default smoke symbols as a lookup.
func smokeSymbolSet() map[string]bool {
	set := map[string]bool{}
	for _, symbol := range DefaultSmokeConfig().SmokeSymbols {
		set[symbol] = true
	}
	return set
}

// smokeCellCount is how many characters are currently drawing a smoke symbol.
func smokeCellCount(term *Terminal, set map[string]bool) int {
	count := 0
	for _, ch := range term.Characters {
		if set[ch.Animation.CurrentVisual().Symbol] {
			count++
		}
	}
	return count
}

// TestSmokeResolvesToTheInputText runs smoke to completion.
//
// Negative controls, both run and both seen to fail:
//
//   - giving the paint scene a fixed symbol instead of ch.InputSymbol. The
//     final frame then reads as rows of that symbol across the text block
//     instead of the input.
//   - never activating the smoke scene, so the paint runs straight away. The
//     opening frame then already reads as the input and the animation check
//     fails.
//
// The middle frame is checked against the last one rather than against the
// input, because it is only the opening of the run that changes the symbols:
// the paint scene that follows the smoke draws the input symbol the whole way
// and moves the colour alone.
func TestSmokeResolvesToTheInputText(t *testing.T) {
	const first, second = "hello world", "second line"
	term := smokeTerminal(first+"\n"+second, 30, 9)
	engine := NewEngine(term, NewRng(7))

	frames, err := Run(NewSmoke(DefaultSmokeConfig()), engine, 40000)
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
	if len(rows) != 2 || rows[0] != first || rows[1] != second {
		t.Errorf("the final frame reads %q, want %q", rows, []string{first, second})
	}
	// The opening frame must have smoke standing where text is, and the
	// middle frame must still be moving.
	if opening := nonBlank(frames[0]); len(opening) == 2 &&
		opening[0] == first && opening[1] == second {
		t.Error("the opening frame already reads as the input, so no smoke was drawn")
	}
	if frames[len(frames)/2] == frames[len(frames)-1] {
		t.Error("the middle frame is identical to the last one, so nothing animated")
	}
}

// TestSmokeSeepsOutFromOneCell is the test for the thing the effect is named
// for. The smoke is lit on a single cell and spreads outwards along a spanning
// tree, so it can only ever reach a cell next to one it has already reached.
//
// Negative controls, both run and both seen to fail:
//
//   - activating the smoke scene on every character in Build instead of on the
//     spanning tree's starting character alone. The whole canvas smokes before
//     the first frame and the opening count is 288 rather than 1.
//   - replacing the breadth-first layer in Advance with a random pick from the
//     characters the smoke has not reached yet. The counts still rise and fall,
//     and the run still resolves to the input, but the smoke lands on cells
//     with no smoking neighbour and the adjacency check fails.
func TestSmokeSeepsOutFromOneCell(t *testing.T) {
	term := smokeTerminal(strings.TrimSpace(strings.Repeat("smoke test line\n", 8)), 24, 12)
	engine := NewEngine(term, NewRng(4))
	effect := NewSmoke(DefaultSmokeConfig())
	if err := effect.Build(engine); err != nil {
		t.Fatalf("Build: %v", err)
	}
	set := smokeSymbolSet()

	// Build lights the starting cell and nothing else.
	if got := smokeCellCount(term, set); got != 1 {
		t.Fatalf("%d characters are smoking before the first frame, want the starting cell alone", got)
	}

	firstSmoked := map[*Character]int{}
	peak := 0
	for frame := 0; frame < 40000; frame++ {
		for _, ch := range term.Characters {
			if _, seen := firstSmoked[ch]; !seen && set[ch.Animation.CurrentVisual().Symbol] {
				firstSmoked[ch] = frame
			}
		}
		peak = max(peak, smokeCellCount(term, set))
		if !effect.Advance(engine) {
			break
		}
	}

	if len(firstSmoked) < 50 {
		t.Fatalf("the smoke reached %d cells, want it to cross the text block", len(firstSmoked))
	}
	if peak < 10 {
		t.Errorf("at most %d cells smoked at once, want the smoke to spread out", peak)
	}

	// Every cell the smoke reached, bar the one it started on, sits next to a
	// cell it reached earlier.
	opening := 0
	for ch, frame := range firstSmoked {
		if frame == 0 {
			opening++
			continue
		}
		adjacent := false
		for _, neighbor := range term.Neighbors(ch) {
			if earlier, seen := firstSmoked[neighbor]; seen && earlier < frame {
				adjacent = true
				break
			}
		}
		if !adjacent {
			t.Errorf("the smoke reached %v on frame %d with no smoking neighbour before it",
				ch.InputCoord, frame)
		}
	}
	if opening != 1 {
		t.Errorf("%d cells were smoking on the opening frame, want the starting cell alone", opening)
	}
}

// TestSmokeKeepsThePictureUnderDynamicColors covers the one place this port
// leaves upstream, and it is scoped to DynamicExistingColors.
//
// Smoke passes over the screen rather than assembling it, so the picture must
// read from the first frame. Upstream starts every character on flat black
// with no background, which blanks a captured screen for as long as the smoke
// takes to cross it and drops every background with it. Here a character
// starts on a dimmed copy of the colours it arrived with, keeps its background
// throughout, and is left wearing exactly what it came in with.
//
// The fixture background is deliberately dark, because that is where this
// goes wrong quietly: dimming a dark panel does not dull it, it sinks it into
// the terminal's own black. So the background is asserted to arrive unchanged
// rather than merely to be present.
//
// Negative control: putting upstream's base colours back, ColorPair Fg black
// with no background. Run that way the opening foreground is black, the
// opening background is gone, and the first two checks fail. Dimming the
// background by smokeDimFactor, as this port first did, fails the second
// check on its own.
func TestSmokeKeepsThePictureUnderDynamicColors(t *testing.T) {
	fg, bg := RGB(240, 240, 240), RGB(31, 41, 55)
	var grid [][]InputCell
	for row := 0; row < 3; row++ {
		var cells []InputCell
		for _, symbol := range []string{"b", "a", "r", "s"} {
			cells = append(cells, InputCell{Symbol: symbol, Fg: fg, HasFg: true, Bg: bg, HasBg: true})
		}
		grid = append(grid, cells)
	}
	term := NewTerminalFromCells(grid, TerminalConfig{
		Width: 10, Height: 6, MakeFillCharacters: true,
		ExistingColorHandling: DynamicExistingColors,
	})
	engine := NewEngine(term, NewRng(11))
	effect := NewSmoke(DefaultSmokeConfig())
	if err := effect.Build(engine); err != nil {
		t.Fatalf("Build: %v", err)
	}

	black := MustParseColor("000000")
	set := smokeSymbolSet()
	for _, ch := range term.InputCharacters {
		if !ch.IsVisible {
			t.Fatalf("%q at %v is hidden before the first frame", ch.InputSymbol, ch.InputCoord)
		}
		opening := ch.Animation.CurrentVisual().Colors
		if !opening.HasBg || opening.Bg != bg {
			t.Fatalf("%q opens with background %v, want exactly the one it arrived with, %v",
				ch.InputSymbol, opening, bg)
		}
		if !opening.HasFg || opening.Fg == black {
			t.Fatalf("%q opens with foreground %v, want the one it arrived with, dimmed",
				ch.InputSymbol, opening)
		}
		// The one cell the smoke starts on is already showing the smoke, which
		// is drawn at full strength, so it is the only one exempt here.
		if opening.Fg == fg && !set[ch.Animation.CurrentVisual().Symbol] {
			t.Errorf("%q opens on the colour it settles on, so nothing is dimmed", ch.InputSymbol)
		}
	}

	frames := 0
	for ; frames < 40000 && effect.Advance(engine); frames++ {
		for _, ch := range term.InputCharacters {
			if got := ch.Animation.CurrentVisual().Colors; !got.HasBg {
				t.Fatalf("%q lost its background on frame %d", ch.InputSymbol, frames+1)
			}
		}
	}
	if frames == 0 || frames >= 40000 {
		t.Fatalf("the effect ran for %d frames, want a run that finishes", frames)
	}
	for _, ch := range term.InputCharacters {
		got := ch.Animation.CurrentVisual().Colors
		if !got.HasFg || got.Fg != fg || !got.HasBg || got.Bg != bg {
			t.Errorf("%q settled on %v, want the colours it arrived with, %v on %v",
				ch.InputSymbol, got, fg, bg)
		}
	}
}
