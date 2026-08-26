package tuiffects

import "testing"

// matrixTestConfig is the default matrix with a rain short enough to test.
// The engine's virtual clock runs at sixty frames a second, so one second of
// rain is sixty frames rather than the default fifteen hundred.
func matrixTestConfig() MatrixConfig {
	config := DefaultMatrixConfig()
	config.RainTime = 1
	return config
}

func matrixTerminal(input string, width, height int) *Terminal {
	return NewTerminalFromText(input, TerminalConfig{
		Width: width, Height: height, MakeFillCharacters: true,
	})
}

// TestMatrixResolvesToTheInputText runs matrix to completion.
//
// Negative control: giving the resolve scene a fixed symbol instead of
// ch.InputSymbol leaves the final frame reading as that symbol.
func TestMatrixResolvesToTheInputText(t *testing.T) {
	const input = "matrix rain"
	term := matrixTerminal(input, 20, 5)
	engine := NewEngine(term, NewRng(11))

	frames, err := Run(NewMatrix(matrixTestConfig()), engine, 20000)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(frames) == 0 {
		t.Fatal("the effect produced no frames")
	}
	if len(frames) >= 20000 {
		t.Fatal("the effect never finished within the frame cap")
	}
	rows := nonBlank(frames[len(frames)-1])
	if len(rows) != 1 || rows[0] != input {
		t.Errorf("the final frame reads %q, want %q", rows, input)
	}
	if first := nonBlank(frames[0]); len(first) == 1 && first[0] == input {
		t.Error("the first frame already reads as the finished text")
	}
	if frames[len(frames)/2] == frames[0] {
		t.Error("the middle frame is identical to the first one, so nothing is animating")
	}
}

// TestMatrixRainFallsDownEachColumn checks the thing the effect is named for:
// the rain falls, so within one column of the canvas a character higher up is
// always shown before a character below it.
//
// The columns come out of GetCharactersGrouped bottom to top, so Build has to
// reverse each one before it becomes the order the drop falls in.
//
// Negative control: dropping the reverseCharacters call in Build fills every
// column from the bottom upwards and this fails on every column.
func TestMatrixRainFallsDownEachColumn(t *testing.T) {
	term := matrixTerminal("falling\ncolumns", 12, 4)
	engine := NewEngine(term, NewRng(5))
	effect := NewMatrix(matrixTestConfig())
	if err := effect.Build(engine); err != nil {
		t.Fatalf("Build: %v", err)
	}

	everything := CharacterFilter{Input: true, InnerFill: true, OuterFill: true}
	all := term.CollectCharacters(everything)
	firstShown := map[*Character]int{}
	for frame := 1; frame <= 20000; frame++ {
		if !effect.Advance(engine) {
			break
		}
		for _, ch := range all {
			if ch.IsVisible {
				if _, seen := firstShown[ch]; !seen {
					firstShown[ch] = frame
				}
			}
		}
	}
	if len(firstShown) != len(all) {
		t.Fatalf("%d of %d characters were never shown", len(all)-len(firstShown), len(all))
	}

	columns := term.GetCharactersGrouped(everything, GroupColumnLeftToRight)
	if len(columns) < 2 {
		t.Fatalf("the canvas has %d columns, want several to check", len(columns))
	}
	for _, column := range columns {
		// The group runs bottom to top, so walk it backwards to go downwards.
		for i := len(column) - 2; i >= 0; i-- {
			above, below := column[i+1], column[i]
			if firstShown[above] > firstShown[below] {
				t.Fatalf("in column %d, row %d appeared on frame %d and row %d above it on frame %d, "+
					"want the higher row first",
					below.InputCoord.Column, below.InputCoord.Row, firstShown[below],
					above.InputCoord.Row, firstShown[above])
			}
		}
	}
}

// TestMatrixDropHeadIsHighlighted checks the leading edge of a falling column
// wears the highlight colour and the characters trailing it do not. That white
// head on a green tail is what makes the rain read as rain.
//
// The scan stops well inside the rain phase on purpose. The resolve scene also
// starts on the highlight colour, so a scan that ran to the end of the effect
// would find resolving characters and pass whatever the rain did: that is what
// the negative control caught the first time this test was written.
//
// Negative control: colouring the new character with a rain colour instead of
// config.HighlightColor never produces a highlighted head and this fails.
func TestMatrixDropHeadIsHighlighted(t *testing.T) {
	term := matrixTerminal("head", 14, 6)
	engine := NewEngine(term, NewRng(19))
	config := matrixTestConfig()
	effect := NewMatrix(config)
	if err := effect.Build(engine); err != nil {
		t.Fatalf("Build: %v", err)
	}

	everything := CharacterFilter{Input: true, InnerFill: true, OuterFill: true}
	columns := term.GetCharactersGrouped(everything, GroupColumnLeftToRight)
	// One second of rain is sixty frames, and the fill and resolve phases come
	// after it, so forty frames is inside the rain with room to spare.
	const rainFrames = 40
	heads := 0
	for frame := 1; frame <= rainFrames && heads == 0; frame++ {
		if !effect.Advance(engine) {
			break
		}
		for _, column := range columns {
			var visible []*Character
			for _, ch := range column {
				if ch.IsVisible {
					visible = append(visible, ch)
				}
			}
			if len(visible) < 2 {
				continue
			}
			// The group runs bottom to top, so the first visible entry is the
			// lowest one on the screen: the head of the drop.
			head := visible[0]
			for _, ch := range visible {
				if ch.Motion.CurrentCoord.Row < head.Motion.CurrentCoord.Row {
					head = ch
				}
			}
			if !matrixIsHighlighted(head, config.HighlightColor) {
				continue
			}
			trailingHighlight := false
			for _, ch := range visible {
				if ch != head && matrixIsHighlighted(ch, config.HighlightColor) {
					trailingHighlight = true
				}
			}
			if !trailingHighlight {
				heads++
			}
		}
	}
	if heads == 0 {
		t.Error("no column ever showed a highlighted head above an unhighlighted tail")
	}
}

