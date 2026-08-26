package tuiffects

import (
	"strings"
	"testing"
)

// overflowTerminal builds the terminal overflow expects. The effect declares
// NeedsFillCharacters, so a test that leaves them out is running a different
// effect from the one a host gets.
func overflowTerminal(input string, width, height int) *Terminal {
	return NewTerminalFromText(input, TerminalConfig{
		Width: width, Height: height, MakeFillCharacters: true,
	})
}

// TestOverflowScrollsTheInputBackIntoPlace runs overflow to completion.
//
// Negative control: dropping the final rows from the pending queue in Build,
// so only the scrambled copies scroll past. Run that way the effect still
// terminates within the cap, but it stops with the copies stranded on the
// canvas and the final frame reads as six rows of scrambled text. Only the
// final-frame check fails; the middle-frame check still passes, because a
// middle frame differs from the input either way.
func TestOverflowScrollsTheInputBackIntoPlace(t *testing.T) {
	const first, second = "hello world", "second line"
	term := overflowTerminal(first+"\n"+second, 20, 8)
	engine := NewEngine(term, NewRng(7))

	frames, err := Run(NewOverflow(DefaultOverflowConfig()), engine, 40000)
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
	// A middle frame must differ, or the effect resolved without animating.
	if middle := nonBlank(frames[len(frames)/2]); len(middle) == 2 &&
		middle[0] == first && middle[1] == second {
		t.Error("the middle frame already reads as the input, so nothing animated")
	}
}

// TestOverflowScrollsCopiesPastBeforeTheRealText is the test for the thing the
// effect is named for. The text overflows: copies of its rows scroll up the
// canvas out of order, and only once they have gone does the real picture
// scroll in behind them.
//
// The three checks are that copies were made at all, that the copies are what
// is on screen while the real characters are still hidden, and that the real
// characters arrive from below the bottom edge rather than being placed.
//
// Negative control: setting OverflowCyclesHigh to zero makes no copies, so the
// added-character check and the copies-on-screen check both fail. Second
// control: dropping the setup call in Advance, so a row enters at the
// coordinate it already holds, leaves the real characters never sitting on
// row 1 and the entry check fails.
func TestOverflowScrollsCopiesPastBeforeTheRealText(t *testing.T) {
	term := overflowTerminal("hello world\nsecond line", 20, 8)
	engine := NewEngine(term, NewRng(11))
	effect := NewOverflow(DefaultOverflowConfig())
	if err := effect.Build(engine); err != nil {
		t.Fatalf("Build: %v", err)
	}

	if len(term.AddedCharacters) == 0 {
		t.Fatal("no copies were made, so nothing overflows")
	}
	if len(term.AddedCharacters)%len(term.InputCharacters) != 0 {
		t.Errorf("%d copies of %d input characters is not a whole number of cycles",
			len(term.AddedCharacters), len(term.InputCharacters))
	}

	// Every real character must enter on row 1 and climb from there.
	enteredOnRowOne := map[*Character]bool{}
	seenVisible := map[*Character]bool{}
	copiesOnScreenWhileTextIsHidden := false

	for frame := 0; frame < 40000; frame++ {
		if !effect.Advance(engine) {
			break
		}
		visibleCopies := 0
		for _, ch := range term.AddedCharacters {
			if ch.IsVisible && overflowOnCanvas(term, ch) {
				visibleCopies++
			}
		}
		visibleText := 0
		for _, ch := range term.InputCharacters {
			if !ch.IsVisible {
				continue
			}
			if !seenVisible[ch] {
				seenVisible[ch] = true
				enteredOnRowOne[ch] = ch.Motion.CurrentCoord.Row == 1
			}
			visibleText++
		}
		if visibleCopies > 0 && visibleText == 0 {
			copiesOnScreenWhileTextIsHidden = true
		}
	}

	if !copiesOnScreenWhileTextIsHidden {
		t.Error("no frame showed copies while the real text was still hidden")
	}
	if len(seenVisible) != len(term.InputCharacters) {
		t.Errorf("%d of %d input characters were never shown",
			len(term.InputCharacters)-len(seenVisible), len(term.InputCharacters))
	}
	for ch, ok := range enteredOnRowOne {
		if !ok {
			t.Fatalf("%q appeared at %v rather than entering on row 1",
				ch.InputSymbol, ch.Motion.CurrentCoord)
		}
	}
}

// overflowLines splits a frame into its rows, top row first, keeping the
// blank ones because where the text is not is half of what these tests check.
func overflowLines(frame string) []string { return strings.Split(frame, "\n") }

// overflowOnCanvas reports whether a character is inside the drawable rectangle. A row
// that has climbed past the top keeps its coordinate and stays visible, so
// visibility alone does not mean it is on screen.
func overflowOnCanvas(term *Terminal, ch *Character) bool {
	coord := ch.Motion.CurrentCoord
	return coord.Row >= 1 && coord.Row <= term.Canvas.Top &&
		coord.Column >= 1 && coord.Column <= term.Canvas.Right
}

