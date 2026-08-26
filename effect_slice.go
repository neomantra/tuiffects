package tuiffects

// slice, ported from ttfx src/effects/slice.rs, which ports
// TerminalTextEffects effects/effect_slice.py by ChrisBuilds.

func init() {
	Register(Descriptor{
		Name:        "slice",
		Description: "The screen is cut in two and the halves slide in from opposite edges",
		New:         func() Effect { return NewSlice(DefaultSliceConfig()) },
		// The horizontal cut runs over the whole canvas, not over the text, so
		// it queries the fill populations. Without this the query comes back
		// empty and the effect animates nothing. Upstream always makes fill
		// characters, so no ttfx effect has to declare it.
		NeedsFillCharacters: true,
	})
}

// SliceDirection is the axis the cut runs along.
type SliceDirection int

// The three directions upstream offers.
const (
	// SliceVertical cuts down the middle column. The left half falls in from
	// above and the right half rises in from below.
	SliceVertical SliceDirection = iota
	// SliceHorizontal cuts across the middle row. The bottom half comes in
	// from the left and the top half from the right.
	SliceHorizontal
	// SliceDiagonal cuts along the diagonals. Half the diagonals rise from
	// below and half fall from above.
	SliceDiagonal
)

var sliceDirectionNames = map[string]SliceDirection{
	"vertical":   SliceVertical,
	"horizontal": SliceHorizontal,
	"diagonal":   SliceDiagonal,
}

// ParseSliceDirection looks a direction up by its upstream name.
func ParseSliceDirection(name string) (SliceDirection, bool) {
	d, ok := sliceDirectionNames[name]
	return d, ok
}

// SliceConfig tunes the slice effect.
type SliceConfig struct {
	// Direction is the axis the cut runs along.
	Direction SliceDirection
	// MovementSpeed is how fast a character travels back to its place. The
	// horizontal cut doubles it, because it crosses the width of the canvas
	// rather than its height.
	MovementSpeed float64
	// MovementEasing shapes the travel. The default holds the halves still,
	// throws them across, and settles them, which is what makes the two sides
	// read as one cut rather than two slides.
	MovementEasing Easing
	// FinalGradientStops colour the text once it lands. They are ignored when
	// the engine is set to resolve to the input's own colours.
	FinalGradientStops     []Color
	FinalGradientSteps     []int
	FinalGradientDirection GradientDirection
}

// DefaultSliceConfig is upstream's default slice.
func DefaultSliceConfig() SliceConfig {
	return SliceConfig{
		Direction:      SliceVertical,
		MovementSpeed:  0.25,
		MovementEasing: InOutExpo,
		FinalGradientStops: []Color{
			MustParseColor("8A008A"), MustParseColor("00D1FF"), MustParseColor("FFFFFF"),
		},
		FinalGradientSteps:     []int{12},
		FinalGradientDirection: Diagonal,
	}
}

// Slice cuts the picture in two, throws each half off the canvas by a
// different edge, and slides both back at once. Every character is placed and
// released in Build, so Advance only has to run frames until the movement
// stops.
//
// This effect assembles the screen rather than passing over it: nothing stands
// on its own coordinate when the first frame is drawn. That holds under every
// colour policy, DynamicExistingColors included. Revealing the whole picture
// up front, which is what a sweep like waves has to do, would leave the slice
// with nothing to bring in.
type Slice struct {
	config SliceConfig
}

// NewSlice builds the effect.
func NewSlice(config SliceConfig) *Slice {
	return &Slice{config: config}
}

// slicePathID is the one path every character gets: from the edge it was
// thrown to back to where it belongs.
const slicePathID = "input_path"

// Build gives every character its final colour, parks it off canvas, and
// starts it on its way home.
func (s *Slice) Build(e *Engine) error {
	finalGradient, err := NewGradient(s.config.FinalGradientStops, s.config.FinalGradientSteps, false)
	if err != nil {
		return err
	}
	canvas := e.Terminal.Canvas
	mapping, err := finalGradient.BuildCoordinateColorMapping(
		canvas.TextBottom, canvas.TextTop, canvas.TextLeft, canvas.TextRight,
		s.config.FinalGradientDirection)
	if err != nil {
		return err
	}
	fallback := finalGradient.Spectrum[0]
	dynamic := e.Terminal.Config.ExistingColorHandling == DynamicExistingColors

	// A slice character never changes appearance, so this is set once and no
	// scene is ever built. ttfx also keeps a map from character to final
	// colour here and then never reads it; over a full screen that is one
	// entry per cell held for the whole run, so it is gone.
	for _, ch := range e.Terminal.GetCharacters(e.Rng, InputOnly(), SortTopToBottomLeftToRight) {
		final := Fg(mapping.At(ch.InputCoord, fallback))
		if dynamic {
			// The character carried its own colours in, background included,
			// so it lands wearing them: it arrives as the piece of the screen
			// it always was.
			final = ch.Animation.InputColors
		}
		ch.Animation.SetAppearance(ch.InputSymbol, final, ch.UsesInputColors)
	}

	// The horizontal cut crosses the canvas the long way, so upstream doubles
	// the speed to keep it taking about as long as the other two. ttfx writes
	// this back into its own config; here it is a local, so building the same
	// effect twice does not double it twice.
	speed := s.config.MovementSpeed

	switch s.config.Direction {
	case SliceHorizontal:
		speed *= 2
		if err := s.buildHorizontal(e, speed); err != nil {
			return err
		}
	case SliceDiagonal:
		if err := s.buildDiagonal(e, speed); err != nil {
			return err
		}
	default:
		if err := s.buildVertical(e, speed); err != nil {
			return err
		}
	}

	for _, ch := range e.ActiveCharacters() {
		e.Terminal.SetCharacterVisibility(ch, true)
	}
	return nil
}

