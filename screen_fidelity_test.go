package tuiffects

import "testing"

// screenGrid is a captured screen with a filled bar across one row. The bar is
// background-only spaces, which is what a selection row, a divider, a progress
// trough and a piece of window chrome all are on a real terminal. Everything
// else is ordinary text with an explicit foreground.
func screenGrid(width, height, barRow int) [][]InputCell {
	const text = "the quick brown fox jumps over the lazy dog 0123456789 "
	bg := Color{R: 0x30, G: 0x30, B: 0x80}
	fg := Color{R: 0xf0, G: 0xf0, B: 0xf0}
	grid := make([][]InputCell, height)
	for y := 0; y < height; y++ {
		row := make([]InputCell, width)
		for x := 0; x < width; x++ {
			if y == barRow {
				row[x] = InputCell{Symbol: " ", Bg: bg, HasBg: true}
				continue
			}
			row[x] = InputCell{Symbol: string(text[(y*7+x)%len(text)]), Fg: fg, HasFg: true}
		}
		grid[y] = row
	}
	return grid
}

// runOnScreen builds a named effect over screenGrid under DynamicExistingColors
// and calls inspect with the engine once per frame. It returns the frame count
// and the engine, so a caller can look at the settled picture afterwards.
func runOnScreen(
	t *testing.T, name string, width, height, barRow int, inspect func(e *Engine, frame int),
) (int, *Engine) {
	t.Helper()
	descriptor, ok := Lookup(name)
	if !ok {
		t.Fatalf("effect %q is not registered", name)
	}
	terminal := NewTerminalFromCells(screenGrid(width, height, barRow), TerminalConfig{
		Width:                 width,
		Height:                height,
		ExistingColorHandling: DynamicExistingColors,
		MakeFillCharacters:    descriptor.NeedsFillCharacters,
	})
	engine := NewEngine(terminal, NewRng(3))
	effect := descriptor.New()
	if err := effect.Build(engine); err != nil {
		t.Fatalf("build: %v", err)
	}
	frames := 0
	for effect.Advance(engine) {
		frames++
		inspect(engine, frames)
		if frames > 6000 {
			t.Fatalf("still running after 6000 frames")
		}
	}
	return frames, engine
}

// TestPrintKeepsARowThatIsOnlyABar guards the one place print judges a row
// empty. A row of spaces is nothing over piped text and is a filled bar on a
// captured screen, and print hides every character up front and only reveals
// what the head strikes, so a row it collapses never comes back at all.
//
// Negative control: dropping the `dynamic && HasBg` clause from makeRow's
// allSpaces loop leaves twenty-three of the twenty-four bar cells missing from
// the finished picture. Run.
func TestPrintKeepsARowThatIsOnlyABar(t *testing.T) {
	const width, height, barRow = 24, 3, 1
	_, engine := runOnScreen(t, "print", width, height, barRow, func(*Engine, int) {})
	rows := engine.Terminal.FrameRows()
	missing := 0
	for _, visual := range rows[barRow] {
		if visual == nil || !visual.Colors.HasBg {
			missing++
		}
	}
	if missing > 0 {
		t.Errorf("%d of %d bar cells never came back", missing, width)
	}
}

// TestMiddleOutNeverFlashesTheBarWhite guards the colour ramp. Middleout opens
// every ramp on StartingColor, which is white. Ramping the background from
// there as well as the foreground turns every filled bar and panel into a
// white slab with its own text invisible on it, for the half second the
// picture is assembling.
//
// Negative control: ramping the background from StartingColor again, which is
// upstream's, reports foreground equal to background on the bar. Run.
func TestMiddleOutNeverFlashesTheBarWhite(t *testing.T) {
	const width, height, barRow = 40, 12, 4
	runOnScreen(t, "middleout", width, height, barRow, func(engine *Engine, frame int) {
		for y, row := range engine.Terminal.FrameRows() {
			for x, visual := range row {
				if visual == nil || !visual.Colors.HasBg {
					continue
				}
				if visual.Colors.HasFg && visual.Colors.Fg == visual.Colors.Bg {
					t.Fatalf("frame %d row %d column %d is %v on %v, which is invisible",
						frame, y, x, visual.Colors.Fg, visual.Colors.Bg)
				}
				near := func(c Color) bool { return c.R > 0xd0 && c.G > 0xd0 && c.B > 0xd0 }
				if near(visual.Colors.Bg) {
					t.Fatalf("frame %d row %d column %d has a near-white background %v",
						frame, y, x, visual.Colors.Bg)
				}
			}
		}
	})
}

