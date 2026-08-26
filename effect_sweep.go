package tuiffects

// sweep, ported from ttfx src/effects/sweep.rs, which ports
// TerminalTextEffects effects/effect_sweep.py by ChrisBuilds.

func init() {
	Register(Descriptor{
		Name:        "sweep",
		Description: "Two bands cross the canvas, the first uncovering the characters in grey and the second colouring them",
		New:         func() Effect { return NewSweep(DefaultSweepConfig()) },
		// The sweep runs over every cell of the canvas, not only over the
		// input, so the empty cells have to hold characters before the effect
		// is built. Upstream always makes them, so no ttfx effect declares
		// this; without it here both bands query an empty set of fill
		// characters and the canvas around the text never shimmers.
		NeedsFillCharacters: true,
	})
}

// SweepConfig tunes the sweep effect.
type SweepConfig struct {
	// SweepSymbols are the glyphs a cell cycles through while a band is on it.
	// The default fades from a solid block down to a light one, which is what
	// makes the band read as a shimmer rather than a wipe.
	SweepSymbols []string
	// FirstSweepDirection is the axis the first band travels along. That band
	// uncovers the characters in grey.
	FirstSweepDirection CharacterGroup
	// SecondSweepDirection is the axis the second band travels along. That
	// band colours the characters.
	SecondSweepDirection CharacterGroup
	// FinalGradientStops colour the text once both bands have passed. They are
	// ignored when the engine is set to resolve to the input's own colours.
	FinalGradientStops     []Color
	FinalGradientSteps     []int
	FinalGradientDirection GradientDirection
}

// DefaultSweepConfig is upstream's default sweep.
func DefaultSweepConfig() SweepConfig {
	return SweepConfig{
		SweepSymbols:         []string{"█", "▓", "▒", "░"},
		FirstSweepDirection:  GroupColumnRightToLeft,
		SecondSweepDirection: GroupColumnLeftToRight,
		FinalGradientStops: []Color{
			MustParseColor("8A008A"), MustParseColor("00D1FF"), MustParseColor("ffffff"),
		},
		FinalGradientSteps:     []int{8},
		FinalGradientDirection: Vertical,
	}
}

// sweepShadesOfGray are the colours the first band shimmers in. They are fixed
// upstream rather than configurable, because the first band's job is to show
// the shape of the text without giving away its colour.
var sweepShadesOfGray = []Color{
	MustParseColor("A0A0A0"), MustParseColor("808080"), MustParseColor("404040"),
	MustParseColor("202020"), MustParseColor("101010"),
}

// sweepMidGray is the colour a character rests in between the two bands.
var sweepMidGray = MustParseColor("808080")

// Sweep runs two bands across the canvas. The first uncovers every cell in
// grey, the second passes back the other way and leaves each cell in its final
// colour. Nothing moves: a cell wears the band for a moment as it goes by.
//
// This effect assembles the screen rather than passing over it. The first band
// is the reveal: every character starts hidden and is only shown when the band
// reaches it. So under DynamicExistingColors the characters stay hidden at
// build time, unlike waves, which passes over a picture that is already there.
type Sweep struct {
	config            SweepConfig
	easer             *sweepEaser
	groupsSecondSweep [][]*Character
	firstPhase        bool
	complete          bool
}

// NewSweep builds the effect.
func NewSweep(config SweepConfig) *Sweep {
	return &Sweep{config: config, firstPhase: true}
}

