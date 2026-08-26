package tuiffects

// expand, ported from ttfx src/effects/expand.rs, which ports
// TerminalTextEffects effects/effect_expand.py by ChrisBuilds.

func init() {
	Register(Descriptor{
		Name:        "expand",
		Description: "Characters start stacked in the middle of the screen and move out to their places",
		New:         func() Effect { return NewExpand(DefaultExpandConfig()) },
	})
}

// ExpandConfig tunes the expand effect.
type ExpandConfig struct {
	// ExpandEasing shapes the travel from the middle of the canvas outwards.
	// The default starts and ends slowly, so the picture swells rather than
	// bursts.
	ExpandEasing Easing
	// MovementSpeed is how fast a character travels to where it belongs.
	MovementSpeed float64
	// FinalGradientStops colour the text once it is in place. They are
	// ignored when the engine is set to resolve to the input's own colours.
	FinalGradientStops     []Color
	FinalGradientSteps     []int
	FinalGradientDirection GradientDirection
}

// DefaultExpandConfig is upstream's default expand.
func DefaultExpandConfig() ExpandConfig {
	return ExpandConfig{
		ExpandEasing:  InOutQuart,
		MovementSpeed: 0.35,
		FinalGradientStops: []Color{
			MustParseColor("8A008A"), MustParseColor("00D1FF"), MustParseColor("FFFFFF"),
		},
		FinalGradientSteps:     []int{12},
		FinalGradientDirection: Vertical,
	}
}

// expandRampSteps is how many colour steps a character wears on the way out.
// Upstream picks 10 and nothing derives it.
const expandRampSteps = 10

// expandRampFrames is how long each of those steps holds. The scene is synced
// to distance, so this only decides how many frames the scene reports itself
// as having; the picture comes from how far the character has travelled.
const expandRampFrames = 5

// expandDynamicRampFrames is the same hold under DynamicExistingColors, where
// upstream uses a shorter one because that branch may carry two gradients.
const expandDynamicRampFrames = 1

// Expand stacks every character on the middle of the canvas and moves it out
// to where it belongs, colouring it as it goes.
//
// This effect ASSEMBLES the screen rather than passing over it, so it does not
// show the picture in place on the first frame. Upstream makes every character
// visible during the build, all of them piled on the centre cell, and the
// picture only exists once they have travelled. Under DynamicExistingColors
// that is the right behaviour unchanged: the screen collapses to a point and
// grows back into itself.
type Expand struct {
	config ExpandConfig
}

// NewExpand builds the effect.
func NewExpand(config ExpandConfig) *Expand {
	return &Expand{config: config}
}

// Build puts every character on the centre of the canvas and gives it the path
// out and the colour ramp it wears on the way.
func (x *Expand) Build(e *Engine) error {
	gradient, err := NewGradient(x.config.FinalGradientStops, x.config.FinalGradientSteps, false)
	if err != nil {
		return err
	}
	canvas := e.Terminal.Canvas
	mapping, err := gradient.BuildCoordinateColorMapping(
		canvas.TextBottom, canvas.TextTop, canvas.TextLeft, canvas.TextRight,
		x.config.FinalGradientDirection)
	if err != nil {
		return err
	}
	fallback := gradient.Spectrum[0]
	// Every character leaves the centre wearing the first colour of the
	// gradient, so the pile at the centre is one solid colour and each ramp
	// starts from the colour the character is already showing.
	rampStart := gradient.Spectrum[0]
	dynamic := e.Terminal.Config.ExistingColorHandling == DynamicExistingColors

	for _, ch := range e.Terminal.GetCharacters(e.Rng, InputOnly(), SortTopToBottomLeftToRight) {
		useInput := dynamic
		final := Fg(mapping.At(ch.InputCoord, fallback))
		if useInput {
			final = ch.Animation.InputColors
		}

		ch.Motion.SetCoordinate(canvas.Center)
		path, err := ch.Motion.NewPath("", PathOptions{
			Speed:   x.config.MovementSpeed,
			Ease:    x.config.ExpandEasing,
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

		e.Terminal.SetCharacterVisibility(ch, true)
		e.Activate(ch)
		e.ActivatePath(ch, path.ID)

		// The ramp is synced to how far the character still has to travel, so
		// a character arrives at its final colour exactly as it arrives at
		// its cell.
		ramp := ch.Animation.NewScene("", SceneOptions{
			Sync:            SyncDistance,
			UsesInputColors: ch.UsesInputColors,
			Frames:          expandRampSteps + 1,
		})
		symbols := []string{ch.InputSymbol}
		if useInput {
			// Both halves of the input colour are ramped, not just the
			// foreground, so a cell that arrived with a background keeps it.
			var fgGradient, bgGradient *Gradient
			if final.HasFg {
				if fgGradient, err = NewGradientSteps(
					[]Color{rampStart, final.Fg}, expandRampSteps, false); err != nil {
					return err
				}
			}
			if final.HasBg {
				if bgGradient, err = NewGradientSteps(
					[]Color{rampStart, final.Bg}, expandRampSteps, false); err != nil {
					return err
				}
			}
			if fgGradient == nil && bgGradient == nil {
				// The character carried no colour of its own, so there is
				// nothing to ramp to: it arrives as the terminal default,
				// which is how it left.
				if err := ramp.AddFrame(
					ch.InputSymbol, expandDynamicRampFrames, VisualParams{}); err != nil {
					return err
				}
			} else if err := ramp.ApplyGradientToSymbols(
				symbols, expandDynamicRampFrames, fgGradient, bgGradient); err != nil {
				return err
			}
		} else {
			charGradient, err := NewGradientSteps([]Color{rampStart, final.Fg}, expandRampSteps, false)
			if err != nil {
				return err
			}
			if err := ramp.ApplyGradientToSymbols(
				symbols, expandRampFrames, charGradient, nil); err != nil {
				return err
			}
		}
		e.ActivateScene(ch, ramp.ID)
	}
	return nil
}

// Advance runs one frame and reports whether the effect is still going. Every
// character is released during the build, so there is nothing to release here.
func (x *Expand) Advance(e *Engine) bool {
	if e.ActiveCount() == 0 {
		return false
	}
	e.Update()
	return true
}