func matrixIsHighlighted(ch *Character, highlight Color) bool {
	colors := ch.Animation.CurrentVisual().Colors
	return colors.HasFg && colors.Fg == highlight
}

// TestMatrixRainTimeIsMeasuredInSeconds checks the effect reads the engine
// clock rather than counting frames of its own: the rain phase is the only
// thing RainTime changes, so three seconds of rain must take about two seconds
// of frames longer than one second of rain.
//
// The engine's clock is virtual and steps once per Update at sixty frames a
// second, so the two extra seconds are about a hundred and twenty frames.
//
// Negative control: replacing the deadline with one that fires immediately
// (comparing the elapsed time against zero) collapses both runs to the same
// length and this fails.
func TestMatrixRainTimeIsMeasuredInSeconds(t *testing.T) {
	run := func(rainTime int) int {
		term := matrixTerminal("seconds", 16, 4)
		engine := NewEngine(term, NewRng(23))
		config := matrixTestConfig()
		config.RainTime = rainTime
		frames, err := Run(NewMatrix(config), engine, 20000)
		if err != nil {
			t.Fatalf("Run: %v", err)
		}
		if len(frames) >= 20000 {
			t.Fatalf("a rain time of %d never finished within the frame cap", rainTime)
		}
		return len(frames)
	}
	short, long := run(1), run(3)
	if long-short < 100 {
		t.Errorf("one second of rain ran for %d frames and three seconds for %d, "+
			"want about 120 frames more for the two extra seconds", short, long)
	}
}

// TestMatrixAssemblesRatherThanPassesOver pins the DynamicExistingColors
// behaviour. Matrix builds the picture out of the rain, so unlike a sweep it
// must not show every character on the first frame: it starts on an empty
// canvas and the resolve phase puts the screen back.
//
// It also checks the input colours come back, background included, which is
// what the dynamic branch of Build exists for.
//
// Negative control: showing every character at the end of Build under dynamic
// handling, the way the sweep effects have to, makes the first-frame check
// fail. It has to go at the end: the columns are set up after the character
// loop and their setup hides everything again, so the same lines written a few
// lines higher are dead and the control passes. Removing the dynamic branch so
// every character resolves to the final gradient instead makes the colour
// checks fail.
func TestMatrixAssemblesRatherThanPassesOver(t *testing.T) {
	red, blue := RGB(220, 30, 30), RGB(20, 40, 200)
	grid := [][]InputCell{
		{{Symbol: "a", Fg: red, HasFg: true}, {Symbol: "b", Fg: red, HasFg: true}, {Symbol: " ", Bg: blue, HasBg: true}},
		{{Symbol: "c", Fg: red, HasFg: true}, {Symbol: " ", Bg: blue, HasBg: true}, {Symbol: "d", Fg: red, HasFg: true}},
	}
	term := NewTerminalFromCells(grid, TerminalConfig{
		Width: 3, Height: 2,
		ExistingColorHandling: DynamicExistingColors,
		MakeFillCharacters:    true,
	})
	engine := NewEngine(term, NewRng(31))
	effect := NewMatrix(matrixTestConfig())
	if err := effect.Build(engine); err != nil {
		t.Fatalf("Build: %v", err)
	}

	visibleOnFirstFrame := 0
	if !effect.Advance(engine) {
		t.Fatal("the effect finished before its first frame")
	}
	for _, ch := range term.InputCharacters {
		if ch.IsVisible {
			visibleOnFirstFrame++
		}
	}
	if visibleOnFirstFrame == len(term.InputCharacters) {
		t.Errorf("every one of the %d input characters was visible on the first frame; "+
			"matrix assembles the screen and must not show it all at once", len(term.InputCharacters))
	}

	for frame := 1; frame <= 20000; frame++ {
		if !effect.Advance(engine) {
			break
		}
		if frame == 20000 {
			t.Fatal("the effect never finished within the frame cap")
		}
	}
	for _, ch := range term.InputCharacters {
		want := ch.Animation.InputColors
		got := ch.Animation.CurrentVisual().Colors
		if got.HasFg != want.HasFg || (want.HasFg && got.Fg != want.Fg) {
			t.Errorf("%q at %v resolved to foreground %v, want the input's %v",
				ch.InputSymbol, ch.InputCoord, got, want)
		}
		if got.HasBg != want.HasBg || (want.HasBg && got.Bg != want.Bg) {
			t.Errorf("%q at %v resolved to background %v, want the input's %v",
				ch.InputSymbol, ch.InputCoord, got, want)
		}
	}
}
