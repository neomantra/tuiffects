package tuiffects

import (
	"strings"
	"testing"
)

// TestPourSettlesIntoTheInputText runs pour to completion.
//
// Negative control: pointing the pour scene at a fixed symbol instead of
// ch.InputSymbol leaves the final frame reading as that symbol.
func TestPourSettlesIntoTheInputText(t *testing.T) {
	const input = "pour me out"
	term := NewTerminalFromText(input, TerminalConfig{Width: 20, Height: 5})
	engine := NewEngine(term, NewRng(3))

	frames, err := Run(NewPour(DefaultPourConfig()), engine, 40000)
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
	// It has to animate on the way, not resolve on frame one.
	if first := nonBlank(frames[0]); len(first) == 1 && first[0] == input {
		t.Error("the first frame already reads as the finished text")
	}
	if frames[len(frames)/2] == frames[0] {
		t.Error("the middle frame is identical to the first one, so nothing is animating")
	}
}

// TestPourFillsTheNearRowFirst checks the thing the effect is named for: the
// canvas fills like a container, so the row the text pours towards is released
// before the row it pours away from.
//
// Pouring down, the bottom row of text has to be released in full before any
// character of the row above it appears.
//
// Negative control: grouping PourDown as GroupRowTopToBottom instead of
// GroupRowBottomToTop releases the top row first and this fails.
func TestPourFillsTheNearRowFirst(t *testing.T) {
	term := NewTerminalFromText("abcd\nefgh", TerminalConfig{Width: 8, Height: 4})
	engine := NewEngine(term, NewRng(3))
	effect := NewPour(DefaultPourConfig())
	if err := effect.Build(engine); err != nil {
		t.Fatalf("Build: %v", err)
	}

	shown := map[*Character]int{}
	for frame := 1; frame <= 40000; frame++ {
		if !effect.Advance(engine) {
			break
		}
		for _, ch := range term.InputCharacters {
			if ch.IsVisible {
				if _, seen := shown[ch]; !seen {
					shown[ch] = frame
				}
			}
		}
	}
	if len(shown) != len(term.InputCharacters) {
		t.Fatalf("%d of %d characters were never shown", len(term.InputCharacters)-len(shown), len(term.InputCharacters))
	}

	bottom, top := term.Canvas.TextBottom, term.Canvas.TextTop
	if bottom == top {
		t.Fatal("the input landed on one row, so there is no fill order to check")
	}
	lastBottom, firstTop := 0, 1<<30
	for ch, frame := range shown {
		switch ch.InputCoord.Row {
		case bottom:
			lastBottom = max(lastBottom, frame)
		case top:
			firstTop = min(firstTop, frame)
		}
	}
	if lastBottom >= firstTop {
		t.Errorf("the bottom row finished on frame %d and the top row started on frame %d, "+
			"want the near row filled first", lastBottom, firstTop)
	}
}

// TestPourEntersFromTheFarEdge checks a character starts off the edge it pours
// from and travels along one axis only: pouring down it enters at the top of
// the canvas in its own column, pouring right it enters at the left in its own
// row.
//
// Negative control: leaving Motion.SetCoordinate out of Build starts every
// character on its input coordinate and both halves fail.
func TestPourEntersFromTheFarEdge(t *testing.T) {
	for _, tc := range []struct {
		name      string
		direction PourDirection
	}{
		{"down", PourDown},
		{"right", PourRight},
	} {
		t.Run(tc.name, func(t *testing.T) {
			term := NewTerminalFromText("abc\ndef", TerminalConfig{Width: 9, Height: 5})
			engine := NewEngine(term, NewRng(7))
			config := DefaultPourConfig()
			config.Direction = tc.direction
			effect := NewPour(config)
			if err := effect.Build(engine); err != nil {
				t.Fatalf("Build: %v", err)
			}
			for _, ch := range term.InputCharacters {
				start := ch.Motion.CurrentCoord
				switch tc.direction {
				case PourDown:
					if start.Row != term.Canvas.Top {
						t.Errorf("%q starts on row %d, want the top of the canvas (%d)",
							ch.InputSymbol, start.Row, term.Canvas.Top)
					}
					if start.Column != ch.InputCoord.Column {
						t.Errorf("%q starts in column %d, want its own column (%d)",
							ch.InputSymbol, start.Column, ch.InputCoord.Column)
					}
				case PourRight:
					if start.Column != term.Canvas.Left {
						t.Errorf("%q starts in column %d, want the left of the canvas (%d)",
							ch.InputSymbol, start.Column, term.Canvas.Left)
					}
					if start.Row != ch.InputCoord.Row {
						t.Errorf("%q starts on row %d, want its own row (%d)",
							ch.InputSymbol, start.Row, ch.InputCoord.Row)
					}
				}
			}
		})
	}
}

