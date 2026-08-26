package tuiffects

// crumble, ported from ttfx src/effects/crumble.rs, which ports
// TerminalTextEffects effects/effect_crumble.py by ChrisBuilds.

func init() {
	Register(Descriptor{
		Name:        "crumble",
		Description: "The text weakens, crumbles into dust on the floor, is vacuumed off the top, then reforms",
		New:         func() Effect { return NewCrumble(DefaultCrumbleConfig()) },
	})
}

// CrumbleConfig tunes the crumble effect.
type CrumbleConfig struct {
	// FinalGradientStops colour the text once it reforms. They are ignored
	// when the engine is set to resolve to the input's own colours.
	FinalGradientStops     []Color
	FinalGradientSteps     []int
	FinalGradientDirection GradientDirection
}

// DefaultCrumbleConfig is upstream's default crumble.
func DefaultCrumbleConfig() CrumbleConfig {
	return CrumbleConfig{
		FinalGradientStops:     []Color{MustParseColor("5CE1FF"), MustParseColor("FF8C00")},
		FinalGradientSteps:     []int{12},
		FinalGradientDirection: Diagonal,
	}
}

// crumbleStage is which of the four passes the effect is in.
type crumbleStage int

const (
	// crumbleFalling weakens characters in growing groups and drops each one
	// to the floor of the canvas.
	crumbleFalling crumbleStage = iota
	// crumbleVacuuming lifts the dust off the top of the canvas, a handful of
	// characters at a time.
	crumbleVacuuming
	// crumbleResetting flies every character back to where it came from and
	// flashes it white as it lands.
	crumbleResetting
	// crumbleComplete is the end.
	crumbleComplete
)

// crumbleNeutralGray is what a character with no colours of its own is dimmed
// from under DynamicExistingColors. Upstream calls it DYNAMIC_NEUTRAL_GRAY.
// Without it such a character would have no colour to weaken towards and would
// crumble at full strength.
var crumbleNeutralGray = MustParseColor("808080")

// Crumble breaks the text up and puts it back. Every character dims, turns to
// dust, falls to the bottom of the canvas, gets sucked out through the top,
// and then flies home and flashes white as it re-forms.
//
// This effect passes over the screen rather than assembling it, so every
// character is visible from the first frame, wearing a dimmed version of the
// colour it will settle back to. That is upstream's own behaviour here, not a
// deviation: crumble starts from a picture that is already on screen, which is
// exactly what DynamicExistingColors wants.
type Crumble struct {
	config CrumbleConfig

	// pendingChars are the characters that have not started to fall yet, in a
	// shuffled order, and unvacuumedChars are the ones still lying on the
	// floor once everything has fallen.
	pendingChars    []*Character
	unvacuumedChars []*Character

	// fallDelay counts frames down to the next group of falling characters.
	// The group grows and the delay shrinks as the effect runs, so the
	// collapse starts as a trickle and ends as a landslide.
	fallDelay        int
	maxFallDelay     int
	minFallDelay     int
	fallGroupMaxSize int

	reset bool
	stage crumbleStage
}

// NewCrumble builds the effect.
func NewCrumble(config CrumbleConfig) *Crumble {
	return &Crumble{config: config}
}

