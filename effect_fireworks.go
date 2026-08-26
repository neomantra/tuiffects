package tuiffects

// fireworks, ported from ttfx src/effects/fireworks.rs, which ports
// TerminalTextEffects effects/effect_fireworks.py by ChrisBuilds.

func init() {
	Register(Descriptor{
		Name:        "fireworks",
		Description: "Characters launch from the bottom in shells, burst apart, and fall into place",
		New:         func() Effect { return NewFireworks(DefaultFireworksConfig()) },
	})
}

// FireworksConfig tunes the fireworks effect.
type FireworksConfig struct {
	// ExplodeAnywhere lets a shell burst anywhere on the canvas. Left off, a
	// shell bursts at or above the row its first character belongs on, so the
	// bursts stay clear of the settled text.
	ExplodeAnywhere bool
	// FireworkColors are picked at random, one per shell.
	FireworkColors []Color
	// FireworkSymbol is the glyph a character wears while it climbs.
	FireworkSymbol string
	// FireworkVolume is how many characters go into one shell, as a fraction
	// of the total. It is at least one character.
	FireworkVolume float64
	// LaunchDelay is roughly how many frames pass between one shell and the
	// next. Each wait is scaled by a random half to one and a half.
	LaunchDelay int
	// ExplodeDistance is how far a character flies from the burst point, as a
	// fraction of the canvas width. It is capped at fifteen cells.
	ExplodeDistance float64
	// FinalGradientStops colour the text once it has fallen into place. They
	// are ignored when the engine is set to resolve to the input's own
	// colours.
	FinalGradientStops     []Color
	FinalGradientSteps     []int
	FinalGradientDirection GradientDirection
}

// DefaultFireworksConfig is upstream's default fireworks.
func DefaultFireworksConfig() FireworksConfig {
	return FireworksConfig{
		ExplodeAnywhere: false,
		FireworkColors: []Color{
			MustParseColor("88F7E2"), MustParseColor("44D492"), MustParseColor("F5EB67"),
			MustParseColor("FFA15C"), MustParseColor("FA233E"),
		},
		FireworkSymbol:  "o",
		FireworkVolume:  0.05,
		LaunchDelay:     45,
		ExplodeDistance: 0.2,
		FinalGradientStops: []Color{
			MustParseColor("8A008A"), MustParseColor("00D1FF"), MustParseColor("FFFFFF"),
		},
		FinalGradientSteps:     []int{12},
		FinalGradientDirection: Horizontal,
	}
}

// Fireworks packs the characters into shells, launches each shell from the
// bottom of the canvas, bursts it, and lets the pieces fall into the cells
// they belong in.
//
// It assembles the screen rather than passing over it. Every character starts
// off its own cell, at the bottom edge, and is hidden until its shell is
// launched. That holds under DynamicExistingColors too: showing the picture
// from the first frame would leave the shells climbing over the finished
// screen they are supposed to be delivering.
type Fireworks struct {
	config FireworksConfig

	// shells are the launch groups, popped from the end one at a time.
	shells [][]*Character
	// fireworkVolume is how many characters go into one shell.
	fireworkVolume int
	// explodeDistance is the burst radius in cells.
	explodeDistance int
	// finalColors is where each character settles.
	finalColors map[*Character]ColorPair
	// launchDelay counts down the frames left before the next shell goes up.
	launchDelay int
}

// NewFireworks builds the effect.
func NewFireworks(config FireworksConfig) *Fireworks {
	return &Fireworks{config: config, finalColors: map[*Character]ColorPair{}}
}

// Build sizes the shells, gives every character its flight, and dresses it.
func (f *Fireworks) Build(e *Engine) error {
	canvas := e.Terminal.Canvas
	f.fireworkVolume = max(1, roundHalfEven(f.config.FireworkVolume*float64(len(e.Terminal.InputCharacters))))
	f.explodeDistance = min(15, max(1, roundHalfEven(float64(canvas.Right)*f.config.ExplodeDistance)))
	f.launchDelay = 0
	if err := f.prepareWaypoints(e); err != nil {
		return err
	}
	return f.prepareScenes(e)
}

