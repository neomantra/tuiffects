package tuiffects

// slide, ported from ttfx src/effects/slide.rs, which ports
// TerminalTextEffects effects/effect_slide.py by ChrisBuilds.

func init() {
	Register(Descriptor{
		Name:        "slide",
		Description: "Characters slide in from off screen, a row at a time, and settle into place",
		New:         func() Effect { return NewSlide(DefaultSlideConfig()) },
	})
}

// SlideGrouping is the axis the characters travel along.
type SlideGrouping int

// The three groupings upstream offers.
const (
	// SlideByRow slides each row in sideways.
	SlideByRow SlideGrouping = iota
	// SlideByColumn slides each column in vertically.
	SlideByColumn
	// SlideByDiagonal slides each diagonal in along itself.
	SlideByDiagonal
)

var slideGroupingNames = map[string]SlideGrouping{
	"row":      SlideByRow,
	"column":   SlideByColumn,
	"diagonal": SlideByDiagonal,
}

// ParseSlideGrouping looks a grouping up by its upstream name.
func ParseSlideGrouping(name string) (SlideGrouping, bool) {
	g, ok := slideGroupingNames[name]
	return g, ok
}

// SlideConfig tunes the slide effect.
type SlideConfig struct {
	// MovementSpeed is how fast a character travels to its place. Raise it to
	// make the slide snappier.
	MovementSpeed float64
	// Grouping is the axis the characters travel along.
	Grouping SlideGrouping
	// Gap is how many frames to wait before releasing the next group.
	Gap int
	// ReverseDirection sends the groups in from the other side. It is ignored
	// when Merge is set, because Merge already uses both sides.
	ReverseDirection bool
	// Merge sends every other group in from the opposite side, so the groups
	// meet in the middle.
	Merge bool
	// MovementEasing shapes the travel. The default eases in and out, which is
	// what makes a group look like it is being pushed rather than dropped.
	MovementEasing Easing
	// FinalGradientStops colour the text once it lands. They are ignored when
	// the engine is set to resolve to the input's own colours.
	FinalGradientStops     []Color
	FinalGradientSteps     []int
	FinalGradientDirection GradientDirection
	// FinalGradientFrames is how many frames each step of a character's
	// colour ramp holds.
	FinalGradientFrames int
}

// DefaultSlideConfig is upstream's default slide.
func DefaultSlideConfig() SlideConfig {
	return SlideConfig{
		MovementSpeed:    0.8,
		Grouping:         SlideByRow,
		Gap:              2,
		ReverseDirection: false,
		Merge:            false,
		MovementEasing:   InOutQuad,
		FinalGradientStops: []Color{
			MustParseColor("833ab4"), MustParseColor("fd1d1d"), MustParseColor("fcb045"),
		},
		FinalGradientSteps:     []int{12},
		FinalGradientDirection: Vertical,
		FinalGradientFrames:    6,
	}
}

// Slide parks every character just off the edge of the canvas and pushes the
// groups on one at a time, each group feeding its characters in one per frame.
//
// This effect assembles the screen rather than passing over it, so a character
// stays hidden until its turn comes. That holds under every colour policy,
// DynamicExistingColors included: revealing the whole picture up front would
// leave nothing for the slide to bring in.
type Slide struct {
	config SlideConfig

	// pendingGroups are the groups still to be released, in order, and
	// activeGroups the ones already released and still feeding characters in.
	pendingGroups [][]*Character
	activeGroups  [][]*Character
	currentGap    int
}

// NewSlide builds the effect.
func NewSlide(config SlideConfig) *Slide {
	return &Slide{config: config}
}

// slidePathID is the one path every character gets: from wherever it was
// parked to where it belongs.
const slidePathID = "input_path"

