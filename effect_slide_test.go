package tuiffects

import (
	"strings"
	"testing"
)

// TestSlideSettlesIntoTheInputText runs slide to completion and checks it both
// animates and lands on the input.
//
// Negative control: pointing the ramp scene at a fixed symbol instead of
// ch.InputSymbol leaves the final frame reading as that symbol.
func TestSlideSettlesIntoTheInputText(t *testing.T) {
	const input = "sliding in"
	term := NewTerminalFromText(input, TerminalConfig{Width: 20, Height: 4})
	engine := NewEngine(term, NewRng(5))

	frames, err := Run(NewSlide(DefaultSlideConfig()), engine, 40000)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(frames) == 0 {
		t.Fatal("the effect produced no frames")
	}
	if len(frames) >= 40000 {
		t.Fatal("the effect never finished within the frame cap")
	}
	last := frames[len(frames)-1]
	rows := nonBlank(last)
	if len(rows) != 1 || rows[0] != input {
		t.Errorf("the final frame reads %q, want %q", rows, input)
	}
	// It has to be an animation and not a single jump to the answer.
	moved := false
	for _, frame := range frames[:len(frames)/2] {
		if plain(frame) != plain(last) {
			moved = true
			break
		}
	}
	if !moved {
		t.Error("every frame in the first half already reads as the final frame")
	}
}

// TestSlideParksEveryCharacterOffCanvas is the test for the thing the effect is
// named for. A slide starts outside the canvas and travels in, so after Build
// no character may be standing on its own coordinate, and each one's path must
// lead back to it.
//
// Negative control: dropping the Motion.SetCoordinate calls in Build leaves
// every character on its input coordinate and every one of these checks fails.
func TestSlideParksEveryCharacterOffCanvas(t *testing.T) {
	term := NewTerminalFromText("abc\ndef", TerminalConfig{Width: 8, Height: 4})
	engine := NewEngine(term, NewRng(5))
	if err := NewSlide(DefaultSlideConfig()).Build(engine); err != nil {
		t.Fatalf("Build: %v", err)
	}
	canvas := term.Canvas
	for _, ch := range term.InputCharacters {
		got := ch.Motion.CurrentCoord
		// The default slides rows in from the left, so each character waits
		// one column outside the canvas on the row it belongs to.
		if want := C(canvas.Left-1, ch.InputCoord.Row); got != want {
			t.Errorf("%q waits at %v, want %v", ch.InputSymbol, got, want)
		}
		path := ch.Motion.Path(slidePathID)
		if path == nil {
			t.Fatalf("%q has no %s", ch.InputSymbol, slidePathID)
		}
		if len(path.Waypoints) != 1 || path.Waypoints[0].Coord != ch.InputCoord {
			t.Errorf("%q travels to %v, want its input coordinate %v",
				ch.InputSymbol, path.Waypoints, ch.InputCoord)
		}
	}
}

// TestSlideReverseDirectionEntersFromTheOtherSide checks the flag turns the
// slide around rather than being ignored.
//
// Negative control: dropping the ReverseDirection branch in the row case parks
// the characters at the left edge and this fails.
func TestSlideReverseDirectionEntersFromTheOtherSide(t *testing.T) {
	term := NewTerminalFromText("abc\ndef", TerminalConfig{Width: 8, Height: 4})
	engine := NewEngine(term, NewRng(5))
	config := DefaultSlideConfig()
	config.ReverseDirection = true
	if err := NewSlide(config).Build(engine); err != nil {
		t.Fatalf("Build: %v", err)
	}
	canvas := term.Canvas
	for _, ch := range term.InputCharacters {
		if want := C(canvas.Right+1, ch.InputCoord.Row); ch.Motion.CurrentCoord != want {
			t.Errorf("%q waits at %v, want %v", ch.InputSymbol, ch.Motion.CurrentCoord, want)
		}
	}
}

