package tuiffects

import (
	"strings"
	"testing"
)

// TestBouncyBallsSettlesIntoTheInputText runs bouncyballs to completion and
// checks the screen it leaves behind is the one it was given.
//
// Negative control: pointing the settling scene at a fixed symbol instead of
// ch.InputSymbol leaves the final frame reading as that symbol.
func TestBouncyBallsSettlesIntoTheInputText(t *testing.T) {
	const input = "bounce here"
	term := NewTerminalFromText(input, TerminalConfig{Width: 20, Height: 5})
	engine := NewEngine(term, NewRng(7))

	frames, err := Run(NewBouncyBalls(DefaultBouncyBallsConfig()), engine, 40000)
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
	// It has to be an animation and not a single frame that is already right.
	// The text is in place well before the end, because the landing gradient
	// keeps running for sixty more frames after the last ball settles, so the
	// evidence of movement is in the early frames rather than the middle one.
	if frames[0] == frames[len(frames)-1] {
		t.Error("the first frame already equals the last, so nothing animated")
	}
	early := plain(strings.Join(frames[:len(frames)/4], ""))
	found := false
	for _, symbol := range DefaultBouncyBallsConfig().BallSymbols {
		if strings.Contains(early, symbol) {
			found = true
			break
		}
	}
	if !found {
		t.Error("no ball symbol appeared in the first quarter of the run")
	}
}

// TestBouncyBallsDropsEveryBallFromAboveTheCanvas checks the start of the fall,
// which is the half of the effect a resolve test cannot see.
//
// Every ball starts in its character's own column, at or above the top row of
// the canvas, and hidden. A ball that started on its input row would settle
// correctly and never fall.
//
// Negative control: passing ch.InputCoord to SetCoordinate instead of the drop
// coordinate puts every ball on its input row and this fails on the first
// character.
func TestBouncyBallsDropsEveryBallFromAboveTheCanvas(t *testing.T) {
	term := NewTerminalFromText("aaaa\nbbbb\ncccc", TerminalConfig{Width: 12, Height: 6})
	engine := NewEngine(term, NewRng(3))
	effect := NewBouncyBalls(DefaultBouncyBallsConfig())
	if err := effect.Build(engine); err != nil {
		t.Fatalf("Build: %v", err)
	}
	if len(term.InputCharacters) == 0 {
		t.Fatal("no input characters")
	}
	for _, ch := range term.InputCharacters {
		start := ch.Motion.CurrentCoord
		if start.Column != ch.InputCoord.Column {
			t.Errorf("%q starts in column %d, want its own column %d",
				ch.InputSymbol, start.Column, ch.InputCoord.Column)
		}
		if start.Row < term.Canvas.Top {
			t.Errorf("%q starts on row %d, want the canvas top (%d) or above",
				ch.InputSymbol, start.Row, term.Canvas.Top)
		}
		if ch.IsVisible {
			t.Errorf("%q is visible before its ball was dropped", ch.InputSymbol)
		}
	}
}

// TestBouncyBallsBounces is the test the effect is named for.
//
// The movement easing is out_bounce, which is not monotonic: a ball reaches
// its input row about a third of the way through the fall, rebounds back up,
// and comes down again. A ball that only fell would reach its row once and
// stay there, which resolves correctly and does not bounce.
//
// Negative control: changing MovementEasing in DefaultBouncyBallsConfig to
// Linear makes every fall monotonic and no character ever rebounds, so this
// fails with "no character rebounded".
func TestBouncyBallsBounces(t *testing.T) {
	term := NewTerminalFromText("bounce", TerminalConfig{Width: 12, Height: 8})
	engine := NewEngine(term, NewRng(5))
	effect := NewBouncyBalls(DefaultBouncyBallsConfig())
	if err := effect.Build(engine); err != nil {
		t.Fatalf("Build: %v", err)
	}
	history := map[*Character][]int{}
	for i := 0; i < 40000; i++ {
		if !effect.Advance(engine) {
			break
		}
		for _, ch := range term.InputCharacters {
			history[ch] = append(history[ch], ch.Motion.CurrentCoord.Row)
		}
	}
	rebounded := 0
	for ch, rows := range history {
		landed := -1
		for i, row := range rows {
			if row == ch.InputCoord.Row {
				landed = i
				break
			}
		}
		if landed < 0 {
			t.Errorf("%q never reached its input row %d", ch.InputSymbol, ch.InputCoord.Row)
			continue
		}
		// Row 1 is the bottom of the canvas, so a rebound is a rise.
		for _, row := range rows[landed:] {
			if row > ch.InputCoord.Row {
				rebounded++
				break
			}
		}
		if last := rows[len(rows)-1]; last != ch.InputCoord.Row {
			t.Errorf("%q came to rest on row %d, want its input row %d",
				ch.InputSymbol, last, ch.InputCoord.Row)
		}
	}
	if rebounded == 0 {
		t.Error("no character rebounded after reaching its input row, so nothing bounced")
	}
}

