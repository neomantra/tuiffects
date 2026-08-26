package tuiffects

import (
	"strings"
	"testing"
)

// crumbleTrack is where one character went: the frame it first left its input
// row on, the lowest row it reached and when, and the highest row it reached
// and when.
type crumbleTrack struct {
	leftInputRow int
	lowestRow    int
	lowestFrame  int
	highestRow   int
	highestFrame int
}

// TestCrumbleReformsIntoTheInputText runs crumble to completion.
//
// Negative control: pointing the strengthen scene at a fixed symbol instead of
// ch.InputSymbol leaves the final frame reading as that symbol.
func TestCrumbleReformsIntoTheInputText(t *testing.T) {
	const input = "crumble me"
	term := NewTerminalFromText(input, TerminalConfig{Width: 20, Height: 7})
	engine := NewEngine(term, NewRng(3))

	frames, err := Run(NewCrumble(DefaultCrumbleConfig()), engine, 40000)
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
	if frames[len(frames)/2] == frames[0] {
		t.Error("the middle frame is identical to the first one, so nothing is animating")
	}
}

// TestCrumbleFallsToTheFloorThenLeavesThroughTheTop checks the thing the
// effect is named for. A crumbling character has to reach the bottom of the
// canvas, and only afterwards be vacuumed out through the top, before it flies
// home. It also has to crumble progressively: the first character leaves its
// row well before the last one does.
//
// Negative control: aiming the fall waypoint at ch.InputCoord instead of the
// canvas floor makes nothing reach the bottom and the first half fails.
// Sending the whole of pendingChars to the weaken scene on one frame, instead
// of one delayed group at a time, makes every character leave on the same
// frame and the progressive half fails.
func TestCrumbleFallsToTheFloorThenLeavesThroughTheTop(t *testing.T) {
	// Anchored in the middle so there is canvas below the text to fall into
	// and canvas above it to be vacuumed through.
	term := NewTerminalFromText("abcd\nefgh", TerminalConfig{Width: 12, Height: 9, AnchorText: AnchorC})
	engine := NewEngine(term, NewRng(7))
	effect := NewCrumble(DefaultCrumbleConfig())
	if err := effect.Build(engine); err != nil {
		t.Fatalf("Build: %v", err)
	}

	canvas := term.Canvas
	if canvas.TextBottom <= canvas.Bottom {
		t.Fatal("the text already sits on the canvas floor, so there is no fall to check")
	}
	if canvas.TextTop >= canvas.Top {
		t.Fatal("the text already sits on the canvas ceiling, so there is no lift to check")
	}

	tracks := map[*Character]*crumbleTrack{}
	for _, ch := range term.InputCharacters {
		tracks[ch] = &crumbleTrack{
			leftInputRow: 0,
			lowestRow:    ch.Motion.CurrentCoord.Row,
			highestRow:   ch.Motion.CurrentCoord.Row,
		}
	}

	frame := 0
	for frame < 40000 && effect.Advance(engine) {
		frame++
		for _, ch := range term.InputCharacters {
			track := tracks[ch]
			row := ch.Motion.CurrentCoord.Row
			if row != ch.InputCoord.Row && track.leftInputRow == 0 {
				track.leftInputRow = frame
			}
			if row < track.lowestRow {
				track.lowestRow, track.lowestFrame = row, frame
			}
			if row > track.highestRow {
				track.highestRow, track.highestFrame = row, frame
			}
		}
	}
	if frame >= 40000 {
		t.Fatal("the effect never finished within the frame cap")
	}

	firstLeft, lastLeft := 1<<30, 0
	for _, ch := range term.InputCharacters {
		track := tracks[ch]
		if track.lowestRow != canvas.Bottom {
			t.Errorf("%q fell only to row %d, want the canvas floor (%d)",
				ch.InputSymbol, track.lowestRow, canvas.Bottom)
		}
		if track.highestRow != canvas.Top {
			t.Errorf("%q rose only to row %d, want the canvas ceiling (%d)",
				ch.InputSymbol, track.highestRow, canvas.Top)
		}
		if track.lowestFrame >= track.highestFrame {
			t.Errorf("%q reached the floor on frame %d and the ceiling on frame %d, "+
				"want it to fall before it is vacuumed", ch.InputSymbol, track.lowestFrame, track.highestFrame)
		}
		if track.leftInputRow == 0 {
			t.Errorf("%q never left its input row", ch.InputSymbol)
			continue
		}
		firstLeft = min(firstLeft, track.leftInputRow)
		lastLeft = max(lastLeft, track.leftInputRow)
		if got := ch.Motion.CurrentCoord; got != ch.InputCoord {
			t.Errorf("%q ended at %v, want its input coordinate %v", ch.InputSymbol, got, ch.InputCoord)
		}
	}
	if firstLeft == lastLeft {
		t.Errorf("every character left its row on frame %d, so the collapse is not progressive", firstLeft)
	}
}

