package tuiffects

import (
	"strings"
	"testing"
)

// burnTerminal builds a terminal with the fill characters burn declares it
// needs. A terminal without them is running a different effect: the spanning
// tree cannot start and Build fails.
func burnTerminal(text string, width, height int) *Terminal {
	return NewTerminalFromText(text, TerminalConfig{
		Width: width, Height: height, MakeFillCharacters: true,
	})
}

// burnIgnitionFrames runs a burn to completion and records, for every input
// character, the first frame on which it showed a burn symbol rather than its
// own. Paper and the cooling that follows both wear the input symbol, so a
// character that differs from it is one the fire is on.
func burnIgnitionFrames(t *testing.T, effect *Burn, e *Engine) map[*Character]int {
	t.Helper()
	if err := effect.Build(e); err != nil {
		t.Fatalf("Build: %v", err)
	}
	first := map[*Character]int{}
	for frame := 0; frame < 5000; frame++ {
		if !effect.Advance(e) {
			return first
		}
		for _, ch := range e.Terminal.InputCharacters {
			if _, seen := first[ch]; seen {
				continue
			}
			if ch.Animation.CurrentVisual().Symbol != ch.InputSymbol {
				first[ch] = frame
			}
		}
	}
	t.Fatal("the effect never finished within the frame cap")
	return nil
}

// TestBurnSettlesIntoTheInputText runs burn to completion and checks the text
// it leaves behind, and that it took its time getting there.
//
// Negative control: replacing ch.InputSymbol with a fixed "#" in
// addCoolingFrames leaves the final frame reading "############" and the
// comparison against the input fails. Run and watched fail.
func TestBurnSettlesIntoTheInputText(t *testing.T) {
	const input = "burn it down"
	engine := NewEngine(burnTerminal(input, 20, 5), NewRng(3))

	frames, err := Run(NewBurn(DefaultBurnConfig()), engine, 5000)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(frames) == 0 {
		t.Fatal("the effect produced no frames")
	}
	if len(frames) >= 5000 {
		t.Fatal("the effect never finished within the frame cap")
	}
	rows := nonBlank(frames[len(frames)-1])
	if len(rows) != 1 || rows[0] != input {
		t.Errorf("the final frame reads %q, want %q", rows, input)
	}
	if plain(frames[len(frames)/2]) == plain(frames[len(frames)-1]) {
		t.Error("the middle frame already reads as the finished text, so nothing animated")
	}
}

// TestBurnSpreadsToNeighbouringCells checks the thing the effect is named for.
// Fire does not appear all over the text at once: it starts at one cell and
// crosses to cells it is touching, which is what the spanning tree is for.
//
// The input is a solid block with no spaces, so every cell of the text
// boundary holds a burnable character and the tree's order is a strict
// adjacency chain. At most one character may catch with nothing already
// alight beside it, and that is the one the fire started at. It is often none,
// because a frame lights two to four characters at once and the second of them
// is usually the first one's neighbour.
//
// Negative control: replacing the tree's order in Build with
// e.Terminal.GetCharacters(e.Rng, InputOnly(), SortRandom) lights cells all
// over the block, so thirteen of the seventy-two catch with no burning
// neighbour and this fails. Run and watched fail.
//
// The block is deliberately large. On a small one a random order satisfies
// this by luck, because the whole block is alight within five frames.
func TestBurnSpreadsToNeighbouringCells(t *testing.T) {
	term := burnTerminal(strings.Join([]string{
		"abcdefghijkl",
		"mnopqrstuvwx",
		"yzabcdefghij",
		"klmnopqrstuv",
		"wxyzabcdefgh",
		"ijklmnopqrst",
	}, "\n"), 12, 6)
	engine := NewEngine(term, NewRng(3))
	first := burnIgnitionFrames(t, NewBurn(DefaultBurnConfig()), engine)

	if len(first) != len(term.InputCharacters) {
		t.Fatalf("%d of %d characters never caught fire",
			len(term.InputCharacters)-len(first), len(term.InputCharacters))
	}

	var isolated []string
	for ch, lit := range first {
		neighbourLit := false
		for _, neighbor := range term.Neighbors(ch) {
			if when, seen := first[neighbor]; seen && when <= lit {
				neighbourLit = true
				break
			}
		}
		if !neighbourLit {
			isolated = append(isolated, ch.InputSymbol)
		}
	}
	if len(isolated) > 1 {
		t.Errorf("%d characters caught fire with nothing alight beside them (%v), want at most the one the fire started at",
			len(isolated), isolated)
	}
}