// TestSlideMergeAlternatesSides checks merge sends every other row in from the
// opposite edge, counting rows from the top as the grouping does.
//
// Negative control: dropping the merge branch parks every row at the left edge,
// so the even rows fail.
func TestSlideMergeAlternatesSides(t *testing.T) {
	term := NewTerminalFromText("abc\ndef\nghi\njkl", TerminalConfig{Width: 8, Height: 6})
	engine := NewEngine(term, NewRng(5))
	config := DefaultSlideConfig()
	config.Merge = true
	if err := NewSlide(config).Build(engine); err != nil {
		t.Fatalf("Build: %v", err)
	}
	canvas := term.Canvas
	// Regrouping gives the groups in their original order, since Build only
	// reorders the copies it kept.
	groups := term.GetCharactersGrouped(InputOnly(), GroupRowTopToBottom)
	if len(groups) != 4 {
		t.Fatalf("the input made %d row groups, want 4", len(groups))
	}
	for index, group := range groups {
		want := canvas.Left - 1
		side := "left"
		if index%2 == 0 {
			want = canvas.Right + 1
			side = "right"
		}
		for _, ch := range group {
			if ch.Motion.CurrentCoord.Column != want {
				t.Errorf("row group %d holds %q at column %d, want the %s edge at %d",
					index, ch.InputSymbol, ch.Motion.CurrentCoord.Column, side, want)
			}
		}
	}
	// And the two edges must both be in use, or "alternates" means nothing.
	seen := map[int]bool{}
	for _, ch := range term.InputCharacters {
		seen[ch.Motion.CurrentCoord.Column] = true
	}
	if !seen[canvas.Left-1] || !seen[canvas.Right+1] {
		t.Errorf("merge used the columns %v, want both %d and %d", seen, canvas.Left-1, canvas.Right+1)
	}
}

// TestSlideParksColumnsAndDiagonalsOnTheirOwnAxis checks the other two
// groupings park their characters where the grouping says, since each one
// leaves the canvas by a different edge, and that both still land.
//
// A column waits above its own column. A diagonal group waits stacked on a
// single coordinate that is still on the group's own diagonal, one row below
// the canvas, so sliding along that diagonal brings every character home.
//
// Negative control: shifting the diagonal starting coordinate by one column
// takes it off the group's diagonal and the diagonal subtest fails; parking a
// column on its input row instead of above the canvas fails the column one.
func TestSlideParksColumnsAndDiagonalsOnTheirOwnAxis(t *testing.T) {
	const input = "one\ntwo\nsix"

	t.Run("column", func(t *testing.T) {
		term := NewTerminalFromText(input, TerminalConfig{Width: 6, Height: 5})
		engine := NewEngine(term, NewRng(5))
		config := DefaultSlideConfig()
		config.Grouping = SlideByColumn
		effect := NewSlide(config)
		if err := effect.Build(engine); err != nil {
			t.Fatalf("Build: %v", err)
		}
		canvas := term.Canvas
		for _, ch := range term.InputCharacters {
			if want := C(ch.InputCoord.Column, canvas.Top+1); ch.Motion.CurrentCoord != want {
				t.Errorf("%q waits at %v, want %v", ch.InputSymbol, ch.Motion.CurrentCoord, want)
			}
		}
		slideRunsHome(t, effect, engine, input)
	})

	t.Run("diagonal", func(t *testing.T) {
		term := NewTerminalFromText(input, TerminalConfig{Width: 6, Height: 5})
		engine := NewEngine(term, NewRng(5))
		config := DefaultSlideConfig()
		config.Grouping = SlideByDiagonal
		effect := NewSlide(config)
		if err := effect.Build(engine); err != nil {
			t.Fatalf("Build: %v", err)
		}
		canvas := term.Canvas
		groups := term.GetCharactersGrouped(InputOnly(), GroupDiagonalTopLeftToBottomRight)
		if len(groups) < 2 {
			t.Fatalf("the input made %d diagonal groups, want several", len(groups))
		}
		for index, group := range groups {
			start := group[0].Motion.CurrentCoord
			diagonal := group[0].InputCoord.Column - group[0].InputCoord.Row
			if start.Column-start.Row != diagonal {
				t.Errorf("group %d waits at %v, which is not on its own diagonal %d",
					index, start, diagonal)
			}
			if start.Row >= canvas.Bottom {
				t.Errorf("group %d waits at %v, which is still inside the canvas", index, start)
			}
			for _, ch := range group {
				if ch.Motion.CurrentCoord != start {
					t.Errorf("group %d is split between %v and %v", index, start, ch.Motion.CurrentCoord)
				}
			}
		}
		slideRunsHome(t, effect, engine, input)
	})
}