// TestCrumblePassesOverTheScreenRatherThanAssemblingIt checks crumble shows
// the whole picture from the first frame under every colour policy.
//
// Crumble takes a screen that is already there and breaks it up, so hiding the
// text and revealing it would give the effect nothing to crumble. That matters
// most under DynamicExistingColors, where the input is a captured screen.
//
// Negative control: dropping SetCharacterVisibility from Build leaves every
// character hidden and the first frame blank, and both halves fail.
func TestCrumblePassesOverTheScreenRatherThanAssemblingIt(t *testing.T) {
	const input = "already here"
	for _, handling := range []ExistingColorHandling{IgnoreExistingColors, DynamicExistingColors} {
		term := NewTerminalFromText(input, TerminalConfig{
			Width: 20, Height: 5, ExistingColorHandling: handling,
		})
		engine := NewEngine(term, NewRng(5))
		effect := NewCrumble(DefaultCrumbleConfig())
		if err := effect.Build(engine); err != nil {
			t.Fatalf("Build: %v", err)
		}
		for _, ch := range term.InputCharacters {
			if !ch.IsVisible {
				t.Fatalf("handling %v: %q is hidden before the first frame", handling, ch.InputSymbol)
			}
		}
		if !effect.Advance(engine) {
			t.Fatalf("handling %v: the effect ended on its first frame", handling)
		}
		if first := strings.Join(nonBlank(engine.Frame()), ""); !strings.Contains(first, input) {
			t.Errorf("handling %v: the first frame reads %q, want the whole input on screen",
				handling, first)
		}
	}
}

// TestCrumbleDimsBeforeItFalls checks a character is weakened rather than
// crumbling at full strength: it starts on a dimmed version of the colour it
// ends on, and it does end on the mapped one.
//
// Negative control: passing 1.0 to AdjustColorBrightness instead of 0.65
// leaves the opening colour equal to the final one and the first half fails.
func TestCrumbleDimsBeforeItFalls(t *testing.T) {
	config := DefaultCrumbleConfig()
	term := NewTerminalFromText("aaaa\nbbbb", TerminalConfig{Width: 10, Height: 6})
	engine := NewEngine(term, NewRng(3))
	effect := NewCrumble(config)
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
		want := AdjustColorBrightness(mapping.At(ch.InputCoord, gradient.Spectrum[0]), 0.65)
		got := ch.Animation.CurrentVisual().Colors
		if !got.HasFg || got.Fg != want {
			t.Errorf("%q at %v opens on %v, want the dimmed colour %v",
				ch.InputSymbol, ch.InputCoord, got, want)
		}
	}
	for i := 0; i < 40000 && effect.Advance(engine); i++ {
	}
	for _, ch := range term.InputCharacters {
		want := mapping.At(ch.InputCoord, gradient.Spectrum[0])
		got := ch.Animation.CurrentVisual().Colors
		if !got.HasFg || got.Fg != want {
			t.Errorf("%q at %v settled on %v, want the mapped colour %v",
				ch.InputSymbol, ch.InputCoord, got, want)
		}
	}
}

// TestCrumbleKeepsTheBackgroundUnderDynamicColors checks a cell that arrived
// with a background keeps one the whole way through and gets the original one
// back at the end. On a captured screen the backgrounds are the selection
// bars, the filled panels and the window chrome, so an effect that dimmed the
// foreground alone would blank all of them for the length of the run.
//
// Negative control: leaving weak.Bg, dust.Bg and the background gradients out
// of the dynamic branch loses the background on the very first frame, so the
// per-frame half fails there; with that check removed, the settled half fails
// too, every character ending on the foreground colour and no background.
func TestCrumbleKeepsTheBackgroundUnderDynamicColors(t *testing.T) {
	fg, bg := RGB(240, 240, 240), RGB(20, 80, 160)
	grid := [][]InputCell{{
		{Symbol: "b", Fg: fg, HasFg: true, Bg: bg, HasBg: true},
		{Symbol: "a", Fg: fg, HasFg: true, Bg: bg, HasBg: true},
		{Symbol: "r", Fg: fg, HasFg: true, Bg: bg, HasBg: true},
	}}
	term := NewTerminalFromCells(grid, TerminalConfig{
		Width: 8, Height: 5, ExistingColorHandling: DynamicExistingColors,
	})
	engine := NewEngine(term, NewRng(11))
	effect := NewCrumble(DefaultCrumbleConfig())
	if err := effect.Build(engine); err != nil {
		t.Fatalf("Build: %v", err)
	}

	for i := 0; i < 40000 && effect.Advance(engine); i++ {
		for _, ch := range term.InputCharacters {
			if got := ch.Animation.CurrentVisual().Colors; !got.HasBg {
				t.Fatalf("%q lost its background on frame %d", ch.InputSymbol, i+1)
			}
		}
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