// TestPourAssemblesRatherThanPassesOver checks the effect keeps the screen
// hidden until it pours it in, including when the engine resolves back to the
// input's own colours.
//
// An effect that sweeps over a screen that is already there has to show every
// character from the first frame. Pour is the other kind: it puts the picture
// on the screen, so showing it up front would leave the pour nothing to do.
//
// Negative control: showing every character in Build, the way waves does under
// the dynamic policy, makes the first frame already read as the whole input.
func TestPourAssemblesRatherThanPassesOver(t *testing.T) {
	const input = "poured in"
	for _, handling := range []ExistingColorHandling{IgnoreExistingColors, DynamicExistingColors} {
		term := NewTerminalFromText(input, TerminalConfig{
			Width: 16, Height: 4, ExistingColorHandling: handling,
		})
		engine := NewEngine(term, NewRng(5))
		effect := NewPour(DefaultPourConfig())
		if err := effect.Build(engine); err != nil {
			t.Fatalf("Build: %v", err)
		}
		for _, ch := range term.InputCharacters {
			if ch.IsVisible {
				t.Fatalf("handling %v: %q is visible before the first frame", handling, ch.InputSymbol)
			}
		}
		if !effect.Advance(engine) {
			t.Fatalf("handling %v: the effect ended on its first frame", handling)
		}
		first := strings.Join(nonBlank(engine.Frame()), "")
		if strings.Contains(first, input) {
			t.Errorf("handling %v: the first frame already reads %q", handling, input)
		}
	}
}

// TestPourRampsToTheFinalGradient checks the closing colour ramp runs its
// whole length: a character starts white and ends on the colour the final
// gradient gives its cell.
//
// Negative control: ending the pour gradient on the starting colour instead of
// the mapped one leaves every character white and the second half fails. The
// first half needs a second control, since a ramp that jumped straight to the
// end colour would still land right: starting the gradient from the mapped
// colour rather than StartingColor makes the first frame's characters arrive
// already coloured and the first half fails.
func TestPourRampsToTheFinalGradient(t *testing.T) {
	config := DefaultPourConfig()
	term := NewTerminalFromText("aaaa\nbbbb\ncccc", TerminalConfig{Width: 8, Height: 5})
	engine := NewEngine(term, NewRng(3))
	effect := NewPour(config)
	if err := effect.Build(engine); err != nil {
		t.Fatalf("Build: %v", err)
	}

	gradient, err := NewGradient(config.FinalGradientStops, config.FinalGradientSteps, false)
	if err != nil {
		t.Fatalf("NewGradient: %v", err)
	}
	canvas := term.Canvas
	mapping, err := gradient.BuildCoordinateColorMapping(
		canvas.TextBottom, canvas.TextTop, canvas.TextLeft, canvas.TextRight, config.FinalGradientDirection)
	if err != nil {
		t.Fatalf("BuildCoordinateColorMapping: %v", err)
	}

	if !effect.Advance(engine) {
		t.Fatal("the effect ended on its first frame")
	}
	for _, ch := range term.InputCharacters {
		if !ch.IsVisible {
			continue
		}
		if got := ch.Animation.CurrentVisual().Colors; !got.HasFg || got.Fg != config.StartingColor {
			t.Errorf("%q enters wearing %v, want the starting colour %v",
				ch.InputSymbol, got, config.StartingColor)
		}
	}
	for i := 0; i < 40000 && effect.Advance(engine); i++ {
	}

	settled := 0
	for _, ch := range term.InputCharacters {
		want := mapping.At(ch.InputCoord, gradient.Spectrum[0])
		got := ch.Animation.CurrentVisual().Colors
		if !got.HasFg || got.Fg != want {
			t.Errorf("%q at %v settled on %v, want the mapped colour %v",
				ch.InputSymbol, ch.InputCoord, got, want)
		}
		if want != config.StartingColor {
			settled++
		}
	}
	if settled == 0 {
		t.Fatal("every mapped colour is the starting colour, so the ramp had nowhere to go")
	}
}

// TestPourKeepsTheBackgroundUnderDynamicColors checks a cell that arrived with
// a background gets it back. On a captured screen the backgrounds are the
// selection bars, the filled panels and the window chrome, so an effect that
// ramped the foreground alone would blank all of them for the length of the
// run.
//
// Negative control: building the dynamic branch with a foreground gradient
// only leaves every character settling with no background at all and this
// fails.
func TestPourKeepsTheBackgroundUnderDynamicColors(t *testing.T) {
	fg, bg := RGB(240, 240, 240), RGB(20, 80, 160)
	grid := [][]InputCell{{
		{Symbol: "b", Fg: fg, HasFg: true, Bg: bg, HasBg: true},
		{Symbol: "a", Fg: fg, HasFg: true, Bg: bg, HasBg: true},
		{Symbol: "r", Fg: fg, HasFg: true, Bg: bg, HasBg: true},
	}}
	term := NewTerminalFromCells(grid, TerminalConfig{
		Width: 6, Height: 3, ExistingColorHandling: DynamicExistingColors,
	})
	engine := NewEngine(term, NewRng(11))
	effect := NewPour(DefaultPourConfig())
	if err := effect.Build(engine); err != nil {
		t.Fatalf("Build: %v", err)
	}
	for i := 0; i < 40000 && effect.Advance(engine); i++ {
	}
	for _, ch := range term.InputCharacters {
		got := ch.Animation.CurrentVisual().Colors
		if !got.HasBg || got.Bg != bg {
			t.Errorf("%q settled with background %v, want the one it arrived with %v",
				ch.InputSymbol, got, bg)
		}
		if !got.HasFg || got.Fg != fg {
			t.Errorf("%q settled with foreground %v, want the one it arrived with %v",
				ch.InputSymbol, got, fg)
		}
	}
}