// TestBurnPassesOverRatherThanAssembles pins which kind of effect this is.
//
// Burn plays over paper that is already on the screen, so under
// DynamicExistingColors every character must be showing from the first frame,
// wearing the unburnt colour. That is the opposite of an effect like wipe,
// which builds the picture up behind its line and must start on an empty
// canvas. Getting this backwards gives an animation that resolves correctly
// and shows a blank screen with a fire crawling over nothing.
//
// Negative control: passing false to SetCharacterVisibility in Build, which is
// how an assembling effect starts, leaves the first frame empty and this
// fails. Run and watched fail.
func TestBurnPassesOverRatherThanAssembles(t *testing.T) {
	term := NewTerminalFromCells(burnGrid(6, 2), TerminalConfig{
		Width: 6, Height: 2, MakeFillCharacters: true,
		ExistingColorHandling: DynamicExistingColors,
	})
	engine := NewEngine(term, NewRng(3))
	frames, err := Run(NewBurn(DefaultBurnConfig()), engine, 5000)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(frames) == 0 {
		t.Fatal("the effect produced no frames")
	}
	// The fire is already on two to four cells by the time the first frame is
	// read, so the first frame is not the input text. What it must be is
	// full: no cell of the picture is missing.
	firstRows := strings.Split(plain(frames[0]), "\n")
	if len(firstRows) != 2 {
		t.Fatalf("the first frame shows %d rows, want the whole picture from the start", len(firstRows))
	}
	for i, row := range firstRows {
		if strings.Contains(row, " ") || len([]rune(row)) != 6 {
			t.Errorf("row %d of the first frame reads %q, want six filled cells for an effect that passes over", i, row)
		}
	}
	last := nonBlank(frames[len(frames)-1])
	if len(last) != 2 {
		t.Fatalf("the final frame has %d rows, want 2", len(last))
	}
	for _, row := range last {
		if row != "abcdef" {
			t.Errorf("the final frame has a row reading %q, want %q", row, "abcdef")
		}
	}
}

// burnGrid builds a block of coloured cells, the shape a captured screen
// arrives in.
func burnGrid(width, height int) [][]InputCell {
	grid := make([][]InputCell, height)
	for row := range grid {
		grid[row] = make([]InputCell, width)
		for column := range grid[row] {
			grid[row][column] = InputCell{
				Symbol: string(rune('a' + column)),
				Fg:     RGB(20, 200, 40), HasFg: true,
				Bg: RGB(0, 0, 90), HasBg: true,
			}
		}
	}
	return grid
}

// TestBurnKeepsInputBackgroundsUnderDynamicColors checks the deviation Build
// documents. A captured screen is full of selection bars and filled panels,
// and burn sets a foreground on every frame of both its scenes, so without
// this the background is gone from the first frame to the last but one.
//
// The background is checked before the fire arrives, while the character is
// alight, and after it has settled: it is the same colour throughout, and the
// foreground still does the whole fire.
//
// Negative control: dropping the two "dynamic && input.HasBg" branches in
// Build, so the paper is Fg(StartingColor) and the burn scene has no
// background gradient, leaves every frame but the settled ones with no
// background at all and this fails. Run and watched fail.
func TestBurnKeepsInputBackgroundsUnderDynamicColors(t *testing.T) {
	term := NewTerminalFromCells(burnGrid(3, 1), TerminalConfig{
		Width: 3, Height: 1, MakeFillCharacters: true,
		ExistingColorHandling: DynamicExistingColors,
	})
	engine := NewEngine(term, NewRng(3))
	effect := NewBurn(DefaultBurnConfig())
	if err := effect.Build(engine); err != nil {
		t.Fatalf("Build: %v", err)
	}

	watched := term.InputCharacters[0]
	want := RGB(0, 0, 90)
	sawFire := false
	for frame := 0; frame < 5000; frame++ {
		visual := watched.Animation.CurrentVisual()
		if !visual.Colors.HasBg || visual.Colors.Bg != want {
			t.Fatalf("on frame %d the character wears %+v, want the input background 000059",
				frame, visual.Colors)
		}
		if visual.Symbol != watched.InputSymbol {
			sawFire = true
		}
		if !effect.Advance(engine) {
			break
		}
	}
	if !sawFire {
		t.Error("the watched character never showed a burn symbol, so the background survived an effect that did nothing")
	}
	settled := watched.Animation.CurrentVisual()
	if !settled.Colors.HasFg || settled.Colors.Fg != RGB(20, 200, 40) {
		t.Errorf("the settled character wears %+v, want the input foreground 14c828", settled.Colors)
	}
}