// sendTo parks a character on an origin off the canvas and starts it moving
// back to its own coordinate.
func (s *Slice) sendTo(e *Engine, ch *Character, origin Coord, speed float64) error {
	ch.Motion.SetCoordinate(origin)
	path, err := ch.Motion.NewPath(slicePathID, PathOptions{
		Speed:   speed,
		Ease:    s.config.MovementEasing,
		HasEase: true,
	})
	if err != nil {
		return err
	}
	if _, err := path.NewWaypoint(ch.InputCoord, nil, ""); err != nil {
		return err
	}
	e.ActivatePath(ch, path.ID)
	return nil
}

// buildVertical cuts down the middle column. The left half of each row is
// thrown above the canvas; the right half of the row's opposite number,
// counting from the other end, is thrown below it. Pairing a row with its
// opposite is what makes the two halves shear past each other instead of
// falling straight back.
func (s *Slice) buildVertical(e *Engine, speed float64) error {
	canvas := e.Terminal.Canvas
	rows := e.Terminal.GetCharactersGrouped(InputOnly(), GroupRowBottomToTop)
	for rowIndex := range rows {
		for _, ch := range rows[rowIndex] {
			if ch.InputCoord.Column > canvas.TextCenterColumn {
				continue
			}
			if err := s.sendTo(e, ch, C(ch.InputCoord.Column, canvas.Top+1), speed); err != nil {
				return err
			}
			e.Activate(ch)
		}
		opposite := rows[len(rows)-(rowIndex+1)]
		for _, ch := range opposite {
			if ch.InputCoord.Column <= canvas.TextCenterColumn {
				continue
			}
			if err := s.sendTo(e, ch, C(ch.InputCoord.Column, canvas.Bottom-1), speed); err != nil {
				return err
			}
			e.Activate(ch)
		}
	}
	return nil
}

// buildHorizontal cuts across the middle row. This is the one direction that
// takes the fill characters as well as the input, so the whole rectangle the
// text occupies moves rather than only the glyphs in it. Anything outside that
// rectangle is dropped, which is why the columns are trimmed before use.
func (s *Slice) buildHorizontal(e *Engine, speed float64) error {
	canvas := e.Terminal.Canvas
	filter := CharacterFilter{Input: true, InnerFill: true, OuterFill: true}
	var columns [][]*Character
	for _, column := range e.Terminal.GetCharactersGrouped(filter, GroupColumnRightToLeft) {
		var trimmed []*Character
		for _, ch := range column {
			if canvas.CoordIsInText(ch.InputCoord) {
				trimmed = append(trimmed, ch)
			}
		}
		if len(trimmed) > 0 {
			columns = append(columns, trimmed)
		}
	}

	midPoint := canvas.TextCenterRow
	for columnIndex := range columns {
		for _, ch := range columns[columnIndex] {
			if ch.InputCoord.Row > midPoint {
				continue
			}
			if err := s.sendTo(e, ch, C(canvas.Left-1, ch.InputCoord.Row), speed); err != nil {
				return err
			}
			e.Activate(ch)
		}
		opposite := columns[len(columns)-(columnIndex+1)]
		for _, ch := range opposite {
			if ch.InputCoord.Row <= midPoint {
				continue
			}
			if err := s.sendTo(e, ch, C(canvas.Right+1, ch.InputCoord.Row), speed); err != nil {
				return err
			}
			e.Activate(ch)
		}
	}
	return nil
}

// buildDiagonal cuts along the diagonals. The first half of them are stacked
// on one point below the canvas and the second half on one point above it, so
// each group slides back out of a single cell rather than travelling as a
// line. The two halves are released alternately, one from each side per turn.
func (s *Slice) buildDiagonal(e *Engine, speed float64) error {
	canvas := e.Terminal.Canvas
	diagonals := e.Terminal.GetCharactersGrouped(InputOnly(), GroupDiagonalBottomLeftToTopRight)
	half := len(diagonals) / 2
	left, right := diagonals[:half], diagonals[half:]

	for len(left) > 0 || len(right) > 0 {
		if len(left) > 0 {
			group := left[0]
			left = left[1:]
			// The lowest character of the group is its first, so the group
			// leaves by the column that character stands in.
			origin := C(group[0].InputCoord.Column, canvas.Bottom-1)
			for _, ch := range group {
				if err := s.sendTo(e, ch, origin, speed); err != nil {
					return err
				}
				e.Activate(ch)
			}
		}
		if len(right) > 0 {
			group := right[0]
			right = right[1:]
			// And the highest character is its last, so a group leaving over
			// the top uses that one's column instead.
			origin := C(group[len(group)-1].InputCoord.Column, canvas.Top+1)
			for _, ch := range group {
				if err := s.sendTo(e, ch, origin, speed); err != nil {
					return err
				}
				e.Activate(ch)
			}
		}
	}
	return nil
}

// Advance runs one frame and reports whether the effect is still going.
func (s *Slice) Advance(e *Engine) bool {
	if e.ActiveCount() == 0 {
		return false
	}
	e.Update()
	return true
}
