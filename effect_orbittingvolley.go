package tuiffects

// orbittingvolley, ported from ttfx src/effects/orbittingvolley.rs, which
// ports TerminalTextEffects effects/effect_orbittingvolley.py by ChrisBuilds.

func init() {
	Register(Descriptor{
		Name:        "orbittingvolley",
		Description: "Four launchers circle the canvas and fire the characters into place",
		New:         func() Effect { return NewOrbittingVolley(DefaultOrbittingVolleyConfig()) },
	})
}

// OrbittingVolleyConfig tunes the orbittingvolley effect.
type OrbittingVolleyConfig struct {
	// TopLauncherSymbol and the three that follow are the glyphs the four
	// launchers are drawn with. The top one is the launcher that actually
	// moves; the other three are placed from its progress each frame.
	TopLauncherSymbol    string
	RightLauncherSymbol  string
	BottomLauncherSymbol string
	LeftLauncherSymbol   string
	// LauncherMovementSpeed is how fast the top launcher crosses the canvas.
	LauncherMovementSpeed float64
	// CharacterMovementSpeed is how fast a fired character flies to its home.
	CharacterMovementSpeed float64
	// VolleySize is the share of the input each volley fires, split across the
	// four launchers. One character per launcher is the floor.
	VolleySize float64
	// LaunchDelay is how many frames pass between volleys.
	LaunchDelay int
	// CharacterEasing shapes a fired character's flight.
	CharacterEasing Easing
	// FinalGradientStops colour the text once it lands. They are ignored when
	// the engine is set to resolve to the input's own colours.
	FinalGradientStops     []Color
	FinalGradientSteps     []int
	FinalGradientDirection GradientDirection
}

// DefaultOrbittingVolleyConfig is upstream's default orbittingvolley.
func DefaultOrbittingVolleyConfig() OrbittingVolleyConfig {
	return OrbittingVolleyConfig{
		TopLauncherSymbol:      "█",
		RightLauncherSymbol:    "█",
		BottomLauncherSymbol:   "█",
		LeftLauncherSymbol:     "█",
		LauncherMovementSpeed:  0.8,
		CharacterMovementSpeed: 1.5,
		VolleySize:             0.03,
		LaunchDelay:            30,
		CharacterEasing:        OutSine,
		FinalGradientStops:     []Color{MustParseColor("FFA15C"), MustParseColor("44D492")},
		FinalGradientSteps:     []int{12},
		FinalGradientDirection: Radial,
	}
}

// orbittingVolleyLauncher is one of the four corner launchers and the queue of
// characters it still has to fire. It is upstream's inner Launcher class.
type orbittingVolleyLauncher struct {
	character *Character
	magazine  []*Character
}

// OrbittingVolley circles four launchers around the edge of the canvas and
// fires the characters out of them a volley at a time. Each character flies
// from whichever launcher holds it to the cell it came from.
type OrbittingVolley struct {
	config OrbittingVolleyConfig

	launcherGradient CoordColorMap
	launcherFallback Color
	launchers        []*orbittingVolleyLauncher
	delay            int
	complete         bool
}

// NewOrbittingVolley builds the effect.
func NewOrbittingVolley(config OrbittingVolleyConfig) *OrbittingVolley {
	return &OrbittingVolley{config: config}
}

// The two path names this effect uses: the launcher's orbit, and the flight
// home that every character is given.
const (
	orbittingVolleyPerimeterPath = "perimeter"
	orbittingVolleyInputPath     = "input_path"
)