// TestBurnGivesOffSmoke checks the particles reach the screen. They rise off
// the top of the canvas from the cell that made them, so they are the only
// thing burn ever puts above the text block.
//
// Negative control: this is asserted both ways round in one test. A smoke
// chance of zero must leave the space above the text empty for the whole run,
// which is what fails if emitSmoke stops reading the chance; a chance of one
// must fill it, which is what fails if the particles are never emitted,
// activated or shown.
func TestBurnGivesOffSmoke(t *testing.T) {
	aboveTheText := func(chance float64) int {
		term := burnTerminal("smoke\nrises", 12, 10)
		engine := NewEngine(term, NewRng(3))
		config := DefaultBurnConfig()
		config.SmokeChance = chance
		frames, err := Run(NewBurn(config), engine, 5000)
		if err != nil {
			t.Fatalf("Run: %v", err)
		}
		if len(frames) == 0 {
			t.Fatal("the effect produced no frames")
		}
		// The text sits on the bottom rows, so everything above the text
		// block is smoke and nothing else.
		blank := 10 - term.Canvas.TextTop
		seen := 0
		for _, frame := range frames {
			rows := strings.Split(plain(frame), "\n")
			if strings.TrimSpace(strings.Join(rows[:blank], "")) != "" {
				seen++
			}
		}
		return seen
	}
	if got := aboveTheText(0); got != 0 {
		t.Errorf("a smoke chance of zero put something above the text on %d frames, want none", got)
	}
	if got := aboveTheText(1); got == 0 {
		t.Error("a smoke chance of one never put anything above the text")
	}
}

// TestBurnDeclaresItNeedsFillCharacters covers the Descriptor. The fire
// follows a spanning tree over the whole text block, and the tree starts at a
// random coordinate in it, so a block with a gap in it needs a character in
// that gap or Build fails outright.
//
// Negative control: building the same terminal without MakeFillCharacters is
// the second half of this test, and it is the failure the declaration
// prevents.
func TestBurnDeclaresItNeedsFillCharacters(t *testing.T) {
	descriptor, ok := Lookup("burn")
	if !ok {
		t.Fatal("burn is not registered")
	}
	if !descriptor.NeedsFillCharacters {
		t.Error("the burn descriptor does not declare NeedsFillCharacters, so a host builds it a terminal its spanning tree cannot start in")
	}

	// A block with a gap in the middle of its text boundary and no fill
	// characters to stand in it. The starting coordinate is random, so a
	// single seed can land on a character by luck; a spread of them cannot.
	failures := 0
	for seed := uint64(0); seed < 20; seed++ {
		term := NewTerminalFromText("ab\n  \ncd", TerminalConfig{Width: 4, Height: 4})
		err := NewBurn(DefaultBurnConfig()).Build(NewEngine(term, NewRng(seed)))
		if err == nil {
			continue
		}
		if err != ErrNoStartingCharacter {
			t.Fatalf("seed %d failed with %v, want ErrNoStartingCharacter", seed, err)
		}
		failures++
	}
	if failures == 0 {
		t.Error("twenty seeds all started the tree in a text block with two empty cells and no fill characters, so the declaration guards nothing")
	}
}