// prepareWaypoints packs the characters into shells and gives each one the
// three-leg flight: up to the burst point, out from it, then home.
func (f *Fireworks) prepareWaypoints(e *Engine) error {
	canvas := e.Terminal.Canvas
	characters := e.Terminal.GetCharacters(e.Rng, InputOnly(), SortTopToBottomLeftToRight)

	// These four are set at each shell boundary and read by every character
	// that follows it, which is how upstream's loop-scoped names behave.
	var shell []*Character
	originX := 0
	originCoord := C(0, 0)
	var burstCoords []Coord

	for _, ch := range characters {
		if len(shell) == f.fireworkVolume || len(shell) == 0 {
			originX = e.Rng.IntBelow(0, canvas.Right)
			// The first boundary files an empty shell. Upstream does the
			// same, and because shells are popped from the end that empty
			// group is launched last and simply costs one frame of delay.
			f.shells = append(f.shells, shell)
			shell = nil
			minRow := canvas.Bottom
			if !f.config.ExplodeAnywhere {
				minRow = ch.InputCoord.Row
			}
			originCoord = C(originX, e.Rng.IntBelow(minRow, canvas.Top+1))
			burstCoords = FindCoordsInCircle(originCoord, f.explodeDistance)
		}

		// The climb. Every character in a shell starts on the same column at
		// the bottom edge and rises to the shared burst point.
		ch.Motion.SetCoordinate(C(originX, canvas.Bottom))
		apex, err := ch.Motion.NewPath("apex_pth", PathOptions{
			Speed: 0.35, Ease: OutExpo, HasEase: true, Layer: 2, HasLayer: true,
		})
		if err != nil {
			return err
		}
		if _, err := apex.NewWaypoint(originCoord, nil, ""); err != nil {
			return err
		}

		// The burst. Each character takes its own point in the ellipse around
		// the burst point, then arcs up over a control point set half the
		// burst radius further along the same ray.
		explodeSpeed := e.Rng.Uniform(0.2, 0.4)
		// ttfx leaves this path auto-numbered. It is named here for the same
		// reason the other two are: the id is only ever used to key an event,
		// and a name reads better than "1". Nothing else changes.
		explode, err := ch.Motion.NewPath("explode_pth", PathOptions{
			Speed: explodeSpeed, Ease: OutCirc, HasEase: true, Layer: 2, HasLayer: true,
		})
		if err != nil {
			return err
		}
		burstCoord := *Choice(e.Rng, burstCoords)
		if _, err := explode.NewWaypoint(burstCoord, nil, ""); err != nil {
			return err
		}
		bloomControl := ExtrapolateAlongRay(originCoord, burstCoord, float64(floorDiv(f.explodeDistance, 2)))
		bloomCoord := C(bloomControl.Column, max(1, bloomControl.Row-7))
		if _, err := explode.NewWaypoint(bloomCoord, []Coord{bloomControl}, ""); err != nil {
			return err
		}

		// The fall home, bowed through a control point on the bottom row so
		// the piece drops before it slides across.
		input, err := ch.Motion.NewPath("input_pth", PathOptions{
			Speed: 0.6, Ease: InOutQuart, HasEase: true, Layer: 2, HasLayer: true,
		})
		if err != nil {
			return err
		}
		if _, err := input.NewWaypoint(ch.InputCoord, []Coord{C(bloomCoord.Column, 1)}, ""); err != nil {
			return err
		}

		ch.RegisterEvent(PathComplete, PathCaller(apex.ID), ActivatePath(explode.ID))
		ch.RegisterEvent(PathComplete, PathCaller(explode.ID), ActivatePath(input.ID))
		// The flight runs a layer above the settled text and drops back to it
		// on arrival.
		ch.RegisterEvent(PathComplete, PathCaller(input.ID), SetLayer(0))
		e.ActivatePath(ch, apex.ID)

		shell = append(shell, ch)
	}
	if len(shell) > 0 {
		f.shells = append(f.shells, shell)
	}
	return nil
}