// Build gives every character the path home that a launcher will send it
// along, then places the four launchers and deals the characters out between
// them from the middle of the text outwards.
func (o *OrbittingVolley) Build(e *Engine) error {
	finalGradient, err := NewGradient(o.config.FinalGradientStops, o.config.FinalGradientSteps, false)
	if err != nil {
		return err
	}
	canvas := e.Terminal.Canvas
	mapping, err := finalGradient.BuildCoordinateColorMapping(
		canvas.TextBottom, canvas.TextTop, canvas.TextLeft, canvas.TextRight,
		o.config.FinalGradientDirection)
	if err != nil {
		return err
	}
	// The launchers ride the edge of the canvas, not the edge of the text, so
	// they read their colour from a second mapping painted across the whole
	// canvas.
	o.launcherGradient, err = finalGradient.BuildCoordinateColorMapping(
		canvas.Bottom, canvas.Top, canvas.Left, canvas.Right,
		o.config.FinalGradientDirection)
	if err != nil {
		return err
	}
	fallback := finalGradient.Spectrum[0]
	o.launcherFallback = fallback
	lastColor := finalGradient.Spectrum[len(finalGradient.Spectrum)-1]

	dynamic := e.Terminal.Config.ExistingColorHandling == DynamicExistingColors

	for _, ch := range e.Terminal.GetCharacters(e.Rng, InputOnly(), SortTopToBottomLeftToRight) {
		// This effect assembles the screen rather than passing over it: every
		// character is fired in from a launcher and is hidden until it is. So
		// the dynamic branch only changes the colour it lands wearing, and
		// characters stay hidden at build time in every colour mode.
		final := Fg(mapping.At(ch.InputCoord, fallback))
		if dynamic {
			// May be the empty pair, which lands the character wearing
			// nothing: the terminal default it arrived as.
			final = ch.Animation.InputColors
		}

		path, err := ch.Motion.NewPath(orbittingVolleyInputPath, PathOptions{
			Speed:    o.config.CharacterMovementSpeed,
			Ease:     o.config.CharacterEasing,
			HasEase:  true,
			Layer:    1,
			HasLayer: true,
		})
		if err != nil {
			return err
		}
		if _, err := path.NewWaypoint(ch.InputCoord, nil, ""); err != nil {
			return err
		}
		ch.RegisterEvent(PathComplete, PathCaller(orbittingVolleyInputPath), SetLayer(0))
		ch.Animation.SetAppearance(ch.InputSymbol, final, ch.UsesInputColors)
	}

	specs := []struct {
		coord  Coord
		symbol string
	}{
		{C(canvas.Left, canvas.Top), o.config.TopLauncherSymbol},
		{C(canvas.Right, canvas.Top), o.config.RightLauncherSymbol},
		{C(canvas.Right, canvas.Bottom), o.config.BottomLauncherSymbol},
		{C(canvas.Left, canvas.Bottom), o.config.LeftLauncherSymbol},
	}
	for _, spec := range specs {
		ch := e.Terminal.AddCharacter(spec.symbol, spec.coord)
		ch.Layer = 2
		e.Terminal.SetCharacterVisibility(ch, true)
		e.Activate(ch)
		o.launchers = append(o.launchers, &orbittingVolleyLauncher{character: ch})
	}

	main := o.launchers[0].character
	main.Animation.SetAppearance(main.InputSymbol, Fg(lastColor), main.UsesInputColors)
	if err := o.buildLauncherPath(e, main); err != nil {
		return err
	}
	e.ActivatePath(main, orbittingVolleyPerimeterPath)

	index := 0
	for _, group := range e.Terminal.GetCharactersGrouped(InputOnly(), GroupCenterToOutside) {
		for _, ch := range group {
			launcher := o.launchers[index%len(o.launchers)]
			launcher.magazine = append(launcher.magazine, ch)
			index++
		}
	}
	o.delay = 0
	return nil
}

// buildLauncherPath gives the top launcher the run it orbits along. Only the
// top launcher moves: the other three are placed from its progress.
func (o *OrbittingVolley) buildLauncherPath(e *Engine, ch *Character) error {
	canvas := e.Terminal.Canvas
	waypoints := []Coord{
		C(canvas.Left, canvas.Top),
		C(canvas.Right, canvas.Top),
	}
	start := 0
	for i, coord := range waypoints {
		if coord == ch.InputCoord {
			start = i
			break
		}
	}
	path, err := ch.Motion.NewPath(orbittingVolleyPerimeterPath, PathOptions{
		Speed:    o.config.LauncherMovementSpeed,
		Layer:    2,
		HasLayer: true,
	})
	if err != nil {
		return err
	}
	for i := 0; i < len(waypoints); i++ {
		if _, err := path.NewWaypoint(waypoints[(start+i)%len(waypoints)], nil, ""); err != nil {
			return err
		}
	}
	return nil
}

