package tuiffects

import "sort"

// bouncyballs, ported from ttfx src/effects/bouncyballs.rs, which ports
// TerminalTextEffects effects/effect_bouncyballs.py by ChrisBuilds.

func init() {
	Register(Descriptor{
		Name:        "bouncyballs",
		Description: "Balls drop from above the screen, bounce where each character belongs, and settle into it",
		New:         func() Effect { return NewBouncyBalls(DefaultBouncyBallsConfig()) },
	})
}

// BouncyBallsConfig tunes the bouncyballs effect.
type BouncyBallsConfig struct {
	// BallColors are picked at random, one per ball.
	BallColors []Color
	// BallSymbols are the glyphs a ball can wear on the way down.
	BallSymbols []string
	// BallDelay is how many frames pass between one group of balls being
	// dropped and the next. Raise it to drop fewer balls at once.
	BallDelay int
	// MovementSpeed is how fast a ball falls.
	MovementSpeed float64
	// MovementEasing shapes the fall. The default bounces on landing, which
	// is what the effect is named for.
	MovementEasing Easing
	// FinalGradientStops colour the text once a ball has landed. They are
	// ignored when the engine is set to resolve to the input's own colours.
	FinalGradientStops     []Color
	FinalGradientSteps     []int
	FinalGradientDirection GradientDirection
}

// DefaultBouncyBallsConfig is upstream's default bouncyballs.
func DefaultBouncyBallsConfig() BouncyBallsConfig {
	return BouncyBallsConfig{
		BallColors: []Color{
			MustParseColor("d1f4a5"), MustParseColor("96e2a4"), MustParseColor("5acda9"),
		},
		BallSymbols:    []string{"*", "o", "O", "0", "."},
		BallDelay:      4,
		MovementSpeed:  0.45,
		MovementEasing: OutBounce,
		FinalGradientStops: []Color{
			MustParseColor("f8ffae"), MustParseColor("43c6ac"),
		},
		FinalGradientSteps:     []int{12},
		FinalGradientDirection: Diagonal,
	}
}

// BouncyBalls drops every character in as a ball from somewhere above the
// canvas, bounces it where the character belongs, and fades the ball into the
// character once it has come to rest.
//
// It assembles the screen rather than passing over it, so a character stays
// hidden until its ball is dropped. That holds under every colour policy,
// including DynamicExistingColors: a character shown before its ball landed
// would be the picture arriving without the animation that puts it there.
type BouncyBalls struct {
	config BouncyBallsConfig

	pending []*Character
	// rowOrder is the row keys still to release, ascending, and rowGroups the
	// characters in each. Row 1 is the bottom of the canvas, so the screen
	// fills from the bottom up.
	rowOrder  []int
	rowGroups map[int][]*Character
	// ballDelay counts down the frames left before the next group is dropped.
	ballDelay int
}

// NewBouncyBalls builds the effect.
func NewBouncyBalls(config BouncyBallsConfig) *BouncyBalls {
	return &BouncyBalls{config: config, rowGroups: map[int][]*Character{}}
}

