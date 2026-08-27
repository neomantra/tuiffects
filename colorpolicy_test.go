package tuiffects

import (
	"fmt"
	"testing"
)

// mixedColorGrid is a screen holding the three kinds of cell a real capture
// holds. Rows repeat in threes: one that carried a foreground and a
// background, one that carried a foreground and no background, and one that
// carried no colour at all. The second and third are most of a shell capture,
// and the second is the one an effect is most likely to get wrong, because it
// has a colour to keep and a background to leave alone.
//
// The middle half of every third row is a filled bar: background-only spaces
// on a background of their own, which is what a selection row, a divider and a
// piece of window chrome all are. The bar's background is not the one the
// coloured rows wear, so a cell that picks up a background from somewhere else
// on the screen is a colour that can be named rather than a coincidence.
//
// The foreground-only rows carry no spaces. A foreground on a blank cell
// paints nothing, so every effect here settles it as an empty cell and the
// colour cannot be asserted on either way.
func mixedColorGrid(width, height int) [][]InputCell {
	const text = "the quick brown fox jumps over the lazy dog 0123456789 "
	const dense = "thequickbrownfoxjumpsoverthelazydog0123456789"
	rowFg := Color{R: 0x40, G: 0xc0, B: 0x80}
	rowBg := Color{R: 0x20, G: 0x20, B: 0x50}
	plainFg := Color{R: 0xf0, G: 0xf0, B: 0xf0}
	barBg := Color{R: 0x80, G: 0x30, B: 0x30}
	barLeft, barRight := width/4, width-width/4
	grid := make([][]InputCell, height)
	for y := 0; y < height; y++ {
		row := make([]InputCell, width)
		for x := 0; x < width; x++ {
			cell := InputCell{Symbol: string(text[(y*7+x)%len(text)])}
			switch {
			case y%3 == 0:
				cell.Fg, cell.HasFg = rowFg, true
				cell.Bg, cell.HasBg = rowBg, true
			case y%3 == 1:
				cell.Symbol = string(dense[(y*7+x)%len(dense)])
				cell.Fg, cell.HasFg = plainFg, true
			case x >= barLeft && x < barRight:
				cell = InputCell{Symbol: " ", Bg: barBg, HasBg: true}
			}
			row[x] = cell
		}
		grid[y] = row
	}
	return grid
}

// runWholeCatalogue builds every registered effect over mixedColorGrid under
// DynamicExistingColors, runs it to the end, and hands the settled screen and
// the input it was built from to check.
func runWholeCatalogue(t *testing.T, check func(t *testing.T, in [][]InputCell, out [][]*CharacterVisual)) {
	t.Helper()
	const width, height, frameCap = 40, 12, 6000
	for _, name := range Names() {
		t.Run(name, func(t *testing.T) {
			descriptor, ok := Lookup(name)
			if !ok {
				t.Fatalf("effect %q vanished from the registry", name)
			}
			grid := mixedColorGrid(width, height)
			terminal := NewTerminalFromCells(grid, TerminalConfig{
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
			check(t, grid, engine.Terminal.FrameRows())
		})
	}
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
	runWholeCatalogue(t, func(t *testing.T, in [][]InputCell, out [][]*CharacterVisual) {
		for y, row := range out {
			for x, visual := range row {
				if visual == nil || in[y][x].HasFg || in[y][x].HasBg {
					continue // this cell arrived coloured and is checked elsewhere
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

// TestDynamicColorsKeepTheColoursACellArrivedWith is the other half of the
// same contract: a cell that did carry colours has to come back wearing
// exactly those, background included. This is what catches an effect that
// settles a filled bar on the effect's own palette, or drops its background.
//
// Negative control: changing any effect's dynamic branch back to
// `Fg(mapping.At(...))` fails it. Run against waves and pour.
func TestDynamicColorsKeepTheColoursACellArrivedWith(t *testing.T) {
	runWholeCatalogue(t, func(t *testing.T, in [][]InputCell, out [][]*CharacterVisual) {
		for y, row := range out {
			for x, visual := range row {
				want := in[y][x]
				if visual == nil || (!want.HasFg && !want.HasBg) {
					continue
				}
				if want.HasFg && (!visual.Colors.HasFg || visual.Colors.Fg != want.Fg) {
					t.Fatalf("row %d column %d settled on fg %+v, want %+v",
						y, x, visual.Colors, want.Fg)
				}
				if want.HasBg && (!visual.Colors.HasBg || visual.Colors.Bg != want.Bg) {
					t.Fatalf("row %d column %d settled on bg %+v, want %+v",
						y, x, visual.Colors, want.Bg)
				}
			}
		}
	})
}

// TestDynamicColorsNeverLendACellABackgroundItNeverHad is the half the two
// tests above cannot express. Both of them read a background that is present
// and ask whether it is right. Neither can see a background that should not be
// there at all.
//
// A cell that arrived with a foreground and no background is ordinary shell
// output, and it is the case an effect breaks by carrying a background from
// somewhere else. An effect that paints a flier over the bar it is crossing,
// so it does not punch a hole through it, has to stop painting once the flier
// lands. If it does not, the cell keeps the bar's colour, permanently, and the
// screen does not come back as itself.
//
// It counts rather than stopping at the first, because the number says whether
// this is one stray cell or a whole flight path.
//
// Negative control: dropping the second PathComplete on tuffbaby's "home" path
// reports 7 of 226 cells at this size, and hundreds on a full screen.
func TestDynamicColorsNeverLendACellABackgroundItNeverHad(t *testing.T) {
	runWholeCatalogue(t, func(t *testing.T, in [][]InputCell, out [][]*CharacterVisual) {
		borrowed, checked := 0, 0
		first := ""
		for y, row := range out {
			for x, visual := range row {
				if visual == nil || in[y][x].HasBg {
					continue
				}
				checked++
				if !visual.Colors.HasBg {
					continue
				}
				borrowed++
				if first == "" {
					first = fmt.Sprintf("row %d column %d on %s", y, x, visual.Colors.Bg.Hex())
				}
			}
		}
		if checked == 0 {
			t.Fatal("no cell arrived without a background, so nothing was tested")
		}
		if borrowed > 0 {
			t.Errorf("%d of %d cells that arrived with no background settled wearing one, first at %s",
				borrowed, checked, first)
		}
	})
}