// Build gives every character its four passes: the dimmed scene it starts in,
// the fall to the floor with a dust animation synced to it, the lift out
// through the top, and the flight home with the white flash that follows.
func (c *Crumble) Build(e *Engine) error {
	white := MustParseColor("ffffff")

	gradient, err := NewGradient(c.config.FinalGradientStops, c.config.FinalGradientSteps, false)
	if err != nil {
		return err
	}
	canvas := e.Terminal.Canvas
	mapping, err := gradient.BuildCoordinateColorMapping(
		canvas.TextBottom, canvas.TextTop, canvas.TextLeft, canvas.TextRight,
		c.config.FinalGradientDirection)
	if err != nil {
		return err
	}
	fallback := gradient.Spectrum[0]
	dynamic := e.Terminal.Config.ExistingColorHandling == DynamicExistingColors

	characters := e.Terminal.GetCharacters(e.Rng, InputOnly(), SortTopToBottomLeftToRight)
	for _, ch := range characters {
		// ttfx resolves the final colours into a map of their own in a pass
		// before this loop, then reads each one back inside it. It reads the
		// same per character, so it is read here instead.
		finalColor := mapping.At(ch.InputCoord, fallback)
		input := ch.Animation.InputColors

		// The four colours and four gradients the character animates through.
		// Each channel is optional: under the dynamic policy a character that
		// arrived with a background but no foreground animates its background
		// alone, and one that arrived with neither is treated as neutral grey.
		var weak, dust ColorPair
		var flashFg, flashBg, strengthenFg, strengthenBg *Gradient
		if dynamic {
			if input.HasFg {
				weak.Fg, weak.HasFg = AdjustColorBrightness(input.Fg, 0.65), true
				dust.Fg, dust.HasFg = AdjustColorBrightness(input.Fg, 0.55), true
			} else if !input.HasBg {
				weak.Fg, weak.HasFg = AdjustColorBrightness(crumbleNeutralGray, 0.65), true
				dust.Fg, dust.HasFg = AdjustColorBrightness(crumbleNeutralGray, 0.55), true
			}
			// The background is carried through every stage. On a captured
			// screen the backgrounds are the selection bars and the filled
			// panels, and an effect that dimmed the foreground alone would
			// blank all of them for the length of the run.
			if input.HasBg {
				weak.Bg, weak.HasBg = AdjustColorBrightness(input.Bg, 0.65), true
				dust.Bg, dust.HasBg = AdjustColorBrightness(input.Bg, 0.55), true
			}
			switch {
			case input.HasFg:
				if flashFg, err = NewGradientSteps([]Color{input.Fg, white}, 6, false); err != nil {
					return err
				}
				if strengthenFg, err = NewGradientSteps([]Color{white, input.Fg}, 9, false); err != nil {
					return err
				}
			case !input.HasBg:
				if flashFg, err = NewGradientSteps([]Color{crumbleNeutralGray, white}, 6, false); err != nil {
					return err
				}
			}
			if input.HasBg {
				if flashBg, err = NewGradientSteps([]Color{input.Bg, white}, 6, false); err != nil {
					return err
				}
				if strengthenBg, err = NewGradientSteps([]Color{white, input.Bg}, 9, false); err != nil {
					return err
				}
			}
		} else {
			weak = Fg(AdjustColorBrightness(finalColor, 0.65))
			dust = Fg(AdjustColorBrightness(finalColor, 0.55))
			if flashFg, err = NewGradientSteps([]Color{finalColor, white}, 6, false); err != nil {
				return err
			}
			if strengthenFg, err = NewGradientSteps([]Color{white, finalColor}, 9, false); err != nil {
				return err
			}
		}

		var weakenFg, weakenBg *Gradient
		if weak.HasFg && dust.HasFg {
			if weakenFg, err = NewGradientSteps([]Color{weak.Fg, dust.Fg}, 9, false); err != nil {
				return err
			}
		}
		if weak.HasBg && dust.HasBg {
			if weakenBg, err = NewGradientSteps([]Color{weak.Bg, dust.Bg}, 9, false); err != nil {
				return err
			}
		}

		e.Terminal.SetCharacterVisibility(ch, true)

		// The dimmed scene the character sits in until its turn comes.
		initial := ch.Animation.NewScene("", SceneOptions{UsesInputColors: ch.UsesInputColors, Frames: 1})
		if err := initial.AddFrame(ch.InputSymbol, 1, VisualParams{Colors: weak}); err != nil {
			return err
		}
		e.ActivateScene(ch, initial.ID)

		// The fall. OutBounce makes the character land, rebound and settle.
		fall, err := ch.Motion.NewPath("", PathOptions{Speed: 0.65, Ease: OutBounce, HasEase: true})
		if err != nil {
			return err
		}
		if _, err := fall.NewWaypoint(C(ch.InputCoord.Column, canvas.Bottom), nil, ""); err != nil {
			return err
		}

		weaken := ch.Animation.NewScene("weaken", SceneOptions{
			UsesInputColors: ch.UsesInputColors,
			Frames:          crumbleGradientFrames(weakenFg, weakenBg),
		})
		if err := weaken.ApplyGradientToSymbols(
			[]string{ch.InputSymbol}, 4, weakenFg, weakenBg); err != nil {
			return err
		}

		// The lift out through the top, bent through the middle of the canvas
		// so the dust converges on its way up.
		top, err := ch.Motion.NewPath("top", PathOptions{Speed: 1, Ease: OutQuint, HasEase: true})
		if err != nil {
			return err
		}
		if _, err := top.NewWaypoint(
			C(ch.InputCoord.Column, canvas.Top), []Coord{canvas.Center}, ""); err != nil {
			return err
		}

		// The flight home.
		home, err := ch.Motion.NewPath("input", PathOptions{Speed: 1})
		if err != nil {
			return err
		}
		if _, err := home.NewWaypoint(ch.InputCoord, nil, ""); err != nil {
			return err
		}

		flash := ch.Animation.NewScene("", SceneOptions{
			UsesInputColors: ch.UsesInputColors,
			Frames:          crumbleGradientFrames(flashFg, flashBg),
		})
		if err := flash.ApplyGradientToSymbols(
			[]string{ch.InputSymbol}, 4, flashFg, flashBg); err != nil {
			return err
		}

		strengthen := ch.Animation.NewScene("", SceneOptions{
			UsesInputColors: ch.UsesInputColors,
			Frames:          crumbleGradientFrames(strengthenFg, strengthenBg),
		})
		if strengthenFg == nil && strengthenBg == nil {
			// Only the dynamic branch reaches this: the character arrived
			// with no colours of its own, so there is nothing to ramp back to
			// and one plain frame drops it to the terminal's own colours.
			if err := strengthen.AddFrame(ch.InputSymbol, 4, VisualParams{}); err != nil {
				return err
			}
		} else if err := strengthen.ApplyGradientToSymbols(
			[]string{ch.InputSymbol}, 4, strengthenFg, strengthenBg); err != nil {
			return err
		}

		// The dust is synced to distance rather than to ticks, so the symbol
		// changes with how far the character has fallen and not with how long
		// it has been falling.
		dustScene := ch.Animation.NewScene("", SceneOptions{
			UsesInputColors: ch.UsesInputColors,
			Sync:            SyncDistance,
			Frames:          5,
		})
		dustSymbols := []string{"*", ".", ","}
		for i := 0; i < 5; i++ {
			symbol := Choice(e.Rng, dustSymbols)
			if err := dustScene.AddFrame(*symbol, 1, VisualParams{Colors: dust}); err != nil {
				return err
			}
		}

		// Once a character has finished weakening it drops, moves in front of
		// everything still standing, and turns to dust on the way down.
		ch.RegisterEvent(SceneComplete, SceneCaller("weaken"), ActivatePath(fall.ID))
		ch.RegisterEvent(SceneComplete, SceneCaller("weaken"), SetLayer(1))
		ch.RegisterEvent(SceneComplete, SceneCaller("weaken"), ActivateScene(dustScene.ID))

		// Landing back home flashes the character white, then ramps it to the
		// colour it keeps.
		ch.RegisterEvent(PathComplete, PathCaller("input"), ActivateScene(flash.ID))
		ch.RegisterEvent(SceneComplete, SceneCaller(flash.ID), ActivateScene(strengthen.ID))

		c.pendingChars = append(c.pendingChars, ch)
	}
	Shuffle(e.Rng, c.pendingChars)

	c.fallDelay = 12
	c.maxFallDelay = 12
	c.minFallDelay = 9
	c.reset = false
	c.fallGroupMaxSize = 1
	c.stage = crumbleFalling
	c.unvacuumedChars = append([]*Character(nil), e.Terminal.InputCharacters...)
	Shuffle(e.Rng, c.unvacuumedChars)
	return nil
}

