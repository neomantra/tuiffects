package tuiffects

import (
	"math"
	"testing"
)

// These helpers exist for effects nobody has ported yet, so their tests are
// what stops them drifting before the first caller arrives.

// TestFindCoordsInCircleIsAnEllipse pins the shape upstream actually builds.
//
// It is named for a circle and is an ellipse: the horizontal semi-axis is the
// diameter and the vertical one is half of it, which is the cell aspect
// correction done in the shape. For a diameter of 4 centred on (10,10) the
// widest row therefore spans columns 6 to 14, nine cells, while the tallest
// column spans rows 8 to 12, five cells.
//
// Negative control: using the diameter for both axes makes the vertical extent
// match the horizontal one and this fails.
func TestFindCoordsInCircleIsAnEllipse(t *testing.T) {
	coords := FindCoordsInCircle(C(10, 10), 4)
	if len(coords) == 0 {
		t.Fatal("no coordinates returned")
	}
	minCol, maxCol := coords[0].Column, coords[0].Column
	minRow, maxRow := coords[0].Row, coords[0].Row
	for _, c := range coords {
		minCol = min(minCol, c.Column)
		maxCol = max(maxCol, c.Column)
		minRow = min(minRow, c.Row)
		maxRow = max(maxRow, c.Row)
	}
	if minCol != 6 || maxCol != 14 {
		t.Errorf("columns span %d to %d, want 6 to 14", minCol, maxCol)
	}
	if minRow != 8 || maxRow != 12 {
		t.Errorf("rows span %d to %d, want 8 to 12", minRow, maxRow)
	}
	if maxCol-minCol <= maxRow-minRow {
		t.Error("the shape is not wider than it is tall, so the aspect correction is gone")
	}
}

// TestFindCoordsInCircleIsEmptyAtZero guards the early return.
//
// Negative control: dropping it makes the loop run once and return the centre.
func TestFindCoordsInCircleIsEmptyAtZero(t *testing.T) {
	if got := FindCoordsInCircle(C(5, 5), 0); len(got) != 0 {
		t.Errorf("a diameter of zero returned %d coordinates, want none", len(got))
	}
}

// TestFindCoordsInRectCoversTheWholeBlock checks the square block, which is
// (2d+1) by (2d+1) cells.
//
// Negative control: iterating to distance rather than through it returns a
// smaller block.
func TestFindCoordsInRectCoversTheWholeBlock(t *testing.T) {
	coords := FindCoordsInRect(C(5, 5), 2)
	if len(coords) != 25 {
		t.Errorf("returned %d coordinates, want 25 for a distance of 2", len(coords))
	}
	seen := map[Coord]bool{}
	for _, c := range coords {
		seen[c] = true
	}
	for _, want := range []Coord{C(3, 3), C(7, 7), C(5, 5), C(3, 7), C(7, 3)} {
		if !seen[want] {
			t.Errorf("%v is missing from the block", want)
		}
	}
	if got := FindCoordsInRect(C(5, 5), 0); len(got) != 0 {
		t.Errorf("a distance of zero returned %d coordinates, want none", len(got))
	}
}

// TestFindCoordsOnRectIsPerimeterOnly checks the outline, not the fill.
//
// A 2 by 1 half-size gives a 5 by 3 rectangle: 15 cells in total, 12 on the
// edge and 3 inside, of which only the centre row's middle is interior.
//
// Negative control: emitting every row for every column returns the filled
// block and the interior check fails.
func TestFindCoordsOnRectIsPerimeterOnly(t *testing.T) {
	coords := FindCoordsOnRect(C(5, 5), 2, 1)
	seen := map[Coord]bool{}
	for _, c := range coords {
		seen[c] = true
	}
	for _, want := range []Coord{C(3, 5), C(7, 5), C(5, 4), C(5, 6), C(3, 4), C(7, 6)} {
		if !seen[want] {
			t.Errorf("%v is missing from the perimeter", want)
		}
	}
	if seen[C(5, 5)] {
		t.Error("the centre is on the perimeter, so this is a filled block")
	}
	if got := FindCoordsOnRect(C(5, 5), 0, 3); len(got) != 0 {
		t.Errorf("a zero half-width returned %d coordinates, want none", len(got))
	}
}

// TestExtrapolateAlongRayGoesPastTheTarget checks the ray carries on.
//
// From (1,1) to (5,1) is four cells with no row change, so carrying on three
// more lands on column 8.
//
// Negative control: clamping t to 1 returns the target itself.
func TestExtrapolateAlongRayGoesPastTheTarget(t *testing.T) {
	if got := ExtrapolateAlongRay(C(1, 1), C(5, 1), 3); got != C(8, 1) {
		t.Errorf("extrapolating three past (5,1) gave %v, want (8,1)", got)
	}
	// A zero-length ray has no direction to travel along.
	if got := ExtrapolateAlongRay(C(4, 4), C(4, 4), 5); got != C(4, 4) {
		t.Errorf("a ray with no length gave %v, want the target back", got)
	}
}