// slideRunsHome drives an already built effect to the end and checks the
// picture arrived.
func slideRunsHome(t *testing.T, effect Effect, engine *Engine, input string) {
	t.Helper()
	frames := 0
	for effect.Advance(engine) {
		frames++
		if frames >= 40000 {
			t.Fatal("the effect never finished within the frame cap")
		}
	}
	if frames == 0 {
		t.Fatal("the effect produced no frames")
	}
	if got := strings.Join(nonBlank(engine.Frame()), "\n"); got != input {
		t.Errorf("the final frame reads %q, want %q", got, input)
	}
}

// TestSlideAssemblesRatherThanPassesOver is the rule that decides how an effect
// behaves under DynamicExistingColors. Slide brings the picture in from off
// screen, so nothing may be on the canvas before the first group is released.
// An effect that sweeps over the screen, waves for one, has to do the opposite
// and show everything from the first frame.
//
// Negative control: applying the pass-over treatment waves needs, which is to
// leave every character on its input coordinate and show it in Build, puts the
// whole picture on the first frame and this fails. Showing them without also
// unparking them changes nothing, because a character parked off canvas is
// clipped by the frame painter whether it is visible or not.
func TestSlideAssemblesRatherThanPassesOver(t *testing.T) {
	red := RGB(255, 0, 0)
	grid := [][]InputCell{
		{{Symbol: "a", Fg: red, HasFg: true}, {Symbol: "b", Fg: red, HasFg: true}},
	}
	term := NewTerminalFromCells(grid, TerminalConfig{
		Width: 2, Height: 1, ExistingColorHandling: DynamicExistingColors,
	})
	engine := NewEngine(term, NewRng(5))
	frames, err := Run(NewSlide(DefaultSlideConfig()), engine, 40000)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(frames) == 0 || len(frames) >= 40000 {
		t.Fatalf("the effect produced %d frames", len(frames))
	}
	if got := strings.TrimRight(plain(frames[0]), " "); got != "" {
		t.Errorf("the first frame already shows %q, so the effect is not assembling the screen", got)
	}
	// The picture still has to come back, colours included.
	last := frames[len(frames)-1]
	if got := plain(last); got != "ab" {
		t.Fatalf("the final frame reads %q, want %q", got, "ab")
	}
	if !strings.Contains(last, "\x1b[38;2;255;0;0m") {
		t.Errorf("the final frame does not carry the input's red: %q", last)
	}
}

// TestSlideResolvesTheInputColours checks the colour policy a screen saver runs
// in, where the input is a picture that has to come back as itself.
//
// Negative control: ignoring DynamicExistingColors makes the final frame carry
// the effect's own gradient instead of the input's green.
func TestSlideResolvesTheInputColours(t *testing.T) {
	green := RGB(0, 255, 0)
	blue := RGB(0, 0, 255)
	grid := [][]InputCell{{
		{Symbol: "x", Fg: green, HasFg: true, Bg: blue, HasBg: true},
		{Symbol: "y", Fg: green, HasFg: true, Bg: blue, HasBg: true},
	}}
	term := NewTerminalFromCells(grid, TerminalConfig{
		Width: 2, Height: 1, ExistingColorHandling: DynamicExistingColors,
	})
	engine := NewEngine(term, NewRng(5))
	frames, err := Run(NewSlide(DefaultSlideConfig()), engine, 40000)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	last := frames[len(frames)-1]
	if !strings.Contains(last, "\x1b[38;2;0;255;0m") {
		t.Errorf("the final frame lost the input's foreground: %q", last)
	}
	// A captured cell's background is half of what it looked like, and an
	// effect that sets only a foreground blanks every filled panel on screen.
	if !strings.Contains(last, "\x1b[48;2;0;0;255m") {
		t.Errorf("the final frame lost the input's background: %q", last)
	}
}
