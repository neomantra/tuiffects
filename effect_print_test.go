package tuiffects

import (
	"strings"
	"testing"
)

// printTerminal builds the terminal print expects. The effect declares
// NeedsFillCharacters, so a test that leaves them out is testing a different
// effect from the one a host runs.
func printTerminal(input string, width, height int, anchor Anchor) *Terminal {
	return NewTerminalFromText(input, TerminalConfig{
		Width: width, Height: height, AnchorText: anchor, MakeFillCharacters: true,
	})
}

// printRows splits a frame into canvas rows, top row first, with the blank
// rows kept so a row index means something.
func printRows(frame string) []string {
	return strings.Split(trimRight(plain(frame)), "\n")
}

// printRowFromBottom reads a canvas row out of a frame. Row 1 is the bottom.
func printRowFromBottom(frame string, row int) string {
	rows := printRows(frame)
	return rows[len(rows)-row]
}

// printInkedCells counts the cells of a row that hold something other than a
// space, which is how many glyphs the print head has put down on it.
func printInkedCells(row string) int {
	count := 0
	for _, r := range row {
		if r != ' ' {
			count++
		}
	}
	return count
}

// printLoneGlyphColumn returns the 1-based column of the only inked cell in a
// row when that cell holds the given glyph, and 0 otherwise. Columns are
// counted in cells rather than bytes, because the glyphs this effect draws are
// three bytes wide.
func printLoneGlyphColumn(row string, glyph rune) int {
	found := 0
	for column, r := range []rune(row) {
		if r == ' ' {
			continue
		}
		if found != 0 || r != glyph {
			return 0
		}
		found = column + 1
	}
	return found
}

// TestPrintResolvesToTheInputText runs print to completion and checks the text
// lands back where the input put it.
//
// The anchor is the centre rather than the default corner, because that is
// what separates "the text is on screen" from "the text is in the right
// place": the effect types every line on the bottom row and scrolls the page
// up under it, so the number of lines it scrolls has to be the height of the
// canvas and not the height of the text.
//
// Negative control: dropping NeedsFillCharacters from the descriptor, or
// building this terminal without MakeFillCharacters. Run that way only the two
// rows holding text produce lines, the page scrolls up by two instead of six,
// and the final frame reads
//
//	hello world
//	second line
//
// on the bottom two rows instead of the middle two. Confirmed failing.
func TestPrintResolvesToTheInputText(t *testing.T) {
	const first, second = "     hello world", "     second line"
	term := printTerminal("hello world\nsecond line", 20, 6, AnchorC)
	engine := NewEngine(term, NewRng(7))

	frames, err := Run(NewPrint(DefaultPrintConfig()), engine, 5000)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(frames) == 0 {
		t.Fatal("the effect produced no frames")
	}
	if len(frames) >= 5000 {
		t.Fatal("the effect never finished within the frame cap")
	}

	last := printRows(frames[len(frames)-1])
	if len(last) != 6 {
		t.Fatalf("the frame has %d rows, want 6", len(last))
	}
	want := []string{"", "", first, second, "", ""}
	for row := range want {
		if last[row] != want[row] {
			t.Errorf("row %d of the final frame reads %q, want %q", row, last[row], want[row])
		}
	}

	// A middle frame must differ, or the effect resolved without animating.
	if middle := nonBlank(frames[len(frames)/2]); len(middle) == 2 &&
		middle[0] == first && middle[1] == second {
		t.Error("the middle frame already reads as the input, so nothing animated")
	}

	// The effect assembles the screen rather than passing over it, so the
	// first frame must hold the print head and nothing else.
	if got := nonBlank(frames[0]); len(got) != 1 || strings.TrimSpace(got[0]) != "█" {
		t.Errorf("the first frame reads %q, want only the print head", got)
	}
}

// TestPrintDeclaresItNeedsFillCharacters pins the declaration the effect
// depends on. The terminal is built before the effect is, so an effect that
// queries the fill populations without saying so gets an empty set.
//
// Negative control: removing the field from the descriptor makes this fail.
func TestPrintDeclaresItNeedsFillCharacters(t *testing.T) {
	descriptor, ok := Lookup("print")
	if !ok {
		t.Fatal("print is not registered")
	}
	if !descriptor.NeedsFillCharacters {
		t.Error("print does not declare that it needs fill characters")
	}
}

// TestPrintTypesEachLineOnTheBottomRowAndLiftsIt is the test for the thing the
// effect is named for. A line is struck out on the bottom row of the canvas,
// and when it is finished the whole page moves up one row so the next line has
// somewhere to go.
//
// The check is the line feed itself: a frame where the bottom row holds a full
// line of eleven struck cells and the row above holds almost nothing, followed
// immediately by a frame where those eleven cells are one row up and the
// bottom row has been handed back to the print head.
//
// Negative control: dropping the moveUp call in finishLine. Run that way every
// line is struck on the bottom row and stays there, the row above never
// reaches eleven cells, and this fails with "no line was ever lifted off the
// bottom row". Confirmed failing.
func TestPrintTypesEachLineOnTheBottomRowAndLiftsIt(t *testing.T) {
	const lineWidth = len("hello world")
	term := printTerminal("hello world\nsecond line", 20, 6, AnchorSW)
	engine := NewEngine(term, NewRng(7))

	frames, err := Run(NewPrint(DefaultPrintConfig()), engine, 5000)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(frames) < 10 {
		t.Fatalf("the effect ran for %d frames, want a real animation", len(frames))
	}

	lifted := false
	for i := 0; i+1 < len(frames); i++ {
		bottom := printInkedCells(printRowFromBottom(frames[i], 1))
		above := printInkedCells(printRowFromBottom(frames[i], 2))
		if bottom != lineWidth || above >= lineWidth {
			continue
		}
		nextBottom := printRowFromBottom(frames[i+1], 1)
		nextAbove := printInkedCells(printRowFromBottom(frames[i+1], 2))
		if nextAbove != lineWidth {
			continue
		}
		if printInkedCells(nextBottom) != 1 || !strings.Contains(nextBottom, "█") {
			t.Errorf("frame %d lifted the line but left %q on the bottom row, want only the print head",
				i+1, nextBottom)
		}
		lifted = true
		break
	}
	if !lifted {
		t.Error("no line was ever lifted off the bottom row, so nothing scrolled")
	}
}