// crumbleGradientFrames is how many frames ApplyGradientToSymbols will add for
// a single symbol: one per colour of the longer gradient. It is only a
// capacity hint, so a nil pair of gradients still reports one frame.
func crumbleGradientFrames(fg, bg *Gradient) int {
	frames := 1
	if fg != nil && len(fg.Spectrum) > frames {
		frames = len(fg.Spectrum)
	}
	if bg != nil && len(bg.Spectrum) > frames {
		frames = len(bg.Spectrum)
	}
	return frames
}

// Advance runs one frame of whichever stage the effect is in and reports
// whether it is still going.
func (c *Crumble) Advance(e *Engine) bool {
	if c.stage == crumbleComplete {
		return false
	}
	switch c.stage {
	case crumbleFalling:
		if len(c.pendingChars) > 0 {
			if c.fallDelay == 0 {
				groupSize := e.Rng.IntBetween(1, c.fallGroupMaxSize)
				for i := 0; i < groupSize && len(c.pendingChars) > 0; i++ {
					next := c.pendingChars[0]
					c.pendingChars = c.pendingChars[1:]
					e.ActivateScene(next, "weaken")
					e.Activate(next)
				}
				c.fallDelay = e.Rng.IntBetween(c.minFallDelay, c.maxFallDelay)
				// Six times in ten the collapse speeds up: the next group is
				// one character bigger and both ends of the delay range come
				// down by a frame.
				if e.Rng.IntBetween(1, 10) > 4 {
					c.fallGroupMaxSize++
					c.minFallDelay = max(0, c.minFallDelay-1)
					c.maxFallDelay = max(0, c.maxFallDelay-1)
				}
			} else {
				c.fallDelay--
			}
		}
		if len(c.pendingChars) == 0 && e.ActiveCount() == 0 {
			c.stage = crumbleVacuuming
		}
	case crumbleVacuuming:
		if len(c.unvacuumedChars) > 0 {
			lifted := e.Rng.IntBetween(3, 10)
			for i := 0; i < lifted && len(c.unvacuumedChars) > 0; i++ {
				next := c.unvacuumedChars[0]
				c.unvacuumedChars = c.unvacuumedChars[1:]
				e.ActivatePath(next, "top")
				e.Activate(next)
			}
		}
		if e.ActiveCount() == 0 {
			c.stage = crumbleResetting
		}
	case crumbleResetting:
		if !c.reset {
			for _, ch := range e.Terminal.GetCharacters(e.Rng, InputOnly(), SortTopToBottomLeftToRight) {
				e.ActivatePath(ch, "input")
				e.Activate(ch)
			}
			c.reset = true
		}
		if e.ActiveCount() == 0 {
			c.stage = crumbleComplete
		}
	}
	e.Update()
	return true
}