// prepareScenes gives each shell a colour and each character the three looks
// it wears: the climbing spark, the burst flash, and the ramp home.
func (f *Fireworks) prepareScenes(e *Engine) error {
	finalGradient, err := NewGradient(f.config.FinalGradientStops, f.config.FinalGradientSteps, false)
	if err != nil {
		return err
	}
	canvas := e.Terminal.Canvas
	mapping, err := finalGradient.BuildCoordinateColorMapping(
		canvas.TextBottom, canvas.TextTop, canvas.TextLeft, canvas.TextRight,
		f.config.FinalGradientDirection)
	if err != nil {
		return err
	}
	fallback := finalGradient.Spectrum[0]
	dynamic := e.Terminal.Config.ExistingColorHandling == DynamicExistingColors

	for _, ch := range e.Terminal.GetCharacters(e.Rng, InputOnly(), SortTopToBottomLeftToRight) {
		final := Fg(mapping.At(ch.InputCoord, fallback))
		if dynamic {
			// May be the empty pair, for a character the input gave no colour
			// of its own. The fall scene has a branch for that.
			final = ch.Animation.InputColors
		}
		f.finalColors[ch] = final
	}

	white := MustParseColor("FFFFFF")
	for _, shell := range f.shells {
		shellColor := *Choice(e.Rng, f.config.FireworkColors)
		shellGradient, err := NewGradientSteps([]Color{shellColor, white, shellColor}, 5, false)
		if err != nil {
			return err
		}
		for _, ch := range shell {
			// The climb: the shell symbol blinking between its own colour and
			// white, looped for as long as the climb lasts.
			launch := ch.Animation.NewScene("", SceneOptions{
				Looping: true, UsesInputColors: ch.UsesInputColors, Frames: 2,
			})
			if err := launch.AddFrame(f.config.FireworkSymbol, 2,
				VisualParams{Colors: Fg(shellColor)}); err != nil {
				return err
			}
			if err := launch.AddFrame(f.config.FireworkSymbol, 1,
				VisualParams{Colors: Fg(white)}); err != nil {
				return err
			}

			// The burst: the character's own glyph flashing through white and
			// back, stepped along with the flight rather than the frame count.
			bloom := ch.Animation.NewScene("", SceneOptions{
				Sync: SyncStep, UsesInputColors: ch.UsesInputColors,
				Frames: len(shellGradient.Spectrum),
			})
			for _, color := range shellGradient.Spectrum {
				if err := bloom.AddFrame(ch.InputSymbol, 2, VisualParams{Colors: Fg(color)}); err != nil {
					return err
				}
			}

			// The fall: a ramp from the shell colour to where the character
			// settles. Both channels ramp, so a cell that carried a background
			// picks it back up rather than losing it for the run.
			fall := ch.Animation.NewScene("fall_scn", SceneOptions{
				UsesInputColors: ch.UsesInputColors, Frames: 16,
			})
			finalColors := f.finalColors[ch]
			var fgGradient, bgGradient *Gradient
			if finalColors.HasFg {
				if fgGradient, err = NewGradientSteps(
					[]Color{shellColor, finalColors.Fg}, 15, false); err != nil {
					return err
				}
			}
			if finalColors.HasBg {
				if bgGradient, err = NewGradientSteps(
					[]Color{shellColor, finalColors.Bg}, 15, false); err != nil {
					return err
				}
			}
			if fgGradient == nil && bgGradient == nil {
				if err := fall.AddFrame(ch.InputSymbol, 10, VisualParams{}); err != nil {
					return err
				}
			} else if err := fall.ApplyGradientToSymbols(
				[]string{ch.InputSymbol}, 10, fgGradient, bgGradient); err != nil {
				return err
			}

			e.ActivateScene(ch, launch.ID)
			ch.RegisterEvent(PathComplete, PathCaller("apex_pth"), ActivateScene(bloom.ID))
			ch.RegisterEvent(PathActivated, PathCaller("input_pth"), ActivateScene(fall.ID))
		}
	}
	return nil
}

// Advance launches a shell when the delay has run out, runs one frame, and
// reports whether the effect is still going.
func (f *Fireworks) Advance(e *Engine) bool {
	if len(f.shells) == 0 && e.ActiveCount() == 0 {
		return false
	}
	if len(f.shells) > 0 && f.launchDelay <= 0 {
		shell := f.shells[len(f.shells)-1]
		f.shells = f.shells[:len(f.shells)-1]
		for _, ch := range shell {
			e.Terminal.SetCharacterVisibility(ch, true)
			e.Activate(ch)
		}
		// Upstream truncates this rather than rounding it.
		f.launchDelay = int(float64(f.config.LaunchDelay) * e.Rng.Uniform(0.5, 1.5))
	}
	f.launchDelay--
	e.Update()
	return true
}