// TestExtrapolateAlongRayUsesTheRawLength pins the one place in this file that
// does not double the row delta.
//
// From (1,1) to (1,5) the raw length is 4 and the doubled length is 8. Asking
// to carry on by 4 lands on row 9 under the raw length and would land
// somewhere else under the doubled one.
//
// Negative control: passing true for doubleRowDiff changes the answer.
func TestExtrapolateAlongRayUsesTheRawLength(t *testing.T) {
	if got := ExtrapolateAlongRay(C(1, 1), C(1, 5), 4); got != C(1, 9) {
		t.Errorf("extrapolating four past (1,5) gave %v, want (1,9)", got)
	}
}

// TestAdjustColorBrightnessKeepsHue checks the round trip changes lightness
// and nothing else.
//
// Negative control: scaling the channels directly instead of going through HSL
// changes the ratios between them and the hue drifts.
func TestAdjustColorBrightnessKeepsHue(t *testing.T) {
	// A saturated red darkened by a third stays red: green and blue stay at
	// zero and only the red channel moves.
	dark := AdjustColorBrightness(RGB(255, 0, 0), 0.5)
	if dark.G != 0 || dark.B != 0 {
		t.Errorf("darkening pure red gave %v, want the other channels at zero", dark)
	}
	if dark.R == 0 || dark.R >= 255 {
		t.Errorf("darkening pure red gave red channel %d, want it between 0 and 255", dark.R)
	}

	// Grey has no hue to keep, so every channel stays equal.
	grey := AdjustColorBrightness(RGB(128, 128, 128), 0.5)
	if grey.R != grey.G || grey.G != grey.B {
		t.Errorf("darkening grey gave %v, want all three channels equal", grey)
	}
}

// TestAdjustColorBrightnessClampsAtTheEnds checks the extremes behave.
//
// Negative control: two clamps guard this and either one alone is enough, so
// removing just one leaves the test passing. Removing both, the lightness
// clamp in the conversion and the upper bound in clampChannel, makes a bright
// factor come back as {141 169 55} instead of a saturated red. Checked that
// way round rather than asserting a control that does not fail.
func TestAdjustColorBrightnessClampsAtTheEnds(t *testing.T) {
	if got := AdjustColorBrightness(RGB(255, 0, 0), 0); got != RGB(0, 0, 0) {
		t.Errorf("a factor of zero gave %v, want black", got)
	}
	got := AdjustColorBrightness(RGB(200, 100, 50), 10)
	if got.R != 255 {
		t.Errorf("a very bright factor gave %v, want the red channel saturated at 255", got)
	}
	// A factor of 1 is the identity, within the rounding the round trip costs.
	same := AdjustColorBrightness(RGB(200, 100, 50), 1)
	for _, d := range []int{int(same.R) - 200, int(same.G) - 100, int(same.B) - 50} {
		if math.Abs(float64(d)) > 1 {
			t.Errorf("a factor of one gave %v, want the colour back within one unit", same)
			break
		}
	}
}

// TestDescriptorCanAskForFillCharacters checks the declaration an effect makes
// when it animates the empty cells of the canvas as well as the input.
//
// The terminal is built before the effect is and a fill character cannot be
// added afterwards, so an effect that queries the fill populations without
// declaring this gets nothing back and silently animates an empty set. Seven
// of the effects still to port need it.
//
// Negative control: building a terminal without MakeFillCharacters leaves both
// fill populations empty, which is the second half of this test.
func TestDescriptorCanAskForFillCharacters(t *testing.T) {
	withFill := NewTerminalFromText("ab", TerminalConfig{
		Width: 6, Height: 3, MakeFillCharacters: true,
	})
	if len(withFill.InnerFillCharacters)+len(withFill.OuterFillCharacters) == 0 {
		t.Error("a terminal asked for fill characters has none")
	}
	filter := CharacterFilter{Input: true, InnerFill: true, OuterFill: true}
	if got := len(withFill.CollectCharacters(filter)); got != 6*3 {
		t.Errorf("the canvas holds %d characters, want one per cell (%d)", got, 6*3)
	}

	without := NewTerminalFromText("ab", TerminalConfig{Width: 6, Height: 3})
	if len(without.InnerFillCharacters)+len(without.OuterFillCharacters) != 0 {
		t.Error("a terminal not asked for fill characters made some anyway")
	}
	if got := len(without.CollectCharacters(filter)); got != 2 {
		t.Errorf("without fill characters the canvas holds %d, want the 2 input characters", got)
	}
}
