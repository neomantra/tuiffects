package tuiffects

import (
	"strings"
	"testing"
)

// synthGridTerminal builds the terminal synthgrid expects. The effect declares
// NeedsFillCharacters, so a test that leaves them out is testing a different
// effect from the one a host runs.
func synthGridTerminal(input string, width, height int) *Terminal {
	return NewTerminalFromText(input, TerminalConfig{
		Width: width, Height: height, MakeFillCharacters: true,
	})
}

// TestSynthGridResolvesToTheInputText runs synthgrid to completion.
//
// Negative control: giving the last frame of the dissolve scene a fixed symbol
// instead of ch.InputSymbol. Run that way the final frame is nine rows of
// thirty X, one per cell of the canvas, instead of the input text, and this
// fails.
func TestSynthGridResolvesToTheInputText(t *testing.T) {
	const first, second = "hello world", "second line"
	term := synthGridTerminal(first+"\n"+second, 30, 9)
	engine := NewEngine(term, NewRng(7))

	frames, err := Run(NewSynthGrid(DefaultSynthGridConfig()), engine, 40000)
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

// TestSynthGridDrawsTheGridBeforeItFillsAnything is the test for the thing the
// effect is named for. A grid is drawn across the whole canvas, every block of
// it is filled in, and only then is the grid taken back down.
//
// Negative control: making the expand phase hand over to the fill phase
// unconditionally, so the grid never draws. Run that way the first assertion
// fails with no grid symbol in any frame, and the ordering assertion fails
// because a generation symbol appears in frame 0.
//
// A second control covers the other end: dropping the collapse phase leaves
// the grid on screen and the last assertion fails.
func TestSynthGridDrawsTheGridBeforeItFillsAnything(t *testing.T) {
	config := DefaultSynthGridConfig()
	term := synthGridTerminal("hello world\nsecond line", 30, 9)
	engine := NewEngine(term, NewRng(7))
	frames, err := Run(NewSynthGrid(config), engine, 40000)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(frames) < 10 {
		t.Fatalf("the effect ran for %d frames, want a real animation", len(frames))
	}

	gridSymbols := []string{config.GridRowSymbol, config.GridColumnSymbol}
	hasAny := func(frame string, symbols []string) bool {
		for _, symbol := range symbols {
			if strings.Contains(frame, symbol) {
				return true
			}
		}
		return false
	}

	// The grid reaches its full size: every canvas edge is drawn, so the top
	// row is a solid run of the row symbol across the whole width.
	fullTop := strings.Repeat(config.GridRowSymbol, 30)
	gridComplete := -1
	for i, frame := range frames {
		if strings.HasPrefix(plain(frame), fullTop) {
			gridComplete = i
			break
		}
	}
	if gridComplete < 0 {
		t.Fatal("the grid never drew a complete top edge")
	}

	// Nothing fills in before the grid is finished.
	for i := 0; i <= gridComplete; i++ {
		if hasAny(plain(frames[i]), config.TextGenerationSymbols) {
			t.Fatalf("frame %d filled a block while the grid was still drawing", i)
		}
	}

	// A block does fill in afterwards, so the grid is not the whole effect.
	filled := false
	for _, frame := range frames[gridComplete:] {
		if hasAny(plain(frame), config.TextGenerationSymbols) {
			filled = true
			break
		}
	}
	if !filled {
		t.Error("no block ever filled in after the grid was drawn")
	}

	// The grid comes back down, so the screen is left as the text alone.
	if hasAny(plain(frames[len(frames)-1]), gridSymbols) {
		t.Error("the grid is still on screen in the final frame")
	}
}

// TestSynthGridAssemblesRatherThanPassesOver checks the one thing that looks
// right in a unit test and wrong on screen. synthgrid builds the picture up
// block by block, so under DynamicExistingColors every character must stay
// hidden until its block's turn comes. A sweep such as waves has to do the
// opposite and show the whole picture from the first frame.
//
// Negative control: showing every character during Build under dynamic, which
// is what waves has to do. Run that way frame 0 already carries the block
// symbols of every cell's dissolve and the first assertion fails. Asserting
// only that the input text is unreadable in frame 0 is not enough, because the
// dissolve scene is activated in Build and its first frame is a block symbol,
// so the screen is full of noise while the text itself is still hidden.
func TestSynthGridAssemblesRatherThanPassesOver(t *testing.T) {
	red, blue := RGB(255, 0, 0), RGB(0, 0, 255)
	// A row of blanks above and below keeps the text off the canvas edges,
	// where the grid's border lines would cover it either way.
	blank := []InputCell{{}, {}, {}, {}}
	grid := [][]InputCell{
		blank,
		{{}, {Symbol: "A", Fg: red, HasFg: true}, {Symbol: "B", Fg: red, HasFg: true, Bg: blue, HasBg: true}, {}},
		blank,
	}
	term := NewTerminalFromCells(grid, TerminalConfig{
		Width: 4, Height: 3,
		ExistingColorHandling: DynamicExistingColors,
		MakeFillCharacters:    true,
	})
	engine := NewEngine(term, NewRng(3))
	effect := NewSynthGrid(DefaultSynthGridConfig())

	frames, err := Run(effect, engine, 40000)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(frames) == 0 {
		t.Fatal("the effect produced no frames")
	}
	// Nothing but the grid is on screen while the grid is still drawing.
	config := DefaultSynthGridConfig()
	allowed := map[rune]bool{' ': true, '\n': true}
	for _, symbol := range []string{config.GridRowSymbol, config.GridColumnSymbol} {
		for _, r := range symbol {
			allowed[r] = true
		}
	}
	for _, r := range plain(frames[0]) {
		if !allowed[r] {
			t.Fatalf("frame 0 already shows %q, so the effect passes over the screen "+
				"instead of assembling it: %q", string(r), plain(frames[0]))
		}
	}

	last := frames[len(frames)-1]
	if got := strings.TrimSpace(plain(last)); got != "AB" {
		t.Fatalf("the final frame reads %q, want %q", got, "AB")
	}
	if !strings.Contains(last, "\x1b[38;2;255;0;0m") {
		t.Errorf("the final frame does not carry the input's red: %q", last)
	}
	// The background a captured cell carried has to come back too, or every
	// selection bar and filled panel resolves as bare text.
	if !strings.Contains(last, "\x1b[48;2;0;0;255m") {
		t.Errorf("the final frame does not carry the input's background: %q", last)
	}
}

// TestSynthGridEvenGapPicksTheDivisorNearestAFifth pins the spacing search,
// which is what decides how many blocks the canvas is cut into.
//
// Two is taken off the dimension first, so a canvas 42 rows tall is divided as
// 40. The candidates over 40 are 40, 39, 20, 13, 10, 8 and 5, a fifth of 40 is
// 8, and 8 is in the list, so 8 wins outright. A dimension of 9 is divided as
// 7, whose candidates are 7 and 6 against a target of 1, so the nearer 6 wins
// and the search cannot stop at the first candidate it finds.
//
// Negative control: dropping the "- 2" at the top. Every case here changes,
// including 42 to 7 and 9 to 8, and the two small dimensions stop returning
// zero.
func TestSynthGridEvenGapPicksTheDivisorNearestAFifth(t *testing.T) {
	cases := []struct{ dimension, want int }{
		{42, 8},
		{9, 6},
		{2, 0},
		{1, 0},
		// Nothing above 4 divides 5 with a remainder of at most one except 5
		// itself, so the fallback is not reached here; a dimension of 8 leaves
		// 6, whose candidates are 6 and 5.
		{8, 5},
	}
	for _, c := range cases {
		if got := synthGridEvenGap(c.dimension); got != c.want {
			t.Errorf("synthGridEvenGap(%d) = %d, want %d", c.dimension, got, c.want)
		}
	}
}