// Build parks every character off canvas, gives it the path back to its own
// coordinate, and the colour ramp it wears once it is moving.
func (s *Slide) Build(e *Engine) error {
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
	rampStart := s.config.FinalGradientStops[0]
	dynamic := e.Terminal.Config.ExistingColorHandling == DynamicExistingColors

	var grouping CharacterGroup
	switch s.config.Grouping {
	case SlideByColumn:
		grouping = GroupColumnLeftToRight
	case SlideByDiagonal:
		grouping = GroupDiagonalTopLeftToBottomRight
	default:
		grouping = GroupRowTopToBottom
	}
	groups := e.Terminal.GetCharactersGrouped(InputOnly(), grouping)

	for _, group := range groups {
		for _, ch := range group {
			path, err := ch.Motion.NewPath(slidePathID, PathOptions{
				Speed:   s.config.MovementSpeed,
				Ease:    s.config.MovementEasing,
				HasEase: true,
			})
			if err != nil {
				return err
			}
			if _, err := path.NewWaypoint(ch.InputCoord, nil, ""); err != nil {
				return err
			}
		}
	}

	for groupIndex := range groups {
		// ttfx keeps a copy of the group taken before any reversal, because
		// the endpoints the diagonal case measures from are the ends of the
		// group as it was grouped, not as it was reordered for release.
		original := append([]*Character(nil), groups[groupIndex]...)

		switch s.config.Grouping {
		case SlideByRow:
			var startingColumn int
			if s.config.Merge && groupIndex%2 == 0 {
				startingColumn = canvas.Right + 1
			} else {
				// Reversed so the character with the furthest to travel is
				// released first and the rest queue up behind it.
				reverseCharacters(groups[groupIndex])
				startingColumn = canvas.Left - 1
			}
			if s.config.ReverseDirection && !s.config.Merge {
				reverseCharacters(groups[groupIndex])
				startingColumn = canvas.Right + 1
			}
			for _, ch := range groups[groupIndex] {
				ch.Motion.SetCoordinate(C(startingColumn, ch.InputCoord.Row))
			}

		case SlideByColumn:
			var startingRow int
			if s.config.Merge && groupIndex%2 == 0 {
				startingRow = canvas.Bottom - 1
			} else {
				reverseCharacters(groups[groupIndex])
				startingRow = canvas.Top + 1
			}
			if s.config.ReverseDirection && !s.config.Merge {
				reverseCharacters(groups[groupIndex])
				startingRow = canvas.Bottom - 1
			}
			for _, ch := range groups[groupIndex] {
				ch.Motion.SetCoordinate(C(ch.InputCoord.Column, startingRow))
			}

		case SlideByDiagonal:
			// A diagonal group is stacked on one point off the corner and
			// slides along its own diagonal, so the whole group shares a
			// starting coordinate. The default runs the diagonal out past the
			// bottom of the canvas from its top end; merge and reverse run it
			// out past the top from its bottom end.
			last := original[len(original)-1].InputCoord
			outsideBottom := last.Row - (canvas.Bottom - 1)
			startingCoord := C(last.Column-outsideBottom, last.Row-outsideBottom)
			if s.config.Merge && groupIndex%2 == 0 {
				reverseCharacters(groups[groupIndex])
				first := original[0].InputCoord
				outside := (canvas.Top + 1) - first.Row
				startingCoord = C(first.Column+outside, first.Row+outside)
			}
			if s.config.ReverseDirection && !s.config.Merge {
				reverseCharacters(groups[groupIndex])
				first := original[0].InputCoord
				outside := (canvas.Top + 1) - first.Row
				startingCoord = C(first.Column+outside, first.Row+outside)
			}
			for _, ch := range groups[groupIndex] {
				ch.Motion.SetCoordinate(startingCoord)
			}
		}

		for _, ch := range original {
			// ttfx keeps a map from character to final colour, filled in a
			// pass of its own before the groups are walked. The colour depends
			// on nothing but the character, so it is worked out here instead
			// and the map is gone. Over a full screen that map is one entry
			// per cell held for the whole run.
			final := Fg(mapping.At(ch.InputCoord, fallback))
			if dynamic {
				// The character carried its own colours in, background
				// included, so it lands wearing them and there is no ramp to
				// run: it arrives as the piece of the screen it always was.
				// A character the input gave no colour resolves to the empty
				// pair and lands as the terminal default, which is how it
				// arrived.
				final = ch.Animation.InputColors
			}

			ramp := ch.Animation.NewScene("", SceneOptions{
				UsesInputColors: ch.UsesInputColors,
				Frames:          12,
			})
			if dynamic {
				if err := ramp.AddFrame(ch.InputSymbol, s.config.FinalGradientFrames,
					VisualParams{Colors: final}); err != nil {
					return err
				}
			} else {
				charGradient, err := NewGradientSteps([]Color{rampStart, final.Fg}, 10, false)
				if err != nil {
					return err
				}
				if err := ramp.ApplyGradientToSymbols(
					[]string{ch.InputSymbol}, s.config.FinalGradientFrames, charGradient, nil); err != nil {
					return err
				}
			}
			e.ActivateScene(ch, ramp.ID)
		}
	}

	s.pendingGroups = groups
	s.activeGroups = nil
	s.currentGap = 0
	return nil
}

// Advance runs one frame and reports whether the effect is still going.
func (s *Slide) Advance(e *Engine) bool {
	if len(s.pendingGroups) == 0 && e.ActiveCount() == 0 && len(s.activeGroups) == 0 {
		return false
	}
	if len(s.pendingGroups) > 0 {
		if s.currentGap == s.config.Gap {
			s.activeGroups = append(s.activeGroups, s.pendingGroups[0])
			s.pendingGroups = s.pendingGroups[1:]
			s.currentGap = 0
		} else {
			s.currentGap++
		}
	}
	// Every group already released feeds in one more character this frame, so
	// several groups can be sliding at once.
	for i := range s.activeGroups {
		if len(s.activeGroups[i]) == 0 {
			continue
		}
		next := s.activeGroups[i][0]
		s.activeGroups[i] = s.activeGroups[i][1:]
		e.Terminal.SetCharacterVisibility(next, true)
		e.ActivatePath(next, slidePathID)
		e.Activate(next)
	}
	kept := s.activeGroups[:0]
	for _, group := range s.activeGroups {
		if len(group) > 0 {
			kept = append(kept, group)
		}
	}
	s.activeGroups = kept
	e.Update()
	return true
}