// TestOverflowWithoutFillCharactersLandsInTheWrongPlace is why the descriptor
// sets NeedsFillCharacters.
//
// The rows that scroll in last are the whole canvas, one per row, so the last
// one in lands on row 1 and every other one stacks above it. Without fill
// characters only the rows holding text exist. The scroll runs out early, the
// picture never reaches the row it came from, and the copies that were meant
// to leave the canvas are still standing on it when the effect stops.
//
// The text is anchored north-west so the two outcomes are far apart: with fill
// characters it settles back on the top row, without them the top row holds
// something else.
//
// Negative control: this test is the control for the descriptor flag. It
// asserts the failure, so removing NeedsFillCharacters cannot be what breaks
// it; what breaks it is the effect learning to place rows without fills, which
// is the change this pins. The half that does have a control is the first
// assertion: queueing the real rows bottom-to-top instead of top-to-bottom
// leaves the picture upside down and the top row wrong.
func TestOverflowWithoutFillCharactersLandsInTheWrongPlace(t *testing.T) {
	const input = "top line"
	run := func(fills bool) []string {
		term := NewTerminalFromText(input, TerminalConfig{
			Width: 12, Height: 6, AnchorText: AnchorNW, MakeFillCharacters: fills,
		})
		engine := NewEngine(term, NewRng(3))
		frames, err := Run(NewOverflow(DefaultOverflowConfig()), engine, 40000)
		if err != nil {
			t.Fatalf("Run: %v", err)
		}
		if len(frames) == 0 {
			t.Fatal("the effect produced no frames")
		}
		return overflowLines(trimRight(plain(frames[len(frames)-1])))
	}

	withFills := run(true)
	if withFills[0] != input {
		t.Errorf("with fill characters the top row reads %q, want the text back on it", withFills[0])
	}
	for _, line := range withFills[1:] {
		if line != "" {
			t.Errorf("with fill characters a row below the text reads %q, want it blank", line)
		}
	}

	without := run(false)
	if without[0] == input {
		t.Fatal("without fill characters the text still settled on the top row, " +
			"so NeedsFillCharacters no longer earns its place")
	}
}

// TestOverflowResolvesToInputColors checks the dynamic colour policy. A
// captured cell settles back into both of the colours it arrived with, not
// into the effect's own gradient and not with its background dropped.
//
// It also checks the other half of that policy: overflow assembles the screen
// rather than passing over it, so no real character may be on screen before
// the scroll delivers it. An effect that showed them all up front would still
// finish on the right frame and would look completely wrong. The check is on
// visibility rather than on what the frame reads, because the copies wear the
// same symbols and would answer for the originals.
//
// Negative control: dropping the dynamic branch in Build so the colour is
// always the gradient's makes the settled foreground wrong and the background
// absent, and the first two checks fail. Second control: showing every
// character in Build, the way a sweep has to under this policy, leaves real
// characters visible in frame 0 and fails the third.
func TestOverflowResolvesToInputColors(t *testing.T) {
	fg, bg := RGB(12, 200, 90), RGB(40, 0, 80)
	cells := [][]InputCell{
		{{Symbol: "a", Fg: fg, HasFg: true, Bg: bg, HasBg: true}},
		{{Symbol: "b", Fg: fg, HasFg: true, Bg: bg, HasBg: true}},
	}
	term := NewTerminalFromCells(cells, TerminalConfig{
		Width: 6, Height: 4,
		ExistingColorHandling: DynamicExistingColors,
		MakeFillCharacters:    true,
	})
	engine := NewEngine(term, NewRng(4))
	effect := NewOverflow(DefaultOverflowConfig())
	if err := effect.Build(engine); err != nil {
		t.Fatalf("Build: %v", err)
	}

	visibleInFrameZero := 0
	for i := 0; i < 40000; i++ {
		if !effect.Advance(engine) {
			break
		}
		if i > 0 {
			continue
		}
		for _, ch := range term.InputCharacters {
			if ch.IsVisible {
				visibleInFrameZero++
			}
		}
	}

	for _, ch := range term.InputCharacters {
		got := ch.Animation.CurrentVisual().Colors
		if !got.HasFg || got.Fg != fg {
			t.Errorf("%q settled on foreground %v, want the input's own %v",
				ch.InputSymbol, got, fg)
		}
		if !got.HasBg || got.Bg != bg {
			t.Errorf("%q settled on background %v, want the input's own %v",
				ch.InputSymbol, got, bg)
		}
	}

	if visibleInFrameZero != 0 {
		t.Errorf("%d of the %d real characters were on screen in frame 0, want none: "+
			"this effect assembles the screen, it does not pass over it",
			visibleInFrameZero, len(term.InputCharacters))
	}
}
