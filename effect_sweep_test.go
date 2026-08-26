package tuiffects

import (
	"strings"
	"testing"
)

// sweepTerminal builds the terminal sweep expects. The effect declares
// NeedsFillCharacters, so a test that leaves them out is running a different
// effect from the one a host runs.
func sweepTerminal(input string, width, height int) *Terminal {
	return NewTerminalFromText(input, TerminalConfig{
		Width: width, Height: height, MakeFillCharacters: true,
	})
}

// TestSweepResolvesToTheInputText runs sweep to completion.
//
// Negative control: giving the last frame of the second_sweep scene a fixed
// symbol instead of ch.InputSymbol. Run that way the final frame is nine rows
// of thirty X, one per cell of the canvas, and this fails. Confirmed failing.
func TestSweepResolvesToTheInputText(t *testing.T) {
	const first, second = "hello world", "second line"
	term := sweepTerminal(first+"\n"+second, 30, 9)
	engine := NewEngine(term, NewRng(7))

	frames, err := Run(NewSweep(DefaultSweepConfig()), engine, 4000)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(frames) == 0 {
		t.Fatal("the effect produced no frames")
	}
	if len(frames) >= 4000 {
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

// TestSweepUncoversRightToLeftThenColoursLeftToRight is the test for the thing
// the effect is named for. Two bands cross the canvas in turn and in opposite
// directions: the first uncovers the characters in grey, starting at the right
// edge, and only when it has finished does the second start colouring them,
// from the left edge.
//
// Negative controls, both confirmed failing:
//   - Never leaving the first phase, by setting s.complete instead of swapping
//     the easer's sequence. Run that way every character stays grey, the final
//     frame still reads as the input text, and the colouring half of this test
//     fails with no character ever leaving grey.
//   - Swapping the two default directions. Run that way both direction
//     assertions fail, because each band arrives at the wrong edge first.
func TestSweepUncoversRightToLeftThenColoursLeftToRight(t *testing.T) {
	const width, height = 30, 9
	term := sweepTerminal("hello world\nsecond line", width, height)
	engine := NewEngine(term, NewRng(11))
	effect := NewSweep(DefaultSweepConfig())
	if err := effect.Build(engine); err != nil {
		t.Fatalf("Build: %v", err)
	}

	const never = -1
	uncovered := map[int]int{} // column -> frame it was first shown in
	coloured := map[int]int{}  // column -> frame its text left grey in
	for frame := 0; effect.Advance(engine); frame++ {
		for _, ch := range term.Characters {
			column := ch.InputCoord.Column
			if ch.IsVisible {
				if _, seen := uncovered[column]; !seen {
					uncovered[column] = frame
				}
			}
			if ch.IsFill {
				continue
			}
			visual := ch.Animation.CurrentVisual()
			if visual == nil || !ch.IsVisible {
				continue
			}
			// The character rests on its own symbol in mid grey between the
			// two bands, so leaving grey on its own symbol is the second band
			// having gone by.
			if visual.Symbol != ch.InputSymbol || visual.Colors.Fg == sweepMidGray {
				continue
			}
			if _, seen := coloured[column]; !seen {
				coloured[column] = frame
			}
		}
		if frame > 4000 {
			t.Fatal("the effect never finished within the frame cap")
		}
	}

	at := func(m map[int]int, column int) int {
		if frame, ok := m[column]; ok {
			return frame
		}
		return never
	}
	leftUncovered, rightUncovered := at(uncovered, 1), at(uncovered, width)
	if leftUncovered == never || rightUncovered == never {
		t.Fatalf("column 1 uncovered at %d and column %d at %d, want both uncovered",
			leftUncovered, width, rightUncovered)
	}
	if rightUncovered >= leftUncovered {
		t.Errorf("the first band uncovered column %d at frame %d and column 1 at frame %d,"+
			" so it did not travel right to left", width, rightUncovered, leftUncovered)
	}

	// The text spans columns 1 to 11, so those are the columns the second band
	// can be watched on.
	leftColoured, rightColoured := at(coloured, 1), at(coloured, 11)
	if leftColoured == never || rightColoured == never {
		t.Fatalf("column 1 coloured at %d and column 11 at %d, want both coloured",
			leftColoured, rightColoured)
	}
	if leftColoured >= rightColoured {
		t.Errorf("the second band coloured column 1 at frame %d and column 11 at frame %d,"+
			" so it did not travel left to right", leftColoured, rightColoured)
	}

	// The bands run in turn, not together: nothing is coloured until the first
	// band has uncovered the whole canvas.
	lastUncovered := 0
	for _, frame := range uncovered {
		if frame > lastUncovered {
			lastUncovered = frame
		}
	}
	firstColoured := leftColoured
	for _, frame := range coloured {
		if frame < firstColoured {
			firstColoured = frame
		}
	}
	if firstColoured <= lastUncovered {
		t.Errorf("colouring started at frame %d but the canvas was not fully uncovered"+
			" until frame %d, so the two bands overlapped", firstColoured, lastUncovered)
	}
}

// TestSweepAnimatesTheEmptyCanvas covers the Descriptor's NeedsFillCharacters.
// Sweep runs over the whole canvas, so the cells around the text have to
// shimmer too.
//
// Negative control: building the terminal without MakeFillCharacters. Run that
// way no frame ever paints outside the text, the widest frame is as narrow as
// the input, and this fails. Confirmed failing.
func TestSweepAnimatesTheEmptyCanvas(t *testing.T) {
	if descriptor, ok := Lookup("sweep"); !ok {
		t.Fatal("sweep is not registered")
	} else if !descriptor.NeedsFillCharacters {
		t.Error("the sweep descriptor does not declare NeedsFillCharacters," +
			" so a host builds it a terminal with no fill characters")
	}

	const width, height = 30, 9
	term := sweepTerminal("hi", width, height)
	engine := NewEngine(term, NewRng(3))
	frames, err := Run(NewSweep(DefaultSweepConfig()), engine, 4000)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	widest, painted := 0, 0
	for _, frame := range frames {
		for _, row := range strings.Split(trimRight(plain(frame)), "\n") {
			if length := len([]rune(row)); length > widest {
				widest = length
			}
		}
		if rows := nonBlank(frame); len(rows) > painted {
			painted = len(rows)
		}
	}
	if widest < width {
		t.Errorf("the widest frame is %d cells across, want the full %d:"+
			" the canvas outside the text never animated", widest, width)
	}
	if painted < height {
		t.Errorf("the fullest frame paints %d rows, want the full %d:"+
			" the canvas above and below the text never animated", painted, height)
	}
}

// sweepPicture builds a small captured screen: a bar of yellow on blue across
// one row, and a run of green text with no background on the row below it.
func sweepPicture(width, height int) [][]InputCell {
	grid := make([][]InputCell, height)
	for y := range grid {
		grid[y] = make([]InputCell, width)
		for x := range grid[y] {
			switch {
			case y == 1:
				grid[y][x] = InputCell{
					Symbol: "A",
					Fg:     RGB(255, 220, 0), HasFg: true,
					Bg: RGB(0, 0, 160), HasBg: true,
				}
			case y == 2 && x < width/2:
				grid[y][x] = InputCell{Symbol: "b", Fg: RGB(0, 255, 120), HasFg: true}
			default:
				grid[y][x] = InputCell{Symbol: " "}
			}
		}
	}
	return grid
}

// TestSweepAssemblesRatherThanPassesOver pins which kind of effect sweep is
// under DynamicExistingColors.
//
// Sweep assembles the screen. Its first band is the reveal: every character
// starts hidden and is shown only when that band arrives. So the canvas is
// empty in the first frame, unlike an effect such as waves that passes over a
// picture already on the screen and therefore has to show it from frame one.
// The picture comes back at the end wearing both its foreground and its
// background.
//
// Negative control: showing every character in Build, which is what an effect
// that passes over the screen has to do. Run that way the whole picture is
// already in frame 0 and the first assertion fails. Confirmed failing.
func TestSweepAssemblesRatherThanPassesOver(t *testing.T) {
	const width, height = 20, 4
	term := NewTerminalFromCells(sweepPicture(width, height), TerminalConfig{
		Width: width, Height: height, MakeFillCharacters: true,
		ExistingColorHandling: DynamicExistingColors,
	})
	engine := NewEngine(term, NewRng(5))
	frames, err := Run(NewSweep(DefaultSweepConfig()), engine, 4000)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(frames) == 0 {
		t.Fatal("the effect produced no frames")
	}
	if rows := nonBlank(frames[0]); len(rows) != 0 {
		t.Errorf("the first frame already shows %q, but sweep assembles the screen"+
			" and must start from an empty canvas", rows)
	}
	// The last frame is the picture again, foreground and background both.
	last := frames[len(frames)-1]
	if rows := nonBlank(last); len(rows) != 2 ||
		rows[0] != strings.Repeat("A", width) || rows[1] != strings.Repeat("b", width/2) {
		t.Errorf("the final frame reads %q, want the picture back", nonBlank(last))
	}
	if !strings.Contains(last, "\x1b[48;2;0;0;160m") {
		t.Error("the final frame has no blue background, so the bar's background was dropped")
	}
	if !strings.Contains(last, "\x1b[38;2;0;255;120m") {
		t.Error("the final frame has no green foreground, so the text's colour was dropped")
	}
}
