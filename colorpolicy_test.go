package tuiffects

import "testing"

// mixedColorGrid is a screen where every third row carried its own colours and
// the rest carried none, which is the shape of a real capture: window chrome
// and selection bars are explicitly coloured, ordinary output is not.
func mixedColorGrid(width, height int) [][]InputCell {
	const text = "the quick brown fox jumps over the lazy dog 0123456789 "
	grid := make([][]InputCell, height)
	for y := 0; y < height; y++ {
		row := make([]InputCell, width)
		for x := 0; x < width; x++ {
			cell := InputCell{Symbol: string(text[(y*7+x)%len(text)])}
			if y%3 == 0 {
				cell.Fg, cell.HasFg = Color{R: 0x40, G: 0xc0, B: 0x80}, true
				cell.Bg, cell.HasBg = Color{R: 0x20, G: 0x20, B: 0x50}, true
			}
			row[x] = cell
		}
		grid[y] = row
	}
	return grid
}

// TestDynamicColorsResolveEveryCharacterBackToItself is the contract of
// DynamicExistingColors, checked over the whole catalogue at once: the input
// was a picture that was already on the screen, so it has to come back as
// itself. A cell that arrived with no colour of its own has to come back with
// no colour of its own, wearing the terminal's default rather than the
// effect's palette. On a real shell capture that is most of the screen, so an
// effect that gets this wrong recolours the picture permanently.
//
// Negative control: putting the guard `dynamic && ch.UsesInputColors` back on
// any effect's `final = ch.Animation.InputColors` fails that effect. It was
// run against all fourteen that carried it.
func TestDynamicColorsResolveEveryCharacterBackToItself(t *testing.T) {
	const width, height, frameCap = 40, 12, 6000
	for _, name := range Names() {
		t.Run(name, func(t *testing.T) {
			descriptor, ok := Lookup(name)
			if !ok {
				t.Fatalf("effect %q vanished from the registry", name)
			}
			terminal := NewTerminalFromCells(mixedColorGrid(width, height), TerminalConfig{
				Width:                 width,
				Height:                height,
				ExistingColorHandling: DynamicExistingColors,
				MakeFillCharacters:    descriptor.NeedsFillCharacters,
			})
			engine := NewEngine(terminal, NewRng(7))
			effect := descriptor.New()
			if err := effect.Build(engine); err != nil {
				t.Fatalf("build: %v", err)
			}
			for frames := 0; effect.Advance(engine); frames++ {
				if frames > frameCap {
					t.Fatalf("still running after %d frames", frameCap)
				}
			}
			for y, row := range engine.Terminal.FrameRows() {
				if y%3 == 0 {
					continue // this row arrived coloured and must stay so
				}
				for x, visual := range row {
					if visual == nil {
						continue
					}
					if visual.Colors.HasFg || visual.Colors.HasBg {
						t.Fatalf(
							"row %d column %d arrived with no colour and settled wearing %+v",
							y, x, visual.Colors)
					}
				}
			}
		})
	}
}

// TestDynamicColorsKeepTheColoursACellArrivedWith is the other half of the
// same contract: a cell that did carry colours has to come back wearing
// exactly those, background included. This is what catches an effect that
// settles a filled bar on the effect's own palette, or drops its background.
//
// Negative control: changing any effect's dynamic branch back to
// `Fg(mapping.At(...))` fails it. Run against waves and pour.
func TestDynamicColorsKeepTheColoursACellArrivedWith(t *testing.T) {
	const width, height, frameCap = 40, 12, 6000
	wantFg := Color{R: 0x40, G: 0xc0, B: 0x80}
	wantBg := Color{R: 0x20, G: 0x20, B: 0x50}
	for _, name := range Names() {
		t.Run(name, func(t *testing.T) {
			descriptor, _ := Lookup(name)
			terminal := NewTerminalFromCells(mixedColorGrid(width, height), TerminalConfig{
				Width:                 width,
				Height:                height,
				ExistingColorHandling: DynamicExistingColors,
				MakeFillCharacters:    descriptor.NeedsFillCharacters,
			})
			engine := NewEngine(terminal, NewRng(7))
			effect := descriptor.New()
			if err := effect.Build(engine); err != nil {
				t.Fatalf("build: %v", err)
			}
			for frames := 0; effect.Advance(engine); frames++ {
				if frames > frameCap {
					t.Fatalf("still running after %d frames", frameCap)
				}
			}
			for y, row := range engine.Terminal.FrameRows() {
				if y%3 != 0 {
					continue
				}
				for x, visual := range row {
					if visual == nil {
						continue
					}
					if visual.Colors.Fg != wantFg || !visual.Colors.HasFg {
						t.Fatalf("row %d column %d settled on fg %+v, want %+v",
							y, x, visual.Colors, wantFg)
					}
					if visual.Colors.Bg != wantBg || !visual.Colors.HasBg {
						t.Fatalf("row %d column %d settled on bg %+v, want %+v",
							y, x, visual.Colors, wantBg)
					}
				}
			}
		})
	}
}