// Build gives every character on the canvas a grey scene for the first band
// and a colouring scene for the second, then groups the characters into the
// bands each sweep travels through.
func (s *Sweep) Build(e *Engine) error {
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

	// The second band's palette. Under the default colour handling it is the
	// final gradient; under DynamicExistingColors it is every colour the input
	// actually carried, so the band shimmers in the picture's own colours
	// before settling into them.
	palette := finalGradient.Spectrum
	if dynamic {
		var fromInput []Color
		for _, ch := range e.Terminal.GetCharacters(e.Rng, InputOnly(), SortTopToBottomLeftToRight) {
			if ch.Animation.InputColors.HasFg {
				fromInput = append(fromInput, ch.Animation.InputColors.Fg)
			}
			if ch.Animation.InputColors.HasBg {
				fromInput = append(fromInput, ch.Animation.InputColors.Bg)
			}
		}
		if len(fromInput) > 0 {
			palette = fromInput
		}
	}

	fillsFilter := CharacterFilter{Input: true, InnerFill: true, OuterFill: true}
	for _, ch := range e.Terminal.GetCharacters(e.Rng, fillsFilter, SortTopToBottomLeftToRight) {
		// The colour the character is left in once the second band has gone
		// by. A fill character has no text to colour, so it goes out: black
		// under the default handling, and nothing at all under the dynamic one
		// so the cell resolves back to bare canvas.
		var final ColorPair
		switch {
		case !ch.IsFill && dynamic:
			final = ch.Animation.InputColors
		case !ch.IsFill:
			final = Fg(mapping.At(ch.InputCoord, fallback))
		case dynamic:
			final = ColorPair{}
		default:
			final = Fg(MustParseColor("000000"))
		}

		// Every frame of both bands carries the background the cell arrived
		// with. Upstream sets a foreground and nothing else, which is right
		// for piped text and wrong over a captured screen: only the very last
		// frame of the second band puts the background back, so a selection
		// bar or a filled panel is a hole in the picture for the whole run,
		// and then snaps on in one frame. A background is only ever present
		// here under DynamicExistingColors, so the default path is exactly
		// upstream's.
		carry := func(fg Color) ColorPair {
			pair := Fg(fg)
			if dynamic && ch.Animation.InputColors.HasBg {
				pair.Bg, pair.HasBg = ch.Animation.InputColors.Bg, true
			}
			return pair
		}

		initial := ch.Animation.NewScene("initial_sweep", SceneOptions{
			UsesInputColors: ch.UsesInputColors,
			Frames:          len(s.config.SweepSymbols) + 1,
		})
		for _, symbol := range s.config.SweepSymbols {
			shade := Choice(e.Rng, sweepShadesOfGray)
			if err := initial.AddFrame(symbol, 5, VisualParams{Colors: carry(*shade)}); err != nil {
				return err
			}
		}
		if err := initial.AddFrame(
			ch.InputSymbol, 1, VisualParams{Colors: carry(sweepMidGray)}); err != nil {
			return err
		}

		second := ch.Animation.NewScene("second_sweep", SceneOptions{
			UsesInputColors: ch.UsesInputColors,
			Frames:          len(s.config.SweepSymbols) + 1,
		})
		for _, symbol := range s.config.SweepSymbols {
			colour := Choice(e.Rng, palette)
			if err := second.AddFrame(symbol, 5, VisualParams{Colors: carry(*colour)}); err != nil {
				return err
			}
		}
		if err := second.AddFrame(ch.InputSymbol, 1, VisualParams{Colors: final}); err != nil {
			return err
		}
	}

	groupsFirstSweep := e.Terminal.GetCharactersGrouped(fillsFilter, s.config.FirstSweepDirection)
	s.easer = newSweepEaser(groupsFirstSweep, InOutCirc, 100)
	s.groupsSecondSweep = e.Terminal.GetCharactersGrouped(fillsFilter, s.config.SecondSweepDirection)
	return nil
}

// Advance releases whichever bands the easing curve has reached and reports
// whether the effect is still going. When the first band runs out the easer is
// pointed at the second sweep's groups and reset, which is what turns one pass
// into two.
func (s *Sweep) Advance(e *Engine) bool {
	if e.ActiveCount() == 0 && s.complete {
		return false
	}
	for _, group := range s.easer.step() {
		for _, ch := range group {
			if s.firstPhase {
				// The first band is the reveal, so it is the only one that
				// makes a character visible.
				e.Terminal.SetCharacterVisibility(ch, true)
				e.ActivateScene(ch, "initial_sweep")
			} else {
				e.ActivateScene(ch, "second_sweep")
			}
			e.Activate(ch)
		}
	}
	if s.easer.isComplete() {
		if s.firstPhase {
			s.easer.sequence = s.groupsSecondSweep
			s.easer.reset()
			s.firstPhase = false
		} else {
			s.complete = true
		}
	}
	e.Update()
	return true
}

// sweepEaser is ttfx's SequenceEaser with its EasingTracker folded in, narrowed
// to what sweep needs: walk an eased fraction along the character groups, hand
// back whichever groups that fraction has just reached, and be resettable onto
// a second sequence.
//
// It is local to this file because the engine has no shared version of it yet.
// Two other effects here carry their own copy for the same reason.
type sweepEaser struct {
	sequence   [][]*Character
	easing     Easing
	totalSteps int

	currentStep int
	easedValue  float64
}

func newSweepEaser(sequence [][]*Character, easing Easing, totalSteps int) *sweepEaser {
	return &sweepEaser{sequence: sequence, easing: easing, totalSteps: totalSteps}
}

// isComplete reports whether the curve has taken all of its steps. The last
// groups it released are still animating for a while afterwards, which is why
// Advance also waits on the active count.
func (s *sweepEaser) isComplete() bool { return s.currentStep >= s.totalSteps }

// reset puts the curve back to the start. The sequence is left alone, so a
// caller can swap it first and run the same curve over new groups.
func (s *sweepEaser) reset() {
	s.currentStep = 0
	s.easedValue = 0
}

// step advances the curve one tick and returns the groups it released.
//
// Upstream also reports groups a curve moved back past, so a bouncing curve can
// take them away again. Sweep ignores that, and its curve is InOutCirc, which
// is fixed upstream and only ever moves forwards, so the backward case is left
// out rather than written and never run.
func (s *sweepEaser) step() [][]*Character {
	previous := s.easedValue
	if s.currentStep < s.totalSteps {
		s.currentStep++
		eased := s.easing.Ease(float64(s.currentStep) / float64(s.totalSteps))
		// Upstream clamps the eased value rather than the position derived
		// from it.
		if eased > 1 {
			eased = 1
		}
		if eased < 0 {
			eased = 0
		}
		s.easedValue = eased
	}
	if len(s.sequence) == 0 {
		return nil
	}
	// Truncation towards zero, which is upstream's int().
	length := int(s.easedValue * float64(len(s.sequence)))
	previousLength := int(previous * float64(len(s.sequence)))
	if length <= previousLength {
		return nil
	}
	return s.sequence[previousLength:length]
}