// Build gives every character a ball, a fall path, and a landing fade.
func (b *BouncyBalls) Build(e *Engine) error {
	gradient, err := NewGradient(b.config.FinalGradientStops, b.config.FinalGradientSteps, false)
	if err != nil {
		return err
	}
	canvas := e.Terminal.Canvas
	mapping, err := gradient.BuildCoordinateColorMapping(
		canvas.TextBottom, canvas.TextTop, canvas.TextLeft, canvas.TextRight,
		b.config.FinalGradientDirection)
	if err != nil {
		return err
	}
	fallback := gradient.Spectrum[0]
	dynamic := e.Terminal.Config.ExistingColorHandling == DynamicExistingColors

	characters := e.Terminal.GetCharacters(e.Rng, InputOnly(), SortTopToBottomLeftToRight)
	for _, ch := range characters {
		final := Fg(mapping.At(ch.InputCoord, fallback))
		if dynamic {
			// A character that carried no colour of its own resolves to an
			// empty pair, which is the colourless branch below: it settles as
			// the terminal default, which is how it arrived.
			final = ch.Animation.InputColors
		}
		ballColor := *Choice(e.Rng, b.config.BallColors)
		ballSymbol := *Choice(e.Rng, b.config.BallSymbols)

		ball := ch.Animation.NewScene("", SceneOptions{UsesInputColors: ch.UsesInputColors, Frames: 1})
		if err := ball.AddFrame(ballSymbol, 1, VisualParams{Colors: Fg(ballColor)}); err != nil {
			return err
		}

		// The ball fades into the character it was standing in for. Both
		// channels ramp from the ball's own colour, which is upstream's, so a
		// cell that carried a background picks it back up over the same six
		// frames the foreground takes.
		settle := ch.Animation.NewScene("", SceneOptions{UsesInputColors: ch.UsesInputColors, Frames: 12})
		var fgGradient, bgGradient *Gradient
		if final.HasFg {
			if fgGradient, err = NewGradientSteps([]Color{ballColor, final.Fg}, 10, false); err != nil {
				return err
			}
		}
		if final.HasBg {
			if bgGradient, err = NewGradientSteps([]Color{ballColor, final.Bg}, 10, false); err != nil {
				return err
			}
		}
		if fgGradient == nil && bgGradient == nil {
			if err := settle.AddFrame(ch.InputSymbol, 6, VisualParams{}); err != nil {
				return err
			}
		} else if err := settle.ApplyGradientToSymbols([]string{ch.InputSymbol}, 6, fgGradient, bgGradient); err != nil {
			return err
		}

		// Start the ball somewhere in the top half above the canvas, in the
		// character's own column, so the balls do not all fall from one line.
		// Upstream truncates this row rather than rounding it.
		dropRow := int(float64(canvas.Top) * e.Rng.Uniform(1.0, 1.5))
		ch.Motion.SetCoordinate(C(ch.InputCoord.Column, dropRow))
		path, err := ch.Motion.NewPath("", PathOptions{
			Speed: b.config.MovementSpeed, Ease: b.config.MovementEasing, HasEase: true,
		})
		if err != nil {
			return err
		}
		if _, err := path.NewWaypoint(ch.InputCoord, nil, ""); err != nil {
			return err
		}
		e.ActivatePath(ch, path.ID)
		e.ActivateScene(ch, ball.ID)
		ch.RegisterEvent(PathComplete, PathCaller(path.ID), ActivateScene(settle.ID))

		b.rowGroups[ch.InputCoord.Row] = append(b.rowGroups[ch.InputCoord.Row], ch)
	}
	for row := range b.rowGroups {
		b.rowOrder = append(b.rowOrder, row)
	}
	sort.Ints(b.rowOrder)
	b.ballDelay = 0
	return nil
}

// Advance runs one frame and reports whether the effect is still going.
func (b *BouncyBalls) Advance(e *Engine) bool {
	if len(b.rowOrder) == 0 && e.ActiveCount() == 0 && len(b.pending) == 0 {
		return false
	}
	if len(b.pending) == 0 && len(b.rowOrder) > 0 {
		row := b.rowOrder[0]
		b.rowOrder = b.rowOrder[1:]
		b.pending = append(b.pending, b.rowGroups[row]...)
		delete(b.rowGroups, row)
	}
	if len(b.pending) > 0 {
		if b.ballDelay == 0 {
			// Two to six balls at a time, taken from anywhere in the row, so a
			// row lands scattered rather than left to right.
			for i, n := 0, e.Rng.IntBetween(2, 6); i < n && len(b.pending) > 0; i++ {
				index := e.Rng.IndexBelow(len(b.pending))
				next := b.pending[index]
				b.pending = append(b.pending[:index], b.pending[index+1:]...)
				e.Terminal.SetCharacterVisibility(next, true)
				e.Activate(next)
			}
			b.ballDelay = b.config.BallDelay
		} else {
			b.ballDelay--
		}
	}
	e.Update()
	return true
}