// TestBouncyBallsAssemblesTheScreenRatherThanPassingOver pins which kind of
// effect this is under the colour policy a screen saver runs in.
//
// bouncyballs assembles: it carries every character in from off the canvas, so
// a character must stay hidden until its own ball is dropped. An effect that
// passed over the screen would have to show the whole picture from the first
// frame and animate on top of it. Doing that here would put a ball in the air
// above every column at once, before any of them had been released, and the
// screen the animation is meant to deliver would already be sitting under
// them.
//
// So the check is on how many characters are showing, not on what the frame
// reads: at the moment Build finishes none of them are, and after the first
// frame only the handful that were dropped are. Checking the first frame did
// not read as the finished picture was not enough, because the balls are still
// in the air at that point and the picture is not on the input rows either
// way.
//
// Negative control: calling SetCharacterVisibility(ch, true) for every
// character at the end of Build makes all sixteen show at once and this fails
// on both counts. Confirmed failing.
func TestBouncyBallsAssemblesTheScreenRatherThanPassingOver(t *testing.T) {
	red := RGB(255, 0, 0)
	blue := RGB(0, 0, 255)
	const side = 4
	var grid [][]InputCell
	for row := 0; row < side; row++ {
		var line []InputCell
		for col := 0; col < side; col++ {
			line = append(line, InputCell{
				Symbol: string(rune('a' + row*side + col)),
				Fg:     red, HasFg: true, Bg: blue, HasBg: true,
			})
		}
		grid = append(grid, line)
	}
	term := NewTerminalFromCells(grid, TerminalConfig{
		Width: side, Height: side, ExistingColorHandling: DynamicExistingColors,
	})
	engine := NewEngine(term, NewRng(11))
	effect := NewBouncyBalls(DefaultBouncyBallsConfig())
	if err := effect.Build(engine); err != nil {
		t.Fatalf("Build: %v", err)
	}
	total := len(term.InputCharacters)
	if total != side*side {
		t.Fatalf("the terminal holds %d characters, want %d", total, side*side)
	}
	if got := visibleCount(term); got != 0 {
		t.Errorf("%d of %d characters are showing before the first frame, want none", got, total)
	}
	if !effect.Advance(engine) {
		t.Fatal("the effect finished on its first frame")
	}
	// Only one row is loaded at a time and at most six balls are dropped per
	// release, so one row of four is the most that can be showing here.
	if got := visibleCount(term); got > side {
		t.Errorf("%d of %d characters are showing after one frame, want at most one row (%d)",
			got, total, side)
	}

	frames := []string{engine.Frame()}
	for i := 0; i < 40000; i++ {
		if !effect.Advance(engine) {
			break
		}
		frames = append(frames, engine.Frame())
	}
	last := frames[len(frames)-1]
	want := "abcd\nefgh\nijkl\nmnop"
	if got := strings.TrimSpace(plain(last)); got != want {
		t.Fatalf("the final frame reads %q, want %q", got, want)
	}
	if !strings.Contains(last, "\x1b[38;2;255;0;0m") {
		t.Errorf("the final frame does not carry the input's red: %q", last)
	}
	if !strings.Contains(last, "\x1b[48;2;0;0;255m") {
		t.Errorf("the final frame does not carry the input's blue background: %q", last)
	}
}

// visibleCount is how many input characters the terminal is currently showing.
func visibleCount(term *Terminal) int {
	n := 0
	for _, ch := range term.InputCharacters {
		if ch.IsVisible {
			n++
		}
	}
	return n
}
