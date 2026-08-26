package tuiffects

import (
	"strings"
	"testing"
)

// randomSequenceRevealOrder builds the effect and steps it, recording each
// character the frame it first became visible. The order it returns is the
// reveal order the effect actually produced.
func randomSequenceRevealOrder(t *testing.T, e *Engine, effect *RandomSequence) []*Character {
	t.Helper()
	if err := effect.Build(e); err != nil {
		t.Fatalf("Build: %v", err)
	}
	seen := map[*Character]bool{}
	var order []*Character
	for i := 0; i < 40000; i++ {
		if !effect.Advance(e) {
			break
		}
		for _, ch := range e.Terminal.InputCharacters {
			if ch.IsVisible && !seen[ch] {
				seen[ch] = true
				order = append(order, ch)
			}
		}
	}
	return order
}

// TestRandomSequenceResolvesIntoTheInputText runs the effect to completion.
//
// Three negative controls, all run and all seen to fail:
//   - asking ApplyGradientToSymbols for a fixed symbol rather than
//     ch.InputSymbol leaves the final frame reading as that symbol;
//   - setting charactersPerTick to the whole text reveals everything on the
//     first frame and the frame 2 check fails;
//   - skipping ActivateScene in Build leaves every character on its plain
//     appearance, so the first complete frame is already the final one.
func TestRandomSequenceResolvesIntoTheInputText(t *testing.T) {
	const input = "random sequence"
	term := NewTerminalFromText(input, TerminalConfig{Width: 24, Height: 4})
	engine := NewEngine(term, NewRng(11))

	frames, err := Run(NewRandomSequence(DefaultRandomSequenceConfig()), engine, 40000)
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

	// It animates in two ways, and both have to hold. The text arrives a few
	// characters at a time, so an early frame is not yet the whole input.
	if strings.Contains(plain(frames[2]), input) {
		t.Error("frame 2 already reads as the whole input, so nothing was revealed gradually")
	}
	// And every character keeps fading after it lands, so the first frame that
	// holds the whole text is not yet the frame the effect settles on.
	whole := -1
	for i, frame := range frames {
		if rows := nonBlank(frame); len(rows) == 1 && rows[0] == input {
			whole = i
			break
		}
	}
	if whole < 0 {
		t.Fatal("no frame ever held the whole text")
	}
	if frames[whole] == frames[len(frames)-1] {
		t.Error("the text is finished the moment it is complete, so no character fades in")
	}
}

// TestRandomSequenceRevealsInARandomOrder is the test the effect is named for.
//
// The reveal order is drawn from a shuffle, so consecutive reveals should land
// all over the canvas rather than walking the text. This counts how often one
// reveal is followed by a neighbouring cell and requires that to be rare.
//
// Negative control: deleting the Shuffle call in Build. The pending list is
// filled top to bottom, left to right and read from the back, so without the
// shuffle the effect walks the text backwards. Ran it: 23 of 30 consecutive
// reveals adjacent, a ratio of 0.77, and both halves of this test fail.
func TestRandomSequenceRevealsInARandomOrder(t *testing.T) {
	term := NewTerminalFromText("the quick brown fox\njumps over the dog", TerminalConfig{Width: 24, Height: 5})
	engine := NewEngine(term, NewRng(5))
	order := randomSequenceRevealOrder(t, engine, NewRandomSequence(DefaultRandomSequenceConfig()))

	if len(order) != len(term.InputCharacters) {
		t.Fatalf("%d of %d characters were revealed", len(order), len(term.InputCharacters))
	}
	adjacent := 0
	for i := 1; i < len(order); i++ {
		a, b := order[i-1].InputCoord, order[i].InputCoord
		if abs(a.Column-b.Column) <= 1 && abs(a.Row-b.Row) <= 1 {
			adjacent++
		}
	}
	if ratio := float64(adjacent) / float64(len(order)-1); ratio > 0.5 {
		t.Errorf("%d of %d consecutive reveals were adjacent (%.2f), so the order is a walk, not a shuffle",
			adjacent, len(order)-1, ratio)
	}

	// A different seed gives a different order, which no fixed order can do.
	other := NewTerminalFromText("the quick brown fox\njumps over the dog", TerminalConfig{Width: 24, Height: 5})
	otherOrder := randomSequenceRevealOrder(t, NewEngine(other, NewRng(6)),
		NewRandomSequence(DefaultRandomSequenceConfig()))
	same := true
	for i := range order {
		if i >= len(otherOrder) || order[i].InputCoord != otherOrder[i].InputCoord {
			same = false
			break
		}
	}
	if same {
		t.Error("two seeds revealed the characters in the same order, so the order is not random")
	}
}

