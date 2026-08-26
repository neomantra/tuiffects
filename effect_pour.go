package tuiffects

// pour, ported from ttfx src/effects/pour.rs, which ports
// TerminalTextEffects effects/effect_pour.py by ChrisBuilds.

func init() {
	Register(Descriptor{
		Name:        "pour",
		Description: "Characters pour in from one edge of the screen, a row at a time, and settle into place",
		New:         func() Effect { return NewPour(DefaultPourConfig()) },
	})
}

// PourDirection is the way the text pours: the edge it falls towards.
type PourDirection int

// The four pour directions.
const (
	// PourDown fills the bottom row first, from characters entering at the
	// top of the canvas.
	PourDown PourDirection = iota
	// PourUp fills the top row first, from characters entering at the bottom.
	PourUp
	// PourLeft fills the left column first, from characters entering at the
	// right edge.
	PourLeft
	// PourRight fills the right column first, from characters entering at the
	// left edge.
	PourRight
)

// PourConfig tunes the pour effect.
type PourConfig struct {
	// Direction is the edge the text pours towards.
	Direction PourDirection
	// PourSpeed is how many characters are released per tick. Raise it to
	// pour faster.
	PourSpeed int
	// MovementSpeedLow and MovementSpeedHigh bound each character's travel
	// speed.
	MovementSpeedLow  float64
	MovementSpeedHigh float64
	// Gap is how many frames to wait between releases.
	Gap int
	// StartingColor is the colour a character wears while it is falling,
	// before its closing ramp begins.
	StartingColor Color
	// FinalGradientStops colour the text once it lands. They are ignored when
	// the engine is set to resolve to the input's own colours.
	FinalGradientStops     []Color
	FinalGradientSteps     []int
	FinalGradientFrames    int
	FinalGradientDirection GradientDirection
	// MovementEasing shapes the travel. The default accelerates towards the
	// resting place.
	MovementEasing Easing
}

// DefaultPourConfig is upstream's default pour.
func DefaultPourConfig() PourConfig {
	return PourConfig{
		Direction:         PourDown,
		PourSpeed:         2,
		MovementSpeedLow:  0.4,
		MovementSpeedHigh: 0.6,
		Gap:               1,
		StartingColor:     MustParseColor("ffffff"),
		FinalGradientStops: []Color{
			MustParseColor("8A008A"), MustParseColor("00D1FF"), MustParseColor("FFFFFF"),
		},
		FinalGradientSteps:     []int{12},
		FinalGradientFrames:    6,
		FinalGradientDirection: Vertical,
		MovementEasing:         InQuad,
	}
}

// Pour empties the text into the canvas one row or column at a time. Every
// character starts off the far edge, travels to where it belongs, and ramps
// from the starting colour to its final one on the way.
//
// This effect assembles the screen rather than passing over it, so every
// character stays hidden until it is released. That holds under every colour
// policy, including DynamicExistingColors: showing the picture up front would
// leave nothing for the pour to fill in.
type Pour struct {
	config PourConfig

	// pendingGroups are the rows or columns still to pour, in order, and
	// currentGroup is the one being released now. Every other group is
	// reversed, so the stream alternates end to end instead of restarting at
	// the same side each time.
	pendingGroups [][]*Character
	currentGroup  []*Character
	gap           int
}

// NewPour builds the effect.
func NewPour(config PourConfig) *Pour {
	return &Pour{config: config}
}

