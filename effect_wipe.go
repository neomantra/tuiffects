package tuiffects

// wipe, ported from ttfx src/effects/wipe.rs, which ports
// TerminalTextEffects effects/effect_wipe.py by ChrisBuilds.

func init() {
	Register(Descriptor{
		Name:        "wipe",
		Description: "A line sweeps across the screen and the text appears behind it",
		New:         func() Effect { return NewWipe(DefaultWipeConfig()) },
	})
}

// WipeConfig tunes the wipe effect.
type WipeConfig struct {
	// WipeDirection is the axis the wipe line travels along. The groups it
	// names are released one after another, and each group is one line of the
	// wipe.
	WipeDirection CharacterGroup
	// WipeDelay is how many frames to wait between groups. Zero releases one
	// group per frame.
	WipeDelay int
	// WipeEase shapes how fast the line crosses the screen. It is applied to
	// the position of the line, not to any one character, so an easing that
	// overshoots and comes back takes characters off the screen again on the
	// way back.
	WipeEase Easing
	// FinalGradientStops colour the text once the line has passed. They are
	// ignored when the engine is set to resolve to the input's own colours.
	FinalGradientStops     []Color
	FinalGradientSteps     []int
	FinalGradientDirection GradientDirection
	// FinalGradientFrames is how many frames each step of that colour ramp
	// holds.
	FinalGradientFrames int
}

// DefaultWipeConfig is upstream's default wipe.
func DefaultWipeConfig() WipeConfig {
	return WipeConfig{
		WipeDirection: GroupDiagonalTopLeftToBottomRight,
		WipeDelay:     0,
		WipeEase:      InOutCirc,
		FinalGradientStops: []Color{
			MustParseColor("833ab4"), MustParseColor("fd1d1d"), MustParseColor("fcb045"),
		},
		FinalGradientSteps:     []int{12},
		FinalGradientDirection: Vertical,
		FinalGradientFrames:    3,
	}
}

// Wipe sweeps a line across the screen and shows each character as the line
// reaches it. Nothing moves and nothing is thrown off: the effect assembles
// the picture behind the line, so a character is hidden until its turn comes.
type Wipe struct {
	config WipeConfig
	easer  *wipeEaser
	// wipeDelay counts down the frames left before the next group goes. It
	// starts at the configured delay, so a configured wait applies before the
	// first group as well as between the rest.
	wipeDelay int
}

// NewWipe builds the effect.
func NewWipe(config WipeConfig) *Wipe {
	return &Wipe{config: config, wipeDelay: config.WipeDelay}
}

// Build gives every character the scene it plays when the line reaches it, and
// groups the characters into the lines the wipe travels through.
func (w *Wipe) Build(e *Engine) error {
	groups := e.Terminal.GetCharactersGrouped(InputOnly(), w.config.WipeDirection)
	w.easer = newWipeEaser(groups, w.config.WipeEase, 100)

	finalGradient, err := NewGradient(w.config.FinalGradientStops, w.config.FinalGradientSteps, false)
	if err != nil {
		return err
	}
	canvas := e.Terminal.Canvas
	mapping, err := finalGradient.BuildCoordinateColorMapping(
		canvas.TextBottom, canvas.TextTop, canvas.TextLeft, canvas.TextRight,
		w.config.FinalGradientDirection)
	if err != nil {
		return err
	}
	fallback := finalGradient.Spectrum[0]
	dynamic := e.Terminal.Config.ExistingColorHandling == DynamicExistingColors

	// The dynamic branch holds one colour for the whole scene, so it needs a
	// frame count of its own to match the length of the ramp the other branch
	// plays. Upstream counts it the same way: one frame per gradient step,
	// plus one for the end stop.
	frameCount := 1
	for _, steps := range w.config.FinalGradientSteps {
		frameCount += steps
	}

	for _, ch := range e.Terminal.GetCharacters(e.Rng, InputOnly(), SortTopToBottomLeftToRight) {
		final := Fg(mapping.At(ch.InputCoord, fallback))
		if dynamic {
			// Carry the background through as well as the foreground. A
			// captured screen puts colour in the background of every
			// selection bar and filled panel, and an effect that sets only a
			// foreground blanks all of it for the length of the run.
			final = ch.Animation.InputColors
		}

		scene := ch.Animation.NewScene("wipe", SceneOptions{
			UsesInputColors: ch.UsesInputColors,
			Frames:          frameCount,
		})
		if dynamic {
			for i := 0; i < frameCount; i++ {
				if err := scene.AddFrame(
					ch.InputSymbol, w.config.FinalGradientFrames,
					VisualParams{Colors: final}); err != nil {
					return err
				}
			}
			continue
		}
		// The character ramps from the head of the final gradient to the
		// colour its own coordinate resolves to, which is what gives the
		// wiped-in text its trailing shade.
		wipeGradient, err := NewGradient(
			[]Color{finalGradient.Spectrum[0], final.Fg}, w.config.FinalGradientSteps, false)
		if err != nil {
			return err
		}
		if err := scene.ApplyGradientToSymbols(
			[]string{ch.InputSymbol}, w.config.FinalGradientFrames, wipeGradient, nil); err != nil {
			return err
		}
	}
	return nil
}

