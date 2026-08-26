package tuiffects

// scattered, ported from ttfx src/effects/scattered.rs, which ports
// TerminalTextEffects effects/effect_scattered.py by ChrisBuilds.

func init() {
	Register(Descriptor{
		Name:        "scattered",
		Description: "Characters start in random places and gather into the text",
		New:         func() Effect { return NewScattered(DefaultScatteredConfig()) },
	})
}

// ScatteredConfig tunes the scattered effect.
type ScatteredConfig struct {
	// MovementSpeed is how fast a character travels to where it belongs.
	MovementSpeed float64
	// MovementEasing shapes that travel. The default overshoots at both ends,
	// which is what makes the gathering read as a snap rather than a drift.
	MovementEasing Easing
	// FinalGradientStops colour the text once it is in place. They are
	// ignored when the engine is set to resolve to the input's own colours.
	FinalGradientStops     []Color
	FinalGradientSteps     []int
	FinalGradientDirection GradientDirection
	// FinalGradientFrames is how many frames each colour step holds.
	FinalGradientFrames int
}

// DefaultScatteredConfig is upstream's default scattered.
func DefaultScatteredConfig() ScatteredConfig {
	return ScatteredConfig{
		MovementSpeed:  0.5,
		MovementEasing: InOutBack,
		FinalGradientStops: []Color{
			MustParseColor("ff9048"), MustParseColor("ab9dff"), MustParseColor("bdffea"),
		},
		FinalGradientSteps:     []int{12},
		FinalGradientDirection: Vertical,
		FinalGradientFrames:    9,
	}
}

// scatteredHoldFrames is how long the scattered picture sits still before the
// characters start moving. Upstream picks 25 and nothing derives it.
const scatteredHoldFrames = 25

// Scattered drops every character at a random spot on the canvas, holds that
// still for a moment, then walks each one to where it belongs.
//
// This effect ASSEMBLES the screen rather than passing over it, so it does not
// show the picture in place on the first frame. Upstream already makes every
// character visible during the build, because a character has to be seen at
// its scattered start for the gathering to read at all. Under
// DynamicExistingColors that is the right behaviour unchanged: the screen
// starts as the same picture shuffled, and reassembles into itself.
type Scattered struct {
	config ScatteredConfig

	holdFrames int
}

// NewScattered builds the effect.
func NewScattered(config ScatteredConfig) *Scattered {
	return &Scattered{config: config}
}

// Build scatters every character across the canvas and gives it the path home
// and the colour ramp it wears on the way.
func (s *Scattered) Build(e *Engine) error {
	gradient, err := NewGradient(s.config.FinalGradientStops, s.config.FinalGradientSteps, false)
	if err != nil {
		return err
	}
	canvas := e.Terminal.Canvas
	mapping, err := gradient.BuildCoordinateColorMapping(
		canvas.TextBottom, canvas.TextTop, canvas.TextLeft, canvas.TextRight,
		s.config.FinalGradientDirection)
	if err != nil {
		return err
	}
	fallback := gradient.Spectrum[0]
	rampStart := gradient.Spectrum[0]
	dynamic := e.Terminal.Config.ExistingColorHandling == DynamicExistingColors

	for _, ch := range e.Terminal.GetCharacters(e.Rng, InputOnly(), SortTopToBottomLeftToRight) {
		useInput := dynamic
		final := Fg(mapping.At(ch.InputCoord, fallback))
		if useInput {
			final = ch.Animation.InputColors
		}

		// A canvas too small to scatter across has one place to put things.
		start := C(1, 1)
		if canvas.Right >= 2 && canvas.Top >= 2 {
			start = canvas.RandomCoord(e.Rng, false, false)
		}
		ch.Motion.SetCoordinate(start)

		path, err := ch.Motion.NewPath("", PathOptions{
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
		// A character in flight is drawn over one that has landed, so the
		// crossings read as movement rather than as gaps.
		ch.RegisterEvent(PathActivated, PathCaller(path.ID), SetLayer(1))
		ch.RegisterEvent(PathComplete, PathCaller(path.ID), SetLayer(0))
		e.ActivatePath(ch, path.ID)
		e.Terminal.SetCharacterVisibility(ch, true)

		// The ramp is synced to how far the character still has to travel, so
		// a character lands exactly as it reaches its final colour.
		ramp := ch.Animation.NewScene("", SceneOptions{
			Sync:            SyncDistance,
			UsesInputColors: ch.UsesInputColors,
			Frames:          10,
		})
		if useInput {
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
		e.Activate(ch)
	}

	s.holdFrames = scatteredHoldFrames
	return nil
}

// Advance runs one frame and reports whether the effect is still going. The
// first frames of a run hold the scattered picture still without stepping
// anything, which is what gives the eye time to see it is scattered.
func (s *Scattered) Advance(e *Engine) bool {
	if e.ActiveCount() == 0 {
		return false
	}
	if s.holdFrames != 0 {
		s.holdFrames--
		return true
	}
	e.Update()
	return true
}