// TestPrintHeadRunsBackToTheStartOfTheNextLine covers the other half of the
// name. Between lines a block travels along the bottom row from the column the
// last line ended in back to the column the next line starts in.
//
// Only the frames where the row above already holds a full line are counted,
// so the block being measured is the print head and not a character still
// ramping down from its own strike.
//
// Negative control: dropping the ActivatePath call in startCarriageReturn.
// Run that way the head is shown where the last line ended and never leaves,
// so it is caught on one frame instead of six and this fails on "the print
// head was seen on 1 frames". Confirmed failing.
func TestPrintHeadRunsBackToTheStartOfTheNextLine(t *testing.T) {
	const lineWidth = len("hello world")
	term := printTerminal("hello world\nsecond line", 20, 6, AnchorSW)
	engine := NewEngine(term, NewRng(7))

	frames, err := Run(NewPrint(DefaultPrintConfig()), engine, 5000)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	var columns []int
	for _, frame := range frames {
		if printInkedCells(printRowFromBottom(frame, 2)) < lineWidth {
			continue
		}
		column := printLoneGlyphColumn(printRowFromBottom(frame, 1), '█')
		if column == 0 {
			continue
		}
		columns = append(columns, column)
	}

	if len(columns) < 4 {
		t.Fatalf("the print head was seen on %d frames, want at least 4", len(columns))
	}
	for i := 1; i < len(columns); i++ {
		if columns[i] >= columns[i-1] {
			t.Fatalf("the print head did not travel: columns %v", columns)
		}
	}
	if columns[0] < 9 {
		t.Errorf("the print head started its run at column %d, want it out near the end of the line it just typed",
			columns[0])
	}
	if columns[len(columns)-1] != 1 {
		t.Errorf("the print head finished its run at column %d, want column 1 where the next line starts",
			columns[len(columns)-1])
	}
}

// TestPrintKeepsTheColoursACapturedScreenArrivedWith covers the branch a
// screen saver runs in. Under DynamicExistingColors every cell must resolve
// back to the foreground and the background it came in with, and a cell that
// carried neither must resolve back to carrying neither.
//
// It also pins the side of the assemble-or-pass-over rule this effect is on.
// An effect that passes over the screen has to show the whole picture from the
// first frame under this policy, because the picture is already up. print
// assembles instead, so the first frame must be empty but for the head.
//
// Negative control, colours: taking the final colours from the gradient
// mapping rather than from the cell's own, which is what the effect does under
// the default policy. Run that way the last frame paints every cell in
// gradient teal and all four colour checks fail. Confirmed failing.
//
// Negative control, first frame: showing every character up front the way
// waves does under this policy. Run that way the first frame already reads as
// the whole picture and the first-frame check fails. Confirmed failing.
func TestPrintKeepsTheColoursACapturedScreenArrivedWith(t *testing.T) {
	red, green, blue := RGB(255, 0, 0), RGB(0, 255, 0), RGB(0, 0, 255)
	grid := [][]InputCell{
		{{Symbol: "a", Fg: red, HasFg: true}, {Symbol: " ", Bg: blue, HasBg: true}},
		{{Symbol: "b", Fg: green, HasFg: true}, {Symbol: "c"}},
	}
	term := NewTerminalFromCells(grid, TerminalConfig{
		Width: 4, Height: 4,
		MakeFillCharacters:    true,
		ExistingColorHandling: DynamicExistingColors,
	})
	engine := NewEngine(term, NewRng(3))

	frames, err := Run(NewPrint(DefaultPrintConfig()), engine, 5000)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(frames) == 0 {
		t.Fatal("the effect produced no frames")
	}
	last := frames[len(frames)-1]

	if got := nonBlank(frames[0]); len(got) != 1 || strings.TrimSpace(got[0]) != "█" {
		t.Errorf("the first frame reads %q, want only the print head", got)
	}

	if !strings.Contains(last, "\x1b[38;2;255;0;0ma\x1b[0m") {
		t.Errorf("the final frame does not carry the red a back: %q", last)
	}
	if !strings.Contains(last, "\x1b[38;2;0;255;0mb\x1b[0m") {
		t.Errorf("the final frame does not carry the green b back: %q", last)
	}
	// A blank cell that arrived with a background is window chrome on a real
	// screen, so the background has to survive the effect.
	if !strings.Contains(last, "\x1b[48;2;0;0;255m \x1b[0m") {
		t.Errorf("the final frame does not carry the blue background back: %q", last)
	}
	// A cell that arrived with no colour of its own must end up with none,
	// which is the branch that adds the last frame of the ramp by hand.
	if !strings.Contains(plain(last), "c") || strings.Contains(last, "mc\x1b[0m") {
		t.Errorf("the colourless c did not come back colourless: %q", last)
	}
}