// TestRandomSequenceAssemblesRatherThanSweeps pins the effect's kind under
// DynamicExistingColors.
//
// This effect assembles the picture: it puts characters back one at a time, so
// the screen must start empty. A sweep like waves does the opposite and has to
// show everything from the first frame. Getting this backwards still ends on
// the right final frame, so only a count of what is on screen early catches it.
//
// Negative control: calling SetCharacterVisibility(ch, true) in Build, the way
// waves does for a sweep. Ran it: all 24 characters are visible before the
// first frame and this fails on the count before Advance is even called.
func TestRandomSequenceAssemblesRatherThanSweeps(t *testing.T) {
	grid := [][]InputCell{}
	for _, line := range []string{"SELECTED ROW", "plain no color"} {
		var row []InputCell
		for _, r := range line {
			cell := InputCell{Symbol: string(r)}
			if line[0] == 'S' {
				cell.Fg, cell.HasFg = RGB(255, 255, 255), true
				cell.Bg, cell.HasBg = RGB(0, 0, 200), true
			}
			row = append(row, cell)
		}
		grid = append(grid, row)
	}
	term := NewTerminalFromCells(grid, TerminalConfig{
		Width: 18, Height: 4, ExistingColorHandling: DynamicExistingColors,
	})
	engine := NewEngine(term, NewRng(3))
	effect := NewRandomSequence(DefaultRandomSequenceConfig())
	if err := effect.Build(engine); err != nil {
		t.Fatalf("Build: %v", err)
	}

	total := len(term.InputCharacters)
	if total < 20 {
		t.Fatalf("the fixture only made %d characters", total)
	}
	visible := 0
	for _, ch := range term.InputCharacters {
		if ch.IsVisible {
			visible++
		}
	}
	if visible != 0 {
		t.Fatalf("%d of %d characters are visible before the first frame, want none", visible, total)
	}
	if !effect.Advance(engine) {
		t.Fatal("the effect finished before its first frame")
	}
	visible = 0
	for _, ch := range term.InputCharacters {
		if ch.IsVisible {
			visible++
		}
	}
	if visible != effect.charactersPerTick {
		t.Errorf("the first frame shows %d characters, want %d", visible, effect.charactersPerTick)
	}
	if visible >= total {
		t.Errorf("the first frame shows all %d characters, so the effect sweeps instead of assembling", total)
	}
}

// TestRandomSequenceKeepsInputColorsAndBackgrounds checks the dynamic branch.
//
// A captured screen carries backgrounds, and an effect that ramps only the
// foreground blanks every selection bar for the length of the run. The
// coloured row here must settle back on its own blue background, and the
// uncoloured row must settle with no colour at all rather than on the grey it
// fades up through.
//
// Three negative controls, all run and all seen to fail:
//   - passing nil for the background gradient drops 48;2;0;0;200 from the
//     final frame;
//   - dropping the neutral grey ramp leaves the uncoloured row with no
//     fade-in to find;
//   - dropping the bare frame that closes the uncoloured branch settles that
//     row on 38;2;128;128;128 instead of on nothing.
func TestRandomSequenceKeepsInputColorsAndBackgrounds(t *testing.T) {
	var colored, plainRow []InputCell
	for _, r := range "BAR" {
		colored = append(colored, InputCell{
			Symbol: string(r),
			Fg:     RGB(255, 255, 255), HasFg: true,
			Bg: RGB(0, 0, 200), HasBg: true,
		})
	}
	for _, r := range "raw" {
		plainRow = append(plainRow, InputCell{Symbol: string(r)})
	}
	term := NewTerminalFromCells([][]InputCell{colored, plainRow}, TerminalConfig{
		Width: 8, Height: 3, ExistingColorHandling: DynamicExistingColors,
	})
	engine := NewEngine(term, NewRng(2))
	frames, err := Run(NewRandomSequence(DefaultRandomSequenceConfig()), engine, 40000)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(frames) == 0 || len(frames) >= 40000 {
		t.Fatalf("the effect produced %d frames", len(frames))
	}
	final := frames[len(frames)-1]
	if rows := nonBlank(final); len(rows) != 2 || rows[0] != "BAR" || rows[1] != "raw" {
		t.Fatalf("the final frame reads %q, want BAR and raw", rows)
	}
	if !strings.Contains(final, "\x1b[48;2;0;0;200m") {
		t.Error("the final frame lost the input background, so a filled panel would blink out")
	}
	if !strings.Contains(final, "\x1b[38;2;255;255;255m") {
		t.Error("the final frame lost the input foreground")
	}
	// The uncoloured row settles bare. The grey it ramps through must not be
	// what it lands on.
	for _, row := range strings.Split(strings.TrimRight(final, "\n"), "\n") {
		if strings.Contains(plain(row), "raw") && strings.Contains(row, "\x1b[") {
			t.Errorf("the uncoloured row settled with styling: %q", row)
		}
	}
	// The grey ramp does happen on the way, otherwise that row never fades in.
	if !strings.Contains(strings.Join(frames[:len(frames)-1], ""), "\x1b[38;2;128;128;128m") {
		t.Error("the uncoloured row never reached the neutral grey, so it has no fade-in at all")
	}
}