// Advance moves the line on by one step and reports whether the effect is
// still going.
func (w *Wipe) Advance(e *Engine) bool {
	if e.ActiveCount() == 0 && w.easer.isComplete() {
		return false
	}
	if w.wipeDelay == 0 {
		added, removed := w.easer.step()
		for _, group := range added {
			for _, ch := range group {
				e.ActivateScene(ch, "wipe")
				e.Terminal.SetCharacterVisibility(ch, true)
				e.Activate(ch)
			}
		}
		for _, group := range removed {
			for _, ch := range group {
				e.DeactivateScene(ch, "")
				ch.Animation.Scene("wipe").reset()
				e.Terminal.SetCharacterVisibility(ch, false)
			}
		}
		w.wipeDelay = w.config.WipeDelay
	} else {
		w.wipeDelay--
	}
	e.Update()
	return true
}

// wipeEaser is ttfx's SequenceEaser, over the wipe's character groups, with
// its EasingTracker folded in. Neither exists in the engine yet and only this
// effect uses them, so they live here rather than in a shared file.
//
// It walks an easing curve over a fixed number of steps and reports which
// groups crossed the head of the sequence since the last step. A curve that
// runs backwards, which several easings do, reports groups as removed.
type wipeEaser struct {
	sequence    [][]*Character
	ease        Easing
	totalSteps  int
	currentStep int
	easedValue  float64
}

func newWipeEaser(sequence [][]*Character, ease Easing, totalSteps int) *wipeEaser {
	return &wipeEaser{sequence: sequence, ease: ease, totalSteps: totalSteps}
}

// step advances the curve one tick and returns the groups that came into the
// swept region and the ones that dropped out of it. At most one of the two is
// ever non-empty.
func (w *wipeEaser) step() (added, removed [][]*Character) {
	previous := w.easedValue
	if w.currentStep < w.totalSteps {
		w.currentStep++
		ratio := float64(w.currentStep) / float64(w.totalSteps)
		value := w.ease.Ease(ratio)
		// Upstream clamps the eased value rather than the position derived
		// from it, so an overshooting easing parks on the last group instead
		// of running off the end of the sequence.
		if value > 1 {
			value = 1
		}
		if value < 0 {
			value = 0
		}
		w.easedValue = value
	}
	count := len(w.sequence)
	if count == 0 {
		return nil, nil
	}
	// Truncation, not rounding: this is Python's int() and ttfx keeps it.
	length := int(w.easedValue * float64(count))
	previousLength := int(previous * float64(count))
	switch {
	case length > previousLength:
		return w.sequence[previousLength:length], nil
	case length < previousLength:
		return nil, w.sequence[length:previousLength]
	}
	return nil, nil
}

// isComplete reports whether the curve has run its full number of steps.
func (w *wipeEaser) isComplete() bool { return w.currentStep >= w.totalSteps }