// Build parks every character off the edge it pours from, gives it a path
// home, and gives it a scene that ramps to its final colour.
func (p *Pour) Build(e *Engine) error {
	gradient, err := NewGradient(p.config.FinalGradientStops, p.config.FinalGradientSteps, false)
	if err != nil {
		return err
	}
	canvas := e.Terminal.Canvas
	mapping, err := gradient.BuildCoordinateColorMapping(
		canvas.TextBottom, canvas.TextTop, canvas.TextLeft, canvas.TextRight,
		p.config.FinalGradientDirection)
	if err != nil {
		return err
	}
	fallback := gradient.Spectrum[0]
	dynamic := e.Terminal.Config.ExistingColorHandling == DynamicExistingColors

	var grouping CharacterGroup
	switch p.config.Direction {
	case PourDown:
		grouping = GroupRowBottomToTop
	case PourUp:
		grouping = GroupRowTopToBottom
	case PourLeft:
		grouping = GroupColumnLeftToRight
	case PourRight:
		grouping = GroupColumnRightToLeft
	}

	groups := e.Terminal.GetCharactersGrouped(InputOnly(), grouping)
	for i, group := range groups {
		for _, ch := range group {
			e.Terminal.SetCharacterVisibility(ch, false)

			// The final colour. ttfx resolves these into a map in a pass of
			// its own before this loop; it reads the same per character, so
			// it is read here instead.
			final := Fg(mapping.At(ch.InputCoord, fallback))
			if dynamic {
				// May be the empty pair, for a character the input gave no
				// colour of its own. The scene below has a branch for that.
				final = ch.Animation.InputColors
			}

			var start Coord
			switch p.config.Direction {
			case PourDown:
				start = C(ch.InputCoord.Column, canvas.Top)
			case PourUp:
				start = C(ch.InputCoord.Column, canvas.Bottom)
			case PourLeft:
				start = C(canvas.Right, ch.InputCoord.Row)
			case PourRight:
				start = C(canvas.Left, ch.InputCoord.Row)
			}
			ch.Motion.SetCoordinate(start)

			speed := e.Rng.Uniform(p.config.MovementSpeedLow, p.config.MovementSpeedHigh)
			path, err := ch.Motion.NewPath("", PathOptions{
				Speed: speed, Ease: p.config.MovementEasing, HasEase: true,
			})
			if err != nil {
				return err
			}
			if _, err := path.NewWaypoint(ch.InputCoord, nil, ""); err != nil {
				return err
			}
			e.ActivatePath(ch, path.ID)

			// The gradients come before the scene so the frame count is
			// known and the scene can size its slices once.
			var fgGradient, bgGradient *Gradient
			if dynamic {
				// Both channels ramp, so a cell that arrived with a
				// background keeps it. An effect that ramped only the
				// foreground would blank every filled panel on a captured
				// screen for as long as it ran.
				if final.HasFg {
					if fgGradient, err = NewGradientSteps(
						[]Color{p.config.StartingColor, final.Fg}, 10, false); err != nil {
						return err
					}
				}
				// The background is held rather than ramped. Upstream ramps
				// it from StartingColor, which is white, exactly as it ramps
				// the foreground; over piped text nothing carries a
				// background so that never fires. Over a captured screen it
				// lands every filled bar and panel white and cools it into
				// its own colour over sixty frames, which reads as a flash.
				// The white-to-colour signature stays on the foreground,
				// which is what it is for.
				if final.HasBg {
					if bgGradient, err = NewGradientSteps(
						[]Color{final.Bg, final.Bg}, 1, false); err != nil {
						return err
					}
				}
			} else if fgGradient, err = NewGradient(
				[]Color{p.config.StartingColor, final.Fg}, p.config.FinalGradientSteps, false); err != nil {
				return err
			}

			frames := 1
			if fgGradient != nil {
				frames = len(fgGradient.Spectrum)
			}
			if bgGradient != nil && len(bgGradient.Spectrum) > frames {
				frames = len(bgGradient.Spectrum)
			}
			pour := ch.Animation.NewScene("", SceneOptions{
				UsesInputColors: ch.UsesInputColors,
				Frames:          frames,
			})
			if fgGradient == nil && bgGradient == nil {
				// The character arrived with no colours of its own, so there
				// is nothing to resolve back to and one plain frame is the
				// whole scene. Only the dynamic branch can reach this.
				if err := pour.AddFrame(
					ch.InputSymbol, p.config.FinalGradientFrames, VisualParams{}); err != nil {
					return err
				}
			} else if err := pour.ApplyGradientToSymbols(
				[]string{ch.InputSymbol}, p.config.FinalGradientFrames, fgGradient, bgGradient); err != nil {
				return err
			}
			e.ActivateScene(ch, pour.ID)
		}

		if i%2 == 0 {
			p.pendingGroups = append(p.pendingGroups, group)
		} else {
			reversed := make([]*Character, len(group))
			for j, ch := range group {
				reversed[len(group)-1-j] = ch
			}
			p.pendingGroups = append(p.pendingGroups, reversed)
		}
	}

	p.gap = 0
	if len(p.pendingGroups) > 0 {
		// ttfx pops this unconditionally and panics on an empty canvas. There
		// is nothing to pour in that case, so Advance reports the effect over
		// on its first call instead.
		p.currentGroup = p.pendingGroups[0]
		p.pendingGroups = p.pendingGroups[1:]
	}
	return nil
}

// Advance releases the next few characters and runs one frame. It reports
// whether the effect is still going.
func (p *Pour) Advance(e *Engine) bool {
	if len(p.pendingGroups) == 0 && len(p.currentGroup) == 0 && e.ActiveCount() == 0 {
		return false
	}
	if len(p.currentGroup) == 0 && len(p.pendingGroups) > 0 {
		p.currentGroup = p.pendingGroups[0]
		p.pendingGroups = p.pendingGroups[1:]
	}
	if len(p.currentGroup) > 0 {
		if p.gap == 0 {
			for i := 0; i < p.config.PourSpeed && len(p.currentGroup) > 0; i++ {
				next := p.currentGroup[0]
				p.currentGroup = p.currentGroup[1:]
				e.Terminal.SetCharacterVisibility(next, true)
				e.Activate(next)
			}
			p.gap = p.config.Gap
		} else {
			p.gap--
		}
	}
	e.Update()
	return true
}
