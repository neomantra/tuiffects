package tuiffects

// randomsequence, ported from ttfx src/effects/random_sequence.rs, which
// ports TerminalTextEffects effects/effect_random_sequence.py by ChrisBuilds.

func init() {
	Register(Descriptor{
		Name:        "randomsequence",
		Description: "Characters fade in one at a time in a random order until the text is whole",
		New:         func() Effect { return NewRandomSequence(DefaultRandomSequenceConfig()) },
	})
}

// RandomSequenceConfig tunes the randomsequence effect.
type RandomSequenceConfig struct {
	// Speed is the share of the text revealed per frame, so 0.007 reveals
	// seven characters per thousand. It is turned into a whole number of
	// characters once, at build time, and never drops below one.
	Speed float64
	// FinalGradientStops colour the text as it fades in. They are ignored
	// when the engine is set to resolve to the input's own colours.
	FinalGradientStops []Color
	FinalGradientSteps []int
	// FinalGradientFrames is how many frames each step of a character's
	// fade-in holds. Raise it to make each character arrive more slowly.
	FinalGradientFrames    int
	FinalGradientDirection GradientDirection
}

// DefaultRandomSequenceConfig is upstream's default randomsequence.
func DefaultRandomSequenceConfig() RandomSequenceConfig {
	return RandomSequenceConfig{
		Speed: 0.007,
		FinalGradientStops: []Color{
			MustParseColor("8A008A"), MustParseColor("00D1FF"), MustParseColor("FFFFFF"),
		},
		FinalGradientSteps:     []int{12},
		FinalGradientFrames:    8,
		FinalGradientDirection: Vertical,
	}
}

// randomSequenceFadeSteps is upstream's step count for the ramp that carries
// one character from the terminal background up to its final colour.
const randomSequenceFadeSteps = 7

// randomSequenceBackground is the colour every fade-in starts from.
//
// ttfx reads this from terminal_background_color, a terminal config field this
// engine does not have. The value here is ttfx's own default, so the effect
// matches upstream for every host that never changed it, which is every host
// here.
var randomSequenceBackground = MustParseColor("000000")

// RandomSequence hides the whole text, then fades characters back in one at a
// time in a random order. Nothing moves and nothing waits on anything else:
// each character runs its own short ramp from the terminal background to its
// final colour as soon as its turn comes up.
type RandomSequence struct {
	config RandomSequenceConfig

	// pending is the reveal order, shuffled, and read from the back.
	pending []*Character
	// charactersPerTick is how many characters are released each frame.
	charactersPerTick int
}

// NewRandomSequence builds the effect.
func NewRandomSequence(config RandomSequenceConfig) *RandomSequence {
	return &RandomSequence{config: config, charactersPerTick: 1}
}

// Build hides every character, gives it a fade-in scene, and shuffles the
// order they will be released in.
func (r *RandomSequence) Build(e *Engine) error {
	r.charactersPerTick = int(r.config.Speed * float64(len(e.Terminal.InputCharacters)))
	if r.charactersPerTick < 1 {
		r.charactersPerTick = 1
	}

	finalGradient, err := NewGradient(r.config.FinalGradientStops, r.config.FinalGradientSteps, false)
	if err != nil {
		return err
	}
	canvas := e.Terminal.Canvas
	mapping, err := finalGradient.BuildCoordinateColorMapping(
		canvas.TextBottom, canvas.TextTop, canvas.TextLeft, canvas.TextRight,
		r.config.FinalGradientDirection)
	if err != nil {
		return err
	}
	fallback := finalGradient.Spectrum[0]
	dynamic := e.Terminal.Config.ExistingColorHandling == DynamicExistingColors

	characters := e.Terminal.GetCharacters(e.Rng, InputOnly(), SortTopToBottomLeftToRight)
	r.pending = make([]*Character, 0, len(characters))
	for _, ch := range characters {
		final := Fg(mapping.At(ch.InputCoord, fallback))
		if dynamic {
			// May be the empty pair, for a character the input gave no colour
			// of its own. The scene below has a branch for that.
			final = ch.Animation.InputColors
		}

		// The picture is assembled here rather than passed over, so every
		// character stays hidden until its turn. This is the opposite of a
		// sweep like waves, which has to show the whole screen from the first
		// frame under DynamicExistingColors.
		e.Terminal.SetCharacterVisibility(ch, false)

		scene := ch.Animation.NewScene("", SceneOptions{
			UsesInputColors: ch.UsesInputColors,
			Frames:          randomSequenceFadeSteps + 1,
		})
		symbols := []string{ch.InputSymbol}
		frames := r.config.FinalGradientFrames

		switch {
		case final.HasFg || final.HasBg:
			// A background is carried through its own ramp, so a character
			// that arrived sitting on a filled panel fades up to that panel
			// rather than punching a hole in it.
			var fgGradient, bgGradient *Gradient
			if final.HasFg {
				if fgGradient, err = NewGradientSteps(
					[]Color{randomSequenceBackground, final.Fg}, randomSequenceFadeSteps, false); err != nil {
					return err
				}
			}
			if final.HasBg {
				if bgGradient, err = NewGradientSteps(
					[]Color{randomSequenceBackground, final.Bg}, randomSequenceFadeSteps, false); err != nil {
					return err
				}
			}
			if err := scene.ApplyGradientToSymbols(symbols, frames, fgGradient, bgGradient); err != nil {
				return err
			}
		default:
			// Only reachable under DynamicExistingColors, for a character with
			// no input colour. It fades up through grey and then drops to the
			// terminal's own default, which is what it arrived wearing.
			neutral, err := NewGradientSteps(
				[]Color{randomSequenceBackground, DynamicNeutralGrey},
				randomSequenceFadeSteps, false)
			if err != nil {
				return err
			}
			if err := scene.ApplyGradientToSymbols(symbols, frames, neutral, nil); err != nil {
				return err
			}
			if err := scene.AddFrame(ch.InputSymbol, frames, VisualParams{}); err != nil {
				return err
			}
		}

		e.ActivateScene(ch, scene.ID)
		r.pending = append(r.pending, ch)
	}
	Shuffle(e.Rng, r.pending)
	return nil
}

// Advance releases the next few characters and reports whether the effect is
// still going.
func (r *RandomSequence) Advance(e *Engine) bool {
	if len(r.pending) == 0 && e.ActiveCount() == 0 {
		return false
	}
	for i := 0; i < r.charactersPerTick && len(r.pending) > 0; i++ {
		next := r.pending[len(r.pending)-1]
		r.pending = r.pending[:len(r.pending)-1]
		e.Terminal.SetCharacterVisibility(next, true)
		e.Activate(next)
	}
	e.Update()
	return true
}