// TestPourNeverFlashesTheBarWhite is the same guard on pour, which opens its
// ramps on the same white StartingColor.
//
// Negative control: ramping the background from StartingColor again reports a
// near-white background on the bar for about sixty frames. Run.
func TestPourNeverFlashesTheBarWhite(t *testing.T) {
	const width, height, barRow = 40, 12, 4
	runOnScreen(t, "pour", width, height, barRow, func(engine *Engine, frame int) {
		for y, row := range engine.Terminal.FrameRows() {
			for x, visual := range row {
				if visual == nil || !visual.Colors.HasBg {
					continue
				}
				c := visual.Colors.Bg
				if c.R > 0xd0 && c.G > 0xd0 && c.B > 0xd0 {
					t.Fatalf("frame %d row %d column %d has a near-white background %v",
						frame, y, x, c)
				}
			}
		}
	})
}

// TestHighlightLightsEveryCharacterOnADefaultColouredScreen guards the band.
// Upstream gives a character with no foreground of its own no band at all,
// which over piped text never happens because every character there is given a
// gradient colour. On a captured screen most of the picture is default
// coloured, so most of it would never light and the effect would read as
// broken.
//
// Negative control: dropping the DynamicNeutralGrey stand-in leaves every one
// of the 262 characters unlit for the whole run. Run.
func TestHighlightLightsEveryCharacterOnADefaultColouredScreen(t *testing.T) {
	const width, height = 40, 8
	const text = "the quick brown fox jumps over the lazy dog 0123456789 "
	grid := make([][]InputCell, height)
	for y := 0; y < height; y++ {
		row := make([]InputCell, width)
		for x := 0; x < width; x++ {
			row[x] = InputCell{Symbol: string(text[(y*7+x)%len(text)])}
		}
		grid[y] = row
	}
	terminal := NewTerminalFromCells(grid, TerminalConfig{
		Width: width, Height: height, ExistingColorHandling: DynamicExistingColors,
	})
	engine := NewEngine(terminal, NewRng(3))
	effect := NewHighlight(DefaultHighlightConfig())
	if err := effect.Build(engine); err != nil {
		t.Fatalf("build: %v", err)
	}
	lit := make(map[*Character]bool, len(terminal.InputCharacters))
	for effect.Advance(engine) {
		for _, ch := range terminal.InputCharacters {
			if ch.Animation.CurrentVisual().Colors.HasFg {
				lit[ch] = true
			}
		}
	}
	unlit := 0
	for _, ch := range terminal.InputCharacters {
		if !lit[ch] {
			unlit++
		}
	}
	if unlit > 0 {
		t.Errorf("%d of %d characters never lit", unlit, len(terminal.InputCharacters))
	}
	// It lights, and it still comes back to the terminal default it arrived as.
	for _, ch := range terminal.InputCharacters {
		if ch.Animation.CurrentVisual().Colors.HasFg {
			t.Fatalf("%q settled wearing the stand-in grey", ch.InputSymbol)
			break
		}
	}
}

// TestSwarmDrawsACellThatIsOnlyABackground guards the flight. A cell whose
// symbol is a space and whose only colour is a background draws nothing at all
// while it flies unless the flash is put on both channels, and on a captured
// screen that is a large part of the picture missing for most of the run.
//
// Negative control: putting Fg(step) back on the flash frames drops every one
// of these cell-frames to nothing drawn. Run.
func TestSwarmDrawsACellThatIsOnlyABackground(t *testing.T) {
	const width, height, barRow = 40, 12, 4
	var inFlight, drawn int
	runOnScreen(t, "swarm", width, height, barRow, func(engine *Engine, _ int) {
		for _, ch := range engine.Terminal.InputCharacters {
			if ch.InputSymbol != " " || !ch.Animation.InputColors.HasBg {
				continue
			}
			if !ch.IsVisible || ch.Motion.CurrentCoord == ch.InputCoord {
				continue
			}
			inFlight++
			if ch.Animation.CurrentVisual().Colors.HasBg {
				drawn++
			}
		}
	})
	if inFlight == 0 {
		t.Fatal("no background-only cell was ever in flight, so nothing was tested")
	}
	if drawn != inFlight {
		t.Errorf("%d of %d in-flight cell-frames drew nothing", inFlight-drawn, inFlight)
	}
}