// launch fires the next character out of one launcher, or reports nil when
// that launcher is empty.
func (o *OrbittingVolley) launch(e *Engine, launcher *orbittingVolleyLauncher) *Character {
	if len(launcher.magazine) == 0 {
		return nil
	}
	next := launcher.magazine[0]
	launcher.magazine = launcher.magazine[1:]
	next.Motion.SetCoordinate(launcher.character.Motion.CurrentCoord)
	e.ActivatePath(next, orbittingVolleyInputPath)
	e.Terminal.SetCharacterVisibility(next, true)
	return next
}

// setLauncherCoordinates places one of the three trailing launchers from how
// far the top launcher has travelled, so the four stay a quarter turn apart.
func (o *OrbittingVolley) setLauncherCoordinates(e *Engine, parent, child *orbittingVolleyLauncher) {
	canvas := e.Terminal.Canvas
	progress := float64(parent.character.Motion.CurrentCoord.Column) / float64(canvas.Right)
	ch := child.character
	switch ch.InputCoord {
	case C(canvas.Right, canvas.Top):
		row := canvas.Top - int(float64(canvas.Top)*progress)
		ch.Motion.SetCoordinate(C(canvas.Right, max(1, row)))
	case C(canvas.Right, canvas.Bottom):
		column := canvas.Right - int(float64(canvas.Right)*progress)
		ch.Motion.SetCoordinate(C(max(1, column), canvas.Bottom))
	case C(canvas.Left, canvas.Bottom):
		row := canvas.Bottom + int(float64(canvas.Top)*progress)
		ch.Motion.SetCoordinate(C(canvas.Left, min(canvas.Top, row)))
	}
	color := o.launcherGradient.At(ch.Motion.CurrentCoord, o.launcherFallback)
	ch.Animation.SetAppearance(ch.InputSymbol, Fg(color), ch.UsesInputColors)
}

// hasAmmo reports whether any launcher still has a character to fire.
func (o *OrbittingVolley) hasAmmo() bool {
	for _, launcher := range o.launchers {
		if len(launcher.magazine) > 0 {
			return true
		}
	}
	return false
}

// Advance moves the launchers, fires a volley when the delay runs out, and
// reports whether the effect is still going. The last frame it produces is the
// one where the launchers are hidden, so the text is left on its own.
func (o *OrbittingVolley) Advance(e *Engine) bool {
	// More than one active character means something other than the top
	// launcher is still flying.
	if o.hasAmmo() || e.ActiveCount() > 1 {
		main := o.launchers[0].character
		if main.Motion.MovementIsComplete() {
			// The launcher reached the far side, so put it back at the start
			// of the run and send it round again.
			path := main.Motion.Path(orbittingVolleyPerimeterPath)
			main.Motion.SetCoordinate(path.Waypoints[0].Coord)
			e.ActivatePath(main, orbittingVolleyPerimeterPath)
			e.Activate(main)
		}
		color := o.launcherGradient.At(main.Motion.CurrentCoord, o.launcherFallback)
		main.Animation.SetAppearance(o.config.TopLauncherSymbol, Fg(color), main.UsesInputColors)
		for _, child := range o.launchers[1:] {
			o.setLauncherCoordinates(e, o.launchers[0], child)
		}

		if o.delay == 0 {
			// Upstream truncates the share to a whole number of characters
			// and keeps a floor of one, so a small input still fires.
			perLauncher := max(int(o.config.VolleySize*float64(len(e.Terminal.InputCharacters))/4.0), 1)
			for _, launcher := range o.launchers {
				for i := 0; i < perLauncher; i++ {
					if fired := o.launch(e, launcher); fired != nil {
						e.Activate(fired)
					}
				}
			}
			o.delay = o.config.LaunchDelay
		} else {
			o.delay--
		}

		e.Update()
		return true
	}

	if !o.complete {
		o.complete = true
		for _, launcher := range o.launchers {
			e.Terminal.SetCharacterVisibility(launcher.character, false)
		}
		return true
	}
	return false
}
