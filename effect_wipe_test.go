package tuiffects

import "testing"

// wipeRevealOrder runs a wipe to completion and records, for every input
// character, the frame it first became visible on. A character that never
// appears is left out.
func wipeRevealOrder(t *testing.T, effect *Wipe, e *Engine) map[*Character]int {
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
			if ch.IsVisible {
				if _, seen := first[ch]; !seen {
					first[ch] = frame
				}
			}
		}
	}
	t.Fatal("the effect never finished within the frame cap")
	return nil
}

// TestWipeResolvesToTheInputText runs wipe to completion and checks the text
// it leaves behind, and that it took its time getting there.
//
// Negative control: replacing ch.InputSymbol with a fixed "#" in both branches
// of Build leaves the final frame reading "###########", and the comparison
// against the input fails. Run and watched fail.
func TestWipeResolvesToTheInputText(t *testing.T) {
	const input = "wipe me down"
	term := NewTerminalFromText(input, TerminalConfig{Width: 20, Height: 4})
	engine := NewEngine(term, NewRng(4))

	frames, err := Run(NewWipe(DefaultWipeConfig()), engine, 5000)
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

// TestWipeRevealsAlongItsDiagonal checks the thing the effect is named for: a
// line crosses the screen and characters appear behind it, in the order of the
// configured grouping and no other.
//
// Two things are asserted. The top-left corner is reached before the
// bottom-right one, which pins the direction. And at no point is a character
// showing while one from an earlier group is still hidden, which pins the
// grouping itself.
//
// Negative control, direction: setting DefaultWipeConfig's WipeDirection to
// GroupDiagonalBottomRightToTopLeft reverses the sweep and the corner check
// fails. Negative control, grouping: setting it to GroupColumnLeftToRight
// keeps the sweep left to right but releases whole columns, which is not a
// prefix of the diagonal order, and the ordering check fails. Both run and
// watched fail.
func TestWipeRevealsAlongItsDiagonal(t *testing.T) {
	term := NewTerminalFromText("abcdefgh\nijklmnop\nqrstuvwx", TerminalConfig{Width: 8, Height: 3})
	engine := NewEngine(term, NewRng(4))
	first := wipeRevealOrder(t, NewWipe(DefaultWipeConfig()), engine)

	if len(first) != len(term.InputCharacters) {
		t.Fatalf("%d of %d characters never appeared", len(term.InputCharacters)-len(first), len(term.InputCharacters))
	}

	topLeft := term.CharacterAtInputCoord(C(1, term.Canvas.TextTop))
	bottomRight := term.CharacterAtInputCoord(C(term.Canvas.TextRight, 1))
	if topLeft == nil || bottomRight == nil {
		t.Fatal("the corners of the text block are missing")
	}
	if first[topLeft] >= first[bottomRight] {
		t.Errorf("the top-left corner appeared on frame %d and the bottom-right one on frame %d, want the top-left first",
			first[topLeft], first[bottomRight])
	}

	// The expected order is taken from the grouping directly rather than from
	// the effect's config, so an effect that quietly changed grouping fails
	// here instead of moving the goalposts with itself.
	groups := term.GetCharactersGrouped(InputOnly(), GroupDiagonalTopLeftToBottomRight)
	latestSoFar := -1
	for i, group := range groups {
		earliest := 1 << 30
		latest := -1
		for _, ch := range group {
			earliest = min(earliest, first[ch])
			latest = max(latest, first[ch])
		}
		if earliest < latestSoFar {
			t.Errorf("group %d appeared on frame %d, before group %d had finished appearing on frame %d",
				i, earliest, i-1, latestSoFar)
		}
		latestSoFar = latest
	}
}

// TestWipeAssemblesRatherThanPassesOver pins which kind of effect this is.
//
// Wipe builds the picture up behind its line, so under DynamicExistingColors
// every character must still start hidden. That is the opposite of a sweep
// like waves, which shows the whole screen from frame one and passes over it.
// Getting this backwards gives an animation that resolves correctly and shows
// the finished picture before the wipe has touched it.
//
// Negative control: adding the waves-style pre-show to Build, an
// Animation.SetAppearance plus SetCharacterVisibility(ch, true) in the dynamic
// branch, makes the first frame read as the whole input. Run and watched fail.
func TestWipeAssemblesRatherThanPassesOver(t *testing.T) {
	const width, height = 6, 2
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
	term := NewTerminalFromCells(grid, TerminalConfig{
		Width: width, Height: height, ExistingColorHandling: DynamicExistingColors,
	})
	engine := NewEngine(term, NewRng(4))
	frames, err := Run(NewWipe(DefaultWipeConfig()), engine, 5000)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(frames) == 0 {
		t.Fatal("the effect produced no frames")
	}
	if got := nonBlank(frames[0]); len(got) != 0 {
		t.Errorf("the first frame already shows %q, want an empty screen for an effect that assembles", got)
	}
	last := nonBlank(frames[len(frames)-1])
	if len(last) != height {
		t.Fatalf("the final frame has %d rows, want %d", len(last), height)
	}
	for _, row := range last {
		if row != "abcdef" {
			t.Errorf("the final frame has a row reading %q, want %q", row, "abcdef")
		}
	}
}

// TestWipeKeepsInputBackgroundsUnderDynamicColors checks the background of a
// captured cell survives the effect. A screen is full of selection bars and
// filled panels, and an effect that sets only a foreground blanks all of them
// for the length of the run.
//
// Negative control: changing the dynamic branch of Build to Fg(...) of the
// input foreground alone drops the background escape and this fails. Run and
// watched fail.
func TestWipeKeepsInputBackgroundsUnderDynamicColors(t *testing.T) {
	grid := [][]InputCell{{
		{Symbol: "x", Fg: RGB(20, 200, 40), HasFg: true, Bg: RGB(0, 0, 90), HasBg: true},
	}}
	term := NewTerminalFromCells(grid, TerminalConfig{
		Width: 1, Height: 1, ExistingColorHandling: DynamicExistingColors,
	})
	engine := NewEngine(term, NewRng(4))
	if _, err := Run(NewWipe(DefaultWipeConfig()), engine, 5000); err != nil {
		t.Fatalf("Run: %v", err)
	}
	ch := term.InputCharacters[0]
	got := ch.Animation.CurrentVisual()
	if !got.Colors.HasBg || got.Colors.Bg != RGB(0, 0, 90) {
		t.Errorf("the settled character wears %+v, want the input background 000059", got.Colors)
	}
	if !got.Colors.HasFg || got.Colors.Fg != RGB(20, 200, 40) {
		t.Errorf("the settled character wears %+v, want the input foreground 14c828", got.Colors)
	}
}

// TestWipeDelayHoldsTheLineStill checks WipeDelay slows the sweep rather than
// changing what it does.
//
// A delay of two means two frames of waiting for every frame that moves the
// line, so the same text takes noticeably longer and still resolves.
//
// Negative control: replacing the "if w.wipeDelay == 0" guard in Advance with
// an unconditional step, so the wait never happens, makes the delayed run come
// out at the same 138 frames as the undelayed one. Run and watched fail.
func TestWipeDelayHoldsTheLineStill(t *testing.T) {
	run := func(delay int) int {
		term := NewTerminalFromText("slow down", TerminalConfig{Width: 12, Height: 3})
		engine := NewEngine(term, NewRng(4))
		config := DefaultWipeConfig()
		config.WipeDelay = delay
		frames, err := Run(NewWipe(config), engine, 20000)
		if err != nil {
			t.Fatalf("Run: %v", err)
		}
		if len(frames) >= 20000 {
			t.Fatalf("a delay of %d never finished within the frame cap", delay)
		}
		rows := nonBlank(frames[len(frames)-1])
		if len(rows) != 1 || rows[0] != "slow down" {
			t.Errorf("a delay of %d finished reading %q, want %q", delay, rows, "slow down")
		}
		return len(frames)
	}
	quick, slow := run(0), run(2)
	if slow <= quick {
		t.Errorf("a delay of two ran for %d frames and no delay for %d, want the delayed run to be longer", slow, quick)
	}
}