// TestNothingPunchesAHoleThroughAFilledBar guards the effects that throw
// something across the screen: a spark, a raindrop, a puff of smoke, a
// travelling digit, a character standing in the wrong cell. Each carries a
// foreground and no background, which over piped text draws on nothing and
// over a captured screen takes the fill out of whatever it is crossing, moving
// every frame.
//
// A hole here means a cell that has already been rebuilt - its own character
// is home and wearing its own background - rendering with no background,
// which can only be something standing over it.
//
// Negative control: removing any one of the six carry-the-background passes
// fails that effect. Run against all six. tuffbaby's pass is its own, because
// what flies over the bar there is the screen's own text rather than anything
// the effect added; dropping both of its call sites reports 80 of 1459 rebuilt
// bar cell-frames punched out.
func TestNothingPunchesAHoleThroughAFilledBar(t *testing.T) {
	const width, height, barRow = 40, 12, 4
	for _, name := range []string{"binarypath", "burn", "errorcorrect", "thunderstorm", "laseretch", "tuffbaby"} {
		t.Run(name, func(t *testing.T) {
			settled := make(map[*Character]bool)
			barCanvasRow := height - barRow
			holes, rebuilt := 0, 0
			frames, _ := runOnScreen(t, name, width, height, barRow, func(engine *Engine, _ int) {
				terminal := engine.Terminal
				row := terminal.FrameRows()[barRow]
				for x := 0; x < width; x++ {
					ch := terminal.CharacterAtInputCoord(C(x+1, barCanvasRow))
					if ch == nil {
						continue
					}
					home := ch.Motion.CurrentCoord == ch.InputCoord
					if home && ch.IsVisible && ch.Animation.CurrentVisual().Colors.HasBg {
						settled[ch] = true
					}
					if !settled[ch] || !home {
						continue
					}
					rebuilt++
					if visual := row[x]; visual == nil || !visual.Colors.HasBg {
						holes++
					}
				}
			})
			if rebuilt == 0 {
				t.Fatal("no bar cell was ever rebuilt, so nothing was tested")
			}
			if holes > 0 {
				t.Errorf("%d of %d rebuilt bar cell-frames were punched out over %d frames",
					holes, rebuilt, frames)
			}
		})
	}
}

// TestSweepCarriesTheBackgroundThroughBothBands guards sweep's bands.
// Upstream writes a foreground on every band frame and puts the background
// back only on the very last frame of the second band, so a filled bar is a
// hole in the picture for the whole run and then snaps on in one frame.
//
// Negative control: writing Fg alone on the band frames again reports the bar
// missing in more than two hundred of the two hundred and twenty-one frames.
// Run.
func TestSweepCarriesTheBackgroundThroughBothBands(t *testing.T) {
	const width, height, barRow = 40, 12, 4
	barCanvasRow := height - barRow
	holes, shown := 0, 0
	frames, _ := runOnScreen(t, "sweep", width, height, barRow, func(engine *Engine, _ int) {
		terminal := engine.Terminal
		row := terminal.FrameRows()[barRow]
		for x := 0; x < width; x++ {
			// The band uncovers the cell. From the frame it is on screen it
			// has to be showing its own fill, not waiting for the last frame
			// of the second band to put it back.
			ch := terminal.CharacterAtInputCoord(C(x+1, barCanvasRow))
			if ch == nil || !ch.IsVisible {
				continue
			}
			shown++
			if visual := row[x]; visual == nil || !visual.Colors.HasBg {
				holes++
			}
		}
	})
	if shown == 0 {
		t.Fatal("no bar cell was ever uncovered, so nothing was tested")
	}
	if holes > 0 {
		t.Errorf("an uncovered bar cell showed no background in %d of %d cell-frames over %d frames",
			holes, shown, frames)
	}
}

// TestErrorCorrectNeverEmptiesACellItVacated guards the ghost. Both members of
// a pair leave their cells on the same frame and arrive on the same frame, so
// for the length of a flight neither cell holds anything, and over a captured
// screen that is a hole straight through a filled bar. Every character is on
// screen from the first frame in this effect, so the bar is never legitimately
// absent and can be asserted on every frame.
//
// Negative control: making layGhost return without laying anything reports the
// bar empty for the length of every flight. Run.
func TestErrorCorrectNeverEmptiesACellItVacated(t *testing.T) {
	const width, height, barRow = 40, 12, 4
	empty := 0
	frames, _ := runOnScreen(t, "errorcorrect", width, height, barRow, func(engine *Engine, _ int) {
		for _, visual := range engine.Terminal.FrameRows()[barRow] {
			if visual == nil {
				empty++
			}
		}
	})
	if empty > 0 {
		t.Errorf("the bar had no character at all in %d cell-frames over %d frames",
			empty, frames)
	}
}
